# 0382 — Action descriptors and verbs are checked when they are registered, with the rules the agent builds its tools by

Issue: #382 · PR: pending · Status: accepted

## Context

The agent gives the model one tool per verb, one per built-in tool, and one per
action type (adr/0380). An action type's tool is named `request_` plus the type
with dots turned into underscores, and a verb's tool is named after the verb.

A tool list only works if:

- every name is a valid tool name for the model API the agent calls. The main
  model APIs differ: some allow 128 characters, some 64; some allow dots or
  colons; one requires the first character to be a letter or underscore;
- no two tools share a name;
- no action input uses the name of a field every action request carries
  (`targets`, `rationale`, `evidence_refs`, `retry_of`), and no input is
  declared twice.

The backend registered descriptors and verbs without checking any of this. Only
the agent checked, while building its tools at the start of each session, and it
never checked characters. So a bad descriptor made every session fail to start,
for every analyst, instead of failing once at backend startup. An out-of-process
adapter could do the same by reporting a verb named like an action tool or a
built-in tool: one bad verb took down every session.

adr/0385 lets the agent use shared packages and created `internal/catalog` for
what the backend's catalogs and the agent's tool list share.

## Decision

- **`internal/catalog` holds the tool-name rules:** how an action type becomes
  its tool name, whether a name is a valid tool name, the fields every action
  request carries, and the agent's built-in tool names. The backend and the agent
  use the same code.
- **A valid tool name is one every main model API accepts:**
  `^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`. That is letters, digits, `_` and `-`, a first
  character that is a letter or underscore, and at most 64 characters, the
  strictest of the documented provider rules. An action tool is `request_` plus
  the type, so an action type has at most 56 characters.
- **An action descriptor is checked when it is registered.** It is refused when
  its type gives an invalid tool name, or when an input uses a request field's
  name or is declared twice. The action catalog is built in code, so a refused
  descriptor fails backend startup with an error naming it.
- **A verb is checked when it is registered.** It is refused when its name is not
  a valid tool name, is a built-in tool name, or is an action type's tool name.
  - A built-in verb that is refused fails backend startup.
  - An adapter's verb that is refused is left out, with a log line naming the
    adapter and the verb. The adapter's other verbs are registered, as with an
    adapter that fails to describe itself (`design/11 §4.2`).
- **The agent stops checking descriptors.** It keeps one check: two tools with
  the same name fail tool building. With the shared rules this should never
  happen, and it keeps such a list from reaching the model API.

## Alternatives rejected

- **One provider's rule.** The agent's model interface is provider-neutral
  (`agent/llm.go`), and a name valid for one API can be refused by another. A catalog checked against the common
  rule works with any of them.
- **The backend reserves the `request_` prefix for action tools** and refuses any
  verb that starts with it. It needs no knowledge of how a tool name is built,
  but it misses clashes with the agent's built-in tools, and adr/0385 already
  allows the shared rules this needs.
- **The agent drops a clashing verb and starts the session anyway.** Each
  session would decide on its own and log on its own. The fault belongs to the
  registration, which happens once.
- **Removing every agent check, as the issue first asked.** The last check costs
  three lines and stops a duplicated tool list from reaching the API if a new
  path ever skips registration.

## Consequences

- A bad descriptor or built-in verb is found once, at backend startup, by
  whoever changed the code.
- An adapter can no longer stop every agent session by naming a verb badly. It
  loses that verb, and the log says which one.
- A new built-in tool needs its name in `internal/catalog`, so the backend can
  refuse a verb that clashes with it.
- `ActionCatalog.Register` and `Catalog.Register` now return an error.
