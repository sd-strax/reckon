# 0095 — The retrieved record folds by key: a pivot and a group-by, never a baseline

Issue: #95 · PR: pending · Status: accepted

## Context

The retrieved record repeats itself and the timeline showed every repetition.
In the reference scenario the file server has nine logons: seven are one
service account's nightly network logon from the backup host at 02:00, one is
a user's RDP logon, one is the service account by RDP from a workstation
address in the afternoon. The Story timeline showed nine rows and the odd one
was row nine; the Entity inspector's "Normal behaviour" section said a
baseline never exists (`design/ui/00-backend-binding.md §6` item 14).

The contract in `implementation/workbench-react.md §8.3` fixed the shape as
an "entity baseline": a pure fold for one entity in one investigation,
grouping events of one class by a per-class key, a group of three or more
events on three or more distinct days a "repeated pattern", the rest
"deviations", a per-entity endpoint, 204 when nothing repeats. Read literally
that is a new endpoint, three fixed field lists and the vocabulary of
behaviour analytics. Read as a concept it is a **pivot** (the entity filter
the events read has had since `adr/0092`) and a **group-by** (a key per
class over the retrieved events) with a having-clause deciding which groups
collapse: a counting fact about the record the case holds, with no data from
outside the case, nothing learned, nothing scored. The decision holds; this
record chooses the rendering.

## Decision

**The group-by rides the events read.** `GET /api/investigations/{id}/events`
takes `group_by`, `default` or `class:slot,slot;…`. With it the read is the
newest `MaxEventLimit` events matching the filter, unpaged, and the response
carries `groups` (class, key parts with their values and refs, count, first,
last, distinct UTC days, member ids newest first), `keys` (per class present:
its slots, the default key, the key used) and `folded_over`. `entity` is then
the pivot. There is no baseline endpoint and no stored summary.

**The key is the event's slots.** Each class template in `capability/ocsfview`
registers its slots as it renders: the objects the event derived and its
distinguishing scalars (logon type, activity, command line, port). A slot keys
by the object the record minted when there is one, else by its text folded
for case, so a file keys by its identity, hash-first (`03 §7`): a renamed copy
folds with its original, two binaries of one name stay apart. The process
itself and the network flow are per-occurrence by construction and never a key
by ref; their slots carry the name. `Fold` is pure: a class without slots, or
a key with no value at all, never folds.

**Defaults are the sheet's lists, the account added to authentication.**
Authentication keys on account, logon type, source and destination; network
on destination and port; process on image and parent; every other class on
all its slots, so only identical events fold. The analyst changes the key per
class in the Story lens, per view; the choice travels as the query parameter
and is persisted nowhere. There is no tenant setting.

**Collapsing is display.** The engine returns groups of every size. The
webview collapses a group of three or more events on three or more distinct
days into one row at the group's first event, "N events over M days", opening
to its members; every other group renders as plain rows. A "fold repeated
events" control turns the fold off, which is the flat paged read of
`adr/0092`; a re-read of the record keeps the fold the timeline is in.

**Pivoted to an entity, its slot leaves the key.** Every event there already
shares it. The Entity inspector's section, titled **Repeated observations**,
is the grouped read for the entity with the default key, shown only when a
group collapses: the span and the sentence of the newest member per group,
then how many of the entity's retrieved events fold into none. No "normal",
no "deviation", no typical hour.

**Not in scope.** The agent reads the same events and gets no tool; the export
prints cited events, which are not grouped.

## Alternatives rejected

- **A per-entity baseline endpoint with patterns and deviations.** The
  literal §8.3. A second read over the same rows, and the words promise a
  verdict the engine does not make; the group-by says exactly what it counts.
- **Fixed field lists per class.** Written in a third place beside the
  normalizer's extraction list and the renderer's slots; and the analyst may
  want the hash, not the image name, as the key. The renderer's slots are
  the one list, and the sheet's lists are the defaults.
- **Fold in the webview.** The export and any later server-side consumer would
  need the same fold; and the fold needs the whole record, not the page the
  webview holds.
- **Fold consecutive runs only.** Preserves order, but splits a repetition at
  every interruption, and the interruptions are the rows the analyst is
  looking for.
- **A learned profile, scoring or thresholds beyond the collapse rule.** That
  is behaviour analytics, a different product on data outside the case.

## Consequences

- A grouped read renders up to `MaxEventLimit` rows per request; the
  response says how many of the total it folded over, and the lens shows it.
- `ocsfview.View` carries `Slots`, not on the wire; the fold reads them.
- The Story lens starts folded; `events.fold` changes the key or turns the
  fold off, and the host remembers the choice per investigation for the
  session.
- The "Normal behaviour" section is gone; "Repeated observations" appears
  only when something repeats.
- `§8.3` is read as this record describes; its paragraph gains a line naming
  the parameter.
