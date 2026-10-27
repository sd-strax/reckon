# The investigation webview in React — technical design

Status: **proposed** (for review before implementation). Decision record:
`adr/0079-investigation-layout.md`. Specification: `design/ui/08-investigation-layout.md`,
bound by `design/ui/00-backend-binding.md` (the authority layer) and the workbench discipline
(`design/13 §3`). Mockups: `design/ui/mockups/`. Parity inventory of the current webview:
`implementation/workbench-parity.md`.

This document fixes the engineering shape: the component tree, who owns which state, the
extension↔webview protocol, how the bundle is built and loaded, the browser harness, and the
engine API contracts the layout needs (`00 §6.12`–`§6.18`). It does not re-decide the design;
where the sheets leave something open it says what is decided here and why.

---

## 0. Constraints carried in

- **The engine is the record** (`00 §1`). The webview renders engine truth and routes intents to
  engine commands. The client keeps a cache with a monotonic sequence, never a second source of
  truth. Transition tables in the client decide which affordances to show; the aggregate decides
  what is legal, and its rejection reasons surface verbatim.
- **The webview is a renderer** (`00 §1`, impl-spec `00 §0.1`): it may be destroyed and rebuilt
  from a host snapshot with no semantic loss. View state (lens choice, scroll, drafts, disclosure
  state, inspector stack) lives in the webview; everything else arrives by message.
- **The agent loop stays client-side in the Go sidecar** (`implementation/agent-sidecar.md`).
  The extension host remains the sole auth owner and does every HTTP call. The webview has no
  network (`default-src 'none'`).
- **Every citation opens in the inspector** (`08 §8.6`). No editor tab, no separate panel. The
  editor stays in reach for analyst scratch work and for opening a retained SOP original, which
  is a document, not a citation.
- **Engine words render verbatim** (`00 §2`). Status, coverage, tier, prediction and action
  vocabulary are the engine's. A word the engine does not have is not shown (§9 lists the
  mockup words this rule removes).
- **Feature parity is mandatory.** Every behavior in `implementation/workbench-parity.md` is
  kept, relocated as §2 says, and ticked off against the harness and the live stack.
- **Public OSS posture.** Nothing here names buyers, tenants, team shape or dates.

---

## 1. Shape of the change

```
workbench/
  src/                         extension host (tsc → out/, commonjs; unchanged entry)
    extension.ts               registers commands, trees, the document host, the stream client
    investigationDocument.ts   InvestigationDocuments: panels, per-investigation cache, routing
    investigationCache.ts      the cache + sequence discipline (§3)
    streamClient.ts            /api/stream WebSocket subscription per open investigation (§3.3)
    protocol.ts                the typed message unions, shared with the webview (§4)
    backend.ts                 HTTP client — still no `vscode` import (the browser surface reuses it)
    agentTransport.ts, auth.ts, casesTree.ts, caseDocument.ts, importView.ts — unchanged
  webview/
    src/
      main.tsx                 bootstrap: picks the transport, mounts <App/>
      transport.ts             WebviewTransport (§6) + VsCodeTransport
      store.ts                 reducer: RenderMessage → InvestigationState; seq discipline
      derive/                  pure functions over the state (§7): stage, lens, turns, citations,
                               matrix, attackPath, stillToDo, preflight
      components/              §2
      styles/tokens.css        design tokens (01) as CSS custom properties; chrome-adjacent from --vscode-*
      styles/base.css          resets, scrollbars, type scale
    tsconfig.json              jsx: react-jsx, lib: dom; includes ../src/protocol.ts, ../src/backend.ts (types)
  harness/
    index.html, main.tsx       the browser dev harness (§5.4): HarnessTransport over recorded snapshots
    snapshots/*.json           recorded states of the reference scenario at each stage
    record.ts                  records a snapshot from a running stack (dev-auth token)
    shots.ts                   layout checks (no horizontal overflow at 1280px, dark + light) — optional
  media/fonts/                 Open Sans 400/600/700/800, JetBrains Mono 400/600 (woff2) + OFL licences
  esbuild.mjs                  webview + harness bundles
  vitest.config.ts             component tests (jsdom + @testing-library/react)
```

`npm run compile` = `tsc -p ./` (host) + `node esbuild.mjs` (webview bundle to
`out/webview/investigation.js` + `.css`). `make workbench-ci` adds `npm test`. The extension
host is not bundled; only the webview is. React 18 is a runtime dependency bundled into the
webview file; nothing is fetched at runtime.

Deleted by this work: `evidenceView.ts`, `evidenceProvider.ts`, the `reckon-evidence:` scheme,
the `reckon.openEvidence` command, the inline template-string UI in `investigationDocument.ts`,
and the transcript-in-editor-tab path (`transcript.open` becomes an inspector view of the
committed turn, §2.5).

---

## 2. Component tree

Mapped to the regions of `08 §8.1`. One canonical component per object; elsewhere a link
(`08 §8.7`). Names below are the files under `webview/src/components/`.

```
<App>                                  regions grid; inspector overlay; dialogs; draft mode
├─ <DraftSeed>                         unseeded state (seed picker, alert, case search) — parity §A
├─ <Header>                            08 §8.2
│   ├─ <Title>                         inline rename (dblclick / pencil); case reference "from INC…" + <CaseNoteControl>
│   ├─ <VerdictPill>                   the verdict act, hover = rationale
│   ├─ <StageIndicator>                derive/stage.ts
│   ├─ <PrimaryAction>                 derive/stage.ts → Review N proposed actions | Send notification | Conclude
│   ├─ <NearestExpiry>                 min expires_at over REQUESTED actions
│   ├─ <SinceYouWereHere>              one-line delta vs last seen (02 §2.12)
│   └─ <HeaderControls>                Pause/Resume/Reopen/Archive · Refresh · (Export moves to Summary)
├─ <WorkQueue>                         08 §8.3; collapsible; icon rail below 1280px
│   ├─ <InvestigationRows>             GET /api/investigations, needs-me-first (ui/06 rules)
│   ├─ <CaseRows>                      GET /api/cases filtered to rows with no investigation; Start investigation
│   └─ <CapabilityHealth>              collapsed fold at the bottom (parity §J; 13 §4 home is the container, v1)
├─ <LensArea>
│   ├─ <LensTabs>                      Summary · Story · Reasoning(count) · Response(pending count)
│   ├─ <SummaryLens>                   08 §8.4 Summary
│   │   ├─ <Tiles>                     verdict · containment · notification · open questions
│   │   ├─ <Narrative>                 what happened + authorship line; Draft / Edit (gap §6.18)
│   │   ├─ <KeyEntities>               grouping objects, role, derivable status words (§9)
│   │   ├─ <StillToDo>                 REQUESTED actions w/ countdown · owner items · notification · open predictions
│   │   ├─ <ProceduresFollowed>        consulted_sops with used=true
│   │   └─ <SummaryActions>            Copy handoff note · Export report (export.md)
│   ├─ <StoryLens>                     08 §8.4 Story (needs §6.12–§6.14)
│   │   ├─ <AttackPath>                derive/attackPath.ts; "Open full graph" → <EvidenceGraph> disclosure (parity §K)
│   │   ├─ <AttackTechniques>          detection finding types + hypothesis labels
│   │   ├─ <TimelineFilters>           All · Auth · Process · Network · Detection (OCSF category)
│   │   ├─ <Timeline>                  rows from GET …/events; baseline rows collapsed; emphasis; citing P<n>
│   │   ├─ <StartDivider>              investigation start (created_at)
│   │   └─ <NotYetKnown>               INCONCLUSIVE / UNTESTED predictions
│   ├─ <ReasoningLens>                 08 §8.4 Reasoning
│   │   ├─ <VerdictBasis>              handles + status, open predictions, discriminators tested; Cards/Matrix toggle
│   │   ├─ <HypothesisCard>*           head (handle · claim · tally · status), ⚡ family, Adopt/Restate/Abandon, + prediction
│   │   │   └─ <PredictionRow>*        one line at rest (glyph · P<n> · title · splits H<n> · count/attested); expands
│   │   ├─ <Matrix>                    derive/matrix.ts
│   │   ├─ <NewHypothesisButton>
│   │   └─ <PinnedEvidence>            the pin fold (canonical place for pins; un-pin here)
│   └─ <ResponseLens>                  08 §8.4 Response
│       ├─ <PlanHeader>                N actions · M owner items · from SOPs …; Review one by one · Approve all T2
│       ├─ <ActionGroup>*              Containment → Eradication → Recovery → Notification → External work
│       │   └─ <ActionCard>*           03 §3.3 decision-grade + policy fact (§6.16); all engine states
│       ├─ <OwnerItems>                §6.17
│       ├─ <NotificationDraft>         assembled from the record; Edit · Preview & send (= request notify.*)
│       └─ <CommsThreads>              Phase F threads: ack / follow up / snooze / done (parity §H)
├─ <Companion>                         08 §8.5; drawer below 1024px
│   ├─ <CompanionModeToggle>           Conversation | Thread (§2.5)
│   ├─ <KnowledgeRail>                 implicit retrieval, include/exclude checkboxes (parity §C)
│   ├─ <Conversation>
│   │   ├─ <AnalystTurn>               the analyst's text; ⚡ scoped turns render as collapsed asides
│   │   ├─ <AgentTurn>                 prose (<Markdown>), <TurnRecords> links, <KnowledgeChips>, <Activity>
│   │   │   ├─ <TurnRecords>           "H1·P5 confirmed", "H1 supported · H2 refuted", "Proposed 3 actions + 1 owner item"
│   │   │   ├─ <ProposedActionCards>   compact <ActionCard variant="compact"> per action-request in the turn
│   │   │   └─ <Activity>              collapsed: "Activity · N tool calls, M records"; expands to <ToolCallBlock>* + records
│   │   └─ <LiveTurn>                  streaming segment: text deltas, reasoning, step marks, tool rows, usage, error
│   ├─ <ThreadView>                    the audit lens: every entry in sequence, act lines, superseded, context-reset boundary
│   └─ <Composer>                      Enter sends, Shift+Enter newline; disabled on CONCLUDED/ARCHIVED; pivot staging
├─ <InspectorDock>                     08 §8.6; up to two panes (pinned + active); Esc closes the active one
│   └─ <Inspector>*                    frame: type label · pin · close · back + breadcrumb; name; <InThisCase>; tabs
│       ├─ <EvidenceInspector>         Details (sentence, curated fields, decoded command line) · Provenance · Raw
│       ├─ <EntityInspector>           type/value/aliases/id-on-hover; role & counts; <Baseline>; actions; other investigations; Pivot; Copy; Pin as evidence
│       ├─ <ProcedureInspector>        What the AI read · Whole document · Versions; hash check; Open original
│       └─ <TurnInspector>             a committed turn's transcript (replaces the transcript editor tab)
└─ dialogs                             in-document modals, never the quick input (13 §3)
    ├─ <PromptCard>                    the reusable text/choice/extras card (parity §L)
    ├─ <VerdictDialog>                 preflight + residual (02 §2.10)
    ├─ <ConcludeDialog>                checklist: verdict · pins · actions terminal · comms resolved · owner items confirmed
    ├─ <ResultDialog>                  Record result with attestation (adr/0054)
    └─ <EnableForm>                    schema-derived enablement form (11 §5.1); confirm hands off to the host
```

Shared primitives (`components/shared/`): `Chip` (entity palette, pinned inset), `Pill`,
`Eyebrow`, `Countdown`, `Markdown` (the escape-first renderer, ported), `Json` (collapsible,
syntax-tinted, copy), `Disclosure`.

### 2.1 Where the current surfaces go

| Today (rail / document) | New home |
|---|---|
| Header title, state pill, verdict badge, lifecycle buttons, Export, Refresh | `<Header>`; Export moves to Summary; state pill becomes the stage indicator; `ACTIVE · REMEDIATING` label is the Contain step |
| Seed chip + Note the case | `<Title>` case reference + `<CaseNoteControl>` |
| Needs your approval (interrupt) | `<PrimaryAction>` "Review N proposed actions" → Response; compact cards in the companion turn |
| Hypotheses (tracker) | `<ReasoningLens>` |
| Institutional knowledge rail | `<KnowledgeRail>` at the top of the companion |
| Pinned evidence | `<PinnedEvidence>` under the hypotheses in Reasoning; "Pinned" marker on prediction rows |
| Actions ledger | `<ResponseLens>` cards in every state (the ledger is the plan) |
| External work (comms) | `<CommsThreads>` in Response under Notification |
| Capabilities fold + gap hints + enable form | `<CapabilityHealth>` at the bottom of the work queue; gap hints stay under tool-call blocks |
| Event-time strip | superseded by `<Timeline>` |
| Minimal evidence graph | `<EvidenceGraph>` disclosure behind "Open full graph" in Story |
| Chronicle (interleaved turns + act lines) | `<ThreadView>` (companion Thread mode); turns in `<Conversation>` |
| Entity popover | `<EntityInspector>` (pivot, pin, copy, appearances live there) |
| Evidence card panel + raw JSON tab | `<EvidenceInspector>` Details / Raw |
| Transcript editor tab | `<TurnInspector>` |

### 2.2 Density rules applied (`08 §8.7`)

A prediction row at rest: status glyph with its word for screen readers, `P<n>`, the short title
(gap §6.12; until it exists, the statement truncated to one line), `splits H<n>`, evidence count
or `attested`. Expanded: the statement in full, the attestation quote with its author by name,
evidence chips, "decides against", Pinned marker, and the row's affordances (⚡ Test P<n> /
Record result / Withdraw / Pin). Principal ids, UUIDs and hashes are on hover or in Provenance.
Timestamps are on hover except in the timeline and the evidence inspector. Sequence numbers and
"committed" lines appear only in `<ThreadView>` and the export.

### 2.3 Action cards

One component, two variants. `full` (Response): header (verb icon tile by group, title, target
chips, tier pill with reversibility word, `action.type` mono, status pill), reason, Because
(evidence chips), the fact grid (Undo · Runs via · Response policy · Window), lineage (`retry of`,
`↺ reversal of`, escalation reason), per-target residual on PARTIAL, the honest error on FAILED,
the pre-send preview on `notify.*`, and the state's buttons (Approve / Approve (challenge)… /
Reject… / Re-request / Reverse…). `compact` (companion): header row, Because chips, and the
Approve/Reject pair; everything else is a link to the full card in Response. Both variants
are the same component rendered from the same `ActionRow`, so they can never disagree.

### 2.4 Inspector frame

`<InspectorDock>` holds at most two `<Inspector>` panes. Opening a citation replaces the active
pane and pushes the previous object onto that pane's back stack; a pinned pane is left alone.
`Esc` closes the active pane. The frame is identical for every type: eyebrow with the type
label, pin and close; back + breadcrumb when reached from another object; a human name;
**In this case** (derive/citations.ts, §7.4); then the typed tabs. Width 440–480px, overlaying
the companion side of the lens area (`08 §8.1`), never replacing the lens.

### 2.5 The thread stays reachable

`08 §8.5` removes sequence numbers and "committed" lines from the reading path and says both
"remain in the thread and the export". `13 §4` keeps the reasoning thread as the audit lens.
The companion therefore has two modes: **Conversation** (default; turns, records as links,
activity disclosure) and **Thread** (the interleaved chronological record: act lines for
approvals, dispatches, results, pins, verdict, lifecycle; superseded acts struck; the context
reset boundary; sequence numbers on hover). The "since you were here" divider renders in both.
This is a decision this document makes; it is flagged in §11.

---

## 3. State ownership

| Concern | Owner | Everyone else |
|---|---|---|
| Investigation state of record | the engine (event-sourced aggregate + projections) | reads |
| Per-open-investigation **cache** of projections, with `seq` | `InvestigationCache` in the extension host | the webview renders snapshots of it |
| Turn streaming (in flight) | `SidecarTransport` → host → `turn.*` messages | the webview renders `<LiveTurn>` |
| Evidence docs, labels, transcripts, SOP excerpts | host caches keyed by ref/hash (content-addressed, shared across panels) | fetched on demand by the webview |
| View state: active lens per investigation, companion mode, inspector stack + pins, matrix toggle, timeline filter, disclosure state, composer draft, rail width, scroll | the webview (`vscode.getState/setState`, keyed by investigation id) | nobody |
| Last-seen event sequence | `SeenTracker` in `globalState` (unchanged) | the tree and the header delta read it |

### 3.1 The cache

```ts
interface InvestigationSnapshot {
  seq: number;                        // investigation.last_event_sequence when fetched
  investigation: InvestigationDetail; // + created_at, opened_by (§8.0)
  hypotheses: Hypothesis[];
  actions: ActionRow[];               // + policy (§6.16), targets keep entity_ref + identifier
  pins: PinRow[];
  comms: CommsThread[];
  ownerItems: OwnerItem[];            // §6.17
  thread: ThreadEntry[];              // + actor.principal_name
  capabilities: Capability[] | null;
  enablement: Enablement | null;
  knowledge: KnowledgeItem[];
  summary: SummaryFacts | null;       // §6.18 structured facts + narrative
  events: EventRow[] | null;          // §6.13, fetched when Story opens or the inspector asks
}
```

The host builds the snapshot with one parallel fetch (as `load()` does today), stamps `seq`
from `last_event_sequence`, and posts `state.snapshot`. Subsequent changes arrive as whole
re-posts of the affected slices (`state.patch { seq, actions }`, `{ seq, hypotheses }`, …);
slices are small and whole re-posts are simpler to reason about than row deltas. The webview
reducer applies a patch only if `patch.seq >= state.seq`; an older patch is dropped. Bulky
payloads (transcripts, evidence docs, raw JSON, SOP bodies, events) never ride a snapshot: they
are requested by the webview and answered one object per message, bounded (~64KB), as today.

### 3.2 Sequence discipline

Every projection message carries `seq`. The webview keeps `state.seq`. If a `state.patch`
arrives with `seq > state.seq + k` where the host did not also send the intervening slices, the
webview requests `ui.resync` and the host re-posts a full snapshot. In practice the host always
reloads after its own commands (as today) and after a stream delta, so a resync is the
self-healing path for sleep/wake and dropped frames, not the normal path.

### 3.3 Live updates

The host opens one `/api/stream?investigation=<id>` WebSocket per open panel (`streamClient.ts`,
bearer from the session; the server closes on token expiry → reconnect with a fresh token). On an
`event` delta: if `sequence_no === cache.seq + 1`, refresh the slices the `event_type` family
touches (`action.*` → actions + thread; `interpretation.*` → hypotheses + pins + thread + verdict;
`lifecycle.*` → investigation + thread; rename → investigation); otherwise reload everything.
The hub only fans out HTTP-handler writes — Temporal-emitted dispatch/result events do not reach
it (`server/hub.go`) — so the existing bounded dispatch poll (`scheduleDispatchWatch`) stays
until the LISTEN/NOTIFY republisher lands. Nothing in the webview knows a WebSocket exists.

---

## 4. The message protocol

The existing unions in `investigationDocument.ts` (`RenderMessage`, `InboundMessage`) move to
`src/protocol.ts`, keep every current member, and gain the members below. The webview imports
the types; the host narrows on `type`. One protocol, extended; no second one.

### 4.1 Host → webview (`RenderMessage`), additions

```ts
| { type: "state.snapshot"; seq: number; snapshot: InvestigationSnapshot }
| { type: "state.patch"; seq: number; patch: Partial<InvestigationSnapshot> }
| { type: "me"; name: string; principal: string }                        // for "edited by you" and attestations
| { type: "queue"; investigations: InvestigationSummary[]; cases: CaseRow[] } // work queue (08 §8.3)
| { type: "inspector.evidence"; ref: string; doc: EvidenceDoc; view: EventView | null; around: EventRow[]; appearances: Appearance[] }
| { type: "inspector.entity"; ref: string; doc: EvidenceDoc | null; label: string; baseline: Baseline | null; appearances: Appearance[]; eventCount: number }
| { type: "inspector.procedure"; sopId: string; sop: SOPView; excerpts: RetrievedExcerpt[]; revisions: SOPRevision[] }
| { type: "inspector.turn"; interpretationId: string; body: string }
| { type: "inspector.error"; ref: string; message: string }
| { type: "events"; seq: number; items: EventRow[]; entity?: string }   // §6.13
| { type: "summary"; seq: number; facts: SummaryFacts; narrative: Narrative | null } // §6.18
| { type: "narrative.drafting" } | { type: "narrative.failed"; message: string }
| { type: "ownerItems"; seq: number; items: OwnerItem[] }                 // §6.17
| { type: "copied" }                                                     // clipboard ack
```

Existing members kept as-is for the streaming channel (`turn.*`, `aside.start`), the draft
(`draft*`, `seeded`), and the one-object answers (`evidence.label`, `entity.info`, `knowledge`,
`enablement`, `renamed`, `rename.begin`, `error`, `loading`). `data`/`pending`/`pins`/`comms`/
`chronicle`/`events.strip`/`graph` are superseded by `state.snapshot`/`state.patch` and are
removed once the React webview is the only consumer (PR 2 ships both for one commit range,
then drops the old ones).

### 4.2 Webview → host (`InboundMessage`), additions

```ts
| { type: "ui.resync" }
| { type: "queue.get" }
| { type: "queue.start"; caseNumber: string }                            // Start investigation from the work queue
| { type: "inspector.open"; target: { kind: "ref"; ref: string } | { kind: "sop"; sopId: string } | { kind: "turn"; interpretationId: string } }
| { type: "events.get"; entity?: string }
| { type: "summary.get" }
| { type: "narrative.draft" }                                            // sidecar BYOK draft (§6.18)
| { type: "narrative.save"; body: string }
| { type: "handoff.copy" }                                               // builds the handoff note host-side and copies
| { type: "owner.new"; title: string; owner: string; detail?: string }
| { type: "owner.confirm"; id: string; note: string }
| { type: "owner.waive"; id: string; reason: string }
| { type: "notify.request"; channel: "notify.email" | "notify.slack"; target: string; subject?: string; body: string; ownerItemIds: string[] }
| { type: "action.reverse"; actionId: string; rationale: string }        // parity: reversal request
| { type: "sop.openOriginal"; sopId: string; revision?: string; location?: string }
| { type: "view.state"; investigationId: string; lens?: Lens; companion?: "conversation" | "thread" } // mirrored to globalState so the tree can deep-link
```

Existing intents stay (`send`, `action.*`, `pin.*`, `verdict.submit`, `hyp.*`, `pred.*`,
`enablement.apply`, `comms.*`, `lifecycle`, `evidence.resolve`, `entity.info`, `export.open`,
`copy`, `investigation.open`, `rename`, `seed.*`, `caseNote.post`). `evidence.open` and
`transcript.open` are removed: both become `inspector.open`.

### 4.3 Rules

- Every host→webview message is JSON-serializable and bounded; raw payloads are requested per
  object. The host never posts a whole transcript set; the webview asks for turns as they come
  into view (the current 40-most-recent prefetch stays as the warm path).
- The webview treats model text and every engine string as untrusted: the `Markdown` renderer is
  the ported escape-first one; no `dangerouslySetInnerHTML` outside it; `Json` renders through
  `textContent`.
- An intent is a request. The host re-validates against its cache what the aggregate will also
  check (T3 requires a challenge; reversal requires SUCCEEDED and `reversible_by`), so a stale
  webview cannot fire an impossible call; the engine's refusal is still the enforcement point.

---

## 5. Build, load, theme, harness

### 5.1 Bundle and CSP

`esbuild.mjs` builds `webview/src/main.tsx` → `out/webview/investigation.js` (iife, es2022,
minified in `package`, sourcemaps in dev) and emits `out/webview/investigation.css` (the CSS
imports in the tree). The host's `html()` becomes:

```html
<meta http-equiv="Content-Security-Policy" content="
  default-src 'none';
  style-src ${cspSource};
  font-src ${cspSource};
  img-src ${cspSource} data:;
  script-src 'nonce-${nonce}';">
<link rel="stylesheet" href="${asWebviewUri(out/webview/investigation.css)}">
<div id="root"></div>
<script nonce="${nonce}" src="${asWebviewUri(out/webview/investigation.js)}"></script>
```

`style-src` drops `'unsafe-inline'`: all styles are in the bundled stylesheet; dynamic values
(entity color, countdown width) are set through CSS custom properties on elements, not inline
`style` strings. Fonts are `@font-face` in `tokens.css` with `url()`s rewritten at build time to
a `__FONTS__` placeholder the host replaces with `asWebviewUri(media/fonts)`; the harness replaces
it with a relative path. No CDN, ever (`06 Webview hardening`).

### 5.2 Tokens and theming

`tokens.css` emits the `01` token set on `:root`: type scale, spacing, radius, motion, brand,
semantic (ok/warn/bad/info/pin), the entity palette, surfaces. Chrome-adjacent tokens derive from
`--vscode-*` with the `01` values as fallbacks (`--text: var(--vscode-foreground)`,
`--editor-bg: var(--vscode-editor-background, #0a0f24)`, `--border: var(--vscode-panel-border,
rgba(255,255,255,.09))`, inputs from `--vscode-input-*`). Semantic and entity tokens are
absolute and switch on `body.vscode-light` / `body.vscode-high-contrast` to the light values of
`01` (the current webview already does this; it ports unchanged). Fonts: Open Sans and JetBrains
Mono from `media/fonts/`, falling back to `--vscode-font-family` / `--vscode-editor-font-family`.

### 5.3 Layout floors

Regions per `08 §8.1`: work queue 220–270px (icon rail below 1280px), lens content max 900px,
companion 340–420px (drawer below 1024px), inspector 440–480px overlaying. The grid uses
container queries on `#root` so the harness can test the collapse points by resizing the
viewport. No horizontal page scroll at 1280px is a harness assertion.

### 5.4 The browser dev harness

`harness/index.html` mounts the same `<App/>` with a `HarnessTransport` that answers from a
recorded snapshot set and echoes intents into a side panel (so a click's intent is visible).
Snapshots are recorded from a running stack by `harness/record.ts` (dev-auth token; writes the
`InvestigationSnapshot` plus the evidence docs, events, SOP excerpts and transcripts it
references) at the reference scenario's stages: `seeded`, `reasoning`, `verdict-reached`,
`remediating`, `concluded`, and one `impossible-travel` snapshot. The recorded files are
committed under `harness/snapshots/` (fixture data only). A theme toggle flips
`body.vscode-light`. `npm run harness` serves it with esbuild's dev server; `npm run harness:shots`
(optional, Playwright) renders each lens at 1280px and 1024px in both themes and fails on
horizontal overflow — the structural comparison against `mockups/` is done by eye against those
renders.

### 5.5 Tests

- **Component tests** (vitest + jsdom + testing-library) over the harness snapshots: each lens
  renders its mockup's elements from recorded data; each derivation in `derive/` has table
  tests (stage, default lens, matrix cells, in-this-case, turn grouping, preflight).
- **Protocol tests**: the reducer applies/drops patches by `seq`; `ui.resync` after a gap.
- **Host tests** are out of scope for v0 (no extension-host test runner in the repo today); the
  host stays thin and the live road test (`implementation/road-test.md`) covers it.

---

## 6. The transport seam

```ts
export interface WebviewTransport {
  send(intent: InboundMessage): void;
  onMessage(handler: (m: RenderMessage) => void): () => void;
  getState<T>(): T | undefined;
  setState(s: unknown): void;
}
```

`VsCodeTransport` wraps `acquireVsCodeApi()`. `HarnessTransport` serves recorded snapshots. A
future browser surface implements the same interface against `BackendClient` directly (which is
why `backend.ts` keeps no `vscode` import) and the agent loop over a local sidecar or a
WebSocket; nothing under `components/` or `derive/` knows which.

---

## 7. Derivations (pure, tested, client-side)

All in `webview/src/derive/`. They read the snapshot and produce display facts; none invents
state.

### 7.1 Stage (`stage.ts`, `08 §8.2`)

Inputs: `investigation.status`, `verdict`, `actions`, `comms`, `ownerItems`.
`terminal(a)` = status ∈ {SUCCEEDED, PARTIAL, FAILED, TIMEOUT, REJECTED, EXPIRED, REVERSED}.
`isNotify(a)` = `action_type` starts with `notify.`.

| Step | done | current |
|---|---|---|
| Investigate | verdict exists | status ∈ {DRAFT, ACTIVE, PAUSED} and no verdict |
| Verdict | verdict exists | never (a point) |
| Contain | verdict and every non-notify action terminal | verdict and a non-terminal non-notify action exists |
| Notify | every notify action terminal, no comms thread awaiting reply, no OPEN/SENT owner item | Contain done and any of those outstanding |
| Close | status CONCLUDED or ARCHIVED | Contain and Notify done, not concluded |

Owner items are counted under Notify because the notification is what carries them to their
owner (`08 §8.4` Response); a confirmed item is a reply. Flagged in §11.

Primary action: Investigate → none; any action with `pending` → "Review N proposed actions"
(scrolls to Response); Contain done and Notify outstanding with no `notify.*` requested yet →
"Send notification" (scrolls to the draft); preflight passes and not concluded → "Conclude".

Lifecycle labels keep today's derived words where the stage does not cover them: `PAUSED`
renders the pause state on the title row and veils the composer; `ARCHIVED` renders read-only.

### 7.2 Default lens (`lens.ts`, `08 §8.4`)

Reasoning while no verdict; Response from the verdict until CONCLUDED; Summary once CONCLUDED
or ARCHIVED. The analyst's last choice per investigation (view state) wins when present.

### 7.3 Turn grouping (`turns.ts`, `08 §8.5`)

Walk the thread by `sequence_no`. A transcript-bearing entry is a turn. The AI_DELEGATED entries
without a transcript recorded since the previous turn (hypothesis, prediction, support,
refutation, inconclusive, action-request, evidence-pin) are that turn's **records**. Each record
renders as one link line: `H1·P5 confirmed` / `H1·P5 disconfirmed` (prediction outcome),
`H2 refuted` (hypothesis outcome), `recorded P3, P4 under H1` (new predictions), `proposed H1,
H2` (new hypotheses), `Proposed 3 actions + 1 owner item` (action-request group), `pinned …`.
Clicking scrolls the lens to the object, switching lens if needed (`ui` state, no message).
Human acts (adopt, record result, approve, pin, verdict, lifecycle) are not records of a turn;
they are in `<ThreadView>` and on their objects. The activity disclosure counts the turn's tool
calls (from its transcript) and records.

### 7.4 In this case (`citations.ts`, `08 §8.6`)

For a ref, from the snapshot: predictions whose `test_result_refs` include it (→ `P<n>` links);
actions whose `evidence_refs` include it (→ "grounds Isolate host") or whose target `entity_ref`
equals it (→ "target of Disable account"); pins citing it; hypotheses `rooted_at_ref`; the seed
(`entity_ref` / `case_ref` / `detection_finding_ref`); the verdict's `input_refs`. Counts for
the entity frame: events (from `events` for the entity), actions. This is a client join over
data the snapshot already holds; `ref_appearances` stays the cross-investigation fold.

### 7.5 Matrix (`matrix.ts`, `08 §8.4`)

Rows: every prediction not WITHDRAWN under any hypothesis not ABANDONED. Columns: hypotheses not
ABANDONED. Owner cell = outcome (CONFIRMED ✓, DISCONFIRMED ✕, else ?); each id in
`discriminates` gets the opposite (CONFIRMED → ✕, DISCONFIRMED → ✓, else ?); other cells blank.
Footer: count of ✕ per column. Rows with an undecided owner cell are tinted.

### 7.6 Attack path (`attackPath.ts`, `08 §8.4` Story)

Entities = `object_refs` of the observed-data cited by CONFIRMED predictions of SUPPORTED
hypotheses, ordered by the first event time of the events those observed-data derive from
(both from `events`, §6.13). Edges = the normalizer relationships among those entities
(`relationship_type` as the label; `events[].relationships`). Span = last − first event time.
Absent when no hypothesis is SUPPORTED; the section then says so.

### 7.7 Timeline rows and emphasis

Rows = `events` (all retrieved events of the investigation) with the server-rendered sentence and
fields (§6.12). Citing predictions = predictions whose `test_result_refs` include one of the
event's `observed_data_refs`. Emphasis: cited by a CONFIRMED prediction of a SUPPORTED hypothesis
→ emphasized; cited by a DISCONFIRMED prediction → "cited against"; part of a repeated pattern
(§6.14) → collapsed into one baseline row with the count and a link to expand; otherwise plain.
The seed row and the investigation-start divider come from the seed and `created_at`.

### 7.8 Preflight (`preflight.ts`)

Verdict: ≥1 active pin (the engine refuses otherwise), refs attached, rationale. Conclude:
verdict recorded · ≥1 pin · every action terminal · no comms thread awaiting reply · no OPEN/SENT
owner item. Each unmet item links to the surface that satisfies it (`README` non-negotiable 7).

---

## 8. Engine work (`00 §6.12`–`§6.18`) — contracts

Each gap is its own issue → ADR → PR per the repo convention; the ADR decides what the sheets
leave open and says why. The shapes below are what the webview binds to; the ADRs may refine
names but not meaning. Every projection change arrives as events with folds and projections in
one transaction where it touches the aggregate; CRUD + thin history where it does not (the
architectural commitment: the investigation aggregate is the only event-sourced thing).

### 8.0 Small view additions (land with the layout PR, no ADR)

- `InvestigationView`: `created_at`; `opened_by_name` (the `investigation.created` actor's
  display name, `adr/0077`); list rows add `has_verdict`, `open_actions`, `open_notifications`
  so the work queue can derive the stage step without fetching every investigation.
- `ThreadActorView`: `principal_name`. `PinView`, `VerdictView`, `AttestationView` already
  carry or gain the name. People render by display name, id on hover (`08 §8.7`).
- `ActionView.targets` keep `entity_ref` and `resolved_identifier` (the client stops flattening).
- `GET /api/cases` is already the queue; the work queue filters client-side to rows with no
  `investigation`.

### 8.1 §6.12 — Per-class event views and prediction short titles

**Decision.** Display knowledge for OCSF classes lives **server-side**, in one Go package
(`capability/ocsfview`, pure functions, no I/O): per `class_uid` a curated field list and a
one-sentence rendering with typed entity slots (so the client renders chips, not text). The
binding allows client-side; server-side is chosen because the export, the timeline, the
inspector and the baseline fold must agree, and the fixtures already exercise the Go side.

```json
// EventView — on GET /api/evidence/{ref} (ocsf kind) as "view", and on every EventRow
{
  "sentence": [ {"t":"entity","ref":"user-account--…","text":"svc_backup"}, {"t":"text","text":" interactive RDP logon to "}, {"t":"entity","ref":"x-host--…","text":"WIN-FILE01"}, {"t":"text","text":" from "}, {"t":"entity","ref":"ipv4-addr--…","text":"10.0.4.20"} ],
  "fields": [ {"label":"User","value":"svc_backup","ref":"user-account--…","mono":true}, {"label":"Logon type","value":"RemoteInteractive (type 10)"}, {"label":"Result","value":"Success","tone":"ok"} ],
  "category": "auth",             // auth | process | network | detection | asset | case | other (from category_uid)
  "decoded": { "field": "process.cmd_line", "encoding": "base64-utf16le", "text": "echo" }  // only when a known encoding decodes cleanly; the original is always shown
}
```

Classes covered: every class the normalizers handle (`03 §4.1`–`§4.12`) plus 2005 and 5001.
Unknown classes get `fields` from the top-level scalars and no sentence.

**Prediction short title.** A new optional `title` on `x-prediction`, recorded with the
prediction (`PredictionNode.Title` on the `prediction` interpretation event; projected to
`prediction_current.title`; `PredictionView.title`). Authored by whoever records the prediction:
the agent's `record_prediction` tool gains an optional `title` parameter (≤ 64 characters, a
noun phrase, no trailing period); the "+ prediction" dialog gains a title field. When absent the
aggregate derives one deterministically at record time (the statement up to its first clause
boundary, clipped to 64 characters) and marks `title_derived: true`, so the record always holds
a title and the UI never generates one at render time. Titles are frozen with the statement.
This is a tool-schema addition, not a base-prompt change; `make eval` runs once as a check and
the baselines are expected to stand.

### 8.2 §6.13 — Entity-scoped event query

**Decision.** Membership of a retrieved event in an investigation is recorded at ingest, not
inferred later. `CapabilityInvokeBody` gains `investigation_ref` (the agent loop and the
workbench already know it; empty keeps today's behavior for ad-hoc calls). `persistInvokeResult`
writes two join tables in the same transaction:

```sql
investigation_events (aggregate_id, ocsf_event_id, verb, coverage, source_tool, invoked_at, invoke_params JSONB, PRIMARY KEY (aggregate_id, ocsf_event_id))
observed_data_events (observed_data_id UUID, ocsf_event_id UUID, PRIMARY KEY (observed_data_id, ocsf_event_id))  -- the derived-from edge, persisted
```

`GET /api/investigations/{id}/events?entity=<ref>&category=<auth|process|network|detection>&around=<event_id>&limit=`
returns, ordered by event time:

```json
{ "events": [ { "id":"…", "class_uid":3002, "class_name":"Authentication", "time":"…", "recorded_at":"…", "source_tool":"fixture:lateral-movement-via-rdp",
  "verb":"enumerate_logons", "coverage":"COMPLETE", "view": EventView, "observed_data_refs":["observed-data--…"], "entity_refs":["user-account--…","x-host--…"],
  "relationships":[{"type":"x-authenticated-to","source_ref":"…","target_ref":"…"}] } ], "total": 17 }
```

`entity` restricts to events whose observed-data `object_refs` include the ref; `around` returns
the window of ±N events on the same host entity as the given event (the inspector's "Around
it"). Payloads are not included (the evidence endpoint serves them). A read over persisted
tables, never a capability call.

### 8.3 §6.14 — Entity baselines

**Decision.** A baseline is a **counting fact about the retrieved record**, computed server-side
by a pure fold (`capability/ocsfview.Baseline`), never a judgment: for one entity within one
investigation, events of one class are grouped by the class's pattern key (auth: logon type ·
source · destination; network: destination · port; process: image name · parent); a group with
≥ 3 events on ≥ 3 distinct days is a **repeated pattern** (count, days, typical hour); events of
the same class in the retrieved set that match no pattern are listed as **deviations** with the
fields that differ. `GET /api/investigations/{id}/entities/{ref}/baseline` returns the patterns
and deviations, or `204` when nothing repeats — the UI shows the section only when the record
holds one and captions it with the window actually retrieved. The timeline collapses a pattern's
events into one row. The word "normal" appears as the sheet's section title only; the body says
"N events over M days".

### 8.4 §6.15 — Procedure excerpt provenance

**Decision.** The `06 §6.2` side store is built: `knowledge_retrievals` in `reckon_main`
(id, tenant, investigation_id, interpretation_id, sop_id, sop_revision_hash, excerpt, excerpt_hash,
location JSONB, score, band, retrieved_at), keyed by content hash so repeated retrievals of one
revision share a row. The agent session already holds each `recall_sops` result; on commit it
sends the retrievals with the interpretation (`consulted_sops[].excerpt`, `.excerpt_hash`,
`.location`), the server writes the side-store rows and the event keeps `retrieval_ref` +
`excerpt_hash` only. **Location** is computed by the knowledge facade from the SOP body: the
excerpt's offset → the heading path above it (markdown) or the page (when the import recorded
pages); the import path records `{format, pages?}` in entry meta. **Retained originals**:
`sop_originals (lineage_id, revision_hash, filename, media_type, bytes)` written by both import
endpoints; `GET /api/sops/{id}/original?rev=` serves it and the workbench opens it in an editor
(a document, not a citation). New reads: `GET /api/sops/{id}/revisions` (hash, at, author,
importer) and `GET /api/investigations/{id}/retrievals?sop=<id>` (the excerpts this
investigation retrieved, k of n, with the turn that retrieved each). The inspector compares
`excerpt_hash` against the current revision's content for the same span: match → "Exactly as
retrieved"; else "updated since this case consulted it" with a line diff rendered client-side.

### 8.5 §6.16 — Response-policy status on requested actions

**Decision.** The Gate 2 evaluation already recorded by `action.policy_evaluated` is folded onto
`action_current` and served on `ActionView.policy`:

```json
"policy": { "effect": "NONE" | "AUTO_APPROVE" | "REQUIRE_TWO_PARTY" | "DENY", "matched_policy_ref": "…", "routes_to": "requester" | "pool", "secondary_approver_pool": ["…"], "shadow_would_fire": ["policy/…"] }
```

The card renders the fact honestly: an action a policy auto-approved never sits in REQUESTED (the
endpoint approves and dispatches it), so a REQUESTED card reads "No policy pre-authorizes
`host.isolate` · needs your approval" or "Two-party: a second approval from <pool>", and
"a shadow policy would have pre-authorized this" when one would. An APPROVED/EXECUTING card keeps
the existing "Auto-approved by policy <ref>" badge. The mockup's "Pre-authorized" beside an Approve
button is not a state the engine has; the sheet's wording ("whether … pre-authorizes … or it
needs an approval routed to its owner") is what renders.

### 8.6 §6.17 — Owner action items

**Decision.** An owner action item is **its own record**, CRUD + thin history, modeled on the
comms threads (`comms/`), not an x-action. It executes nothing in any tool, carries no blast
radius, and never dispatches; forcing it through Gate 2 and the dispatch ledger would invent a
tier that measures no harm and a dispatch that never happens. `owner_items (id, tenant,
investigation_id, title, owner, detail, status OPEN|SENT|CONFIRMED|WAIVED, notify_action_id,
created_by, created_by_name, created_at, confirmed_by, confirmed_by_name, confirmed_at,
confirmation_note, history JSONB)`. Endpoints: `GET/POST /api/investigations/{id}/owner_items`,
`POST /api/owner_items/{id}/confirm|waive`. "Add to notification" attaches the item to the
notification draft; when the `notify.*` action it rides on SUCCEEDS, the item moves to SENT
(the comms layer already observes that result); the analyst records the owner's confirmation
(CONFIRMED, with a note) or waives it. The agent gets a `propose_owner_item` tool whose
description teaches when to use it (work only an owner outside reckon can do); no base-prompt
rule changes. The conclude preflight counts OPEN/SENT items as an input (decision support at
v0, like comms; the engine-side gate lands with Phase F).

### 8.7 §6.18 — Narrative summary at any time

**Decision.** Two tiers, as `06 §3.2`: structured facts are a server-side pure function over
the projections at any state (the existing `buildSummary` generalized out of `temporal/` into
`summary/` and served on `GET /api/investigations/{id}/summary`: verdict with author name,
containment counts and nearest expiry, notification status, open questions, key entities with
role and derivable status words, procedures followed, as_of_sequence); the **narrative** is drafted
on the client plane (`transport.draftNarrative` → sidecar `draftNarrative` → one no-tools BYOK
completion over the record, the backend holds no model key) and stored with its authorship as
CRUD + thin history: `investigation_narratives (id, investigation_id, revision, body,
generator_model, drafted_at, as_of_sequence, author_kind AI_DELEGATED|HUMAN, author_id,
author_name, created_at)`; `PUT /api/investigations/{id}/narrative` appends a revision (an
analyst edit is a HUMAN revision; the AI draft is never overwritten, it is superseded). The
authorship line reads "drafted by reckon · edited by <name>". At conclude, the latest narrative
prefills the conclusion summary; the knowledge summary pipeline is unchanged. The handoff note
is built from the summary facts + narrative + still-to-do, host-side, and copied.

---

## 9. What the mockups show that the record cannot back

Listed as gaps, not faked (`00` seam rules). Each renders the honest nearest fact or nothing.

| Mockup element | Status |
|---|---|
| Tenant label on header and queue rows | rendered only when tenancy is enabled; absent in OSS |
| "owner <name>" on the header line | no ownership model; rendered as "opened by <name>" from the creating actor |
| Entity status words "compromised", "unidentified", "malicious", "suspicious" | not engine vocabulary; rendered: role (seed subject / cited / target), action-derived words ("isolation requested", "isolated", "disabled", "blocked"), and the verdict word only for the investigation |
| "Pre-authorized" beside an Approve button; "Request Contoso approval" | impossible state (§8.5); the card states the policy fact; two-party routing renders when the engine says TWO_PARTY |
| Timeline "expected" on a bystander logon | no source; emphasis classes are cited-for / cited-against / baseline / plain |
| "Needs Contoso's approval · credential rotation is an owner action item" | first half from `policy.routes_to`; second from owner items |
| `[FORMAT]`, `[SOURCE NAME OR URL]`, `[PAGE, HEADING OR ANCHOR]` | from SOP meta and §8.4; "located at" absent when the import recorded no structure |
| Notification "to Contoso security contact" | no configured contact; the analyst supplies the target on Edit |
| Baseline chart (seven nights) | rendered only from §8.3 patterns; absent otherwise |
| Narrative wording | §8.7; absent until drafted; never templated by the UI |

---

## 10. Delivery

PRs in dependency order, each green on `make ci` and `make workbench-ci`, each referencing #79
(no new ADR for the UI PRs; one ADR per engine gap):

1. **Design + parity inventory** — this document and `implementation/workbench-parity.md`.
2. **React move with parity** — esbuild, tokens, fonts, `protocol.ts`, cache + seq, the
   component tree rendering the *current* surfaces (same regions as today: document + rail),
   harness with recorded snapshots, component tests. Every parity row ticked in the harness and
   on the live stack. The old inline UI is deleted in the same PR (two renderers would be two
   truths).
3. **Inspector (L1)** — evidence / entity / turn renderers, frame, back stack, pinning, Raw +
   Provenance tabs; `evidenceView.ts`, `evidenceProvider.ts`, the scheme and the command deleted;
   the entity popover folded in. Procedure inspector ships its frame and "Whole document" tab
   (the excerpt tabs wait for §8.4).
4. **Density + layout (L2, L3)** — header with stage and primary action, work queue, lens tabs
   with the lifecycle default, companion column with turn records and the activity disclosure,
   one-line prediction rows and the matrix, Response lens from the existing action data,
   `/api/stream` subscription, §8.0 view additions.
5. **Engine gaps**, one PR each, each landing its UI slice so it is demoable:
   §6.12 (event views + titles; Timeline rows and inspector Details), §6.13 (events endpoint;
   Story timeline, Around it, attack path), §6.14 (baseline), §6.15 (excerpt provenance; the
   procedure inspector's excerpt tabs and Versions), §6.16 (policy fact on cards), §6.17 (owner
   items; Response and Summary sections), §6.18 (summary endpoint, narrative, handoff note).
6. **Cleanup** — dead code, `design/ui` sheet amendments forced by implementation (in the PRs
   that force them, this PR sweeps), `design/13 §7` note, road test and demo script updates.

Done means (from the brief): `make ci` and `make workbench-ci` green; new Go has tests in the
repo's style; components have harness tests; both scenarios run end to end through every lens,
the inspector, approvals and conclude; every element names its source or is listed in §9; each
harness screen matches its mockup in structure and tokens in both themes with no horizontal
overflow at 1280px; the parity checklist is complete; `make eval` shows no regression when a key
is set.

---

## 11. Decisions this document makes that the sheets leave open — for review

1. **The thread keeps a home** as the companion's second mode (§2.5), since `13 §4` keeps it as
   the audit lens and `08 §8.5` only removes it from the reading path.
2. **Proposed actions in the companion are compact cards** (§2.3): header, Because chips and the
   Approve/Reject pair, linking to the full card in Response — the reconciliation of "render as
   action cards, not prose" (`08 §8.5`) with "one canonical place per object" (`08 §8.7`).
3. **Owner items count under the Notify stage** (§7.1) and are their own CRUD record (§8.6).
4. **OCSF display knowledge is server-side** (§8.1) so export, timeline, inspector and baseline
   agree; the binding permitted client-side.
5. **The prediction title is author-supplied with a deterministic derived fallback recorded at
   record time** (§8.1); a tool-schema addition, not a prompt change.
6. **Baselines are counting facts over the retrieved record**, computed server-side (§8.3).
7. **Narratives are CRUD revisions drafted on the client plane** (§8.7), not aggregate events;
   the conclusion summary remains the aggregate's.
8. **The entity popover is retired into the entity inspector**; pins, capability health and comms
   relocate per §2.1.
9. **The webview's stylesheet is fully bundled**, so `style-src` loses `'unsafe-inline'`.
10. **The `/api/stream` subscription is added now**; Temporal-emitted events keep the bounded
    poll until a LISTEN/NOTIFY republisher exists.
