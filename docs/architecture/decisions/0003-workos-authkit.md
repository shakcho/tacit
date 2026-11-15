# ADR 0003: Use WorkOS AuthKit for managed identity

- Status: accepted for implementation; sandbox release gate open
- Date: 2026-11-15
- Owners: access/security and web

## Context

Tacit needs hosted sign-in, secure sessions, organization membership, invitations, MFA, social login, enterprise SSO, and directory provisioning. The product keeps stable local users, organizations, memberships, and roles in PostgreSQL. An identity provider authenticates an external subject; it does not authorize access to Tacit resources.

TAC-006 compares WorkOS AuthKit and Clerk. TAC-007 will implement the selected provider. This ADR does not add production callbacks, middleware, webhooks, or identity persistence methods.

## Decision

Use WorkOS AuthKit for the first production identity integration.

WorkOS matches Tacit's near-term enterprise path. Its AuthKit organization model, SAML and OIDC SSO, Directory Sync, session events, and free staging environment fit a product that connects Zoom and Microsoft Teams for company workspaces. Clerk remains a viable replacement and has lower published entry pricing for initial SSO and Directory Sync connections.

The browser will use the WorkOS Next.js SDK for hosted flows and session renewal. Go will verify each access token locally from a pinned issuer, client ID, and JWKS. Go will then resolve the external subject and organization to stable local IDs and load current local membership. Go will not call WorkOS on every product request.

Production has one authentication operation:

```go
Authenticate(ctx, bearerToken, requestedLocalOrganization) (LocalPrincipal, error)
```

This operation hides token verification, denial checks, organization mapping, and current membership lookup. It returns local IDs only. Vendor claims, roles, SDK values, and JWT objects do not enter `internal/access` or product handlers.

## Comparison

The comparison uses vendor documentation reviewed on 2026-11-15. Live sandbox cases have not run because the repository has no vendor credentials. The release gate below keeps that missing evidence visible.

| Requirement | WorkOS AuthKit | Clerk | Decision effect |
|---|---|---|---|
| Next.js hosted flows | The official SDK supports callbacks, renewal, sign-out, and organization switching. | The official SDK provides hosted components, session state, and an organization switcher. | Both fit. |
| Local Go verification | Session JWTs expose a client-specific JWKS, `sub`, `sid`, `client_id`, and `org_id`. | Session JWTs expose JWKS, `sub`, `sid`, `azp`, and version 2 `o.id`. | Both support local verification without a per-request vendor call. |
| Invitations and multiple organizations | Invitations cover new and existing users. Refresh can select another authorized organization. | Organization invitations and active-organization switching are first-class flows. | Both fit. |
| MFA and social login | AuthKit includes MFA, passkeys, and common social providers. | Clerk supports MFA and social providers. Production MFA requires a paid plan. | Both fit. Plan terms differ. |
| Enterprise lifecycle | SAML and OIDC SSO, Directory Sync, directory provisioning, and Admin Portal are separate documented products. | SAML and OIDC SSO plus SCIM and Google Workspace Directory Sync are available on eligible plans. | WorkOS better matches the first enterprise integration milestone. |
| Revocation events | AuthKit emits `session.revoked` and organization-membership lifecycle events. | Clerk supports session revocation and webhooks. | Both still need local denial state and an expiry bound. |
| Test environment | WorkOS documents a free staging environment and a test identity provider. | Clerk development instances support integration testing. | WorkOS provides the clearer enterprise test path. |
| Export and replacement | APIs expose users, organizations, memberships, events, and directory records. Export terms and retention still require contract review. | Backend APIs and dashboard export cover user and organization data. Export terms and retention still require contract review. | Neither provider replaces Tacit's local identity map. |
| Published pricing | AuthKit is free up to 1 million monthly active users. SSO and Directory Sync cost $125 per connection for the first 15 connections. | The free tier includes 50,000 monthly retained users and 100 monthly retained organizations. Eligible paid plans include one SSO and one Directory Sync connection; additional connections 2 through 15 cost $75 each. Directory Sync billing starts on 2027-01-01. | Clerk is cheaper at small enterprise-connection counts. Recheck terms before procurement. |

Primary sources include [WorkOS sessions](https://workos.com/docs/authkit/sessions), [WorkOS invitations](https://workos.com/docs/authkit/invitations), [WorkOS Directory Sync](https://workos.com/docs/directory-sync), [WorkOS events](https://workos.com/docs/events), [WorkOS environments](https://workos.com/docs/authkit/environments), [WorkOS pricing](https://workos.com/pricing), [Clerk session tokens](https://clerk.com/docs/guides/sessions/session-tokens), [Clerk manual JWT verification](https://clerk.com/docs/guides/sessions/manual-jwt-verification), [Clerk organizations](https://clerk.com/docs/nextjs/guides/organizations/getting-started), [Clerk sign-in options](https://clerk.com/docs/guides/configure/auth-strategies/sign-up-sign-in-options), and [Clerk pricing](https://clerk.com/pricing).

## Token admission policy

Trusted deployment configuration supplies the identity namespace, exact issuer, expected client ID, and JWKS URL. A token cannot select its issuer, algorithm, JWKS host, or application binding.

The WorkOS browser-session profile requires the following claims:

- `sub` for the external user;
- `sid` for the external session;
- `client_id` equal to the configured application;
- `org_id` for the selected organization;
- `iat` and `exp`; and
- an exact issuer confirmed from a real staging token.

The WorkOS session reference and session guide differ on the trailing slash in the example issuer. TAC-007 must pin the value observed in the configured environment. The verifier must not normalize both spellings.

The Clerk comparison profile uses version 2 session tokens. It requires `sub`, `sid`, exact `azp`, nested `o.id`, `iat`, `nbf`, `exp`, and the configured issuer. It rejects deprecated version 1 flat `org_id` tokens.

Both profiles reject unsigned tokens, an unexpected algorithm, a forged signature, an unknown key, duplicate security claims, an unexpected issuer, the wrong application party, a missing active organization, future issuance, an invalid not-before time, expiry, and a lifetime above policy. The fixture spike proposes a five-minute maximum token lifetime and 30 seconds of clock tolerance. The staging run must prove that the selected WorkOS session policy can meet those values before TAC-007 adopts them.

The fixture verifier uses fixed local RSA keys and Go's standard library to keep the spike dependency-free. Production must use a maintained JOSE library and a bounded JWKS cache. An unknown key ID may trigger one bounded refresh. If no trusted key is available, authentication fails closed.

## Local ownership and account linking

The local identity key is `(identity namespace, external subject)`. The namespace identifies the vendor environment and application. A bare vendor name such as `workos` is insufficient. The existing `provider` column can store the compact namespace, but TAC-007 must document its deployment mapping.

TAC-007 must add a distinct mapping from `(identity namespace, external organization)` to `access.OrganizationID`. `tacit_membership_external_ids` cannot replace this mapping because that table already requires a local organization and user.

Account linking follows these rules:

1. An existing alias resolves its current local user.
2. A new account creates its local user and external alias in one transaction.
3. Linking a new external identity to an existing user requires fresh proof of the existing local identity and fresh authentication of the new identity.
4. A short-lived, single-use intent binds both identities. One transaction consumes the intent and inserts the alias.
5. An identical retry returns the original result. A conflicting alias fails and never moves between local users.
6. Email is an invitation address and display attribute. Matching email, verified email, or matching domain never links or merges a local account.
7. An organization invitation grants only the named local membership after the invited subject accepts it. It does not claim an unrelated existing account.

Before cutover, an authenticated legacy user can create a migration intent and complete a new WorkOS authentication. After cutover, an unresolved user enters audited recovery and must prove existing-account control independently of email. An administrator assertion alone cannot merge accounts.

## Revocation and tenant isolation

Offline JWT verification cannot discover an upstream revocation by itself. Tacit combines short token lifetime, verified WorkOS events, and local denial state.

Local sign-out writes session denial before it reports success or clears the browser session. `session.revoked`, membership deactivation, and user deletion events add session denial or advance a subject issuance cutoff. Duplicate and reordered events cannot clear denial state. TAC-007 keeps session denials indefinitely because a failed upstream logout can leave the provider session able to refresh. A later cleanup policy requires proof that WorkOS has ended the session and every issued token has expired.

Every protected operation maps the verified external organization to the requested local organization and loads current local membership. A token for organization B cannot satisfy a request for organization A, even when the user belongs to both. Vendor roles do not grant Tacit permissions.

Organization switching must obtain a new organization-bound token. The Next.js client must cancel organization-scoped work, clear or isolate TanStack Query data, and reconnect authenticated SSE after TAC-007 resolves the new local principal.

## Threat model

| Threat | Required control |
|---|---|
| Forged token or algorithm confusion | Fixed asymmetric algorithm profile, trusted keys, signature verification, and negative fixtures. |
| Wrong environment or client | Exact issuer and `client_id`; identity namespace comes from trusted configuration. |
| Cross-organization request | External-to-local organization mapping plus a fresh local membership lookup. |
| Stale or revoked session | Local denial state, monotonic event handling, short expiry, and fail-closed storage errors. |
| Email takeover or invitation collision | Two-sided account-link proof, single-use intents, unique aliases, and no email matching. |
| Webhook forgery or replay | Raw-body signature verification, bounded timestamp checks, durable event identity, and monotonic updates. |
| JWKS substitution or outage | Pinned HTTPS endpoint, bounded cache and refresh, no token-supplied key URL, and fail-closed unknown keys. |
| Callback or redirect attack | Exact redirect allowlist, state, PKCE, nonce where supported, secure same-site cookies, and no token logging. |
| Vendor administrator compromise | Local membership remains authoritative; sensitive support and recovery actions produce audit records. |

## Cutover policy

Tacit will not migrate legacy access tokens, refresh tokens, password hashes, or provider OAuth credentials.

The cutover sequence is:

1. Rehearse user aliases, organization mappings, collisions, invitations, and recovery on an isolated copy of the data.
2. Let signed-in legacy users create two-sided WorkOS migration intents before the cutoff.
3. Notify users that the switch requires a new sign-in and reconnection of Zoom and future meeting providers.
4. Stop legacy identity writes and export local mappings and unresolved-user reports.
5. Stop accepting every legacy access and refresh token at the declared cutoff.
6. Enable WorkOS admission and complete new-provider links or audited recovery.
7. Track failed admission, alias conflicts, invitation completion, and provider reconnection.

A rollback can restore an earlier application release only if it still rejects legacy sessions. Rollback never restores old password or provider credentials.

## Vendor exit

Tacit keeps stable local user, organization, membership, role, project, recording, and audit identifiers. A provider export supplements this local record; it does not replace it.

Before a contract is approved, verify API export rights, pagination, rate limits, audit retention, SSO and directory configuration portability, and deletion timelines. A versioned export records local aliases, external organization mappings, link attestations, memberships, invitations, and audit history. It excludes password secrets, session tokens, refresh tokens, MFA secrets, passkeys, and provider OAuth credentials.

To replace WorkOS:

1. Create a new identity namespace and provision the replacement provider in staging.
2. Map replacement organizations to the same local organization IDs.
3. Run the same conformance catalog against the replacement.
4. Link replacement subjects through proof of both authenticated identities or the audited recovery path. Never infer a link from email.
5. Stop new WorkOS admission, revoke old sessions, wait through the maximum token lifetime, and switch the trusted issuer and keys.
6. Remove old secrets, webhooks, and SDK code after the audit and retention periods.

## Evidence and release gate

`spikes/identity/identityprobe` runs the shared fixture cases for WorkOS and Clerk. The fixtures prove local cryptographic and claim-policy behavior. They do not prove vendor issuance, hosted flows, refresh behavior, event delivery, or commercial terms.

The following evidence remains open:

- hosted sign-in and callback in both sandboxes;
- hosted sign-out and refresh rejection in both sandboxes;
- remote session revocation and measured event delay in both sandboxes;
- organization switching in both sandboxes;
- independent Go verification of a real WorkOS staging token;
- invitation behavior for both new and existing users; and
- procurement review of export, support, retention, and current pricing.

TAC-007 may implement the contract after this decision. TAC-007 cannot merge as production-ready until the required sandbox rows pass or this ADR records an approved change to the gate.

## Architecture synthesis

The design review selected the conformance-probe candidate because it gives TAC-006 executable evidence without adding production authentication early. The final design takes the two-sided account-linking rule from the strict identity-boundary candidate. It takes the single organization-scoped `Authenticate` operation and the separate public-share boundary from the local-principal candidate.

The production contract does not expose a separate verifier result followed by a caller-controlled resolver. That split would let a handler omit revocation, organization mapping, or current membership checks.

## Rejected alternatives

### Return verified JWT claims to product handlers

This shape hides signature verification but makes each caller coordinate revocation, organization mapping, and membership checks. One missed step becomes an authorization bug.

### Build a complete multi-vendor authentication facade in TAC-006

This shape would add callbacks, middleware, webhooks, persistence, and lifecycle methods before the comparison has live sandbox evidence. TAC-007 owns those changes.

### Use provider roles as Tacit authorization

Provider roles cannot express Tacit's project, share, media, highlight, and collaboration rules. Go remains authoritative.

### Exchange provider identity for a Tacit-issued session

This shape can centralize browser sessions, but it makes Tacit own signing keys, refresh rotation, and another revocation system. The current requirements do not justify that responsibility.

## Consequences

- TAC-007 integrates one vendor, not a runtime provider switch.
- Production adds an organization-alias table and identity lifecycle operations in a new migration. Migration `000001_foundation.sql` remains unchanged.
- Identity verification adds local database reads for denial state, organization mapping, and current membership.
- Remote revocation has a measured, bounded delay. The system does not claim instant revocation from offline JWT verification.
- WorkOS costs more than Clerk for the first published enterprise connections. Procurement accepts or reopens that tradeoff before launch.
