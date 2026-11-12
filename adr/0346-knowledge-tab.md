# 0346 — Knowledge is a top-level tab whose only navigation is the sidebar

Issue: #346 · PR: #347 · Status: accepted · Supersedes: `0344`'s "a row opens its item beside
the investigation" and its Ask rule, and the section list and Import section of `0107`

## Context

After `0344`, clicking an item in the sidebar's Knowledge view opened the Knowledge document
beside the investigation. The document still carried its own navigation:
- a section list (Procedures, Past cases, Import)
- the procedure list and the past-case list
- the item, in an inspector over its right edge

Everything in that navigation repeated the sidebar. Knowledge is a service of its own, not
a pane of an investigation.

## Decision

- **The Knowledge document is a top-level tab.** A sidebar row opens it in the active editor
  group, where it takes the focus. There is one Knowledge tab, and it is reused and revealed
  where it is.
- **The sidebar is its only navigation.** The document has no section list, no procedure
  list, no past-case list and no Import section. Import is the **Import SOPs…** command and
  its panel, on the sidebar's Knowledge title bar.
- **The page is the item.** A procedure or a past case fills the tab, with the content and
  the actions its inspector had: the outline and sections, where it was cited, and the
  revisions. Links inside it move within the tab, with a way back.
- **Search is a page of the same tab.** The sidebar's **Search** opens the tab on that page.
  It has the query bar, the results as two lists (procedures, past cases) with relevance in
  words, and the past-case filters (technique, disposition, concluded range). With no query,
  the filters alone list past cases. A result opens as the page.
- **Ask reckon to use this targets the investigation last active, and only that one.** The
  Knowledge tab now takes the focus, so the target is no longer the active editor. It is the
  investigation the analyst was last in. Ask is offered while that investigation can take a
  request. It stages the request there and reveals the investigation. When that
  investigation cannot take it (concluded, or with a turn running), Ask refuses with the
  reason. It never falls back to another investigation, which would put the request on a
  record the analyst was not working in.
- **An item opened from the search page has a way back to it**, to the same results or
  filters. An item opened from the sidebar starts a fresh page.

## Alternatives rejected

- **Keep the beside pane and drop only its lists.** The item would still be a pane of
  whichever investigation was open, when it belongs to the organization's knowledge.
- **Show the item in the sidebar itself.** A tree row cannot hold a procedure's sections,
  its citations or its revisions.
- **Drop the past-case filters along with the lists.** They are the only filtered listing of
  past cases. On the search page, they keep it without a second navigation.

## Consequences

- Reading an item and then asking the agent about it costs one tab switch, which the Ask
  action makes itself.
- `ui/09 §9.1` describes the tab as an item page and a search page, opened from the sidebar.
