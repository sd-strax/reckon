# 0090 — An event reads as one sentence everywhere, and a prediction carries its title

Issue: #90 · PR: pending · Status: accepted

## Context

An OCSF event appeared on every surface as its class name and time:
"Authentication @ 14:32:11Z" in the Story timeline, the Evidence inspector
header and the exported report. To learn who logged on where, the analyst
opened the raw payload and read OCSF field names, although the normalizer had
already extracted exactly those entities to mint the observed-data
(`03 §4.1`–`§4.12`). A prediction had no name beyond its ordinal; the tracker
clipped the statement at render time, and each surface clipped at its own
length, so one prediction read three ways and nobody had authored any of them
(`design/ui/00-backend-binding.md §6` item 12).

The contract in `implementation/workbench-react.md §8.1` had fixed the shape:
display knowledge lives server-side in one pure package so the timeline, the
inspector, the export and the later baseline fold agree; a prediction's title is
recorded with it and never computed at render time. What remained was where
the rendering runs, where the entity refs in a sentence come from, and what
the aggregate does when no title is authored.

## Decision

**`capability/ocsfview` renders an event.** `Render(event, entities)` is a
pure function of the payload and the objects the record derived from the
event. It returns the `EventView` of §8.1: the sentence as a token list, text
and typed entity slots the client renders as chips; a curated field list, each
field with its value, the entity ref when it names one, a monospace flag and a
tone for an outcome; and the category word (`process`, `detection`, `auth`,
`network`, `asset`, `case`, `other`), from `category_uid` with the case class
named apart. Every class a normalizer handles has a template, plus device
inventory (5001). A class without one gets the payload's top-level scalars as
fields and no sentence. The `decoded` field of §8.1 is reserved and not built
here (#300).

**The view is rendered at read time.** Nothing is persisted at ingest. The
events read loads the page's payloads and derived objects and every `EventRow`
carries `view`; `GET /api/evidence/{ref}` carries `view` on an OCSF event. One
source of truth: a template fix applies to every event already in the record.

**Entity refs come from the record, not from a read-time identity run.** An
event row does not record the source scope it was ingested under, and identity
is minted per scope namespace (`03 §3.5`), so re-running the normalizer at read
time would mint wrong ids for a scoped source. The store already persists the
chain from event to observed-data to `object_refs` and holds each object's
properties. `attachEntities` reads those objects for the page; a sentence slot
binds to the object of its type whose properties carry the value the payload
shows (a host by hostname, an account by its shaped login, an address by value,
a process by pid), and never by type alone, so two accounts on an account
change or two addresses on a connection bind correctly. A slot the record
never minted renders as text.

**A prediction carries its title.** `PredictionNode.Title` is optional on the
`prediction` interpretation, one line of at most 64 characters, authored by the
agent (`record_prediction` gains `title`) or the analyst (the "+ prediction"
dialog gains a title field). When absent the aggregate derives one at record
time, deterministically: the statement up to its first clause boundary when
that leaves a readable head, clipped to 64 characters, no trailing period, and
records `title_derived`. Title and flag land on the recorded event, the
`x-prediction` payload (`01 §x-prediction`), `prediction_current` (migration
0027) and `PredictionView`. The tracker, the chips, the record lines and the
export show the stored title; the webview's render-time clip is gone. There
are no stored predictions to carry forward, so the derivation lives in the
aggregate only.

**The export reads the sentence.** A cited event is labelled by its sentence
as plain text, with its time; a class without a template keeps the class name.

## Alternatives rejected

- **Persist the view at ingest.** Cheaper per read and freezes what the analyst
  saw, but every template fix would leave older events rendered the old way,
  and the record would hold a second, derived copy of its own facts.
- **Render client-side keyed by `class_uid`.** The binding allowed it; the
  export is server-side, and the baseline fold of §8.2 needs the same
  rendering, so the knowledge would have been written twice.
- **Re-run the normalizer at read time for the refs.** Deterministic in a
  single-scope install and wrong under a source scope, since the row does not
  carry its scope. The record's own refs are the truth.
- **Bind a slot by type when the record holds one object of that type.**
  Robust against a shaping mismatch, wrong for the slot the record never
  minted (an actor without a name on an account change would take the
  target's ref). Value matching with shaping-aware login comparison instead.
- **Derive titles in the projector on replay.** Nothing to replay; one place
  to derive is one place to be wrong.
- **Leave the title to the UI when absent.** The contract's rule is "never at
  render time"; a derived title that differs by surface is the defect.

## Consequences

- An events page read now carries payloads and derived objects: two queries
  per page instead of one, and heavier rows. The page is bounded by
  `MaxEventLimit`.
- `evidenceStore` gains `EventEntities`; fakes implement it.
- The `record_prediction` tool definition changed, so the prompt version and
  the tool's digest moved (`adr/0232`); the eval run that re-accepts the
  baselines is scheduled with the other model-facing changes, not in this PR.
- The category word on a row comes from the server; the Story filters keep
  their four categories, and `asset`, `case` and `other` rows show under All.
- Encoded command lines (`decoded`) are #300.
