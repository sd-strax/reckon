# 0030 — The case queue is a read-only view with its own non-persisting read surface

Issue: #30 · PR: #31 · Status: accepted

## Context

A T2/T3 analyst's day starts in the case system's queue, already assigned and
prioritized. After #28 the tenant could name that queue (`policies.case_queue`,
`design/14-case-seed.md §4.2`), but the only way to see it was to begin a new
investigation and expand *From a case…*. The analyst could not browse the
queue, read a case, or see whether a case was already being investigated
without starting a draft.

Three things bound the answer:

- **Reads through the capability layer persist.** `POST /api/capability/{verb}`
  promotes everything it reads into the telemetry layer (`design/03 §4.13`),
  because a ref the agent *cites* must stay openable. Browsing a queue on every
  refresh is not citing evidence; it would write every case into the store
  again each time.
- **Writes are actions inside an investigation.** A `ticket.*` write is an
  x-action, grounded by evidence refs and recorded against a human principal
  (`design/04`, `08`). Before the investigation exists there is nothing to
  record a comment or a state change against.
- **Two investigations on one case are legal.** `14 §6` deliberately keeps no
  dedup on `(source, case_id)`: a second analyst's parallel look and a
  post-reimage verification are both legitimate.

## Decision

- **A dedicated read surface, `GET /api/cases` and `GET /api/cases/{number}`.**
  Both resolve the case verbs through the capability layer like any read and
  **do not persist** the result. The case is promoted exactly once, when it
  seeds an investigation (`14 §3`). The picker's search now uses the same
  surface, so browsing no longer writes telemetry anywhere. The queue defaults
  to the tenant policy; `filter` narrows within it; any authenticated reader
  may call it (a read is T0).
- **Each row carries the open investigation rooted on it**, if any: an
  investigation whose seed is a case with the same `case_id`, not concluded or
  archived, newest first. This is a join on the projection, computed
  server-side; the active set is small.
- **A Cases tree view** below Investigations in the reckon container, fed by
  the queue. Tree items have no columns, so the label is the title and the
  description carries number · priority · status · age; the tooltip holds the
  rest. A refresh button, no poller (`14 §0` keeps push and polling triggers
  out of v0).
- **A read-only case document.** Clicking a row opens the case: metadata, the
  full body, a link out to the system of record, and one action. If no
  investigation is rooted on the case, **Start investigation** runs the
  existing seed flow (which fails closed on anything but exactly one case). If
  one is, the document shows it and the button opens it instead. Starting a
  second investigation remains possible through the picker, as an explicit
  act. The rule is enforced by the view, not the aggregate.
- **Nothing on these surfaces writes to the case system.**

## Alternatives rejected

- **A `persist: false` flag on the generic invoke endpoint.** It would hand
  every client a way to skip promotion, including the agent's tool path, where
  skipping it mints citations that cannot be opened. A dedicated surface says
  what it is for.
- **A hard guard against a second investigation per case.** Contradicts the
  deliberate non-decision in `14 §6`; the legitimate cases above are real.
- **Client-side join of queue and investigations.** Works at this scale, but
  the server already holds both and the filter belongs where the data is.
- **Writing "under investigation" to the case on start.** Deferred, not
  rejected: it is naturally the new investigation's optional first action (a
  comment on the seed case, through the same gate as any ticket comment,
  grounded by `case_ref`), which is the status-sync item `14 §0` already
  defers. Nothing here depends on it.

## Consequences

- The capability invoke endpoint is no longer the only reader of the case
  verbs; the two new handlers duplicate its coverage-to-status mapping in a
  smaller form. If a third non-persisting read appears, factor the mapping.
- Queue rows show the projection's view of "open"; a concluded investigation
  releases the case for a fresh one, which is the intended verification path.
- Memory annotations on queue rows (similar past investigations, matching
  SOPs) are the next step this surface invites. Separate decision.
