# 0079 — The investigation document is organized by stage, the conversation is a companion, and every citation opens in an inspector

Issue: #79 · PR: #80 · Status: proposed

## Context

The investigation document grew one surface at a time — chronicle, hypothesis tracker,
pinned evidence, the conversation, the event-time strip, the evidence graph — and by the
time an investigation reaches a verdict they compete for the same width with the same
visual weight. Read on the reference scenario (`fixtures/lateral-movement-via-rdp/`) at
`VERDICT_REACHED`:

- **The same objects render three times.** A prediction appears truncated in the
  chronicle, in full in the tracker, and again under pinned evidence. The analyst
  reconciles three copies instead of reading one.
- **Engine bookkeeping sits in the reading path.** Sequence numbers, "committed aside"
  lines, attestation principal ids and per-row timestamps are interleaved with the
  analyst's question and the agent's answer.
- **Every row is chip-dense.** Status, rival, evidence count, entity chips and the pin
  marker share one style, so nothing on a row reads first.
- **The next step is prose.** Asked what the procedures require, the agent answers in
  paragraphs that name `host.isolate` and `account.disable`; the actions themselves are
  elsewhere.
- **The layout does not change with the stage.** After the verdict the screen still
  presents investigation work, though the analyst's job has become containment.
- **A citation opens a raw JSON editor tab.** "Every citation opens" (`design/ui/README`
  non-negotiable 8) is honored mechanically, but the analyst lands outside the
  investigation, in a file view of a single record, with no account of why it was cited.

`design/ui/00-backend-binding.md §7` already records the underlying questions as open:
panel vs document, structural navigation ("the thread is the right audit lens but not the
only working lens"), and the two clocks. Constraints that bind the answer: the record is
the engine's (`00 §1`), engine vocabulary is rendered verbatim (`00 §2`), the workbench
discipline (`design/13-workbench.md §3`), and the competing-set tracker with the decide
guard shown where it bites (`adr/0047`, `adr/0051`).

## Decision

The full specification is `design/ui/08-investigation-layout.md`; as facts about the
surface:

- **The investigation document has four lenses: Summary, Story, Reasoning, Response.**
  Summary is the pick-up view (where it stands, what happened, key entities, what is left).
  Story is event time (an attack path over the entities, and the timeline). Reasoning is the
  competing set. Response is the action plan, owner action items and outbound notification.
  The default lens follows the derived lifecycle (`00 §2.4`): Reasoning while `ACTIVE`,
  Response from `VERDICT_REACHED` through `REMEDIATING`, Summary once `CONCLUDED`.
- **The header states where the investigation is and what is next**: title, verdict, a
  stage indicator over the derived lifecycle, and one primary action for the current stage.
- **The conversation is a companion column, not the document.** It holds the analyst's
  turns and the agent's prose. What a turn records (a prediction result, a proposed action)
  renders as a link to that object, not a restatement. Tool calls and record-keeping fold
  into one per-turn activity disclosure; the tool-call block (`02 §2.3`) lives inside it.
- **Proposed actions render as action cards wherever the agent proposes them**, including
  in the conversation, never as prose naming a verb.
- **Every citation opens in the inspector**, a panel over the document's right edge with a
  typed renderer per object (evidence, entity, procedure excerpt), a statement of why the
  object is in this investigation, a back stack, and pinning. Raw JSON is a tab inside it.
  No citation opens an editor tab.
- **Prediction rows are one line at rest** (status glyph, ordinal, a short title, a
  discriminator marker, evidence count) and expand in place. The Reasoning lens adds a
  matrix view of the same set.
- **People are named, ids are on hover.** Attestations and approvals show the principal's
  display name (`adr/0077`).
- **The work queue keeps investigations and cases as separate sections.** A case already
  under investigation appears once, as its investigation, carrying its case reference;
  the case section lists only cases with no investigation yet (`adr/0030` unchanged).
- **The investigation webview moves to React,** incrementally: new surfaces are built as
  components first, starting with the inspector.

## Alternatives rejected

- **Keep the chronological document with the conversation panel beside it.** It is the
  right audit lens and stays available as the thread, but as the only working lens it is
  what produced the duplication and the missing next step.
- **Open evidence in read-only editor tabs (`13 §5` "editor-in-reach").** It takes the
  analyst out of the investigation and answers "what is this record" without "why is it
  here"; the editor remains in reach for analyst scratch work, not for citations.
- **One merged queue of cases and investigations.** A case is the system of record's
  object and an investigation is reckon's; merging them hides which one a row is, and the
  start-investigation act (`adr/0030`) is the boundary between them.
- **A full rewrite of the webview before any redesign ships.** Too long without analyst
  feedback; the component move rides along with the first new surface instead.
- **Preact.** Viable, but React keeps the components directly reusable by a browser
  surface over the same API.

## Consequences

- `design/ui/08-investigation-layout.md` is the new sheet; `02 §2.2`, `§2.8`, `§2.9` and
  `03 §3.4`, `§3.7` carry pointers to the sections of `08` that supersede them.
  `design/13-workbench.md §4`, `§5` and `§7` are amended for the new surfaces and the
  inspector.
- New engine work is listed in `00-backend-binding.md §6` (items 12–18): per-class event
  projections, an entity-scoped event query, entity baselines, excerpt-level procedure
  provenance with source locations and retained originals, response-policy status on
  requested actions, owner action items, and a narrative summary at any time.
- The open questions "panel vs document", "structural navigation" and "two clocks" in
  `00 §7` are resolved by this record; the layout floor remains open.
- `design/ui/mockups/` holds the reference screens for the reference scenario; they are
  illustrative, and where they disagree with the sheets, the sheets win.
