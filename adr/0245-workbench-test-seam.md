# 0245 — The workbench host is tested against a typed `vscode` fake, without an editor

Issue: #245 · PR: #TBD · Status: accepted

## Context

The webview was well covered and the host was not. Every host module that
imports `vscode` (the investigation document, the sidecar transport, auth, the
turn driver, the trees and side panels, activation) could not load under
vitest, because `vscode` exists only inside a running editor. The host tests
that did exist covered only the modules that stay clear of it.

The host bugs found so far are races and state bugs in the host's own code: a
panel closed while a read is in flight, a sidecar that dies during the
handshake, a token refresh that fails, a decision submitted twice. A test for
any of them has to control timing and inject the failure at an exact point.

## Decision

- `workbench/src/test/vscode.ts` is a hand-written stand-in for the `vscode`
  module, and `vitest.config.ts` resolves `vscode` to it. Host modules load and
  run unchanged.
- It covers the API the host uses at runtime. Its classes `implement` the real
  types and its namespace literals `satisfy` the real namespaces' member
  names, all checked by `tsc -p ./` (the compile step of `make workbench-ci`),
  so a member the real API does not have fails the build.
- Panels and webviews are recorders the test drives: `receive` delivers a
  message from the page, `posted` holds what the host sent, `dispose` closes
  the panel as the user would, and a disposed webview throws on `postMessage`
  as the real one does. `EventEmitter` and `SecretStorage` are working
  implementations. Messages, dialogs and commands are spies.
  `resetVscodeFake` restores fresh state between tests.
- Host tests run in the node environment. The sidecar transport is tested
  against a mocked `child_process.spawn` whose stdio carries a vscode-jsonrpc
  peer, so the real message framing is exercised. Auth is tested against a
  stubbed `fetch` and fake timers.
- The first tests cover the turn driver, the sidecar transport, auth, and the
  investigation document's panel lifecycle. They pin current behaviour; each
  host bug fix brings its regression test on this seam.

## Alternatives rejected

- **`@vscode/test-electron`** (run the suite inside a real editor). It does not
  meet "without an editor". It needs a downloaded editor and a display in CI,
  and the editor, not the test, owns the timing of disposal, process exit and
  token expiry, so the races above cannot be made deterministic. A thin
  activation smoke test on it remains possible later. It would complement this
  seam, not replace it.
- **Narrow interfaces injected into each module** (a UI port, a secret store, a
  log), with the `vscode` implementations built at activation. It is cleaner in
  the end, but it means a large refactor of the investigation document and
  activation to reach the same testability, and a module can be moved onto
  ports later without losing the tests.

## Consequences

- Any host module can now be tested. A test that needs an API the fake lacks
  adds it to the fake, typed against `@types/vscode`.
- Tests never ship: `.vscodeignore` excludes the compiled fake (`out/test/`)
  and every compiled `*.test.js`, which the package carried before.
- The fake checks shapes, not behaviour: where the real editor behaves
  differently from the fake (ordering of events, what a disposed object does),
  the test is only as good as the fake. Its behaviours follow the real API
  where the host depends on them, and are documented in the fake.
