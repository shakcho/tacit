# Managed platform

Tacit starts in one Kubernetes cluster with nodes in several availability zones. PostgreSQL stays outside the cluster and owns identity, product state, and durable work. Immutable objects belong in external object storage. A broker can wake consumers once enabled. PostgreSQL owns the intent and accepted result.

## Workload ownership

The [platform registry](../../deploy/charts/tacit/files/platform.v1.json) defines workloads, identities, ports, resources, lifecycle, database use, and network edges. The [bindings schema](../../deploy/charts/tacit/values.schema.json) restricts inputs to digest pins, existing Secret names, specific endpoint addresses, DNS names, and database allocations. The deployment tool validates both files and reports disabled capabilities.

| Role | Execution and authority |
| --- | --- |
| Web | Next.js forwards authenticated HTTP and SSE to API. Web reaches WorkOS and has zero SQL connections. |
| API | Go verifies WorkOS tokens and signed webhooks. API uses the transaction pool and one direct LISTEN connection per process. |
| Migration | A revision-named Job uses one direct PostgreSQL session and a separate DDL login. The same artifact can verify the schema ledger read-only. |
| Dispatch, worker-ingress, connectors, capture, media, speech, LLM, delivery | Disabled contracts. The registry allocates zero provider permits and GPU capacity. |

The foundation Helm release owns one namespace per runnable role. Each namespace enforces restricted Pod Security and has a dedicated ServiceAccount, resource quotas, and default deny in both directions. The policies permit DNS and the catalog edges. Each internal edge grants both the sender and the receiver. WorkOS, the SQL pooler, LISTEN, and DDL bind separate endpoints. Workload Pods run without Kubernetes API credentials.

The application Helm release owns two Deployments, Services, HPAs, PDBs, and Ingresses. CPU HPA owns replica counts. The scheduler prefers an even distribution across available zones. PDBs cover voluntary disruption. Web readiness requires managed WorkOS configuration and a ready API. API readiness checks the transaction pool within one second. LISTEN outages leave polling and replay available. The API drains SSE and HTTP on SIGTERM, then closes the listener and pool. Node runs as PID 1 in the web image and handles its native Next.js shutdown.

The web ingress uses fixed paths. Web routes SSE through `/api/v1/events` on the same origin as the product. Traefik flushes streaming responses and uses no sticky sessions here. The API ingress exposes the exact `/webhooks/workos` path on a separate TLS hostname. Kubernetes probes use internal traffic. Production web and API omit process-local sample operations.

## Capacity during rollout

The quotas bound CPU, memory, ephemeral storage, and pods per namespace. The `count/pods` limit counts every surviving Pod object, including an object held past deletion grace. The compute quota `pods` releases the charge after deletion grace expires. The matching limits can stall rollout until an old object is gone. Force-deleting a Pod whose node can still run the process can orphan a database client. Database-side limits remain mandatory. [Kubernetes quota evaluator](https://github.com/kubernetes/kubernetes/blob/v1.34.1/pkg/quota/v1/evaluator/core/pods.go#L339-L368)

The example admits up to four API Pod objects. Three HPA replicas plus one surge slot use at most 16 pooled clients and four direct listeners, including the old and new process mix. Migration uses one slot. The example adds three operation slots, 20 provider-reserved slots, and 15 backends for the API's tuple, for a total of 43 PostgreSQL backends. Staging must measure actual connection demand and provider settings.

Supavisor allocates backends per `(pooler, database, login, mode)`. Count each distinct tuple and add all clients per pooler. Declare live allocations from other clusters, credential generations, database users, backups, and restores beyond the shared operational allowance. The validator refuses budgets that meet or exceed either provider ceiling. The API checks the transaction URL against the declared endpoint, database, and login. [Supavisor pool behavior](https://supabase.com/kb/troubleshooting/supavisor-faq-YyP5tI)

## Work and recovery progression

Before enabling workers, fix claim, renewal, completion, and outbox checks to use authoritative PostgreSQL time after the transition lock. Add a fenced relinquish operation with an explicit attempt policy. PostgreSQL must reject missing workload and tenant limits. Match already accepted results idempotently and reject stale, unaccepted completions.

Acquire provider operation permits in a stable order in the transaction that claims work. Retain an ambiguous permit until the provider confirms completion, a verified remote lifetime expires, or the provider rejects the operation. GPU admission combines allocatable devices, namespace quota, and the measured session limit for the exact device and model. The registry keeps these groups at zero until the enforcement and replacement drills pass.

Prove zone loss and managed recovery in one cluster before considering active/standby. A second cluster needs independent writer identities and a shared PostgreSQL owner epoch checked in every canonical write transaction. Independent database timelines also require revocation at the old primary and each external authority. Measure RPO and RTO against the five-minute and four-hour targets in ADR 0002. Record evidence in the [platform acceptance ledger](../runbooks/platform-acceptance.md).
