# 0361 — The Response lens puts pending decisions on top and everything decided on one timeline, with detail on demand

Issue: #361 · PR: #363 · Status: accepted

## Context

The Response lens showed every action as a full card, grouped by phase (containment, eradication, recovery, notification, case record). Every card carried every fact at the same weight:
- a header with the title, the type id, the targets, a tier and reversibility pill, and a state pill
- a "Did" or "Will do" box that repeated the title
- a row of evidence chips
- a four-column facts grid (can be undone, runs on, approval, time to decide), mostly "yes" and "—" once an action had settled

A failure was hard to pick out among cards that looked like it. Grouping by phase lost the order in which things happened, and that order matters when an analyst reconstructs the response or hands it over.

A pending card also offered "Decide later". That button set a view-only flag (`aside`, saved in the webview's state) and moved the card into a "Set aside" section. The engine never saw it, and the request stayed open until its window closed whether or not the button was pressed.

`design/ui/03-remediation.md §3.3` says a requested card must answer the decision: the approval countdown, reversibility, the escalation reason, the dispatch route and the evidence.

## Decision

- **Two sections.** "Needs your decision" holds the pending actions, the one whose window closes soonest first. "Timeline" holds every other action in the order it was requested, oldest first by default. A toggle switches to newest first, and the choice is view state.
- **A pending card at rest** shows:
  - the group tile, the title and the target chips
  - the time it was requested and the live countdown
  - the will-do sentence, ending with the dispatch route ("via okta")
  - reversibility in words, and the escalation reason when the tier was escalated
  - the approval requirement, when it is more than the reader's own approval: a two-party request, or one awaiting a second approver after the first approval
  - Approve, Edit… (only when the request has inputs that are not entities) and Reject…, below the sentence and not in the header
- **A pending card's details** hold the evidence it was based on, the type id, the tier, the approval requirement with the policy decision link, and the action record link.
- **A timeline item at rest** is one line: the request time, the group tile, the title, the targets, and the status as a glyph plus the engine's word (`SUCCEEDED`, `FAILED`). Tier and reversibility are not pills on it.
- **Items that need attention open at rest.** A `FAILED`, `TIMEOUT`, `PARTIAL` or `OUTCOME_UNKNOWN` item, and an `EXPIRED` one that can be re-requested, stay at their place in the timeline as an open card. A failure that a later action has taken over is handled, so it is one line with a marker linking to that action, which is its own item. A later action takes over a failure when it re-requests it, when it reverses a partial result, or when it is a later reversal of the same original as a failed reversal. It must not itself have been rejected or left to expire. The card says what the outcome means for the target, with Resolve… or Re-request where they apply.
- **A timeline item's details** hold:
  - a history line: requested, then approved (with the time, when the action has one) or rejected or expired
  - the type id, tier, adapter, approval, evidence and the raw tool error
  - the message for a notification
  - Reverse…, the retry and reversal successor links, and the action record link
- **The phase is the group tile**, not a section heading.
- **"Decide later" and the "Set aside" section are removed,** along with the saved `aside` view state.
- **The action list carries its times and its outcome.** `GET /api/investigations/{id}/actions` adds three fields to each row:
  - `requested_at`, the projection's `created_at`
  - `approved_at`, its `primary_approved_at`
  - `outcome`, the outcome on record: the result's final outcome (`SUCCEEDED`, `FAILED`, `PARTIAL` or `TIMEOUT`), or a person's resolution of an unknown one. It is empty until one is recorded. The action record carries the same field.

  The row needs the outcome because the status folds a partial result into `SUCCEEDED` and a timeout into `FAILED`. Whether an item needs attention is read from the outcome, or from a `FAILED` status when there is no outcome. A partial or timed-out item shows the outcome word in place of the status word. A later status such as `REVERSED` still wins. Every place the workbench says what happened reads the outcome: the status word, the handoff note, the Notification tile ("partly notified", "notification unconfirmed") and the case note. The stage strip and the dependency rules follow the status, as the server does.
- **The compact card** in the conversation shares the new header. Its body keeps its own layout, but it also carries the pending card's decision lines: reversibility in words, the escalation reason, and the approval requirement when it is more than the reader's own approval. The header no longer carries them, and the compact card has Approve too.

`design/ui/03-remediation.md §3.3` and the Response part of `design/ui/08-investigation-layout.md §8.4` are amended to match.

## Alternatives rejected

- **Keep the phase groups and sort by time inside each.** Most incidents interleave phases, so the order of events would still be split across sections.
- **Place items by when they ran or settled.** That is closer to "what changed when". But the projection records neither time, so it would need a migration and new projector writes. The request order is already the projection's order and reads well enough for v0.
- **Approve and Reject in the card header.** It saves one row. But it lets the analyst approve from the title, before reading what the action will do. The header also already holds the countdown.
- **Keep evidence at rest on a pending card,** as §3.3 asked. Evidence is why the AI asked. The analyst checks it when unsure, not on every decision, so one click away is enough. The countdown, the reversibility, the escalation and the route say what approving will do, and they stay at rest.

## Consequences

- At rest, only a pending decision and an item that needs attention (a failure, an unknown outcome, a re-requestable expired request) are cards with a coloured border. Everything else is one line.
- The timeline shows request time, not run time. Two actions requested together and approved minutes apart stay in request order. A later change can add dispatch and settle times to the projection and re-anchor the timeline without changing the layout.
- An old saved view state with an `aside` key is ignored.
