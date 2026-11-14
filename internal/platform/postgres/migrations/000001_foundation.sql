CREATE TABLE tacit_users (
    id uuid PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tacit_organizations (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tacit_organization_memberships (
    organization_id uuid NOT NULL REFERENCES tacit_organizations(id),
    user_id uuid NOT NULL REFERENCES tacit_users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, user_id)
);

CREATE INDEX tacit_organization_memberships_user_idx
    ON tacit_organization_memberships (user_id, organization_id);

CREATE TABLE tacit_membership_roles (
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
    PRIMARY KEY (organization_id, user_id, role),
    FOREIGN KEY (organization_id, user_id)
        REFERENCES tacit_organization_memberships (organization_id, user_id)
        ON DELETE CASCADE
);

CREATE TABLE tacit_identity_subjects (
    provider text NOT NULL CHECK (length(provider) BETWEEN 1 AND 64),
    subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 512),
    user_id uuid NOT NULL REFERENCES tacit_users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject)
);

CREATE INDEX tacit_identity_subjects_user_idx
    ON tacit_identity_subjects (user_id, provider);

CREATE TABLE tacit_membership_external_ids (
    organization_id uuid NOT NULL,
    namespace text NOT NULL CHECK (length(namespace) BETWEEN 1 AND 128),
    external_id text NOT NULL CHECK (length(external_id) BETWEEN 1 AND 512),
    user_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, namespace, external_id),
    FOREIGN KEY (organization_id, user_id)
        REFERENCES tacit_organization_memberships (organization_id, user_id)
        ON DELETE CASCADE
);

CREATE INDEX tacit_membership_external_ids_member_idx
    ON tacit_membership_external_ids (organization_id, user_id);

CREATE TABLE tacit_access_audit (
    organization_id uuid NOT NULL REFERENCES tacit_organizations(id),
    id uuid NOT NULL,
    actor_user_id uuid NOT NULL REFERENCES tacit_users(id),
    target_user_id uuid NOT NULL REFERENCES tacit_users(id),
    action text NOT NULL CHECK (length(action) BETWEEN 1 AND 128),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, id)
);

CREATE INDEX tacit_access_audit_order_idx
    ON tacit_access_audit (organization_id, occurred_at, id);

CREATE TABLE tacit_idempotency_records (
    organization_id uuid NOT NULL REFERENCES tacit_organizations(id),
    operation_scope text NOT NULL CHECK (length(operation_scope) BETWEEN 1 AND 128),
    client_key text NOT NULL CHECK (length(client_key) BETWEEN 1 AND 128),
    fingerprint_version smallint NOT NULL CHECK (fingerprint_version > 0),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    result_version smallint NOT NULL CHECK (result_version > 0),
    result_code smallint NOT NULL CHECK (result_code > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, operation_scope, client_key)
);
