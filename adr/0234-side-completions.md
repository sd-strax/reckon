# 0234 — Summary and SOP-draft completions take a turn's path into the model

Issue: #234 · PR: #274 · Status: accepted

## Context

Besides turns, a session makes two single model calls over its conversation:
`SummarizeConcluded` (the client tier of the knowledge summary, `06 §3.2`) and
`ProposeSOP` (the candidate SOP, `adr/0156`). Each built its own request:
the history plus the instruction as a new analyst message, with no tools. They
bypassed what a turn's call goes through:

- After a failed turn the history ends on the analyst's message; a turn merges
  into it, but these calls added a second analyst message in a row, which the
  provider rejects.
- A live session's history holds tool calls; these requests defined no tools.
- They were not fitted to the context window (`adr/0230`).
- The sidecar ran them under a blocking lock rather than the turn's
  refuse-when-busy one, and never registered their cancel, so a call queued
  silently behind a turn and `cancel` did not reach it.
- The summary's not-yet-written retry matched "404" in the error text.

## Decision

- **One request builder.** A turn's call and a side completion both build
  their request in `Session.request`. A side completion's instruction is added
  to a copy of the history as the analyst's last message, merged into a
  trailing analyst message exactly as a turn merges after a failed one; the
  history itself is unchanged, as before.
- **Tools defined, none callable.** A side completion sends the session's tool
  definitions with `tool_choice: none` (`CompleteRequest.ToolChoiceNone`). The
  request is well-formed whatever the history holds, the model cannot start a
  tool call, and the cached tools and system prefix stay valid; only the
  messages are reprocessed.
- **Fitted like a turn.** Side completions go through `completeFitted`: the
  history is compacted when it is past the mark, and an overflow refusal adopts
  the reported window, compacts hard and retries once. The model's `[context]`
  note rides the instruction; the analyst is not notified, there being no live
  turn to show it in.
- **One sidecar entry.** Turn, reset, summarize and propose-SOP all enter
  through `sessionState.begin`: it refuses at once when the session is busy
  and registers the cancel, so `cancel` reaches whatever is running and a
  cancelled side completion answers with the coded cancellation.
- **Not-found by status.** The summary retry matches `*APIError` with status
  404, as the tools do.

## Alternatives rejected

- **Strip tool blocks from the history for a side completion:** a full cache
  miss on every call, and the summary loses the evidence it describes.
- **Keep the blocking wait for the summary:** a silent hang behind a long turn.
  Refused at once, the summary falls back to the server's deterministic
  baseline, which stands either way.

## Consequences

A summary or SOP draft after a failed turn, over a long session, or over a live
session's tool calls is a valid request. A summary asked for while a turn is
running is refused rather than queued, so the narrative is not written for that
conclusion; the deterministic baseline stands. A compaction made for a side
completion stays in the history, as it would for the next turn.
