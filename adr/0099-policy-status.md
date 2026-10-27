# 0099 — The Gate 2 evaluation is folded onto the action and served with it, so a card states whether a policy pre-authorizes the action and where its approval routes

Issue: #99 · PR: #100 · Status: accepted

## Context

Gate 2 evaluates the tenant's policies when an action is requested and records the
evaluation as `action.policy_evaluated` in the same transaction (`04 §4`), but the action as
listed carried none of it. A REQUESTED card could not say that no policy pre-authorizes the
action, that a second approver is needed from a named pool, or that a shadow policy would have
fired (`04 §4.4`); the card guessed its sentence from the frozen `required_mode` alone. Gap 16
of `design/ui/00-backend-binding.md §6`; `08 §8.4` puts the policy fact on every card.

Constraints: the authorization requirement is frozen at request time and the evaluation is
already on the event log; a surface renders facts the engine holds (`00 §1`) and a REQUESTED
action a policy auto-approved does not exist (the endpoint approves and dispatches it), so
"pre-authorized" beside an Approve button is not a state to render.

## Decision

- **`action_current.policy`** is the projection of `action.policy_evaluated` folded with the
  request's frozen requirement (`foldActionPolicy`, pure): the `effect` that drove
  authorization (`NONE` when no policy matched and a human decides; `AUTO_APPROVE`;
  `REQUIRE_TWO_PARTY`; `DENY`), the matched policy and its version, `routes_to`
  (`requester` or `pool`, with the pool) when a human decides, and the shadow policies that
  would have fired. The projector handles the evaluation event in the same transaction as the
  request; a replay rebuilds the column.
- **`ActionView.policy`** serves it on the actions list; the action record keeps the full
  evaluations as before.
- **The card renders the fact**: a pending card reads "No policy pre-authorizes `<type>` ·
  needs your approval" or "Two-party (policy …): a second approval from <pool>", with "a shadow
  policy would have pre-authorized this (<ref>)" when one would; an approved card reads
  "Auto-approved by policy <ref>" when the policy decided, "approved" otherwise. An action
  requested before the fold has no `policy` and the card falls back to the frozen requirement.

## Alternatives rejected

- **Re-evaluating policies at read time** to predict what would happen: a prediction, not a
  record, and it drifts from what actually authorized the request.
- **Reading the evaluation from the event log per card**: the list view is the hot path and
  the projection exists for exactly this.
- **A "pre-authorized" state on a pending card** (the mockup's wording): not a state the engine
  has.

## Consequences

- Migration `0027`. Earlier actions carry no `policy` until a replay.
- The conclude preflight and the Summary's still-to-do can read `routes_to` to say who is
  waited on; not changed here.
