# 0223 — SIGHUP and the enablement endpoint run one reload routine, applying both sides or neither

Issue: #223 · PR: #277 · Status: accepted

## Context

A tenant config edit reaches a running backend without a restart in two ways
(11 §5.1). `reckon adapter setup`/`reload` signals SIGHUP; the workbench's
enablement endpoint (`POST /api/enablement/adapters/{name}`) rewrites an
adapter's stanza itself. The file is the single authority for both the read
side (`adapters`/`bindings`) and the write side
(`action_adapters`/`action_bindings`).

The two paths had drifted. The SIGHUP handler rescanned the adapter host,
rebuilt and swapped the capability surface, then rebuilt the action resolver
and swapped it into the worker's dispatch holder and the `/action-types`
readout, each side on its own: a write-side failure was logged and the read
side stayed swapped. The endpoint rebuilt the capability surface only, with no
rescan and no write-side rebuild. After an apply the process ran the read side
of the file as it is and the write side as it was at the last signal or boot,
and a plugin installed without a signal could not be enabled from the
endpoint. Nothing serialised the two, and two applies raced on the file's
read-modify-write.

## Decision

- **One routine.** `Backend.Reload` builds both sides from the file and swaps
  the capability surface, the `/action-types` readout and the worker's
  dispatch holder. SIGHUP and the endpoint both call it.
- **Both sides or neither.** A build failure on either side swaps nothing and
  returns the error: SIGHUP logs it, the endpoint answers a 500 saying the
  file was written and the reload failed. Investigation and response are both
  the product, and boot already refuses a file whose write side does not
  build; a reload that applied half of it would leave a process no restart
  with the same file reproduces.
- **The build is injected.** Runtime holds the adapter host and the dispatch
  holder, so it supplies `BackendConfig.Rebuild` (rescan, build capability,
  build action) and `BackendConfig.ActionDispatch`. With none injected the
  backend builds in-package, fixture adapters only.
- **One at a time.** Reloads are serialised, and the endpoint holds the same
  lock across its file write and the reload, so concurrent applies do not lose
  each other's edit and the last reload reads the latest file.
- `ReloadCapability` and `SetActionResolver` are removed; runtime was their
  only caller.

## Alternatives rejected

- **Have the endpoint send itself SIGHUP:** the endpoint could not report a
  failed reload to the analyst who asked for it, and the file write would
  still race.
- **Keep the sides independent:** a broken write stanza would leave the
  process running only half of the file's intent, with the failure in a log
  line.

## Consequences

- A write-side stanza that does not build blocks an unrelated read-side
  enablement until it is fixed, which is the same outcome a restart gives.
- A reload logs one line, success or failure, instead of one per side.
- Edits made by another process (`reckon adapter setup` writing the file) are
  not covered by the lock; they are followed by a SIGHUP, which reads the file
  under it.
