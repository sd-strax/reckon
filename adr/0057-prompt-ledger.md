# 0057 — The base prompt is rendered from a ledger, pinned by hash, and may not quote the fixtures

Issue: #57 · PR: #62 · Status: accepted

## Context

The agent's base prompt (`agent/prompt.go`) grew one sentence per eval
failure for a week, every failure observed on the one bundled scenario
(`lateral-movement-via-rdp`, design/10 §6) and every fix graded against the
same scenario. The result showed in the text: the framing example named the
scenario's service account, the MITRE example was the scenario's technique,
and the manual-test example named the one check the scenario makes
tool-testable (#54). Nothing in the repository distinguished a rule that
states a principle from a rule that patches Tuesday's transcript, and nothing
stopped the next sentence from quoting the fixture again.

Two constraints bound the answer. The prompt's words are versioned by hash
(`PromptVersion`), and the per-model eval baselines are accepted under that
version (design/10 §4.4), so any change to the rendered bytes costs two
baseline reruns; a fix for this had to leave the bytes alone. And the hard
rules of the system live in the engine (the acknowledgment gate, the evidence
rule, adr/0051, adr/0054), so the prompt is the soft layer; the discipline
belongs to how it is changed, not to what it enforces.

## Decision

- **The prompt is rendered from a ledger.** `promptLedger` holds every rule
  as data: a stable id, what introduced it (one issue or commit) and what
  reworded it since, the
  principle it encodes in one line with no scenario vocabulary, the engine
  mechanism that also enforces it when one exists, the eval assertion that
  grades it, and the scenario ids its wording was accepted against. The
  prompt text is these rules in order under their section headings. A rule
  cannot exist without its record, and a rule whose principle is fully
  carried by an engine mechanism is visibly a candidate to shrink to "when
  the engine refuses, say so".
- **The prompt version is pinned.** `TestPrompt_Frozen` holds the hash for
  each framing dial. A change to the words fails the suite until the golden,
  the ledger entry, and the baselines are updated together. A refactor that
  keeps the bytes passes; this PR is one.
- **Scenario vocabulary is refused mechanically.** `TestPrompt_NoScenarioVocabulary`
  builds a denylist from the fixtures themselves (identifier-bearing OCSF
  fields, MITRE ids, the asset-criticality keys of every scenario under
  `fixtures/`) and fails if the base prompt under either dial, any intrinsic
  tool description, or the verb tools' shared entity description contains
  one. The four identifiers present today are listed as dated exemptions that
  the prompt revision (#60) removes; the test also fails when an exemption
  stops matching, so the list can only shrink. A new scenario extends the
  guard with no edit.

## Alternatives rejected

- **A comment above each rule.** Not enforced: a comment can be omitted,
  and nothing ties it to the rendered text. Rendering from the ledger makes
  the record the source.
- **A hand-maintained denylist.** The judgment of what counts as scenario
  vocabulary is exactly what drifted. Deriving the list from the fixtures
  removes the judgment and grows with the corpus.
- **Review of prompt diffs alone.** The author of the rule was the only
  reader of the transcripts and the only reviewer; a single-author loop is
  what produced the overfit. Review is still wanted (#60) but is not the
  mechanism.
- **Striking the four identifiers now.** That moves the version and costs the
  reruns for a cosmetic change; the revision PR (#60) pays that cost once for
  the whole prompt.

## Consequences

- The prompt version is unchanged by this PR; no baseline rerun.
- Every future prompt edit touches three things in one PR: the words, the
  ledger entry, and the frozen golden, with the baseline rerun that follows.
  That friction is the point.
- The ledger is the input to the prune (#60): rules whose `Enforced` carries
  their `Why` are the first candidates, and `Validated` with one entry says
  which rules have never been tested against a second shape of case (#58).
- The guard covers the prompt and the tool descriptions, not the per-turn
  `[engine]` blocks (`agent/state.go`), which carry the investigation's own
  identifiers by design.
