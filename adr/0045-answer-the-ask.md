# 0045 — A question is answered, not investigated: initiative is scoped by the ask

Issue: #45 · PR: #46 · Status: accepted

## Context

Asked "what does the case claim, and what have we verified so far?", the agent
read the case, answered both halves correctly, and then ran two further reads
on its own initiative and reported what it found. Everything it found was real.
The behaviour is still wrong for this product, for three reasons that do not
depend on the reads being cheap or the findings being useful:

- **Hypothesis-first is the reasoning model** (`design/01 §x-hypothesis`,
  `09`). Evidence fetched before any hypothesis exists shapes the framing;
  whatever arrives first anchors it. The opening move is exactly where
  uninvited collection does the most damage.
- **Every read is the analyst's record.** Reads are persisted, cited and
  threaded into the chronicle and the evidence graph, attributed to the human
  principal, and cannot be un-fetched. Eagerly fetched material is not help
  the analyst can ignore; it is record they now own.
- **Sequencing is the analyst's main instrument of control.** "They would
  have asked anyway" does not rescue it: who decided when the work happened is
  the difference between a delegate and a principal.

The counter-argument was weighed: an assistant that asks before every step is
unusable, and "verified so far: nothing" invites verification. The answer is
not less initiative but initiative scoped by what was asked.

## Decision

A prompt rule, not an engine gate — initiative is a judgment about what was
asked, and a gate that counted tool calls per turn would be crude and brittle.
The rule (agent/prompt.go, output discipline) distinguishes three asks:

- **A question** ("what does the case claim", "what have we verified") is
  answered from what the agent already holds plus the one read the question
  itself requires. The next read is **offered in one line, never taken**.
- **A task** ("verify the claims", "run the tests", any ⚡ command) is done,
  with as many reads as it needs. Unchanged.
- **A verdict-shaped question** ("is this host compromised?") is an
  investigation, not a lookup: the agent states what it holds, proposes the
  leading hypothesis and its alternative with the tests that would decide
  between them, and waits for the go. After the go it is a task, and the
  recorded predictions are the contract for what it may fetch.

It is measured: eval assertion **O3** (SHOULD) grades a driver-marked
`answer_only` turn from the committed tool-call log — no call beyond
`allowed_tools` (empty = none). The lateral-movement scenario gains such a
turn before the framing turn. Catalogue v0.11; the prompt text changed, so
both supported models' baselines are rerun.

## Alternatives rejected

- **Change nothing; scope the demo question.** Would paper over a policy
  ("fetch when I judge it helps") that has no natural stopping point: two
  reads on this question, ten on "is the host compromised?".
- **Gather data first, rule later.** Correcting from "did it anyway" means
  cleaning records; correcting from "asked first" is a sentence. The rule is
  cheap; the eval tells us if it tips too far.
- **An engine-side cap on reads per turn.** Measures the wrong thing; a task
  legitimately needs many reads.
- **Resist "lazy" verdict questions.** Lecturing is bad product, and the
  question is not lazy — it is the investigation's question. The structure
  answers it: the agent cannot conclude, so the question must become a
  hypothesis set to be answerable at all. Frame, then confirm.

## Consequences

- Investigation-shaped questions gain one turn before collection starts.
- Classifying an ask is a judgment; the agent will sometimes frame when a
  quick answer was wanted, or answer when it should have framed. O3 holds
  the clear "question" case; the verdict-shaped case is stated in the prompt
  but not yet graded — a second assertion when a scenario turn for it exists
  that does not disturb the R1–R3 framing turns.
- The demo's Act 1 question stays as written; it is the rule's test on stage.
