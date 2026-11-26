# Session realtime delivery

The canonical `GET /v1/auth/session` response carries the current roles, authorization decisions, revision, and replay cursor from one repeatable-read transaction on the primary. The browser opens the same-origin `GET /api/v1/events` stream with that cursor in `Last-Event-ID`. The signed cursor binds the reader, workspace, policy revision, and signing generation to a persisted event ID.

The organization feed retains 1,024 changes and the ID at the retention boundary. A writer holds the organization feed head until its canonical mutation, invalidation, and notification commit together. Writers for local membership, organization creation, admission, invitation acceptance, subject and organization blocks, and session denial all use the feed. Invitation grant registration reserves a role for a future acceptance and has no admitted session to invalidate.

Each API replica listens on one dedicated PostgreSQL connection. Notification payloads identify the organization and wake a bounded scan of durable state. An independent poll runs roughly every two seconds, including when the listener has been unavailable at attachment. A lost established listener generation closes affected streams with `RESYNC_REQUIRED`. A replica can replay up to 512 positions at a time in pages of at most 32. Filtered changes advance through checkpoints. Stable snapshots and retained cursor UUIDs are shared by every replica.

Every delivery takes fresh membership and session authority under shared identity and organization locks. Writers use conflicting locks, so a frame flush either precedes revocation or sees the revoked state. Each feed read and frame authorization operation has a 500 ms budget and at most two run concurrently. A socket flush has a 250 ms deadline, and each replica accepts at most 64 active streams. When a stalled connection can still accept a terminal frame, its backlog or retention gap produces `RESYNC_REQUIRED`. A socket already blocked past its write deadline closes, and the next attachment checks the last processed cursor for a gap or oversized backlog. Heartbeat comments arrive every ten seconds while idle.

The browser acknowledges complete frames and retries any pending canonical session refetch before reconnecting. The generated session query drives CASL. Resync clears workspace query data and hides protected UI until the new snapshot arrives. A revoked stream clears workspace data and ends the subscription. Workspace changes remount the scoped query client and abort old requests.

## Deployment and recovery

Apply migration 4 after draining older canonical writers. Start the new writers and readers together so every canonical access change advances the feed. Set the same random `TACIT_REALTIME_CURSOR_KEY` of at least 32 bytes on every production API replica. Changing it invalidates prior cursors. Rotate it on each replica after restoring PostgreSQL to an earlier timeline, before opening traffic.

Set `TACIT_REALTIME_LISTEN_DATABASE_URL` to a direct or session-pooled runtime account when the main database URL uses transaction pooling. A directly connected development database can use the main URL for both. The connection budget reserves four dedicated listener connections at maximum API replicas plus rollout surge, in addition to four direct operational connections.

Forward the exact SSE route incrementally, preserve `Last-Event-ID`, disable buffering and transformation, and set the upstream idle timeout beyond two heartbeat intervals. Keep load balancing free of session affinity. The API refuses attachments once drain starts and closes existing streams before the normal HTTP shutdown deadline.

Public share delivery remains outside this contract until TAC-018 provides canonical share state and revocation.

## Verification

`pnpm test:postgres` exercises the database snapshot/replay boundary, retention and continuity, commit ordering, listener failure, and revocation serialization against a test PostgreSQL database. The Go HTTP tests exercise bounded write deadlines, unavailable attachments, missed wakes, explicit resync, and drain over real HTTP framing on in-memory sockets. The web tests exercise incremental proxy forwarding, refetch retry, cache purge, and workspace switch isolation.

Use [the two-replica ingress fixture](../../deploy/realtime/README.md) to exercise an authenticated connection through the reverse proxy. The different-replica reconnect budget under concurrent commits is p95 at or below five seconds.
