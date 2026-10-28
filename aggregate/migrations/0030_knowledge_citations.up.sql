-- 0030: knowledge citations (adr/0109; binding gap 22). A projection folded
-- from every interpretation.recorded event's consulted_sops and
-- consulted_similar_investigations: THAT a piece of knowledge was consulted in
-- an investigation, by which reasoning act, and whether the act applied it.
-- The citing act is an investigation event, so the fold runs in the command's
-- transaction (the Projector contract) and Reset + replay rebuilds it. The
-- retrieval side store (0026) keeps WHAT text was read; this keeps the count.
CREATE TABLE IF NOT EXISTS knowledge_citations (
    tenant_id         UUID NOT NULL,
    kind              TEXT NOT NULL,            -- sop | case
    knowledge_ref     TEXT NOT NULL,            -- the SOP lineage id, or the cited investigation's grouping-- ref
    aggregate_id      UUID NOT NULL,            -- the citing investigation
    interpretation_id UUID NOT NULL,            -- the citing act
    used              BOOLEAN NOT NULL DEFAULT FALSE,
    recorded_at       TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (kind, knowledge_ref, interpretation_id)
);
CREATE INDEX IF NOT EXISTS knowledge_citations_ref_idx ON knowledge_citations (tenant_id, kind, knowledge_ref, aggregate_id);
CREATE INDEX IF NOT EXISTS knowledge_citations_aggregate_idx ON knowledge_citations (aggregate_id);
