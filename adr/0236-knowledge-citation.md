# 0236 — A recalled SOP or past case is `cited` when the agent names it by ref

Issue: #236 · PR: #301 · Status: accepted

## Context

Every interpretation records the SOPs and prior cases its turn recalled
(`consulted_sops`, `consulted_similar_investigations`, design/06 §6.1), each with
a flag saying whether the turn built on it. The flag was `used`, and the session
set it when the item's title or id appeared, case-insensitively, anywhere in the
turn's text:

- **The text included the engine footer.** The action reconciliation and the
  no-action attestation are appended before the lists were finalized, so engine
  words could mark an item used.
- **Titles over-match.** "Phishing" is in every reply about a phish. The one test
  passed on a topic phrase ("RDP lateral movement") that any reply on the case
  would contain.
- **Paraphrases under-match.** "Per the isolation runbook" names no title or id.
- **The record presented a heuristic as a fact.** The inspector said "applied",
  the chips "followed" and "drew on case", the export "followed SOP", and the
  summary lens listed "Procedures followed". Knowledge citation counts (#109)
  would have counted it.

Only the similar-case list had a test, and neither list had a false case.

The ceiling is what the engine can attest. It can show that the agent retrieved
an item (already recorded) and that the agent itself named it to the analyst. It
cannot show that the agent applied a procedure.

## Decision

- **The flag is `cited`.** It is renamed in place on the aggregate's
  `ConsultedSOP` and `ConsultedSimilarInvestigation`, the agent client types, the
  server view, the workbench wire shapes and view types, and the markdown export.
  There is no sibling field and no dual reading.
- **The signal is the ref recall returned**: the SOP's `sop_id` from
  `recall_sops`, the case's `investigation_ref` from
  `recall_similar_investigations` or `prior_investigations`, appearing in the
  model's own text for the turn, in any case. Titles are not matched. This is
  how the agent already cites evidence: by ref, in prose.
- **The text is every text block the model wrote in the turn**, across all
  rounds; the analyst saw all of it. The session keeps it apart before the
  engine footer is appended (`modelText`, `agent/session.go`). There is no
  last-round restriction: an early-ended turn has no "answer" round, and matching
  refs only already removes the topic over-match.
- **A prompt rule says so**, under "Output discipline": `cite-knowledge`, Origin
  #236. When a procedure or past case the agent recalled shaped its answer, it
  names it by the ref recall returned, as it cites evidence refs, and it does not
  name one that did not. Its `PendingValidation` is #236 (see Consequences).
- **The words follow the flag.** Chips say "cited:" / "recalled:" for SOPs and
  "cited case:" / "recalled case:" for past cases. The inspectors say "cited" /
  "recalled, not cited". The export says "cited SOP" / "recalled SOP" and "cited
  prior case" / "recalled prior case". The summary lens and the handoff note list
  "Procedures cited". design/06 §6.1, design/01, and design/ui/02, /08 and /09
  are amended to match.

## Alternatives rejected

- **Keep title matching beside refs.** A title says neither which recalled item
  the model meant nor that it meant one: it is the topic vocabulary.
- **Only the last round's text.** A turn that ends early has no answer round,
  and the topic over-match this guarded against is gone once titles are dropped.
- **An LLM judge deciding "applied".** That would put a model's opinion in the
  audit trail, add a completion to the commit path, and make #109's counts
  nondeterministic. If "applied" is ever wanted, it is a separately labelled
  annotation beside `cited`, never in its place.
- **A new `cited` field beside `used`, with dual reading.** The product is
  pre-release and a demo reseed wipes local data, so there is nothing to
  migrate.

## Consequences

- #109 can promise "cited in N investigations" and a where-cited list from this
  field. It cannot promise "applied".
- Accepted residual: a model that echoes a ref in narration and never returns to
  it is marked cited. The claim stays literally true, because it named it.
- This is a change to what the model reads. The `prompt` component's digest
  moves from a870ddbf to 67a4243d. `PromptVersion` moves from 60c97621 to
  db8cd908 under `leading_null`, and from bc5bd82c to 308ef84d under
  `leading_only`. No other component moves.
- **The baselines are not rerun in this PR.** [0232](0232-model-facing-text.md)
  expected the next prompt change to rerun them; that rerun is deferred to a
  later run under the full version. Until then the committed baselines keep
  a870ddbf, and the interval in design/10 §4.4 now spans this rule too: support
  stays with them, a run on this code is held to their scores, and it prints
  each version it does not share with them. `cite-knowledge` stays
  `PendingValidation` until that run accepts it on a scenario.
- The ledger test still refuses a rule with neither `Validated` nor
  `PendingValidation`, so the pending state is visible on the rule itself.
