# Investigation webview — parity inventory

Every behavior the current webview (`workbench/src/investigationDocument.ts`, `evidenceView.ts`,
`evidenceProvider.ts`) and the Import SOPs panel (`importView.ts`) support, as of the React move
(`implementation/workbench-react.md`).
Each row names where the behavior lives in the new tree (`workbench-react.md §2`) and is ticked
twice: in the browser harness against recorded snapshots, and on the live stack against the
reference scenario. A row is not done until both are ticked. Rows marked *relocated* keep the
behavior in a different place by design (`08`); rows marked *superseded* are replaced by a named
new surface and the old one is deleted.

Legend: `[ ]` not yet · `[h]` harness · `[l]` live · `[x]` both.

## A. Seed (draft) mode

| # | Behavior | New home | Status |
|---|---|---|---|
| A1 | Unseeded panel opens as the draft surface; `__draft__` key re-keys to the aggregate id on create; the surface never closes | `<DraftSeed>`; host `seedDraft` unchanged | [ ] |
| A2 | Free-text seed: live classification entity vs question (whitespace / trailing `?`), entity sub-label (IP / file / host), one-click correction toggle | `<DraftSeed>` | [h] |
| A3 | Start disabled until text; Enter submits; double-submit guard on both sides | `<DraftSeed>` + host `seeding` set | [h] |
| A4 | "From an alert…" path: alert id + source, Start from alert | `<DraftSeed>` | [ ] |
| A5 | "From a case…" path: filter field, Search, results list, pick seeds (kind `case`); opens on the configured queue; `reckon.seedFromCase` opens straight into it | `<DraftSeed>`; `draft`/`draft.cases`/`draft.caseError` messages | [ ] |
| A6 | Create errors render inline (`draft.error`) and re-enable | `<DraftSeed>` | [h] |
| A7 | Case seed: `caseNoteOnSeed` posture — auto requests the note; confirm shows "Note the case" on the seed line; off shows nothing | `<CaseNoteControl>` in `<Title>`; host `offerCaseNote` | [ ] |
| A8 | "Note the case" prompt (editable body) → `ticket.comment` action; the line then shows the note's status from the action record | `<CaseNoteControl>` | [ ] |

## B. Header and lifecycle

| # | Behavior | New home | Status |
|---|---|---|---|
| B1 | Title; seq meta line | `<Title>`; seq moves to hover / Thread mode (`08 §8.7`) | [h] |
| B2 | Engine state pill with derived `ACTIVE · REMEDIATING` / `ACTIVE · VERDICT REACHED` | *relocated*: `<Header>` stage indicator (Investigate · Verdict · Contain · Notify · Close) + the primary action; the engine word shows as a pill only for DRAFT / PAUSED / ARCHIVED | [h] |
| B3 | Verdict badge with rationale tooltip | `<VerdictPill>` | [h] |
| B4 | Verdict… button (hidden when closed) | `<HeaderControls>` / `<ReasoningLens>` | [h] |
| B5 | Lifecycle buttons by state: Pause; Conclude… (disabled until a verdict, with the comms warning); Resume; Reopen…; Archive… (terminal) | `<Header>` controls; Conclude becomes the primary action when the preflight is clean | [h] |
| B6 | Composer disabled with the state's placeholder on CONCLUDED / ARCHIVED; rename pencil hidden on closed records | `<Composer>`, `<Title>` | [h] |
| B7 | Export (live markdown) opens in an editor tab | *relocated*: `<SummaryLens>` Export report | [h] |
| B8 | Refresh reloads from the backend | `<HeaderControls>` | [ ] |
| B9 | Inline rename: pencil, double-click, tree-initiated `rename.begin`; Enter commits, Esc cancels, blur commits; `renamed` echo; tab title updates | `<Title>` | [h] |
| B10 | Invisible activate note on a DRAFT's first pending action | `<ActionCard>` (full) on a DRAFT | [h] |
| B11 | "Since you were here" (first unseen entry) and the delta line | `<SinceYouWereHere>`; divider in Conversation and Thread modes | [ ] |

## C. Knowledge section (`06 §5.1`, `09 §9.6`)

The rail becomes the Knowledge section at the top of the companion column; C1–C3 are the rail's
behaviors kept, C4–C7 the section's additions from `09`.

| # | Behavior | New home | Status |
|---|---|---|---|
| C1 | Items (SOP / PRIOR CASE) with title, band, rationale, excerpt tooltip, include toggle; header posture word | `<KnowledgeSection>` rows; "The agent reads only what is included." | [h] |
| C2 | Included set rides the next turn (`includedKnowledge`); undefined when nothing surfaced | webview state → `<Composer>` → `send` | [h] |
| C3 | Loads only for workable states (DRAFT/ACTIVE/PAUSED) | host `load` unchanged (the section renders for workable states) | [h] |
| C4 | Collapsed summary "N retrieved · M included" once reviewed | `<KnowledgeSummaryLine>` | [h] |
| C5 | Search within the investigation; results join the list with their toggle off | `<KnowledgeSearch>` → `knowledge.search` → sidecar `searchKnowledge` (`workbench-react.md §8.0`) | [h] |
| C6 | Each item opens in the inspector (Procedure / Similar case); "Open the Knowledge view" | `inspector.open` / `knowledge.open` | [h] |
| C7 | Nudge beside ⚡ Propose tests, ⚡ Decide and the response plan when auto-posture items are not included; links to the section, includes nothing | `<LensArea>` nudge above Reasoning and Response (`derive/knowledgeNudge.ts`) | [h] |

## D. Conversation and streaming

| # | Behavior | New home | Status |
|---|---|---|---|
| D1 | Analyst bubble; `turn.start`; reasoning (italic) deltas; text deltas re-rendered as markdown with the streaming caret; step marks from round 2; tool rows (name, args, FIFO settle with coverage · N event(s) / error); committed line; usage line; error line preserved until the next turn | `<LiveConversation>` inside the companion (both modes) | [h] |
| D2 | ⚡ asides stream into a collapsed disclosure labelled `… · working…` → `… · done — see the tracker` | `<Conversation>` renders committed ⚡ asides collapsed; live ones stream collapsed | [h] |
| D3 | Composer: Enter sends, Shift+Enter newline; disabled during a turn; re-enabled unless closed | `<Composer>` at the foot of the companion | [h] |
| D4 | Markdown: inline code, bold, bullets, numbered (loose lists stay one list), headings, fenced blocks, pipe tables; escape-first | `<Markdown>` (ported) | [h] |
| D5 | Tool-result pin button on the summary line; inert when already pinned; recovers on un-pin; ref chips in the body | `<ToolCallBlock>` | [h] |
| D6 | Gap hint under an UNAVAILABLE tool result when an installed-not-enabled adapter could serve it; "Set up…" opens the enable form | `<ToolCallBlock>` + `<EnableForm>` | [h] |
| D7 | Cold restore: committed turns rebuilt from line-framed transcripts (user / assistant / tool_use / tool_result / loop), envelopes distilled into coverage + chips, non-envelope results pretty-printed, aside turns reconstructed collapsed | `<AgentTurn>` via `<Activity>`; transcripts fetched on demand | [h] |
| D8 | Chronicle act lines (request / approval / rejection / dispatch / result / reversal / expiry / pin / verdict / lifecycle / conclusion) with icon, summary, up to two chips, time; superseded struck + tagged | *relocated*: `<ThreadView>` (companion Thread mode) | [h] |
| D9 | Context-reset boundary line | `<ThreadView>` (and a marker in Conversation) | [h] |
| D10 | Turn stub for a transcript not loaded → opens the transcript | *superseded*: the stub opens the Turn inspector (Transcript tab) | [h] |
| D11 | SOP chips (followed / consulted) and similar-case chips (drew on / similar) under a turn | `<KnowledgeChips>` | [h] |
| D12 | Empty-conversation hint | `<Conversation>` | [ ] |

## E. Hypothesis tracker (`02 §2.9`, `adr/0047`, `0051`, `0054`, `0075`)

| # | Behavior | New home | Status |
|---|---|---|---|
| E1 | One card per hypothesis, live first; head = handle · status pill (self-explaining tooltip) · tested/total (withdrawn excluded); claim in full | `<HypothesisCard>` in `<ReasoningLens>`: head = handle · tally · status; the claim in full; one-line `<PredictionRow>`s | [h] |
| E2 | Handle click → rename (ack with handle on PROPOSED; revise on OPEN) | `<HypothesisCard>` | [h] |
| E3 | Adopt on PROPOSED with the statement editable; frozen wording when any prediction is tested | `<HypothesisCard>` + `<PromptCard>` | [h] |
| E4 | ⚡ Propose tests / ⚡ Run tests / ⚡ Decide by state; button disables with a working label; rivals brief carried | `<HypothesisCard>` | [h] |
| E5 | Decide guard note beside ⚡ Decide naming the blocking `P<n> (H<m>)` | `<HypothesisCard>` | [h] |
| E6 | + prediction: statement, discriminates checkboxes (single rival pre-checked), "A person tests this" | `<PredictionRow>` expanded: Withdraw · Record result · ⚡ Test · Pin | [h] |
| E7 | Restate (OPEN, nothing tested); Abandon with optional reason | `<HypothesisCard>` | [h] |
| E8 | New hypothesis (lands OPEN, handle `H<n+1>`) | `<ReasoningLens>` “+ Add a hypothesis” | [h] |
| E9 | Predictions section names itself (two sentences) / empty text by liveness | the empty-state line on the card; the row anatomy of `08 §8.4` replaces the two-sentence header | [h] |
| E10 | Prediction row: `P<n>`, statement, status pill, `vs H<m>` chip, "a person tests this" chip, "undecided until tested" chip, test-result ref chips (open), attestation line (author by name, channel, source, quote) | *superseded by one-line rows*: `<PredictionRow>` at rest + expanded (`08 §8.4`) — every element retained in the expansion | [ ] |
| E11 | UNTESTED row: Withdraw (reason) · Record result (dialog: outcome, prefill from a comms reply, who, how, text) · ⚡ Test P<n> (not on manual) | `<PredictionRow>` expanded + `<ResultDialog>` | [ ] |
| E12 | Decided row: Pin (prefilled finding; inert when pinned) | `<PredictionRow>` expanded | [ ] |
| E13 | Status badge tones (ok / bad / warn / muted) | `<Pill>` | [ ] |

## F. Pins

| # | Behavior | New home | Status |
|---|---|---|---|
| F1 | Pin dialog (finding, refs shown, up to 12 + "more"), from tool results, predictions, entity, inspector | `<PromptCard>` | [h] |
| F2 | Pinned list with count; actor word; time; chips with amber inset; superseded struck; un-pin with reason | `<ReasoningLens>` pinned-evidence fold (the canonical place; un-pin there) | [h] |
| F3 | Pin state refreshes every pin affordance both ways | store-derived | [ ] |

## G. Approvals and the action ledger (`03 §3.3`, binding §2.3)

| # | Behavior | New home | Status |
|---|---|---|---|
| G1 | Pending queue: pending rows + EXPIRED rows without a successor; hidden when empty | *relocated*: `<ResponseLens>` cards in every state + `<Header>` “Review N proposed actions” | [h] |
| G2 | Card: type, tier pill (T2 square / T3 diamond), pending label / EXPIRED, targets, planned adapter ("will dispatch via"), notify pre-send preview (subject, body, follow-up marker), retry lineage, escalation reason, reversibility word, evidence chips, live countdown (warn < 5m, due) | `<ActionCard variant="full">`: tier · reversibility pill, status pill, targets, Because chips, the fact grid (Undo · Runs via · Response policy · Window), lineage, escalation, error, the notify pre-send preview | [h] |
| G3 | Approve (T2) / Approve (challenge)… (T3 typed challenge in the prompt card) / Approve · Send (notify) / Reject… (reason) ; one decision in flight | `<ActionCard>` buttons; one decision in flight per actions slice | [h] |
| G4 | EXPIRED: 0.7 opacity, note, ↻ Re-request (rationale) → new action with lineage | `<ActionCard>` | [h] |
| G5 | Ledger: every action newest first, ↺ reversal marker, status badge mapping (DISPATCHING for EXECUTING…), via adapter → result ref, REVERSED / best-effort line, escalation line, error detail on FAILED / TIMEOUT / PARTIAL | *relocated*: the Response lens IS the ledger (every state, grouped Containment → Eradication → Recovery → Notification → External work); the ledger marker, via adapter → result ref, error, best-effort note live on the full card | [h] |
| G6 | Dispatch watch: poll while APPROVED/EXECUTING, bounded | host unchanged | [ ] |
| G7 | Reversal: request the inverse action on a SUCCEEDED reversible original | `<ActionCard>` Reverse… on a SUCCEEDED reversible original → `action.reverse` (host resolves the inverse type from the catalog) | [h] |

## H. Comms / external work (`04`, binding §4)

| # | Behavior | New home | Status |
|---|---|---|---|
| H1 | Thread cards: target with channel glyph, status pill, subject, last trail entry quoted (inbound green), sent/follow-up meta, escalation callout | `<Comms>` under Notification in the Response lens | [h] |
| H2 | Acknowledge · Follow up… (new notify action with thread lineage) · Snooze 24h · Mark done | `<CommsThreads>` | [h] |
| H3 | Conclude warning when threads await replies | `<Header>` Conclude helper lists the preflight (comms threads awaiting replies named) | [h] |

## I. Verdict (`02 §2.10`)

| # | Behavior | New home | Status |
|---|---|---|---|
| I1 | Dialog: checklist (pins; refs attached; revising a prior verdict), disposition radios, rationale, residual (unavailable verbs, undecided hypotheses with untested-discriminator counts, untested predictions, "no known gaps") | `<VerdictDialog>` | [h] |
| I2 | Submit disabled until disposition + rationale + a pin; refs = all active pins' inputs | `<VerdictDialog>` | [h] |

## J. Capabilities and enablement (`11 §5.1`)

| # | Behavior | New home | Status |
|---|---|---|---|
| J1 | Collapsed fold with available/total; rows sorted by status; dot tone; "capability layer off" / "none configured" | `<LeftRegion>` capability fold at the bottom | [h] |
| J2 | "set up" on an unavailable verb an installed-not-enabled adapter could serve | `<LeftRegion>` | [h] |
| J3 | Enable form: schema-derived fields, required marks, descriptions, secret note; confirm → host native modal + secure input for secret references → apply → reload | `<EnableForm>`; host `applyEnablement` unchanged | [h] |

## K. Derived lenses over citations

| # | Behavior | New home | Status |
|---|---|---|---|
| K1 | Event-time strip (dots by event time, open on click, span label) | *superseded*: `<StoryLens>` timeline rows in event time (the strip stays as the span overview) with citing predictions and the start divider; sentences wait for §6.12 | [h] |
| K2 | Minimal two-layer evidence graph (STIX left, OCSF right, grounding edges tinted by actor, join edges dashed, ungrounded reasoning nodes flagged, click opens) | `<StoryLens>` under Attack path (the path itself waits for §6.13) | [h] |

## L. Citations, chips, popover, evidence

| # | Behavior | New home | Status |
|---|---|---|---|
| L1 | Ref chip: type label from the id prefix, enriched with the resolved value (`host · WIN-FILE01`), palette class per type, full id in tooltip; label cache shared across panels | `<Chip>` | [h] |
| L2 | Entity popover: type, value, deterministic id, appearances in other investigations (open on click), Pivot (stages a composer question), Pin, Open raw JSON, Copy value | *superseded*: `<EntityInspector>` (pivot, pin, copy, appearances, properties, raw) | [h] |
| L3 | Evidence card panel: title by type, badges, provenance line, generic depth-limited field renderer, cross-investigation memory, raw JSON disclosure, "Open raw JSON tab" | *superseded*: `<EvidenceInspector>` Details · Provenance · Raw (same generic field renderer, raw JSON with copy, appearances on the entity inspector) | [h] |
| L4 | Attestation (`note`) and null observation (`x-null-observation`) titled as human statement / "Nothing observed: … (coverage complete)" | `<AttestationInspector>` (the statement, who said it, who recorded it, the question it answers) and `<AbsenceInspector>` (Search · Provenance · Raw, re-run wider) | [h] |
| L5 | Transcript opens for a thread step | *superseded*: `<TurnInspector>` Answer · Tool calls · Consulted · Transcript | [h] |
| L6 | Every ref type the current chips can carry opens something: event, observed-data, null observation, indicator / sighting, note, prediction, hypothesis, entity types | every ref type resolves to a renderer (`derive/inspect.ts`): evidence, entity, absence, detection (with `related`), attestation, prediction, hypothesis, alias; action, thread, case, turn, person, procedure by target kind | [h] |

## M. Infrastructure

| # | Behavior | New home | Status |
|---|---|---|---|
| M1 | Prompt card: title, label, textarea, refs, choices, extras (select / text / check, onChange API), require flag, Cmd/Ctrl+Enter, Esc | `<PromptCard>` | [h] |
| M2 | Resizable rail width persisted in webview state | *superseded*: the companion has the `08 §8.1` width floors (340–420px) and becomes a drawer below 1024px; no analyst-resized width | [h] |
| M3 | Loading dim; error banner "Could not load" | `<LensArea>` banner; `body.loading` | [h] |
| M4 | Light theme token overrides | `tokens.css` | [ ] |
| M5 | CSP: no network from the page; nonce script | host `html()` (§5.1) | [h] |
| M6 | Post-conclude: knowledge-summary narrative (fire-and-forget) | host `lifecycleTransition` unchanged | [ ] |
| M6b | Post-conclude: candidate-SOP draft → editor tab → Add to SOP library / Discard; a `warranted:false` draft shows nothing | *superseded* by the Candidates section of the Knowledge document (`09 §9.7`, `workbench-react.md §8.13`): the host stores the draft, no prompt; the editor-tab path stays until §6.24 lands | [ ] |
| M7 | Export markdown opens in an editor tab beside | host `openMarkdownExport` unchanged | [ ] |
| M8 | Copy to clipboard | host | [h] |
| M9 | Open another investigation from a link | host `investigation.open` | [h] |

## N. Import SOPs panel (`importView.ts`, `06 §2.4`, `13 §3`)

Behaviors of the current Import SOPs panel, which becomes the Import section of the Knowledge
document (`09 §9.5`, `workbench-react.md §2.6`); `importView.ts` is deleted when every row is ticked.

| # | Behavior | New home | Status |
|---|---|---|---|
| N1 | `reckon.importSOP` command opens the panel (never the quick input) | the command keeps its id and opens the Knowledge document at Import | [ ] |
| N2 | Add documents through the native file picker (multi-select); `file:` and `untitled:` URIs only | `<Import>` → `import.choose`; folder choice added per `09 §9.5` | [ ] |
| N3 | One row per document: checkbox, parsed title, filename; toggle rows in and out | `<Import>` rows (`import.toggle`) | [ ] |
| N4 | Import the checked rows; each `POST /api/sops/import_markdown` with the raw markdown | host `import.run` → `importSOPMarkdown` unchanged | [ ] |
| N5 | Per-document outcome: created ("Added to the SOP library as …"), revised ("Revised existing SOP …"), failed with the server's message | `<Import>` outcome badges: added · revised (with version) · failed | [ ] |
| N6 | Refresh re-reads the chosen documents from disk | `import.refresh` | [ ] |
| N7 | "Imported — consultable now on the knowledge rail" line | reworded to name the Knowledge section | [ ] |
| N8 | Parsed frontmatter shown before import; missing recommended fields flagged | *new* (`09 §9.5`): dry run, `workbench-react.md §8.0` | [ ] |
