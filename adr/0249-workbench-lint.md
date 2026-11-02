# 0249 — The workbench has a linter, and the compiler checks indexed reads

Issue: #249 · PR: #298 · Status: accepted

## Context

The workbench had no ESLint, yet two `eslint-disable` comments named rules that
nothing ran. TypeScript ran in strict mode without `noImplicitReturns` or
`noUncheckedIndexedAccess`, so an array or map read typed as always present. Some
comments described the code as it had been: the investigation document as a
"next step", and a JSDoc block above the wrong method.

## Decision

- **ESLint runs in `make workbench-ci`, and any finding fails it.** The config is
  `@eslint/js` and `typescript-eslint` recommended everywhere, plus
  `eslint-plugin-react-hooks` recommended on the webview. Unused disable
  directives are errors. `make ci` stays Go-only.
- **Underscore-prefixed names count as deliberately unused,** the convention
  `noUnusedParameters` already allows.
- **Every finding is fixed, or kept with its reason inline.** The one kept is the
  `DraftView` effect that runs on `path` alone, so focus does not jump while the
  analyst types.
- **Both compiler flags are on** for the extension, the webview and the harness
  recorder. Production code handles the missing element with a guard or a
  fallback. Tests assert with `!`, because a missing element there should fail
  the test.
- **The four packages are dev dependencies only** and do not ship in the
  `.vsix`. Each is MIT-licensed. What they pull in is MIT, ISC, Apache-2.0, BSD,
  BlueOak-1.0.0, and CC-BY-4.0 for the `caniuse-lite` data package.

## Alternatives

- **Biome.** One dependency in place of four, but weaker coverage of React hook
  rules, and it would mean replacing the existing directives.
- **Lint warnings instead of errors.** Cheaper now, but warnings accumulate, and
  the lint would not own anything.
- **`noImplicitReturns` alone.** Close to free, but it leaves out the check that
  finds unhandled missing elements.

## Consequences

The React hooks rules found two bugs, both now covered by tests. Text staged in
the composer came back after every turn, because the effect re-ran each time the
input re-enabled. A rename started from the tree reopened the editor when the
new title arrived. Both now apply each request once, keyed by its nonce.

Rule changes in a new plugin release can add findings. Each version is pinned
in the lockfile, so that happens only when someone bumps it.
