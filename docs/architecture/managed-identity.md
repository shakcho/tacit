# Managed identity

Tacit uses WorkOS AuthKit for the browser session. PostgreSQL remains authoritative for local users, organizations, memberships, and roles.

For dashboard configuration, environment-variable discovery, webhook setup, and demo admission, follow the [WorkOS local setup runbook](../runbooks/workos-local-setup.md).

## Request contract

The browser sends only the AuthKit session cookie to Next.js. The catch-all `/api` route reads that session on the server, rejects a browser-supplied `Authorization` header, and forwards an allowlisted request to Go with one WorkOS access token. It does not forward cookies or `x-workos-*` headers.

The server-rendered product layout also calls `GET /v1/auth/session` before it renders any workspace screen. An authenticated WorkOS user without a current local admission sees the access-denied page instead of the product shell. An identity-store outage produces a separate unavailable state and never falls back to provider-only access.

Go checks the fixed `RS256` profile, the exact issuer, `client_id`, `sub`, `sid`, `org_id`, `iat`, and `exp`. The verifier obtains keys only from `TACIT_WORKOS_JWKS_URL`. The token cannot select an issuer, an algorithm, a key URL, or an identity namespace.

After verification, one `Authenticate` call performs these checks:

1. The session has no local denial record.
2. The subject and the organization are not blocked.
3. Immutable external subject and organization mappings exist.
4. The provider membership admission is active.
5. A current local membership and at least one local role exist.
6. For a tenant route, the requested local organization matches the mapped token organization.

`GET /v1/auth/session` uses the organization in the verified token to return the first local principal after sign-in. Tenant routes require `X-Tacit-Organization` and repeat the full admission check.

## Runtime variables

The web process uses these variables:

| Variable | Purpose |
|---|---|
| `WORKOS_CLIENT_ID` | Selects the WorkOS application. |
| `WORKOS_COOKIE_PASSWORD` | Encrypts the AuthKit session cookie. Use at least 32 random characters. |
| `WORKOS_ISSUER` | Matches the access token `iss` claim exactly. An unset value disables SDK issuer validation, so Tacit requires it. |
| `NEXT_PUBLIC_WORKOS_REDIRECT_URI` | Names the callback URI registered in WorkOS. |
| `WORKOS_API_KEY` | Enables WorkOS management APIs. Sign-in can use public-client mode without this key. |
| `TACIT_API_ORIGIN` | Names the private Go origin used by the Next.js API route. |
| `TACIT_WEB_ORIGIN` | Sets the allowed origin for cookie-authenticated mutations. |

The Go API uses these variables:

| Variable | Purpose |
|---|---|
| `TACIT_DATABASE_URL` | Connects the API to PostgreSQL through the runtime pooler. |
| `TACIT_IDENTITY_NAMESPACE` | Separates one WorkOS environment and application from every other identity deployment. |
| `TACIT_WORKOS_ISSUER` | Matches the verified JWT issuer exactly. |
| `TACIT_WORKOS_CLIENT_ID` | Matches the verified `client_id` claim. |
| `TACIT_WORKOS_JWKS_URL` | Supplies the pinned HTTPS JWKS endpoint. |
| `TACIT_WORKOS_WEBHOOK_SECRET` | Verifies the raw WorkOS webhook body. |

If no Go identity variable is set, the API starts without identity dependencies for public development checks. A process with `TACIT_ENV=production` refuses to start in that mode. If any identity variable is set, every required variable must be present and PostgreSQL must use schema version 2.

## Lifecycle behavior

`POST /webhooks/workos` verifies `WorkOS-Signature` against the exact raw body before it parses JSON. The endpoint rejects timestamps outside the configured tolerance. It writes the event ID, the SHA-256 payload hash, and the applied effect in one database transaction.

The handler applies these events:

- `session.revoked` creates an indefinite session denial.
- `user.deleted` blocks the external subject.
- `organization.deleted` blocks the external organization and denies its provider admissions.
- organization membership creation or an active update creates the first active provider admission.
- organization membership deletion or an inactive update denies the provider admission.

A duplicate event with the same hash is a successful replay. Reuse of the event ID with a different hash returns a conflict. An active event cannot clear a denied admission. Reactivation needs a future authoritative reconciliation path. WorkOS events never remove a local membership or change a local role.

The Next.js sign-out route writes the local session denial before it redirects to WorkOS logout. If the denial write fails, Tacit preserves the browser session and returns an error.

## Invitations and organization switching

WorkOS sends invitation links through its hosted AuthKit flow. The normal callback stores the selected organization in the WorkOS session. Tacit does not use the invitation email address to find or merge a local user.

An invitation grant must name an external invitation, a local organization, and a local role before acceptance. The verified `invitation.accepted` event binds the accepted WorkOS subject to that grant. The user still needs an active provider membership event before admission succeeds.

Register the grant after WorkOS returns the invitation ID and before the invitee accepts it:

```bash
TACIT_ALLOW_INVITATION_GRANT=1 \
go run ./cmd/tacit-db invitation-grant \
  --namespace workos-staging-client_replace_me \
  --external-invitation invitation_replace_me \
  --organization-id org_00000000000000000000000000000001 \
  --role viewer
```

The command uses `TACIT_DATABASE_DIRECT_URL`. An identical retry succeeds. A different local organization or role for the same external invitation fails.

`POST /auth/switch-organization` asks AuthKit for a new organization-bound access token. The route sends that token to `GET /v1/auth/session` before it resumes product navigation. The check reloads the local organization mapping, provider admission, membership, and roles.

## Legacy token cutoff

The new Tacit API has no legacy access-token or refresh-token verifier. The compatibility window is zero from the first deployment of the Go control plane. A rollback may use only a release that keeps legacy tokens disabled.

This implementation admits explicitly bootstrapped identities and users created from registered invitation grants. It does not infer that an imported Listener user and a new WorkOS subject are the same person. Before Listener users are imported, Tacit needs the two-sided, single-use account-link intent and audited recovery flow specified in ADR 0003.

## Open release gates

Local fixtures prove the token profile and webhook state changes. They do not prove WorkOS staging behavior. Production remains blocked until staging evidence confirms:

- the exact issuer, algorithm, claims, token lifetime, and JWKS rotation behavior;
- hosted sign-in, invitation acceptance, organization switching, and sign-out;
- remote session revocation and measured webhook delay;
- duplicate, delayed, and missed-event recovery; and
- two-sided account linking and recovery before any Listener user import;
- log redaction for cookies, tokens, callback codes, webhook bodies, signatures, and vendor errors.

The Go verifier currently uses a strict dependency-free `RS256` implementation because the development environment could not fetch the selected maintained JOSE module. Replace that adapter with the maintained module before closing the production gate. The provider-neutral `CredentialVerifier` interface and its conformance tests remain unchanged.
