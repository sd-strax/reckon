-- 0030: the investigation's narrative (design/06 §3.2 as amended, design/ui
-- binding §6 gap 18, adr/0103): one row per revision, folded from
-- investigation.narrative_recorded in the append transaction. The actor
-- columns are the event's actor: a draft is AI_DELEGATED under the person
-- who asked, with the model; an edit is HUMAN. Nothing is overwritten; the
-- current narrative is the highest revision.
CREATE TABLE IF NOT EXISTS investigation_narratives (
    aggregate_id   UUID NOT NULL,
    revision       INT NOT NULL,
    sequence_no    BIGINT NOT NULL,
    body           TEXT NOT NULL,
    actor_kind     TEXT NOT NULL,            -- HUMAN | AI_DELEGATED
    principal_id   TEXT NOT NULL DEFAULT '',
    principal_name TEXT NOT NULL DEFAULT '',
    model          TEXT NOT NULL DEFAULT '', -- the drafting model, '' for a person's revision
    occurred_at    TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (aggregate_id, revision)
);
