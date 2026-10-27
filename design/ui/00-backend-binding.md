# 00 — Backend Binding

How the UI package in this directory binds to the reckon engine. **This sheet is the
authority layer over the package**: where a design sheet and this binding disagree, the
binding wins, because the engine's data model wins in all respects in the UI. The UI
introduces no state of record, no vocabulary, no identity computation, and no state machine
of its own — it renders engine truth and routes intents to engine commands.

Context: the package was produced from a standalone HTML prototype that necessarily faked a
backend. Its file-centric persistence model (`.inv.md` as source of truth, client-side
stores, watchers, client-computed identity) was prototype scaffolding, and this sheet
replaces it. The prototype itself has been removed — the sheets are the distillation and
stand alone: `01` is the visual contract, `02`–`07` the interaction spec, and the *data
architecture* is the engine's.

Seam rules for this subtree (they bind every sheet here):

- `design/13-workbench.md` stays authoritative for *what exists* — the surface inventory and
  phasing (`13 §4`), the workbench discipline (`13 §3`). Sheets here own how surfaces look
  and behave. A surface not in `13 §4`'s inventory is a scope change to make there first.
- Cross-references point out of this subtree (engine specs, endpoints), never into it.
- Every rendered element names its data source. An element with no serving endpoint or spec
  section is a flagged gap (§6), not an implication that one exists.

---

## 1. Authority: what replaces the prototype's persistence model

| Prototype construct | Product binding |
|---|---|
| `.inv.md` on disk as the record ("invariant I1") | The **event-sourced investigation aggregate** is the record (`01`, `02`): the Postgres event log, with unforgeable per-event actors (JWT-derived principal, issuer-stamped `delegate_kind`). The "Enhanced file view" becomes the **investigation document** — a webview rendering backend state (`GET /api/investigations/{id}`, `/thread`, `/hypotheses`, `/actions`). The portable, GitHub-renderable markdown artifact survives as an **export**: a projection the backend renders on demand (generalizing the `design/07` export path). "Paste into a ticket unedited" is an export action, not a file on disk. |
| `InvestigationStore` (client-side mutate, YAML AST merge, byte-preserving appends, atomic writes, debounce) | Deleted. The backend is the single writer of record. The client keeps a per-investigation **cache** of backend state with a monotonic sequence. The store's *fan-out shape* survives — one change event re-renders tree, status bar, and webviews — with the source swapped from file mutations to backend deltas. |
| File watcher, reconcile-on-external-edit, self-write suppression, hydrate-the-workspace | Deleted. No workspace files (`13 §3`: an investigation is not a directory — or a file). The tree lists investigations from `GET /api/investigations`. |
| Client-enforced state machines gating mutations | **Display knowledge only.** The aggregate command handler is the arbiter (approver invariant, AI write-protection, optimistic concurrency — enforced at one layer so no path bypasses them). The UI uses transition tables to decide which affordances to show; engine rejections surface verbatim, which is already the sheets' own rule ("the UI surfaces the *reason*, never a bare invalid"). |
| Client-computed entity identity (fixed-namespace UUIDv5, TS canonicalization) | Deleted. Identity is tenant-scoped UUIDv5 minted by the engine's identity resolver — the single arbiter (`03 §7`). Ids reach the UI as opaque `<type>--<uuid>` strings. The entity popover's "Deterministic ID" row displays the engine's id. |
| `POST /investigations/:id/turns` → SSE from the backend | The agent loop is **client-side by architectural commitment** (`05 §2.7`: BYOK model key never crosses to the backend; every loop call carries the AI-delegate token). Turns run through the stdio sidecar (`implementation/agent-sidecar.md`); progress arrives as `turn/*` notifications. Projection changes (action state, thread growth from other sessions) arrive on the `/api/stream` WebSocket. See §2.6. |
| Validation code catalog (`E1xx`) | The *pattern* (named, specific, analyst-readable refusal reasons) is engine behavior already; the catalog re-derives from engine rules (expiry, approver invariants, tier gates, conclude preconditions). File-integrity codes die with the file. |
| Scenario fixtures (the prototype's scripted investigation) | The reference scenario survives in the sheets (`05 §5.1` worked example, `03 §3.4` canonical plan); bind walkthroughs and demos to the shipped OCSF fixture scenario (`fixtures/lateral-movement-via-rdp/`, `03 §9`) so design review and the eval harness run the same story. |

What carries forward from the package's engineering sheet unchanged, because it matches the
built workbench discipline: the webview-is-a-renderer rule, the typed `ui.*` intent union,
sequence-checked messages with snapshot resync, size-bounded messages with raw payloads
fetched on demand, and the designed-degradation table (with its file rows deleted and its
transport rows rebound to the version-handshake and sidecar-respawn behaviors that already
exist).

---

## 2. Vocabulary and field bindings

Analysts learn state words as *meaning* (the design system says so itself). There is exactly
one vocabulary — the engine's — because the engine's words are what the audit record stores.

### 2.1 Investigation fields (the "frontmatter card" data)

| Design field | Engine source |
|---|---|
| `id` | Aggregate UUID (a display alias may exist; the id of record is the UUID) |
| `title`, `status`, timestamps | `GET /api/investigations/{id}` (`investigation_current`) |
| `verdict`, `rationale`, `verdict_at` | Verdict act — gap §6.3 |
| `seed.{type,source,id}` | The Seed extension (`01`). Alert-seeded ↔ detection-finding-rooted; hypothesis-seeded ↔ hunt |
| `entities[]` | STIX objects in the investigation's grouping. Roles (`subject/suspicious/related`) are presentation, derivable from seed + sightings |
| `evidence[]` (pins) | Pin act — gap §6.2 |
| `remediation.actions[]` | `GET /api/investigations/{id}/actions` (`action_current`) |
| `external_work[]`, `comms.*` | Phase F (§4) |
| `conclusion_ref` | The export artifact (§6.5) |
| `schema` | The `/status` `api_version` handshake (built; fails closed) |

### 2.2 Coverage (tool-call pills) — engine words, verbatim

| Design | Engine (`03 §6.1`) |
|---|---|
| `FULL` | `COMPLETE` |
| `PARTIAL` | `PARTIAL` |
| `NOT_AVAILABLE` / `NOT_CONFIGURED` | `UNAVAILABLE_TENANT` |
| — | `UNAVAILABLE_TRANSIENT` (configured but unhealthy — a distinction the sheets want and get for free) |
| `ERROR` | `FAILED` |

The package's rule "no data is information, not failure" is the engine's
`COMPLETE`-with-zero-events semantic: evidence of absence. Keep the rule; use the words.

### 2.3 Action status

| Design | Engine (`04 §3.1`) |
|---|---|
| `PENDING` / `AWAITING_APPROVAL` | `REQUESTED` (presentation labels `PENDING_MANUAL` / `PENDING_TWO_PARTY`) |
| `AWAITING_SECOND` | `PENDING_SECONDARY` |
| `RUNNING` | `EXECUTING` |
| `SUCCEEDED / FAILED / REJECTED / EXPIRED / REVERSED` | identical |
| `WAIVED` | No engine state; nearest truth is `REJECTED` with a waive-flavored rationale. Open question §7. |

**Retry binds to a new action, never the same id.** The dispatch ledger (`08 §6b`)
guarantees one x-action id never dispatches twice, and the write path is no-fall-through
(a partial state change must not be re-attempted). "Retry" = request a new action with the
same descriptor/targets carrying `retry_of` lineage (§6.6); the card renders the chain.

**Card states the sheets must add** (engine truths with no design yet):

- `PARTIAL` — multi-target honest residual (`08 §6c`: failure never infers success;
  unresolved targets are `UNKNOWN`). Needs a per-target outcome list.
- **Reversal-attempted** — BEST_EFFORT reversibility (`04 §7.1`): the original stays
  `SUCCEEDED` with `reversal_attempted_by_ref`; never a claimed undo. Distinct from
  `REVERSED`. (`ioc.block` is the canonical BEST_EFFORT case.)
- **Expiring / expired** — approval windows are frozen at request time and lazily enforced:
  the status may still read `REQUESTED` while the engine refuses the approve. Built interim:
  `EXPIRED` badge, zero affordances, "re-request if still needed." The card needs the
  designed version, and the summary bar should surface nearest-expiry.
- **Blast-radius escalation** — a T2 action over the target threshold becomes T3
  (non-negotiable, `04 §1`); the card should say *why* ("escalated: N targets").

### 2.4 Lifecycle

Engine states: `DRAFT / ACTIVE / PAUSED / CONCLUDED / ARCHIVED`. The sheets'
`VERDICT_REACHED` and `REMEDIATING` bind as **derived presentation states** — ACTIVE with a
recorded verdict act; ACTIVE with non-terminal actions — the same derived-flag pattern as
lazy expiry, so every surface stays truthful without new aggregate states. The package's
central thesis survives whole: *verdict is the midpoint; the investigation stays open until
every action is terminal.* Verdict preconditions (evidence required) and conclude
preconditions (all actions terminal) are enforced engine-side (§6.3).

### 2.5 Tiers and approval

- **T3 = typed challenge** (`04 §5.5`). Two-party is an *authorization mode*
  (`TWO_PARTY` → `PENDING_SECONDARY`) assigned by Gate 2 policy — orthogonal to tier and
  phased per `13 §4`. The second-approver strip renders on mode, whatever the tier.
- The sheets' typed-challenge phrasing — type the action summary to confirm — is adopted
  (better than a generic challenge input; compatible with `challenge_response`).
- "Auto-approved by policy" badge ↔ `AUTO_POLICY` + `policy_ref`/`policy_version`, already
  recorded on the approval.
- Batch approval ("Approve all T2") = N individual approvals fanned out, each its own audit
  event and card. There is no cross-action execution ordering (dispatches are independent
  workflows); the sheets drop the "watch containment land in order" sequencing claim.
- The full T2/T3 write path (request → Gate 2 → typed challenge → durable dispatch → honest
  result → reversal) is live engine + workbench behavior; the sheets bind to it directly.

### 2.6 Streaming

| Design event | Real channel |
|---|---|
| `text.delta` | `turn/text_delta` (built; word-batching is a render choice over real deltas — never simulate pacing) |
| `tool.status` / `tool.result` | `turn/tool_call` / `turn/tool_result`, with engine-distilled coverage + event count |
| `step.begin/end` | Per-round markers — gap §6.4 |
| `file.append` | Derived client-side: document and export render from the same events |
| `action.state` | `/api/stream` WebSocket projection deltas |
| `confidence` | Replaced — see §5 |
| `comms.event` | Phase F (§4) |

The `calling → received → normalizing → done` status choreography is deleted: the capability
layer's invoke+normalize is one span, and animating stages that did not happen violates the
package's own honesty rule. Render real stages only.

### 2.7 Hypotheses

Engine statuses, verbatim: `PROPOSED / OPEN / SUPPORTED / REFUTED / INCONCLUSIVE /
ABANDONED` — and **predictions**: falsifiable test conditions with per-prediction status and
test-result refs (`01`; served by `/hypotheses`). The hypothesis card grows a prediction
sublist; the flat open/supported/refuted list in the sheets is superseded. Hypotheses carry
a `handle` and predictions a `discriminates` list (`01 §x-hypothesis` "Competing
hypotheses"); the tracker renders them as a tabbed scoreboard (`02 §2.9`), one working
hypothesis at a time with the whole set always in view.

### 2.8 Entities

- Type mapping: `file-hash` → STIX `file` (hashes are properties, not a type); `alert` →
  detection finding (Indicator + Sighting, `03 §4.12`).
- **Process identity is the instance, not the image name**: `(host, pid, created_time@1s)`,
  a deliberate STIX deviation (`03 §7`). Process chips render host-qualified; the image name
  is a pivot query, not an identity. (Basename identity would collapse every `powershell.exe`
  everywhere into one entity and make cross-investigation appearance counts meaningless.)
- Aliasing is linked, never merged — matches the engine's alias edges exactly, including the
  popover copy ("not surveillance, just join keys").
- Cross-investigation appearances are a backend query — gap §6.1.

---

## 3. Adopted from the package (supersedes current workbench rendering)

1. **The tool-call block as the trust primitive** — collapsed one-liner → query / normalized
   summary / raw JSON, coverage pill, pin action, independent fan-out resolution. Replaces
   the current disclosure rows.
2. **The entity chip system** — fixed per-type palette everywhere, mono-for-data, chip →
   popover → pivot *stages a question in the composer* rather than firing.
3. **The density and token system** ("Bloomberg, not Apple") with the VS Code
   theme-variable derivation strategy.
4. **Designed degradation and empty states** — normal states with a designed appearance,
   never a stack trace.
5. **The seed picker** — an investigation always begins from something concrete
   (entity-rooted seeds, `01`); never an empty chat. Hypothesis seeds are in scope (hunts).
6. **Streaming discipline** — p50-to-first-token as an acceptance criterion; never block on
   the slowest tool; failures are non-blocking.
7. **Verdict-as-midpoint** — as derived states per §2.4.

---

## 4. Comms, follow-up, escalation — Phase F, on the action layer

Built (first slice). An outbound message is a **T2 action** (`notify.*` descriptors +
the fixture comms write adapter) — there is no T1 external action (`04 §1` names chat
explicitly; tier is calibrated to harm, and low friction comes from the approval surface
or an auto-approve policy, never a lower tier). The mandatory pre-send preview *is* that
light-weight approval surface: the approval card renders the exact frozen message body,
and Approve = Send. A SUCCEEDED `notify.*` result opens (or, via `thread_ref`, extends)
a comms thread (`comms/` — CRUD + thin history; the audit record of every send is the
action event log). Inbound replies land through `POST /api/comms/inbound` (the v0
ingestion seam; vendor webhooks arrive with their adapters); acknowledge / mark-done /
snooze are analyst acts on the thread. Follow-up dueness and the stale escalation policy
(`external-work-stale-72h`) are DERIVED at read time — policies surface prompts and never
auto-fire; a durable Temporal nudge (the expiry-emitter pattern) can layer on later
without schema change, since nothing engine-side gates on dueness. Open threads are a
conclude-gate *input* (decision support in the conclude dialog) at v0; the engine-side
gate lands when Phase F completes. Every card and flow in the comms sheet carries
forward; only the tier vocabulary moved (the engine wins, §0).

---

## 5. Replaced elements

- **The confidence meter (percentage bar)** — no calibrated percentage exists to display;
  an invented one is fabricated precision. Replaced by coverage-derived categorical
  statements ("2 of 2 applicable tools answered · COMPLETE · 1 not configured") plus, where
  a confidence word is wanted, the interpretation's committed `HIGH/MEDIUM/LOW`.
- **Staged tool-status animation** — see §2.6.
- **Model pickers as hardcoded enums** — model choice is agent configuration, not a
  package.json enum.
- **Client-side identity, stores, watchers, file machinery** — see §1.

---

## 6. Engine gaps this package motivates (build ledger)

Built (endpoint + surface live):

1. **Evidence pinning** — `evidence-pin` acts, `GET .../pins`, un-pin as supersession.
2. **Verdict act** — `verdict` acts with the pin gate and the conclude gate; AI verdicts
   behind the default-off `trust.ai_verdict` dial (the enabling ref rides the event).
3. **Raw evidence in reach** — eager promotion at ingest (`ocsf_events` + `stix_objects`
   persistence in the invoke path), `GET /api/evidence/{ref}`, read-only editor tabs
   (superseded as the surface by the inspector, `ui/08 §8.6`; the endpoint stays).
4. **Cross-investigation appearances** — the `ref_appearances` fold (seeds + citations +
   action targets), `GET /api/entities/{ref}/appearances`.
5. **Real seeds** — the Seed extension on `CreateInvestigation` (alert / entity /
   question), the projected `seed_summary` triage line, the workbench seed picker.
6. **`retry_of` lineage** — command → event → projection → view → the agent's
   `request_action` parameter; the card renders the chain.
7. **Per-round step markers** — `turn/step` sidecar notifications (additive).
8. **Transcript-open** — `GET /api/interpretations/{id}/transcript` from the
   content-addressed side store.
9. **`consulted_*` provenance** — on the command/event/thread; the agent loop attaches its
   `recall_sops` retrievals per turn, `used` decided conservatively (only when the model's
   own final text references the SOP — the transcript is deliberately not searched, since
   it contains the retrieval results themselves).

10. **Live markdown export** — `GET /api/investigations/{id}/export.md`, the `design/07`
    export generalized from post-conclusion to any-time; the package's file schema is the
    format, rebound to engine vocabulary. The signed bundle stays the finalized artifact.
11. **Phase F comms, first slice** — `notify.*` T2 descriptors + fixture comms adapter,
    the comms-thread store, inbound ingestion, follow-up/escalation derivation, and the
    workbench surfaces (§4). Remaining for full Phase F: real vendor comms adapters,
    vendor webhook ingestion, the engine-side conclude gate, durable follow-up nudges.

Motivated by the investigation layout (`ui/08`, `adr/0079`), not yet built:

12. **Per-class event projections** — for each OCSF class the normalizers handle
    (`03 §4.1`–`§4.12`), the curated field list and a one-sentence rendering the inspector
    and the Story timeline show, plus a short title per prediction. Display knowledge, so
    it may live client-side keyed by `class_uid`; the short title is recorded with the
    prediction, never generated at render time. **Landed** (`adr/0090`): server-side, in
    `capability/ocsfview`, with the entity slots carrying the normalizer's ids; the title is
    a field of `x-prediction`, derived once by the aggregate when not authored.
13. **Entity-scoped event query** — the cited and retrieved events for one entity within
    one investigation, ordered by event time, for the inspector's "around it" strip and
    the Story timeline. A read over persisted `ocsf_events` (§6.3), not a new capability
    call. **Landed** (`adr/0092`): membership is the capability call, recorded at ingest;
    `GET /api/investigations/{id}/events` with `entity`, `category`, `around`.
14. **Entity baselines** — where a capability answer establishes normal behaviour (the
    reference scenario's nightly logon baseline), a summary the inspector and timeline can
    render. Shown only when the record holds one; never inferred by the UI. **Landed**
    (`adr/0095`): a counting fact folded from the investigation's retrieved events,
    `GET /api/investigations/{id}/entities/{ref}/baseline` or 204.
15. **Procedure excerpt provenance** — at import, record a location per chunk (page,
    heading path, or anchor, whatever the format yields) and retain the original file or a
    snapshot of the URL, so a citation names "where in the source" and "open original"
    shows what existed at retrieval time (`06 §2.4`, `§6.2`). **Landed** (`adr/0097`): the
    `06 §6.2` side store keyed by excerpt hash with revision and heading-path location; the
    original retained with the revision; `GET /api/sops/{id}/revisions[/{hash}]`,
    `…/original?rev=`, `GET /api/investigations/{id}/retrievals?sop=`.
16. **Response-policy status on requested actions** — whether the tenant's Gate 2 policies
    would auto-approve the action, and where the approval routes when they would not,
    readable on the requested action (`04 §4`). **Landed** (`adr/0099`): `action_current.policy`
    folded from `action.policy_evaluated`, served as `ActionView.policy`.
17. **Owner action items** — work that only an owner outside reckon can do, tracked to
    confirmation with the investigation and counted by the conclude preflight. Open: an
    `x-action` with an external-work descriptor (`00 §4`'s external work) or its own record.
    **Landed** (`adr/0101`): its own record (`owner_items`), attached to the notification that
    asks for it, SENT on that notification's success; `GET/POST
    /api/investigations/{id}/owner_items`, `POST /api/owner_items/{id}/confirm|waive|attach`.
18. **Narrative summary at any time** — the two-tier summary of `06 §3.2` produced on
    demand for an open investigation, not only at conclusion, with its authorship (drafted,
    edited by) recorded. **Landed** (`adr/0103`): `GET /api/investigations/{id}/summary`,
    `PUT …/narrative` as revisions, the sidecar's `draftNarrative`.
19. **Roles on acts** — the role that authorized an act, captured from the token at command
    time alongside `principal_name` (`adr/0077`), for the person inspector (`ui/08 §8.6.1`).
    Display only: authorization still reads the live token, and reckon still mirrors no
    roles. **Landed** (`adr/0105`): `Actor.roles` on every act, `ThreadActorView.roles`,
    the approver's roles on `GET /api/actions/{id}`.
20. **Addressable capability calls** — a stable id per capability call within a committed
    turn record, resolvable by an endpoint, so the bundle, the Story timeline and the
    residual panel can open the call that produced their evidence (`ui/08 §8.6.1`).
    **Landed** (`adr/0092`): `call_id` in the envelope and on the turn's tool-call row;
    `GET /api/investigations/{id}/calls[/{call_id}]`.

Motivated by the knowledge surfaces (`ui/09`, `adr/0084`), not yet built:

21. **Listing concluded-case summaries** — browse the past-case corpus without a query, with
    filters on technique, disposition and conclusion date (`ui/09 §9.4`). Recall requires a
    query today; the procedure facade already owns an empty-query browse, and past cases
    need the same. **Landed** (`adr/0107`): `knowledge.ListSummaries`,
    `GET /api/knowledge/case_summaries?technique=&disposition=&from=&to=&limit=`.
22. **Citation counts and where-used** — for a procedure or a past case, the investigations
    that consulted it and whether each applied it, from the `consulted_*` provenance on
    interpretations (`00 §6.9`), as a fold across investigations (`ui/09 §9.3`).
23. **Ask outside an investigation** — an agent session not bound to an investigation,
    whose only tools are the two recall calls, which answers with citations and records
    nothing in any investigation (`ui/09 §9.2`). Same client-side loop and model key as
    investigation turns (`05 §3.4`).
24. **Stored procedure candidates** — the conclude hook's draft (`proposeSOP`) is kept as a
    candidate record (draft text, source investigation, drafted at, status, discard reason)
    instead of being shown once at conclude, so it can be reviewed later in the Knowledge
    view (`ui/09 §9.7`). Accepting still imports through the existing path.

---

## 7. Open questions

Deliberate non-decisions, tracked here per house convention:

- **`WAIVED`**: is "decided not to do, without rejecting the idea" worth a first-class
  engine status, or does rejection-with-rationale cover it?
- **Layout floor**: `ui/08 §8.1` sets the collapse points (queue to a rail below 1280px,
  companion to a drawer below 1024px); the minimum width at which a lens stays readable
  with the inspector open is still undefined.
- **Auth surface**: sign-in/out state, the 401-refresh appearance, and the welcome view's
  sign-in affordance need design — the extension's dual-client PKCE flow is built and
  load-bearing.
- **Mid-turn steering**: a multi-round agent turn is currently atomic (watch or cancel).
  A queued interjection the loop reads between tool rounds ("skip DNS, chase the service
  account") would change the feel from batch job to colleague — needs sidecar/agent
  design, not just UI.

(Resolved by `ui/08` and `adr/0079`: **panel vs document** — the conversation is a
companion column and the document is the reading surface, organized as four lenses;
**structural navigation** — the lenses are Summary, Story (event time), Reasoning (by
hypothesis) and Response, with entities reached through the inspector; **two clocks** —
the Story lens is event time, with investigation time marked once where the
investigation began.)

(Resolved: naming. The package's original product name was a prototype-era simplification;
the product is **reckon** — command ids, config keys, and copy use the `reckon.` prefix
throughout, applied across every sheet. Resolved: the prototype itself was removed once the
sheets stood alone as the distillation.)

---

*Revision path for the sheets: delete the file-authority machinery (02 §2.1 raw/byte-accuracy
duties, 05 §5.1/5.6, impl-spec 01 §§1.1–1.2/1.8–1.9), rebind data sources per §2, adopt
engine vocabularies verbatim, add the four missing card states (§2.3), and cite each element
to its engine spec (`03 §6.1`, `04 §3/§5/§7`, `08 §6`, `01`). The visual system (01) and the
surface map (06) carry forward nearly untouched.*
