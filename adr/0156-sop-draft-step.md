# 0156 — Drafting a procedure from a concluded case is the analyst's step, with every outcome shown

Issue: #156 · PR: #197 · Status: accepted

## Context

Concluding fired a candidate-SOP draft in the background: one model completion
whose result, if the model judged a procedure warranted, opened as a document
with an Add / Discard notification. It was silent when the completion failed,
silent when the model declined, and lost without trace when a window reload
killed the agent process mid-draft — which the road test hit 36 seconds after
concluding. The analyst never asked for the draft and could not tell which of
the three had happened.

What the organization learns is the analyst's decision (the human decides
what is acted on and what is learned); a draft started for them, out of sight,
is the opposite.

## Decision

- **Concluding no longer drafts.** The Summary lens of a concluded
  investigation offers **Draft SOP from this case** beside Export report.
- **Every outcome shows on the panel:** drafting; declined, with the model's
  reason ("covered by …") and **Draft anyway**, which asks again with the
  analyst's override; failed, with the error and **Try again**; ready, with the
  draft open beside the panel and **Add to SOP library** / **Discard** on the
  panel (not a notification that disappears). Add imports the document as it
  reads at that moment, with the analyst's edits.
- The completion takes a `force` flag (sidecar `proposeSOP {force}`), which
  adds one sentence to its instruction; the base prompt is unchanged.
- **The case summary stays automatic** — it is the investigation's memory for
  recall, not a decision — but its outcome is shown beside the report:
  narrative written (by which model), the structured baseline only, or the
  narrative failed and the baseline stands. On opening a concluded case the
  workbench reads which it is.

## Alternatives rejected

- **Keep the background draft and report failures.** A draft nobody asked for
  still interrupts with a document, and a reload still loses it.
- **Draft automatically and park it for later review.** Storing unreviewed
  candidates makes a second inbox to clear; the analyst can ask when the case
  is fresh in mind, which is when the draft is worth reviewing.

## Consequences

- No candidate is drafted unless someone asks; organizations that want every
  case reviewed for a procedure make that part of closing the case.
- A reload no longer loses anything: the button is there to press again.
