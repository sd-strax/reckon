# 0376 — `request_action` is a strict tool, and the loop no longer repairs its parameters

Issue: #376 · PR: pending · Status: accepted

## Context

`request_action.parameters` is a JSON object. The tool schema, the tool
description and a prompt rule all say so. Nothing enforced it. A tool's input
is text the model samples, and the Messages API holds that text to
`input_schema` only when the tool sets `strict: true`. No tool did.

On the long `ticket.create` handoff the model sometimes wrote `parameters` as
a string. The string held the whole object and then one stray closing brace,
because the object's own closing braces had been written inside the string.
The loop repaired this: `UnwrapStringifiedObject` decoded the string, kept the
first complete object and dropped the rest. So the action that reached
approval was correct, and H5 passed. H6 failed, because the model had broken
the parameter contract (`design/10-eval-harness.md §3`).

Two things were wrong with that arrangement.

- H5 passed because of the repair. The H5 grader called the same function, so
  it graded what the loop had fixed, not what the model had sent.
- A repair guesses. Keeping the first object and dropping what follows is
  right for a stray brace. It is not a rule the engine should apply to an
  action a person is about to approve.

The engine already refuses a `parameters` that is not an object
(`action.validateParameters`, `design/08-write-side-actions.md §3`). The
repair was the only reason a string got past it.

Strict mode has rules that bound the answer.

- Every object in a strict schema lists its keys and sets
  `additionalProperties: false`. An object with no listed keys can only be
  `{}`.
- Keys are written in the schema's order, required keys first. An optional
  key the model passes over cannot be written later.
- A request may carry at most 24 optional parameters and at most 16
  parameters that use `anyOf` or a list of types, counted across all its
  strict tools.
- Strict and non-strict tools can be sent in the same request.
- The guarantee does not hold when the response is cut off by `max_tokens` or
  is a refusal.

The second rule decided the shape. `parameters` was one object holding the
union of every action type's keys, all optional, because which keys are
required depends on the action type. Sent strict as it stood, the model wrote
the short ticket fields first and came to the long description last. The
grammar no longer allowed that key, so the description was written inside the
string of the last field the model had opened. That request has declared keys
and a summary, so the engine would accept it. An optional key in a strict
schema is a way to lose a value into another field.

## Decision

- **`request_action` is sent with `strict: true`.** `ToolDef` carries the
  flag, so it is part of the tool definition that the prompt version hashes
  (adr/0232).
- **A strict tool has no optional key.** Every object in its schema is closed,
  and every key is required. A key that may have no value is typed as its type
  or `null`, and the model writes `null` for it. The tool builder refuses a
  strict tool that breaks this rule. For `request_action`, `parameters` and
  `retry_of` are required, and `retry_of` may be `null`.
- **`parameters` is one closed object per action type**, joined with `anyOf`.
  Each shape lists that type's non-entity inputs
  (`design/08-write-side-actions.md §3`). An input the descriptor requires is
  a plain type. An input it does not require may be `null`. Action types with
  no such inputs share one empty shape. The schema does not tie the shape to
  `action_type`; the engine's validation still does.
- **The loop drops a key whose value is `null`** before the request goes to
  the engine. `null` is how the schema spells an absent key, so the engine
  sees the same parameters as before.
- **No other tool is strict.** Every verb tool takes a free-form `entity`
  object, which strict mode would reduce to `{}`. The other intrinsic tools
  have not shown the fault.
- **The schema is checked against the limits when the tools are built.** The
  built-in catalog uses 12 of the 16 union parameters: ten inputs that may be
  `null`, `retry_of`, and `parameters` itself. A catalog that would pass a
  limit gets the same schema without the strict flag, because the API would
  otherwise refuse every request of the session. A test pins that the
  built-in catalog fits.
- **With no action catalog, `request_action` is not strict.** The loop then
  knows no parameter keys, and a closed `parameters` object would allow none.
  `parameters` stays an open object, and the engine's validation is the only
  check.
- **`UnwrapStringifiedObject` is deleted.** The loop no longer turns a string
  into an object. A string `parameters` is refused by the engine's own
  validation before anything is recorded, and the model reads the error and
  sends the call again. This is the path for a non-strict `request_action`
  and for a cut-off or refused response.
- **H5 and H6 grade the value as sent.** Neither unwraps. A `parameters` that
  is a string fails H6 whether or not the string is valid JSON. A key sent as
  `null` counts as absent. H5 still looks only at accepted requests, and an
  accepted request can no longer carry a string. The catalogue version moves.
- **The prompt rule stays.** `parameters-are-an-object` keeps its text. Its
  ledger entry names the strict schema and the engine's validation as what
  enforces it, in place of the loop's repair (adr/0057).

## Alternatives rejected

- **Send the union of optional keys as a strict schema.** A key passed over
  cannot be written later, and its value lands in another field that the
  engine accepts. This was tried and is described above.
- **One shape per action type, with optional keys left optional.** The
  required keys arrive, but an optional key is still lost the same way.
- **Keep the repair and add strict mode beside it.** H5 would still pass
  through the repair on any path where strict mode is not in force, so the
  grader could not tell enforcement from repair.
- **Keep unwrapping a string that is valid JSON, and drop only the repair of
  an invalid one.** It is still the loop rewriting the model's call. It also
  keeps two encodings of one field alive in the graders.
- **Make every tool strict.** The verb tools' `entity` object has no fixed
  keys, so it would need a new shape first. That is a change to the read
  surface with no fault behind it.
- **Reject a string in the loop, before the engine.** The engine already
  refuses it with a message that names the fault. A second check in the loop
  would be a copy that can drift.
- **A switch to turn strict mode off for a gateway that refuses the field.**
  No such gateway is known. A switch can be added when one is.

## Consequences

- A malformed `parameters` cannot be produced when strict mode is in force,
  and cannot reach approval on any path.
- The model is held to each action type's keys and required inputs when it
  writes the call, not only when the engine checks it.
- The model writes `null` for every input it has no value for. The recorded
  tool call shows those keys; the request the engine stores does not.
- A cleanly double-encoded `parameters` used to be accepted silently. Where
  strict mode is not in force it is now a refused attempt and costs the model
  one more call.
- The tool definition changes, so the prompt version moves and the per-model
  baselines are run again on every bundled scenario.
- The API compiles a strict schema on first use and caches it. The first
  request of a new catalog shape is slower.
- A catalog that grows past a limit loses strict mode on `request_action`
  without an error, only a logged warning. The action catalog is built in, so
  this can happen only when the catalog changes in code, and the fit test
  catches it there.
- The schema is serialized with its keys sorted, so the model writes a
  shape's keys in alphabetical order, not the descriptor's. Every key is
  required, so the order loses nothing.
- A later strict tool follows the same rule: closed objects, every key
  required, `null` for absent.
- `design/10-eval-harness.md §3`, `eval/README.md` and
  `implementation/agent-reliability.md` describe the repair and are updated
  with this change.
