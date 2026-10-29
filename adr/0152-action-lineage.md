# 0152 — Each action has at most one re-request and one live reversal, enforced by the engine and shown on the original

Issue: #152 · PR: #168 · Status: accepted

## Context

A re-request (`retry_of`) and a reversal (`reversal_of_ref`) are new actions
that point back at an original (`04 §7`; `design/ui/00-backend-binding.md
§2.3`). The pointer lived only on the successor, and nothing checked it when
the request was made: the engine accepted a re-request of any id, in any
state, as often as asked, and checked a reversal's original only on the HTTP
path (it exists, it SUCCEEDED, the inverse type matches) — not whether a
reversal of it was already under way.

The card for the original could not see its successors either, so it kept
offering the step it had already taken. In the road test the expired CISO
email was re-requested four times from the same card (two approved and sent,
two rejected), and the isolation of WIN-FILE01 was reversed twice from the same
card while the first reversal was failing. The analyst approved each send in
full view of its preview, so no message went out unseen; the defect is the
branching chain and the stale button, not a duplicate the system made on its
own.

## Decision

- **The engine keeps every chain a line.** `applyRequestAction` checks lineage
  against the folded stream, inside the command's transaction (`checkLineage`
  in `aggregate/action.go`), so two requests racing on one original cannot
  both pass:
  - a **re-request** names an action of this investigation, of the same type,
    that is EXPIRED, FAILED or REJECTED — or still pending with its approval
    window elapsed, before the expiry timer has written EXPIRED — and has not
    been re-requested already. Each action has at most one re-request; the
    chain continues from its latest link. (The statuses match what the
    re-request endpoint already accepted; the deadline is folded into the
    aggregate's action state so the check can see it.)
  - a **reversal** names a SUCCEEDED action of this investigation with no
    reversal pending or done. A reversal that FAILED, was REJECTED or EXPIRED
    did not take effect, so the original may be reversed again.
  - a refusal is a domain rejection (422) naming the action and its successor.
- The folded action state carries `RetryOf` and `ReversalOf`; the actions list
  carries `reversal_of_ref` beside `retry_of`.
- **The original shows its successor.** The card reads "↻ re-requested as …"
  or "↺ reversal …" with the successor's status, linking to it, in place of
  the button. Re-request is offered only when the engine would accept it;
  Reverse is offered again, as "Reverse again…", only after a reversal ended
  without effect. One derivation (`successors`, `canRerequest`, `canReverse`
  in `derive/actions.ts`) feeds the card and the approval queue.
- **A fresh request is never limited.** Asking for the same message again
  through the conversation creates an action with no lineage, and the analyst
  approves it with its preview in view. Sending something twice on purpose is
  the analyst's call.

## Alternatives rejected

- **Hide the button in the workbench only.** Another client, the CLI or the
  agent could still branch a chain, and two quick clicks race the refresh.
- **Allow a second re-request from behind one that was rejected.** The
  rejection is the analyst's answer to that request; asking again from behind
  it reopens a decided question. A new request through the conversation is
  the way to ask afresh.
- **Refuse a second send of the same message to the same recipient.** The
  engine cannot tell an accidental repeat from a deliberate one (a reminder,
  a corrected recipient list), and the preview already puts the decision in
  the analyst's hands. A warning in the preview stays possible later, if road
  use shows repeats are being approved by mistake.

## Consequences

- Records written before this change may already hold branched chains (the
  road-test investigation has four re-requests of one email); they fold and
  render as before, and the card shows the first re-request as the successor.
- The re-request on a FAILED action is accepted by the engine but the card
  offers it only on EXPIRED, as before; offering it on FAILED is a separate
  change.
