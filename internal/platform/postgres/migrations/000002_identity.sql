CREATE OR REPLACE FUNCTION tacit_reject_identity_mapping_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'identity mappings are immutable';
END;
$$;

CREATE TRIGGER tacit_identity_subjects_immutable
BEFORE UPDATE OR DELETE ON tacit_identity_subjects
FOR EACH ROW EXECUTE FUNCTION tacit_reject_identity_mapping_update();

CREATE TABLE tacit_identity_organizations (
    namespace text NOT NULL CHECK (length(namespace) BETWEEN 1 AND 64),
    external_organization_id text NOT NULL CHECK (length(external_organization_id) BETWEEN 1 AND 512),
    organization_id uuid NOT NULL REFERENCES tacit_organizations(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (namespace, external_organization_id),
    UNIQUE (namespace, organization_id)
);

CREATE TRIGGER tacit_identity_organizations_immutable
BEFORE UPDATE OR DELETE ON tacit_identity_organizations
FOR EACH ROW EXECUTE FUNCTION tacit_reject_identity_mapping_update();

CREATE TABLE tacit_identity_membership_admissions (
    namespace text NOT NULL CHECK (length(namespace) BETWEEN 1 AND 64),
    external_organization_id text NOT NULL CHECK (length(external_organization_id) BETWEEN 1 AND 512),
    subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 512),
    external_membership_id text NOT NULL CHECK (length(external_membership_id) BETWEEN 1 AND 512),
    state text NOT NULL CHECK (state IN ('active', 'denied')),
    source_updated_at timestamptz NOT NULL,
    source_event_id text NOT NULL CHECK (length(source_event_id) BETWEEN 1 AND 512),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (namespace, external_organization_id, subject),
    UNIQUE (namespace, external_membership_id)
);

CREATE INDEX tacit_identity_membership_admissions_subject_idx
    ON tacit_identity_membership_admissions (namespace, subject, external_organization_id);

CREATE TABLE tacit_identity_session_denials (
    namespace text NOT NULL CHECK (length(namespace) BETWEEN 1 AND 64),
    session_id text NOT NULL CHECK (length(session_id) BETWEEN 1 AND 512),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (namespace, session_id)
);

CREATE TABLE tacit_identity_subject_states (
    namespace text NOT NULL CHECK (length(namespace) BETWEEN 1 AND 64),
    subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 512),
    blocked boolean NOT NULL DEFAULT false,
    not_before timestamptz,
    source_event_id text NOT NULL CHECK (length(source_event_id) BETWEEN 1 AND 512),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (namespace, subject)
);

CREATE TABLE tacit_identity_organization_states (
    namespace text NOT NULL CHECK (length(namespace) BETWEEN 1 AND 64),
    external_organization_id text NOT NULL CHECK (length(external_organization_id) BETWEEN 1 AND 512),
    blocked boolean NOT NULL DEFAULT false,
    source_event_id text NOT NULL CHECK (length(source_event_id) BETWEEN 1 AND 512),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (namespace, external_organization_id)
);

CREATE TABLE tacit_identity_invitation_grants (
    namespace text NOT NULL CHECK (length(namespace) BETWEEN 1 AND 64),
    external_invitation_id text NOT NULL CHECK (length(external_invitation_id) BETWEEN 1 AND 512),
    organization_id uuid NOT NULL REFERENCES tacit_organizations(id),
    role text NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'accepted')),
    accepted_subject text CHECK (accepted_subject IS NULL OR length(accepted_subject) BETWEEN 1 AND 512),
    accepted_user_id uuid REFERENCES tacit_users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    accepted_at timestamptz,
    PRIMARY KEY (namespace, external_invitation_id),
    CHECK (
        (state = 'pending' AND accepted_subject IS NULL AND accepted_user_id IS NULL AND accepted_at IS NULL)
        OR
        (state = 'accepted' AND accepted_subject IS NOT NULL AND accepted_user_id IS NOT NULL AND accepted_at IS NOT NULL)
    )
);

CREATE INDEX tacit_identity_invitation_grants_organization_idx
    ON tacit_identity_invitation_grants (organization_id, state, created_at);

CREATE TABLE tacit_identity_event_inbox (
    namespace text NOT NULL CHECK (length(namespace) BETWEEN 1 AND 64),
    event_id text NOT NULL CHECK (length(event_id) BETWEEN 1 AND 512),
    event_kind smallint NOT NULL CHECK (event_kind BETWEEN 0 AND 6),
    payload_hash bytea NOT NULL CHECK (octet_length(payload_hash) = 32),
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    outcome text NOT NULL CHECK (outcome IN ('applied', 'ignored')),
    PRIMARY KEY (namespace, event_id)
);

CREATE INDEX tacit_identity_event_inbox_received_idx
    ON tacit_identity_event_inbox (received_at, namespace, event_id);
