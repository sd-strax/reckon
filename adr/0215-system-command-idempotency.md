# 0215 — A system command retried after its commit is recognised by its correlation id

Issue: #215 · PR: #265 · Status: accepted

## Context

The workflow activities that record an action's lifecycle (`EmitDispatched`,
`EmitResulted`, `EmitReversed`, `EmitReversalAttempted`, `EmitExpired`) each
call `aggregate.Handler.Handle`, which commits in Postgres, and then report
completion to the Temporal server. Temporal runs an activity at least once. If
the commit succeeds and the completion never arrives (the process stops, the
server is unreachable, the attempt outlives its timeout, or the commit's OK
never reaches the client), the activity runs again against a stream that
already holds its write.

The aggregate's checks ask whether a transition is legal, not whether it has
already happened, so the retry was rejected. For a dispatch that is the
damaging case: the action read EXECUTING, the workflow failed before the
outbound call, and nothing had been sent. A retried result or reversal failed
its workflow even though the record was right. Rejections were retryable, so
each spent the full retry budget first, and so did ids that could never parse.

Each attempt minted a random correlation id, so a retry looked like a new
command.

## Decision

- A system-emitted command's correlation id is derived from the activity that
  issues it: UUIDv5 over workflow id, run id and activity id. Every retry of
  one activity carries the same id. A new run or another activity carries a
  different one. The run id is part of the key because activity ids restart
  per run and an action's workflow id is fixed.
- `Handle`, after loading the stream in the transaction and before
  `applyCommand`, checks a SYSTEM envelope's correlation id against the
  stream. If it is there and its domain event is the one this command
  records, the command is already applied: nothing is appended and the call
  succeeds. If it is there under another event type, the command is rejected.
- Only SYSTEM envelopes are checked. Commands from people and delegates mint a
  fresh id per request, and a reused id there would silently drop a human
  command.
- The activities return a domain rejection and a malformed id as
  non-retryable errors. `ErrConcurrent` and infrastructure errors stay
  retryable.
- `EmitResulted` opens or extends a `notify.*` comms thread only on the
  attempt that recorded the result. Opening a thread is not idempotent.

## Alternatives rejected

- **A repeat check written into each command:** it covers only the commands
  someone remembered. A missing check is how `action.reversal_attempted`
  duplicated (`adr/0214`).
- **Treat every rejection in the activity as "already happened":** it cannot
  tell a repeat from a real refusal, and would let a workflow carry on past
  one.
- **Deduplicate every actor's commands:** no caller outside the workflows
  sends a repeated id today, and the first one that did would lose a human
  command without an error. Retry safety for HTTP callers, such as an
  idempotency key header, is a separate decision.
- **A dedicated idempotency table:** `correlation_id` is already stored and
  indexed on every event, and the stream is already loaded in the
  transaction.

## Consequences

- Any new system-emitted command needs a `systemEventType` entry, or a repeat
  of it is rejected rather than recognised.
- This does not cover an action left EXECUTING by a workflow that stopped
  between dispatch and result. Whether the outbound call happened is not on
  the record, so no retry can decide it.
