# 0103 — The summary's facts are a server-side fold at any state; the narrative is drafted on the client plane and kept as revisions with their authorship, the AI draft superseded by an analyst edit and never overwritten

Issue: #103 · PR: #104 · Status: accepted

## Context

`06 §3.2` defined a two-tier summary — structured facts extracted by the server, a narrative
written on the client plane because the backend holds no model key — and built it only at
conclusion, inside the knowledge pipeline. An open investigation's Summary lens showed a gap
line where the narrative should be, the handoff note was assembled from scattered slices in the
webview, and nothing recorded who drafted or edited a narrative. Gap 18 of
`design/ui/00-backend-binding.md §6`.

Constraints: the backend never holds a model key (`06 §3.2`, the user's decision recorded in the
knowledge arc); the AI is a delegate whose prose is a proposal the analyst may edit (`01`); the
export, the handoff note and the Summary lens must say the same things; nothing on the lens is
templated prose pretending to be the record.

## Decision

- **One fold, one package.** `summary/` holds the knowledge extraction (`Extract`, moved from
  `temporal/`; the pipeline calls it unchanged) and `BuildFacts`: at any state, from the
  projections — verdict with its author's name from the thread, containment counts and nearest
  expiry, notification status and outstanding replies, open questions as predictions with their
  titles, owner items by status, procedures applied, the ordered still-to-do, `as_of_sequence`.
  `GET /api/investigations/{id}/summary` serves it with the narrative lineage and the handoff note
  rendered from both, so the clipboard copy and the export agree.
- **The narrative is revisions.** `investigation_narratives` keeps every revision with
  `author_kind` (`AI_DELEGATED` | `HUMAN`), author, model, time and the record sequence it
  describes. `PUT /api/investigations/{id}/narrative` appends: the AI draft from the agent token,
  the analyst's edit from theirs. The authorship line reads the lineage: "drafted by reckon
  (model) · edited by <name> · revision N", and says when the record has moved on since.
- **Drafting runs on the client plane**: `narrative.draft` → `transport.draftNarrative` →
  sidecar `draftNarrative` → `agent.Session.DraftNarrative`, one no-tools completion over the
  session's own conversation with a where-it-stands instruction, recorded by the session through
  `PutNarrative`. A failure is a message on the lens, never a templated text.
- **At conclude**, the latest narrative prefills the conclusion summary; the knowledge summary
  pipeline is unchanged (it still writes its own narrative for the corpus at conclusion).

## Alternatives rejected

- **Templating a narrative from the facts**: the lens would show prose nobody wrote; the facts
  are shown as facts and the narrative is absent until someone drafts it.
- **Overwriting the narrative in place**: the AI draft is provenance; an edit is a new revision.
- **A server-side model call**: ruled out by the no-key posture.

## Consequences

- Migration `0029`. The webview's own handoff rendering stays as the fallback when the summary
  read fails.
- The facts endpoint gives the export (`07`) and the owner-item and comms counts one source;
  the Summary lens still derives its tiles from the snapshot for now, from the same slices.
