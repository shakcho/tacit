# System boundary

Tacit begins as a modular Go control plane with separately scalable workloads, not independently persisted microservices.

```text
Next.js web -> Go API/realtime -> PostgreSQL
Provider ingress -----------> durable inbox/outbox/job ledger -> NATS JetStream
                                                        |-> connector/capture workers
                                                        |-> media workers
                                                        |-> speech executor -> Python/CUDA model service
                                                        |-> delivery/LLM workers
All large immutable artifacts --------------------------------> private object storage
```

PostgreSQL owns product truth, work intent, attempts and accepted outcomes. NATS wakes workers and supports replay; it is not a second product database. Executors receive immutable inputs and a fenced lease, then submit results through the Go control boundary.

The web application may use CASL for presentation, but Go authorization is authoritative for every read, mutation, subscription, job result, search result and media grant.

See `boundaries.json` for the machine-enforced import rules and `ownership.md` for review ownership.
