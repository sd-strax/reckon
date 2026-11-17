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

- **Each action type in the catalog is its own tool.** The tool name is
  `request_` followed by the action type with each `.` written as `_`, so
  `ticket.create` is `request_ticket_create`. Each tool is built with its
  action type, and the handler takes the type from the tool, never from its
  name. Tool building fails when two names collide, as it already
  does for a verb that takes an intrinsic tool's name.
- **A tool's inputs are its action type's inputs, at the top level.** Each
  non-entity input the descriptor declares is a property of the tool, and the
  inputs the descriptor requires are the tool's required keys. Every tool also
  carries the fields every request shares: `targets`, `rationale`,
  `evidence_refs` (required), and `retry_of` (optional). There is no nested
  `parameters` object for the model to write.
- **The loop builds the same request as before.** Every action tool runs one
  handler. It takes the action type from the tool, puts every key that is not
  a shared field into `parameters`, and sends the `ActionRequest` to
  `POST /api/actions`. An undeclared key reaches the engine, which refuses it
  and names it. Gate 2, the aggregate and the request body do not change. The
  engine gains one check, below.
- **A tool's description comes from its descriptor.** It holds the
  descriptor's intent, its tier and reversibility, "degraded" when that is the
  type's status, and a short form of the guidance every request shares: the
  request goes through authorization policy, the agent proposes and never
  approves, and it must cite evidence. An unavailable type's description
  leaves the guidance out, because the request cannot be made.
- **An unavailable type has a tool, and the engine refuses its requests.** A
  degraded type can still dispatch, and its description says it is degraded.
  An unavailable type cannot be carried out in this install at all. Its
  description says so.
  - The type stays visible because other descriptions name it. "Reversible by
    ioc.unblock" must point at a tool that exists, even one that is refused.
  - `POST /api/actions` refuses a request for a type with no usable binding,
    with a 422 that names the type and says it is unavailable in this install
    and cannot be carried out. It reads the binding state live, at the moment
    of the request, before anything is recorded. The re-request endpoint and
    a reversal whose inverse type has no binding are refused the same way. A
    reversal's own checks run first, so a bad reversal gets its own reason.
  - A usable binding is one whose adapter is configured and enabled, and
    whose scope matches the request. Health does not enter into it: a binding
    whose adapter is unhealthy is degraded and still accepted. The resolver
    answers this for the one requested type, with no health probe.
  - The catalog applies the same rule. A type whose bindings all lack a
    configured, enabled adapter is unavailable, not degraded, so its tool's
    description and the engine's refusal agree.
  - The case-note offer uses the same check, so a note that cannot be
    requested is not offered.
  - The agent hands that refusal to the model like any other engine
    refusal. The loop has no refusal of its own, so it cannot work from a
    stale status, and every caller of the API is covered, not only the agent.
  - On main the engine accepted such a request: it was recorded, waited for
    approval, and failed only at dispatch.
  - An action a person carries out is a different status from unavailable
    (#383).
- **No catalog means no action tools.** The catalog fetch and
  `POST /api/actions` answer 503 under the same condition, the action layer
  being off, so a generic tool could only be refused. The loop tries the
  fetch up to three times, with a short pause, before it reads a 503 as the
  action layer being off, so a passing 503 from a proxy does not remove the
  actions for the whole session. Any other error that survives the retries
  fails the session start, instead of being read as "no catalog".
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
- **The trial record keeps the tool list the session offered.** The model is
  shown its tools in each request, beside the conversation, not in it, so the
  record of the conversation does not hold them. The session exposes the list
  it built: each tool's name, its kind (verb, intrinsic or action), and the
  action type of an action tool. The eval runner records it in the trial
  record beside the assembled system prompt, which is kept for the same
  reason: it is what the model was told. The graders look each call's name up
  in that list. A name in it as an action tool is an action request of that
  type, and a name not in it was not offered. So the graders read the loop's
  own answer and never work it out again from the name or the catalog. The
  tool name is never the test: the `request_` prefix only names the tool for
  the model.
- **The prompt names the action tools.** The base-prompt rules that named
  `request_action` name the action tools instead. `parameters-are-an-object`
  is retired, because the model no longer writes a `parameters` object.
  `contain-on-harm` pointed at `list_action_types`, which the model has no
  tool for. It now points at the reversibility in the action tool's
  description.
- **H3, H5 and H6 grade the action tools.** H3 fails a call to any tool the
  session did not offer. H5 and H6 take the action requests from the
  recorded tool list and grade each call against its own tool's inputs. The
  catalogue version moves. Runs recorded before this change are not regraded.
- **The eval is the live check.** No separate probe of the tool shapes is
  kept. A probe caught strict decoding forcing the wrong shape, and that
  cannot happen with flat tools and no strict mode. Whether the model picks
  the right tool and fills it in correctly is what H5 and H6 grade on the
  bundled scenarios.
- **The eval run stops if it cannot fetch the action catalog.** An eval runs
  in a controlled environment, so a missing catalog means a broken setup.
  The runner fails the run at once, with no retry, instead of grading
  against no inputs.
- **The eval run stops if it cannot load the investigation's action list at
  the end of a trial.** The graders read that list as the record of what was
  requested. Without it H4 falls back to counting every attempt, including
  one the engine refused, so the run stops instead. The first such failure
  cancels the trials still running, so no more tokens are spent on a run
  that will be thrown away.

## Alternatives rejected

- **A tag inside `parameters`.** Each shape begins with an `action_type` key
  that names it, so writing the tag picks the shape. The union stays in the
  arguments, the fix still depends on one provider's grammar, and it keeps the
  `null` pattern, a budget against the union limit and a non-strict fallback.
- **A tagged union over the whole call.** An `anyOf` at the root of the input
  schema, one branch per action type. It is undocumented for strict tools, and
  the shared fields repeated in every branch would pass the union limit.
- **Write the keys in the descriptor's order.** It does not decide the shape:
  `body` would still lead both `ticket.comment` and `notify.email`.
- **Keep strict mode on the action tools.** About fifteen action tools would
  pass the limit of 20 strict tools. Flat schemas do not need it, and it ties
  the tool definitions to one provider.
- **A generic `request_action` beside the action tools, for a missing
  catalog.** With no catalog the engine refuses every request, and the open
  `parameters` object is the fault adr/0376 recorded.
- **Hide unavailable types from the model, as unavailable verbs are.**
  Descriptions name other action types ("Reversible by ioc.unblock"). With the
  type hidden, the model is pointed at a tool that does not exist, and a call
  to it is an invented tool.
- **Let a request for an unavailable type through, as on main.** It would be
  recorded and wait for approval, so a person could approve an action that can
  never run. It fails only at dispatch.
- **Refuse it in the loop.** The loop knows only the status from session
  start, it covers only the agent, and a refusal that never reaches the
  engine is a third kind of outcome every grader has to learn.
- **Name the unavailable types in one line in every action tool, or through a
  `list_action_types` tool.** The line repeats in every tool, and the tool
  only repeats the tool list. A tool per type that refuses says the same thing
  where the model looks for it.
- **Tell action tools apart by the `request_` prefix.** A read verb could
  carry the same prefix. The loop already knows what it built.
- **Reload the catalog mid-conversation.** A new tool list makes the next
  request rebuild the whole prompt cache once. It also needs a command in the
  workbench, so it is a feature of its own.
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
- Descriptions are built from the catalog the session fetched. A type wired
  mid-session is still described as unavailable until the analyst resets the
  context or opens a new session, but the engine accepts a request for it,
  since it checks live. A reset fetches the catalog again, and keeps the tools
  it has when the fetch fails.
- A binding can still go after the request, before approval or dispatch. No
  check at request time can close that window. Dispatch is the final check
  (#384).
- A session in an install with actions switched off waits through the retries
  of the catalog fetch before it starts.
- The trial record gains the session's tool list. The field is added, and
  nothing existing changes shape. The record the backend stores is unchanged.
- The tool definitions change, so the prompt version moves and the per-model
  baselines are run again on every bundled scenario.
- The baselines were accepted with H6 at 1/3 for Sonnet on impossible travel.
  In two trials the model added an undeclared key to `ticket.create`, the
  engine refused it, and the next call left the key out. adr/0373 records it.
- `request_action` stays the name of the request, the API body and the domain
  operation. `design/08-write-side-actions.md §2` says that the model reaches
  it through one tool per action type. `design/10-eval-harness.md §3`,
  `eval/README.md` and `implementation/agent-reliability.md` follow.
- adr/0376's decisions on the strict flag, the required-and-`null` rule, the
  union of shapes and the strict limits no longer hold. Its deletion of the
  repair, and the engine's refusal of a string, stand.
