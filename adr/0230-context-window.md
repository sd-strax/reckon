# 0230 — The session keeps its history within the context window by condensing it, and recovers from an overflow

Issue: #230 · PR: #268 · Status: accepted

## Context

A session kept every turn whole for its lifetime: the analyst's message with
its engine blocks, every tool call and result (up to 16 rounds a turn, each
result up to the `adr/0209` limit), and the model's prose. Only rehydration
was bounded (`maxRehydratedTurns`). When the history outgrew the model's
context window the provider refused the request as too long, a 400 that is
not retried, and every later turn failed the same way until the analyst
reset the context by hand.

Rehydration already settled what a past turn is worth keeping: the
analyst's words and the model's prose, with tool detail dropped because it
is re-fetchable and stays in the committed transcript. Every turn's
transcript and every cited event are persisted server-side, so what the
model is shown can shrink without the record losing anything.

## Decision

- The session marks where each analyst turn starts in its history. A turn
  merged into a failed turn's dangling message shares that turn's span;
  rehydrated turns arrive already condensed.
- Before each model call the session estimates the request's size: the
  system prompt, tool definitions and history in bytes, times a
  tokens-per-byte ratio. The ratio starts at 1/3 and is recalibrated from the
  input tokens every response reports. The budget is the context window
  (`agent.Config.ContextWindowTokens`, default 200,000; sidecar `initialize`
  `context_window_tokens`; workbench `reckon.agent.contextWindowTokens`;
  `RECKON_CONTEXT_WINDOW_TOKENS` for `reckon investigate`) less the output
  reserve.
- Over three quarters of the budget, the history is compacted to half of it,
  by the cheapest steps that suffice, in order:
  1. earlier turns, oldest first, are reduced to the analyst's words and the
     model's prose, the shape rehydration produces (`noReply` stands in for a
     turn that ended without prose);
  2. the current turn's older tool results are replaced by a placeholder
     naming their size and the way back (re-run the call, or `read_result`
     while a paged result is held), sparing the round the model is about to
     read; every tool call stays answered;
  3. the oldest turns are dropped, never the current one.
  Compacting to half rather than just under the mark keeps it rare, so the
  prompt cache is not invalidated every turn.
- A provider refusal as too long is a typed `*ContextOverflowError`
  (`agent/anthropic.go` parses "prompt is too long: N tokens > M maximum").
  The session adopts the provider's limit and token count, compacts to half
  the new budget (this time allowing the current round's results to be
  replaced too), and retries once. A second refusal fails the turn.
- Every compaction is announced. The model's last message carries a
  `[context]` note saying what it no longer sees and to re-run a query for
  detail. The transcript records an `[engine]` line, and the
  `OnContextCompacted` hook carries a one-line account to the surface: the
  sidecar notifies `turn/context_compacted`, the workbench shows it in the
  live turn, and the CLI prints it.

## Alternatives rejected

- **A model-written summary of earlier turns:** an extra model call per
  compaction, and a summary nothing checks standing in for the turns; the
  model's own prose already is its summary of each turn.
- **Drop the oldest turns only:** keeps the bulky tool results and discards
  the conclusions, the reverse of their value.
- **A larger window, or an automatic context reset:** the first delays the
  failure; the second throws away the thread without the analyst choosing
  to.
- **Count tokens exactly per request:** a provider round trip before every
  call; reported usage calibrates the estimate well enough, and a wrong
  estimate is caught by the overflow path.

## Consequences

A long investigation keeps working without a manual reset, and a wrongly
configured window corrects itself on the first refusal. After a compaction
the model works from its own earlier conclusions rather than their evidence,
and must re-run a query to see detail again; it is told so. The prompt cache
misses once per compaction. The summarize and propose-SOP completions build
their requests from the same history but do not compact it; they share
Turn's entry discipline under `#234`.
