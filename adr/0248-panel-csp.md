# 0248 — Every reckon panel is a surface of the one bundle, loaded by one page under one policy

Issue: #248 · PR: #292 · Status: accepted

## Context

The investigation document loads the React bundle under a nonce-only CSP
(`default-src 'none'`, `style-src` and `font-src` from the extension's own
files, `script-src 'nonce-…'`) with `localResourceRoots` confined to
`out/webview` and `media`. The two side panels, the case document and Import
SOPs, each carried a whole page as a template string: their own CSS, a
hand-written script, a hand-written HTML escaper, a local message union, and a
weaker policy (`style-src 'unsafe-inline'`, no `font-src`, no
`localResourceRoots`). They sat outside the bundle, the build, the component
tests and the shared protocol.

`implementation/workbench-react.md §1` already records the shape for more than
one surface: one bundle, several roots, chosen by the `data-surface` attribute
the host puts on the root element.

## Decision

- `workbench/src/panelHtml.ts` builds the one page every reckon panel loads and
  the one set of panel options. The page carries the bundle's stylesheet and
  script and nothing inline; the CSP is the investigation document's, nonce-only
  for scripts, the extension's own files for styles and fonts, no network. The
  options confine local resources to `out/webview` and `media` and retain the
  context when hidden.
- The root element names its surface: `<div id="root" data-surface="…">`.
  `webview/src/main.tsx` reads it and mounts that surface's app:
  `investigation` (the default) is the existing app under its provider; `case`
  and `import` are small components in `webview/src/surfaces/` with local state
  over the same transport. Their styles are in `webview/src/styles/panels.css`,
  on the tokens.
- `protocol.ts` carries a pair of unions per surface (`Case*`, `Import*`) beside
  the investigation's, with the shapes the panels already used, so the host
  tests of both panels pass unchanged. The webview transport is generic over
  the surface's pair; the investigation's remains the default.
- The hosts (`caseDocument.ts`, `importView.ts`) keep their state and acts and
  lose their pages; both take the extension URI the page needs.
- `panelHtml.test.ts` pins the policy for every surface; `CaseApp.test.tsx` and
  `ImportApp.test.tsx` cover what the panels render and post.

## Alternatives rejected

- **Tighten the two template strings in place** (nonce the inline `<style>`,
  add `localResourceRoots`). It keeps two hand-written pages, two escapers and
  two local protocols outside the build and the tests; the next panel would copy
  a third.
- **A bundle per surface.** Three entry points and three stylesheets to ship and
  to keep in step, for pages that share the tokens, the primitives and the
  transport. One bundle is what the design already set; the surfaces' code is
  small beside the investigation's.
- **The Import surface as a section of the Knowledge document** (`workbench-react.md
  §2.6`). That document does not exist yet; building it is not a CSP fix. The
  surface moves there as a component when it does.

## Consequences

- One place decides what a reckon panel may load; a new panel is a surface and
  a pair of unions, not a page.
- No hand-written HTML or escaping remains in the host; the escaping is JSX's.
- `CaseDocuments` and `ImportSOPsView` need the extension URI, as the
  investigation document always did.
