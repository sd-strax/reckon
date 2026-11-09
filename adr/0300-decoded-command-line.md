# 0300 — An encoded command line is read back beside the original, in the event view, only when a known encoding decodes cleanly

Issue: #300 · PR: #310 · Status: accepted

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
event line as a suffix carrying the field as stored and then the decoded
text with its encoding named. The Story timeline's row
shows the sentence, which never carries the command line, and is untouched.
The layout table's row is amended to say so.

**Only when it decodes cleanly.** A decoder that guesses puts plausible
garbage next to evidence. The encodings are a closed list, each with an
exact test: `base64-utf16le`, PowerShell's, found where PowerShell's own
command-line grammar puts it and nowhere else. The line is read as a shell
reads it: cut into commands at the shell's chain operators, glued to words or
not, outside the shell's own quoting (cmd toggles at every double quote and
escapes with a caret, sh quotes with either character and escapes with a
backslash); each command's words split as argv splits them (a quote toggles,
a backslash before one makes it a literal character, an apostrophe quotes
only under POSIX rules, a quote with no match ahead is text so a truncated
line still reads word by word), so a quoted path is one word and a quoted
sub-command one word holding a command line of its own, read under cmd's own
rule for a string that opens with a quote. The first word of each command is
its program; an unquoted program path with spaces, as a Run key or a task
records it, is read as CreateProcess tries it, word by word until a word
names PowerShell or another program file. PowerShell is in command position when it is that program, or
when the program is a shell or wrapper the view knows (cmd's `/c`, `/k` or
`/r` string, `start` after its switches and the first quoted word that is
its title, `call`, an sh-family `-c` after options, whose command string is
the one word that follows, quoted or not, since the words after it are the
shell's positional parameters, and sudo, env, nohup, timeout and nice after
their options, the assignments sudo and env accept and timeout's duration,
each word read by its text since the wrapper sees argv without quotes) and
PowerShell is in command position on the command line it launches. Every PowerShell so found is tried, in order. From there its host options are read as
PowerShell's parser reads them, from its switch table transcribed in the
parser's order with the first match winning: each switch a case-insensitive
prefix no shorter than its accepted abbreviation, the explicit aliases
(`-ep`, `-if`, `-of`, `-ea`, `-wd`, `-ec`) as entries of their own, an
optional second dash stripped, each value-taking switch consuming the next
word, and the options ending at `-File`, `-Command`, a lone dash, a word that
is no switch or a switch the parser does not know, after which the line is
the script's own arguments. So another tool's `-e`, a `-e` after `-File`,
and PowerShell named as a word on someone else's command line all mean
nothing. The flagged token must then be an even byte count, well-formed code
units, and
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

**The export carries the original too, and escapes the whole line.** The
sentence names the program and never the command line, and the export
renders no field list, so beside the decoded text the cited-event line
carries the field as stored, named: it is the fact, and a reader verifies
the decode from it. Both are clipped, the stored value at a bound several
times the decoded text's since base64 of UTF-16LE runs near three times the
length of its text; the inspector shows the whole, and a blob cited by five
predictions does not become five pages. The whole label sits in a code span
and all of it is telemetry text, the sentence's names as much as the command
line; a backtick would close the span and let the rest render as markup and
a line break would break the row, so the export makes the whole label
span-safe where it adds the backticks. The view renders with or without
resolved entities, so a failed entity read costs the export its chips, never
the command line or its decode.

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
- A command line that does not decode cleanly looks exactly as before. So
  does one where an encoded PowerShell line is the value of another
  program's option, as in a task scheduler's `/tr` or a registry tool's
  `/d`: that process did not invoke PowerShell, so its line decodes nothing.
  The scheduled job's own command-line field carries the PowerShell
  invocation as the program and decodes there; the process the task later
  starts carries it as its own line and decodes there.
- The view's JSON grows by one optional object; no storage changes.
- A further encoding, if one proves common, is one exact test added to the
  closed list and to the tests of innocent command lines that must not
  decode.
