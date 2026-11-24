CREATE TABLE tacit_runtime_command_outcomes (
    organization_id uuid NOT NULL REFERENCES tacit_organizations(id),
    operation_scope text NOT NULL CHECK (length(operation_scope) BETWEEN 1 AND 128),
    client_key text NOT NULL CHECK (length(client_key) BETWEEN 1 AND 128),
    fingerprint_version smallint NOT NULL CHECK (fingerprint_version > 0),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    outcome_version smallint NOT NULL CHECK (outcome_version > 0),
    outcome jsonb NOT NULL CHECK (jsonb_typeof(outcome) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, operation_scope, client_key)
);

CREATE TABLE tacit_runtime_jobs (
    organization_id uuid NOT NULL REFERENCES tacit_organizations(id),
    id uuid NOT NULL,
    kind text NOT NULL CHECK (length(kind) BETWEEN 1 AND 128),
    workload text NOT NULL CHECK (length(workload) BETWEEN 1 AND 64),
    state text NOT NULL CHECK (state IN (
        'queued', 'retry_wait', 'running', 'succeeded', 'failed', 'canceled'
    )),
    event_revision bigint NOT NULL DEFAULT 1 CHECK (event_revision > 0),
    input_version smallint NOT NULL CHECK (input_version > 0),
    input jsonb NOT NULL CHECK (jsonb_typeof(input) = 'object'),
    resource_type text NOT NULL CHECK (length(resource_type) BETWEEN 1 AND 64),
    resource_id text NOT NULL CHECK (length(resource_id) BETWEEN 1 AND 256),
    resource_revision bigint NOT NULL CHECK (resource_revision > 0),
    correlation_id text NOT NULL CHECK (length(correlation_id) BETWEEN 1 AND 128),
    causation_id text CHECK (causation_id IS NULL OR length(causation_id) BETWEEN 1 AND 128),
    priority smallint NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL,
    deadline_at timestamptz,
    max_attempts smallint NOT NULL CHECK (max_attempts BETWEEN 1 AND 100),
    attempt_count smallint NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND max_attempts),
    fence bigint NOT NULL DEFAULT 0 CHECK (fence >= 0),
    lease_owner text,
    lease_expires_at timestamptz,
    output_version smallint,
    output jsonb,
    terminal_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    PRIMARY KEY (organization_id, id),
    UNIQUE (id),
    CHECK ((lease_owner IS NULL) = (lease_expires_at IS NULL)),
    CHECK ((state = 'running') = (lease_owner IS NOT NULL)),
    CHECK ((output_version IS NULL) = (output IS NULL)),
    CHECK (output IS NULL OR jsonb_typeof(output) = 'object'),
    CHECK ((state IN ('succeeded', 'failed', 'canceled')) = (completed_at IS NOT NULL)),
    CHECK (state <> 'succeeded' OR output IS NOT NULL),
    CHECK (deadline_at IS NULL OR deadline_at > available_at)
);

CREATE INDEX tacit_runtime_jobs_claim_idx
    ON tacit_runtime_jobs (workload, available_at, priority DESC, created_at, id)
    WHERE state IN ('queued', 'retry_wait', 'running');

CREATE INDEX tacit_runtime_jobs_tenant_state_idx
    ON tacit_runtime_jobs (organization_id, workload, state, updated_at, id);

CREATE INDEX tacit_runtime_jobs_deadline_idx
    ON tacit_runtime_jobs (deadline_at, id)
    WHERE deadline_at IS NOT NULL AND state NOT IN ('succeeded', 'failed', 'canceled');

CREATE TABLE tacit_runtime_job_attempts (
    organization_id uuid NOT NULL,
    job_id uuid NOT NULL,
    attempt smallint NOT NULL CHECK (attempt > 0),
    fence bigint NOT NULL CHECK (fence > 0),
    worker_id text NOT NULL CHECK (length(worker_id) BETWEEN 1 AND 128),
    state text NOT NULL CHECK (state IN (
        'running', 'succeeded', 'retry', 'failed', 'canceled', 'expired'
    )),
    started_at timestamptz NOT NULL,
    lease_expires_at timestamptz NOT NULL,
    finished_at timestamptz,
    reason text,
    retry_at timestamptz,
    output_version smallint,
    output jsonb,
    PRIMARY KEY (organization_id, job_id, attempt),
    UNIQUE (organization_id, job_id, fence),
    FOREIGN KEY (organization_id, job_id)
        REFERENCES tacit_runtime_jobs (organization_id, id)
        ON DELETE CASCADE,
    CHECK ((state = 'running') = (finished_at IS NULL)),
    CHECK ((state = 'retry') = (retry_at IS NOT NULL)),
    CHECK ((output_version IS NULL) = (output IS NULL)),
    CHECK (output IS NULL OR jsonb_typeof(output) = 'object')
);

CREATE INDEX tacit_runtime_job_attempts_worker_idx
    ON tacit_runtime_job_attempts (worker_id, state, lease_expires_at);

CREATE TABLE tacit_runtime_outbox (
    event_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES tacit_organizations(id),
    subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 192),
    schema_name text NOT NULL CHECK (length(schema_name) BETWEEN 1 AND 128),
    schema_version smallint NOT NULL CHECK (schema_version > 0),
    aggregate_type text NOT NULL CHECK (length(aggregate_type) BETWEEN 1 AND 64),
    aggregate_id text NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 256),
    aggregate_revision bigint NOT NULL CHECK (aggregate_revision > 0),
    causation_id text CHECK (causation_id IS NULL OR length(causation_id) BETWEEN 1 AND 128),
    correlation_id text NOT NULL CHECK (length(correlation_id) BETWEEN 1 AND 128),
    occurred_at timestamptz NOT NULL,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'publishing', 'published', 'dead')),
    publish_attempts smallint NOT NULL DEFAULT 0 CHECK (publish_attempts >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    publisher_id text,
    publish_lease_expires_at timestamptz,
    published_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((publisher_id IS NULL) = (publish_lease_expires_at IS NULL)),
    CHECK ((state = 'publishing') = (publisher_id IS NOT NULL)),
    CHECK ((state = 'published') = (published_at IS NOT NULL))
);

CREATE INDEX tacit_runtime_outbox_publish_idx
    ON tacit_runtime_outbox (available_at, occurred_at, event_id)
    WHERE state IN ('pending', 'publishing');

CREATE INDEX tacit_runtime_outbox_aggregate_idx
    ON tacit_runtime_outbox (organization_id, aggregate_type, aggregate_id, aggregate_revision);

CREATE TABLE tacit_runtime_consumer_inbox (
    consumer text NOT NULL CHECK (length(consumer) BETWEEN 1 AND 128),
    event_id uuid NOT NULL,
    payload_hash bytea NOT NULL CHECK (octet_length(payload_hash) = 32),
    outcome_version smallint NOT NULL CHECK (outcome_version > 0),
    outcome jsonb NOT NULL CHECK (jsonb_typeof(outcome) = 'object'),
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer, event_id)
);

CREATE INDEX tacit_runtime_consumer_inbox_received_idx
    ON tacit_runtime_consumer_inbox (received_at, consumer, event_id);

CREATE TABLE tacit_runtime_provider_inbox (
    provider text NOT NULL CHECK (length(provider) BETWEEN 1 AND 64),
    account_id text NOT NULL CHECK (length(account_id) BETWEEN 1 AND 256),
    external_event_id text NOT NULL CHECK (length(external_event_id) BETWEEN 1 AND 512),
    payload_hash bytea NOT NULL CHECK (octet_length(payload_hash) = 32),
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    outcome_version smallint NOT NULL CHECK (outcome_version > 0),
    outcome jsonb NOT NULL CHECK (jsonb_typeof(outcome) = 'object'),
    PRIMARY KEY (provider, account_id, external_event_id)
);

CREATE INDEX tacit_runtime_provider_inbox_received_idx
    ON tacit_runtime_provider_inbox (received_at, provider, account_id);

CREATE TABLE tacit_runtime_dead_letters (
    organization_id uuid NOT NULL,
    id uuid NOT NULL,
    job_id uuid NOT NULL,
    attempt smallint NOT NULL CHECK (attempt >= 0),
    reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),
    replay_count integer NOT NULL DEFAULT 0 CHECK (replay_count >= 0),
    failed_at timestamptz NOT NULL,
    replayed_at timestamptz,
    replayed_by text,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, job_id)
        REFERENCES tacit_runtime_jobs (organization_id, id)
);

CREATE INDEX tacit_runtime_dead_letters_job_idx
    ON tacit_runtime_dead_letters (organization_id, job_id, failed_at DESC);

CREATE TABLE tacit_runtime_concurrency_limits (
    organization_id uuid REFERENCES tacit_organizations(id),
    workload text NOT NULL CHECK (length(workload) BETWEEN 1 AND 64),
    maximum_running integer NOT NULL CHECK (maximum_running > 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX tacit_runtime_concurrency_limits_global_idx
    ON tacit_runtime_concurrency_limits (workload)
    WHERE organization_id IS NULL;

CREATE UNIQUE INDEX tacit_runtime_concurrency_limits_tenant_idx
    ON tacit_runtime_concurrency_limits (organization_id, workload)
    WHERE organization_id IS NOT NULL;
