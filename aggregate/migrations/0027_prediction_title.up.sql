-- A prediction carries a short title, recorded with it (adr/0090): authored by
-- whoever records the prediction, or derived once by the aggregate at record
-- time (title_derived), never at render time.
ALTER TABLE prediction_current
    ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS title_derived BOOLEAN NOT NULL DEFAULT FALSE;
