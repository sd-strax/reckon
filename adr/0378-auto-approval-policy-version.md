# 0378 — An auto-approval records the content hash of the policy that fired, and the aggregate refuses one without it

Issue: #378 · PR: pending · Status: proposed

## Context

When a Gate 2 policy auto-approves an action, the approval is an
`Authorization` with mode `AUTO_POLICY` (`04 §3.3`). It names the policy in
`policy_ref`. `04 §3.3` also defines `policy_version`, the content hash of
the policy at the time it fired. The approval left it empty.

The hash was computed. Gate 2 evaluates each policy and records its
`ContentHash()` on that policy's row of `action.policy_evaluated`. But the
`Decision` Gate 2 returns names the matched policy by id only, so the
auto-approval had no hash to write.

A policy file can be edited while keeping its id. So the approval alone
could not say which text of the policy approved the action. A reader had to
find the matching evaluation row to learn it.

Nothing stopped an `AUTO_POLICY` approval with no policy at all. The
aggregate checks mode, stage and approver, but not `policy_ref` or
`policy_version`.

## Decision

- **The decision carries the matched policy's hash.** `Decision` gains
  `MatchedPolicyVersion`, set whenever a policy drives the decision (deny,
  two-party or auto-approve). It is the same `ContentHash()` value written
  on that policy's evaluation row, from the same evaluation pass, so the
  two are always equal.
- **The auto-approval writes it.** The `AUTO_POLICY` `Authorization` sets
  `policy_version` from the decision, beside `policy_ref`.
- **The aggregate refuses an `AUTO_POLICY` approval without both.**
  `ApproveAction` with mode `AUTO_POLICY` and an empty `policy_ref` or
  `policy_version` is rejected. It is the single check every path to an
  auto-approval goes through.

## Alternatives rejected

- **Looking the hash up in the evaluation rows when the approval is
  written.** It gives the same value by a longer path, and an approval
  written elsewhere would still depend on someone remembering to do it.
- **Re-hashing the policy file when approving.** The file can change
  between evaluation and approval. The hash must be the one that fired.
- **Leaving the guard out.** A later auto-approval path could drop the
  hash again, and nothing would notice.

## Consequences

- An auto-approved action's record says which text of the policy approved
  it, without the evaluation rows.
- `design/04 §3.3` marks `policy_ref` and `policy_version` as required when
  the mode is `AUTO_POLICY`.
