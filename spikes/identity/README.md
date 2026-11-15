# Identity conformance probe

TAC-006 compares the WorkOS AuthKit and Clerk session profiles before TAC-007 adds production authentication. The probe verifies locally signed fixture tokens against the same observable cases for both vendors.

Run the fixture suite from the repository root:

```text
go test ./spikes/identity/identityprobe
```

The suite reads `identityprobe/cases.json`. Every case that requires `fixture` evidence must have one implementation, and both vendor profiles run that implementation.

## What the fixtures prove

The fixtures prove the following behavior with controlled RSA keys and deterministic time:

- exact issuer and application-party checks;
- RS256 signature checks with a configured key ID;
- WorkOS `client_id` and `org_id` handling;
- Clerk version 2 `azp` and `o.id` handling;
- expiry, not-before, issued-at, and maximum-lifetime checks;
- session and subject denial behavior;
- organization mismatch rejection; and
- rejection of forged tokens, duplicate security claims, unsupported token shapes, and unknown keys.

The verifier uses only Go's standard library so that the spike has no production dependency. TAC-007 must use a maintained JOSE library, a bounded JWKS cache, and the selected vendor's real keys. Do not promote `identityprobe` into the API.

## What remains open

Fixture tokens do not prove hosted sign-in, callback handling, refresh behavior, vendor logout, event delivery, or organization switching. No WorkOS or Clerk sandbox credentials are stored in this repository, so the live rows remain not run.

Run the same sequence in each sandbox before TAC-007 merges:

1. Sign in through the hosted Next.js flow and send its access token to an isolated Go verifier.
2. Confirm that Go resolves the expected subject, session, and active organization.
3. Switch from organization A to organization B. Confirm that the new token is bound to B and cannot satisfy a request for A.
4. Sign out. Replay the captured access token and measure when local denial and provider refresh rejection take effect.
5. Revoke another session through the vendor administration API or dashboard. Measure event delivery and the expiry fallback.
6. Exercise a new-user invitation and an existing-user invitation. Confirm that matching email alone never links a local account.
7. Record the environment, token profile, SDK version, case ID, outcome, and timestamp. Never record a token, key, email, or external subject.

Use `passed`, `failed`, or `not_run` for every sandbox case. Missing credentials are `not_run`; fixture success cannot replace sandbox evidence.

## Production handoff

TAC-007 implements one protected-request operation:

```go
Authenticate(ctx, bearerToken, requestedLocalOrganization) (LocalPrincipal, error)
```

The operation completes signature and claim checks, local revocation checks, external-to-local organization mapping, and a fresh local membership read. It returns local IDs only. Product handlers never receive vendor claims or roles.

Public share routes do not use this operation. Their revocable share capability remains a separate access boundary.
