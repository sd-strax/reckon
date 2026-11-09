-- 0029: knowledge citations (design/06 §6.1, design/ui binding §6 gap 22,
-- adr/0109): the fold across investigations of what each reasoning act
-- consulted. One row per consulted entry on an InterpretationRecorded event:
-- a procedure (kind sop, ref = sop lineage id) or a past case (kind case,
-- ref = the summary's investigation ref), at the revision and cut the act had
-- in front of it. Nothing is folded at write time: one act may hold one
-- procedure at two revisions or two cuts, and the record never loses a fact
-- it carried. The reads collapse: "cited in N investigations" is a distinct
-- count over aggregate_id, and the per-investigation list groups by act.
CREATE TABLE IF NOT EXISTS knowledge_citations (
    ref               TEXT NOT NULL,
    kind              TEXT NOT NULL,               -- sop | case
    aggregate_id      UUID NOT NULL,
    tenant_id         UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001',
    interpretation_id UUID NOT NULL,
    sequence_no       BIGINT NOT NULL,
    revision          INT NOT NULL DEFAULT 0,      -- 0 for a case, or a record that named none
    cut               TEXT NOT NULL DEFAULT '',    -- '' for a case, or a record that named none
    cited             BOOLEAN NOT NULL,
    occurred_at       TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (ref, aggregate_id, interpretation_id, revision, cut)
);

CREATE INDEX IF NOT EXISTS knowledge_citations_aggregate_idx ON knowledge_citations (aggregate_id);
