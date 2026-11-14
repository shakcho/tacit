# OpenAPI control-plane shell

## Decision

TAC-004 exposes the product control plane as versioned HTTP operations described by OpenAPI 3.1. The OpenAPI document is the caller-visible source of truth. Go owns authorization and product behavior; transport structs are parsed into domain values at the adapter boundary.

The Next.js application uses an Orval-generated Fetch/TanStack Query client. Realtime product updates will use a separate server-sent event stream containing small invalidations and replay cursors. Provider media, raw audio/video, and speech-executor streams remain outside the product HTTP API.

See [ADR 0001](decisions/0001-openapi-control-plane.md) for the transport decision.

## Usage

Start the API and inspect the contract:

```text
go run ./cmd/api --listen 127.0.0.1:8080
go run ./cmd/api --version
GET  /healthz
GET  /readyz
GET  /openapi.yaml
POST /v1/shell-samples
GET  /v1/shell-samples/{sampleId}
```

The web application calls the root-relative `/api` base path. Next.js proxies it to the server-only `TACIT_API_ORIGIN`, so browser code never needs a private service hostname and local development avoids a separate CORS policy.

```text
Browser /api/v1/shell-samples -> Next.js proxy -> Go /v1/shell-samples
```

`NEXT_PUBLIC_API_BASE_PATH` can change the browser-visible prefix. It must remain root-relative. `TACIT_API_ORIGIN` is server-only.

## Contract ownership

`internal/adapters/httpapi/openapi.yaml` owns paths, operation IDs, parameters, response statuses, and JSON shapes. It uses OpenAPI 3.1 rather than the newest specification version until the selected Go and TypeScript generators prove the same 3.2 feature set.

The contract follows these rules:

- Paths are resource- and workflow-oriented, not table-shaped CRUD.
- Commands use normal HTTP semantics, including `Idempotency-Key`, `Location`, and specific success statuses.
- Errors use `application/problem+json` with a stable machine `code` in addition to RFC 9457 fields.
- Request IDs are accepted or generated once and returned as `X-Request-ID`.
- Media time is a canonical decimal string containing nonnegative recording-relative microseconds.
- IDs use documented canonical wire formats and are parsed once at the boundary.
- Async work will return `202 Accepted` with a job resource rather than holding a request open.

The shell sample proves the boundary without defining a product aggregate. The create operation accepts this body:

```json
{
  "id": "smp_0123456789abcdef0123456789abcdef",
  "offsetMicroseconds": "1234567"
}
```

It requires one `Idempotency-Key` header. A new command returns `201 Created`; an exact retry returns the original result with `200 OK` and `replayed: true`. Reusing a key for another command or reusing an ID for another sample returns `409 Conflict` with a distinct problem code.

## Boundary behavior

`internal/adapters/httpapi` owns HTTP decoding, OpenAPI wire structs, problem responses, and translation into domain values. Transport types never enter a domain package.

The adapter enforces these contracts before calling a domain interface:

- JSON command bodies are limited to one MiB, including streamed bodies.
- `Content-Type` must be `application/json`.
- Unknown JSON fields, malformed bodies, and trailing JSON values are rejected.
- IDs, media times, and idempotency keys are converted to validated domain types exactly once.
- Stable problem codes are allowlisted and details contain no internal error or secret content.

The domain-facing interface remains independent of HTTP:

```go
type Samples interface {
	Create(context.Context, CreateCommand) (CreateResult, error)
	Find(context.Context, ID) (Sample, bool, error)
}
```

The in-memory adapter owns sample and idempotency indexes under one mutex. Its state is deliberately process-local and bounded; it proves typed outcomes and retry behavior, not durable multi-replica idempotency.

## Frontend generation

`apps/web/orval.config.ts` reads the canonical OpenAPI document and writes disposable code under the ignored `apps/web/src/api/generated` directory. The generated surface includes:

- Fetch request functions;
- TanStack Query hooks and query keys;
- request, response, and problem models; and
- typed mutation error unions.

`apps/web/src/api/client.ts` is the small hand-authored transport policy. It applies same-origin credentials, parses response bodies, and throws `APIError` for non-success HTTP responses so TanStack Query never caches a problem response as successful data.

The application owns one `QueryClient` through `QueryProvider`. Product features consume generated hooks; they do not hand-build URLs or duplicate response types.

Orval is pinned in the workspace lockfile. `pnpm generate` is deterministic, and development, CI and the web container run it before consuming the client. `scripts/check-generated.sh` rejects committed Orval output and verifies that generation produces the expected hooks.

## Process lifecycle

`internal/platform/process` owns the listener, probes, admission state, request correlation, safe structured logging, and the drain deadline.

Shutdown follows one sequence:

1. mark readiness unavailable and reject new application work;
2. let active HTTP requests finish within the configured drain bound;
3. cancel request contexts and close the server at the deadline; and
4. report whether the process drained cleanly.

There is no WebSocket lifecycle in TAC-004. TAC-010 will add an SSE stream manager that closes long-lived event responses during drain and reconstructs state through a durable cursor. Raw media does not use that stream.

Logs use an allowlist: role, safe HTTP method, route template, status class, and request ID. Headers, bodies, raw URLs, transcript text, tokens, credentials, and arbitrary error strings are excluded.

## Modules

```text
cmd/api                         public API composition
cmd/worker-ingress              inert worker-result ingress composition
cmd/dispatch                    inert background dispatch composition
internal/runtime/shellsample    domain values, outcomes, and Samples interface
internal/adapters/httpapi       OpenAPI document, HTTP decoding, and response mapping
internal/platform/memory        bounded process-local Samples implementation
internal/platform/process       probes, admission, correlation, logging, and drain
apps/web/src/api/generated      ignored Orval/TanStack Query build output
```

Domain packages cannot import HTTP, adapters, platform implementations, SQL, or vendor SDKs. The repository boundary check enforces adapter and platform exclusions for every domain root.

## Generator decision

Orval is selected for the TypeScript client because the frontend directly consumes TanStack Query hooks. The Go shell currently keeps a small handwritten HTTP adapter while the repository evaluates `ogen` against strict `oapi-codegen` using this contract.

The Go generator spike must compare:

- OpenAPI 3.1 coverage and validation behavior;
- strict server interfaces and compile-time response variants;
- `application/problem+json`, headers, and SSE support;
- generated diff stability and maintenance activity; and
- OpenTelemetry integration without transport types leaking into domain packages.

Exactly one Go generator will be selected before the first product aggregate is ported. Tacit will not maintain parallel generated and handwritten product endpoints.

## Tradeoffs

- Purpose-built read models replace arbitrary client-selected field graphs. This keeps authorization, performance, and caching visible in the contract at the cost of adding an endpoint when a genuinely new view is required.
- SSE is one-way by design. It matches invalidation plus canonical refetch and avoids a bidirectional socket protocol that the product does not currently require.
- Orval output is disposable. Reviews focus on the OpenAPI source, generator configuration and handwritten transport policy; a pinned generator reproduces hooks in CI and builds.
- The temporary shell sample is not a product model and will be removed after real aggregates prove the same boundary.

## Verification

- Generate the Orval client from a clean checkout and verify that no generated hook is tracked by Git.
- Exercise create, replay, lookup, conflicts, invalid inputs, unknown JSON fields, unsupported content types, and oversized bodies.
- Confirm every problem response has `application/problem+json` and a stable code.
- Run boundary, Go, TypeScript, lint, and component tests.
- Terminate the process during an active request and verify completion or forced cancellation at the configured bound.
