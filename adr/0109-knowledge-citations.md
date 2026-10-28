# 0109 — Knowledge citations are an aggregate projection folded from the interpretation event; counts and where-used are reads over it

Issue: #109 · PR: #110 · Status: accepted

## Context

The Procedures list shows "cited in N investigations" and a procedure's detail shows where it
was used, with the two words the sheet names (`ui/09 §9.3`): applied, or consulted but not
applied. A past case shows the procedures learned from it. The citing act is already on the
`interpretation.recorded` event (`consulted_sops`, `consulted_similar_investigations`,
`06 §6`), but nothing folded it into a readable count or list. Gap 22 of
`design/ui/00-backend-binding.md §6`.

## Decision

- **A projector, because the citing act is an investigation event.**
  `aggregate.KnowledgeCitationProjector` folds every `interpretation.recorded` event's consulted
  lists into `knowledge_citations (tenant_id, kind sop|case, knowledge_ref, aggregate_id,
  interpretation_id, used, recorded_at)` in `reckon_main`, in the command's transaction, so the
  count can never disagree with the log; `Reset` + replay rebuilds it. A superseded act keeps its
  citation: the consultation happened.
- **Two facts, two stores, one event.** The retrieval side store (`adr/0097`) records *what text
  was read*; this projection records *that the knowledge was consulted and whether it was
  applied*.
- **Reads.** `GET /api/knowledge/citations?kind=sop|case&ref=` returns the investigations that
  consulted it, each with `applied` (any act with `used`), the act count, first and last time,
  title and status from `investigation_current`; for a case it adds `learned`, the inverse read of
  `source.url = reckon://investigation/<id>`. `GET /api/knowledge/citations/counts?kind=` returns
  `{ref: n}` over distinct investigations, applied or only consulted.
- **Workbench.** The Procedures list gains a Cited column and the detail a Where used tab; the
  similar-case inspector gains "Procedures learned from this case" and "Drawn on by".

## Alternatives rejected

- **Counting from the knowledge side store** (`knowledge_retrieval_uses`): it records what was
  retrieved for a turn, not what the committed act cited, and holds no past-case rows.
- **Counting at read time over the events table**: a scan of every event per list render.

## Consequences

- Migration `0030`. An existing deployment folds the projection on its next replay; until then
  the count reads zero for acts recorded before the projector existed.
