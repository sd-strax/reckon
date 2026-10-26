# 0069 — The eval driver answers a manual test with an authored answer the reader interprets

Issue: #69 · PR: #70 · Status: accepted

## Context

A prediction the agent marks manual (adr/0054) is the analyst's to close: the
agent cannot record its outcome. In the eval nobody played the person who
answers, so every trial with a manual test ended with the hypothesis set
OPEN, the decide turn could only be graded as a decline, and the attestation
path was never exercised by the harness. The second scenario (adr/0058) is
built around a people-tested alternative, so this was its normal ending.

Constraints. The driver is a deterministic analyst (design/10 §2): its inputs
are authored and it never branches on model output. It acts through the same
product API a human uses, never an eval-only door (design/10 §1). And the
polarity of an answer depends on the agent's own wording of the prediction,
which the script cannot know in advance: "the owner will confirm a VPN" wants
DISCONFIRMED where "the owner will deny the session" wants CONFIRMED.

## Decision

- **A turn-level driver option, `attest_manual`, carrying an authored
  answer.** Content (what the person said), source (who), channel (how), per
  scenario, written like the analyst's turns. Before the turn is sent, the
  driver records an outcome with that attestation for every UNTESTED manual
  prediction, through the Record result path with the human token; the engine
  mints the STIX note and the outcome cites it, exactly as a click would. A
  turn with nothing manual outstanding is a logged no-op.
- **A reader decides which way the answer points, per prediction.** The same
  model mechanism as the judge, in a different role: given the owning
  hypothesis, the prediction's own statement, and the answer, it returns
  CONFIRMED, DISCONFIRMED, or INCONCLUSIVE with one line of reason. Its
  reading, the prediction and the answer are written into the trial record
  (`attestations`) so a reviewer can check it. Nothing in the runner encodes
  a polarity, a leader, or a handle convention.
- **The graders are untouched.** R3 already distinguishes a decision taken
  with no discriminator outstanding from one taken over an outstanding one;
  the step only changes which branch the decide turn can reach. The reader
  is built from the run's key regardless of the grading judge switch: a
  driver that cannot read the answer cannot answer a manual test, and fails
  loudly rather than guess.

## Alternatives rejected

- **A polarity rule in the runner** (the answer favours the leader; CONFIRMED
  on the leader's predictions, DISCONFIRMED on the rival's). Both bundled
  scenarios are true positives, so a fixed rule fits them and forecloses the
  case that matters most: the answer that exonerates. Reading the leader from
  the handle text would also couple the driver to a prompt convention.
- **Withdrawing the manual predictions instead.** Clears the gate, but it
  scripts the analyst discarding the test, not the answer arriving; the
  attestation path would stay untested.
- **Feeding the answer to the agent as a turn.** The agent cannot record a
  manual outcome; it would have to ask the analyst, which is where the
  scenario already was.
- **A fourth status for an unreadable answer.** INCONCLUSIVE already exists
  for an answer that does not settle the prediction; an unparseable reading
  is an error, not a status.

## Consequences

- Both driver hashes move. Baselines accepted before this record were graded
  on a decide turn that could only decline; the Sonnet runs for both scenarios
  are rerun, then Opus.
- The decide turn now tests deciding as well as declining, and #54's
  attestation is exercised end to end on every run that produces a manual
  test.
- The step's coverage depends on the agent marking something manual; the log
  says when it did nothing, and the trial record lists what it did.
- The structural gap remains that both scenarios are attacks and both
  authored answers confirm it. A scenario whose person contradicts the
  evidence, or a benign case that ends in refuting the leader, is corpus
  growth (design/10 §6) and the real counter to that bias.
