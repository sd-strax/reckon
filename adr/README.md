# Architectural decision records

An issue says what is wrong or what is wanted. It does not say how. The how —
the design, the alternatives weighed, and why one won — lives here, one record
per decision, committed in the PR that implements it.

## Naming

`NNNN-component.md`, where `NNNN` is the GitHub issue number zero-padded to
four digits and `component` is a short slug for the part of the system the
decision mostly touches. `0030-cases-pane.md` is the record for issue #30.

A decision that spans several issues takes the number of the issue that
prompted it and lists the others in its header. A decision with no issue
(rare — a refactor nobody asked for in words) takes the PR number instead,
prefixed `pr`.

## Shape

Keep it short. A record is read by someone who wants to know why the code is
the way it is, months later, without the issue thread.

```markdown
# NNNN — Title (a sentence naming the decision)

Issue: #NNNN · PR: #MMMM · Status: accepted | superseded by NNNN

## Context
What was wrong, and the constraints that bound the answer (specs, invariants,
principles already decided).

## Decision
What was done, stated as facts about the system.

## Alternatives rejected
Each with the one reason it lost.

## Consequences
What this makes easier, harder, or now needs doing.
```

Reference specs by file and section (`design/14-case-seed.md §4.2`), the same
way the specs reference each other. A record states architectural merit only:
it belongs to a public repository.

## Index

| ADR | Decision |
|---|---|
| [0030](0030-cases-pane.md) | The case queue is a read-only view with a non-persisting read surface; starting an investigation is the only act on it |
| [0032](0032-case-note.md) | The "opened in reckon" note on a seed case is a `ticket.comment` action in the new investigation, offered under `cases.note_on_seed` (confirm by default) |
| [0034](0034-start-detach.md) | `reckon start --detach` returns when the stack is ready; foreground stays the default; `scripts/demo-reseed.sh` composes the lifecycle |
| [0047](0047-hypotheses-panel.md) | The hypothesis tracker shows the whole competing set in full, names predictions `P<n>` from the server view, and shows the decide guard where it bites |
| [0051](0051-decide-guard.md) | Decide-against-rivals is an aggregate gate for AI delegates (the human override is a visible act, never a flag); scoped ⚡ commands say where they end; predictions name themselves |
| [0054](0054-manual-predictions.md) | A prediction declares whether a tool or a person tests it; a person's answer is recorded by the analyst as a STIX `note` the outcome cites; the AI can propose such a test, never record its result |
| [0057](0057-prompt-ledger.md) | The base prompt is rendered from a ledger (id, origin, principle, enforcing mechanism, grading assertion, validated scenarios), its version is pinned by a frozen test, and a fixture-derived guard refuses scenario vocabulary in the prompt and tool descriptions |
| [0058](0058-second-scenario.md) | The eval corpus has two scenarios of different shape (host-rooted lateral movement; identity-rooted impossible travel); the stack is wired for one fixture scenario at a time (`demo seed --scenario`, `demo scenario`), the driver declares it and the runner checks it; fixture matchers accept a list of alternatives |
| [0059](0059-judge-graded-honesty.md) | E3/R3 keep the anchors as the deterministic first pass and ask a BYOK judge model, against a versioned rubric, only when a declined-outcome turn matches no anchor; the verdict quotes the sentence relied on and the run records the judge |
| [0060](0060-prompt-revision.md) | Five prompt rules restated as principles with no scenario vocabulary and no re-legislation of engine gates; the revision reviewed by a party who did not write it against a fixed rubric; a revised rule is `PendingValidation` in the ledger until both scenarios' baselines accept it |
| [0061](0061-null-observation.md) | Absence is citeable: a COMPLETE, empty capability answer mints an `x-null-observation` (verb, subject, parameters, window, sources; UUIDv5 over the same) whose id rides `observed_data_refs`; a PARTIAL empty answer mints nothing |
| [0069](0069-driver-answers-manual-tests.md) | The eval driver answers a manual test with a per-scenario authored answer through the Record result path; a reader model decides which way it points for each prediction's wording and the reading is in the trial record; graders untouched |
| [0075](0075-test-button.md) | A prediction row's test is a scoped ⚡ turn (`⚡ Test P<n>`), the per-row member of the ⚡ family; the workbench maps the server's prediction ordinal so analyst and agent share one name |
