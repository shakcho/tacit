# Deploy the Tacit platform

## Bind an environment

1. Use a Kubernetes 1.32 or newer cluster with enforceable NetworkPolicy, ResourceQuota, Pod Security Admission, metrics-server, and schedulable nodes in several zones.
2. Install the `traefik` IngressClass. Run controller Pods in the `traefik` namespace with `app.kubernetes.io/name=traefik`. Use the `websecure` entry point and set `providers.kubernetesIngress.strictPrefixMatching=true`. Set the load balancer timeouts to allow streaming and verify SSE on the public hostname. The chart expects `cluster.local` and CoreDNS Pods labeled `k8s-app=kube-dns` in `kube-system`. [Traefik Kubernetes Ingress settings](https://doc.traefik.io/traefik/reference/install-configuration/providers/kubernetes/kubernetes-ingress/)
3. Build the `web`, `api`, and `db` Dockerfiles from the same reviewed source revision. Publish them to a registry the nodes can pull from. Record the three registry digests. Container CI checks the three artifacts and generates their SBOMs.
4. Copy [example.json](../../deploy/platform/example.json) to an environment bindings file. Replace the environment name, source revision, image digests, TLS hostnames, Secret names, and egress addresses. Resolve the actual WorkOS, SQL pooler, and direct database addresses. Keep egress changes and DNS changes coordinated.
5. Confirm the provider reserve, project size, per-tuple backend limits, and aggregate client limits at the database and pooler. Include other clients sharing the project and overlapping credential generations. Bind the API transaction endpoint on port 6543 and the direct endpoints on port 5432.
6. Use a release runner restricted to this environment. The deployment command creates the `tacit-<environment>-system` namespace for Helm release records and the deployment lock. Give workloads no access to that namespace or its credentials.

Use Node, Helm, and kubectl from the trusted toolchain. Keep the bindings file free of credential values.

```bash
tacit_environment=staging
tacit_context=tacit-staging-us
tacit_bindings=/path/to/staging.json

node scripts/platform.mjs validate --file "$tacit_bindings"
node scripts/platform.mjs render --file "$tacit_bindings"
node scripts/platform.mjs deploy --foundation-only --file "$tacit_bindings" \
  --environment "$tacit_environment" --context "$tacit_context"
```

Populate the Secrets through the environment's secret manager after the foundation creates the role namespaces. The workload registry defines the keys.

| Namespace suffix | Secret data |
| --- | --- |
| `web` | WorkOS client ID, issuer, and cookie password. Put the web certificate and key in the named web TLS Secret. Public-client sign-in does not require an API key; add one only when a reviewed management feature introduces its own scoped binding. |
| `api` | Dedicated transaction URL, separate direct LISTEN URL, shared cursor key of at least 32 bytes, WorkOS namespace, client ID, issuer, JWKS URL, and webhook secret. Put the webhook certificate and key in the named API TLS Secret. |
| `migration` | One dedicated direct DDL connection URL for the application database. |

Use distinct database roles for the application, LISTEN, and DDL. Limit the direct LISTEN role to the allocated listener capacity. Verify PostgreSQL's TLS certificate on each URL. Grant application permissions to the transaction login and reserve schema changes for the migration role. Point the public DNS records at the ingress load balancer. Register the web `/auth/callback` and the separate `/webhooks/workos` endpoint in WorkOS.

## Deploy and roll back

Run deploy for the first application installation and each compatible application release.

```bash
node scripts/platform.mjs deploy --file "$tacit_bindings" \
  --environment "$tacit_environment" --context "$tacit_context"
```

The command acquires the environment lock and confirms the foundation and initialized quotas. It validates Secret keys, checks the live schema on upgrades, waits for migration, and updates the application atomically. It waits for the Deployments to meet their availability floors. It checks the web route, the unsigned webhook rejection, and denial of public probe and API paths.

Archive the JSON receipt and image digests. The receipt identifies the environment, context, revision, schema, and manifest hashes. If an ingress smoke fails on an upgrade, the command restores the previous application and repeats the checks. A failed first-release smoke removes the application release and retains the foundation. Failed schema Jobs stop the release and remain available until their TTL expires.

To revert the application to the most recent successful prior revision, use the active environment bindings.

```bash
node scripts/platform.mjs rollback --file "$tacit_bindings" \
  --environment "$tacit_environment" --context "$tacit_context"
```

Rollback checks the previous manifests against the installed foundation and verifies schema 4 and the ledger checksums using the active DB artifact. It rolls back only the application and checks rollout and ingress again. A foundation or schema change requires planned maintenance. Fence all database writers before migrating an old installation outside the current release history.

## Recover an interrupted release

The deployment command creates a ConfigMap lock named `tacit-platform-lock`. The lock has no expiry. Every phase checks ownership before a mutation.

1. Check the deployment runner and its Kubernetes audit identity. Stop the process and its children. Revoke its write credential if the runner could resume or reach the API server.
2. Review the Helm histories, Deployment revisions, and any migration Job. Preserve Job diagnostics without copying credentials.
3. Clear the lock only after the old runner is fenced. If a schema Job failed, confirm its process has stopped before deleting the Job. Retry using the same bindings.
4. If rollout is blocked by the object quota, inspect the older Pod and its node. Remove a stale terminal object only after its process is confirmed dead or fenced at PostgreSQL. Recheck the quota and pod readiness.
5. If Helm records a pending release, inspect the partial state while the environment is serialized. Recover the affected release before retrying. Preserve the foundation while repairing the application.
