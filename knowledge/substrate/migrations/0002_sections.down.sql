DELETE FROM substrate_embeddings WHERE section <> 0;
ALTER TABLE substrate_embeddings DROP CONSTRAINT substrate_embeddings_pkey;
ALTER TABLE substrate_embeddings ADD PRIMARY KEY (entry_id, model);
ALTER TABLE substrate_embeddings DROP COLUMN section;
DROP TABLE substrate_sections;
