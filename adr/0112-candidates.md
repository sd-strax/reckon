# 0112 — A procedure candidate is a CRUD record in the knowledge database, never a corpus entry; accepting imports it through the procedure path, discarding carries a reason

Issue: #112 · PR: #114 · Status: accepted

## Context

At conclusion the sidecar drafts a candidate procedure generalized from the case
(`proposeSOP`), and the workbench opened it as an editor tab with Add / Discard. The draft
was not a record: the Knowledge view's Candidates section (`ui/09 §9.7`) could not list what
was waiting for review, nothing recorded who accepted or discarded a candidate and why, and a
closed tab lost the draft. Gap 24 of `design/ui/00-backend-binding.md §6`.

## Decision

- **A CRUD record, not a corpus entry.** `procedure_candidates` in `reckon_knowledge`
  (`knowledge.CandidateStore`, migration `0004`): title, body, tags, recommendation, rationale,
  generator model, who drafted it and when, `status NEW | ACCEPTED | DISCARDED`, who decided
  and when, the discard reason, the accepted procedure's id. An unreviewed draft must never be
  recallable, and every corpus the substrate holds is.
- **Producer.** The conclude hook keeps calling the sidecar's `proposeSOP`; when warranted the
  host `POST`s the draft to `/api/knowledge/candidates` and shows a status-bar line, no prompt;
  when not warranted nothing is stored and nothing is shown. The investigation shows no prompt
  for it (`09 §9.7`).
- **Reads.** `GET /api/knowledge/candidates?status=` and `GET …/{id}`, whose detail adds
  `closest`: one recall over the draft's text, computed on read, never stored.
- **Writes.** `POST …/{id}/accept { body }` imports the edited body through the existing
  procedure import with the `reckon://investigation/<id>` source URL (re-accepting revises the
  same procedure) and marks the row ACCEPTED with the procedure's id; `POST …/{id}/discard
  { reason }` marks it DISCARDED. A discarded candidate cannot be accepted; a decided one cannot
  be discarded. Under `gated` governance the import's own status rules apply unchanged.
- **Workbench.** The Candidates section lists with a status filter; the detail is the draft as
  an editable body with the closest procedure beside it, Add to library and Discard with a
  reason. The editor-tab prompt is deleted.

## Alternatives rejected

- **A substrate corpus with a draft status**: recallable before review, and a second governance
  model beside the procedures'.
- **Keeping the draft only in the editor**: no listing, no decision record, nothing survives a
  closed tab.

## Consequences

- `BackendConfig.Candidates`; nil leaves the routes a clean 503. The knowledge layer is still
  optional in v0.
