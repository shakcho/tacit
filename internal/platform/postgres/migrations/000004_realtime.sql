CREATE TABLE tacit_realtime_heads (
    organization_id uuid PRIMARY KEY REFERENCES tacit_organizations (id) ON DELETE CASCADE,
    head bigint NOT NULL DEFAULT 0,
    floor bigint NOT NULL DEFAULT 0,
    floor_event_id uuid NOT NULL DEFAULT gen_random_uuid(),
    CHECK (floor >= 0 AND head >= floor AND head - floor <= 1024)
);

CREATE TABLE tacit_realtime_changes (
    organization_id uuid NOT NULL REFERENCES tacit_realtime_heads (organization_id) ON DELETE CASCADE,
    position bigint NOT NULL CHECK (position > 0),
    event_id uuid NOT NULL DEFAULT gen_random_uuid(),
    kind text NOT NULL CHECK (kind IN ('invalidate', 'scope_reset')),
    resource_type text NOT NULL CHECK (resource_type = 'authentication_session'),
    resource_id uuid,
    audience_user_id uuid,
    resource_revision bigint NOT NULL CHECK (resource_revision = position),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, position),
    UNIQUE (organization_id, event_id),
    CHECK (resource_id IS NOT DISTINCT FROM audience_user_id)
);

INSERT INTO tacit_realtime_heads (organization_id)
SELECT id FROM tacit_organizations;

CREATE FUNCTION tacit_initialize_realtime_head() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO tacit_realtime_heads (organization_id) VALUES (NEW.id);
    RETURN NEW;
END;
$$;

CREATE TRIGGER tacit_initialize_realtime_head
    AFTER INSERT ON tacit_organizations
    FOR EACH ROW EXECUTE FUNCTION tacit_initialize_realtime_head();
