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

Strict mode has limits that bound the answer.

- Every object in a strict schema lists its keys and sets
  `additionalProperties: false`. An object with no listed keys can only be
  `{}`.
- A request may carry at most 24 optional parameters across all its strict
  tools. Every parameter not in `required` counts.
- Strict and non-strict tools can be sent in the same request.
- The guarantee does not hold when the response is cut off by `max_tokens` or
  is a refusal.

## Decision

- **`request_action` is sent with `strict: true`.** Its three object schemas
  set `additionalProperties: false`: the tool input, each target, and
  `parameters`. The model can no longer sample a `parameters` that opens with
  a quote, a key outside the catalog's union, or a field of the wrong type.
  `ToolDef` carries the flag, so it is part of the tool definition that the
  prompt version hashes (adr/0232).
- **No other tool is strict.** Every verb tool takes a free-form `entity`
  object, which strict mode would reduce to `{}`. The other intrinsic tools
  have not shown the fault, and each optional field they have would spend the
  shared budget of 24.
- **The schema is checked against the budget when the tools are built.**
  `request_action` has two optional top-level fields, and every key in the
  `parameters` union is optional. The built-in catalog gives 14 in all. A
  catalog that would take the count past 24 gets a non-strict
  `request_action`, because the API would otherwise refuse every request of
  the session. A test pins that the built-in catalog fits.
- **`UnwrapStringifiedObject` is deleted.** The loop passes `parameters` to
  the engine as the model sent it. A string is refused by the engine's own
  validation before anything is recorded, and the model reads the error and
  sends the call again. This is the path for a non-strict `request_action`
  and for a cut-off or refused response.
- **H5 and H6 grade the value as sent.** Neither unwraps. A `parameters` that
  is a string fails H6 whether or not the string is valid JSON. H5 still looks
  only at accepted requests, and an accepted request can no longer carry a
  string. The catalogue version moves.
- **The prompt rule stays.** `parameters-are-an-object` keeps its text. Its
  ledger entry names the strict schema and the engine's validation as what
  enforces it, in place of the loop's repair (adr/0057).

## Alternatives rejected

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
- **One schema per action type, joined with `anyOf`.** It would also enforce
  each type's required keys at sampling time. It changes the tool's shape,
  and the engine already enforces required keys. The fault here is the
  encoding of the field, not its keys.
- **A switch to turn strict mode off for a gateway that refuses the field.**
  No such gateway is known. A switch can be added when one is.

## Consequences

- A malformed `parameters` cannot be produced when strict mode is in force,
  and cannot reach approval on any path.
- A cleanly double-encoded `parameters` used to be accepted silently. Where
  strict mode is not in force it is now a refused attempt and costs the model
  one more call.
- The tool definition changes, so the prompt version moves and the per-model
  baselines are run again on every bundled scenario.
- The API compiles a strict schema on first use and caches it. The first
  request of a new catalog shape is slower.
- A catalog that grows past the budget loses strict mode on `request_action`
  without an error. The fit test catches this for the built-in catalog. An
  install that adds its own action types can still cross the limit.
- `design/10-eval-harness.md §3`, `eval/README.md` and
  `implementation/agent-reliability.md` describe the repair and are updated
  with this change.
