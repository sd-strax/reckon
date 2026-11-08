# 0109 — Knowledge citations are a projection: cited in N investigations, and where

Issue: #109 · PR: #311 · Status: accepted

## Context

Every reasoning act records what it consulted: `consulted_sops` and
`consulted_similar_investigations` on the `InterpretationRecorded` event, each
entry with `cited` (adr/0236: the agent's own text named it by the ref recall
returned). The thread read folds them per investigation; nothing folded them
across investigations. design/06 §6.1 promises that "every interpretation that
cited SOP X" is one SQL query against a projection, and `ui/09 §9.3` wants
"cited in N investigations" on the procedures list and a where-cited list on
the detail, `§9.4` the procedures learned from a past case. The projection
was never built (design/ui binding §6 gap 22), so the procedure and case
inspectors could say only what happened in the open investigation.

`ref_appearances` (`aggregate/appearances.go`) is the precedent: a projector
in the append transaction folding one event type into a join table, served
with title and status from `investigation_current`.

After adr/0303 one act may carry one procedure twice, at two revisions or two
cuts of one revision, and the aggregate accepts that: a key over
(ref, investigation, act) would collide.

## Decision

- **`KnowledgeCitationProjector`**, a sibling of `RefAppearanceProjector`,
  applied in the append transaction. Table `knowledge_citations(ref, kind,
  aggregate_id, tenant_id, interpretation_id, sequence_no, revision, cut,
  cited, occurred_at)`, one row per consulted entry: a procedure (kind `sop`,
  ref the SOP's lineage id) or a past case (kind `case`, ref the summary's
  investigation ref). It folds `InterpretationRecorded` only; `Reset`
  truncates, and replay is the standard rebuild. No replay runs at release:
  there are no existing investigations to fold.
- **The key is the record's own: `(ref, aggregate_id, interpretation_id,
  revision, cut)`.** Nothing is folded at write time; one procedure at two
  revisions in one act is two rows. An investigation record never loses a
  fact it carried, and the row says which revision and cut each act had in
  front of it, so the table can answer how many investigations consulted a
  procedure under an older revision, and a later read can go from a citation
  row to the exact section under the exact cut. A past case carries revision
  0 and an empty cut. On a conflict the citing fact is kept (`bool_or`),
  never overwritten.
- **The reads collapse to one row per investigation.** `ListKnowledgeCitations
  (ref)` groups by investigation: `cited = bool_or`, `last_at = max`, the
  count of acts, the distinct revisions, title and status joined from
  `investigation_current`, most recently touched first. `CitationCounts(refs)`
  is one grouped query for a list: `cited_in` is the distinct investigations
  with a citing act, `recalled_in` those that only recalled. Turn-level
  detail stays on the investigation's own record.
- **Two routes, mirroring `GET /entities/{ref}/appearances`.**
  `GET /api/sops/{id}/citations` and
  `GET /api/knowledge/case_summaries/{ref}/citations` answer `{ref,
  cited_in, recalled_in, investigations: [{investigation_id, title, status,
  cited, last_at, acts, revisions}]}`. They read the projection alone: an id
  nothing consulted is an empty list, not a 404, and a retired procedure
  keeps its history. They register with the knowledge routes; without an
  aggregate handler they answer 503, as appearances does.
- **Counts on the lists are composed by the server.** `GET /api/sops` rows
  and `GET /api/knowledge/case_summaries` rows gain `cited_in`: one
  `CitationCounts` query over the list's refs in the main database, merged by
  ref into the knowledge database's list (`SOPRow`, `CaseSummaryRow` embed
  the facade's shapes). The knowledge facade stays unaware of the
  projection. A failed count fails the list rather than showing zeros as
  facts; a deployment without the aggregate lists its corpus with every count
  zero.
- **"Procedures learned from this case"** are the SOPs whose source url is
  `reckon://investigation/<id>` (the draft-accept path, adr/0156). The facade
  gains `ListBySource(prefix)` (current, unretired) and the constant
  `InvestigationSourcePrefix`; `GET /api/knowledge/case_summaries/{ref}`
  gains `learned_procedures: [{id, title}]`. A summary's ref is the
  investigation's id, or `grouping--<id>` for a case seeded without a local
  aggregate; the prefix is stripped before the lookup.
- **No self-citation guard.** A summary is written at conclusion, so an
  investigation cannot recall its own; the lifecycle does not produce the
  state.
- **The words are adr/0236's.** Rows say "cited in N investigations"; a
  where-cited row says "cited" or "recalled, not cited"; never "applied" or
  "followed". The engine attests that the agent retrieved and named a
  procedure; it cannot attest that the agent applied it.
- **Workbench.** The procedure and case inspectors gain a "Cited elsewhere ·
  k of n" tab: one row per other investigation (the open one is left out;
  "In this case" above already covers it) with the word, title, status, the
  last act's time, the act count when more than one, and the SOP revisions
  seen; a row opens that investigation. The case inspector's Summary tab
  gains "Procedures learned from this case", each opening the procedure. The
  Library rows carry "cited in N investigations". The citations read rides
  beside the document read; when it fails the pane still opens and the tab
  says the read failed, never "none". The host's `inspector.procedure` and
  `inspector.case` messages carry `citations` optionally.

## Alternatives rejected

- **Fold per act on apply: `cited = bool_or`, `revision = max`, drop the
  cut.** Loses the revision and cut an act actually saw, the fact adr/0303
  made the record carry; the historical section read needs both.
- **Fold on read over every investigation's events.** Correct and
  schema-free, linear in the whole log per list render, and contrary to
  design/06 §6.1's single query against a projection.
- **Store `cited_in` on the SOP or summary entry.** The corpus lives in
  another database and the count belongs to the investigation record; a
  write into the knowledge store per act would couple the two and race the
  revision.
- **A self-citation guard.** The lifecycle cannot produce the state; a guard
  would be code for a case that does not occur.

## Consequences

- The procedure and case inspectors answer "where else" and "what was
  learned", and the Library rows carry the count the Knowledge view's lists
  will show.
- Two wire shapes change additively (`cited_in` on list rows,
  `learned_procedures` on the case detail) and one is new (`citations`); the
  workbench decoders read all three against regenerated fixtures.
- A list render costs one more query over the main database; the counts are
  grouped by ref over an indexed table.
- The row keeps revision and cut for the historical read of a cited section
  (#97); the shape of that read is that record's to decide.
- `ListBySource` lists the corpus and filters in memory, as the import's
  source lookup does; an indexed source key is a store change with its own
  record.
