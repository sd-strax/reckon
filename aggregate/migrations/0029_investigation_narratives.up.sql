-- 0029: the investigation narrative at any time (adr/0103; design/06 §3.2 two
-- tiers; binding gap 18). Revisions-as-rows: the AI draft is never
-- overwritten, an analyst edit supersedes it as a new revision, and the
-- authorship line reads from the lineage ("drafted by reckon · edited by …").
CREATE TABLE IF NOT EXISTS investigation_narratives (
    id               UUID PRIMARY KEY,
    tenant_id        UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001',
    investigation_id UUID NOT NULL,
    revision         INT  NOT NULL,
    body             TEXT NOT NULL,
    generator_model  TEXT NOT NULL DEFAULT '',
    as_of_sequence   BIGINT NOT NULL DEFAULT 0,  -- the record's last event sequence the text describes
    author_kind      TEXT NOT NULL,              -- AI_DELEGATED | HUMAN
    author_id        TEXT NOT NULL,
    author_name      TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMP WITH TIME ZONE NOT NULL,
    UNIQUE (investigation_id, revision)
);
CREATE INDEX IF NOT EXISTS investigation_narratives_investigation_idx ON investigation_narratives (investigation_id, revision DESC);
