-- 0028: owner action items (adr/0101; binding gap 17): work only an owner
-- outside reckon can do, tracked with the investigation to the owner's
-- confirmation. Its own record, CRUD + thin history, modeled on the comms
-- threads: it executes nothing, carries no blast radius and never dispatches,
-- so it is not an x-action. notify_action_id is the notify.* action that
-- asked for it; the item moves to SENT when that action succeeds.
CREATE TABLE IF NOT EXISTS owner_items (
    id                UUID PRIMARY KEY,
    tenant_id         UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001',
    aggregate_id      UUID NOT NULL,
    title             TEXT NOT NULL,
    owner             TEXT NOT NULL,
    detail            TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL,            -- OPEN | SENT | CONFIRMED | WAIVED
    notify_action_id  UUID,
    created_by        TEXT NOT NULL,
    created_by_name   TEXT NOT NULL DEFAULT '',
    created_by_kind   TEXT NOT NULL DEFAULT 'HUMAN',  -- HUMAN | AI_DELEGATED (a proposal)
    created_at        TIMESTAMP WITH TIME ZONE NOT NULL,
    sent_at           TIMESTAMP WITH TIME ZONE,
    confirmed_by      TEXT,
    confirmed_by_name TEXT,
    confirmed_at      TIMESTAMP WITH TIME ZONE,
    confirmation_note TEXT,
    updated_at        TIMESTAMP WITH TIME ZONE NOT NULL,
    history           JSONB NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS owner_items_aggregate_idx ON owner_items (aggregate_id);
CREATE INDEX IF NOT EXISTS owner_items_notify_idx ON owner_items (notify_action_id) WHERE notify_action_id IS NOT NULL;
