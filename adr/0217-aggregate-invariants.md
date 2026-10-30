# 0217 — The aggregate's stated invariants hold in its code: a pure decide step, a checked payload version, a named human on every expiry

Issue: #217 · PR: #271 · Status: accepted

## Context

Three statements in `aggregate/` described rules the code did not keep.

- **The decide step was not pure.** `handler.go` says identities are minted
  in `Handle`, not in `applyCommand`, so the pure layer stays free of
  write-time concerns. `applyCommand` called `uuid.New()` at 15 sites, one for
  each paired interpretation id. The same envelope, command and state gave
  different events on each call.
- **`Event.Version` was never read.** `event.go` and `02 §5` say a payload
  shape change bumps the event type's version and comes with an upcaster at
  read time. The version was written and scanned back, and nothing checked
  it. A bump without its upcaster would have decoded old payloads as the new
  shape, silently, in the fold and in every reader of the events table.
- **An expiry named no human.** `event.go`, `02 §7` and `04` say every event
  records a named human principal; a SYSTEM actor is a workflow acting for
  one (dispatch and result carry the approver). The expiry timer had no
  approver to name and recorded the literal principal `"system"`.

In each case the code was changed to keep the stated rule.

## Decision

- **`applyCommand` takes an id source.** `applyCommand(env, cmd, state,
  newID)`; every helper that mints an interpretation id takes it too.
  `Handle` passes `uuid.New`; tests pass a fixed sequence. The decide step
  now returns the same events for the same input.
- **One version check, called by every payload reader.**
  `CheckEventVersion(eventType, version)` (`event.go`) returns
  `ErrUnknownEventVersion` for a version this binary does not read. It is
  called by the store's row scan (the fold, `LoadStream`, replay) and by every
  query that reads payloads from the events table directly: the verdict
  projection, the recorded-outcome read, the thread, the action record, and
  the transcript lookup. Every type is at version 1. A shape change raises its
  type's version in that function and adds the upcaster at the same time.
- **An expiry is recorded on the requester's behalf.** The write-side fold
  remembers each action's requester (the request event's principal).
  `ExpireAction` is rejected unless its principal is that requester, and
  `EmitExpired` reads it with `Handler.ActionRequester`. Kind stays SYSTEM.
  The workflow input is unchanged, so timers already running pick this up
  when they fire.

## Alternatives rejected

- **Rewrite the comments to match the code:** each one would keep a stated
  rule with an exception in it, in the one layer meant to have none.
- **An upcaster registry now:** no payload has changed shape; the registry
  would be machinery for a change nobody has made. The check is where the
  first upcaster goes.
- **Check the version only in the store's row scan:** eight queries read
  payloads directly and would still misread a bumped event.
- **Route every payload reader through the store:** the cleanest single path,
  but it rewrites the server's read views, well beyond keeping the rule.
- **Allow `"system"` as the principal of SYSTEM events:** it turns "AI is a
  delegate, never a principal" into a rule with an exception the next
  background job could use.

## Consequences

- A test can assert the exact events `applyCommand` returns, ids included.
- A new query that reads event payloads must select `event_version` and call
  `CheckEventVersion`; nothing enforces that mechanically.
- An event at an unknown version stops the read with an error: a command on
  that investigation, or the view reading it, fails until the upcaster ships.
- Expiry events written before this change keep the principal `"system"`;
  events are never edited.
