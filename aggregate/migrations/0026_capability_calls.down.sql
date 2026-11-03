ALTER TABLE ai_tool_calls DROP CONSTRAINT IF EXISTS ai_tool_calls_capability_call_fk;
ALTER TABLE ai_tool_calls DROP COLUMN IF EXISTS capability_call_id;
DROP TABLE IF EXISTS observed_data_events;
DROP TABLE IF EXISTS call_events;
DROP TABLE IF EXISTS capability_calls;
