-- 0025: when a requested action was last edited before approval (adr/0127),
-- so a surface can say the request was changed after it was made.
ALTER TABLE action_current ADD COLUMN edited_at TIMESTAMPTZ;
