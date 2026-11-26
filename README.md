# Tacit

Tacit is the behavior-preserving rewrite of Listener: a Go control plane, Next.js web application, PostgreSQL canonical state, managed identity, Kubernetes workers, Zoom and Microsoft Teams connectors, owned speech-to-text, async media processing, and evidence-linked AI assistance.

This repository is a monorepo so application code, worker protocols, deployment contracts, OpenAPI sources and pinned generators change atomically. Disposable frontend clients are generated from those sources during development, CI and container builds.

## Repository map

| Path | Ownership |
|---|---|
| `apps/web` | Next.js web application and owned Tailwind/shadcn system |
| `cmd` | Go process entry points |
| `internal` | Go domain modules and private platform adapters |
| `packages/contracts` | Shared wire artifacts used by more than one workspace |
| `protocols` | Versioned cross-language protobuf/JSON contracts |
| `services/speech` | Python/CUDA inference boundary |
| `deploy/charts` | Kubernetes Helm charts |
| `docs/architecture` | Boundaries, decisions and operating contracts |

## Bootstrap

Install [mise](https://mise.jdx.dev/) and Docker, then run:

```bash
./scripts/bootstrap.sh
```

The pinned toolchain is Go 1.27.4, Node 26.13.1 LTS, pnpm 10.33.2, Python 3.13.18 and uv 0.11.19. Python 3.13 is intentional: speech/CUDA compatibility is more important than using the newest Python feature release.

Run the local CI equivalent at any time:

```bash
./scripts/verify.sh
```

## Local PostgreSQL

Start the development database:

```bash
pnpm db:up
```

`pnpm dev` applies the embedded forward migrations when `TACIT_DATABASE_DIRECT_URL` is configured. Use `pnpm db:migrate` when you need to apply migrations without starting the application.

Create a separate database whose name starts with `tacit_test_`, then run the PostgreSQL integration suite:

```bash
PGPASSWORD=tacit_dev createdb --host 127.0.0.1 --port 54330 --username tacit_dev tacit_test_source
export TACIT_TEST_DATABASE_URL='postgres://tacit_dev:tacit_dev@127.0.0.1:54330/tacit_test_source?sslmode=disable'
pnpm test:postgres
```

The sample store remains memory-backed. When WorkOS variables are present, the API protects the sample routes with the same local identity and organization checks used by product routes.

Compose binds PostgreSQL to port `54330` by default. Set `TACIT_POSTGRES_PORT` before `pnpm db:up` to use another host port, and update the database URLs to match.

## Local development

For public pages and process health checks, start both application processes from the repository root:

```bash
pnpm dev
```

This command loads the repository-root `.env.local` and `.env` files. Existing shell variables take precedence, followed by `.env.local`, and then `.env`. When `TACIT_DATABASE_DIRECT_URL` is configured, the command applies database migrations before it starts the application. Next.js listens at `http://127.0.0.1:3000`, and the Go API listens at `http://127.0.0.1:8080`. The readiness check uses the public web and API health routes. Press `Ctrl+C` once to stop both processes.

Product pages fail closed until you configure WorkOS. The browser keeps its AuthKit session in an HttpOnly cookie. The Next.js `/api` route obtains the short-lived access token on the server and sends it to Go. Go verifies the token and resolves current PostgreSQL membership before it returns local identity data.

To run an authenticated demo, copy `.env.example` to `.env.local` or `.env` and replace every WorkOS placeholder. Configure these WorkOS application URLs:

- callback URI: `http://127.0.0.1:3000/auth/callback`
- initiate login URI: `http://127.0.0.1:3000/auth/sign-in`
- sign-out URI: `http://127.0.0.1:3000`
- invitation URL: use the hosted AuthKit default

Start PostgreSQL before you start the application:

```bash
pnpm db:up
```

Create the local demo mapping with exact IDs from the WorkOS staging dashboard. The command accepts no email address and refuses a non-loopback database URL.

```bash
TACIT_ENV=development TACIT_DEMO_BOOTSTRAP=1 \
go run ./cmd/tacit-db demo-bootstrap \
  --namespace workos-staging-client_replace_me \
  --external-user user_replace_me \
  --external-organization org_replace_me \
  --external-membership om_replace_me \
  --user-id usr_00000000000000000000000000000001 \
  --organization-id org_00000000000000000000000000000001 \
  --name "Tacit demo"
```

Now run `pnpm dev` and open `http://127.0.0.1:3000/meetings`. The command is idempotent for the same IDs. It fails on conflicting mappings.

See [managed identity](docs/architecture/managed-identity.md) for the runtime contract, webhook behavior, and release gates. Use the [WorkOS local setup runbook](docs/runbooks/workos-local-setup.md) when configuring credentials and dashboard URLs.

Use the [platform deployment runbook](docs/runbooks/deploy-platform.md) to deploy or roll back the managed web and API workloads.

Override the default listeners when needed:

```bash
TACIT_WEB_PORT=3001 TACIT_API_ADDRESS=127.0.0.1:8081 pnpm dev
```

## Branches

Use one ticket branch at a time, named `tac-NNN-short-description`. Merge only after the ticket records `VERIFIED`.
