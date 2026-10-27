-- 0026: the retrieved-excerpt side store (design/06 §6.2 Layer B; adr/0097).
-- knowledge_retrievals holds the bytes the model saw, keyed by the excerpt's
-- content hash so repeated retrievals of one revision share a row; the
-- revision hash names the procedure content the excerpt came from and
-- location says where in it. knowledge_retrieval_uses records each
-- interpretation that consulted the excerpt, with the retrieval's score and
-- band at the time. Both are written in the interpretation's transaction.
CREATE TABLE IF NOT EXISTS knowledge_retrievals (
    tenant_id     UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001',
    excerpt_hash  TEXT NOT NULL,
    sop_id        UUID NOT NULL,
    revision_hash TEXT NOT NULL,
    excerpt       TEXT NOT NULL,
    location      JSONB,
    first_seen_at TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (tenant_id, excerpt_hash)
);
CREATE INDEX IF NOT EXISTS knowledge_retrievals_sop_idx ON knowledge_retrievals (tenant_id, sop_id);

CREATE TABLE IF NOT EXISTS knowledge_retrieval_uses (
    tenant_id         UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001',
    investigation_id  UUID NOT NULL,
    interpretation_id UUID NOT NULL,
    excerpt_hash      TEXT NOT NULL,
    score             DOUBLE PRECISION,
    band              TEXT,
    retrieved_at      TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (tenant_id, investigation_id, interpretation_id, excerpt_hash)
);
CREATE INDEX IF NOT EXISTS knowledge_retrieval_uses_investigation_idx ON knowledge_retrieval_uses (tenant_id, investigation_id);
