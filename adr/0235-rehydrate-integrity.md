# 0235 — The context-reset marker is a type, and transcript escaping round-trips

Issue: #235 · PR: #276 · Status: accepted

## Context

A rehydrated session rebuilds its conversation from the committed thread and
skips everything before the newest context reset. The reset was recognised by
its rationale starting with "context reset:" — in the agent's `rehydrate` and,
for display, in the workbench's turn grouping and chronicle. A turn's rationale
is the model's own reply, clipped, so a reply that began with that phrase cut
every earlier turn out of a rebuilt conversation. The model could also record
an interpretation of almost any type: `evaluate_hypothesis` passed its
`disposition` through as the type unchecked.

The committed transcript is line-framed; `sanitizeTranscript` escaped line
breaks as `\n` and `\r` but left backslashes alone, so a literal `\n` in the
text decoded back as a line break. The agent's `unsanitizeTranscript` and the
workbench's `unescapeT` both did. A tool result's JSON, full of `\n` escapes,
was displayed with them turned into line breaks.

## Decision

- **The marker is a type.** `ResetContext` records an interpretation of the
  new type `context-reset` (`design/01` §Interpretation types; the aggregate's
  reasoning allowlist). The agent's `rehydrate` and the workbench's
  `isContextReset` recognise a reset by that type alone; the rationale is for
  the reader. `ThreadEntry` carries `interpretation_type`, which the thread
  endpoint already served.
- **The model cannot record it.** `evaluate_hypothesis` accepts only `support`,
  `refutation` and `inconclusive` and refuses anything else with an error the
  model reads; no other agent tool takes a type from the model. The tool's
  schema is unchanged.
- **Escaping is an exact inverse.** `sanitizeTranscript` escapes `\` as `\\`,
  a line break as `\n` and a carriage return as `\r`. `agent.UnescapeTranscript`
  and the workbench's `unescapeT` decode in one left-to-right pass: those three
  escapes, and a backslash before anything else kept as it is. The eval
  harness decodes each transcript line's content with `UnescapeTranscript`
  before its graders read it.
- No compatibility path: markers recorded before this change are type `other`
  and are no longer recognised, and older transcripts decode under the new
  rule.

## Alternatives rejected

- **Keep the textual marker, matched exactly and only without a transcript:**
  the model could still record a type-`other` interpretation with that exact
  rationale through `evaluate_hypothesis`; the text would still be the marker.
- **A format version line in every transcript, with two decoders:** only
  needed to keep old records decoding as before, which is not a constraint.

## Consequences

A model reply can say anything without cutting the rebuilt conversation, and
text and tool results come back from a transcript exactly as written. A thread
whose resets were recorded before this change rehydrates as if it had none.
