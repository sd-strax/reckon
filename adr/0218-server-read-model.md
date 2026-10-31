# 0218 — The server reads through a read model and writes evidence through a store

Issue: #218 · PR: #280, #282 · Status: accepted

## Context

`aggregate.Handler.DB()` gave the HTTP layer a raw `*sql.DB`, and nine server
files ran their own SQL. `persistInvokeResult` wrote `ocsf_events`,
`stix_objects` and `stix_edges` from a handler file, in a transaction it opened
itself. `loadActionRecord` read `events` directly and worked out what each
action event meant, a third time after `foldActionEvent` and
`ActionCurrentProjector`. It ignored decode errors, so a corrupt payload showed
as a blank history row, and it reported every `LoadActionCurrent` error as "not
found", so a database outage was a 404.

## Decision

- **`evidence.Store`** owns evidence: `Persist` writes one invocation's telemetry
  and objects in one transaction, and `Object`, `Event` and
  `IndicatorNeighbourhood` read them. Missing and malformed refs are the errors
  `ErrNotFound` and `ErrMalformedRef`. It reaches the server as
  `BackendConfig.Evidence`. Rendering stays in the server: `EvidenceView`, the
  export's labels, the case-system name.
- **The reasoning projector keeps its `stix_objects` writes.** They belong to the
  event transaction. The store reads those rows and never writes them.
- **`aggregate.ReadModel`**, reached through `Handler.Reads()`, holds every read
  of the aggregate's tables and side stores. Missing rows return
  `aggregate.ErrNoRecord`; `ErrNotFound` stays the write side's, a command
  addressed to an investigation that does not exist.
- **The aggregate owns what an action event means.** `ActionHistory` returns the
  action's events as typed, decoded entries with the status each one shows; a
  decode failure is an error. `approvalStatus` and `resultStatus` hold the two
  status rules that are computed, shared by fold and projector. History uses
  `approvalStatus` too but shows a result's final outcome verbatim, because the
  record must keep PARTIAL and TIMEOUT that the action's status folds away
  (08 §6c). Fold and projector keep their own field updates, one filling memory
  and one filling columns. The server keeps the sentences and view shapes.
- **`Handler.DB()` is removed**, so the compiler holds the boundary. `temporal`
  moves to the read model. `runtime` passes its own handle to `comms`. The dependency probe's `sql.Open` in `backend.go` stays: it checks
  a service, not investigation data.

## Consequences

- The action record fails on a corrupt event instead of showing a blank row, and
  a database failure on it is a 500.
- The server's status for an approval is the fold's. They could differ only on a
  two-party event with no stage.
- Fold and projector agree on an unrecognised final outcome: FAILED, with no
  success inference. The projector used to read it as SUCCEEDED.
- The thread fails on an event whose actor does not decode, as it already did for
  its payload.
- SQL stays hand-written. An ORM would not hold this boundary, and the event
  model has no row to save.
- Server tests that exercise evidence need `Evidence` set; a nil store skips
  persistence and evidence routes answer 503.
