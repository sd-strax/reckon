# 0161 — A draft started by mistake can be discarded, with a reason, as a terminal lifecycle state

Issue: #161 · PR: #TBD · Status: accepted

## Context

A draft could not leave the work queue. Archive needed CONCLUDED, conclude
needed ACTIVE with a verdict, and a draft became ACTIVE only on its first
approved action. In the road test a draft started on a mistyped host
("WIN-FILE)1") stayed in the analyst's list for good; the only way out was to
push it through a fake verdict, which would put a false conclusion on the
record. The record is append-only: nothing is deleted.

## Decision

- A new lifecycle transition **draft ──discard──▶ discarded**
  (`DiscardInvestigation`, recorded as an `investigation.status_changed` with
  the analyst's reason and its `lifecycle` interpretation). `discarded` is
  terminal, like `archived`: the aggregate accepts no further commands.
- **Only a draft** can be discarded — an active investigation has acted on the
  world and ends by conclusion. A draft with an action request still awaiting a
  decision is refused until the analyst rejects it, so no request is left open
  on a record nobody will look at again.
- **Human only**: the command is not in the AI allowlist.
- `POST /api/investigations/{id}/lifecycle {transition: "discard", reason}`.
- A discarded investigation leaves the work queue and the Investigations tree;
  it never counts as the investigation of a case, so the case can be started
  again. Its panel stays readable, parked like an archived one.
- The workbench header offers **Discard draft…** on a draft, asking why.

## Alternatives rejected

- **Delete the draft.** The event store is append-only; a deletion would also
  erase that the investigation was started, and by whom.
- **Allow archive from draft.** Archive means "concluded, retained"; a
  discarded draft was never concluded, and the two must stay distinguishable
  in the record and in retention.
- **Discard automatically after a period of inactivity.** Leaving a draft is
  the analyst's decision; a timer would discard work someone means to return to.

## Consequences

- The status set gains `discarded`; every surface that treats archived as
  closed treats discarded the same way.
