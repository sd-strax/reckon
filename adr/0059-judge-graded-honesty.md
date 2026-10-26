# 0059 — A judge model decides a declined-outcome turn when the anchor strings miss

Issue: #59 · PR: #64, amended by #68 · Status: accepted

## Context

E3 (acknowledgment-gate honesty) and R3 (decide-against-rivals honesty) grade
the turn where the agent is asked to decide and should not. When nothing is
recorded, the only question is whether the reply said why: that a human must
first acknowledge the hypothesis, or that an untested discriminator is still
outstanding. The v0 graders answered it with anchor lists, phrases the model
had been seen to use. An honest refusal in other words failed; a reply that
merely contained the word passed. The strings graded phrasing, and the
phrases were the prompt author's expectations written into the grader, the
same single-author loop adr/0057 names. design/10 §3 marks both assertions
"judge refines in v1"; the second scenario (adr/0058) makes the wording
problem worse, since its honest refusals name a person rather than a test.

Two constraints. Grading had to stay deterministic wherever it could be,
because a judge is itself a model behavior to validate (design/10 §1). And
the backend never holds a model key: any judge runs client-side on the
analyst's own key, like the agent.

## Decision

- **Anchors first, judge second, only on a decline.** The anchors stay the
  deterministic first pass: a hit is a PASS with no model call, and the
  decision branch of R3 (a support attempt over an outstanding discriminator)
  stays deterministic. When nothing was recorded and no anchor hits, the
  grader asks a judge against a fixed rubric instead of failing the turn on
  wording. Without a judge the graders behave exactly as before.
- **The judge is a no-tools completion on the agent's own provider seam**
  (`agent.LLM`), BYOK, with the rubric, the engine state the rubric refers to
  (the outstanding discriminators by id and handle), and the reply. It answers
  a strict JSON object: pass, the sentence it relied on quoted verbatim, one
  line of reason. The quote is written into the verdict so a reader can check
  the judge from the report.
- **Attribution.** A run that consulted the judge records the judge model and
  the rubric version; a run that did not leaves them empty. The judge's tokens
  are reported apart from the agent's. `RECKON_EVAL_JUDGE_MODEL` selects the
  model (default a cheaper model with committed pricing); `RECKON_EVAL_JUDGE=off`
  grades by anchors alone. A judge error keeps the deterministic verdict and
  says the judge was unavailable.
- **Regrade may judge.** `make eval-regrade` re-judges the same turns when a
  key is present, so a rubric fix never requires a model run; it is no longer
  token-free in that one case, and the report says so.
- Catalogue version moves to v0.11.

## Alternatives rejected

- **Longer anchor lists.** More phrases is the same mechanism with more
  coupling to the author's expectations, and it still fails the next honest
  wording.
- **Judge everything.** The decision branches are graded from the tool-call
  log and the engine state, and those answers are exact; replacing them with a
  model trades a fact for an opinion. The judge is confined to the one
  question the record cannot answer.
- **A deterministic rubric over the state block.** The reply's own words are
  what the analyst reads; whether they name the outstanding test is a reading
  question, not a parsing one.
- **A judge on the backend.** The backend holds no model key by commitment;
  the judge runs where the agent runs.

## Consequences

- An anchor-missed verdict can now differ between a run with a key and a
  regrade without one; the attribution makes the difference visible, and the
  regression rule compares like with like once both baselines are judged.
- Judging a model with a sibling of itself is a known v1 limitation
  (design/10 §9); the quoted sentence is the check on it.
- The second scenario's drivers may keep short anchor lists; the judge
  carries the long tail of wording.
- The rubric text is versioned (`JudgeRubricVersion`); a rubric change is a
  grading change and moves the catalogue version with it.

## Amendment (#68)

The anchors-first order graded a false positive without recourse: a reply
containing "outstanding" while deciding anyway passed on the string and the
judge was never asked. With the judge's cost near zero per turn, the order is
inverted. On a declined-outcome turn the judge is the grader whenever one is
present; the anchor that hit, if any, is recorded on the verdict beside the
judge's answer, and the run summary counts agreements and lists the
disagreements, each checkable against the quoted sentence. That agreement rate
is the judge-drift signal design/10 §9 left open. Without a judge, or when it
errors, the anchors decide as before and the attribution says so. The judge
model stays a Sonnet-class model by default: the quote mitigates a weaker
reader but does not replace one. Catalogue v0.12.
