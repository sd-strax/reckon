-- 0024: the act that requested each action (#153), so the thread's request
-- line can name the action it asked for. Backfilled from the request events.
ALTER TABLE action_current ADD COLUMN requesting_interpretation_id UUID;
UPDATE action_current ac
   SET requesting_interpretation_id = (e.payload->>'requesting_interpretation_id')::uuid
  FROM events e
 WHERE e.event_type = 'action.requested'
   AND e.payload->>'action_id' = ac.action_id::text
   AND e.payload->>'requesting_interpretation_id' IS NOT NULL;
