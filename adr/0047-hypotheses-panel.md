# 0047 — The hypothesis tracker shows the whole competing set in full, and the decide guard where it bites

Issue: #47 · PR: #49 · Status: accepted

## Context

The tracker (`design/ui/02 §2.9`) had two layers: a tab strip with one tab
per hypothesis, and a working card for the selected tab. Above both sat a
"Next:" banner quoting the outstanding discriminating prediction in full with
its own "Test this" button. Read top to bottom on a real investigation: an
instruction, then tabs whose descriptors differed (`0/1` on one, `proposed`
on the other), then one hypothesis with its claim cut off by a line clamp,
then its prediction, also clamped. Nothing said what Adopt was for.

Three things were wrong, and they are related.

- **The guard pointer was placed as a headline.** The banner is the decide
  guard's state ("this test is untested and separates H1 from H2, so neither
  can be called"), which is useful at the moment the analyst reaches for
  Decide. At the top of the panel, before the hypotheses, it reads as the
  agent sequencing the analyst: "do this next". That is the same fault as
  uninvited collection in the conversation, in a different surface.
- **The two sentences that matter were the two that were hidden.** A
  hypothesis is its claim; a prediction is its statement. Clamping them to
  make room for chrome inverted the panel's priorities, and tabs hid the
  rival, which is the thing the analyst is trying to kill.
- **Predictions had no name.** Hypotheses carry a `handle` so every surface
  can point at the same one. Predictions had only an id and their text, so
  the banner had to quote them whole, and the agent could only paraphrase.

## Decision

- **Every hypothesis is a card, stacked, live ones first.** No tab strip. The
  comparison is the content; with the two or three hypotheses an
  investigation holds, all of them fit and all of them should be read.
- **Claims and predictions render in full.** No line clamp on either. The
  card grows to fit its sentence.
- **Predictions get an ordinal, `P1`, `P2`, …,** assigned by the server view
  in the order predictions were recorded across the investigation, and used
  by the tracker (a chip on the row, "Test P3"), by the agent's reasoning
  state block (`untested P3 (x-prediction--…)`, `H1 holds P3 against H2`),
  and therefore by the analyst in the composer. One assignment, one
  vocabulary. The id remains the reference the tools take; the ordinal is
  display vocabulary only.
- **The guard appears where it bites.** The outstanding discriminator's row
  is marked ("undecided until tested"). Beside ⚡ Decide, when a live rival
  holds an untested prediction that splits it from this hypothesis, a note
  says which one blocks ("blocked: P2 (H2 legit-admin) is untested and splits
  it from H1"). The banner is gone.
- **The status pill explains itself.** PROPOSED: proposed by the agent; adopt
  it to take ownership; until then the agent can test against it but cannot
  record it supported or refuted. OPEN: adopted, decided by its tests against
  the rivals. The card head reads handle · status · tested/total, the same
  shape on every card.

## Alternatives rejected

- **Keep the tabs, fix the descriptors.** Tabs still hide the rival. The
  neglected alternative silting up out of view is the failure mode the
  competing-set model exists to prevent.
- **Keep the banner, shorten it.** Even one line at the top is a "do this
  next" from the system. The same information at the Decide button is a
  reason; at the top it is an instruction.
- **Client-side ordinals.** Cheaper, but the agent would not see them, and the
  first time an analyst typed "test P2" into the composer the shorthand would
  fail. The ordinal has to come from the one place both sides read.
- **Persist the ordinal as a prediction field.** A domain-model change for a
  display label. Record order already determines it.

## Consequences

- The agent's reasoning-state block changes shape (ordinals appear). It is a
  per-turn block on the user message, not the system prompt, so the prompt
  version is unchanged; the eval's R-graders read the hypotheses view, not
  the block text, so no baseline moves.
- The "+ prediction" chooser and the adopt card are unchanged.
- The compound-prediction problem (several claims joined by "and/or" in one
  prediction) is now plainly visible on the panel. That is the agent's
  output, and it belongs to the eval and the prompt, not to this layout.
