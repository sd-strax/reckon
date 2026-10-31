# 0218 — The server reads through a read model and writes evidence through a store

Issue: #218 · PR: #TBD · Status: accepted

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
  of the aggregate's tables. Missing rows return `aggregate.ErrNotFound`.
- **The aggregate owns what an action event means.** It returns the action's
  history as typed, decoded entries; a decode failure is an error. One helper
  gives fold, projector and history the same status for an event. Fold and
  projector keep their own field updates, one filling memory and one filling
  columns. The server keeps the sentences and view shapes.
- **`Handler.DB()` is removed**, so the compiler holds the boundary. `temporal`
  moves to the read model and `SideStore` reads. `runtime` passes its own handle
  to `comms`. The dependency probe's `sql.Open` in `backend.go` stays: it checks
  a service, not investigation data.

## Consequences

- The action record fails on a corrupt event instead of showing a blank row, and
  a database failure on it is a 500.
- The server's status for an approval is the fold's. They could differ only on an
  event with no stage.
- SQL stays hand-written. An ORM would not hold this boundary, and the event
  model has no row to save.
- Server tests that exercise evidence need `Evidence` set; a nil store skips
  persistence and evidence routes answer 503.
