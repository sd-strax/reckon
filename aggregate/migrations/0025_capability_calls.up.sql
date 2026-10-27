-- 0025: capability calls as first-class records (adr/0092; binding gaps 13
-- and 20). A call is one invocation of a verb for an investigation; every
-- event it returned joins it (call_events), and every observed-data joins
-- the event it derives from (observed_data_events: the normalizer's
-- derived-from edge, persisted). Membership of an event in an investigation
-- is recorded here at ingest, never inferred later.
CREATE TABLE IF NOT EXISTS capability_calls (
    id                   UUID PRIMARY KEY,
    tenant_id            UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001',
    aggregate_id         UUID,                   -- NULL for an ad-hoc call outside any investigation
    verb                 TEXT NOT NULL,
    binding              TEXT,                   -- the adapter that answered (the first, under fan-out)
    adapter              TEXT,
    operation            TEXT,
    priority             INT,
    params_sent          JSONB,                  -- exactly as templated
    coverage             TEXT NOT NULL,
    bindings_answered    INT NOT NULL DEFAULT 0,
    bindings_total       INT NOT NULL DEFAULT 0,
    null_observation_ref TEXT,
    started_at           TIMESTAMP WITH TIME ZONE NOT NULL,
    duration_ms          INT NOT NULL DEFAULT 0,
    degradation_notes    JSONB,
    attempts             JSONB                   -- every binding considered, with its outcome
);

CREATE INDEX IF NOT EXISTS capability_calls_aggregate_idx ON capability_calls (aggregate_id, started_at);

CREATE TABLE IF NOT EXISTS call_events (
    call_id       UUID NOT NULL,
    ocsf_event_id UUID NOT NULL,
    PRIMARY KEY (call_id, ocsf_event_id)
);
CREATE INDEX IF NOT EXISTS call_events_event_idx ON call_events (ocsf_event_id);

CREATE TABLE IF NOT EXISTS observed_data_events (
    observed_data_id UUID NOT NULL,
    ocsf_event_id    UUID NOT NULL,
    PRIMARY KEY (observed_data_id, ocsf_event_id)
);
CREATE INDEX IF NOT EXISTS observed_data_events_event_idx ON observed_data_events (ocsf_event_id);

-- A committed turn's tool-call row names the capability call it made, so a
-- turn resolves to its calls and a call back to the turn.
ALTER TABLE ai_tool_calls ADD COLUMN capability_call_id UUID;
