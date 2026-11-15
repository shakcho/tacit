# WorkOS local setup

Use this runbook when configuring managed authentication for local Tacit development. Never commit `.env`, `.env.local`, API keys, cookie passwords, or webhook secrets.

## Setup checklist

- [ ] Copy the WorkOS Client ID and optional API key.
- [ ] Discover the exact session issuer and JWKS URI.
- [ ] Generate a cookie-encryption password.
- [ ] Configure callback, login, and sign-out URIs in WorkOS.
- [ ] Choose a stable Tacit identity namespace.
- [ ] Create a WorkOS webhook endpoint and copy its secret.
- [ ] Put the shared configuration in the repository-root `.env`.
- [ ] Start PostgreSQL, run migrations, and start the development stack.
- [ ] Bootstrap one exact WorkOS user, organization, and membership mapping.
- [ ] Verify sign-in, local admission, sign-out, and a test webhook.

## Environment-file ownership

`apps/web/.env.local` is read by Next.js only. The Go API does not read that file. `pnpm dev` loads the repository-root `.env` and passes it to both processes, so keep the complete local configuration there.

Tacit does not read `TACIT_WORKOS_API_KEY`. The WorkOS API key, when used, must be named `WORKOS_API_KEY`. It is optional for the current AuthKit sign-in flow because the SDK supports PKCE public-client mode.

The same WorkOS Client ID is required under two names:

```dotenv
WORKOS_CLIENT_ID=client_replace_me
TACIT_WORKOS_CLIENT_ID=client_replace_me
```

`WORKOS_CLIENT_ID` configures AuthKit in Next.js. `TACIT_WORKOS_CLIENT_ID` pins the expected token application in the Go API.

## Discover issuer and JWKS

Do not copy an issuer or JWKS URI from an example. Query the OpenID discovery document for the configured WorkOS Client ID:

```bash
client_id='client_replace_me'

curl -fsS \
  "https://api.workos.com/user_management/${client_id}/.well-known/openid-configuration" \
  | jq '{issuer, jwks_uri}'
```

Copy the returned values exactly, including any trailing slash:

```dotenv
WORKOS_ISSUER=https://issuer-returned-by-discovery.example
TACIT_WORKOS_ISSUER=https://issuer-returned-by-discovery.example
TACIT_WORKOS_JWKS_URL=https://jwks-uri-returned-by-discovery.example
```

Next.js and Go must use the same issuer. Go also obtains signing keys only from the discovered, pinned JWKS URI.

## Generate the cookie password

Generate the AuthKit session-cookie encryption password locally. This value does not come from WorkOS:

```bash
openssl rand -base64 32
```

Store the result as a quoted value with at least 32 characters:

```dotenv
WORKOS_COOKIE_PASSWORD="replace_with_generated_value"
```

## Configure WorkOS redirects

In WorkOS Dashboard, open the application and configure its Redirects settings. The default development stack uses `127.0.0.1`; use that hostname consistently instead of mixing it with `localhost`.

| Setting | Local value |
|---|---|
| Redirect URI | `http://127.0.0.1:3000/auth/callback` |
| Initiate login URI | `http://127.0.0.1:3000/auth/sign-in` |
| Default sign-out URI | `http://127.0.0.1:3000` |

Add the callback to `.env`:

```dotenv
NEXT_PUBLIC_WORKOS_REDIRECT_URI=http://127.0.0.1:3000/auth/callback
```

## Choose the identity namespace

The identity namespace is a Tacit-owned identifier, not a WorkOS credential. It separates mappings from different WorkOS applications and environments. Choose it once and do not rename it after creating mappings.

```dotenv
TACIT_IDENTITY_NAMESPACE=workos-staging-client_replace_me
```

The value must be between 1 and 64 characters. Shorten the Client ID portion if necessary, but keep the result unique and stable.

## Configure the webhook

WorkOS requires a publicly reachable HTTPS endpoint; it will not deliver webhooks directly to localhost. For local testing, expose port `8080` through an HTTPS tunnel and register:

```text
https://your-public-development-host/webhooks/workos
```

Subscribe only to the events Tacit handles:

- `session.revoked`
- `user.deleted`
- `organization.deleted`
- `organization_membership.created`
- `organization_membership.updated`
- `organization_membership.deleted`
- `invitation.accepted`

Copy the generated endpoint secret:

```dotenv
TACIT_WORKOS_WEBHOOK_SECRET=whsec_replace_me
```

Do not substitute an API key for the webhook secret. They have different purposes and are validated differently.

## Root `.env` template

Create `/path/to/tacit/.env` from this template and replace every placeholder:

```dotenv
TACIT_ENV=development

TACIT_DATABASE_URL=postgres://tacit_dev:tacit_dev@127.0.0.1:54329/tacit_dev?sslmode=disable
TACIT_DATABASE_DIRECT_URL=postgres://tacit_dev:tacit_dev@127.0.0.1:54329/tacit_dev?sslmode=disable
TACIT_TEST_DATABASE_URL=postgres://tacit_dev:tacit_dev@127.0.0.1:54329/tacit_test_source?sslmode=disable

TACIT_API_ORIGIN=http://127.0.0.1:8080
TACIT_WEB_ORIGIN=http://127.0.0.1:3000

WORKOS_CLIENT_ID=client_replace_me
# Optional for the current sign-in flow; required for WorkOS management API calls.
WORKOS_API_KEY=sk_test_replace_me
WORKOS_COOKIE_PASSWORD="replace_with_generated_value"
WORKOS_ISSUER=https://issuer-returned-by-discovery.example
NEXT_PUBLIC_WORKOS_REDIRECT_URI=http://127.0.0.1:3000/auth/callback

TACIT_IDENTITY_NAMESPACE=workos-staging-client_replace_me
TACIT_WORKOS_CLIENT_ID=client_replace_me
TACIT_WORKOS_ISSUER=https://issuer-returned-by-discovery.example
TACIT_WORKOS_JWKS_URL=https://jwks-uri-returned-by-discovery.example
TACIT_WORKOS_WEBHOOK_SECRET=whsec_replace_me
```

If the application runs in public-client mode, remove the entire `WORKOS_API_KEY` line rather than leaving a production-looking placeholder.

## Start the stack

```bash
pnpm db:up
pnpm db:migrate
pnpm dev
```

Open `http://127.0.0.1:3000/meetings`. Successful WorkOS authentication is not enough by itself: Tacit also requires a current local organization membership.

## Bootstrap a local admission

Create a WorkOS organization, add the demo user to it, and obtain the exact WorkOS user, organization, and organization-membership IDs. Then register their local mapping:

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

The namespace argument must exactly equal `TACIT_IDENTITY_NAMESPACE`. The command refuses non-loopback databases and never links accounts by email.

## Verification

1. Sign in through `/auth/sign-in` and confirm `/meetings` renders.
2. Confirm an authenticated user without the bootstrap mapping sees the access-denied page.
3. Sign out and confirm the local session denial is written before the AuthKit cookie is cleared.
4. Send a WorkOS test webhook and confirm the endpoint returns a successful response.
5. Restart `pnpm dev` after every environment-variable change.

## References

- [WorkOS AuthKit Next.js SDK](https://workos.com/docs/sdks/authkit-nextjs)
- [WorkOS sessions](https://workos.com/docs/authkit/sessions)
- [WorkOS webhook setup](https://workos.com/docs/events/data-syncing/webhooks)
