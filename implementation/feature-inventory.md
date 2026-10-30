# Feature inventory — the investigation-layout redesign

What the investigation-layout redesign (`implementation/workbench-react.md §10`, `design/ui/08`,
`design/ui/09`) added, stated as **what an analyst can now do**, not as screens. A screen is
where a capability is delivered; it is listed separately (§3) and is not itself a feature.

The **Decision** column records whether a capability ships: **keep** · **defer** (its branch
stays, unmerged, until it is decided again) · **cut**. Decisions recorded 2026-10-27. From here
on a capability enters this list by decision, not as a byproduct of a mockup.

Line counts are added lines, `wb` = `workbench/`, `go` = Go + migrations outside it (docs and
ADRs excluded).

## 1. Capabilities the analyst already had (reference, no decision)

As of `main` before the redesign (`46fc148`), in the workbench unless noted.

**Access and setup**
- Sign in and out (PKCE against the IdP); store their own model key in the OS keychain; check
  the backend connection.
- Apply or revert the workbench posture that trims the editor to an investigation profile.

**Intake**
- See the case system's queue, sorted by age or by priority then age, and open a case.
- Start an investigation from an alert, an entity or a case (with case search); list their
  investigations; open and rename one.

**Investigating with the agent**
- Work with the agent turn by turn, streamed, with every tool call and its result visible.
- Have the agent query telemetry through the capability layer, see which capabilities are
  available or degraded, and cite an absence when a complete search finds nothing.
- Run a scoped side turn (propose tests, ⚡ Test one prediction) without derailing the main
  thread.
- Reset the agent's context, rebuilding it from the investigation record.
- Enable a missing integration from where the gap shows up: a form derived from its schema,
  secrets captured out of band, the change recorded.

**Reasoning**
- Keep competing hypotheses: propose, acknowledge (restating while untested), revise, abandon,
  decide supported or refuted.
- Attach predictions that discriminate between hypotheses; propose and run their tests; record a
  result, including one a person attests to; withdraw an untested one.
- Record a verdict; supersede an earlier interpretation.
- Read the reasoning thread: every act with who did it (person, or the agent on their behalf)
  and what it cites; open any turn's full transcript.

**Evidence**
- Open any cited evidence as raw OCSF; see an entity's other appearances across investigations;
  pin evidence; see the cited events in time and a minimal evidence graph.

**Knowledge**
- See the procedures and similar past cases retrieved for this investigation with their
  relevance; choose which the agent may use; see on each act what was consulted.
- Import procedures from markdown (workbench panel, or `reckon knowledge import`).

**Response**
- Review actions the agent requests (each grounded in evidence): approve (T2 confirm, T3 typed
  challenge), reject, re-request an expired one; policy auto-approval where a policy covers it;
  dispatch and per-target results; reverse a completed action.
- Notify people through notification actions; follow the thread with replies; acknowledge, mark
  done, snooze, follow up.

**Closing**
- Pause, resume, conclude; post a note to the case; export the investigation as markdown.
- On conclude: the case summary enters the knowledge base (with a narrative drafted from the
  conversation) and a drafted procedure is offered for the library.

**Operator (CLI)**
- Seed, reset or switch the demo scenario; import procedures in bulk.

The redesign **re-presents** these: the same capabilities, delivered by new screens (§3). That
is a layout decision, reviewed as UX and architecture, not a feature to keep or cut.

## 2. New capabilities

### Running an investigation

| # | The analyst can… | How | Engine | PR / ADR | Decision |
|---|---|---|---|---|---|
| C1 | **see where each case stands and what to do next** — the stage (investigate · verdict · contain · notify · close), the one next step, the nearest approval about to expire; cases needing them first | derived client-side from the record | list/detail view fields, no ADR | #88 | **keep** |
| C2 | **read raw telemetry without reading JSON** — each event as one sentence with its entities linked; encoded commands decoded beside the original | `capability/ocsfview` renders per OCSF class | `view` on evidence reads | #91 · adr/0090 · wb 1,297 / go 1,375 | **keep** |
| C3 | **audit what the agent actually queried** — every call's verb, the tool that answered, the parameters as sent, coverage, which bindings failed; every event any call returned, not only the cited ones; which call produced a piece of evidence | calls recorded as their own rows at invoke time | `capability_calls`, `call_events` tables; events endpoint | #94 · adr/0092 · wb 980 / go 992 | **keep** |
| C4 | **see the attack's sequence across entities** — a timeline of the case's events and the path between hosts and accounts | derived from C3's events | — | #94 | **keep** |
| C5 | **tell normal from abnormal for an entity** — "N logons over M days, usually around 09:00" and how this case deviates, only when the retrieved record shows a pattern | pure fold over retrieved events | baseline endpoint | #96 · adr/0095 · wb 859 / go 437 · needs C3 | **keep** |
| C6 | **know why an action needs them** — no policy covers it, a policy requires a second approver from a named pool, or a policy auto-approved it; a shadow policy that would have | Gate 2's evaluation folded onto the action | `action_current.policy` | #100 · adr/0099 · wb 67 / go 149 | **defer** — a refinement of approving actions, not its own capability |
| C7 | **see an action's full history** — every state change with who and when, how authorization was met, per-target results, reversal lineage | read from the action's own events | `GET /api/actions/{id}`, no ADR | #87 | **not a capability** — the inspector's read of an action's existing events; ships with the inspector |
| C8 | **track asks to other teams to completion** — "identity team: rotate the credential" sent through a notification, marked sent only when the send succeeds, closed by the analyst with the owner's confirmation or waived with a reason; the agent may propose one; open asks block conclude | owner items, a CRUD record | `owner_items` table, endpoints, agent tool `propose_owner_item` | #102 · adr/0101 · wb 368 / go 713 | **cut** — `ticket.create` already hands work to another team; tracking it to completion is reading ticket status back (Phase F), not a parallel record |
| C9 | **hand a case off mid-flight** — a summary of where it stands at any state (not only concluded), an AI-drafted narrative they can edit with authorship shown, a handoff note to paste | summary facts + narrative revisions | `summary/` package, `investigation_narratives`, sidecar `draftNarrative` | #104 · adr/0103 · wb 377 / go 1,039 · counts C8 | **defer** — no handoff support at this time |
| C10 | **see under what authority each act was done** — the roles the actor held when they acted | roles captured from the token at command time, display only | `Actor.Roles` | #106 · adr/0105 · wb 87 / go 29 | **keep** |
| C11 | **search the knowledge base mid-investigation** and offer hits to the agent (excluded until they include them) | sidecar `searchKnowledge` | — | #88 | **keep** |
| C12 | **see changes by others as they happen** — another analyst's approval, a dispatch result — without reloading | websocket per open document | none: the stream was removed (adr/0222) | #88 | **defer** — the host reloads after its own commands and keeps the dispatch poll |

### Managing what the organization knows

| # | The analyst can… | How | Engine | PR / ADR | Decision |
|---|---|---|---|---|---|
| C13 | **maintain the procedure library in the product** — edit (each save a revision), retire with a reason, see versions | in-product editor | retire reason, list fields, no ADR | #89 · wb 2,475 / go 263 | **defer** — knowledge management is secondary; import from markdown and use, which exists, is enough |
| C14 | **check an import before writing it** — parsed metadata, missing fields, whether it would create or revise | dry-run import | `import_markdown?dry_run=true`, no ADR | #89 | **defer** — knowledge management is secondary; import from markdown and use, which exists, is enough |
| C15 | **verify exactly what procedure text the agent relied on** — the excerpt, where it sits in the document, which revision; the original imported file | excerpts stored at retrieval | `knowledge_retrievals` tables, revision + original endpoints | #98 · adr/0097 · wb 598 / go 749 | **defer** — knowledge management is secondary; import from markdown and use, which exists, is enough |
| C16 | **browse past cases** by technique, disposition and date | a listing over the case-summary corpus | `GET /api/knowledge/case_summaries` | #108 · adr/0107 · wb 210 / go 216 | **defer** — knowledge management is secondary; import from markdown and use, which exists, is enough |
| C17 | **see which knowledge is actually used** — how many investigations cited each procedure, whether it was applied, which procedures a case taught | citations projected from investigation events | `knowledge_citations` | #110 · adr/0109 · wb 206 / go 344 | **defer** — knowledge management is secondary; import from markdown and use, which exists, is enough |
| C18 | **ask the knowledge base a question** outside any investigation, answered only from it, with checked citations | separate agent loop, no tools but recall | `agent.Ask`, second governed prompt, sidecar `askKnowledge` | #113 · adr/0111 · wb 95 / go 467 · prompt not yet eval-accepted | **defer** — knowledge management is secondary; import from markdown and use, which exists, is enough |
| C19 | **review drafted procedures as a queue** instead of a prompt at conclude — with the closest existing procedure, accept (edited) or discard with a reason | candidates as a stored record | `procedure_candidates` | #114 · adr/0112 · wb 339 / go 570 · **changes** an existing capability | **defer** — knowledge management is secondary; import from markdown and use, which exists, is enough |

## 3. Screens — where capabilities are delivered (UX review, not keep/cut)

| Screen | Delivers | PR |
|---|---|---|
| Header (stage, primary action) | C1 | #88 |
| Left region: work queue, Work \| Knowledge toggle | C1, navigation | #88, #89 |
| Lenses: Reasoning, Response, Story, Summary | existing capabilities re-presented; C4, C6, C8, C9 | #88 + gap PRs |
| Companion: conversation, thread, Knowledge section | existing capabilities re-presented; C11 | #88 |
| Inspector (one pane for every cited object) | existing "see the evidence", re-presented; C3, C5, C7, C10, C15, C17 | #87 + gap PRs |
| Knowledge document: Procedures, Import, Search, Past cases, Ask, Candidates | C13–C19 | #89 + gap PRs |

## 4. Architecture — reviewed rather than kept or cut

| # | Change | PR | Size | What to review |
|---|---|---|---|---|
| A1 | Webview moved to React, bundled by esbuild; old inline HTML/JS deleted | #86 (main) | wb 8,988 | component split, `store.ts` (343 lines), `app.tsx` |
| A2 | Typed host↔webview contract `src/protocol.ts` (382 lines) | #86 | — | one coherent contract or a pile of messages |
| A3 | Transport seam: `webview/src/transport.ts` with a VS Code and a harness implementation | #86 | — | do both honour the same contract |
| A4 | Browser harness over recorded JSON snapshots + vitest component tests | #86, grown every PR | — | does it test behaviour or markup |
| A5 | Pure derivations in `webview/src/derive/` | #86–#114 | 17 files | do components re-derive |
| A6 | Host side: `backend.ts` (2,278 lines), `investigationDocument.ts` (1,559), `knowledgeDocument.ts` (610) | all | — | the largest UI files; likely mixed concerns |

## 5. Dependencies that constrain decisions

- The stack is linear: deferring a PR in the middle means re-stacking the ones above it onto its
  base.
- C4 and C5 read the events C3 records. C9's summary counts C8's asks (none if C8 is cut).
- C13–C19 live in the Knowledge document. C19 removes the conclude-time prompt, so cutting it
  means restoring that prompt or having no path from a concluded case to a new procedure.
- C1, C11, C12 and the re-presentation ship as one unit (#87 + #88): separable as hidden
  features, not as code removal, without real work.
- C18 adds a second governed prompt; keeping it means eval acceptance like the base prompt.
