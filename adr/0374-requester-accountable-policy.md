# 0374 — A policy can credit the requester, and the install ships one that approves a person's own ticketing

Issue: #374 · PR: (to be opened) · Status: accepted

## Context

A fresh install asked a person to approve every ticketing action. That included the tickets the same person had just asked for. Gate 2 can auto-approve through a policy (`design/04-action-authorization.md` §4). But `reckon init` loaded no policies, and the shipped example under `examples/policies/` is off.

Two rules bound the answer:

- Every approval names a human (`design/04` §3.3). An `AUTO_POLICY` approval names the policy's last signer, else its author. An install has no such person when `reckon init` runs, because users exist only once the identity provider is up.
- AI is a delegate, never a principal. An AI-delegated request carries a human principal, but that person did not decide anything.

## Decision

- A policy has an optional `accountable` field, `author` (the default) or `requester`. Under `requester`, an auto-approval names the person who requested the action as `primary_approver_ref`. The approval carries that person's display name.
- A `requester` policy fires only for a `HUMAN` requester with a principal id. `Gate2.Evaluate` enforces this, so no predicate can credit the principal behind an AI-delegated request. The policy evaluation records `would_have_fired: false` for such a request.
- `requester` is valid only on `AUTO_APPROVE`. `NewGate2` refuses it on other effects, and refuses any unknown value.
- The binary embeds `policies/ticket-own-auto-approve.policy.yaml`. It matches `ticket.*`, its predicate is `requested_by.kind == "HUMAN" && tier == "T2"`, and it has `accountable: requester`.
- `reckon init` writes the embedded policies to `<install>/policies/` and sets `capability.policy_dir`. On an existing install with no `policy_dir`, re-running `reckon init` does the same. It never changes a `policy_dir` that is set, and it never overwrites a policy file that exists.

What still needs a person's click:

- the agent's ticket requests;
- a ticket request the blast-radius escalator raised to T3 (`design/04` §1);
- the first action of a draft investigation, which activates it.

## Alternatives rejected

- **Credit a fixed install user.** The user's id is not known at init time, and every ticket would name one person who never saw it.
- **Build the policy into the engine like the baseline DENY.** Tenant policies are files the tenant owns, edits, shadows and deletes (`design/04` §4.1). A built-in approval could not be turned off the same way.
- **Leave the guard to the predicate.** A predicate is tenant text. A requester policy without the `HUMAN` check would credit a person with an approval they never gave.
- **Drop the tier guard.** Blast radius drives the trust tier. Mass ticketing is the case the escalator exists for.

## Consequences

- An install's own ticketing works without a click once a ticketing write binding is configured. With no capability config, the action layer stays off, as before.
- An operator who wants every ticket approved by hand sets `shadow: true` on the file or deletes it. Deleting the directory also needs `policy_dir` cleared, because a missing policy directory fails startup.
- On an existing install, re-running `reckon init` rewrites `config.yaml` to add `policy_dir`, the same way `reckon demo seed` rewrites it.
