# Platform acceptance ledger

Record each run against the source revision, environment, image digests, configuration digest, date, and operator. Attach the measured result and a log or query location with credentials removed. CI render checks and a disposable cluster exercise are distinct evidence.

| Proof | Current evidence | Required acceptance run |
| --- | --- | --- |
| `PLAT-REGISTRY` | Node validation and deployment command tests | Run the Helm CI job and archive the phase manifests and chart lint results. |
| `PLAT-ROLLOUT` | Runtime and command tests | Deploy real web, API, and migration images on a disposable policy-enabled cluster. Exercise healthy ingress, authorized SSE reconnect without affinity, unsigned webhook rejection, a signed staging webhook, PDB eviction, failure before and after migration, and compatible rollback. Confirm AuthKit bypass on internal probes. |
| `PLAT-QUOTA` | Registry projection and manifest assertions | With three API replicas and one surge object, hold an old Pod in deletion past its grace period with an isolated test finalizer. Observe the next Pod denied by `count/pods` and inspect pool and direct session peaks. Remove the finalizer and confirm rollout resumes. |
| `PLAT-POLICY` | Declared allow and deny manifests | Use the real CNI. Prove web reaches WorkOS and API, API reaches JWKS and both SQL endpoints, and migration reaches direct DDL. Confirm an untrusted Pod cannot reach role ports, undeclared SQL endpoints, or the Kubernetes API. Pair each denial with a reachable control using the same endpoint and a permitted source. |
| `PLAT-TELEMETRY` | Probes and structured process logs | Deliver real web and API telemetry to queryable disposable sinks. Confirm collector queue bounds, dashboard and alert rule load, and absence of secret canaries. |
| `PLAT-WORKER` | Pending, contracts disabled | Prove fenced claim, renew, complete, relinquish, shutdown, outbox replay, and stale process recovery on real workers. Record capture gaps and accepted attempts. |
| `PLAT-PROVIDER` | Pending, zero permits | Prove concurrency limits during cancellation, timeout, and ambiguous remote completion. Record the authoritative close, TTL, or rejection used to release each permit. |
| `PLAT-GPU` | Pending, zero capacity | Replace an actual device-backed Pod at the measured model semaphore limit. Compare device capacity, namespace quota, and active model sessions. |
| `PLAT-PITR` | Pending managed recovery. The repository has a separate logical restore check. | Restore an isolated Supabase project to a known timestamp. Check migration checksums, durable watermarks, missing IDs, and object references. Record measured RPO and RTO. |
| `PLAT-BROKER-OBJECT` | Pending | Rebuild broker notifications from PostgreSQL intent. Recover immutable objects and reconcile references, missing objects, and pending remote effects. |
| `PLAT-ZONE` | Pending | Remove one real availability zone under load. Check web and API admission, SSE replay, surviving pod quotas, database reachability, request errors, and elapsed recovery. |
| `PLAT-STANDBY` | Pending, independent writer activation disabled | Run the external fencing procedure and test old-node return with the old credentials and open connections. Record RPO and RTO and reconcile accepted writes and ambiguous operations. |

The [recovery procedure](recover-platform.md) defines the evidence required before standby promotion.
