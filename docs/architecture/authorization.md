# Authorization

Tacit's Go control plane is the only product authorization authority. WorkOS
proves the external identity; local admission maps it to a Tacit user,
organization, and current role set; the authorization catalog then decides an
action against an organization context or canonical resource facts.

## Request authority

`AuthenticationService` returns an opaque `LocalPrincipal`. It retains the
verified provider coordinates needed for an authoritative recheck, but it is
not a durable credential. Streams, jobs, worker results, search hydration, and
media grants must revalidate the current authority at their own boundary.

User, anonymous-share, and worker authority are distinct types. A user ID,
share ID, queue payload, resource ID, or previous allow decision is never
authority by itself.

## Decisions and presentation

`internal/access/authorization.go` contains one compiled capability catalog.
Each entry binds an action, resource, scope, and pure evaluator. Unknown or
mismatched combinations deny. Resource owners load canonical relationships and
project them into typed facts; HTTP input is never decoded into policy facts.

Go emits exact subject keys with every decision. The session endpoint returns
context decisions, while future resource operations return decisions for the
specific object they loaded. The web application filters allowed decisions and
copies `(action, subject)` into CASL. It has no role, ownership, collaborator,
publication, or wildcard permission matrix.

CASL is presentation only. Changing a browser rule cannot authorize a server
request.

## Membership administration

Membership writes use `MembershipAdministration`, not the provisioning
directory. PostgreSQL revalidates the actor and performs the mutation in one
transaction using this lock order:

1. identity-namespace advisory lock (shared for administration, exclusive for
   admission-changing identity writes);
2. organization row lock;
3. operation-specific idempotency advisory lock.

The transaction rechecks provider admission, local membership, current roles,
and exact organization before reading a replay receipt. It then protects the
final-admin invariant, writes the mutation and audit, and records a successful
removal receipt atomically. A revoked actor cannot replay a prior receipt.

Bootstrap is a trusted maintenance operation, but it follows the same identity
and organization lock protocol because it can add an administrator to an
existing organization.

## Deferred runtime integrations

TAC-008 defines decision and authority contracts for SSE, search, jobs, worker
results, media grants, and anonymous shares. Their runtime guarantees are owned
by later tickets:

- SSE must reauthorize and close affected streams within the measured bound.
- Search must recheck canonical visibility before returning data or snippets.
- Jobs persist immutable scope; worker results match job, resource, attempt,
  fence, and input revision in the commit transaction.
- Media grants and public shares revalidate exact scope and use documented
  expiry/revocation windows.

Policy fixtures are conformance inputs for those integrations, not evidence
that the deferred infrastructure already exists.
