# 0054 — A prediction a person tests is recorded by the analyst, with an attestation the record can cite

Issue: #54 · PR: #55 · Status: accepted

## Context

Some predictions are decided by asking someone, not by running a query:
the account owner confirms they started the session, the change owner
confirms the maintenance window, the night-shift operator confirms the
reboot. Under `leading_null` the benign rival is usually decided exactly
this way, and it is usually the discriminator.

The evidence rule (`04 §1`, enforced in the aggregate) says a CONFIRMED or
DISCONFIRMED prediction must cite what was observed. A phone call produces
no `ObservedData`. So the analyst who made the call and got an answer had
no honest move: withdraw says the test was bad, inconclusive says it was not
decisive, and both are false. With the decide gate (`adr/0051`) the untested
discriminator then blocks the rival for good.

Constraints that bound the answer: the evidence rule stays (a confirmation
with nothing observed is a claim); the capability layer stays pure I/O and
never authors anything a person said; `x-interpretation` remains the only
invented primitive beyond the two reasoning nodes and `x-action`
(`01 "Adopted vs invented"`); and the model must never be able to record
"the user confirmed it".

## Decision

- **A prediction declares who tests it.** `x-prediction.test_kind` is
  `tool` (default) or `manual`. Manual means a person's answer decides it;
  `test_query` then says whom to ask and what. The word stays *prediction*:
  "if H2 holds, the owner will confirm the session" is a claim about what
  will be observed. What is manual is the test, not the claim.
- **The human observation becomes citeable as a STIX `note`.** A prediction
  outcome may carry an `attestation` (content, who was asked, over what
  channel, when). The aggregate materializes it as a `note` authored by the
  human principal, with `object_refs` to the prediction and its hypothesis,
  and prepends the note id to the outcome's `test_result_refs`. The
  evidence rule is unchanged: a decisive outcome still cites what was
  observed. What changed is that a human observation is now an object the
  record can point at, and it reads as attested, never as telemetry. `note`
  is adopted unchanged from STIX 2.1; nothing new is invented.
- **Two human-only guards at the aggregate**, beside withdraw. An AI
  delegate cannot carry an attestation, on any prediction. An AI delegate
  cannot record any outcome but withdraw on a manual prediction. The agent
  may propose a manual prediction and may ask for its result; it cannot
  supply one. A human may attest the outcome of a tool prediction too (they
  checked a console themselves): the attestation is a human act regardless
  of test kind.
- **The decide gate treats manual predictions like any other.** An untested
  manual discriminator blocks the AI exactly as a tool one does; the analyst
  records the result or withdraws the test. That is the point of the gate.
- **The surfaces follow.** The tracker row of a manual prediction says so
  and offers "Record result" in place of "Test P<n>"; every untested row
  offers "Record result" as the human's way through `adr/0051`, which until
  now had no affordance. The dialog takes outcome, who was asked, how, and
  what they said, and can prefill from an inbound reply on a comms thread.
  The reasoning-state block marks manual predictions; the prompt tells the
  agent when to propose one and that it never records their result; ⚡ Run
  tests skips them by name. The export and the evidence view render the
  note as an attestation: author, channel, source, content.

## Alternatives rejected

- **Let a human record a decisive outcome with no evidence.** Weakens the
  one rule the hypothesis loop is built on, two PRs after the engine
  started enforcing its twin for decisions.
- **Fake an `ObservedData` with human provenance.** A phone call is not
  telemetry, and `derivation_mode = DIRECT` would stop meaning anything.
- **A new `x-attestation` primitive.** STIX `note` already is a human-
  authored statement about other objects; inventing a sibling would need a
  justification the adopted object removes.
- **Auto-decide a prediction from an ingested reply.** Even with a real
  comms adapter a reply is read by a human and recorded by a human; the
  seam stays, the typing goes away.
- **A numeric trust weight on attested evidence.** People misremember,
  cover for colleagues, or are the insider. The record labels the evidence
  as attested and names who was asked; the weighing stays with the analyst
  and the export reader.

## Consequences

- Prompt text changed (the test-kind sentence), so the prompt version moves
  and both model baselines need a rerun. The tool schema changed too; it
  is outside the prompt hash.
- Migration 0023 adds `test_kind` and `attestation` to `prediction_current`.
  Replay rebuilds the notes from the events.
- The archive bundle carries the notes alongside the reasoning nodes.
- The thread, the export and the evidence view show a human statement as
  evidence. That is intended, and the label makes it legible as such.
- A comms-thread reply is prefilled, not linked: the note quotes it. A
  typed link from note to trail entry can come with a real comms adapter.
