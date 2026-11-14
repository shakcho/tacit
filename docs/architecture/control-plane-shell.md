# Control-plane shell design

## Problem

TAC-004 adds a typed GraphQL path to the existing Go HTTP shell. The change must preserve the current CLI, probe, and local development contracts. It must also prove the boundary between GraphQL, domain code, and an in-memory adapter without adding authentication, persistence, product aggregates, or job execution.

The process runtime has one more constraint. `http.Server.Shutdown` does not close hijacked WebSocket connections. The shell therefore needs one owner for HTTP admission, WebSocket admission, readiness, telemetry, and the drain deadline.

## Usage

The public API keeps these commands and routes:

```text
go run ./cmd/api --listen 127.0.0.1:8080
go run ./cmd/api --version
GET /healthz
GET /readyz
POST /graphql
```

The worker roles run as separate processes:

```text
go run ./cmd/worker-ingress
go run ./cmd/dispatch
```

The worker processes expose probes and bounded shutdown. They do not accept worker results or dispatch jobs until later tickets define those protocols.

The sample GraphQL mutation accepts `Idempotency-Key` and an optional `X-Request-ID` header. The query reads the sample from the same process. Sample state does not survive a restart and is not shared across replicas.

```graphql
mutation CreateShellSample($input: CreateShellSampleInput!) {
  createShellSample(input: $input) {
    __typename
    ... on ShellSampleCreated {
      sample { id offset }
      replayed
    }
    ... on ShellSampleConflict { code }
  }
}

query FindShellSample($id: ShellSampleID!) {
  shellSample(id: $id) { id offset }
}
```

The API root creates the GraphQL handler through the process runtime. The callback receives the WebSocket tracker before it builds the handler.

```go
samples := memory.NewShellSamples(memory.Limits{
	Samples:            256,
	IdempotencyEntries: 512,
})

return process.Run(ctx, cfg, func(sockets process.SocketTracker) http.Handler {
	return graph.NewHandler(samples, sockets)
})
```

The resolver sees only the domain interface:

```go
type Samples interface {
	Create(context.Context, CreateCommand) (CreateResult, error)
	Find(context.Context, ID) (Sample, bool, error)
}
```

## Shape

### Domain values and outcomes

`internal/runtime/shellsample` owns the sample interface and these validated values:

- `ID` stores 16 bytes. Its wire form is `smp_` followed by 32 lowercase hexadecimal characters.
- `MediaTime` stores nonnegative recording-relative microseconds in an `int64`. Its GraphQL form is a canonical decimal string.
- `IdempotencyKey` contains 1 to 128 characters from the documented ASCII allowlist.
- `CreateCommand` contains an ID, a media time, and an idempotency key.
- `CreateResult` has private fields and constructors for created, replayed, idempotency conflict, sample conflict, and unavailable outcomes.

Private fields prevent callers from constructing an invalid result. GraphQL scalar codecs parse wire values into domain values once. Domain code does not parse them again.

The in-memory adapter owns two indexes under one mutex. The first maps an ID to a sample. The second maps an idempotency key to the validated command and its original result. One lock makes command comparison, sample creation, and result recording one transition.

The sample uses these fixed rules:

- The same key and the same command return the original result as a replay.
- The same key and a different command return `IDEMPOTENCY_CONFLICT`.
- The same ID and the same media time under a new key return the existing sample and bind the key.
- The same ID and a different media time return `SAMPLE_CONFLICT`.
- A full adapter returns `UNAVAILABLE`. It does not evict an idempotency record.

These rules prove typed outcomes and atomic retries in one process. They do not promise durable or cross-replica idempotency.

### GraphQL boundary

`internal/adapters/graphql` owns the schema, generated gqlgen code, scalar codecs, resolvers, and the error presenter. Generated or transport types do not enter a domain package.

The boundary enforces these contracts:

- An HTTP body can contain at most 1 MiB. The limit also applies to streamed bodies.
- A WebSocket message can contain at most 1 MiB.
- Invalid IDs, media times, cursors, idempotency keys, and request bodies return stable codes.
- Mutations require one `Idempotency-Key` header.
- The shell rejects mutations over WebSocket. This keeps one idempotency convention.
- The error presenter returns only allowlisted codes and fixed messages.

`Cursor` remains a boundary codec because it describes navigation, not sample state. Its wire form is unpadded base64url containing `v1:<sample-id>`. The parser validates the version and the ID, then rejects noncanonical encodings.

### Process lifecycle and telemetry

`internal/platform/process` owns the listener, probe state, admission state, request metadata, telemetry providers, WebSocket registry, and drain deadline.

```go
type HandlerFactory func(SocketTracker) http.Handler

type Config struct {
	Role         string
	Listen       string
	DrainTimeout time.Duration
}

func Run(context.Context, Config, HandlerFactory) error
```

`Run` creates the socket registry before it calls the handler factory. The GraphQL adapter registers every accepted socket with that registry. Shutdown marks the process unready, rejects new application work, cancels active socket operations, drains HTTP and WebSocket work against one deadline, force-closes remaining work, and flushes telemetry with the time that remains.

Liveness remains healthy while the process drains. Readiness changes to unavailable before the listener closes. Tests observe the state transition inside the process and do not assume that probes stay reachable after listener shutdown starts.

Request middleware accepts one valid `X-Request-ID` value or creates a random value. It echoes the value and adds it to the request context, logs, and spans. Metrics never use request IDs as labels.

The telemetry allowlist contains the role, HTTP method, route template, status class, bounded GraphQL operation name, and request ID for logs and spans. Logs, spans, and metrics exclude request bodies, GraphQL variables, headers, raw URLs, transcript text, tokens, credentials, and arbitrary error strings.

### Modules

```text
cmd/api                         public API composition
cmd/worker-ingress              inert worker-result ingress composition
cmd/dispatch                    inert background dispatch composition
internal/runtime/shellsample    domain values, outcomes, and Samples interface
internal/adapters/graphql       schema, gqlgen output, scalars, and resolvers
internal/platform/memory        bounded process-local Samples implementation
internal/platform/process       probes, admission, telemetry, and drain
```

The composition roots stay small. They parse flags, provide role configuration, construct role dependencies, and call `process.Run`.

### Generated files and import rules

The repository pins gqlgen as a Go tool. `go generate` writes committed Go files under the GraphQL adapter. The generated-file check compares the complete expected file set and file contents, including new untracked output.

The boundary checker parses Go imports for every production file under a domain root. Domain packages cannot import GraphQL, SQL, HTTP, adapters, platform packages, or vendor SDKs. Positive and negative fixtures prove that the checker accepts a domain import and rejects each forbidden class.

The API container copies `go.mod`, `go.sum`, `cmd`, and `internal` before it compiles the executable.

## Synthesis decision

Candidate 2 is the base because its two-method domain interface hides the in-memory adapter's atomic behavior without adding a service and repository relay. It also defines one owner for readiness, socket admission, telemetry, and shutdown.

The design uses Candidate 1's plain `memory.Limits` value instead of a single-purpose functional option. The cross-judge found three required corrections. Media time remains in microseconds, the sample has one fixed conflict policy, and `process.Run` creates the socket tracker before GraphQL construction.

The design rejects Candidate 1's role-host packages. Those packages would forward handlers without hiding current policy.

## Tradeoffs accepted

- The sample API is temporary. It proves the dependency direction without claiming a product aggregate.
- The memory adapter uses one mutex. The lock keeps the sample transition atomic and easy to review.
- The worker roles are inert. Their roots prove deployment and lifecycle boundaries without inventing protocols.
- WebSocket mutations are unavailable. HTTP remains the only command path until a later ticket defines socket idempotency.

## Alternatives considered

A generic command bus would expose registration, routing, and transaction order to the GraphQL caller. The two-method interface hides more behavior with fewer concepts.

Separate service and repository interfaces would repeat the same methods and arguments. One domain interface keeps the resolver-to-adapter path at two calls.

A global GraphQL idempotency cache would compare wire variables and operation text. The chosen design compares validated domain commands inside the adapter.

## Implementation reconciliation

No implementation deviation is accepted yet. Update this section if implementation changes a caller contract, a type, or an ownership rule.

## Open questions and risks

- A real socket test must prove that the selected gqlgen transport exposes every accepted connection to `SocketTracker`.
- The managed development environment cannot download new Go modules. Generation and tests need a machine with Go module network access before the PR can claim completion.

## Next implementation step

Add the schema, domain values, in-memory adapter, and one GraphQL integration test. Then add the shared process runtime and its drain tests.
