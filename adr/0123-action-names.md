# 0123 — The action catalog names each action type and says what it does to the incident

Issue: #123 · PR: #TBD · Status: accepted

## Context

An action descriptor carried only the machine id (`ticket.comment`) and an
intent written for the model. The workbench titled cards with the raw id and
guessed the response group from the id's prefix, defaulting anything it did
not recognise to "containment": a comment on the case record read as a
containment plan, and the progress row counted it as containing the incident.
A new action type (a real vendor's `vendor.quarantine_device`) would have been
guessed the same way.

## Decision

- Every `ActionDescriptor` declares a **Name** — the short human title
  ("Isolate host", "Comment on a ticket") — and a **Category**: `containment`,
  `eradication`, `recovery`, `notification` or `case_record` (a write to the
  system of record, never containment). Inverses are `recovery`.
- Both are pinned by the catalog freeze test, like tier and reversibility.
- The actions list serves `name` and `category` on every row from the catalog;
  the action types listing serves them in the descriptor.
- The workbench titles a card by the name (the id beside it, muted), groups by
  the category, and the progress row's Contain step counts only containment and
  eradication. The id-prefix guess stays only for a row served without a
  category.

## Alternatives rejected

- **A name/category table in the workbench.** A second source for what an
  action is; every new type, and every adapter-contributed type, would need a
  workbench release.
- **Derive the category from D3FEND.** Ticketing and notification carry no
  D3FEND id, and a technique id does not say containment versus recovery.

## Consequences

- The agent's `list_action_types` output carries the two fields too; the
  catalog hash recorded by eval runs changes once.
- A descriptor added later must state both, or the freeze test fails.
