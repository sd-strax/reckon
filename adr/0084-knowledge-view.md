# 0084 — Institutional knowledge gets its own view, kept separate from investigation work, with a curation section inside each investigation and markdown as the import format

Issue: #84 · PR: #85 · Status: proposed

## Context

The knowledge service (`design/06-knowledge-service.md`) holds two corpora — procedures and
concluded-case summaries — with semantic recall, revisions under a stable lineage id, and
markdown import. The analyst-facing surface for it was thin:

- **Adding** was a single command (Import SOPs) with no view to land in afterwards.
- **Browsing** did not exist; the procedure library was phased for later (`13 §4`) and
  `06 §9` only sketched it.
- **Querying** happened only by asking the agent mid-investigation, so every answer was
  filtered through the agent's reasoning. An analyst checking the agent's grounding could
  not search the corpus themselves.
- **Past cases** were reachable only as retrieved suggestions.
- The investigation-layout redesign (`adr/0079`, `ui/08`) gave the retrieved-knowledge rail
  no home. Under the default `opt_in` injection posture (`06 §5.1`), nothing reaches the
  agent until the analyst includes it, so without the rail the agent effectively never sees
  the organization's procedures.

Procedures may eventually arrive in many formats (documents, PDFs, web pages); today the
parser accepts markdown with YAML frontmatter (`internal/sopdoc`).

## Decision

The full specification is `design/ui/09-knowledge.md`; as facts about the surfaces:

- **A Knowledge view**, opened from the left region beside the work queue, outside any
  investigation, with sections for Procedures, Past cases, Import and Candidates listed
  once in the left region, and one query bar.
- **Knowledge and investigation work stay separate.** The Knowledge view shows no
  hypotheses, actions, verdicts, investigation status or investigation controls, and acts
  on no investigation; an investigation shows knowledge only as what the agent may read and
  what it cited. An investigation a piece of knowledge came from is named as a link only.
- **The query bar is natural language with two modes.** Search runs semantic recall over
  both corpora and lists scored results with band and rationale, unsummarized. Ask has the
  agent answer from the knowledge base only, citing what it used, with no telemetry tools
  and no effect on any investigation.
- **Procedures can be browsed, read, compared across versions, edited (a new revision) and
  retired** from the view. "Cited in N investigations" and "where it was used" are shown
  when the engine provides them.
- **Past cases are listed and opened** from the view; the detail is the similar-case
  inspector at full width.
- **Markdown with YAML frontmatter is the import format.** Other formats are converted to
  markdown before import, at the edge, when support for them is added. The Import section is
  the existing Import SOPs panel, showing parsed frontmatter before import and a
  per-document outcome (added, revised, failed) after.
- **Each investigation has a Knowledge section** at the top of the companion column,
  replacing the rail: retrieved items with band, rationale and an include toggle whose
  initial state follows the injection posture, a scoped search, and nudges beside Propose
  tests, Decide and the response plan when strong matches are not included. Inclusion is
  only ever the analyst's act.
- **Candidate procedures go to a queue in the Knowledge view.** The draft the conclude hook
  produces is stored and reviewed there (edit, add to library, discard with a reason), not
  prompted for at conclude. A concluded case joins Past cases without any prompt.

## Alternatives rejected

- **Query only through the agent.** Answers come filtered through its reasoning; the
  analyst cannot check grounding or see what the corpus does not contain.
- **Search without Ask.** Workable, but a question in plain language answered with
  citations is the faster path for an analyst who does not know the corpus's vocabulary;
  Search remains the default so the unsummarized view is one keystroke away.
- **Accept every format at import now.** Conversion quality, retained originals and source
  locations (`00 §6.15`) are each their own work; markdown keeps the import path exact and
  testable while those land.
- **Keep the rail as a separate panel beside the lenses.** It would compete with the
  companion column for width; the decision about what the agent reads belongs next to the
  conversation that uses it.
- **Prompt for the candidate procedure at conclude.** It puts knowledge management in the
  middle of closing an investigation, asks the person closing it to curate the library on
  the spot, and loses the draft if they dismiss it. A queue lets whoever owns the library
  review drafts when they choose.
- **Nudges that include strong matches automatically.** That is the `auto` posture, a
  tenant decision (`06 §5.1`); the nudge informs and never changes the included set.

## Consequences

- `design/13-workbench.md §4`: the SOP library row becomes the Knowledge view; the knowledge
  rail row moves into the companion column.
- `ui/08 §8.3` and `§8.5` point to `ui/09` for the knowledge entry point and the Knowledge
  section. The workbench's conclude-time candidate prompt is replaced by the queue.
- New engine gaps in `00-backend-binding.md §6`: listing concluded-case summaries (21),
  citation counts and where-used across investigations (22), an Ask session outside an
  investigation (23), and stored procedure candidates (24).
- The gated review queue around procedures is not specified here and remains separate work.
