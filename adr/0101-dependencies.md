# 0101 — Work the conclusion depends on that only someone outside reckon can do is a dependency: its own record, asked through the message, resolved by the analyst alone

Issue: #101 · PR: #307 · Status: accepted

## Context

An investigation's response often needs work only an owner outside reckon can
do: rotate a credential, re-image a host, confirm a change. Today the ask goes
out in a `notify.*` message and the work then lives in the message body and
the analyst's head. The comms thread a SUCCEEDED send opens (`comms/`,
binding §4) tracks the conversation per message: awaiting reply, replied,
followed up, closed. It does not track the unit of work. One message asks for
several things; one reply settles some of them.

The webview already consumes a count of outstanding owner work in the conclude
preflight and the stage derivation, and feeds both a hardcoded zero, so the
preflight always reads that nothing is outstanding and the Notify step
completes with work still owed. Gap 17 of `design/ui/00-backend-binding.md §6`
asks for this work tracked to confirmation with the investigation and counted
by the preflight, and leaves open whether it is an x-action with an
external-work descriptor or its own record. `implementation/workbench-react.md
§8.6` decides for its own record modelled on the comms threads, with an agent
tool and attachment to "the notification draft".

Two facts shaped the reading. The conversation belongs to the channel that
carries it; reckon holds the send already, as an action with the frozen body
the analyst approved, and mirroring the exchange would duplicate the channel.
The resolution of the work, by contrast, has to live in reckon, because the
investigation is what gets concluded, exported and signed, and the channel
history is outside all three. The owner's word is a human attestation, as it
is for a manually tested prediction (`adr/0054`): a person records it, nothing
infers it from traffic.

## Decision

**A dependency is its own record.** Package `dependency`, table `dependencies`
in the main database, CRUD + thin history like the comms threads: what has to
be done, who owes it, why, a status, the `notify.*` action the ask rides on, an
external reference pointing to where the work lives outside (a ticket, a
thread permalink), who recorded it, who resolved it with a note, and a history
of events. It is not an x-action: nothing executes, there is no blast radius
and no dispatch, so Gate 2 and the dispatch ledger would measure nothing. It is
not a message either: the comms thread keeps the conversation's state, the
dependency keeps the work's.

**Four statuses, one engine transition, two human ones.** OPEN when recorded.
SENT when the `notify.*` action it rides on SUCCEEDS, from the engine's own
fact about its own send, idempotently (only OPEN rows move), wherever that
fact is established: the result activity on every attempt that carries the
success, a retry after the commit included, and a failure there fails the
activity so Temporal's retry is the durable path; the route a person resolves
an `OUTCOME_UNKNOWN` send on, which never passes through the activity; the
attach and create routes, which read the action again after the write so an
attachment that landed as the send completed is not left OPEN under a sent
message; and, under all of them, the stranded-action sweep, which on every
pass marks the OPEN dependencies riding on a SUCCEEDED send, so a MarkSent
that failed after its fact committed is caught within the sweep's interval.
While the ride is in flight (APPROVED, EXECUTING, OUTCOME_UNKNOWN) the
dependency stays on it: re-pointing or detaching is refused, since the send's
outcome is what decides whether the owner was asked; a ride that stopped
(failed, expired, rejected) can move to a pending replacement. CONFIRMED or
WAIVED by the analyst, with a note; a waive needs one, since a conclusion with
work knowingly left open is legitimate only when the record says why. Resolved
is final. A reply on the channel never moves a dependency.

**Every write is a person's act on an open investigation.** An AI delegate's
token is refused on record, attach, confirm and waive, as it is on an action's
outcome: a confirmation is an attestation in the human's name (`adr/0054`),
and the analyst route role alone does not tell a delegate from the analyst it
acts for. A write names an investigation that exists, and one that is not
CONCLUDED, ARCHIVED or DISCARDED: the concluded record is what was exported
and signed, so its dependencies are as sealed as the rest of it. The seal is
the store's, in the same transaction as every mutation (create, attach, mark
sent, confirm, waive): a share lock on the investigation's `investigation_current`
row, which the aggregate updates in the transaction that appends the
concluding event, so a conclusion that committed first is read and refused
and one that starts later waits. The route's own check before the write gives
the clearer answer (404, 409); the transaction is what holds. A send that
completes after the conclusion moves nothing: the sealed record says the
owner had not been asked when the investigation concluded, and that stays
true.

**Attachment is the action id.** A dependency rides on a requested, pending
`notify.*` action of its own investigation. There is no draft object: a
notification is a requested action whose frozen parameters are the draft, and
the dependency points at that.

**The count feeds what already consumed it.** The snapshot carries the
dependencies as a slice beside the comms threads; the preflight line and the
Notify step read OPEN + SENT where they took a zero, and a benign verdict no
longer skips Notify while one is owed. A slice that could not be read is not
zero: the preflight says the count is unchecked and Notify stays in play,
where an empty slice that did read is clean. The preflight stays decision
support, as the comms sheet frames it; there is no engine-side gate.

**Surfaces.** A "Dependencies on others" section in the Response lens, after
the conversations: each dependency with its status, owner and reason, the
message it rides on and its reference, Confirm and Waive opening a prompt for
the note, Record a dependency, and Attach to a pending notification. A
notification card names the dependencies it asks for. The Summary's "Still to
do" lists the open ones. An inspector shows what, owner, why, when asked,
status, the resolution and the history. An OPEN dependency whose notification
failed, expired or was rejected can move to a pending replacement. The export
lists them under "Dependencies" with the notes and references, and the signed
bundle (`07 §2`) carries them whole in `dependencies.json` and lists them in
its report: the dependencies live beside the event stream, not in it, so the
bundle is the only signed form they have. A bundle built without a dependency
store has no `dependencies.json`; absent means not recorded, an empty file
means none.

**Endpoints.** `GET/POST /api/investigations/{id}/dependencies`; `POST
/api/dependencies/{id}/attach|confirm|waive`. Not found 404, resolved 409,
invalid input 422.

## Alternatives rejected

- **An x-action with an external-work descriptor** (gap 17's other option).
  Forcing a record that executes nothing through tiers, Gate 2 and the dispatch
  ledger invents a tier that measures no harm and a dispatch that never
  happens.
- **The comms thread as the dependency.** The thread is per message; the work
  is per unit, and one message asks several things. Overloading the thread's
  status would make "replied" mean "done" for every item at once.
- **A note on the thread trail.** No status, no count, nothing for the
  preflight to read.
- **The §8.6 agent tool.** A model-facing change: under `adr/0232` it moves
  the prompt version and the tool digest and adds an eval item. The agent says
  in prose that an owner has to act; the analyst records it. The tool can
  follow once the record has been used.
- **Mirroring the conversation.** The channel owns it; the send is already on
  the record as the action's frozen body; a dependency carries a pointer out.
- **A reply moving a dependency.** Per message, not per item; and the owner's
  word is an attestation a person records, not a signal the engine reads.

## Consequences

- The preflight and the Notify step now say something true about external
  work; before, the zero made them agree with nothing.
- A concluded record states what was asked of whom, what came back, and what
  was knowingly left open, with the analyst's notes and pointers out, in the
  export and the signed bundle, and nothing changes it after the conclusion.
- One more slice in the snapshot, one more store on the main database, one
  more hook on the result path beside the comms thread and on the outcome
  resolution route, one more step in the stranded-action sweep, one more file
  in the bundle; the aggregate and its events are untouched. The store reads
  the aggregate's `investigation_current` projection for the seal, the first
  side record to do so; it lives on the same database for that reason.
- The owner's name is free text and the reference is a string. Binding either
  to an identity or a connector is a later step, as it is for the comms target.
