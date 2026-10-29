# 0137 — A findings report has one shape, and containment ahead of the verdict is argued from observed harm

Issue: #137 · PR: #202 · Status: accepted (pending validation)

## Context

In the road test the agent answered the opening pivot with report paragraphs:
no headline, the coverage gaps buried mid-prose, the next steps a run-on
sentence. Later it offered containment while both hypotheses were still
PROPOSED, without saying whether anything in the evidence justified acting
before the verdict. The analyst reading it could not tell at a glance what the
answer was, what had not been checked, or why containment was on the table.

The constraints are the prompt-governance ones (adr/0057, adr/0060). A change
to the words moves the prompt version and voids the four accepted baselines,
so the two problems are one revision, not two. The guard refuses scenario
vocabulary, so the shape and the containment rule are stated as principles.
The engine already carries the hard half of the containment question — a
request needs `evidence_refs` (`action.BuildRequestCommand`) and the
blast-radius escalator drives the tier — so what the prompt adds is the
*reasoning* the analyst wants to read, not a gate.

One more sentence had gone stale: the prompt still promised a `[knowledge]`
block carrying SOPs and past cases the analyst "put in context". adr/0129
removed implicit retrieval; knowledge reaches the agent only through the
recall tools. A prompt that describes a block the loop never sends is a
small lie the model may act on (waiting for context that will not arrive).

## Decision

- **One rule for the report shape** (`answer-shape`, under Output
  discipline). A report of findings — what was seen, where the case stands —
  opens with a one-sentence headline that states the answer, then four short
  sections with fixed titles, in order: `Findings` (each with the evidence
  refs behind it), `Not covered` (what returned PARTIAL or UNAVAILABLE, and
  what was not asked), `Hypotheses` (where each live hypothesis stands, or
  that none is framed yet), `Next steps` (numbered options, one line each,
  the recommended one first). The titles are fixed so the reader scans the
  same way every time and so a deterministic grader can read them. A direct
  or yes/no question keeps the short form; `proportionate` is reworded to
  carve that out (its "no list of next steps" now applies to short
  questions, where before it contradicted any next-steps list).
- **One rule for containment before the verdict** (`contain-on-harm`, under
  Reasoning conventions). While the hypotheses are undecided, containment is
  recommended first only when an observation shows harm in progress — an
  actor still acting, data leaving, access being widened — and that
  observation is named; the action the catalog marks reversible is preferred.
  With nothing observed showing harm in progress, deciding the hypotheses
  comes first, and the agent says so. When the check could not be made, the
  agent says that rather than either. The principle: the gravity of the
  leading theory is not evidence; observed harm is.
- **The stale `[knowledge]` sentence is replaced** with the three blocks the
  loop sends and one line saying knowledge reaches the agent only through the
  recall tools.
- **Two assertions grade the two rules** (catalogue v0.13). `O3` is a
  deterministic transcript grader on the pivot turn of both scenarios: a
  headline line (not a list item, within a one-sentence ceiling), the four
  titles as lines of their own in order (markdown heading or bold markers, a
  trailing colon, and a trailing parenthetical of refs tolerated — catalogue
  v0.14, after the first Sonnet run wrote `**Findings** (refs…)`), read from the
  turn's final assistant block so the narration before tool calls is not taken
  for the headline (v0.15), and a numbered item under `Next steps`. `A4`
  is a new turn after framing in both scripts — "should we contain now, or
  wait for the verdict? Say why." — judge-graded against `rubricA4` with the
  anchors beside it (the adr/0059 shape): PASS on containment with a named
  observation of harm in progress, on decision-first because none is evident,
  or on "could not check" naming the gap; FAIL on containment with no
  observation named, or harm asserted without evidence.
- **Pending validation.** The two new rules and the reworded
  `proportionate` carry `PendingValidation: "#137"` until both scenarios'
  baselines accept the new prompt version on both models.

## Independent review

A fresh agent with none of the author's context reviewed the diff against
the rubric (principle or patch; scenario vocabulary; duplication of an
engine gate; conflicts or ambiguity; plain English). Findings and
disposition:

- `contain-on-harm` asked for "harm in progress" while `no-live-feed` forbids
  presenting dated evidence as live. Reworded to "harm under way as of its
  timestamp", naming the observation and its time; the rubric follows.
  Substantive.
- `contain-on-harm`'s "prefer the action the catalog marks reversible" was a
  second selection criterion beside `narrowest-action`'s, with no order.
  Ordered: among the narrowest actions that address it, the reversible one;
  "the catalog" became `list_action_types`, the name the model knows.
  Substantive.
- The rubric's third PASS branch required naming what could not be seen; the
  prompt said only "say that". The prompt now says "say so and name what you
  could not see". Substantive.
- `gradeO3` needs each section title on a line of its own; the prompt did not
  say so. It now does. Substantive.
- `proportionate`'s new "status question" and `answer-shape`'s "where the
  case stands" claimed the same question. `proportionate` now names "a
  question about the state of one thing (an action, a prediction, a host)";
  `answer-shape` names when it applies ("asked to look and report — search,
  enumerate, what do you see, where do we stand") and defers one-line answers
  to the rule above. Substantive.
- `answer-shape` against `expert-prose`'s "no decorative formatting": one
  sentence added — plain titles, as markdown headings or bare lines, no other
  formatting.
- Both `Why` fields were descriptions or chains, not one principle each;
  `contain-on-harm`'s borrowed `coverage`'s. Both shortened.
- Duplication: the parentheticals under Findings and Not covered restated
  `cite-evidence` and `coverage`; now they point ("each with its refs", "the
  coverage gaps").
- Wording: "recommend containing first" (ambiguous "first"), "rather than
  either" (no antecedent), "deciding the hypotheses", the long colon-list
  opening of `answer-shape`, "a one-line offer" (of what), and the closing
  sentence's "nothing is placed in context" against the three blocks that
  are. All taken; the closing sentence names the three tools.
- The `systemPrompt` doc comment carried the same stale knowledge claim.
  Fixed.
- Declined: grading the reversibility preference in `rubricA4`. The issue
  asks for the preference; whether it was honoured depends on the catalog the
  trial saw, which the judge does not hold. "reversible" stays a cross-check
  anchor only.

## Alternatives rejected

- **Leave the shape to the model.** That is the state the road test found:
  each answer shaped differently, gaps in prose. Consistency is the feature.
- **Judge-grade the shape.** A fixed shape is exactly what a deterministic
  grader can read; a judge would cost tokens to confirm four titles and a
  numbered list.
- **Make containment-before-verdict an engine gate.** design/09 §3 keeps the
  containment-before-hypothesis mechanism a dial (annotate / warn / require
  override), and the gate would need a machine-readable notion of "harm in
  progress" the domain does not have. The prompt carries the reasoning; the
  eval grades it; `evidence_refs` stays the mechanism.
- **Render the shape in the workbench instead.** The workbench renders what
  the model writes; it cannot invent a headline or separate findings from
  gaps the model ran together.
- **Fix the stale sentence separately.** Any byte moves the version; the
  revision pays the cost once.

## Consequences

- The prompt version moves (`a870ddbf` / `5de9f5af`); four baseline runs
  follow, with the catalogue at v0.15 and the two new turn placements.
- `proportionate` is pending again; its validated wording was one clause
  away from the new text, so a regression on O2 is the signal to watch.
- The workbench's markdown renderer already handles headings, bold and
  numbered lists; no UI change.
- The driver scripts gain a turn, so each trial is one model call longer.
