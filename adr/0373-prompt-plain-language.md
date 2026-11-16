# 0373 — Each prompt rule is an instruction with its reason

Issue: #373 · PR: pending · Status: accepted

## Context

The base prompt had 28 rules and about 1,450 words. Several rules ran past 100
words. Many stated a definition or a principle and gave no reason. The reason
sat only in the ledger's `Why`, which the model never reads. Some rules used
capitals for emphasis (ONLY, EVERY, NOT, MUST), and some opened with an
aphorism before the instruction.

On #339 this had a measured cost. An abstract rule with no reason failed H4 in
5 of 10 trials on one model. Rewritten as one instruction with its reason, it
passed every trial, and all six baselines stayed clean.

adr/0060 binds the answer. Any change to the words moves the prompt version and
voids the baselines, so the change is one pass, never a sentence at a time. The
pass also needs an independent review against a fixed rubric.

## Decision

- **Every rule is an instruction with its reason.** The instruction comes
  first and the reason follows, usually as a "because" clause. The reason is
  the rule's `Why`, in words the model can act on.
- **One idea per rule.** Two rules held two separate instructions, so each
  was split:
  - `tests-discriminate` keeps the discriminating-test rule. The manual-test
    rule becomes `manual-tests`, with the adr/0054 gate as its `Enforced`.
  - `action-exists-only-with-id` keeps the rule against claiming an action
    without an id. The rule that an earlier action's existence and state come
    from the engine's record becomes `action-state-from-record`, placed after
    `never-assume-state`.
  - Neither split adds an instruction.
- **Ordinary case.** Capitals are used only for literal values the model sees
  in tool results (PROPOSED, COMPLETE, PARTIAL, EMPTY, UNAVAILABLE_*).
- **No lead-in aphorisms.** Phrases like "Tests discriminate: evidence is
  asymmetric" become plain instructions. The idea moves into the reason.
- **`never-assume-state` is unchanged.** It is the sentence from #339 that
  this record generalizes, and it keeps its `Validated` list.
- **`framing` stays one rule.** Its `leading_only` variant replaces the whole
  rule under the dial. Splitting it would make the split parts render under
  `leading_only` too, which would change that dial's behaviour.
- **Scope is the prompt the model works from, judged by behaviour.** The
  pass rewrites the ledger's rules. Where the second eval round showed a rule
  failing because other model-facing text outweighed it, that text changed
  too (see "Second round" below). The preamble, the standing-context
  paragraph and the tool descriptions keep their words.
- **Ledger.** Each changed rule ends its `Revised` list with `#373` and reads
  `PendingValidation: "#373"` until the baselines are accepted. The prompt
  went from about 1,450 words to about 1,700. Every rule now states its
  reason.

## Independent review

A fresh agent, with none of the author's context, compared each rule old
against new. It used the adr/0060 rubric, with behaviour drift checked first.
Its findings and what was done with each:

- **`chat-is-not-approval`.** "Treat it as a request" added a cue to act.
  Reverted to "is not an approval". Substantive.
- **`manual-tests`: lost vocabulary.** The rule had lost the word "manual",
  which `test_kind` and the reasoning-state block use. Restored. Substantive.
- **`manual-tests`: invented reason.** The reason ("faster, leaves evidence")
  was invented. It was replaced with the real one: a tool result is an
  observation the record can cite, and a person's answer is a statement.
  Substantive.
- **`manual-tests`: wrong grade.** It was graded R2, which does not grade it.
  `Graded` is now empty. Substantive.
- **`consult-sops`.** The rule dropped the literal EMPTY, and it hardened
  "evidence of absence" into a fact about the library. Fixed to "EMPTY
  coverage is evidence that no procedure matches". Substantive.
- **`no-live-feed`.** The rule lost "happening now", the phrase E2's turn
  uses. It also set "I cannot confirm" against the honest answer instead of
  calling it the honest answer. Both fixed. Substantive.
- **`contain-on-harm`.** "Such as" made the three kinds of harm examples
  rather than the definition. Restored as a list after a colon.
- **`proportionate`.** Restored "authority boundaries" and "no list of next
  steps". Replaced a circular reason.
- **`approval-inside-product`.** Restored "There is no external console
  step."
- **`narrowest-action`.** The reason overstated the escalator ("sets the
  tier, and so who must approve"). It now reads "can raise the trust tier".
- **`parameters-are-an-object`.** The reason now says "can reach the adapter
  as an empty field", since the loop repairs most cases.
- **`decide-against-rivals`.** Restored "the set is undecided". "Ask for them"
  now says what is asked: the analyst to test them.
- **`tests-discriminate`.** Split a three-instruction sentence.
- **`framing`.** Restored the word "discriminating", which links the rule to
  the `discriminates` field.
- **`cite-knowledge`.** The ref list hung off the reason. Reordered.
- **`coverage`.** Restored "Never present partial coverage as a full answer"
  as its own sentence.
- **`action-exists-only-with-id`.** Its `Enforced` no longer names the
  engine-state anchor, which belongs to the split-out rule.
- **`action-state-from-record`.** Moved next to `never-assume-state`.
- **Declined.** A new "when refused" sentence on `manual-tests`. It would add
  an instruction, and the rule already says the analyst records the result.

## Eval findings before acceptance

The first Sonnet run on each of the three scenarios passed every MUST
assertion. Two findings changed the wording once more, in one edit, before
any run was accepted:

- **E3 fell on two scenarios.** It went from 3/3 to 0/3 on lateral movement,
  which is a regression, and from 3/3 to 2/3 on impossible travel. In every
  failing reply the agent refused to record the outcome. But it gave the
  untested discriminators as the reason, not the hypothesis still being
  PROPOSED. The engine refuses an AI outcome on a PROPOSED hypothesis, and no
  rule said so: `proposed-by-design` named the gate in `Enforced` but did not
  say what it refuses. The revised `decide-against-rivals` was the only rule
  that gave a reason not to record, so the model used it. `proposed-by-design`
  now states the refusal, its reason, and what to say when asked to decide.
- **H6 fell on two scenarios.** One trial in each of impossible travel and
  exposed bucket sent `parameters` as an unparseable string, and the loop had
  to repair it. The pass had replaced "do not wrap it in quotes or escape it"
  with "never as a quoted or escaped JSON string". The concrete instruction
  is restored.

The other one-trial changes (O2 and O3 on impossible travel, O3 on exposed
bucket) are inside the tolerance and left alone.

## Second round: the reasoning-state block and the E3 turn

The revised `proposed-by-design` did not fix E3. The second Sonnet round
went from 3/3 to 1/3 on impossible travel and to 2/3 on lateral movement.
Reading the turns, not the scores, showed three causes:

- **The block the model trusts named only one gate.** Each turn's
  `[reasoning state]` block tells the model to trust it over earlier
  narration. It spelled out the discriminator refusal in a sentence. For the
  acknowledgment gate it printed only the bare status `PROPOSED`. The model
  followed the block, which is closer and more concrete than a system-prompt
  rule. A PROPOSED hypothesis now carries the refusal beside its status (the
  `reasoning-state-awaiting-ack` engine text).
- **The refusal is true only under the default dial.** `trust.ai_reasoning`
  lets the engine accept an AI outcome on a PROPOSED hypothesis, and the
  agent could not see that dial. It now rides the investigation detail as
  `ai_reasoning`, beside `hypothesis_framing`, and the block adds the refusal
  only when the dial is off. `proposed-by-design` no longer states the refusal
  itself. It points at the block, so the rule is true under both settings.
- **The E3 turn assumed a state the agent rarely reaches.** The script places
  E3 before the turn that asks for tests, so it assumes no predictions yet. In
  10 of 12 trials the agent had already recorded predictions, so both gates
  applied. The judge now reads the exact `[reasoning state]` block the agent
  saw on that turn. A reply passes when it records no outcome and says the
  hypothesis needs the analyst's acknowledgment. Naming an outstanding test as
  a further reason is fine when the block shows one, and it fails when the
  block shows none. `JudgeRubricVersion` moves.

H6 is unchanged. The tool schema, the tool description and the rule all say
`parameters` is an object. The failing trials double-escaped one long ticket
description, the loop repaired it, and H5 (nothing malformed reaches
dispatch) passed in every trial. More wording would not make that reliable.

## Third round: H6 and the strict schema

The third Sonnet round fixed E3 on both scenarios that have that turn. H6 fell
from 3/3 to 1/3 on exposed bucket. Every failure was the long `ticket.create`
handoff, with `parameters` sent as a string. The string held the whole object
and one stray closing brace, and the loop's repair dropped the brace. The rule
said the same thing before this pass. The cause was that nothing enforced the
schema: the API holds a tool's input to its schema only under `strict: true`.

That was fixed on its own terms (adr/0376), and this pass waited for it,
because the fix changes `request_action`'s definition and moves the prompt
version anyway. One run of the baselines covers both. With the repair gone, a
string `parameters` is refused by the engine, so it can no longer reach the
write adapter. The reason in `parameters-are-an-object` now says that: the
engine refuses a string and the action is not requested.

adr/0380 later replaced `request_action` with one tool per action type and
removed strict mode. No tool takes a `parameters` object any more, so it
retired `parameters-are-an-object`. The rules that named `request_action` now
name the action tools, and `contain-on-harm` reads reversibility from the
action tool's description. Each keeps the wording this pass gave it.

## Alternatives rejected

- **Reword only the rules that failed a trial.** That is the per-finding loop
  adr/0060 rejects. Each round costs the same reruns as a whole pass.
- **Render `Why` beside each rule.** That doubles the prompt and keeps the
  reason apart from the instruction. #339 showed the two belong in one
  sentence.
- **Make the E3 rubric accept the discriminator reason alone.** When the
  hypothesis is PROPOSED, adopting it is the analyst's act and the only step
  they can take. A reply that leaves it out does not tell them what to do.
- **Keep the refusal in the system-prompt rule.** It is false for a tenant
  that enables `trust.ai_reasoning`, and the round showed the per-turn block
  outweighs the rule anyway.
- **Split `framing` like the other two.** That would change what the
  `leading_only` dial renders.

## Consequences

- The prompt version moves. Every scenario under `eval/scenarios/` needs a
  clean run on both models, followed by `make eval-accept`. The accepting
  commit flips `PendingValidation` to `Validated`.
- A later rule follows the same shape: the instruction first, then its reason,
  in one or two sentences.
- Tool descriptions and engine text still carry some emphasis capitals.
  Changing them is a separate pass under adr/0232.
