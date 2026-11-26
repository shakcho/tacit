# Durable work

TAC-009 makes PostgreSQL the sole owner of asynchronous intent and accepted
results. NATS JetStream is a replayable wake-up channel, not a second job
database.

## Ownership and state

One enqueue transaction records the versioned command outcome, job, and outbox
event. A matching command retry returns the recorded job; the same key with a
different fingerprint is a typed conflict. Jobs expose their current state,
attempt count, next availability, deadline, terminal reason, and causation.

Claims increment both attempt and monotonic fence. Renew, completion, retry,
cancellation, and terminal failure match organization, job, attempt, fence,
worker, state, and the live lease. Once another worker replaces an expired
lease, the old worker cannot attach its output or advance state.

Outbox publishing has its own bounded lease. A crash before publish leaves a
pending record. A crash after publish but before acknowledgement lets another
dispatcher publish the same event ID. JetStream message de-duplication reduces
duplicates, while consumer inbox receipts make duplicates safe. Requeueing
published rows reconstructs broker delivery without altering canonical jobs.

Provider and consumer inbox helpers compare payload identity on replay. Owner
modules call the transaction-scoped helpers in the same PostgreSQL transaction
as their product effect; receipt-only consumers may use the public operation.

## Design choice

Two designs were considered:

1. PostgreSQL ledger with a transactional outbox and broker wake-ups.
2. Broker-owned job state where acknowledgements and redelivery define truth.

The first design was selected because intent, fences, tenant scope, accepted
results, and replay remain queryable after broker loss. The broker-owned design
would force domain operations to coordinate a database transaction with a
queue acknowledgement and would let replay change apparent job truth.

This remains a deliberately small job state machine, not a workflow language.
Domain-specific orchestration belongs to its owner module. If work grows into
long multi-step workflows with compensation and branching, revisit a workflow
engine rather than expanding this ledger indefinitely.

## Operations

- Deadline dispatch moves overdue work to terminal failure and a dead letter.
- Operator replay records who replayed the dead letter, preserves the monotonic
  fence, grants one additional attempt, and clears the expired deadline.
- Workload and per-organization concurrency limits are read during claim under
  a workload advisory lock, so multiple dispatchers cannot over-claim.
- Outbox publishing uses bounded batches, leases, retry backoff, and a terminal
  publish state. Reconciliation can requeue published events by durable event
  ID after a broker rebuild.

The versioned JSON contracts live in [`protocols/runtime/v1`](../../protocols/runtime/v1).
