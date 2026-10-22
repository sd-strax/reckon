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
| [0045](0045-answer-the-ask.md) | A question is answered, not investigated: the agent takes only the read the ask requires and offers the next; a verdict-shaped question is framed, then waits for the go (prompt contract, graded by O3) |
