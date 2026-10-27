# 0095 — An entity's baseline is a counting fact folded from the retrieved record, shown only when it holds one; the timeline collapses a repeated pattern into one row

Issue: #95 · PR: #96 · Status: accepted

## Context

A capability answer that establishes normal behaviour — the reference scenario's nightly
service-account logon, retrieved seven nights deep — rendered as one Story timeline row per
night, and the entity inspector's "Normal behaviour" section had nothing to show, so the one
interactive logon from a workstation did not read as the deviation it is. Gap 14 of
`design/ui/00-backend-binding.md §6` asked for a summary the inspector and the timeline can
render, shown only when the record holds one and never inferred by the UI.

Constraints: the capability layer does not reason (`03`), so a baseline must be a mechanical
fact about what was retrieved, not a judgment about what is normal; it must come from the
record the investigation actually retrieved (`adr/0092`), captioned with the window that record
covers; and a surface may not derive a baseline the engine did not (`00 §1`).

## Decision

- **A baseline is a counting fact, folded server-side** by `capability/ocsfview.Baseline`, a
  pure function over the retrieved events that cite one entity: events of one class are
  grouped by the class's **pattern key** (`PatternKey`: authentication → logon type · source ·
  destination; network → destination · port; process → image name · parent); a group with at
  least three events on at least three distinct days is a **repeated pattern** (count, days,
  the most frequent UTC hour, first and last, the event ids); an event of a class that has a
  pattern and matches none is a **deviation**, listed with the key fields that differ from the
  nearest pattern. Classes with no key take no part.
- **`GET /api/investigations/{id}/entities/{ref}/baseline`** answers the fold over the
  investigation's events that cite the entity, with the window the record covers and the
  number of events considered, or **204** when nothing repeats. The events listing carries each
  row's `pattern` key.
- **The surfaces show it only when the record holds one.** The entity inspector's "Normal
  behaviour" section says "N events over M days, typically around HH:00 UTC" per pattern and
  "How this case deviates" per deviation, captioned with the retrieved window; otherwise it says
  no baseline exists and that one is never inferred. The Story timeline collapses a pattern's
  events into one row at its first event with the same sentence (the same thresholds applied
  over the server's keys, for display), expandable to its events. The word "normal" is the
  section title only.

## Alternatives rejected

- **A baseline from a wider query** (re-asking the capability for the entity's history): a
  second outbound call for a display fact; the baseline is what this investigation retrieved,
  captioned as such, and widening the window is the analyst's act.
- **A learned or statistical notion of normal**: a judgment the layer is not allowed to make;
  thresholds of three events on three days are counts an analyst can verify by eye.
- **Letting the UI derive patterns from payloads**: the key per class is engine knowledge
  (`adr/0090`); the webview applies the same counts over the engine's keys and never reads a
  payload for it.

## Consequences

- An entity retrieved on fewer than three days has no baseline, by design; the inspector says
  so rather than guessing.
- The thresholds live in one Go constant pair and one TypeScript pair; a change is a change to
  both, covered by the fold tests on each side.
- Gap 6.13's `?around=` and this fold share the event rows; a future per-class key (DNS, file)
  is one case in `PatternKey`.
