# 0225 — The PR gate runs the full test suite, embedded-deps tests included

Issue: #225 · PR: #TBD · Status: accepted

## Context

CI ran `go test -race -short ./...`. Under `-short` most of the server and
aggregate suites skip: every package whose `TestMain` boots embedded Postgres
(`aggregate/`, `server/`, `knowledge/`, `knowledge/substrate/`), so `Handle`,
`Replay`, the optimistic-concurrency tests and the HTTP surface were never run
on a PR. The supervisor's Postgres and Temporal lifecycle tests and the worker
round trip against a Temporal dev server skipped too. `make test-all` ran them
all, but only when someone remembered to.

A test that only runs on request does not gate anything. Every later fix
should land with a test the PR actually runs.

## Decision

- `make ci` is lint + `make test-all` + build, and the CI `engine` job runs
  the same steps, so the local gate and the PR gate are one gate.
  `make ci-full` is kept as an alias. `make test` (`-short`) stays as the inner
  development loop.
- CI caches the downloaded test binaries between runs: the embedded-postgres
  archive (`~/.embedded-postgres-go`) and the Temporal dev server
  (`temporal/worker_test.go`'s bin directory), keyed on `go.sum`.
- `TestWorkerPingRoundTrip` dispatches `RenormalizePass`, the remaining
  skeleton workflow, to prove the domain inventory is registered on the
  `reckon` task queue. It dispatched `ActionLifecycle`, which now has a body:
  with no activities registered on the test worker, that only failed after
  its activity retry budget ran out, about five and a half minutes, and
  tested a timeout rather than registration.

## Alternatives rejected

- **A separate tier of only the Postgres-backed packages:** it would leave the
  supervisor's own Postgres and Temporal components, the path a real install
  boots through, untested on PRs.
- **Scheduled or label-gated runs:** they find a break after it merges, or
  only when someone applies the label.
- **Keep `make ci` fast and enforce in CI only:** two gates drift; a change
  that passes `make ci` locally would fail the PR.

## Consequences

- `make ci` takes a few minutes, not seconds. Most of it is embedded Postgres
  boots, dominated by `initdb`.
- If the PR job grows too slow, the supervisor and Temporal lifecycle tests
  are the ones to move out to a scheduled job; the Postgres-backed packages
  stay.
- The supervisor's own Temporal test downloads the Temporal CLI into a fresh
  temp directory each run, so that download is not cached.
