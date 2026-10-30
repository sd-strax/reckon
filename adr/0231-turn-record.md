# 0231 — Every agent turn is committed, including one that ends early, and a failed commit is retried

Issue: #231 · PR: #270 · Status: accepted

## Context

A turn is committed to the reasoning thread as one interpretation: the
transcript bytes and the tool-call log, content-addressed in the side store
(05 §3.4). That commit was the turn's last step, and the turn reached it only
when it finished normally. A model-call error or a cancel returned from the
loop straight away, so the turn was never committed, although tool calls in it
may already have recorded hypotheses or requested actions, each committed on
its own as it ran. When the commit itself failed, the error was returned once
and the transcript existed only in that response. Either way the thread held
acts with no turn explaining them.

The commit could not simply be retried: the backend mints each
interpretation's id, so a retry after a lost response would record the turn
twice.

## Decision

- **One exit.** A model-call error or a cancel ends the loop instead of
  returning from it. The transcript gets a closing `[engine] turn ended
  early: …` line (the model error, or `cancelled by the analyst`), and the
  turn goes through the same closing work as any other: the ground-truth
  footer, the pending-action refresh and the commit. Every turn the analyst
  starts is committed, whether or not it ran a tool. A turn with no prose has
  the rationale `(turn ended early — see transcript)`; rehydration already
  skips a turn without prose.
- **The closing work outlives a cancel.** It runs on
  `context.WithoutCancel(ctx)` bounded by 30 seconds: a cancel stops the
  model, not the record.
- **Retry, then hold.** A commit that fails in transit (no response, 408, 429,
  5xx) is retried twice, after 1s and 4s. One still failing is held by the
  session and committed, in order, before the next turn's own commit; the
  turn's error says it is held and will be recorded with the next turn. A
  commit the backend refuses (any other 4xx) is reported and neither retried
  nor held. `ResetContext` commits held turns before its reset marker, so
  rehydration treats them as reset away.
- **The backend recognises a repeated turn.** The aggregate folds each
  committed turn's id with its interpretation and transcript hash. A
  `RecordInterpretation` whose transcript carries a turn id already on the
  stream, with the same content hash, appends nothing; `POST
  /api/interpretations` answers 200 with the interpretation the turn was first
  recorded as. The same turn id with a different transcript is rejected. This
  is the turn-commit counterpart of the system-command check `#215` added,
  keyed on the turn id the session mints rather than the correlation id.

## Alternatives rejected

- **Commit only turns that ran a tool:** leaves a hole where the analyst's own
  words were, and the rule is harder to state than "every turn".
- **Persist held turns to local disk:** covers only the case where the backend
  is unreachable and the sidecar process then exits; more machinery than that
  case warrants now.
- **Check the thread for the turn before retrying:** racy, and the thread does
  not expose turn ids; the aggregate answers it atomically under the same
  snapshot as the append.

## Consequences

A cancelled or failed turn appears on the thread with its transcript, so every
hypothesis and action has the turn that produced it. A turn whose commit could
not reach the backend appears once the backend is back, at the next turn or
reset. A held turn is lost if the sidecar process exits before it is
committed. The aggregate's folded state grows by one entry per committed turn.
