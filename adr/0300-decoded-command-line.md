# 0300 — An encoded command line is read back beside the original, in the event view, only when a known encoding decodes cleanly

Issue: #300 · PR: #310, #317 · Status: accepted

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

**Only when it decodes cleanly, and only PowerShell's own line.** A decoder
that guesses puts plausible garbage next to evidence. The encodings are a
closed list, today of one entry, with an exact test for each: `base64-utf16le`,
PowerShell's, found where PowerShell's own command-line grammar puts it and
nowhere else. The line's words are split as argv splits them (a quote
toggles, a backslash before one makes it a literal character, an apostrophe
quotes only under sh's rules, which apply when the program lives under a
POSIX path, and a quote with no match ahead runs to the end of the line, so
the rest of a truncated line is one word and reads as nothing), so a quoted
path is one word. The first word is the
program; an unquoted program path with spaces, as a Run key or a task
records it, is read as CreateProcess tries it, prefix by prefix: bare
words are parts of the path, a quoted word, a switch-shaped word or a word
that begins a path of its own is the first argument and the program is
whatever came before it, and PowerShell is found when a word continuing the
path names it, unless another program file ends the path first; a POSIX
argv[0] is exact and gets no such recovery. The view reads the line only when
that program is a PowerShell console host, `powershell` or `pwsh`, under any
path, quoted or not; the ISE is not one, its command line has no encoded
command. From there its host options are read as PowerShell's parser reads
them, from its switch table transcribed in the parser's order with the first
match winning: each
switch a case-insensitive prefix no shorter than its accepted abbreviation,
the explicit aliases (`-ep`, `-if`, `-of`, `-ea`, `-wd`, `-ec`) as entries
of their own, an optional second dash stripped, each value-taking switch
consuming the next word, and the options ending at `-File`, `-Command`, a
lone dash, a word that is no switch or a switch the parser does not know,
and on pwsh at `-Version`, which prints and stops reading (Windows
PowerShell's takes the engine version as a value), after which the line is
the script's own arguments. So another tool's `-e`, a `-e` after `-File`,
and PowerShell named as a word on someone else's command line all mean
nothing. The flagged token is read exactly as PowerShell receives it,
quotes an argv split left on it included, through the conversion PowerShell
itself applies: base64 in the standard alphabet with padding that fits, an
even byte count, well-formed code units, and text that is at least three
quarters ASCII, since a PowerShell command is and a short run of random
bytes read as UTF-16 is not (it lands in the graphic ideographic planes and
would otherwise pass as text); the text must be wholly printable, with no
replacement character, and non-empty. A flagged token that is not UTF-16LE
is not a PowerShell command and gets no second guess. Anything else yields
nothing, and the view says nothing.

**The claim is the designation, not the run.** The view decodes the
argument PowerShell's parser designates as the encoded command, as
PowerShell would decode it. Whether PowerShell then ran it is not the view's
claim: a `-Help` on the same line, an option value PowerShell would reject,
an execution policy, a missing module all decide that, and so does what the
decoded command itself does. Those are PowerShell's runtime, and the
analyst's reading of the event; the view's one statement is "this argument
decodes to this text", beside the field that carries it.

**A program that is not PowerShell is not read.** A line whose program is a
shell (`cmd /c powershell -enc …`, `cmd /c start powershell …`, `sh -c 'pwsh
-e …'`), a wrapper (`sudo`, `nohup`) or any other tool shows as stored, even
when PowerShell
and its encoded command are plainly on it. Reading such a line means reading
the launcher's own grammar, which is where the view guessed: each shell and
wrapper has its own quoting, its own operators and its own rule for which
word is the command, and every one the view learned brought a form it
misread. The view's reading has a closed surface, the argv split of one
command and PowerShell's switch table, so that a false decode has nowhere to
come from; what it declines to read is read on demand, by a decoder the
analyst or the agent points at the string, where the result is labelled as
a decode of what was given and never stands beside the event as a fact
about it.

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
  is a false statement. A bare-token pass with a length floor and the
  printable test was built and then withdrawn: it had no anchor in any
  grammar, so a credential in a header, a quoted multi-word argument or a
  word in a shell comment decoded as readily as a shell's `base64 -d` blob,
  and each fix for one such reading contradicted another.
- **Reading the line as a shell reads it.** The view did for a while cut
  the line into commands at cmd's and sh's operators under their quoting,
  and followed cmd's `/c`, `start`, an sh-family `-c` and the common
  wrappers to the PowerShell they launch, so that `cmd /c powershell -enc …`
  decoded. Every construct of the shell the reading did not yet know (a
  comment, a line continuation, a here-document, a word a wrapper takes as
  its own) made the view decode a PowerShell the shell never ran, or move a
  command's words onto another's, and teaching it each one widened the
  reading without closing it. The surface is
  the whole of two shell languages and every wrapper's option table, and it
  grows with every launcher an attacker reaches for; a closed surface that
  misses is preferred to an open one that fabricates. The lines it covered
  are better served by a decode on demand than by a reading that must be
  right unasked.
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
  `/d`, or the command a shell or wrapper launches, as in `cmd /c
  powershell -enc …`: that process did not invoke PowerShell as its program,
  so its line decodes nothing. The scheduled job's own command-line field
  carries the PowerShell invocation as the program and decodes there; the
  process the shell or the task later starts carries it as its own line and
  decodes there, which in a process tree is the usual case.
- The view's JSON grows by one optional object; no storage changes.
- A further encoding, if one proves common, is one exact test added to the
  closed list and to the tests of innocent command lines that must not
  decode, anchored as this one is on the grammar of the program that reads
  it. A further launcher is not added: a string the view does not read is
  decoded on demand, outside this view.
