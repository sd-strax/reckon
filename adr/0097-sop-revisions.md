# 0097 — A cited section is readable as it was, and the inspector says whether it changed since

Issue: #97 · PR: #312 · Status: accepted

## Context

Since [0303](0303-section-recall.md) every reasoning act records, per
procedure it consulted, the revision, the cut (`<splitter>/<budget>`) and the
section indexes it had in front of it. Those three numbers name text: the
substrate keeps every revision as its own row and the cut says how the body
was divided. But nothing could read that text back. `Sections` and `Section`
(`knowledge/sections.go`) resolve the current revision only and answer
`ErrRevisionChanged` or `ErrCutChanged` (409) for any other, which is right
for the agent's `read_sop`, pinned so an index is never answered with a
different section under the same number, and wrong for an analyst asking
what the agent saw. The facade listed no revisions. `Reindex` re-cuts a row
stored under another splitter version or budget and `writeSections` deletes
the row's prior sections first, so the cut an act recorded may no longer be
stored for its row. And `SplitSections` was the version-1 rule with no
version dispatch: recomputing an old cut needs the rule the version names.

The procedure inspector's "What the agent read" showed only the excerpt the
recall tool result carried in the transcript (`derive/knowledgeUse.ts`). It
could not show the sections the agent then read with `read_sop`, could not
say whether the procedure changed since, and had no diff. design/ui/08 §8.6
asks the pane to be anchored on what the agent retrieved; the issue asks for
"exactly as retrieved" and "revised since this case consulted it".

Two decisions already made bound the answer. The excerpt is recomputable
from the revision row and the cut id, both under the content hash
([0303](0303-section-recall.md); the retrieved-bytes side store and a hash at
retrieval were set aside). And [0236](0236-knowledge-citation.md) fixes the
words: a procedure is cited or recalled, never applied or followed.

## Decision

- **The splitter is versioned at the call.** `SplitSectionsAt(version, body,
  budget)` (`knowledge/substrate/sections.go`) dispatches on the version;
  version 1 is today's rule, unchanged and frozen as `splitSectionsV1`. An
  unknown version is `ErrUnknownSplitter`. `SplitSections` is
  `SplitSectionsAt(SplitterVersion, …)`. Every section carries the version
  and budget it was cut under. The rule [0303](0303-section-recall.md)
  stated for `splitter_version` is now a dependency of this record: a change
  to the splitter's output for any input is a new version added beside the
  old, and every past version stays in code, so a recorded cut is
  reproducible from the revision's body for as long as the body is.
  `ParseCut` reads a cut back into its version and budget (`ErrBadCut` when
  malformed).
- **The facade lists revisions and reads any of them.** `Revisions(tenant,
  id)` lists every row of a lineage, newest first, retired lineages included,
  each with its revision, content hash, created and superseded times, author,
  importer and source, and `current` on the one not superseded.
  `RevisionSections(tenant, id, revision, cut)` and `RevisionSection(tenant,
  id, revision, idx, cut)` resolve the row at `revision` (the current one
  included, so one shape serves "as retrieved" and "current") and serve its
  stored sections when they carry the requested cut, or when none is
  requested; otherwise they recompute the cut from the row's body under the
  splitter version and budget the cut names. A row with no stored sections
  and no requested cut is cut under the current version and default budget.
  The read never refuses a cut that was once valid: an unknown lineage,
  revision or index is `ErrNotFound`; a malformed cut, or one naming a
  splitter this build lacks, is `ErrBadCut`. `Sections`, `Section` and the
  agent's `read_sop` are untouched: the agent stays pinned to the current
  revision.
- **Three routes beside the section routes**, read roles:
  `GET /api/sops/{id}/revisions` answers `{sop_id, current, revisions}`;
  `GET /api/sops/{id}/revisions/{r}/sections?cut=` the outline;
  `GET /api/sops/{id}/revisions/{r}/sections/{n}?cut=` one section whole with
  the outline, the same `SOPOutline` and `SOPSectionRead` shapes the current
  reads serve. 404 for an unknown procedure, revision or section; 400 for a
  malformed path value or a cut this build cannot make; never 409. Wire
  fixtures `sopRevisions`, `sopOutline`, `sopSection`.
- **The inspector reads the record back.** The workbench thread entry now
  carries the revision, cut and sections of each consulted procedure (the
  server sent them since 0303; the decoder named only the id and `cited`).
  On opening a procedure the host reads the committed thread, and for every
  act that recorded sections reads each distinct `(revision, cut, idx)`
  through the revision route, once however many acts recorded it
  (`src/procedureReads.ts`). When an act's revision is not the current one
  it reads the current revision's outline once and, for each section, the
  current sections under the same heading path, never by index, since an
  edit or a re-cut renumbers: a heading cut into parts is one heading (the
  same part when the parts match, otherwise every current part together,
  so the pane never says a heading is gone because its parts moved), and a
  heading the document repeats is told apart by its order among its
  namesakes. An entry carrying sections without its cut is treated as not
  recorded: the index cannot be read back honestly. The reads ride on
  `inspector.procedure` as `reads`, optional like `citations`: absent when
  no act recorded sections (acts before 0303) and when the read-back
  failed; the pane distinguishes the two from the thread it already holds.
  The reads start from the committed thread, never from anything the webview
  holds.
- **"What the agent read" says what it is.** One block per act: "revision N,
  exactly as retrieved" when N is the current revision, or "revised since
  this case consulted it: revision N then, M now". Each section shows its
  heading path and its text; under a revised procedure it offers As
  retrieved · Current · Changes, and when no section of the current revision
  sits under that heading any more, Current and Changes say so. The recall
  results' excerpts follow as "Recall showed": recall and read are two
  different things. The words are the issue's and [0236](0236-knowledge-citation.md)'s;
  never "applied" or "followed".
- **The diff is computed in the webview**, a line-level longest common
  subsequence (`webview/src/derive/diff.ts`): equal runs kept, removed and
  added lines marked. Both texts are one section each, bounded by the
  section budget, so the quadratic table is small. No dependency.
- **The case inspector is unchanged.** Case summaries are revisions too, but
  whether a past case's narrative changed since is #103's question.

## Alternatives rejected

- **A retrieved-bytes side store, or a hash at retrieval.** Redundant: the
  revision row is the bytes, under the content hash, and the cut is
  recomputable; a second record of the same text would be the second copy
  the substrate's §3 forbids. Set aside when #97 was re-cut after 0303.
- **Refusing a historical read whose cut the store no longer holds.** The
  data exists and an investigation record is entitled to it; a 409 here
  would make a reindex erase what the agent saw from every inspector that
  cites it.
- **Reading the current counterpart through the current-revision route
  with its pin.** It would answer 409 the moment a reindex re-cut the
  current row between the outline and the section read; the revision route
  answers the same shape without a pin, and one code path serves both
  sides.
- **Matching the counterpart by section index.** An edit that adds a
  heading above, or a re-cut under another budget, renumbers every section
  after it; the heading path is what the analyst reads the section as.
- **Matching the counterpart by location string.** The location carries
  the part suffix, so a heading whose parts moved would read as gone, and a
  repeated heading would match its first namesake.
- **A server-side diff route.** The webview already holds both texts; the
  server stays a reader of revisions.
- **A diff library.** A third-party license shipped with the extension for
  sixty lines of code.
- **Reading the thread entries' record from the webview's snapshot** instead
  of the committed thread. The webview's copy is the same record, but the
  host's reads should start from what the server holds, as every other
  inspector read does.

## Consequences

- A number on the record now points at text anyone with a read role can
  open, and the inspector says whether the procedure moved on since the
  agent read it, section by section, without claiming anything about what
  the agent did with it.
- A splitter change is now a code addition, not an edit: `splitSectionsV1`
  is frozen, and the next rule is `splitSectionsV2` beside it with
  `SplitterVersion` moved to 2. `TestSplitSectionsAt_VersionOneIsTheCurrentRule`
  pins version 1 to the current output until that day.
- The revision reads list the lineage's rows through `List` with the lineage
  tag and sort in memory; a lineage has as many rows as edits, which is
  small. A historical read of a re-cut row recomputes the cut on each
  request rather than storing it; sections are bounded by the budget and a
  body is one document.
- Opening a procedure costs the host one thread read and one revision
  section read per distinct section the acts recorded, plus the current
  outline and counterparts when the procedure was revised; the pane opens
  on the document and the citations if those reads fail.
- Three wire shapes are new and the thread decoder names three fields the
  server already sent; the fixtures are regenerated. `consulted_sops` on the
  wire is unchanged.
- design/06 §4.2, design/ui/00-backend-binding gap 15 and design/ui/08 §8.6
  note what is built; the retained original file stays with #4.
