# 0385 — The agent may use shared packages but never the engine's, and the availability statuses have one definition in `internal/catalog`

Issue: #385 · PR: pending · Status: accepted

## Context

The backend tells the agent whether each verb and each action type can be used.
It defines the words as two types with the same three values:
`capability.CapabilityStatus` and `action.ActionAvailability`, each `available`,
`degraded` or `unavailable`.

The agent read both into plain strings and compared them as literals, in four
places, plus four more in the example catalog behind the prompt version. A
renamed word on the backend compiled on both sides and changed what the agent
did without any test failing.

The agent's production code imported no reckon package at all, so it could not
use the backend's constants. Nothing asked for that. `design/05-component-architecture.md §2.7`
says where the loop runs and where the model key lives, not what it imports.
adr/0237 kept the sidecar off `server` because that pulled in the whole engine,
and it did so by moving the API version into a leaf package the sidecar imports.

What does matter is how the agent reaches the engine. It does so only through the
HTTP API, because that is where the AI-delegate token and the engine's gates
apply (05 §2.7, `design/04-action-authorization.md`).

## Decision

- **What the agent may import.** The agent never depends on the engine's
  packages: `aggregate`, `server`, `action`, `capability`, `temporal` and
  `knowledge`. It may use reckon's shared packages, such as those under
  `internal/` that depend on none of them. A test checks the agent's full
  dependency list (`go list -deps`), in the same form as adr/0237's test for the
  sidecar.
- **`internal/catalog` holds what the backend's catalogs and the agent's tool
  list share.** It imports no reckon package, and a test checks that. This
  change puts the availability statuses there.
- **Two types, as the backend had them:** `catalog.CapabilityStatus` and
  `catalog.ActionAvailability`, each with three constants. The two can grow
  apart: a status for actions only does not become valid for verbs.
- **The backend's types are aliases of the shared ones**, and their constants
  are declared from them, so backend code is unchanged.
- **The agent's `Capability.Status` and `ActionType.Status` take the shared
  types**, and every comparison and assignment in `agent/` goes through a
  constant.
- How the agent treats a word it doesn't know is unchanged. A new value is added
  together with an API version change, and the sidecar refuses a backend whose
  API version differs from its own, so an agent in use doesn't receive a word
  newer than itself.

## Alternatives rejected

- **The agent keeps its own copy of the constants, pinned to the backend's by a
  test.** It works, but it writes the same fact twice to keep a boundary nothing
  asked for.
- **The agent imports `action` and `capability`.** Those are engine packages. An
  agent that links them could call engine code directly, outside the API and its
  gates.
- **One shared type for both.** The two statuses mean different things and can
  grow apart.
- **One package per subject.** The statuses and the tool-name rules (#382) are
  the same contract: the catalog as the agent sees it. Splitting them gives each
  half a package of its own for no gain.

## Consequences

- A renamed status can't drift between the backend and the agent: both use the
  same constant.
- The agent's boundary is a tested rule instead of a habit. A new agent import of
  an engine package, direct or through another package, fails `make test`.
- #382 puts the tool-name rules in `internal/catalog`, so the backend checks
  descriptors and verbs with the same code the agent builds its tools with.
- The prompt version does not change: the constants render the same text.
