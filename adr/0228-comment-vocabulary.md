# 0228 — Comments say what the code does, in words a reader can look up

Issue: #228 · PR: #TBD · Status: accepted

## Context

Comments named build steps: "Phase C", "C.4", "K3", "the A.8 done-bar". Those
labels point at a plan outside this repository, so a reader cannot resolve them.
Some had also gone stale. `server/actions.go` described an approval surface as
future work, and `action_approval.go` already implements it. The same words ran
through `CLAUDE.md`, which every contributor reads first.

## Decision

- **Build-step labels go, everywhere in the engine's source:** Go comments, SQL
  migration comments, YAML example and manifest comments, and `CLAUDE.md`. A
  comment states the behaviour. Spec and ADR references stay, because a reader
  can open them.
- **A stale comment is rewritten to match the code,** not just relabelled. Claims
  about future work are kept only where the work is still undone.
- **Migration files are edited, comments only.** `golang-migrate` tracks version
  numbers, not file contents, so an applied migration is unaffected.
- **`RenormalizePass` stays.** It remains in the workflow inventory `design/05`
  specifies, and its comments now say plainly that it is a skeleton. Removing it
  would be a design decision of its own.
- **Three kinds of file are out of scope.** Existing ADRs are history. `design/`
  and `implementation/` are specs and need their own review. The workbench's
  comments go with its lint work in #249.

## Alternatives

- **Replace each label with an ADR link.** Many labels have no matching ADR, so
  the link would often not exist, and the behaviour is the useful part.
- **Change only the comments the issue names.** The rest would stay unreadable to
  anyone outside the project.

## Consequences

The change is comments plus four strings, all of which named a phase: two in the
`RenormalizePass` log and error text, one in `reckon` CLI output, and one in the
header `demo seed` writes. Comparing Go token streams against the base commit
shows those four as the only differences, so no logic changes. Nothing stops a
new label appearing in a future comment; review is the check.
