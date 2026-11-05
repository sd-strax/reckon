# 0303 — The section is the unit of SOP recall, and the agent reads the rest by section

Issue: #303 · PR: #306 · Status: accepted

## Context

`recall_sops` returned each hit with the first 480 runes of the body, cut
mid-document (`excerptRuneBudget`, `knowledge/substrate/recall.go`); the
browse path cut at 1000. No tool fetched the remainder: `read_result` pages
the tool result, not the document, and the prompt says SOPs are never placed
in context ([0129](0129-knowledge-through-tools.md)). For the shipped
containment procedure the model saw the title and the scope paragraph; the
triage and containment sections never reached it, and a procedure it then
named by ref was recorded as `cited` on the strength of its preamble.

Ranking already ran over the whole body (one vector per entry over title and
body), so this was not a selection failure: the right procedure came back,
and the part that matched was not in the hit. design/06 §4.2 asked for the
full body up to a token budget and, for a longer document, the most relevant
section by sub-document embedding. Neither was built. The first alone would
not have fixed it: for a long document a budget returns a longer preamble and
charges the budget for it; the ten relevant lines on page thirty still never
arrive.

Two constraints bound the answer. The substrate is host-free
(`00-substrate §12`): it cannot say "procedure", and it cannot import the
agent's result bound. And the content hash is a one-way door (`§3`): whatever
the hit carries must stay a view over hashed content, never a second record.

## Decision

- **The section is the unit of retrieval.** At write time the substrate cuts
  a body on its ATX headings (`SplitSections`, `knowledge/substrate/sections.go`):
  text before the first heading is its own section, a body with no headings is
  one section, headings inside code fences are text (a fence closes only on a
  run of its own character at least as long as the opening, as CommonMark
  says), and a section longer than the store's section budget is cut on
  paragraph boundaries into parts. Sections are rows (`substrate_sections`:
  index, heading path, rune offsets, text, the `splitter_version` and budget
  that made them). The content hash still covers the whole entry; a section
  locates text within it. A body with no content to cut is refused on write
  (`ErrBlankBody`): one unrankable entry would fail every recall that reached
  it.
- **One vector per section beside the document's.** `substrate_embeddings` is
  keyed by `(entry_id, model, section)`; section 0 is the whole document as
  before and serves SIMILARITY mode, sections 1..n embed as title, heading
  path and text and serve RANK mode. A write embeds all of them in one
  embedder call.
- **RANK ranks sections and the caller says what a hit stands for.**
  `RecallQuery.Unit` is `ENTRY` (default: aggregate to the entry by its best
  section, one hit per entry carrying it) or `SECTION` (one hit per section;
  an entry may appear twice). The knowledge layer maps the tool's words,
  `procedure` and `section`, onto them. The keyword backend ranks sections the
  same way, over the section text, its heading path and the entry title.
  SIMILARITY mode is unchanged in ranking.
- **A hit carries the section, its place and the outline.** `excerpt` is the
  matched section's text (SIMILARITY: the first section); `section` is
  `{idx, of, heading_path, part, parts, location}`; `outline` is one location
  per section; `truncated` says the budget cut the excerpt; `revision` is the
  SOP revision the indexes belong to. The fields are additive on the `§8`
  one-way-door wire. The match rationale names the section: `best section
  "A > B" (3 of 5)`. The browse path (no query) reads the stored cut too, so
  its numbering is the one a section read answers to.
- **One budget, applied at both ends.** `knowledge.excerpt_budget_tokens`
  (default 4000, design/06 §4.2) becomes runes at four to a token
  (`config.RunesPerToken`). The runtime passes the same number to the
  substrate as the section budget and to the knowledge store as the excerpt
  budget, so a section arrives whole by default. A request may ask for a
  smaller excerpt (`excerpt_budget`), never a larger one; the agent asks for
  one that keeps five hits under its result page
  (`maxResultBytes × 2 / (5 × 3)` runes), so a hit is never clipped after the
  fact.
- **The rest is read by section, against the revision recalled.**
  `GET /api/sops/{id}/sections` answers the outline (every section's place and
  length, no text) and `GET /api/sops/{id}/sections/{n}` one section whole
  with the outline around it, both on the read role, over the SOP's current
  revision. Section indexes belong to a revision, so a read passes
  `?revision=` from the hit and is refused with 409 (`ErrRevisionChanged`)
  when a re-import moved the current revision, rather than answered with a
  different section under the same number; the agent then recalls. The
  agent's `read_sop {sop_id, revision?, section?}` tool reaches them. A
  section larger than the result page is reshaped into `text_parts` the page
  machinery pages, so every section is readable whatever the budget. The
  recall tool's text and the prompt say a hit carries one section and the
  rest is read with `read_sop`.
- **The record names what was shown, and `cited` requires it.**
  `ConsultedSOP.Sections` (agent, server body, aggregate; validated 1-based and
  bounded) is the union of the section indexes a turn had in front of the
  model: each recall hit's, on every page the model read (a held result
  remembers its tool, and `read_result` credits each page), each `read_sop`
  section's; an outline read records the procedure with no section. The entry
  carries the revision the sections belong to; a newer revision replaces it
  and an older one's sections are not merged under it. The agent caps what it
  records at the aggregate's bound (256 sections per procedure, beyond what
  the default rounds and recall limit can show), so a commit never fails on
  what the tools showed. `cited` now means the model's own text named the
  procedure by its ref *and* at least one section of it was shown; the
  aggregate refuses a cited entry with no section on every write path, since
  the structural rule cannot depend on which client wrote. This tightens
  [0236](0236-knowledge-citation.md), whose ref rule stands. The
  `cite-knowledge` prompt rule is revised to say so (Revised #303,
  PendingValidation #303).
- **`Reindex` is an operational path, on the contract.** It joins the
  substrate's `Store` interface, works without an embedder (sections only),
  re-cuts sections stored under another splitter version or budget, and fills
  missing document and section vectors under the active model. A current cut
  is kept and only the active model's vectors are filled, so a backfill under
  a new model leaves the old model's section vectors in place and a reader
  still on it keeps working (the coexistence rule in `00-substrate §10`); only
  a stale or missing cut is rewritten. The backend runs it in `Backend.Start`
  after the dependency probes, before the listener, failing startup on error,
  so a changed embedding model or section rule is reconciled before the first
  recall; a current corpus costs one scan. On demand the run is a job:
  `POST /api/knowledge/reindex` (analyst) starts it and answers 202, `GET`
  reports idle, running, done or failed with the count or the error, one run
  at a time, and `reckon knowledge reindex` starts and polls; a run is one
  embedder call per stale entry and must not sit under a request's deadline.
  A candidate without sections is `ErrNoSections` on both backends (the
  keyword ranker checks the eligible entries before ranking from the
  sections table, so an un-cut entry is never answered EMPTY), a section
  without a vector `ErrNoEmbedding`, never a silent gap. There is no grace
  path for rows made before the migration: there is no deployed data, and the
  demo corpus is re-imported.

## Alternatives rejected

- **The body up to the budget, as the issue first asked.** A larger constant,
  not a mechanism: a long document's preamble, charged at the budget, with the
  matched content still absent.
- **A model-generated summary at import, returned in the hit.** It helps
  choosing among hits and does nothing for reading the procedure; it is
  derived content a signed-off document never covered; it puts an LLM on the
  import path the substrate does not have. If ever wanted, it is an
  author-written abstract first and a generated fallback labelled as derived,
  keyed to the content hash, outside the substrate.
- **Model-generated tags at import.** Tags are a hard filter; a wrong
  generated tag silently excludes a procedure.
- **Whole-document retrieval plus a read tool, no sections.** Cheap recall,
  but the agent must guess where to read, there is no "where in the source",
  and design/06 §4.2 is not met.
- **Sections cut at query time under the request's budget.** Sections are
  stored rows with vectors; a per-query cut would have no vector to rank. The
  budget is a store setting at write time, and a request can only shrink the
  excerpt.
- **Heading path in the sections' generated full-text column.**
  `array_to_string` is not immutable in Postgres; the column indexes the text,
  and the heading path and title join the surface at query time.
- **`cited` unchanged, merely recording sections beside it.** "Cited" without
  "read" is what let a preamble stand for a procedure.

## Consequences

- A long procedure is usable: a hit costs one section, not the document, and
  the model can read any other section by index. Five hits fit the result page
  under the default budget.
- The turn record carries which sections of which procedure the model was
  shown. #97 reads it to say "excerpt k of n" and where in the source; the
  retrieved-bytes side store, hash-at-retrieval and the retained original stay
  with #97. design/ui/00-backend-binding gap 15 notes what is built.
- Keyword-only deployments gain section-level recall too, with a coarser
  ranker.
- This is a change to what the model reads. The `prompt` component's digest
  moves from 67a4243d to 7ba7d77c, `tools/recall_sops` from 753f05fb to
  8c6809eb, and `tools/read_sop` is new at ba156d8e. `PromptVersion` moves
  from 362b6290 to 22ddad5b under `leading_null` and from e7568f3c to
  42e7c29e under `leading_only`.
- **The baselines are not rerun in this PR.** As in
  [0236](0236-knowledge-citation.md), the rerun is deferred to the pass that
  closes the open epics; `cite-knowledge` stays `PendingValidation` until a
  run accepts it on a scenario, and the interval in design/10 §4.4 spans this
  change too.
- Substrate migration 0002 changes the embeddings key; the substrate's spec
  (§2.3, §5.1, §5.2, §5.6, §7, §8, §10) and design/06 §4.2 and §5.2 are
  amended. The workbench decoders list only the fields they read and need no
  change; `recallSOPs`, `recallSimilar` and `thread` wire fixtures are
  regenerated. The section routes have no workbench decoder yet, so no
  fixture.
