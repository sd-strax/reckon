-- 0026: capability calls as addressable records, and event membership in an
-- investigation (adr/0092; design/ui/00-backend-binding.md §6 items 13 and 20).
--
-- An event was persisted without any record of the call that retrieved it or
-- the investigation that call was made for, so a surface could show only the
-- events a prediction happened to cite. The call is the unit of membership:
-- one row per invocation, every event it returned joined to it, and the
-- investigation on the call. A call made outside an investigation (the
-- workbench's own reads) has a null investigation_id: it keeps its id, so an id
-- in an envelope always resolves, but it is listed under no investigation.
--
-- observed_data_events is the derived-from edge (03 §4), persisted: which
-- telemetry record an observed-data was normalized from. It is what lets the
-- entity filter reach events through the entities their observed-data name.
--
-- Events persisted before this migration have no call and no membership; they
-- keep appearing through citations only.

CREATE TABLE IF NOT EXISTS capability_calls (
    id                   UUID PRIMARY KEY,
    tenant_id            UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001',
    investigation_id     UUID,
    verb                 TEXT NOT NULL,
    adapter              TEXT NOT NULL DEFAULT '',
    operation            TEXT NOT NULL DEFAULT '',
    priority             INT NOT NULL DEFAULT 0,
    params_sent          JSONB,
    -- Every binding the resolver walked, in priority order: adapter, operation,
    -- priority, the parameters as rendered, whether it answered or why it was
    -- skipped. adapter/operation/priority/params_sent above summarize the first
    -- that answered; a fan-out call is read from here.
    bindings             JSONB NOT NULL DEFAULT '[]',
    coverage             TEXT NOT NULL,
    bindings_answered    INT NOT NULL DEFAULT 0,
    bindings_total       INT NOT NULL DEFAULT 0,
    null_observation_ref TEXT,
    degradation_notes    JSONB NOT NULL DEFAULT '[]',
    started_at           TIMESTAMP WITH TIME ZONE NOT NULL,
    duration_ms          INT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS capability_calls_investigation_idx
    ON capability_calls (tenant_id, investigation_id, started_at);

CREATE TABLE IF NOT EXISTS call_events (
    call_id       UUID NOT NULL REFERENCES capability_calls (id),
    ocsf_event_id UUID NOT NULL REFERENCES ocsf_events (id),
    PRIMARY KEY (call_id, ocsf_event_id)
);

CREATE INDEX IF NOT EXISTS call_events_event_idx ON call_events (ocsf_event_id);

CREATE TABLE IF NOT EXISTS observed_data_events (
    observed_data_id UUID NOT NULL,
    ocsf_event_id    UUID NOT NULL REFERENCES ocsf_events (id),
    PRIMARY KEY (observed_data_id, ocsf_event_id)
);

CREATE INDEX IF NOT EXISTS observed_data_events_event_idx ON observed_data_events (ocsf_event_id);

-- The engine-minted call behind a turn's tool dispatch. call_id on the
-- interpretation event stays the model's tool_use id; this column is the
-- capability layer's, so the two never collide.
ALTER TABLE ai_tool_calls ADD COLUMN IF NOT EXISTS capability_call_id UUID;
