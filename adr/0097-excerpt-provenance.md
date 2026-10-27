# 0097 — A retrieved procedure excerpt is kept by its content hash with its revision and location; the lineage and the retained original are readable, and the inspector says whether the procedure changed since

Issue: #97 · PR: #98 · Status: accepted

## Context

A turn that consulted a procedure recorded the procedure's id, title, score and whether it was
applied (`06 §6.1`), but not the excerpt the model read, where in the document it sat, or
which revision it came from. `06 §6.2` specified a Layer B side store for "the actual retrieved
SOP excerpts … keyed by content hash" so that "what did the AI see when it cited this SOP?" is
answerable, and `06 §2.4` asked that import retain the original; neither was built. The
inspector's "What the AI read" tab could show only the excerpt the current retrieval happened
to hold, could not say whether the procedure had changed since the turn consulted it, and had
no original to open. Gap 15 of `design/ui/00-backend-binding.md §6`.

Constraints: the event log carries hashes, never bulk (`02 §6`); the knowledge facade reaches
the substrate only through its interface, and a revision's content stays hash-addressable
(`knowledge/design/00-substrate.md §6`); the parser stays the one parser (`internal/sopdoc`).

## Decision

- **The side store is built in `reckon_main`**: `knowledge_retrievals` holds the excerpt's
  bytes keyed by `(tenant, excerpt_hash)` with the procedure, the revision hash it came from and
  its location, so repeated retrievals of one revision share a row; `knowledge_retrieval_uses`
  records each interpretation's use with the score and band at the time. Both are written by
  the aggregate's side store in the interpretation's transaction. The event's `consulted_sops`
  entry gains `excerpt_hash` and `revision_hash` only.
- **Recall names its provenance.** `SOPMatch` carries the revision's `content_hash`, its
  `revision` and a `location` (`knowledge.Locate`: the excerpt's rune offset and the markdown
  heading path in force there; a page when an import records pages). The agent session keeps
  each result's excerpt, hash, location and band and sends them with the commit as
  `consulted_sops[].excerpt` etc.; the server computes the excerpt hash and writes the rows.
- **Imports retain the original with the revision.** `POST /api/sops/import_markdown` stores
  the file's bytes, name and media type in the revision's meta (part of the hashed content, so
  the revision hash covers them) with `format: markdown`; `GET /api/sops/{id}/original?rev=`
  serves it. The JSON import endpoint has no file and retains none.
- **Reads.** `GET /api/sops/{id}/revisions` lists the lineage (revision, hash, time, author,
  importer, source version, whether an original exists); `GET /api/sops/{id}/revisions/{hash}`
  returns a revision's content; `GET /api/investigations/{id}/retrievals?sop=` returns the
  excerpts the investigation's turns retrieved with the turn, the location, the band, and
  `matches_current` against the procedure's current hash.
- **The inspector** shows each retrieved excerpt verbatim with "excerpt k of n", its heading
  path, its band and the turn that retrieved it; "Exactly as retrieved · hash matches current
  version" when the revision is current, else "updated since this case consulted it" with a
  line diff against the same span of the current text, rendered client-side; Versions lists the
  lineage with Open original. The Knowledge document's Versions tab compares any two revisions.

## Alternatives rejected

- **A separate `sop_originals` table in `reckon_main`**: the original belongs with the revision
  it was imported as, and the facade owns no SQL; the revision's hashed meta already is the
  attestation boundary.
- **Chunking procedures at import** to give "k of n" real meaning: the substrate serves one
  budgeted excerpt per revision today; the fields are carried as 1 of 1 rather than invented,
  and chunking is a substrate change when it comes.
- **Storing the excerpt on the event**: the event log is not for bulk, and the hash is what
  verifies.

## Consequences

- Migration `0026`. Turns committed before it have no excerpts; the inspector says so and shows
  the rail's current excerpt as such.
- Editing a procedure in the product keeps the original on the revision it came with; the
  current revision carries the meta forward, so the original stays reachable.
- Gap 22 (where a procedure was used) has its join: `knowledge_retrieval_uses` by procedure.
