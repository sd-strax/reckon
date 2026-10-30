# 0209 — A tool result over the limit is shown a page at a time from a session-held snapshot, with a digest of the whole

Issue: #209 · PR: TBD · Status: accepted

## Context

The agent loop fed every tool result back to the model capped at 64 KB, by
cutting the raw bytes at the limit and appending `… [truncated]`
(`agent/tools.go`). The cut could land mid-JSON, and nothing told the model
how much it had not seen, so it reasoned over an arbitrary prefix as if it
were the whole answer. With a COMPLETE coverage label still attached, the
missing rest could read as "nothing else happened". Fixture envelopes never
reach the cap; a real endpoint or SIEM adapter will on the first busy host.

The bulk is in capability results: `events` (full OCSF payloads) and
`normalized` (one entry per event), beside small fields (`coverage`, the
ref lists, `degradation_notes`). The backend persists every event and
normalized object before it returns their refs (`03 §4.13`), so any ref the
model cites stays openable whether or not the model was shown the item. The
limit bounds what the model reads, not what is recorded.

## Decision

- **The limit is configurable**, default 128 KB: `agent.Config.MaxToolResultBytes`,
  the sidecar's `initialize` parameter `max_tool_result_kb`, the workbench
  setting `reckon.agent.maxToolResultKB`, and `RECKON_MAX_TOOL_RESULT_KB` for
  `reckon investigate`.
- **A result within the limit passes through unchanged.**
- **A larger result is shown a page at a time** (`agent/bounded.go`). Its
  top-level arrays are lists; everything else rides every page. A page holds
  the same index range of every list, the largest that fits, so `events[i]`
  and `normalized[i]` stay together. Items are carried byte for byte and a
  page is always valid JSON. A top-level array is paged as the list `items`.
- **Every page says what it leaves out**, under `result_bounded`: the
  `result_id`, `offset`, `next_offset`, each list's `returned` and `total`, the
  limit and the full size, and a note that items not shown are not absent.
- **The first page carries a digest of the whole result**: for capability
  events, the count, the first and last time, and counts by class, host and
  user (the 20 most frequent values of each, plus the number of distinct
  values). It is computed, not written by a model, so it is exact.
- **The model reads further pages with `read_result(result_id, offset)`**, an
  intrinsic tool served from the session's memory with no backend call. Every
  page comes from the same snapshot, which a re-run query against live
  telemetry could not promise. Each read is an ordinary tool call, so the
  transcript records exactly what the model saw.
- **The session holds its last 8 large results** (64 MB at most), oldest
  dropped first, and drops them all on a context reset. An unknown or dropped
  `result_id` is answered with an error saying to run the query again.
- **A result that cannot be paged** (no list, not JSON, or its other fields
  alone over the limit) is answered with `result_bounded` alone: its size, the
  limit, and a note to narrow the query. An item alone larger than a page is
  named as skipped and paging steps past it.
- The sidecar's tool-result notification reports the whole result's event
  count for a paged envelope, not the page's.

## Alternatives rejected

- **Raise the limit only:** it moves the cliff, and every result stays in
  the conversation for the rest of the session.
- **Cut at the last JSON boundary within the limit:** well-formed, but still
  silent about how much is missing.
- **Have a model summarize an oversized result:** the summary would stand in
  for evidence with nothing checking it, a second model would read raw
  attacker-influenced telemetry, it would add a model call to every large
  result, and the summarizer would face the same size problem.
- **Paging in the capability layer (an offset on every verb):** every adapter
  would have to honour it, and each page would be a fresh query against data
  that may have changed since the first.
- **Minify:** the backend already writes compact JSON.

## Consequences

The model is never shown a malformed result and always knows the size of what
it has not read, and it can read on or narrow the query. A model that reads
every page still fills its context; bounding the conversation as a whole is
`#230`. A held result lives only as long as the session process: after a
sidecar restart its `result_id` is gone and the query is run again.
