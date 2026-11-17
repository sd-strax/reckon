# 0386 — Each action tool's description names its action type

Issue: #386 · PR: pending · Status: accepted

## Context

The agent requests each action type through its own tool, named `request_`
plus the type with each `.` written as `_` (adr/0380). The tool name is the
action type spelled for the model APIs, which don't allow a dot in a tool name
(adr/0382).

Everything the engine tells the model about actions uses the action type id:

- the engine-state block's action rows;
- the rows `list_actions` returns;
- the request refusals, such as `action_type "ticket.create": …`;
- the descriptors' intent texts, such as "Reversible by ioc.unblock".

The agent's code holds the link between a tool and its type, but nothing the
model reads states it. A tool's description held the intent, tier and
reversibility and the request guidance, never the type. So when the model
re-requested a failed action, followed a "reversible by", or read a refusal, it
linked the two by the naming pattern.

## Decision

- **Each action tool's description starts with its action type**:
  `Action type: ticket.create.` Every tool states it, unavailable ones included.
- **The action type stays the one name for actions.** The state rows,
  `list_actions`, the engine's refusals and the intent texts keep the type id.
  The model matches any of them to the tool that names that id.
- The line is engine text (adr/0232), so the prompt version moves.

## Alternatives rejected

- **Everything the model reads uses tool names.** The agent would rewrite the
  state rows, the `list_actions` rows and the engine's refusals on the way
  through, and the intents would name tools. The tool spelling would become a
  second vocabulary, and some of that text belongs to the engine, which has no
  reason to know how the agent spells a type.
- **Tool names equal action type ids.** The model APIs don't allow a dot in a
  tool name.

## Consequences

- Every action the model reads about matches its tool by an exact string.
- A new place that tells the model about an action can use the type id, with no
  translation.
- The eval baselines need a rerun for the moved prompt version.
