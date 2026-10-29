# 0127 — A requested action can be edited before approval, as the same action, on the record

Issue: #127 · PR: #TBD · Status: accepted

## Context

A pending `notify.*` or `ticket.*` card could only be approved or rejected as
worded. Changing a word meant rejecting the request and asking the agent
again — a second action, a second approval, and a rejection on the record that
was never a disagreement with the action itself. The road test hit it with a
placeholder recipient and a draft the analyst wanted to soften.

The constraints: the record is event-sourced and an action's history must
show what was approved, not only its final text; Gate 2 decided the
authorization requirement over the request as it was (`04 §4`), so a change of
content must be decided again; an AI delegate never changes what goes out
under a human's approval (`04 §5.6`); and the engine's write vocabulary stays
vendor-neutral.

## Decision

- **A new aggregate command and event, `EditAction` → `action.edited`**, paired
  with an `action-edit` interpretation ("<action>: edited before approval:
  <reason>"). It replaces the action's parameters and keeps the prior ones in
  the event (`prior_parameters`), so the record shows the change.
- **Same action.** Id, targets, tier, evidence and approval window are
  unchanged; only parameters move. Legal only while the action is REQUESTED —
  once a first approver signed a two-party request, its content is frozen.
- **Human only.** `EditAction` is not in the AI command allowlist, and the
  endpoint refuses a delegate token.
- **Gate 2 runs again** over the edited request, and its evaluation is recorded
  in the same transaction (`action.policy_evaluated`). A requirement can rise
  (to TWO_PARTY); an edit never approves on its own, even where a policy would
  auto-approve — the analyst approves the text they edited.
- **Parameters are checked against the type's declared inputs**, as on
  request (`action.ValidateParameters`).
- `POST /api/actions/{id}/edit {parameters, reason}` (analyst); the actions
  list carries `edited_at` and the type's editable `inputs`; the record's
  history line names each changed field, before → after.
- **Vendor-neutral ticket priority:** `ticket.create` declares `priority`
  (low | medium | high | critical). The ServiceNow adapter maps it to urgency
  when no urgency is given.
- The workbench card offers **Edit…** on an open request with declared fields:
  a form of those fields, prefilled, the longest text as the main box. An
  edited card says so and links to what changed.

## Alternatives rejected

- **Reject and re-request.** Two actions and a false rejection on the record
  for a wording change.
- **Edit by superseding the request with a new action.** Lineage would read as
  a retry; approvals and windows would reset for a change of words.
- **Let the edit change targets.** A different target is a different action
  with a different blast radius; that goes through a new request.
- **Honour auto-approval after an edit.** The edit is the analyst's own act;
  it should not dispatch without their approval of the result.

## Consequences

- The action event set grows to nine; projections rebuild from the stream as
  before (`edited_at` via migration 0025).
- Other ticketing adapters map `priority` when they are added.
