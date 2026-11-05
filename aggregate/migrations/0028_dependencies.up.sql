-- 0028_dependencies: the work an investigation's conclusion depends on that
-- only someone outside reckon can do (binding §6 gap 17, adr/0101). OWNED BY
-- THE dependency PACKAGE; it lives in this migration set because
-- aggregate/migrations is the reckon_main schema set. CRUD + thin history like
-- comms_threads (only the investigation aggregate is event-sourced): a
-- dependency is not an x-action (nothing executes) and not a message (the
-- channel owns the conversation); it keeps the ask, its status, who resolved
-- it and why, and a pointer out.
CREATE TABLE IF NOT EXISTS dependencies (
    dependency_id    UUID PRIMARY KEY,
    aggregate_id     UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    title            TEXT NOT NULL,            -- what has to be done
    owner            TEXT NOT NULL,            -- who outside reckon owes it
    detail           TEXT NOT NULL DEFAULT '', -- why
    status           TEXT NOT NULL,            -- OPEN | SENT | CONFIRMED | WAIVED
    notify_action_id UUID,                     -- the notify.* action the ask rides on
    external_ref     TEXT NOT NULL DEFAULT '', -- where the work lives outside (ticket, permalink)
    created_by       TEXT NOT NULL,
    created_by_name  TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMP WITH TIME ZONE NOT NULL,
    resolved_by      TEXT,
    resolved_by_name TEXT,
    resolved_at      TIMESTAMP WITH TIME ZONE,
    resolution_note  TEXT,
    updated_at       TIMESTAMP WITH TIME ZONE NOT NULL,
    history          JSONB NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS dependencies_aggregate_idx ON dependencies (aggregate_id);
CREATE INDEX IF NOT EXISTS dependencies_notify_action_idx ON dependencies (notify_action_id) WHERE notify_action_id IS NOT NULL;
