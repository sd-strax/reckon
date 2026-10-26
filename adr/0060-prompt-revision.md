# 0060 — The prompt states principles, not patches, and its author is not its only reviewer

Issue: #60 · PR: #65 · Status: accepted

## Context

With the ledger in place (adr/0057) the prompt's history was legible: every
rule had been validated on one scenario, four sentences quoted that scenario's
fixtures, two rules re-legislated at length what the engine now refuses
(adr/0051, adr/0054), and the manual-test example named the one check the
scenario makes tool-testable — which the eval then showed steering a model to
the phone call over the available query (two of three trials). The prompt's
author had also been its only reviewer: the same party read the transcripts,
wrote the sentence, and wrote the grader strings that passed it.

Constraints. A change to the words moves the prompt version and voids the
accepted baselines (design/10 §4.4), so the revision had to be one pass, not
a sentence at a time. The guard (adr/0057) refuses fixture identifiers, so the
revised wording could not lean on examples drawn from the data. The second
scenario (adr/0058) now exists, so a revised rule can be validated on two
shapes of case before it is accepted.

## Decision

- **Five rules revised, none added.** `one-claim` loses its scenario-shaped
  compound example. `framing` replaces the fixture's benign alternative with
  the three archetypes a rival takes (authorized activity, the account's own
  owner acting legitimately, a different actor behind the same artifact).
  `tests-discriminate` replaces "the account owner confirming the session, the
  change owner confirming the window" with the principle: a test is manual
  only when no capability can answer it, so check what you hold before saying
  a person must be asked. `decide-against-rivals` shrinks to the principle
  plus what to do when the engine refuses, since the engine refuses.
  `mitre-labels` loses its technique example. The authority and output rules
  are unchanged.
- **Tool descriptions follow the same rule.** The verb tools' entity example
  uses placeholders, and gains the address form the indicator verb needs; the
  hypothesis handle and label examples and the prediction tool's manual-test
  guidance are stated without a case.
- **The guard's exemption list is empty** and stays so.
- **Review by a party who did not write it.** The revision was reviewed by a
  fresh agent with none of the author's context against a fixed rubric:
  principle or patch, scenario vocabulary, duplication of an engine gate,
  conflicts or ambiguity, and plain English. Its findings and what was done
  with each are recorded below. The owner's part of the review is a checklist
  that needs no judgment of English (eval/README "Changing the prompt").
- **Pending validation is a ledger state.** A revised rule carries
  `PendingValidation: "#60"` in place of `Validated` until both scenarios'
  runs are accepted under the new version; the ledger test refuses a rule with
  neither. Accepting the baselines flips the field.

## Independent review

A fresh agent with none of the author's context reviewed the draft against
the rubric. Its findings and their disposition, before the hash was pinned:

- `decide-against-rivals` described the gate as narrower than it is (a
  rival-owned discriminator only, where the engine blocks on either side) and
  used "you" for a hypothesis in a prompt where "you" is the agent. Both
  fixed: "whichever side owns it", and the hypotheses named as leader and
  rival throughout. Substantive.
- `framing`'s three archetypes were the two bundled scenarios' null
  hypotheses restated generically, one redundant with another. Collapsed to
  the two distinct kinds: the activity was legitimate, or a different actor
  produced the same evidence. Substantive.
- The manual-test rule was stated three times in three vocabularies
  (capability, verb, tool). One vocabulary ("tool available to you"), the
  prompt rule carries the principle, the tool description the mechanics.
- "no capability you hold" was ambiguous against the degraded-capabilities
  section (does a degraded verb count?). "no tool available to you" resolves
  it.
- `proportionate` ("do not explain your authority boundaries unless asked")
  was in mild tension with "when the engine refuses, say so". One clause
  added: unless asked or an engine refusal makes them the answer.
- The hypothesis-handle example presupposed a benign rival, contradicting the
  revised `framing`. Now "rival mechanism".
- The entity example's `<pid>` placeholder lost the one fact the old literal
  carried, that pid is a number. A numeric literal with the note restored it.
- Wording: "Split it" (antecedent), "where evident" (dangling), a stacked
  colon-then-"then", a doubled "it". All taken.
- Declined: two unchanged rules name product affordances (the three action
  kinds in the no-fabrication rule; the `/pending` command). That is product
  vocabulary, not scenario vocabulary, and the rules are load-bearing.

## Alternatives rejected

- **Revise one sentence per eval finding.** That is the loop that produced the
  overfit; each revision costs the same reruns as a whole pass.
- **Keep the examples but swap them for other cases.** An example drawn from
  any one case is the same mechanism with a different case; the archetypes in
  `framing` are the only concession, and they name kinds, not instances.
- **Delete the engine-enforced rules outright.** The model should know the
  rule exists so it reasons with the gate rather than discovering it by
  rejection (the prompt's standing design); the principle stays, the
  re-legislation goes.
- **Review by the author re-reading the diff.** Not independent.

## Consequences

- The prompt version moves. Four baseline runs are needed: both scenarios,
  both models (`make eval`, `make eval SCENARIO=impossible-travel`, each per
  `RECKON_MODEL`, then `make eval-accept`). Until then the five revised rules
  read `PendingValidation` in the ledger.
- The eval will say whether the tool-first manual rule holds across both
  shapes. If a model still prefers the phone call where a query exists, that
  is now a finding about the model, not about an example in the prompt.
- Future prompt changes follow the same shape: one PR, words plus ledger plus
  golden, an independent review against the rubric, four runs, accept.
