# ADR 0002: Use Supabase as managed PostgreSQL

Status: accepted

## Context

Tacit needs one relational authority outside the Kubernetes clusters. The database must support normal PostgreSQL migrations, transactions, connection pooling, backups, and point-in-time recovery. Browsers must not bypass the Go authorization boundary.

## Decision

Use Supabase as a managed PostgreSQL host. Treat it as PostgreSQL infrastructure, not an application framework.

Use the shared transaction pooler for horizontally scaled application workloads. Disable pgx statement and description caches for those connections. Use direct connections for migrations, logical backups, and restores.

Keep the application schema, authorization, migrations, encryption policy, capacity plan, and restore drills under Tacit ownership. Use Supabase managed backups and optional point-in-time recovery as infrastructure controls.

Start production on a paid project with seven days of point-in-time recovery. Use an initial recovery point objective of five minutes and a recovery time objective of four hours. Run a logical restore drill before production, then at least monthly. Store the drill timestamp, duration, schema version, and data checksum outside the restored project.

The initial connection budget assumes a Supabase Micro database with 60 PostgreSQL connections and 20 provider-reserved connections. The [managed platform registry](../platform.md#capacity-during-rollout) derives direct connections from role quotas and validates backend allocations per pooler tuple. Confirm the actual plan and pooler settings before deployment.

## Consequences

- The Next.js application continues to call the Go OpenAPI service.
- Tacit does not use Supabase Auth, Realtime, or generated Data APIs for product tables.
- Production uses a pooled application URL, a distinct direct LISTEN URL, and a separate direct DDL URL.
- The connection budget includes provider backend pools, direct operations, reserved capacity, process pools, replica ceilings, and rollout surge.
- A provider backup does not close the recovery acceptance criterion. Tacit records restore evidence from an isolated environment.

## Sources

- [Supabase connection management](https://supabase.com/docs/guides/database/connection-management)
- [Supabase connection methods](https://supabase.com/docs/guides/database/connecting-to-postgres)
- [Supabase backups and point-in-time recovery](https://supabase.com/docs/guides/platform/backups)
- [Supabase shared responsibility model](https://supabase.com/docs/guides/deployment/shared-responsibility-model)
