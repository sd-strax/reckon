# 0075 — A prediction row's test is a scoped ⚡ turn, and the row carries the prediction's name

Issue: #75 · PR: #76 · Status: accepted

## Context

An UNTESTED prediction row had a "Test this" button that staged the test's
query in the composer and waited for the analyst to send it. The design
(design/ui/02) chose that deliberately, on the pivot pattern: operational
text the analyst might want to edit is staged, not fired. Beside it sit the
⚡ buttons, Propose tests, Run tests, Decide, each of which fires a scoped
agent turn on click. In rehearsal the difference read as a defect: "it dumps
the comment into the chat and does nothing".

Separately, the row read "Test this" where the design (adr/0047) says "Test
P3". The server view assigns the ordinal and the agent's reasoning-state block
speaks in P<n>, but the workbench never mapped the field, so the analyst and
the agent were using different names for the same test.

## Decision

- **The per-row test is a member of the ⚡ family.** The button reads
  `⚡ Test P3` and fires a scoped turn: run this prediction's declared test,
  record its outcome citing the observed evidence, say which way it cuts for
  the rival it discriminates against, and stop. Same contract as Run tests,
  for one prediction. The button disables while the turn runs, as the others
  do.
- **The staging pattern stays where it belongs.** A pivot composes a free
  question the analyst should read before sending, and still stages. A
  prediction's test is already written and recorded; running it is the
  analyst ordering an act whose text they authored or adopted. To refine a
  test's wording, withdraw and re-add it, or type the refined ask.
- **The workbench maps the ordinal.** `Prediction.ordinal` rides from the
  server view; rows, chips, guard notes and the ⚡ Test label use P<n>, the
  same vocabulary as the agent.
- A manual prediction has no ⚡ Test; Record result remains its path
  (adr/0054).

## Alternatives rejected

- **Keep staging and relabel ("Stage test").** Honest, but it leaves two
  different behaviours on one row for two kinds of test, and the staged text
  was rarely edited; the useful edit is to the prediction, which Withdraw and
  + prediction already cover.
- **Fire without the ⚡ prefix.** The prefix is the tracker's signal that a
  click spends a turn; the row should say so.

## Consequences

- One click runs one test; outcomes land on the row with their evidence.
- The scoped instruction is new agent-facing text. It is a per-turn
  instruction, not the base prompt, so the prompt version is unchanged; the
  eval's driver never presses the button, so the baselines stand.
- design/ui/02 is updated; the road test and demo script say ⚡ Test P<n>.
