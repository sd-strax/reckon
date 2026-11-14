# 0107 — The Knowledge view is built: past cases listed with filters, procedures and cases read outside any investigation

Issue: #107 · PR: #314 · Status: accepted · entry point superseded by 0342 (the sidebar's Investigations title bar) · section list and Import section superseded by 0346

## Context

[0084](0084-knowledge-view.md) and `design/ui/09` give knowledge its own view
outside any investigation. Inside an investigation, the Library section of the
companion column lists the procedures and past cases ([0129](0129-knowledge-through-tools.md)).
The view itself was never built: the workbench had three surfaces
(investigation, case, import; [0248](0248-panel-csp.md)).

`ui/09 §9.4` asks for past cases listed one row per concluded case, carrying
title, concluded date, disposition, techniques, primary entities, actions
taken with outcomes, and duration. The list is filterable by technique,
disposition and date range. Binding gap 21 named this a missing listing.
Part of that was stale:

- **The listing existed.** `GET /api/knowledge/case_summaries` returned the
  newest 100 summaries, with no parameters.
- **The Library row** showed a title and one line of text.
- **The facets existed as tags.** The summary producer writes technique ids,
  `disposition:<word>`, `seed:<kind>` and `entity:<id>` as substrate tags on
  every entry (`summaryTags`). But `substrate.ListFilter` took one tag.
- **The conclusion time** is host meta, which the substrate must not read
  (`00-substrate §12`).

The two seeded demo cases carried `true_positive` as their disposition.
Investigations write the aggregate's words (`BENIGN | SUSPICIOUS | MALICIOUS`),
so a disposition filter would have shown two vocabularies.

Two placements were weighed: filters in the Library inside the investigation,
or the Knowledge view. The Knowledge view won:

- `ui/09 §9.1` puts browsing the archive outside the investigation.
- The companion column is about 400px, too narrow for entities and actions
  without a fourth line per row.
- The Ask mode (#111) needs a surface outside any investigation.

## Decision

- **The substrate's list filter takes tags, ANDed.** `ListFilter.Tag string`
  becomes `Tags []string`; an entry must carry every one. Each tag is one SQL
  `= ANY(tags)` clause. Every existing caller passes its one tag in the list.
  The substrate stays host-neutral: tags are opaque strings to it.
- **The facade filters the past-case listing.**
  `ListSummaries(tenant, SummaryFilter)` takes `Techniques` (ANDed),
  `Disposition` (the verdict's word, compared without regard to case),
  `ConcludedAfter` (inclusive), `ConcludedBefore` (exclusive) and `Limit`.
  It returns `SummaryList{Summaries, Total}`, where `Total` counts every match
  before the limit.
  - Techniques go to the substrate as tags.
  - The disposition and the date range apply in the facade after the list,
    over the entry's meta. The disposition is compared there so it matches in
    any case, as the facets count it, whatever case an older entry's tag was
    written in. A summary without a conclusion time, which the producer
    writes as the zero time, has none: it lists last with an empty time and
    never matches a range.
  - `CaseSummaryFacets` counts the techniques and dispositions present in the
    corpus, most common first.
- **One route gains parameters, and one route is new.** Both use read roles.
  - `GET /api/knowledge/case_summaries` takes `technique` (repeatable),
    `disposition`, `concluded_after`, `concluded_before` and `limit`. A date is
    RFC 3339, or `YYYY-MM-DD` read as midnight UTC. The limit runs from 1 to
    500 and defaults to 100.
  - The response is `{case_summaries, total}`. A malformed date or limit is
    400, and so is a technique carrying a colon: a technique is an ATT&CK id,
    and a colon would reach the summary's other tags through it.
  - `GET /api/knowledge/case_summaries/facets` answers
    `{techniques: [{value, count}], dispositions: [{value, count}]}`: the
    values present in the corpus, so a picker never offers one no case
    carries. The counts are corpus-wide, not narrowed by the other filters.
  - Wire fixtures `caseSummaries` (with `total` and the row's structured
    fields) and `caseFacets`.
- **A fourth surface, `knowledge`.** `src/knowledgeDocument.ts` hosts one
  singleton panel, the way the case document is hosted. It is opened by the
  `reckon.openKnowledge` command, by a Knowledge button in the left region's
  header (`ui/08 §8.3`), and from the Library. Every load runs the backend
  handshake.
  - The page is `webview/src/surfaces/KnowledgeApp.tsx`. It lists three
    sections with their counts: Procedures, Past cases and Import. A query bar
    sits above the content, and the inspector opens beside the list.
  - It mounts the investigation document's store with no investigation
    loaded, so the procedure and past-case panes render unchanged.
  - Its message pair, `KnowledgeInboundMessage` / `KnowledgeRenderMessage`, is
    a typed subset of the investigation document's: the knowledge reads,
    opening an investigation a case names, and copying a record's JSON. The
    page drops any other intent the store sends (the label reads of chips in
    a document's text) before it reaches the host. A pane the host cannot
    answer here, a reference from a document's text, says so rather than
    waiting.
  - It shows knowledge only. With no investigation loaded, the procedure and
    case inspectors hide "In this case" and "What the agent read", and the
    citations tab reads "Where cited · k of n" over every investigation.
  - The only thing it can do to an investigation is open one: a past case
    names the investigation it came from as a link.
  - An import, and the panel coming back into view, re-read the library, the
    facets and the past cases under the last filter asked for.
  - The library answer carries the corpus total beside its page, which the
    section count and "n of m match" read; when that read fails neither
    claims a size. A failed procedures read is reported as a failure with
    Retry, never as an empty library, and keeps the list read before it.
- **Past cases are a table, filtered by the API.** Every filter change is a
  request; nothing is sieved in the page.
  - The controls are technique chips from the facets (ANDed), a disposition
    select, and concluded-from and to days. Both days are inclusive; the
    client sends the to-day as the next day, to the exclusive bound. An answer
    for a filter no longer on screen is not shown under it.
  - The view says "n of m match". Each row carries the title (it opens the
    case inspector), the concluded day, the disposition as a verdict pill, the
    techniques, the entities, each action with its recorded outcome word
    verbatim, the duration, and "cited in N investigations"
    ([0109](0109-knowledge-citations.md)).
  - The client maps the server row to a typed `PastCaseRow`. The Library
    inside an investigation keeps its compact rows, built from the same read.
- **One query bar component** (`components/QueryBar.tsx`) is mounted in the
  Knowledge view and in the Library. In this record it searches only. A
  search replaces the view's content with the results under a header naming
  the query, with a way back to the section. The store shows only the answer
  to the search asked last, and none once it was dismissed (Clear, back, or
  another section), so a slow answer cannot replace what the analyst moved
  to.
- **The procedure pane shows the outline of the revision its body is**, in
  both surfaces ([0303](0303-section-recall.md)). The document is read first;
  the outline is then read for that revision through the revision route
  ([0097](0097-sop-revisions.md)), since two independent reads of the current
  revision could straddle an edit and show one revision's body under
  another's headings. A failed outline read leaves the pane without it.
  `src/knowledgeAnswers.ts` composes the procedure and past-case answers once
  for both hosts.
- **Pane names come from the answer** where no investigation's thread names
  the item: the store records the procedure's or case's title as it arrives.
- **The demo seeds use the aggregate's disposition words.**

## Alternatives rejected

- **Filters in the Library inside the investigation.** At the column's width
  a row cannot carry entities and actions without another line. Browsing the
  archive belongs outside the investigation (`ui/09 §9.1`), and #111 needs
  that surface anyway.
- **Filtering in the page over the full listing.** It is correct only up to
  the listing's limit, it hides the total, and it duplicates a query the
  corpus answers.
- **Several tag queries intersected in the facade.** That costs one list per
  facet, where one SQL statement does the same over an indexed column.
- **The conclusion time as a substrate column.** The substrate would then
  read host vocabulary; the range is a cheap pass over the listing.
- **A separate store for the Knowledge view.** It would mean writing the
  procedure and case panes a second time, or forking them; with no
  investigation loaded the investigation store serves them as they are.

## Consequences

- An analyst can browse, filter and search the past cases outside any
  investigation and read any of them in full.
- `substrate.ListFilter` changes shape. It is a Go type inside this module
  with no out-of-tree caller.
- The listing's wire shape gains `total` and the row's structured fields.
  `facets` is new.
- Not built here, all named by `ui/09` or `workbench-react §2.6`:
  - procedure filters (tag, technique, recommendation, status, author);
  - edit and retire from the view, and a Versions tab (the revisions route,
    [0097](0097-sop-revisions.md), is served);
  - a Work toggle that lists the queue in the Knowledge document's region;
  - the Ask mode, which is #111 and joins the same query bar.
- design/ui/00-backend-binding gap 21, ui/09 §9.4 and workbench-react §2.6
  note what is built. design/13's phasing of the view stays as written for
  the parts above.
