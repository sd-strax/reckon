# 0216 — A stranded action is restarted by a sweep, and one whose outcome is unknown waits for a person

Issue: #216 · PR: #267 · Status: accepted

## Context

An approval records APPROVED and then asks Temporal to start the action's
workflow. When that start failed, the server logged it and returned; the
comment promised a sweep or a manual retrigger, and neither existed. The
action stayed APPROVED with nothing to run it.

APPROVED is a known state: the dispatch is recorded before any call leaves
reckon, so nothing has been sent. Starting the workflow again is safe even
after an ambiguous start error, because an action's workflow id is fixed
(`action-lifecycle-<id>`, `reversal-saga-<id>`) and Temporal refuses a second
start while a run is live.

Two more stranded states had no owner:

- **Dispatched, then the run stopped before recording a result.** The call may
  have left reckon. A new run's guard saw a dispatch and returned "", so the
  action read EXECUTING forever, and nobody could tell whether the tool acted.
  Dispatching again could act twice.
- **A reversal saga that stopped after its reversing action finished.** A
  restarted saga got "" back from the child's guard, treated it as not
  SUCCEEDED, and never marked the original.

The design principle (`design/04-action-authorization.md §6.2`): the record
never lies about uncertainty, and a state nobody can determine waits for a
person instead of being guessed.

## Decision

- **A sweep restarts stranded workflows**, at backend startup and every five
  minutes (`server/action_sweep.go`). It starts the workflow of every APPROVED
  action, every EXECUTING action, and every reversing action whose recorded
  outcome is SUCCEEDED while its original carries neither `reversed_by_ref`
  nor `reversal_attempted_by_ref`. "Already started" counts as started, in
  `startOnce` (`temporal/client.go`), so the sweep and the approval path share
  one start path and running it at any time is safe.
- **An approval stands.** A stranded APPROVED action is dispatched whenever
  the sweep finds it, however long it waited. Failure handling adds no
  business rules the working path does not have.
- **The workflow decides, not the sweep.** The dispatch-ledger guard
  (`ActionDispatchState`) reads the action's status and its recorded outcome.
  Temporal runs one execution per workflow id, and `adr/0215` removed the
  lost-ack path, so a run that finds EXECUTING with no result knows the run
  that dispatched stopped: it records `OUTCOME_UNKNOWN` and calls nothing. An
  action that already has an outcome returns it, read from the events, so a
  PARTIAL reversal stays PARTIAL and a restarted saga never over-claims. The
  guard is versioned (`workflow.GetVersion`) so histories recorded with the
  old boolean `CheckDispatched` still replay.
- **`OUTCOME_UNKNOWN` is a status of its own.** `action.outcome_unknown` is
  SYSTEM-only, legal only from EXECUTING, paired with an `action-result`
  interpretation, and carries its reason.
- **Only a person resolves it, and only by saying what the tool shows.**
  `POST /api/actions/{id}/resolve` records `action.outcome_resolved`:
  SUCCEEDED or FAILED, a required note on what was checked, and optionally a
  reference and an artifact location (`adr/0139`). It pairs with a new
  `action-resolution` interpretation. AI delegates and the system are refused.
  There is no re-request from `OUTCOME_UNKNOWN` and no reversal of it; a rerun
  is a resolution to FAILED and then a re-request. Nothing times it out.
- Resolving a reversing action SUCCEEDED restarts its saga, which then marks
  the original.
- The workbench card shows `OUTCOME UNKNOWN`, says the target may or may not
  have changed, and offers Resolve.

## Alternatives rejected

- **Close an unknown outcome as FAILED with every target UNKNOWN:** the
  existing residual for a call with no answer (`08 §6c`). FAILED reads as a
  fact the record does not have, and the action would offer a re-request
  before anyone checked whether the first call took effect.
- **Allow a re-request straight from `OUTCOME_UNKNOWN`:** a rerun without
  knowing the first call's effect is the blind retry this design exists to
  prevent.
- **Expire an approval the sweep finds stale:** it adds a freshness rule only
  failure paths would use. The approval was given for the action, and the
  action is unchanged.
- **Ask Temporal whether each EXECUTING action's workflow is running:** the
  workflow's own guard already knows, from the one-execution-per-id rule,
  without a second source of truth.
- **Sweep at startup only:** a start that fails while the backend stays up
  would wait for the next restart.

## Consequences

- An action left APPROVED by a failed start runs within one sweep interval.
- With no dispatch client configured, approvals still stay APPROVED: nothing
  can run them, and the sweep does nothing.
- `OUTCOME_UNKNOWN` needs a person. The investigation reads as remediating
  until someone resolves it.
- A new system-emitted command still needs a `systemEventType` entry
  (`adr/0215`); `RecordOutcomeUnknown` has one.
