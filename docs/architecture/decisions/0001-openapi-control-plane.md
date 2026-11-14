# ADR 0001: Use OpenAPI for the product control plane

- Status: accepted
- Date: 2026-11-14
- Owners: product platform and web

## Context

The rewrite initially selected gqlgen GraphQL because the legacy Listener application used Apollo queries, mutations, subscriptions, and normalized cache updates. The recorded rewrite design did not compare GraphQL with an OpenAPI-first HTTP API.

Tacit does not preserve legacy wire compatibility. It has one primary Next.js client, while Zoom, Microsoft Teams, project-management tools, workers, webhooks, uploads, and signed media access all benefit from ordinary HTTP contracts. The new realtime requirement is also narrower than the legacy subscription surface: an authorized event invalidates cached state, and the client refetches the canonical resource. Reconnect uses a durable cursor and does not depend on a connection-local result stream.

## Decision

Use OpenAPI as the source of truth for product HTTP operations.

- Generate the Next.js Fetch/TanStack Query client with Orval.
- Use domain-oriented HTTP operations, typed problem responses, idempotency keys, resource revisions, and job resources.
- Deliver product invalidations through authenticated SSE with cursor replay and `RESYNC_REQUIRED`.
- Keep raw provider media, audio/video transfer, and speech/model streaming on separate data-plane protocols.
- Select one strict Go OpenAPI generator after a bounded `ogen` versus `oapi-codegen` spike.
- Do not run a parallel GraphQL product API.

## Consequences

OpenAPI becomes a reusable contract for browser, mobile, CLI, connector, webhook, and test consumers. HTTP caching, status codes, conditional requests, uploads, downloads, and observability route names remain explicit. Orval supplies generated query hooks and query keys without requiring Apollo's normalized graph cache.

Orval output is ignored rather than committed. The OpenAPI contract, Orval configuration, pinned package version and handwritten fetch policy are the reviewed inputs; development, CI and container builds regenerate the TanStack Query client before type checking or bundling.

Tacit gives up arbitrary per-client field selection. When a screen needs a materially different aggregate shape, the API adds a purpose-built read model. Realtime is intentionally one-way; any future bidirectional use case must justify a separate protocol rather than extending SSE beyond its role.

The GraphQL-specific TAC-004 and TAC-010 text, generation checks, environment variables, and lifecycle assumptions must be replaced. Transport-independent domain and process behavior remains valid.

## Rejected alternatives

### Keep GraphQL and gqlgen

GraphQL remains useful for many heterogeneous clients requesting arbitrary nested graphs or for rich graph payload subscriptions. Those requirements are not documented for Tacit, while field-level authorization, query-cost controls, resolver batching, cache normalization, and WebSocket lifecycle would remain mandatory.

### Run GraphQL and HTTP together

Maintaining two public schemas, two client-generation stacks, and two realtime models would increase migration and authorization risk without a known consumer requiring GraphQL compatibility.

### Use WebSockets for all realtime delivery

The current product requirement is server-to-client invalidation plus refetch. SSE uses normal HTTP infrastructure and supplies the required direction. Media and interactive streaming have different scaling and latency requirements and remain separate.
