# 0380 — The agent requests each action type through its own tool, and no tool is strict

Issue: #380 · PR: pending · Status: accepted · Supersedes: the strict-mode decisions of [0376](0376-strict-request-action.md)

## Context

The agent proposed every action through one tool, `request_action`. The tool
mirrored the body of `POST /api/actions` (`design/08-write-side-actions.md §2`):
an `action_type`, the targets, the evidence, and a `parameters` object whose
keys depend on the action type (`§3`). That is the right shape for the API and
for the domain record. As the shape the model writes, it is a tagged union
inside the arguments. `action_type` is the real tool name, and `parameters` is
the real input schema, chosen by `action_type`.

Each fix since the catalog was checked against `parameters` has worked around
that union.

- With `parameters` an open object, the model sometimes wrote it as a JSON
  string. adr/0376 records that fault.
- adr/0376 made the tool strict. `parameters` became an `anyOf` of one closed
  shape per action type, with every key required and `null` for a key with no
  value. Nothing tied the shape to `action_type`.
- Strict mode writes an object's keys in `properties` order, and the schema is
  serialized with its keys sorted. So the first key of `parameters` chose the
  shape. For `ticket.create` the model reached for `summary`, which leads no
  shape. The grammar refused it and took the likeliest key it allowed,
  `resolution`, which exists only in `ticket.close`. The call went out as
  `ticket.create` with `{"resolution": null}`, and the engine refused it.
- The model's own text named the right keys. The grammar made the right call
  impossible to write, so the agent sent the same call again and again in one
  turn, and the ticket was never created.

The read side never had this fault. The loop gives the model one tool per
capability verb, and each tool has its own flat schema.

Three more constraints bound the answer.

- reckon has to work with more than one model provider. Strict mode, its
  key-order rule and its limits (at most 20 strict tools, 16 union parameters
  and 24 optional parameters per request) belong to one provider's API. A
  tool definition that relies on them does not carry over.
- Every tool-calling provider lets the model choose a tool by name, and gives
  each tool its own input schema.
- The tool list is the head of every request, and the prompt cache matches it
  byte for byte. It must not move within a session.

## Decision

- **Each requestable action type is its own tool.** The tool name is
  `request_` followed by the action type with each `.` written as `_`, so
  `ticket.create` is `request_ticket_create`. The loop maps the name back to
  the action type. Tool building fails when two names collide, as it already
  does for a verb that takes an intrinsic tool's name.
- **A tool's inputs are its action type's inputs, at the top level.** Each
  non-entity input the descriptor declares is a property of the tool, and the
  inputs the descriptor requires are the tool's required keys. Every tool also
  carries the fields every request shares: `targets`, `rationale`,
  `evidence_refs` (required), and `retry_of` (optional). There is no nested
  `parameters` object for the model to write.
- **The loop builds the same request as before.** Every action tool runs one
  handler. It takes the action type from the tool, gathers the declared input
  keys into `parameters`, and sends the `ActionRequest` to `POST /api/actions`.
  The engine, Gate 2, the aggregate and the API do not change.
- **A tool's description comes from its descriptor.** It holds the
  descriptor's intent, its tier and reversibility, "degraded" when that is the
  type's status, and a short form of the guidance every request shares: the
  request goes through authorization policy, the agent proposes and never
  approves, and it must cite evidence. It also names, in one line, the action
  types with no tool wired, so the agent can tell the analyst that one has to
  be done by hand.
- **Only available and degraded action types get a tool.** A degraded type can
  still dispatch. An unavailable type has nothing to dispatch to, so its
  request is now impossible rather than discouraged by a prompt rule. This
  matches the verb tools, which are trimmed the same way (`design/03 §6.3`).
- **No catalog means no action tools.** The catalog fetch and
  `POST /api/actions` answer 503 under the same condition, the action layer
  being off, so a generic tool could only be refused. Any other error from the
  catalog fetch fails the session start, instead of being read as "no catalog".
- **The tool list keeps a fixed order.** Verbs by name, then the intrinsic
  tools in declared order, then the action tools by action type. The catalog
  is frozen for the session, so the list is byte-stable.
- **No tool is strict.** `ToolDef` loses its strict flag, and the strict-only
  rules go with it: closed objects, every key required, `null` for absent, and
  the counting against the strict limits. A wrong call gets the engine's
  refusal, and the model can write the fix, because nothing in decoding blocks
  it.
- **The engine's check stays.** The engine still refuses a `parameters` that
  is not an object, and the loop still never repairs one (adr/0376).
- **The loop drops a `null` value only for a declared optional input.** A model
  may write `null` for an optional key it has no value for, and that means the
  key is absent. A `null` for a required input reaches the engine, which
  refuses it as missing.
- **One function answers "is this an action tool, and of which type".** The
  loop, the reconciliation of a creation claim, the prompt version and the
  eval's graders all read it, so none of them can disagree on which calls are
  action requests. The turn record shows each call as made, under its own tool
  name.
- **The prompt names the action tools.** The base-prompt rules that named
  `request_action` name the action tools instead. `parameters-are-an-object`
  is retired, because the model no longer writes a `parameters` object.
- **H5 and H6 grade the action tools.** They find action requests through the
  shared function and grade each call against its own tool's inputs. The
  catalogue version moves.
- **A live check, run by hand.** An env-gated test sends a ticket handoff and
  an account disable N times per model, and reports for each call the tool
  chosen, its inputs, and whether the engine would accept them. It needs an
  API key and is never part of `make test` or `make ci`.

## Alternatives rejected

- **A tag inside `parameters`.** Each shape begins with an `action_type` key
  that names it, so writing the tag picks the shape. The union stays in the
  arguments, the fix still depends on one provider's grammar, and it keeps the
  `null` pattern, a budget against the union limit and a non-strict fallback.
- **A tagged union over the whole call.** An `anyOf` at the root of the input
  schema, one branch per action type. It is undocumented for strict tools, the
  shared fields repeated in every branch would pass the union limit.
- **Write the keys in the descriptor's order.** It does not decide the shape:
  `body` would still lead both `ticket.comment` and `notify.email`.
- **Keep strict mode on the action tools.** About fifteen action tools would
  pass the limit of 20 strict tools. Flat schemas do not need it, and it ties
  the tool definitions to one provider.
- **A generic `request_action` beside the action tools, for a missing
  catalog.** With no catalog the engine refuses every request, and the open
  `parameters` object is the fault adr/0376 recorded.
- **State the shared guidance once in the base prompt.** It would keep each
  description short, but it puts the rule away from the call it governs. The
  placement is open to revisit (#381).

## Consequences

- A call cannot carry another action type's inputs. Choosing the wrong action
  is still possible, and it is a choice of tool the analyst sees and H6
  grades, not a malformed call.
- The tool definitions use plain JSON Schema with nothing specific to one
  provider, so a second provider adapter can send them as they are.
- The model sees about fifteen more tools. The shared guidance repeats in each
  description, cached with the tool list.
- An action type that becomes available mid-session gets a tool only in a new
  session, as a verb does.
- The tool definitions change, so the prompt version moves and the per-model
  baselines are run again on every bundled scenario.
- `request_action` stays the name of the request, the API body and the domain
  operation. `design/08-write-side-actions.md §2` says that the model reaches
  it through one tool per action type. `design/10-eval-harness.md §3`,
  `eval/README.md` and `implementation/agent-reliability.md` follow.
- adr/0376's decisions on the strict flag, the required-and-`null` rule, the
  union of shapes and the strict limits no longer hold. Its deletion of the
  repair, and the engine's refusal of a string, stand.
