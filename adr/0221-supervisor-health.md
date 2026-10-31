# 0221 — A component's health is what it supervises, every probe has a deadline, and a restart is of one component

Issue: #221 · PR: #283 · Status: accepted

## Context

The supervisor polls each component's `Health` every 5s and restarts one that
misses twice (`implementation/supervisor-design.md`). Four things undercut it:

- `Backend.Health` reported the `started` flag. If the HTTP server's `Serve`
  returned on its own, the goroutine logged it and the backend stayed "ready"
  with nothing listening, so it was never restarted.
- The watcher passed its run context to `Health`, which has no deadline. The
  Temporal probe bounded itself (2s dial) and Keycloak's used a 5s client;
  Postgres and the worker had no bound on reckon's side. A hung probe stalled
  its watcher, so the hung component was never declared down, and the
  `/status` rollup probed the components one after another, so one hang
  stalled it too.
- `Postgres.Health` opened a new pool on every probe and closed it.
- A restart dropped `Stop`'s error. If `Start` then failed, the escalation
  logged the second failure without the first. Nothing bounded `Stop`
  either, and the Temporal, Postgres and worker `Stop`s do not watch their
  context, so a hung one blocked its watcher and, through it, shutdown.

The issue also asked that a restart carry the components that depend on the
one restarted.

## Decision

- **Backend health is the server serving.** `Serve` returning for any reason
  but `Shutdown` is recorded and makes `Health` not-ready with the cause; the
  watcher restarts the backend through the existing path.
- **Every probe has a deadline.** The watcher and the rollup call `Health`
  under `probeTimeout` (3s). A probe that has not answered by then is a miss,
  whether or not it honours its context. The rollup runs the probes at once.
- **Postgres keeps one handle.** `Start` opens it, `Health` pings it, `Stop`
  closes it.
- **Every `Stop` is bounded.** The supervisor waits on a `Stop` for at most
  `stopTimeout` (30s), whether or not it honours its context, both before a
  restart's `Start` and for each component in a shutdown. A restart logs
  `Stop`'s error before `Start`. A `Stop` that times out is not restarted
  over, since the old `Stop` may still be changing the component; the
  watcher escalates to fatal instead. A component is never in two `Stop`s
  at once: the shutdown that follows waits on the `Stop` still running
  rather than calling it again, since the contract is Stop once.
- **No restart cascade.** A restart is of the one component. Its dependents
  reconnect on their own: the worker and the backend's dispatch client
  carried on through a Temporal stop and start on the same port and SQLite
  file without being restarted (`temporal.TestClientsSurviveTemporalRestart`
  holds this), and the backend's token verifier refetches Keycloak's signing
  keys, which live in Keycloak's database. Postgres exiting ends the
  supervisor.

## Alternatives rejected

- **Declared dependencies with a cascading restart:** it adds stop/start
  ordering inside the watcher for dependents that already recover; the test
  that shows they recover is cheaper and fails if that stops being true.
- **Trust each probe and `Stop` to honour its context:** the deadline has to
  hold for a call that blocks on something else, or the watcher is only as
  live as the worst component.

## Consequences

- A backend whose server dies is restarted within two polls.
- A dependency that hangs is declared down after two probe deadlines, and
  `/status` answers within one.
- A probe or `Stop` that ignores its context leaves a goroutine blocked
  until it returns; the supervisor does not wait for it.
- A component whose `Stop` hangs on a restart ends the supervisor rather
  than being restarted, and a shutdown takes at most `stopTimeout` per
  component.
- If a future component holds state that a dependency's restart invalidates,
  it reports that in its own `Health`, and the watcher restarts it.
