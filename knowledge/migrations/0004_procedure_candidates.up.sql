-- 0004: procedure candidates (adr/0112; binding gap 24). A candidate procedure
-- the agent drafted at an investigation's conclusion, held for review by
-- whoever manages knowledge. A CRUD record in reckon_knowledge, deliberately
-- NOT a substrate corpus entry: an unreviewed draft must never be recallable,
-- and every corpus the substrate holds is. Accepting imports the (edited) body
-- through the procedure import path with a reckon://investigation/<id> source
-- URL and records the resulting procedure here.
CREATE TABLE IF NOT EXISTS procedure_candidates (
    id                UUID PRIMARY KEY,
    tenant_id         UUID NOT NULL,
    investigation_id  UUID NOT NULL,
    title             TEXT NOT NULL,
    body              TEXT NOT NULL,
    tags              TEXT[] NOT NULL DEFAULT '{}',
    recommendation    TEXT NOT NULL DEFAULT '',
    rationale         TEXT NOT NULL DEFAULT '',
    generator_model   TEXT NOT NULL DEFAULT '',
    drafted_at        TIMESTAMP WITH TIME ZONE NOT NULL,
    drafted_by        TEXT NOT NULL,
    drafted_by_name   TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL,            -- NEW | ACCEPTED | DISCARDED
    decided_by        TEXT,
    decided_by_name   TEXT,
    decided_at        TIMESTAMP WITH TIME ZONE,
    discard_reason    TEXT,
    accepted_sop_id   UUID
);
CREATE INDEX IF NOT EXISTS procedure_candidates_status_idx ON procedure_candidates (tenant_id, status, drafted_at DESC);
CREATE INDEX IF NOT EXISTS procedure_candidates_investigation_idx ON procedure_candidates (investigation_id);
