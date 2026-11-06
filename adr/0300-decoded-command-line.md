# 0300 — An encoded command line is read back beside the original, in the event view, only when a known encoding decodes cleanly

Issue: #300 · PR: pending · Status: accepted

## Context

A process event's command line is often not readable as stored. PowerShell's
`-EncodedCommand` takes base64 of UTF-16LE text, so the record shows
`powershell -enc SQBFAFgAIAAo…`; a shell one-liner may pipe a base64 blob
through a decoder. The inspector and the export showed the string as the
telemetry carried it, and the analyst pasted it into a decoder to learn what
ran. `implementation/workbench-react.md §8.1` reserved a `decoded` slot on
the event view for this when the view was built (`adr/0090`), and #300 was
split out of #90 to fill it.

Two documents placed the work differently: the implementation note on the
server, in the event view; the layout table (`design/ui/08 §8.6.1`)
client-side. The event view is produced server-side so that the timeline,
the inspector, the export and the baseline fold agree; that reasoning
applies to this field as to the sentence.

## Decision

**A derived rendering of one telemetry field, like the sentence.** The
telemetry layer stays immutable. The decoded text is a view: not evidence,
not an interpretation, nothing recorded, nothing cited. The original field
is the fact and stays primary: the sentence, the field list and the group-by
key (`adr/0095`) all stay on the stored value, and the decoded text sits
beside it, labelled.

**Server-side, in the view package.** `ocsfview.View` gains `decoded`
(`field`, `encoding`, `text`), set by the process and scheduled-job
templates from `process.cmd_line` and `job.command_line`. One decode per
view. Every surface that renders the view carries it: the evidence inspector
in a "Decoded command line" section under the summary, the export's cited
event line as a clipped suffix naming the encoding. The Story timeline's row
shows the sentence, which never carries the command line, and is untouched.
The layout table's row is amended to say so.

**Only when it decodes cleanly.** A decoder that guesses puts plausible
garbage next to evidence. The encodings are a closed list, each with an
exact test: `base64-utf16le`, PowerShell's, anchored on its flag in any
spelling PowerShell accepts (`-e`, `-ec`, `-enc`, `-EncodedCommand`, with a
dash or a slash) on a line that has named PowerShell before it, so another
tool's `-e` means nothing; an even byte count, well-formed code units, and
text that is at least three quarters ASCII, since a PowerShell command is
and a short run of random bytes read as UTF-16 is not (it lands in the
graphic ideographic planes and would otherwise pass as text); and `base64`,
the shell pattern, for a bare token of at least sixteen base64 characters
with padding that fits. In both the text must be wholly printable, with no
replacement character, and non-empty. Quote characters around a token are
stripped whether or not they pair, since the blob is often the last word of
a quoted sub-command. A flagged token that is not UTF-16LE is not a
PowerShell command and gets no second guess as bare base64. Anything else
yields nothing, and the view says nothing.

**Not deobfuscation.** The view decodes one encoding of one field and
stops. What the decoded command does, and whether it hides a further layer,
is the analyst's reading. The agent does not see the view: it reads
capability results, not the inspector's rendering, so nothing model-facing
changes (`adr/0232`); giving the agent the decoded text is a later step
through a capability result, not through this view.

**The export escapes it.** The decoded text is the attacker's and the
export's cited-event line sits in a code span; a backtick in the text would
close the span and let the rest render as markup, so the export replaces
backticks with straight quotes and clips the line. The view renders with or
without resolved entities, so a failed entity read costs the export its
chips, never the decoded text.

## Alternatives rejected

- **Client-side decode** (the layout table). The export has no client, and
  four surfaces have to agree; the view package already exists for that.
- **Decoding every base64-looking token.** Short alphanumeric words are valid
  base64 by accident; a decoded word beside evidence that was never encoded
  is a false statement. The length floor and the printable test are the
  cost of saying nothing wrong.
- **Recording the decoded text on the event.** The telemetry layer is
  immutable, and the decode is a function of the stored bytes; recording it
  would make a view look like a fact.
- **Decoding in the fixtures.** The scenario fixtures are untouched: the
  prompt's scenario-vocabulary test derives from them, and the view's tests,
  the wire fixture and the harness carry the encoded command lines instead.

## Consequences

- An encoded command line reads decoded in the inspector and the export,
  with the encoding and the field named and the original beside it.
- A command line that does not decode cleanly looks exactly as before.
- The view's JSON grows by one optional object; no storage changes.
- A further encoding, if one proves common, is one exact test added to the
  closed list and to the tests of innocent command lines that must not
  decode.
