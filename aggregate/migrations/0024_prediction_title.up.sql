-- 0024: the prediction short title (adr/0090). title is the name of record
-- the tracker's row, the matrix and the timeline show; title_derived says
-- the aggregate derived it from the statement at record time. NULL on a row
-- projected before this migration; a replay (or the read) derives it.
ALTER TABLE prediction_current ADD COLUMN title TEXT;
ALTER TABLE prediction_current ADD COLUMN title_derived BOOLEAN NOT NULL DEFAULT false;
