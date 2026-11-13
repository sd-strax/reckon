# 0350 — The Story timeline's grouping controls are one line: an event count, a switch, and one Grouping panel

Issue: #350 · PR: #TBD · Status: accepted · Amends: the controls of `0095`, and the count after `0348`

## Context

`adr/0095` let the analyst fold repeated events and change, per event class, which fields
must match for events to fold. The controls were:
- a "Fold repeated events" checkbox
- one link per class ("Authentication: Account · Logon type · Source · Destination ▾"),
  each opening a list of checkboxes

Nothing said what the links were for. They read as unrelated links, and the checkboxes
meant something only to a reader who knew what a fold key is. The count beside them, "22
retrieved events in the record", counted retrievals. Since `0348` one row can stand for
several retrievals, so the count disagreed with the rows.

## Decision

- **One line above the timeline**:
  - **N events · M retrievals**, counting the events the timeline shows (after `0348`'s
    merge, before folding). The retrievals part appears only when it differs. When the
    record holds more than the page read, the line also gives the record's total, as
    before.
  - A **Group repeats** switch, which turns folding on and off (`0095`'s fold). It
    remembers its state as before.
  - A **Grouping…** button, offered when the timeline has an event kind that can fold.
- **Grouping… opens one panel**, themed like the editor's inputs:
  - A one-line explanation at the top: "Events of one kind are grouped when every ticked
    field matches."
  - One row per event kind present in the timeline: the kind's name, "group when these
    match:" and one checkbox per field. Each kind shows how many groups its key makes now,
    and a **Reset** appears once its key differs from the default.
  - The panel closes on Escape, or on a click outside it.
- **The key semantics are unchanged.** It is the same per-class key that `0095` sends, under
  the same names.

## Alternatives rejected

- **Keep a link per class, but restyle it.** It is still N controls for one setting, and it
  is still unexplained.
- **Hide the key choice entirely.** The analyst does sometimes need to group logons
  regardless of source, and `0095` made that the analyst's choice.

## Consequences

- The count matches the rows.
- Changing what groups together is one control that explains itself.
