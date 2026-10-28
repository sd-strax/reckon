# Feature inventory — the investigation-layout redesign

What the investigation-layout redesign (`implementation/workbench-react.md §10`, `design/ui/08`,
`design/ui/09`) built, one row per feature. Every row states what exists; the **Decision** column
records whether it ships: **keep** · **hold** (stays on its branch or ships hidden, decided later)
· **cut**. From here on a feature enters this list by decision, not as a byproduct of a mockup.
Line counts are added lines, `wb` = `workbench/`, `go` = Go + migrations outside it (docs and
ADRs excluded).

## 1. Baseline — before the redesign (reference, no decision)

The `design/13 §7` v0 slice: sign-in (PKCE) and BYOK key, investigation list and seed entry, the
conversation with streaming and tool dispatch, the reasoning thread (chronicle) with cited
evidence, hypotheses and the prediction tracker with ⚡ Test, inline T2/T3 approvals, enablement
widgets, raw evidence in an editor tab, a minimal graph, the knowledge rail with include toggles,
Import SOPs panel, conclude with summary narrative and the candidate-SOP prompt, cases tree.

## 2. Architecture — not features, reviewed rather than kept or cut

| # | Change | PR | Size | What to review |
|---|---|---|---|---|
| A1 | Webview moved to React, bundled by esbuild; old inline HTML/JS deleted | #86 (main) | wb 8,988 | component split, `store.ts` (343 lines), `app.tsx` |
| A2 | Typed host↔webview contract `src/protocol.ts` (382 lines) | #86 | — | is it one coherent contract or a pile of messages |
| A3 | Transport seam: `webview/src/transport.ts` with a real (VS Code) and a harness implementation | #86 | — | do both implementations honour the same contract |
| A4 | Browser harness over recorded JSON snapshots + vitest component tests | #86, grown every PR | — | does it test behaviour or markup |
| A5 | Pure derivations in `webview/src/derive/` (stage, lens, turns, matrix, summary, attack path, baseline, …) | #86–#114 | 17 files | good sign; check components don't re-derive |
| A6 | Host side: `backend.ts` (2,278 lines), `investigationDocument.ts` (1,559), `knowledgeDocument.ts` (610) | all | — | the two biggest files in the UI; likely mixed concerns |
| A7 | Live updates: `/api/stream` websocket per open panel, refreshing the slices an event touches | #88 | — | backend endpoint added inside a UI PR, no ADR |

## 3. Investigation document — features

| # | Feature | What the analyst gets | Backend it needs | PR / ADR | Decision |
|---|---|---|---|---|---|
| I1 | **Inspector** | Every citation opens one side pane: back/breadcrumb, pin (2 panes max), "In this case", typed tabs per object (evidence, entity, absence, detection, attestation, prediction, hypothesis, action, notification thread, similar case, turn, person, alias, procedure). Replaces the editor-tab evidence view and the entity popover. | `GET /api/actions/{id}` (full action record), `related` on indicator evidence, `GET …/case_summaries/{ref}` — no ADR (plan §8.0) | #87 · wb 2,408 / go 952 | |
| I2 | **Header with stage** | Title/rename, case ref, verdict pill, 5-step stage (Investigate · Verdict · Contain · Notify · Close), one primary action per stage, nearest approval expiry | list/detail view additions (`has_verdict`, `open_actions`, …) | #88 | |
| I3 | **Work queue (left region)** | My open investigations needs-me-first; case-system queue with Start investigation; capability fold | as above | #88 | |
| I4 | **Four lenses** | Reasoning (verdict basis, one-line prediction rows, Cards/Matrix ACH toggle), Response (action cards grouped by phase, notification draft + Preview & send), Story (cited events in time, evidence graph), Summary (tiles, key entities, still-to-do, export). Default lens follows the stage. | — | #88 · wb 2,425 / go 229 (I2–I6 together) | |
| I5 | **Companion column** | Conversation \| Thread modes; Knowledge section on top (include toggles, collapsed count line, scoped search, nudges); turns grouped with what each recorded, consulted chips, compact action cards, activity disclosure | sidecar `searchKnowledge` | #88 | |
| I6 | **Live refresh** | Panels update as events land instead of polling only | `/api/stream` (A7) | #88 | |
| I7 | **Event sentences** | Each OCSF event reads as one sentence with entity links (Story timeline, evidence Details, chips, export); decoded PowerShell `-EncodedCommand` beside the original; prediction short titles | `capability/ocsfview` package; `view` on `GET /api/evidence/{ref}` | #91 · adr/0090 · wb 1,297 / go 1,375 | |
| I8 | **Capability calls as records** | Story timeline of all retrieved events, "Around it", attack path, Call inspector (verb, binding, parameters as sent, coverage, attempts), "Produced by" links, bundle view | `capability_calls`, `call_events` tables; resolver records attempts; `call_id` on tool rows; events endpoint | #94 · adr/0092 · wb 980 / go 992 | |
| I9 | **Entity baselines** | Entity inspector "Normal behaviour" (N events over M days around HH:00) and "How this case deviates"; Story collapses repeated patterns | pure fold `ocsfview.Baseline`; `GET …/entities/{ref}/baseline` | #96 · adr/0095 · wb 859 / go 437 · needs I8 | |
| I10 | **Policy fact on action cards** | Card says why it needs you: "No policy pre-authorizes…", two-party with pool, "Auto-approved by policy…", shadow would-fire | `action_current.policy` projection column | #100 · adr/0099 · wb 67 / go 149 | |
| I11 | **Owner action items** | "Ask the identity team to rotate X": items with owner, sent through a notification, marked SENT on delivery, confirmed/waived by the analyst; agent can propose them; open count blocks conclude preflight | `owner_items` table (CRUD + history), endpoints, new agent tool `propose_owner_item`, hook in the action-result path | #102 · adr/0101 · wb 368 / go 713 | |
| I12 | **Summary + narrative + handoff** | Summary facts at any state (not only concluded); AI-drafted narrative with edit history and authorship line; copy-able handoff note; Conclude prefilled | `summary/` package, `investigation_narratives` table, `GET …/summary`, `PUT …/narrative`, sidecar `draftNarrative` | #104 · adr/0103 · wb 377 / go 1,039 · reads I11 | |
| I13 | **Roles on acts** | Person inspector shows the roles each act was performed under | `Actor.Roles` captured from the token, display only | #106 · adr/0105 · wb 87 / go 29 | |

## 4. Knowledge view — features

| # | Feature | What the analyst gets | Backend it needs | PR / ADR | Decision |
|---|---|---|---|---|---|
| K1 | **Knowledge document** | Its own panel with sections and counts; Work \| Knowledge toggle on both documents | — | #89 · wb 2,475 / go 263 (K1–K4 together) | |
| K2 | **Procedures** | List with filters, detail, in-product markdown editor (save = revision), retire with a reason, copy link | `DELETE /api/sops/{id}` takes a reason; list rows gain revision/retired fields; fix: edits kept import attribution — no ADR | #89 | |
| K3 | **Import with dry-run preview** | Old Import panel moved in, plus parsed-frontmatter preview and predicted outcome before writing | `import_markdown?dry_run=true` — no ADR | #89 | |
| K4 | **Search** | Both recalls with score, band, rationale; results open in the inspector | existing | #89 | |
| K5 | **Procedure provenance** | Procedure inspector shows the exact excerpt the agent read, where in the document, which revision; Versions tab; original imported file retained and downloadable | `knowledge_retrievals` + `_uses` tables, `SOPMatch` gains hash/location, revisions + original endpoints | #98 · adr/0097 · wb 598 / go 749 | |
| K6 | **Past cases** | Browse concluded cases, filter by technique, disposition, date | `knowledge.ListSummaries`, `GET /api/knowledge/case_summaries` | #108 · adr/0107 · wb 210 / go 216 | |
| K7 | **Citations / where used** | Procedures list "Cited" column, "Where used" tab; case inspector "Procedures learned from this case", "Drawn on by" | `knowledge_citations` projection over the aggregate | #110 · adr/0109 · wb 206 / go 344 | |
| K8 | **Ask** | Question → answer from the knowledge base only, with checked `[n]` citations, streamed; no investigation needed | `agent.Ask` loop, its own prompt ledger + golden, sidecar `askKnowledge` | #113 · adr/0111 · wb 95 / go 467 · prompt wording not yet eval-accepted | |
| K9 | **Procedure candidates** | Conclude stores a drafted SOP as a candidate (no prompt in the investigation); review queue in the Knowledge view with closest existing procedure; accept (edits allowed) or discard with reason | `procedure_candidates` table + endpoints; replaces the conclude-time editor-tab prompt | #114 · adr/0112 · wb 339 / go 570 | |

## 5. Dependencies that constrain decisions

- The stack is linear: holding a PR in the middle means re-stacking the ones above it onto its
  base.
- I9 (baselines) reads the events I8 records. I12's summary facts count I11's owner items (it
  degrades to none if I11 is cut).
- K5–K9 all live in the Knowledge document (K1). K9 removes the old conclude prompt, so cutting
  K9 means restoring that prompt or having no candidate path.
- I1–I6 are one merged unit on the stack (#87 + #88): separable as *hidden* features, not as
  code removal, without real work.
- K8 adds a second governed prompt; keeping it means it eventually needs eval acceptance like the
  base prompt.
