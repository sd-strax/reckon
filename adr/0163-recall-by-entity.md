# 0163 — Past investigations are found by entity with an exact lookup; similarity recall is for patterns

Issue: #163 · PR: #TBD · Status: accepted

## Context

Asked "have we seen this before?" on a new investigation of WIN-FILE01, the
agent answered "No" — though an investigation of the same host and account had
concluded MALICIOUS the same day. Four causes, all below the prompt:

- The only lookup was similarity recall over case summaries. With nothing but
  a host name to go on it compares two words against a page of summary, which
  scores low even when the host is in the text; the concluded case banded
  DISTINCT.
- The recall tool described its tags filter with the example `seed:entity`.
  Summaries are tagged `seed:<kind>` (`seed:case`), so the agent filtered on
  `seed:WIN-FILE01`, which no summary carries.
- A summary recorded an entity only when the investigation was seeded from
  one. That case was seeded from a case, so nothing tied it to WIN-FILE01.
- A recall emptied by a filter returned the same EMPTY as a corpus with
  nothing alike, and EMPTY is described to the agent as evidence of absence.

"Have we investigated this before?" and "have we seen this pattern before?"
are different questions. The first is exact and answerable from the seed
alone; the second needs observed behaviour to compare.

## Decision

- **A summary records the entities the investigation involved:** the seed
  entity plus every action target's resolved identifier, each once (compared
  case-insensitively, first spelling kept). Each is an `entity:<identifier>`
  tag, lowercased. The extractor version is 2.
- **An exact lookup answers the first question.** `POST
  /api/knowledge/prior_investigations {entity}` (any reader) and the agent tool
  `prior_investigations` return every concluded investigation whose summary
  records the entity, most recent first, COMPLETE with matches and EMPTY
  without. The tool description says it works before any telemetry and that
  similarity recall is for patterns. Its matches are tracked as consulted past
  cases on the turn, the same as recall hits.
- **A filtered recall says when the filter emptied it.** When a recall with
  tags returns nothing and a filter tag matches no summary, coverage is
  `EMPTY_UNDER_FILTER` and `unmatched_tags` names them.
- **The tags example names the real vocabulary:** a technique id,
  `seed:<kind>`, `disposition:<verdict>`, `entity:<identifier>`.

## Alternatives rejected

- **Fold the exact lookup into similarity recall** (an `entity` parameter).
  One tool answering two questions with two coverage meanings invites the
  same misreading.
- **Extract entities from every cited observation.** Action targets are the
  concrete identifiers the analyst approved; the observations' entities live
  in the STIX store under ids, and resolving them is a larger change than the
  lookup needs.
- **Re-derive all existing summaries now.** Summaries written before this
  change carry no entity tags; re-concluding an investigation re-derives its
  summary (the producer revises, it never duplicates). The demo's seeded
  prior cases already list their entities and are tagged on seed.

## Consequences

- An investigation with no entity seed and no actions records no entities and
  cannot be found this way; its summary is still recalled by similarity.
- The knowledge rail's recall at open from the bare seed is a separate change
  (#129, which removes implicit retrieval).
