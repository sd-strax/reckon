# 0348 — The Story timeline shows an event once, however many queries returned it

Issue: #348 · PR: #TBD · Status: accepted

## Context

Every capability call's answer is stored as its own telemetry records, and each event in
each answer gets a fresh id at ingest. That is correct for the record, because each record
says which call retrieved it and when. The Story timeline (`adr/0092`) drew one row per
record. When the agent's queries overlapped, one event appeared once per query that
returned it, and the rows differed only in their "via <query>" label. On the
lateral-movement scenario, one RDP logon read as three logons in the same second, and one
PowerShell launch read as four. An analyst reads that as repeated activity.

The interpretation layer already knows these records are one event. Their observed-data
ids are deterministic over the content (`03 §7`), so the records of the same event carry
the same `observed_data_refs`.

## Decision

- **Two records are the same event** when their class, their event time and their
  observed-data refs (as a set, non-empty) are equal, and they came from different calls.
  A record with no observed-data refs is never merged, because nothing vouches that it is
  the same event.
- **Within one call, records stay separate.** If a source returns two identical records in
  one answer, that is what the source said happened, and the timeline keeps both. Across
  calls, the merged rows number the most copies any single call returned.
- **A merged row is one row.** It shows the event once and names every query that returned
  it: "via get_process_executions, get_process_ancestry", or "via enumerate_logons ×3"
  when the same verb ran more than once. Each name opens its call's inspector, as the
  single "via" did.
- **Citations and pins** count against every record the row stands for. A prediction that
  cites any one of them marks the row. Groups (`adr/0095`) are built from the merged rows,
  so a repeat group counts events, not retrievals.
- **The record does not change.** The telemetry records, their ids and their call
  membership stay as they are, and the thread and the export still show each retrieval.
  This decision covers what the timeline presents.

## Alternatives rejected

- **Deduplicate at ingest, giving an event one telemetry id across calls.** That rewrites
  the immutable telemetry layer's identity and loses which call retrieved what.
- **Match on the rendered sentence.** Two different events can read the same. The
  observed-data id is what the record already uses for identity.
- **Collapse identical records within one call too.** That would hide an event the source
  reported twice.

## Consequences

- The timeline's row count is the number of events, which is the number an analyst
  reasons about.
- `ui/08 §8.4` (Story) states that a row is an event and lists the queries that returned
  it.
