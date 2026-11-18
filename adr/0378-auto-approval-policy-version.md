# 0378 — An auto-approval records the content hash of the policy that fired, and the aggregate refuses one without it

Issue: #378 · PR: #424 · Status: accepted

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

Gate 2 also relied on policy ids it never checked. It notes the first
policy of each effect that fires by its id, and an empty id counts as
"nothing fired yet". So a `DENY` policy with no `id` fired unseen, and a
lower-priority `AUTO_APPROVE` could approve the action. Two policies could
also share an id, which leaves a record that names a policy by id
ambiguous.

The hash itself followed the Go struct, not the policy. It was the sha256 of
the `Policy` struct marshalled to JSON. Renaming a struct field or adding
one changed every policy's hash, with no policy edited. The marshal error
was ignored, so a policy that failed to encode hashed as empty input.

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
- **Every policy has its own id.** Gate 2 refuses to start with a policy
  that has no id, or with two policies that share one. A policy that
  reuses the baseline's id is refused too, whether it came from a file or
  from code.
- **The hash covers an explicit canonical form.** `04 §4.1` defines
  `content_hash` as the sha256 of the policy's canonical form. That form now
  lists each policy field by name, with times in UTC. A field left unset is
  left out, so a field added later changes no existing policy's hash. Gate 2
  computes each hash once, when it starts, and refuses a policy whose form
  cannot be encoded.

## Alternatives rejected

- **Looking the hash up in the evaluation rows when the approval is
  written.** It gives the same value by a longer path, and an approval
  written elsewhere would still depend on someone remembering to do it.
- **Re-hashing the policy file when approving.** The file can change
  between evaluation and approval. The hash must be the one that fired.
- **Leaving the guard out.** A later auto-approval path could drop the
  hash again, and nothing would notice.
- **A separate "fired" flag in Gate 2 instead of the id check.** It closes
  the fail-open, but the record would still name a policy by an empty or
  shared id.
- **Hashing the policy file's bytes.** A comment or a reformat would then
  count as a new version. Policies built in code, such as the baseline,
  have no file.

## Consequences

- An auto-approved action's record says which text of the policy approved
  it, without the evaluation rows.
- `design/04 §3.3` marks `policy_ref` and `policy_version` as required when
  the mode is `AUTO_POLICY`.
- A policy set with a missing or repeated id stops the backend at startup,
  as a predicate that fails to compile already does.
- Every policy's hash changes once, with the new canonical form. A test
  pins the form, so a later change to it is deliberate.
