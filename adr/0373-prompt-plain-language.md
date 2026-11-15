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
- **Scope is the ledger's rules.** The preamble, the standing-context
  paragraph, tool descriptions and engine text keep their words. Each of them
  has its own ledger entry and hash (adr/0232).
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

## Alternatives rejected

- **Reword only the rules that failed a trial.** That is the per-finding loop
  adr/0060 rejects. Each round costs the same reruns as a whole pass.
- **Render `Why` beside each rule.** That doubles the prompt and keeps the
  reason apart from the instruction. #339 showed the two belong in one
  sentence.
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
