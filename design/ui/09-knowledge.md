# 09 — Knowledge: the Knowledge view and knowledge inside an investigation

This sheet defines how analysts add, browse and query institutional knowledge — the
procedure corpus and the concluded-case corpus of `design/06-knowledge-service.md` — and how
retrieved knowledge is curated inside an investigation. The decision and its alternatives
are `adr/0084-knowledge-view.md`. Reference screens: `mockups/12`–`14`.

Binding rules apply unchanged (`00`): the knowledge service is the record, every element
names its source, and an element with no source is a gap in `00 §6`.

### Out of scope

- The gated review workflow (`06 §2.2`: submit, request changes, sign off). Under `gated`
  governance this view shows each procedure's status; the review queue around it is not
  specified here.
- Formats other than markdown for import (§9.5).
- Analytics beyond "cited in N investigations" (`06 §9.2`).

---

## 9.1 Two surfaces

| Surface | Where | Job |
|---|---|---|
| **Knowledge view** | Its own document in the main area, opened from the left region's header (`ui/08 §8.3`) alongside the work queue | Add, browse, edit, retire and query knowledge, outside any investigation |
| **Knowledge section** | Top of the companion column inside an investigation (`ui/08 §8.5`) | Decide what knowledge the agent reasons over in this investigation |

The Knowledge view has a query bar (§9.2) and tabs for **Procedures** (§9.3), **Past
cases** (§9.4) and **Import** (§9.5); a **Results** tab appears after a query and holds its
results.

## 9.2 Query bar: search and ask

One natural-language box at the top of the Knowledge view, with two modes.

**Search** (default). Runs semantic recall over both corpora and lists results with their
relevance signals; nothing is summarized.

| Element | Source |
|---|---|
| Procedure results: title, version, excerpt, score as a band, `match_rationale` | `POST /api/knowledge/recall_sops` (`06 §4`) |
| Past-case results: title, conclusion date, outcome, excerpt, band, `match_rationale` | `POST /api/knowledge/recall_similar_investigations` (`06 §4`) |
| Filters: techniques, entity types, threat categories, minimum severity | The `scope` fields of both requests |
| "No procedure matches" shown as information, not an error | `coverage = EMPTY` (`06 §4.3`) |
| Degraded-search note when keyword fallback answered | `SOPMatch.Backend` |

**Ask.** The agent answers the question from the knowledge base only, citing what it used;
each citation opens in the inspector (`ui/08 §8.6`). It has no telemetry tools and changes
nothing. Source: gap `00 §6.23`.

Both modes record nothing in any investigation.

## 9.3 Procedures

**List.** One row per procedure: title, tags and techniques, recommendation token, version,
status (`PUBLISHED` / `RETIRED`; the gated states when that mode is on), author, last
updated, and "cited in N investigations". Filters: tag, technique, recommendation, status,
author. Retired procedures are hidden by default and shown marked when included.

| Element | Source |
|---|---|
| Rows, filters | `GET /api/sops`; empty-query recall browses by tag |
| Cited in N investigations | Gap `00 §6.22` |
| Added from an investigation (candidate accepted at conclude) | Source URL `reckon://investigation/<id>` |

**Detail** (opens beside the list):

- The body rendered from markdown, with its frontmatter as a metadata block (author,
  source system and URL, version, recommendation, `applies_to`).
- **Versions**: each revision with its time and who made it; any two can be compared.
  Source: substrate revisions (`06 §2.2`, lineage id).
- **Where it was used**: investigations that consulted it, applied or only consulted. Gap
  `00 §6.22`.
- Actions: **Edit** (markdown editor with preview on a reckon panel, never the quick input,
  `13 §3`; saving makes a new revision through `/api/sops/{id}`), **Retire** (with a
  reason), **Copy link**.

## 9.4 Past cases

**List.** One row per concluded investigation summary: title, concluded date, disposition,
techniques, primary entities, actions taken with outcomes, duration. Filters: technique,
disposition, date range. Source: gap `00 §6.21` (recall needs a query; listing does not
exist).

**Detail** is the similar-case inspector (`ui/08 §8.6.1`) at full width, with **Open the
investigation** (read-only once concluded) and "Procedures learned from this case" when a
candidate from it was accepted.

## 9.5 Import

Markdown with YAML frontmatter is the import format (`06 §2.4`); other formats come later
by converting to markdown before import. The Import tab is the existing Import SOPs panel
(`13 §3`) moved into the view:

- Choose files or a folder; each document is listed with a checkbox and its parsed title.
- Frontmatter is shown parsed before import, with missing recommended fields (title,
  author, tags, source URL) flagged; import still proceeds without them.
- Per-document outcome after import: **added**, **revised** (same source URL as an existing
  procedure: a new version under the same id), or **failed** with the parser's reason.
- Source: `POST /api/sops/import_markdown`; importer and time recorded server-side.

## 9.6 Knowledge inside an investigation

The **Knowledge section** at the top of the companion column replaces the knowledge rail
(`ui/08 §8.5`). It is how the analyst decides what the agent reasons over: under the
default `opt_in` posture nothing reaches the agent until it is included here.

| Element | Source |
|---|---|
| Retrieved procedures and past cases, each with band, `match_rationale` and an include toggle | `createSession.knowledge` (`06 §5.1`) |
| Initial state of each toggle | The injection posture (`config.Knowledge.Injection`: `opt_in` all off; `auto` strong matches on) |
| Collapsed summary once reviewed: "3 retrieved · 1 included" | Same |
| Search within the investigation: results join the list with their toggle off | §9.2 Search, scoped to the investigation's seed context |
| Each item opens in the inspector | `ui/08 §8.6` / `§8.6.1` |
| Open in the Knowledge view | §9.3 / §9.4 |

The included set rides the next turn (`06 §5.1`); toggling never rewrites past turns.

**Nudges.** Beside ⚡ Propose tests, ⚡ Decide and the response plan, a one-line note when
retrieved items that the `auto` posture would include (procedures; past cases banded
`NEAR_DUPLICATE` or `RELATED`) are not included: "1 procedure and 1 past case the auto
posture would include are not included · Review". It links to the Knowledge section; it never includes
anything by itself.

## 9.7 What a concluded case teaches

At conclude, the Summary lens (`ui/08 §8.4`) adds a **Knowledge** step:

- **Candidate procedure** when the agent judges one warranted: an editable markdown draft
  with **Add to library** / **Discard**. Accepting imports it with a
  `reckon://investigation/<id>` source URL, so accepting again revises rather than
  duplicates. Source: `proposeSOP` (existing conclude hook).
- **This case is now recallable**: a note that the concluded summary has joined the
  past-case corpus, with a link to it in the Knowledge view.

The analyst decides both what is acted on and what is learned; nothing is added without
an explicit accept.

---

*The Knowledge view reads and writes only the knowledge service; it records nothing in any
investigation. Inside an investigation, inclusion is the analyst's act, carried on the
next turn.*
