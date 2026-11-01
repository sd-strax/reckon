# 0246 — The workbench host has one copy of each helper and one path for a panel write

Issue: #246 · PR: #288 · Status: accepted

## Context

The host had grown by copying. `errText` was defined five times and inlined a
sixth; `makeNonce` three times, each drawing from `Math.random`, which is not a
cryptographic source and so makes the CSP nonce the weakest part of the panel's
policy. Eighteen methods of the investigation document had the same body: call
the client, toast the failure as `reckon: <verb> failed — <message>`, re-read
the record. The tree refresh command appeared four times, the tab title format
five, and the create-or-open-existing flow (a case already investigated opens
its investigation, #158) twice. `decide` re-read the record a second time when
the action it was asked about was not in it.

`adr/0245` gave the host a test seam, so a consolidation can be checked against
the behaviour the tests pin rather than by inspection.

## Decision

- `workbench/src/util.ts` is the one home of the host's small helpers:
  `errText`, `makeNonce`, `escapeHtml`, `panelTitle`. Every private copy is
  deleted and every module imports from it. `documentText.ts` keeps only the
  candidate-SOP markdown it is named for.
- `makeNonce` returns 24 bytes from `crypto.randomBytes` as 32 base64url
  characters, every one legal in a CSP `nonce-` source.
- `InvestigationDocuments.mutate(id, panel, verb, fn, {reload})` is the one
  path for a write from the panel: run `fn`, toast a failure under `verb`, then
  refresh the record (or reload the whole snapshot when the write changed
  slices the refresh does not carry, as enablement does). The eighteen methods
  call it; their guards (an empty reason, an unchanged restatement) stay in
  front of the call, and their success toasts inside `fn`.
- `refreshTrees({cases})` re-reads the Investigations tree, and the Cases tree
  when a case changed hands. `createOrOpen(create, opened, existing, failed)`
  holds the identity guard, the write-back offer, the tree refresh and the
  already-investigated recovery once; the draft seed and start-from-case supply
  what differs (re-keying the draft panel, the toasts).
- `decide` refreshes after a decision it attempted. When the action is not in
  the record after the read made to find it, it tells the panel the decision
  settled and stops; the read it just made is current.

No message to the webview, no toast and no command changed wording.

## Alternatives rejected

- **`documentText.ts` as the shared module.** It already exported `errText`
  and `makeNonce`, but its name and header say what it is for (the text the
  investigation document builds), and auth and the trees have no business
  importing it. A module named for what it holds keeps the next helper from
  landing in the wrong place.
- **`mutate` taking the whole failure message.** Every site used the same
  shape; a verb keeps the wording in one place and a site cannot drift from it.
- **A shared escape helper for the side panels' browser-side script.** That
  script is a template string outside the bundle and cannot import a host
  module; the panels move onto the shared protocol under #243, where JSX
  escapes. `escapeHtml` serves the host side of what remains.

## Consequences

- A failure from the panel reads the same everywhere, and a new write is one
  `mutate` call rather than a copied block.
- The CSP nonce comes from the platform's CSPRNG.
- `util.test.ts` pins the nonce's length and alphabet and the escape set.
- The inline `esc` in `caseDocument.ts` is the last escape helper outside
  `util.ts`; it goes with #243.
