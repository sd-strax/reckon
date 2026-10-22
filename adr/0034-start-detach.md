# 0034 — `reckon start --detach` returns when the stack is ready; foreground stays the default

Issue: #34 · PR: #35 · Status: accepted

## Context

`reckon start` is the supervisor process: it holds the console until `reckon
stop`. Everything else in the demo lifecycle already cooperates with scripts
(`stop` waits for the process to exit; `demo reset` refuses while a backend is
reachable; `demo seed` is a plain HTTP client), so the one blocking step made
the whole stop → reset → start → seed → restart sequence a two-terminal job.

## Decision

- **`--detach` (`-d`) on `start`.** It re-executes the same binary's
  foreground `start` as a child in its own session (`setsid`), with the
  console sent to `<data>/logs/start.log`, then polls `/status` until every
  component is ready. Exit 0 with the ready line when it is; exit 1 naming the
  log and `reckon status` when `--timeout` (default 3m) passes while the child
  is still starting; exit 1 immediately if the child exits first (bad config,
  port in use). Idempotent: if a supervisor is already running it does not
  start another, it waits for that one.
- **Foreground stays the default.** The process is the stack; a human
  diagnosing a bad start wants the log on the console, and a service manager
  (launchd, systemd) wants a foreground child it owns. Detach is the scripting
  shape, not the product's.
- **`scripts/demo-reseed.sh`** (and `make demo-reseed`) composes the
  primitives: stop if running, reset, start, seed, stop, start, status. No
  compound `demo rebuild` command: the script shows the restart that
  activates the seeded capabilities instead of hiding it.

## Alternatives rejected

- **Daemonize inside the supervisor** (fork in-process). Go cannot fork
  safely after the runtime starts; re-executing the binary is the idiomatic
  form and keeps one code path for the actual start.
- **A `--wait` on a start that still backgrounds itself by default.** Changes
  the default behaviour every existing operator relies on, for a scripting
  convenience.
- **A `reckon demo rebuild` command.** Hides the restart and duplicates the
  primitives' error handling; a script is the right altitude.

## Consequences

- The ready signal scripts wait on is `/status` reporting `ok`, the same
  surface `reckon status` reads — one definition of ready (see #24 for the
  supervisor's own ready line).
- `start.log` under the data directory is a second copy of the console; the
  rolling `reckon.log` remains the structured log.
