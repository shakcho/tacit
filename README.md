# Tacit

Tacit is the behavior-preserving rewrite of Listener: a Go control plane, Next.js web application, PostgreSQL canonical state, managed identity, Kubernetes workers, Zoom and Microsoft Teams connectors, owned speech-to-text, async media processing, and evidence-linked AI assistance.

This repository is a monorepo so application code, worker protocols, deployment contracts and generated clients change atomically.

## Repository map

| Path | Ownership |
|---|---|
| `apps/web` | Next.js web application and owned Tailwind/shadcn system |
| `cmd` | Go process entry points |
| `internal` | Go domain modules and private platform adapters |
| `packages/contracts` | Generated web clients and shared wire artifacts |
| `protocols` | Versioned cross-language protobuf/JSON contracts |
| `services/speech` | Python/CUDA inference boundary |
| `deploy/charts` | Kubernetes Helm charts |
| `docs/architecture` | Boundaries, decisions and operating contracts |

## Bootstrap

Install [mise](https://mise.jdx.dev/) and Docker, then run:

```bash
./scripts/bootstrap.sh
```

The pinned toolchain is Go 1.27.4, Node 26.13.1 LTS, pnpm 12.19.0, Python 3.13.18 and uv 0.11.19. Python 3.13 is intentional: speech/CUDA compatibility is more important than using the newest Python feature release.

Run the local CI equivalent at any time:

```bash
./scripts/verify.sh
```

## Branches

Use one ticket branch at a time, named `tac-NNN-short-description`. Merge only after the ticket records `VERIFIED`.
