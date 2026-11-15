# Contributing

## Change flow

1. Start from `master` after the ticket dependencies are verified.
2. Create one `tac-NNN-*` branch.
3. Add behavior-focused tests and update versioned contracts in the same change.
4. Run `./scripts/verify.sh`.
5. Record `VERIFIED`, `NOT VERIFIED`, or `INCONCLUSIVE` in the pull request.

## Boundaries

- Domain packages under `internal/access`, `internal/recordings`, `internal/library`, `internal/runtime`, and `internal/delivery` own policy and canonical types.
- Domain packages cannot import adapters, platform implementations, or another domain's private package.
- `internal/adapters` translates external wire formats and contains no product policy.
- `internal/platform` implements storage, broker and infrastructure interfaces defined by owner modules.
- Cross-language work uses a versioned contract under `protocols`; workers never update canonical product tables directly.
- Canonical contracts and pinned generators are committed. Disposable generated clients are ignored and regenerated in development, CI, and container builds.

See [the boundary manifest](docs/architecture/boundaries.json) for the machine-enforced rules.
