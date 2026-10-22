# 0032 — The "opened in reckon" note on a seed case is an action in the new investigation, offered under a tenant posture that asks by default

Issue: #32 · PR: #33 · Status: accepted

## Context

When an analyst roots an investigation on a case, the case in the system of
record shows nothing. A colleague looking at the queue there cannot tell the
case is being worked in reckon. `design/14-case-seed.md §0` defers status sync
in general; this is its smallest useful piece: one note, with a pointer back.

The constraint is the write model. Every write reckon makes to an external
system is an x-action inside an investigation: validated against the action
catalogue, grounded by evidence refs, authorized by Gate 2, recorded against
a human principal (`design/04`, `08`). A note posted as a side effect of
creating the investigation would be a write with no action record, no
authorization decision, and no evidence, exactly the kind of silent write the
model exists to prevent.

The user's direction: offer it as configuration, and by default require the
analyst's confirmation at creation.

## Decision

- **The note is a `ticket.comment` action inside the new investigation.** The
  case is the single target, the promoted case read (`case_ref`, which the
  seed flow always produces, `14 §3`) is the grounding evidence, and the
  rationale names the posture that produced it. It goes through the same
  request path as every other action: Gate 2 evaluates, the request is
  recorded, and it is dispatched on approval or waits for one. Because the
  investigation is a draft at that moment, the first approval is also the
  activating one (`01 §Extension 2`): the note never auto-dispatches before a
  human has approved an action on this investigation.
- **A tenant posture, `cases.note_on_seed`**, validated at startup like its
  sibling dials:
  - `confirm` (default): the create response carries the offer with the
    default note text; the workbench opens the prompt card over the new
    document with the text editable; **Post note** requests the action,
    **Cancel** writes nothing anywhere.
  - `auto`: the workbench requests the action immediately, without asking.
    Gate 2 still decides whether it waits for approval.
  - `off`: no offer is sent, and the endpoint refuses.
- **One endpoint, `POST /api/investigations/{id}/case_note`**, builds the
  request server-side from the investigation's seed (the client never
  assembles an action body) and hands it to the shared action path. The
  default text names who opened the investigation, when, its title, and a
  `reckon://investigation/<id>` pointer, the same stable form the SOP import
  path uses to point at an investigation.
- **The posture is applied by the client that creates the investigation.**
  The server enforces `off`; `confirm` and `auto` describe what the creating
  surface does next. A creation path with no interactive client posts
  nothing, which is the honest outcome: nobody confirmed.

## Alternatives rejected

- **Post the note inside the create handler.** A write outside the action
  model: no x-action, no Gate 2 decision, no evidence, and a failure mode
  where the investigation exists but the note silently did not. Rejected on
  the model, not on effort.
- **Server-side auto mode** (the create handler requesting the action
  itself). It would need the action request path split out of its HTTP
  handler for one caller. Deferred until a second server-side requester
  exists; the client-applied posture gives the same result for the surfaces
  that create investigations today.
- **A free-form "annotate the case" action at any time.** That is the
  existing `ticket.comment` action, already available to the analyst and the
  agent. This decision is only about the one note that marks the handoff.
- **Full status sync** (close the case when the investigation concludes,
  mirror state). Still deferred (`14 §0`); it is workflow policy, and this
  note does not prejudge it. The default text promises an update at
  conclusion, which the conclude-time handoff ticket (`07`) already provides.

## Consequences

- Under the default posture the demo's Act 1 gains a beat: the note prompt
  appears after **Start investigation**; posting it puts the first pending
  action on the tracker, which the audience sees approved later as the
  activation moment.
- The note's text is editable before posting but not after; a comment is
  part of the case's permanent record (`ticket.comment` is irreversible).
- When a server-side creation path appears (a webhook trigger, `14 §0`), the
  `auto` posture will need the action request path refactored into a
  non-HTTP core. That refactor is the price of this decision's simplicity,
  paid later only if needed.
