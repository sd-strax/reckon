# 0384 — A dispatch that fails before the call is not retried, and its record says nothing was sent

Issue: #384 · PR: pending · Status: proposed

## Context

An approved action is dispatched by its workflow. The workflow records
`action.dispatched`, then runs the dispatch under a retry budget of three
attempts (`04 §6.2`). When the budget runs out, or the adapter reports a
fatal error, the action ends `FAILED` with every target `UNKNOWN`.

`UNKNOWN` means the call left reckon, but the adapter can't confirm what the
tool did (`08 §5`). That's the right record when a call was made and its
answer was lost.

Some dispatches fail before any call is made:

- The binding the action was planned on is no longer usable. Its adapter
  was disabled or removed, its operation was dropped from the manifest, its
  scope no longer matches, or its params no longer render for this request.
  With bindings frozen at request time (adr/0406), this happens only when the
  configuration is reloaded while the action waits for approval.
- No write resolver is wired at all.

These were recorded the same way as a lost answer. A missing binding came back
as a plain error, so it was retried twice for nothing. Its targets were then
marked `UNKNOWN`, which tells the analyst the tool may have acted. Nothing was
sent, so the tool can't have acted, and a re-request is safe. The record said
the opposite.

The reason for a failure reached the workbench but not the agent. The
investigation's action list carries `error_detail`, but the agent's view of a
row left it out. The agent saw `FAILED` and nothing else, so it couldn't tell
the analyst whether the action never went out or failed at the tool.

## Decision

- **A failure before the call is "not sent".** The resolver returns every
  failure that happens before the outbound call as one kind of error: the
  frozen binding missing, disabled or out of scope, its params not rendering.
  The dispatch activity treats "no write resolver" the same way. The error
  names what broke, for example `adapter crowdstrike-falcon is not configured or not enabled`.
- **On the first attempt, it isn't retried.** The dispatch activity raises it as a non-retryable
  error of its own type. Retrying can't help, because the configuration
  won't change between attempts.
- **Only the first attempt is recorded as not sent.** A later attempt that
  fails before the call is retried, and ends `UNKNOWN` like any other
  failure, because an earlier attempt may have reached the tool.
- **The record says nothing was sent.** The action ends `FAILED` with
  `attempts` 0, every target `FAIL`, and the reason as `error_detail`. `FAIL`
  is exact here: the tool didn't act on the target. `attempts` 0 is what says
  no call was made. The result names no adapter, so the record keeps the
  planned one, which says which adapter the action was meant for.
- **The lifecycle doesn't change.** `action.dispatched` is still recorded
  before the dispatch, as the ledger requires (`08 §6`). The result that
  follows says the call never happened.
- **Every other failure keeps its record.** An exhausted retry budget, a
  fatal error the adapter reports, and a timeout still end with the targets
  `UNKNOWN`, because a call may have left reckon.
- **The agent sees the reason.** The agent's view of an action row carries
  `error_detail`, so `list_actions` shows why an action failed.

## Alternatives rejected

- **A new per-target value such as `NOT_SENT`.** It widens a vocabulary the
  specs, the record API and the graders share, and `attempts` 0 already
  carries the fact.
- **Checking the binding before `action.dispatched` and failing the action
  from `APPROVED`.** It adds a lifecycle transition. The binding can still
  become unusable between that check and the call, so the dispatch has to
  handle it anyway.
- **Picking another binding when the frozen one is gone.** The approval was
  given for the planned adapter and operation (adr/0406). Sending the action
  somewhere else would act on something nobody approved.

## Consequences

- An action that never went out says so, and a re-request of it is safe.
  The re-request picks its binding against the configuration it finds, and
  is refused when no binding can carry it (adr/0406).
- `04 §6.2` gains the category "approved, but the binding can no longer
  carry it", and `08 §6(c)` says a failure before the first attempt's
  call is never `UNKNOWN`.
- Disabling an adapter while actions that use it wait for approval is still
  allowed. Those actions now fail with a clear reason when they're approved.
