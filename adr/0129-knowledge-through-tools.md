# 0129 — Knowledge reaches the agent only through its recall tools; the rail becomes a library

Issue: #129 (with #128, #130, #131, #132) · PR: #TBD · Status: accepted · Supersedes: `design/06 §5.1` (implicit retrieval and the injection dial), the include section of `adr/0084`

## Context

`design/06 §5.1` had the agent session recall SOPs and similar past cases against
the seed on its own, list them in the investigation's rail with similarity
bands and an include checkbox, and deliver what the analyst included to the
model as a `[knowledge]` block; a tenant dial (`knowledge.injection`,
opt_in | auto) set each item's initial inclusion.

The road test showed three problems with that model:

- **Relevance is not a property of the knowledge.** A procedure is relevant
  when the analyst or the agent is asking a question it answers — "what's our
  SOP for RDP?" — not because a recall over the bare seed scored it. Recall at
  open runs before anything has been observed, so its results are weak, and
  presenting them as "retrieved for this case" overstated them.
- **Two ways in, one of them invisible.** The agent already has
  `recall_sops`, `recall_similar_investigations` and `prior_investigations`,
  visible in the turn and audited on the record. The checkbox put knowledge
  into context with no act in the conversation, so the turn did not show what
  the agent read.
- **The rail was a curation surface pretending to be a library.** Procedures
  and past cases shared one list, distinguished by a dim word; a DISTINCT band
  showed for cases that were not alike; searches piled into the list and could
  not be cleared; the inspector said "retrieved at seed time" for search hits
  and showed an excerpt the agent never received.

## Decision

- **No implicit retrieval.** The session no longer recalls at open, holds no
  retrieved set, and sends no `[knowledge]` block. `knowledge.injection` and
  its validation are removed (an old config key is ignored). The sidecar's
  `createSession` no longer returns knowledge, `searchKnowledge` is gone, and a
  turn carries no `included_knowledge`.
- **Knowledge enters an investigation only through the agent's recall tools**,
  asked by the analyst or on the agent's judgement — visible in the turn, and
  linked on the record as consulted (`06 §6`, unchanged).
- **The rail is a library**, the same in every investigation: procedures and
  past cases as separate lists (`GET /api/sops`, new `GET
  /api/knowledge/case_summaries`), a search over both whose hits are their own
  list with relevance in words (close match / related / loosely related) and a
  Clear, **Import procedures…**, and on each item **Ask reckon to use this**,
  which stages a request in the composer.
- **The inspectors say only what happened here.** "In this case" lists the
  turns whose recalls carried the item; "What the agent read" shows what those
  recall results gave the agent, read from the committed turns — nothing else.

## Alternatives rejected

- **Keep implicit retrieval but off by default.** The surface and the
  invisible path stay, and every reading of the rail has to explain a state
  the analyst did not create.
- **Auto-include search results the agent runs.** The agent's own recalls are
  already in its context through the tool result; a second copy adds nothing.

## Consequences

- The agent's prompt still mentions `[knowledge]` blocks in one sentence; with
  none sent it is stale but not wrong, and is removed in the next governed
  prompt change (#137).
- Eval scenarios that relied on included knowledge must ask for it instead.
