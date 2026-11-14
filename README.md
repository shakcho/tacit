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

Start the development database and apply the embedded forward migrations:

```bash
pnpm db:up
export TACIT_DATABASE_DIRECT_URL='postgres://tacit_dev:tacit_dev@127.0.0.1:54329/tacit_dev?sslmode=disable'
pnpm db:migrate
```

Create a separate database whose name starts with `tacit_test_`, then run the PostgreSQL integration suite:

```bash
PGPASSWORD=tacit_dev createdb --host 127.0.0.1 --port 54329 --username tacit_dev tacit_test_source
export TACIT_TEST_DATABASE_URL='postgres://tacit_dev:tacit_dev@127.0.0.1:54329/tacit_test_source?sslmode=disable'
pnpm test:postgres
```

The API shell remains memory-backed until managed authentication can supply an actor and an active organization. TAC-005 tests the PostgreSQL foundation without exposing an unauthenticated tenant selector.

## Local development

Start the current application processes together from the repository root:

```bash
pnpm dev
```

This starts the Next.js workspace at `http://127.0.0.1:3000/meetings` and the Go API shell at `http://127.0.0.1:8080`. The API exposes health/readiness probes, its canonical contract at `GET /openapi.yaml`, and the typed `/v1/shell-samples` exercise endpoints. The browser uses the generated Orval client through the Next.js `/api` proxy. Press `Ctrl+C` once to stop both processes.

Override the default listeners when needed:

```bash
TACIT_WEB_PORT=3001 TACIT_API_ADDRESS=127.0.0.1:8081 pnpm dev
```

## Branches

Use one ticket branch at a time, named `tac-NNN-short-description`. Merge only after the ticket records `VERIFIED`.
