# 0247 — The webview bundle does not reach the host's HTTP client, and `extension.ts` is only the composition root

Issue: #247 · PR: pending · Status: accepted

## Context

The webview is a browser bundle (esbuild, from `webview/src/main.tsx`); the
host is tsc-compiled Node code with the editor API and the tokens. The two
share types through `src/protocol.ts` and `src/viewTypes.ts`, which the webview
imports with `import type`, so nothing of the host is emitted into the page.

One import broke that: `ProvenanceInspectors.tsx` took `relevanceOfBand`, a
value, from `src/backend.ts`, the HTTP client, and `webview/tsconfig.json`
included that file to make the page compile. The page's dependency graph then
contained the client module, its request helpers and its DTO mappers; what of
it reached the bundle was left to the bundler's tree-shaking rather than to a
boundary.

`extension.ts`, the activation entry point, also held the Investigations tree
(its provider, item classes, group table and colours, about two hundred lines)
and the workbench posture, beside the wiring. The Cases tree was already its
own module. Nothing could construct the Investigations tree except activation,
so it had no test.

## Decision

- `workbench/src/relevance.ts` holds the presentation helpers both bundles
  render with (`relevanceOfBand`, `relevanceOfScore`). It imports neither
  `vscode` nor the client. The client imports it for `librarySearch`; the
  webview imports it for the inspectors. `webview/tsconfig.json` no longer
  includes `../src/backend.ts`.
- The rule that follows: the webview imports from `src/` only `protocol.ts`,
  `viewTypes.ts` and `relevance.ts`. A value the page needs from the host side
  goes into `relevance.ts` or a sibling with the same two constraints, never
  into the client.
- `workbench/src/investigationsTree.ts` is the Investigations view, exported as
  `CasesProvider` is; `workbench/src/posture.ts` is the posture (`applyPosture`
  and the first-run `offerPosture`). `extension.ts` constructs and wires.
- `investigationsTree.test.ts` pins the view's three top-level states (backend
  incompatible or unreachable, signed out, signed in), the grouping with its
  pending rollup, and the needs-me-first order within a group.

## Alternatives rejected

- **Move `relevanceOfBand` into the webview.** The host's `librarySearch`
  renders the same words; two copies would drift.
- **Put the helpers in `viewTypes.ts`.** It is a types-only module imported
  with `import type` everywhere; a value in it would make every one of those a
  runtime import.
- **Test the tree through `activate`.** Activation spawns the sidecar transport,
  opens channels and restores a session; a tree test has no use for any of it.

## Consequences

- The page can be built without the client module; a future import of it from
  `webview/src` is a visible boundary violation, not a tsconfig entry.
- `extension.ts` reads as the list of what the extension is made of.
- The trees and the posture can each be tested and changed on their own.
