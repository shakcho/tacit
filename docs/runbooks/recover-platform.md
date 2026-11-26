# Recover a failed Tacit environment

ADR 0002 targets a recovery point of at most five minutes and a recovery time of at most four hours. Use the outage start, last recoverable write, and end-to-end recovery timestamps to measure them. Archive the result outside the restored project.

## Recover within one cluster

1. Record the outage start, surviving zones, PostgreSQL timeline, last committed durable IDs, and capture watermark.
2. If one zone failed, route traffic to the surviving ingress endpoints and check ready applications and the database. Leave the provider primary in place while it is healthy.
3. If PostgreSQL recovery is required, pause traffic and writer operations. Restore to an isolated project using provider point-in-time recovery. Compare the schema ledger, durable event IDs, accepted work, and object inventory against the external recovery record.
4. If a database timeline moved backward, rotate the shared SSE cursor key before admitting readers. Confirm stale cursors require a new authorized session snapshot.
5. Reconcile missing IDs, ambiguous provider operations, lost capture intervals, and object references before reopening traffic. Record the actual RPO and RTO even if a target was missed.

## Fence an old primary before a standby promotion

Standby promotion remains disabled until the acceptance ledger records a passing fencing drill. Perform these steps first on an isolated environment.

1. Acquire the incident-controlled promotion lock outside either application cluster. Assign one named operator sole promotion authority. Record the candidate database timeline and intended owner epoch.
2. Withdraw old ingress and provider webhook routing. Pause scheduled jobs, publishers, and workers. Revoke the old deploy and database-operation credentials independently of the old cluster. Kill old processes where reachable and block their network paths at external controls.
3. At PostgreSQL, block old-cluster access and revoke old application, direct listener, and migration logins. Terminate their existing server and pooler sessions and verify reconnection fails. If restoring to an independent timeline, make the old primary unavailable to writers at the provider before enabling the restored writer.
4. At the broker, revoke the old publish and control identities, then disconnect existing sessions. At object storage, block the old writer and invalidate any effective write grants. Revoke old provider credentials and reconcile in-flight calls. Hold ambiguous operations until the provider confirms closure or expiry.
5. Confirm that the old identities fail each authoritative write path, including inbox and webhook writes, worker results, admin changes, outbox transitions, object writes, and schema operations. Test old-node return and traffic on existing connections.
6. Advance the shared owner epoch at the surviving PostgreSQL authority. Start the candidate with the new identities. Check the epoch in every canonical write transaction and replay PostgreSQL-owned work intent. Rotate the SSE cursor key after a rewind.
7. Reconcile database and object watermarks, provider effects, incomplete captures, and work leases. Verify current identity and SSE admission before routing traffic to the candidate. Record missing IDs, measured data loss, and the time at which writes resume.
