# 0107 — Concluded-case summaries are listed as a read over the case-summaries corpus, no new table

Issue: #107 · PR: #108 · Status: accepted

## Context

The Knowledge view's Past cases section (`ui/09 §9.4`) lists every concluded investigation
summary with its structured fields, narrowed by technique, disposition and conclusion window.
The DERIVED `case-summaries` corpus (`06 §3.2`, K3) could only be recalled by similarity
against a query; a listing did not exist, so the section carried a gap line. Gap 21 of
`design/ui/00-backend-binding.md §6`.

## Decision

- **The corpus is the record; a listing is a read over it.** `knowledge.ListSummaries(ctx,
  tenant, SummaryFilter)` is the mirror of the empty-query browse the procedure facade already
  owns: `substrate.List` over `case-summaries`, with the technique (or, failing that, the
  disposition) applied at the substrate as the tag the producer already writes, the rest
  applied from the structured meta; newest conclusion first; reserved tags stripped;
  `investigation_ref` recovered from the source tag. A re-derivation or an enrichment is a
  revision, so a case is one row.
- **`GET /api/knowledge/case_summaries?technique=&disposition=&from=&to=&limit=`** (reader),
  beside the existing `…/case_summaries/{ref}`. A row is the existing `CaseSummaryView`;
  "whether a narrative exists" is `generator_model` being set.
- **Workbench**: the host lists on load and on `cases.list` with the analyst's filters; the
  section's count stays the unfiltered total. The detail remains the similar-case inspector
  with Open that investigation.

## Alternatives rejected

- **A `case_summaries` table in `reckon_main`**: a second copy of what the corpus holds, to
  drift from it.
- **Listing from the aggregate's `investigation_current`**: concluded investigations without a
  written summary would appear with nothing to show, and the demo's seeded prior cases have no
  local aggregate at all.

## Consequences

- No migration. "Procedures learned from this case" on a past case is the inverse read of
  `source.url = reckon://investigation/<ref>` and lands with the citation projector (gap 22).
