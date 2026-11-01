# 0226 — The large files split along their seams, and handlers test against interfaces

Issue: #226 · Status: accepted

## Context

`server/backend.go` held the config, the lifecycle, the status endpoints, the
capability swap, the dependency probes and the JSON helpers. `server/actions.go`
mixed handlers with workflow orchestration and `actorFromClaims`, the one
derivation of who is acting. `aggregate/action.go` held event payloads, the fold,
commands and apply functions in one 1045-line file. Most server tests needed
Postgres, because handlers could only reach the concrete stores.

## Decision

- **`server/backend.go`** keeps the config and the lifecycle. `status.go` has the
  health, status, auth-config and identity endpoints and the dependency probes,
  `surface.go` the capability swap and `Reload`, `respond.go` the JSON and error
  helpers, and `routes.go` the router.
- **`server/actions.go`** keeps the handlers and views. `action_dispatch.go` has
  the path from an approved action to its workflow, `action_sweep.go` gains the
  expiry timers, and `actor.go` holds `actorFromClaims` alone, so a change to it
  stands out in review.
- **`aggregate/action.go`** keeps the constants. `action_events.go` has the
  payloads, `action_fold.go` the state and the fold, `action_commands.go` the
  commands and their validation, and `action_apply.go` the apply functions.
- **Only the stores that touch Postgres get interfaces.** `BackendConfig` has
  about thirty fields, and four reach a database: `Handler`, `Evidence`,
  `Knowledge` and `Comms`. Each becomes a small interface in `server`, declaring
  what handlers call, as `actionStarter` and `pipelineStarter` already do. Reads
  get their own field, because `Handler.Reads` returns a concrete type. Strings,
  flags and the in-memory policy and catalog objects stay concrete.
- **`TestRouteAuthorization` and one handler test per store run on fakes**, in
  `-short` mode, with no Postgres. The other integration tests stay: they exercise
  the real stores and already pass.

## Consequences

An interface holding a nil pointer does not compare equal to `nil`, so a "not
configured" check would pass and the call would crash. `runtime` is the only
production constructor and always builds these stores, so a store that is off
must be left unset, never passed as a nil pointer.

No API answer changes. The moved code is unchanged apart from imports.

## Alternatives considered

Grouping `BackendConfig` into nested structs: it reads better but tests nothing,
and only `runtime` constructs it. Interfaces for every field: the in-memory
objects are already usable from a test, so they would be indirection with no
caller.
