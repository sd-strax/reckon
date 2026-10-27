# 0090 — An OCSF event renders as a server-side per-class sentence and field list with entity slots; a prediction's short title is recorded with it, authored or derived once

Issue: #90 · PR: #91 · Status: accepted

## Context

A cited event opened or listed in the investigation showed only its class name
("Authentication @ 14:32:11Z") in the Story timeline, the inspector and the markdown export,
and the raw payload below it; the analyst read JSON to learn who logged on where. A
prediction had no short name beyond `P<n>`: the tracker clipped the statement at render time,
so the same prediction read differently on the row, in the matrix and in the inspector's
title. `design/ui/00-backend-binding.md §6` listed both as gap 12 and allowed the display
knowledge to live client-side keyed by `class_uid`.

Constraints: the record is the engine's and engine vocabulary renders verbatim (`00 §1`,
`§2`); identity is minted by the normalizer through the resolver, never by a client
(`03 §7`); the export, the timeline, the inspector and (later) the baseline fold must agree
on what an event says; the base prompt is governed (`adr/0057`) and a tool-schema addition
must not be a prompt change.

## Decision

- **Display knowledge for OCSF classes is server-side, in one pure package**,
  `capability/ocsfview`: per `class_uid` a one-sentence rendering with typed entity slots, a
  curated field list (values verbatim, a tone word for a result), a category word (`auth` ·
  `process` · `network` · `detection` · `case` · `asset` · `other`, by OCSF category), and the
  decoded form of an encoded field when a known encoding decodes cleanly (a PowerShell
  `-EncodedCommand` as base64 UTF-16LE), always beside the original. Every class the
  normalizers handle plus 2005 and 5001 has a renderer; an unknown class gets its top-level
  scalars as fields and no sentence.
- **Entity slots carry the ids the normalizer minted.** The package takes a `RefLookup`; the
  server re-normalizes the persisted event in memory through the tenant registry (the resolver
  is deterministic, so the ids equal those persisted at ingest) and indexes the result by each
  type's salient property. Without a resolver the slots carry text only, never a guessed id.
- **The view rides `GET /api/evidence/{ref}`** on an OCSF record as `view`, and the host puts
  the sentence and the category on each event-time strip item, so the Story timeline rows, the
  evidence inspector's Details tab and the chip label all show the same sentence; the markdown
  export labels a cited event by it. The generic field renderer stays below as "All event
  fields".
- **A prediction's short title is a field of record.** `x-prediction` gains `title` and
  `title_derived`: authored by whoever records the prediction (the agent's `record_prediction`
  tool gains an optional `title`; the "+ prediction" card gains a title field), else derived
  by the aggregate at record time from the statement (the earliest clause boundary whose head
  can stand alone, clipped to 64 characters at a word, no trailing period) and marked derived.
  The projection stores both; a row projected before this record derives its title by the same
  function at read time, so no surface ever clips a statement itself. Titles are frozen with the
  statement.

## Alternatives rejected

- **Client-side rendering keyed by `class_uid`** (what the binding allowed): the export and a
  future baseline fold would re-implement it, and the fixtures already exercise the Go side.
- **Persisting the event → SCO join at ingest and reading it back** instead of re-normalizing:
  the join is deterministic from the payload already, and the persisted ObservedData carries
  no event id; adding one is a schema change for a value the resolver recomputes in microseconds.
- **Deriving the title in the webview** (the prior state): three surfaces clipped differently,
  and the agent's reasoning-state block could not name the row the way the analyst sees it.
- **Rejecting an over-long authored title**: a rejected tool call costs the agent a turn for a
  display field; the aggregate normalizes instead (clip, strip the period), deterministically.

## Consequences

- `GET /api/sops`-style wholeness for events: a client needs no class knowledge to show an
  event; a new normalizer lands with its renderer in the same package.
- Migration `0024` adds `title` / `title_derived` to `prediction_current`; a replay backfills
  them from the statements.
- The eval baselines are expected to stand: the tool gains an optional parameter and the
  prompt text is unchanged (`TestPrompt_Frozen`, `TestPrompt_NoScenarioVocabulary` pass); the
  live run is the check when a key is available.
- Gap 13 (the entity-scoped event query) now has the row shape it needs: an `EventRow` is a
  persisted event plus this view.
