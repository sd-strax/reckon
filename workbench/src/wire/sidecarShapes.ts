// The sidecar's results and notifications, mirroring the Go wire structs in
// internal/sidecar (adr/0244): a field is required where its tag has no
// omitempty.
import { anything, bool, list, num, object, optional, str } from "./decode";

export const initialize = object({
  protocol_version: num,
  backend_api_version: num,
  server_version: str,
});

export const createSession = object({ session_id: str, tools: list(str) });

export const turn = object({
  text: str,
  interpretation_id: optional(str),
  tool_rounds: num,
  usage: object({ input: num, output: num, cache_read: num, cache_write: num }),
  pending_actions: optional(list(object({
    action_id: str,
    action_type: str,
    tier: str,
    status: str,
    targets: optional(list(str)),
    expired: optional(bool),
  }))),
  stopped: optional(bool),
  error: optional(str),
});

export const proposeSOP = object({
  warranted: bool,
  title: str,
  body: str,
  tags: list(str),
  recommendation: str,
  rationale: str,
});

export const text = object({ session_id: str, text: str });

export const step = object({ session_id: str, round: num });

export const toolCall = object({ session_id: str, name: str, input: anything });

export const toolResult = object({
  session_id: str,
  name: str,
  content: str,
  is_error: bool,
  coverage: optional(str),
  event_count: optional(num),
  refs: optional(list(str)),
});
