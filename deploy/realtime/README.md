# Two-replica SSE fixture

Export the staging WorkOS API variables from the [managed identity configuration](../../docs/architecture/managed-identity.md), along with a shared `TACIT_REALTIME_CURSOR_KEY`. Start the fixture from the repository root.

```bash
docker compose -f deploy/realtime/compose.yaml up --build -d
```

Use [demo bootstrap](../../docs/architecture/managed-identity.md) with `TACIT_DATABASE_DIRECT_URL=postgres://tacit_fixture:tacit_fixture@127.0.0.1:54331/tacit_fixture?sslmode=disable` to seed the fixture database for an admitted staging user. Set `TACIT_FIXTURE_BEARER` to that user's organization-bound staging bearer. Keep the token in your local environment.

```bash
python3 deploy/realtime/verify.py
docker compose -f deploy/realtime/compose.yaml down
```

The verifier fetches one snapshot, opens the same token-bound cursor twice, verifies that the ingress selects different replicas, and reads four incremental heartbeats on the second connection. The fixture uses round-robin balancing, disables buffering, and allows 35 seconds of upstream inactivity. The PostgreSQL integration suite exercises writes, replay, and revocation independently of the staging identity provider.
