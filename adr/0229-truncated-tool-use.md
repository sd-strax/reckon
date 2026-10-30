# 0229 — A tool call whose input did not arrive whole enters history as `{}` and is never run

Issue: #229 · PR: #266 · Status: accepted

## Context

A streamed completion builds each tool call's input by concatenating its
`input_json_delta` fragments (`readSSEMessage`, `agent/anthropic.go`). When the
stream stops on `max_tokens` partway through a call, that input is unfinished
JSON. `Session.Turn` appended the response to the session's message history
as received, then answered the call with a synthetic error result. The history
is reused for every later model call, and the unfinished `json.RawMessage`
fails `json.Marshal`, so every subsequent turn failed until `ResetContext`.
The existing max_tokens test used a well-formed input and a scripted model
that never serialized the request, so it could not see this.

The invariant at stake belongs to the session, not to one provider: the
history must always be serializable, whichever provider or path (streaming or
not) produced the response.

## Decision

- Before an assistant response enters history, the session checks every
  tool_use input. One that is not a JSON object is replaced by `{}` and the
  call is marked cut off (`sealToolInputs`, `agent/session.go`). An empty
  input is a call with no arguments, not a cut-off.
- A cut-off call is never dispatched, whatever the stop reason. It is answered
  with an `is_error` tool result telling the model its input was cut off at the
  output limit and to issue the call again with a shorter input. Well-formed
  calls in the same response are dispatched as usual, and all results answer
  the assistant message in one user message.
- The committed transcript records the attempt:
  `[tool_use <name> id=<id>] input cut off after N bytes; not run`, followed by
  its error result.
- Tests cover the cut-off at the session level (history stays serializable,
  the call is not dispatched, the record says so, the next turn succeeds) and
  end to end through the real Anthropic adapter against a stream cut off
  mid-`input_json_delta`, asserting the next request is valid JSON.

## Alternatives rejected

- **Repair the input by closing its brackets:** it invents arguments the model
  did not finish; a `request_action` cut off mid-target-list would run against
  half its targets.
- **Validate only in `readSSEMessage`:** it guards one provider on one path,
  while the invariant is the session's.
- **Drop the cut-off block from the response:** the assistant message can end
  up empty, which the API rejects, and the model never learns why its call did
  not happen.

## Consequences

A max_tokens stop mid-tool_use costs the call, not the session. The model sees
why the call did not run and can reissue it. A provider that returns a
malformed input for any other reason is handled the same way.
