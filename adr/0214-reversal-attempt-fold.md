# 0214 — The write-side fold records a reversal attempt, and refuses an event type it does not know

Issue: #214 · PR: #264 · Status: accepted

## Context

`foldState` routed every `action.*` event to `foldActionEvent` except
`action.reversal_attempted`, on the reasoning that the event leaves the
original SUCCEEDED (`design/04-action-authorization.md §7.1`, Position C) and
so changes no folded state. But the folded state is what every command is
checked against. `applyReversalAttempt` could only check that the original
was SUCCEEDED, which stays true after an attempt, so a retried or re-run
`ReversalSaga` appended a second attempt. The projection hid it: it holds one
`reversal_attempted_by_ref`, and the duplicate overwrote it. The stream kept
both.

`foldState` also had no default case. Any event type it did not list was
dropped without a word, and every later check ran on a state missing that
fact.

## Decision

- `actionState` carries `ReversalAttemptedBy`, the reversing action whose
  unverified attempt is recorded on the original. `foldActionEvent` folds
  `action.reversal_attempted` into it. Status is unchanged.
- `RecordReversalAttempt` for the same reversing action again appends nothing
  and returns no error: the attempt is already recorded. One by a different
  reversing action is rejected. The lineage guard on `RequestAction`
  (`adr/0152`) already refuses a second reversal while one is pending or has
  succeeded, so reaching this point means something upstream is broken.
- `foldState` lists `investigation.renamed`, `member_added`, `member_removed`
  and `evidence_attached` as explicit no-ops and returns an error for any
  other type it does not know.

## Alternatives rejected

- **Reject the repeat and let the activity treat the rejection as benign:**
  it makes this fix depend on the activity layer, and a rejection that means
  "fine" is a rejection every caller has to know to ignore.
- **Record a second attempt by a different reversing action:** the lineage
  guard means a legitimate one cannot arrive, and the projection can hold only
  one reference.
- **Rely on correlation-id deduplication alone (`adr/0215`):** it covers a
  retry within one workflow run. A later run of the saga for the same
  reversing action carries a new run id, and only this check sees it.

## Consequences

- The write side folds every event type the projectors see. A new event type
  must be added to `foldState`, or loading any investigation that holds it
  fails loudly.
