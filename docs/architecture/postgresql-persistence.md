# PostgreSQL persistence

TAC-005 adds the shared PostgreSQL foundation. It does not expose tenant operations through the unauthenticated shell API.

## Ownership

The `access` module owns organizations, users, memberships, roles, and membership outcomes. The `runtime` module owns idempotency values. PostgreSQL implementations live under `internal/platform/postgres`.

Tenant identity is an explicit typed argument. A request context carries cancellation and deadlines, not an active organization. PostgreSQL sessions do not store tenant identity.

The store exposes complete membership operations. Each operation owns its transaction, lock order, audit write, and idempotency record. Callers cannot import a transaction or write the foundation tables through a generic repository.

## Tenant constraints

Global users can join several organizations. A membership is identified by `(organization_id, user_id)`. Membership roles and external aliases reference that composite key, so PostgreSQL rejects cross-organization references.

Every operation that changes the administrator set locks the organization row first. The transaction reads the current administrators after it obtains the lock. A final-admin removal or demotion changes neither the membership nor the audit log.

An idempotency record is identified by organization, operation scope, and client key. The store compares a versioned request fingerprint. A matching retry returns the stored domain result. A different request with the same key returns a conflict. The effect, the audit record, and the accepted result commit in one transaction.

## Migrations

`internal/platform/postgres/migrations` contains immutable forward migrations. The migration command uses a direct PostgreSQL connection, obtains one advisory transaction lock, and records each version and SHA-256 checksum in `tacit_schema_migrations`.

The API does not run migrations during startup. A deployment runs `tacit-db migrate` before it starts a binary that requires the new schema. The binary rejects a schema version outside its supported range.

Version 2 adds managed-identity organization mappings, admission state, session denials, invitation grants, and the lifecycle event inbox. The integration suite creates a populated version-1 schema in an isolated PostgreSQL schema, applies migration 2, and checks that the existing user, organization, membership, role, and identity alias remain intact.

## Managed service

Tacit uses Supabase as managed PostgreSQL. The browser still calls the Go API. Tacit does not use the Supabase Data API, Auth, or Realtime products for product data.

Application workloads use the shared transaction pooler. The Go driver disables prepared-statement and description caches for that endpoint. Migration, backup, and restore commands use a direct connection.

Supabase documents daily backups on paid plans and optional point-in-time recovery. Tacit treats those features as provider capabilities, not recovery proof. CI performs a logical dump and restore against disposable databases. Production requires a separate scheduled restore drill with recorded recovery time and checksums.

## Connection budget

[`deploy/database/connections.json`](../../deploy/database/connections.json) records two separate limits.

- PostgreSQL backend connections include the provider pool, direct operations, and the provider reserve.
- Pooler client connections include every process pool at its replica ceiling and rollout surge.

`scripts/check-database-budget.mjs` rejects a budget that reaches either limit. TAC-011 must use the same replica and surge ceilings in Kubernetes. Until TAC-011 adds those controls, the file proves the arithmetic and per-process caps only.

## Verification

The PostgreSQL integration suite must run against a real server. It covers the populated version-1 upgrade, composite tenant foreign keys, concurrent final-admin changes, duplicate commands, identity admission, invitation acceptance, lifecycle replay, and alternating tenant access through a reused pool.

The restore check dumps a quiescent disposable source, restores it into an empty disposable target, and compares deterministic data hashes and the migration ledger. A passing logical restore does not measure the managed point-in-time recovery objective.

## Synthesis decision

The architecture arena selected complete domain operations over a generic repository or public unit of work. The chosen design hides SQL, transactions, and lock order behind `access.Directory`. It keeps the TAC-004 shell sample in memory until managed authentication can resolve an actor and an active organization.

The losing design contributed an idempotent `Store.Close` contract and explicit pool acquisition and health-check bounds. Its generic command callback, HTTP-shaped stored result, unused HTTP dependency injection, and combined client/backend budget formula were rejected.

## Tradeoffs

- Membership changes within one organization serialize in exchange for a direct final-admin proof.
- Idempotency records remain until a later retention policy defines safe deletion.
- The first schema stores only access, external identity, audit, idempotency, and migration state. Product tables remain in their owner tickets.
- PostgreSQL RLS may add another check later, but Go policy and composite tenant constraints remain authoritative.

## Implementation reconciliation

The Go store owns transaction sequencing. PostgreSQL owns keys, references, checks, and uniqueness. No security-definer mutation functions are part of the first implementation because they would duplicate the Go transaction protocol.
