# 0051 — Decide-against-rivals is an aggregate gate for AI delegates, not a prompt contract

Issue: #51 (with #50, #52) · PR: #53 · Status: accepted · Supersedes the "deliberately not engine gates" stance in the hypothesis-loop notes

## Context

The competing-hypotheses model (`design/01 §x-hypothesis`) has one rule that
makes the set a set rather than a list: while a prediction that discriminates
between two live hypotheses is untested, neither can be called. The agent
was told this in its prompt, shown the outstanding discriminators in a
per-turn reasoning-state block, and graded on it by the eval (R3). The
engine did not check it. The project notes said so deliberately: a prompt
contract, graded, not an engine gate.

Then, in one ⚡ Propose tests turn on H2, the agent recorded two predictions
under H2 (one discriminating against H1), ran the first, recorded it
disconfirmed, recorded H2 refuted and H1 supported. The second prediction,
the discriminator, was still untested. Three things had failed at once:

- **Scope.** A command that said "record predictions" was executed as
  propose, run and decide. The instruction never said where it ended.
- **The rule.** H1 was recorded supported over an outstanding discriminator.
  The state block could not have shown it: the block is rendered at the
  start of the turn and the discriminator was written during it. A per-turn
  prompt mechanism cannot police an intra-turn sequence.
- **Visibility.** R3 grades the scripted decide turn. A decision landing on
  a propose turn was outside its view. The panel also gave the analyst no
  hint that the rows under a hypothesis were the tests that decide it.

## Decision

- **An aggregate gate, for AI delegates only.** `outstandingDiscriminatorGate`
  rejects a SUPPORTED or REFUTED outcome on hypothesis H recorded by an AI
  delegate when any UNTESTED prediction discriminates between H and a live
  rival: one a rival holds against H, or one H holds against a live rival.
  The folded prediction state now carries `discriminates` so the check is
  pure, and the rejection names the blocking prediction so the agent can
  relay it. Inconclusive and abandon are not gated: they decide nothing.
- **The override is a human act, never a flag.** A human actor is not gated.
  The analyst clears the way by testing the prediction, withdrawing it with
  a reason, or recording the outcome themselves. There is no "record anyway"
  instruction to the agent any more; an override an AI could pass would be
  an override the model could talk itself into.
- **Scoped commands say where they end.** Propose tests ends when the
  predictions are recorded: no test run, no outcome recorded. Run tests ends
  when the outcomes are recorded: no hypothesis decided. Decide names the
  gate and the three human ways through it. These strings live in the
  workbench, so they change nothing about the prompt version.
- **R3 also grades the propose turn** in the eval scenario, reading the
  attempt from the tool-call log whether or not the engine accepted it.
- **The predictions section names itself** on the tracker: what the rows
  are, what a confirmed or disconfirmed one means, and, when there are none,
  that nothing can confirm or refute the hypothesis until a test exists.

## Alternatives rejected

- **Keep it a prompt contract, strengthen the wording.** The failure was
  intra-turn. No wording shown at the start of a turn constrains a sequence
  the agent composes inside it.
- **Gate humans too, with an explicit override field.** The human's override
  already has visible forms (withdraw with reason; record the outcome
  themselves) that leave a better record than a boolean.
- **Gate inconclusive as well.** Recording "we could not decide" over an
  undecided set is honest, not a violation.
- **Re-render the state block after every tool call.** Expensive on the
  prompt cache, and still advisory. A rule the engine can check belongs in
  the engine.

## Consequences

- The prompt sentence about overrides changed, so the prompt version moves
  and both model baselines must be rerun and re-accepted. The scenario also
  gains an assertion on one turn, so the driver hash moves with it.
- The agent will sometimes hit the gate and relay its message. That is the
  intended behaviour: the analyst sees exactly which test stands in the way.
- The earlier note that these behaviours are "deliberately not engine
  gates" is superseded for decide-against-rivals. Propose-tests yielding a
  discriminator remains a prompt contract graded by R1 and R2.
- A human-recorded prediction outcome on the tracker (the "people test"
  gap) is the natural companion to this gate and is not part of it.
