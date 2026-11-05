-- Sections (00-substrate §2.3): a body cut on its headings at write time, one
-- row per section, so RANK recall can rank and return the piece that matched
-- rather than the document's opening. The cut is a view over the hashed body
-- (start_rune/end_rune locate it); the content hash still covers the whole
-- entry. splitter_version and budget_runes record the rule that produced the
-- rows, so Reindex can tell stale rows from current ones.
CREATE TABLE substrate_sections (
    entry_id         UUID NOT NULL REFERENCES substrate_entries(id) ON DELETE CASCADE,
    idx              INT  NOT NULL CHECK (idx >= 1),
    heading_path     TEXT[] NOT NULL DEFAULT '{}',
    part             INT  NOT NULL DEFAULT 0,
    parts            INT  NOT NULL DEFAULT 0,
    start_rune       INT  NOT NULL CHECK (start_rune >= 0),
    end_rune         INT  NOT NULL CHECK (end_rune >= start_rune),
    text             TEXT NOT NULL,
    splitter_version INT  NOT NULL,
    budget_runes     INT  NOT NULL,

    -- Keyword backend (§10): the degraded ranker ranks sections too. The
    -- heading path and the entry's title join the surface at query time
    -- (array_to_string is not immutable, so it cannot sit in the column).
    fts tsvector GENERATED ALWAYS AS (to_tsvector('english', text)) STORED,

    PRIMARY KEY (entry_id, idx)
);

CREATE INDEX substrate_sections_fts ON substrate_sections USING GIN (fts);

-- One vector per (entry, model, section). Section 0 is the whole document
-- (title + body) as before and serves SIMILARITY mode; sections 1..n are the
-- rows above and serve RANK mode.
ALTER TABLE substrate_embeddings ADD COLUMN section INT NOT NULL DEFAULT 0 CHECK (section >= 0);
ALTER TABLE substrate_embeddings DROP CONSTRAINT substrate_embeddings_pkey;
ALTER TABLE substrate_embeddings ADD PRIMARY KEY (entry_id, model, section);
