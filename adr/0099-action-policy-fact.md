# 0099 — A requested action's card states the frozen Gate 2 requirement from the list row; the policy decision stays on the record

Issue: #99 · PR: #305 · Status: accepted

## Context

Gate 2 evaluates the tenant's policies when an action is requested (`04 §4`) and
writes the outcome as the `action.policy_evaluated` audit event in the same
transaction as the request: every matching policy, whether it fired, whether it
was shadow, and the one non-shadow effect that drove the mode. The decision is a
fact of the request, frozen like its reversibility, its tier escalation and its
dispatch route, which the card states before approval (`design/ui/03 §3.3`).

Half of that fact already sits on the action projection: `required_mode` and
`secondary_approver_pool`, frozen at request time so the approval surface
enforces two-party without re-evaluating policy (`08 §8.1`). The other half, the
matched policy, its effect and the shadow policies that would have fired, lives
in the audit event, which `GET /api/actions/{id}` replays into its
`authorization` block and the Action inspector renders in full.

The list the card reads, `GET /api/investigations/{id}/actions`, served
`required_mode` alone. Two REQUESTED cards, one where nothing matched and one
where a DENY fired beside a shadow auto-approve, read identically; a two-party
card said two people must approve and not from which pool. Gap 16 of
`design/ui/00-backend-binding.md §6` asks for the decision to be readable on the
requested action; `implementation/workbench-react.md §8.5` renders that as a
`policy` object folded onto the projection with five fields.

The fact is informational: the aggregate refuses a solo approval of a two-party
action and an approver outside the pool regardless of what a card says. Of the
three pieces the card lacked, the pool is the one that bears on the approver's
next step, since the first approver needs to know who completes it. The matched
policy restates the tier while the only DENY in force is the baseline rule that
an AI-requested T3 never auto-approves; the shadow outcome is the policy
author's audience during a rollout (`04 §4.4`), not the approver's.

## Decision

**The list row serves the pool and the first approver.** `ActionView` gains
`secondary_approver_pool` and `primary_approver_ref`, read from the projection
where the request and the first approval wrote them. No new column, no
projector change: `action.policy_evaluated` stays audit-only and the record
endpoint keeps replaying it. The list and the record agree by construction on
what both serve, since both read the projection; the matched policy and the
evaluations are served once, by the record.

**The card states the human approval still needed, and nothing the row cannot
support.** The Approval row of a pending card reads "needs your approval" for a
manual request; "two-party · a second approval from one of <pool>" for a
two-party one, or "a second analyst must also approve" when the policy froze no
pool (the engine holds no rotation to name: the approve path then accepts any
distinct analyst); once the primary approval is in, "awaiting a second approval
from one of <pool less the first approver>", since two-party needs two distinct
humans (`04 §3.3`), and when that leaves nobody, it says so. Any other pending
row, one frozen `AUTO_POLICY` included, reads "needs your approval". Settled
cards keep their words.

The row carries the requirement, not the evaluation, and the two differ. A
pending row frozen `AUTO_POLICY` does not say why the approval did not happen:
the server holds a pre-authorized request on a draft investigation, leaves it
for a person when the policy names no accountable human or the approval write
fails, and an edit keeps the frozen mode when the edited request stops
matching. An edit never relaxes a frozen MANUAL either, even when the edited
request matches an auto-approve policy. So the card makes no claim about
whether a policy matched or why it did not fire; that is the record's to say,
one click away.

**The decision is one click away, not duplicated.** The Approval row links to
the action record, whose Authorization section already shows the matched
policy, its effect and every evaluation with its shadow outcome. The card does
not carry a second copy of what the record holds.

## Alternatives rejected

- **The §8.5 `policy` object folded onto the projection.** A migration, a
  projector that reads the audit event it deliberately skips today, and a
  five-field object on every row, to put on the card two facts the approver does
  not act on and the record already serves. The field list was one rendering of
  gap 16, which asks that the decision be readable on the requested action.
- **Reading the policy event at list time.** Serving the matched policy and the
  shadow outcomes on the list by joining the event stream per row would make the
  list as expensive as the record for every action, pending or not.
- **The card fetching the record for pending actions.** A second read per card,
  racing the snapshot the card renders from; the record is already behind a
  link the card has.
- **The first approver's name on the awaiting-second strip** (`design/ui/03
  §3.3`'s "first approval by …") is not on the list row and is left as is.

## Consequences

- One field on the list view and its decoder; the card's Approval row is a
  function of the row (`approvalFact`), tested on its own.
- A card can be wrong only where the row is wrong, and the row is the projection
  the aggregate enforces from; a two-party card and the approve path read the
  same frozen pool.
- The matched policy and the shadow outcomes have one home, the record. A
  surface that needs them opens it. If the shadow outcome ever becomes a
  decision-time fact (a shadow policy promoted mid-rollout, say), the §8.5 fold
  is the next step and nothing here stands in its way.
