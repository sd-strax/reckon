# 0433 — The conversation is the investigation's workspace; one panel beside it holds the case state and everything opened on demand

Issue: #433 · PR: — · Status: accepted · Supersedes: the lenses, the stage header, the
companion column and the turn-as-link rule of `0079`; the stage-step bullet of `0342`; the
home of the action history in `0361`; `ui/00 §2` for the words on screen; the pin control of
`ui/08 §8.6`

## Context

`adr/0079` built the investigation document around four lenses (Summary, Story, Reasoning,
Response). The conversation was a companion column beside them. A stage strip in the header
(Investigate, Verdict, Contain, Notify, Close) said where the case was and offered one
primary action.

Three things were wrong with that shape.

- **The work happens in the conversation, and it had the narrow column.** The analyst asks,
  the agent answers, and the lenses restate what the turns found. A turn showed a link to
  the lens, not the evidence itself.
- **The strip showed an order the engine does not have.** A verdict can be recorded at any
  time, given one pinned piece of evidence. An action can be requested and approved before
  any verdict. The strip and the stage-driven default lens suggested otherwise.
- **The screen spoke the record's language.** `ui/00 §2` says the screen uses the engine's
  words verbatim. So the analyst read "hypothesis", "prediction", `P3`, `UNTESTED` and
  `DISCONFIRMED` on every surface.

The constraints are these. The aggregate rules, the events, the projections and the audit
trail do not change. Nothing the agent reads changes. The workbench discipline of `13 §3`
holds. Nothing the lenses offered is removed: a thing may be folded or opened on demand, but
it stays reachable.

## Decision

`design/ui/10-realignment-wireframes.md` draws the screens. The decision is this.

- **The document has two regions: the conversation, and one panel on its right.** The
  conversation has the centre and the width. Nothing takes its place. It keeps the
  Conversation / Thread toggle and the composer.
- **A turn shows what it found.** The agent's reply carries its evidence rows inline, each
  one click from its record. An action the agent requests shows as its full card in the
  conversation, where it was asked for.
- **The panel shows the case state.** One scrolling column, in this order: the verdict and
  status, the open items, the pending approvals, the basis of the verdict (collapsed to one
  line), a line of counts for what happened, the key entities, the pinned evidence.
- **Everything else opens in the panel, in place of the case state, with a step back.**
  - **Timeline**, **Reasoning** and **Report** each open from the section they are the
    longer form of: what happened, the basis, the verdict. There is no tab strip.
  - **Reasoning** is the full explanation cards and the matrix. **Report** is the
    narrative, the verdict and its basis as they will export, what is still open, the
    procedures cited, the handoff note, the export and the procedure draft.
  - An **inspector** opens the same way. One is open at a time. One reached from a view or
    from another inspector steps back to it. The pin control is gone.
  - Nothing sends the analyst to a view. No stage switches one.
- **The header carries no stage.** It has the title, one count, **Verdict…** and
  **Conclude**. The count is the open items plus the pending approvals. Conclude is
  disabled until a verdict is on record.
- **An open-items list replaces the stage strip.** It holds the checks not done, the checks
  that need a person, the work waiting on someone outside, and what could not be checked.
  "Could not check" is always on the panel and is not in the count. Adopting an explanation
  is not an open item. It stays on the full cards.
- **The timeline holds the action history.** Telemetry rows sit at the time the event
  happened. Each step of an action is its own row at its own time: requested, edited,
  approved (or rejected, or expired), run, the result, reversed. Steps of one action in the
  same minute share a row. Query rows, one for each capability call at the time it ran, are
  off by default. Grouping and the merge of `adr/0348` apply to telemetry rows only. Actions
  waiting for approval stay on the panel.
- **The screen uses plain words, and the record keeps the engine's.** The sheet's "Words on
  screen" table is the one mapping. The Thread and every inspector's Raw tab show the stored
  words unchanged. The short codes (`P3`, `H2`) stay on hover, on the full cards and in the
  Thread.
- **The explanation that leads** is the supported one when there is one. Otherwise it is the
  one proposed first. The labels are "Leading explanation" and "Other explanations".
- **The webview reads `trust.ai_reasoning`.** With the dial on, the agent tests and decides
  what it proposed, and adopting is not a step. This decision does not change the dial's
  default.
- **A sidebar row shows the verdict status and the open count** in place of the stage step.
- **In a narrow window the panel is a drawer** over the conversation. That is the one width
  at which the conversation is covered.
- **Every derivation is a pure function** in the webview's `derive/` directory, over the
  view the server already sends.

The change lands in steps, each its own issue and PR. Until a step lands,
`ui/08-investigation-layout.md` describes the screen as built. Each step rewrites the
section of `ui/08` or `ui/02` it replaces.

## Alternatives rejected

- **Keep the lenses and widen the companion column.** The conversation would still be beside
  the work, and the stage strip would still assert an order.
- **Open the views in the centre.** Beside the conversation, three columns leave it too
  narrow on a laptop. In its place, the analyst loses the conversation to read a timeline.
- **A tab strip on the panel.** It makes the case state one of four equal tabs, and it is
  the lens row again in a narrower place.
- **One timeline row for each action, with its status.** The analyst would have to work out
  which state is the latest. The engine records each step as its own event with its own
  time, and the rows show that.
- **Keep the engine's words on screen.** One vocabulary cannot drift, and that was the reason
  for `ui/00 §2`. The cost fell on every reader of every surface. The record, the Thread and
  the Raw tabs keep the single vocabulary, and one table maps it.
- **Keep the stage step on the sidebar.** It would be the only place a stage is shown, for a
  sequence the document no longer has.
- **A simple mode and an expert mode.** Depth follows the case: a case with one explanation
  and every check done shows one line of basis, and a contested one shows more.

## Consequences

- Of `adr/0079`, these stand: action cards wherever the agent proposes one, one-line check
  rows, people shown by name with ids on hover, and the React webview. Of `adr/0342`,
  everything stands but the stage step on the row. `adr/0344` is untouched.
- Mockups `01` to `17` remain the record of `0079`'s layout. `18` and `19` show two screens
  of this one.
- A view in the panel gets less width than a lens had. Timeline rows put the time and the
  kind on one line and the sentence below. The panel can be dragged wider.
- While a view is open the open items are off screen. The header keeps the verdict status
  and the count, and a new approval still arrives as a card in the conversation.
- The agent's own prose still names explanations and checks by short code, because the state
  it reads names them so. Changing that means changing text the agent reads. That is listed
  as later work and is not part of this decision.
- With `trust.ai_reasoning` off, the agent stops at an explanation nobody adopted, and the
  default screen no longer shows why. The dial's default is therefore settled, as its own
  step, before the regions move.
- The sidebar's count may need the number of open checks in the list response.
- Tests that pin the lens row, the stage strip and the companion column change with the step
  that removes each.
- How work that waits on someone outside is tracked is a separate decision. The open-items
  list shows it from the records that exist.
