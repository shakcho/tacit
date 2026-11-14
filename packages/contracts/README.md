# Generated contracts

Cross-application wire artifacts belong here when more than one workspace consumes them. Hand-authored product policy does not.

The control-plane OpenAPI source is owned by `internal/adapters/httpapi/openapi.yaml`. The pinned Orval version generates an ignored Next.js client next to its only current consumer under `apps/web/src/api/generated` during development, CI and container builds. Move stable generated models into this package when a second TypeScript application needs them; do not copy the OpenAPI source.
