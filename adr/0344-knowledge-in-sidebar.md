# 0344 — The knowledge library is a sidebar view; the companion column carries none

Issue: #344 · PR: #345 · Status: accepted · Supersedes: the rail bullet of `0129`, and the
Knowledge entry point of `0342` (the Investigations title bar) · row-open and Ask rules superseded by 0346

## Context

`ui/09 §9.1` put a **Knowledge section** at the top of the investigation's companion column
so that the analyst could decide what knowledge the agent reasoned over in that
investigation. `adr/0129` removed that decision: there is no implicit retrieval and no
include tick, and the agent reads knowledge only through its recall tools, in the turn.
The section stayed, as a library: every procedure and the newest page of past cases,
unfiltered and the same in every investigation. It took the top of the companion in every
investigation, above the conversation. Nothing in it was in the agent's context, and nothing
in it was about the open investigation.

Knowledge belongs to the organization and exists before any investigation opens. That is
the argument `adr/0342` used to move the work queue to the editor's sidebar, and it applies
here.

## Decision

- **The sidebar holds four views, in this order:** **Investigations**, **Cases**,
  **Knowledge**, **Capabilities**. Capabilities starts collapsed: it is read-only health,
  with set-up as its one act, and the analyst opens it when a verb is missing.
- **The Knowledge view is a native tree with two groups.**
  - **Procedures (N)**: one row per procedure, with its title and its tags, and the
    *cited in N investigations* reading in the tooltip.
  - **Past cases (N)**: one row per concluded case summary, with its title, its
    disposition and its concluded date. When the corpus holds more past cases than the
    page, the group reads *N of M*.

  The data is the same listing the library read (`GET /api/sops`,
  `GET /api/knowledge/case_summaries`).
- **A row opens its item in the Knowledge view** (`adr/0107`), the document that hosts the
  procedure and past-case inspectors. That view opens on that item.
- **Ask reckon to use this** is an inline action on every row. It is offered while an
  investigation document is the active editor, and it stages the same request in that
  investigation's composer that the library staged. The request text is written once.
- **The view's title bar** carries:
  - **Search**, which opens the Knowledge view with its query bar focused.
  - **Import SOPs…**, the existing command.
  - **Open the Knowledge view**. This moves the entry point that `0342` put on the
    Investigations title bar.
  - Refresh.

  Search by meaning stays where it is answered: the Knowledge view's query bar.
- **The view re-reads when the library changes.** That covers an import, a procedure added
  from **Draft SOP from this case**, and a conclusion (a new past case, read again once its
  summary settles). It also re-reads on sign-in, on sign-out and when the backend changes,
  as the other views do.
- **The companion column has no library.** It opens on the conversation. Investigation
  documents no longer read the library or answer `library.*` requests. The Knowledge view
  keeps its own reads.

## Alternatives rejected

- **Keep the library in the companion, collapsed by default.** It is still a catalogue
  unrelated to the open investigation, repeated in every investigation, and collapsing it
  only hides that.
- **A list ranked for the open investigation, in the companion.** That is implicit
  retrieval by another name, which `0129` rejected. What the agent consulted and cited
  for this investigation is already on the record, as chips in the conversation.
- **A sidebar webview carrying the library's own search.** That adds a second webview
  host for one search box. The Knowledge view already answers the query, and the tree only
  has to get the analyst there.

## Consequences

- Every investigation gives its companion column to the conversation.
- Searching the library from an investigation takes one more step: the sidebar's
  **Search**, then the Knowledge view's query bar.
- The demo's knowledge beat reads from the sidebar. The Procedures count goes 0 → 1 on
  import, and the search runs in the Knowledge view (`implementation/demo-script.md`).
- `ui/08 §8.5` and `ui/09 §9.1` describe the companion without a knowledge section and
  the Knowledge view's sidebar entry.
