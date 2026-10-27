# 0101 — An owner action item is its own record, attached to the notification that asks for it, moved to sent by that notification's success, confirmed or waived by the analyst, and proposed but never closed by the agent

Issue: #101 · PR: #102 · Status: accepted

## Context

An investigation's response often includes work only an owner outside reckon can do: rotate a
credential, confirm a session, re-image a host, change a perimeter rule. It lived in the
notification's prose or the analyst's head: nothing tracked it to the owner's confirmation, the
Response and Summary lenses showed a gap line, and the conclude preflight could not count it.
Gap 17 of `design/ui/00-backend-binding.md §6` left open whether it is an `x-action` with an
external-work descriptor or its own record.

Constraints: an `x-action` is a state-changing dispatch under Gate 2 with a blast-radius tier
(`04 §1`) and a dispatch ledger (`08 §6b`); the comms threads already model "what follows a
sent message" as CRUD + thin history beside the event-sourced aggregate; the analyst decides
what is asked of whom, and the agent proposes (`01`: a delegate, never a principal).

## Decision

- **An owner action item is its own record**, `owner_items` in the main DB, CRUD + thin
  history, modeled on `comms/`: what, owner, why, status `OPEN | SENT | CONFIRMED | WAIVED`,
  who created it and whether as a human or an AI proposal, the notification it rides on, who
  confirmed or waived it with the note, and a history. It executes nothing, carries no blast
  radius and never dispatches, so it is not an `x-action`; forcing it through Gate 2 would
  invent a tier that measures no harm and a dispatch that never happens.
- **It is asked for through a notification.** "Add to notification" puts the item's ask into
  the draft and, once the `notify.*` action is requested, attaches the item to it; when that
  action SUCCEEDS the action-result path moves the item to SENT (the same place that opens the
  comms thread), so an item never claims to have been asked when the send did not happen.
- **The analyst closes it**: CONFIRMED with the owner's confirmation as a note, or WAIVED with
  a reason. Confirming, waiving and attaching are human acts; an AI-delegated token is refused.
- **The agent proposes.** `propose_owner_item` (title, owner, detail) creates an item marked
  `AI_DELEGATED`; the tool's description teaches when to use it and the base prompt does not
  change.
- **Endpoints**: `GET/POST /api/investigations/{id}/owner_items`,
  `POST /api/owner_items/{id}/confirm|waive|attach`. The snapshot carries `ownerItems`; the
  Response lens lists them with Confirm… and Waive…; the Summary's still-to-do and the stage
  derivation count OPEN and SENT items; the conclude preflight counts them as decision support
  (the engine-side gate lands with Phase F, as for comms); the Owner-item inspector shows the
  record and its history.

## Alternatives rejected

- **An `x-action` with an external-work descriptor**: the write side would carry a dispatch
  that never happens and a tier over nothing, and reversal, retry and expiry would all need
  carve-outs.
- **A line in the notification only** (the prior state): nothing to confirm against, nothing
  to count.
- **Letting the agent confirm from a reply it reads**: the owner's word reaches reckon through
  the analyst; the agent reports, the analyst records.

## Consequences

- Migration `0028`; `temporal.NewActivities` takes the store (the worker registers the struct
  wholesale, so a builder method is not available).
- The Summary's "what we need" line in the notification draft is assembled from OPEN items as
  well as pending approvals.
- Gap 18's handoff note and narrative read the items' status words.
