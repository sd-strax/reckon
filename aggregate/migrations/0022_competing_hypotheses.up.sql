-- 0022: competing hypotheses (01 §x-hypothesis "Competing hypotheses").
-- handle: the short mnemonic the tabs/composer/thread/export name a hypothesis
-- by. discriminates: the rival hypothesis ids whose expectation for a
-- prediction is the opposite of its owner's — the ACH matrix as data.
ALTER TABLE hypothesis_current ADD COLUMN handle TEXT NOT NULL DEFAULT '';
ALTER TABLE prediction_current ADD COLUMN discriminates JSONB;
