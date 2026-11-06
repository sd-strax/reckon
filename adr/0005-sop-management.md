# 0005 — An import says what it did, and the operator reads and retires the corpus from the CLI

Issue: #5, #294 · PR: — · Status: accepted

## Context

The SOP corpus had a complete HTTP surface (`GET/POST /api/sops`,
`GET/PUT/DELETE /api/sops/{id}`, `?include_retired=true`, two import routes)
and a store with `Get`, `List`, `Update` and `Retire`, but the CLI
`reckon knowledge` offered only `import` and `reindex`. Retiring a procedure
or seeing what the corpus held meant curl with a dev token. The workbench
deliberately has no library view (`design/13 §4`: SOPs are consultative,
import is an occasional command), so the operator had no window at all.

A byte-identical re-import wrote a new revision. `sopMeta` stamps
`imported_at` with the current time on every import, so the entry's meta,
and with it the content hash, always differed, and `Store.Import` could only
answer created or revised. After [0303](0303-section-recall.md) a needless
revision is not free: it re-cuts the sections, re-embeds every one, and
moves the revision a running agent session is pinned to, so its next
`read_sop` is refused with 409 for a document that did not change.

`getSOP` answered 404 "sop not found" for every store error, a failed
substrate read included, with nothing logged (#294). The update, retire and
section routes beside it already branched on `knowledge.ErrNotFound`. The
inspector reads this route to show a whole procedure, so an outage told the
analyst the procedure did not exist.

Three CLI commands each carried their own copy of the dev-auth login.

## Decision

- **The outcome is a three-valued type on the store.** `Store.Import` returns
  `ImportOutcome`: `ImportCreated`, `ImportRevised`, `ImportUnchanged`,
  replacing the boolean. Both import routes answer the same word in
  `outcome`; a created lineage is 201, a revision and an unchanged re-import
  200. The workbench panel adds `unchanged` to its union ("Already in the
  SOP library as …, nothing changed"), and the draft-accept path says
  "already in the library, unchanged"; a word the panel does not know is
  reported as that row's failure naming the word, never guessed at.
- **`unchanged` compares content, not ingest provenance.** Title, body, tags,
  recommendation, the document's author, and the source's system and
  version are compared against the current revision. The importer and the
  import time are excluded: design/06 §2.4 calls them the provenance of the
  ingest, not the content. The comparison mirrors what a revision would
  write: tags as the substrate stores them (reserved tags stripped,
  duplicates folded, order ignored), and an import naming no author keeps
  the revision's author, so it compares equal. An unchanged import writes
  nothing: no revision, no re-cut, no embedding, and a section pin against
  the current revision holds. Accepted consequence: an identical re-import
  by a different person leaves no trace. A retired lineage is never
  unchanged: the re-import revives it, which is a change to the record.
- **A body has one stored form.** `sopdoc.CanonicalBody` (LF line endings, no
  leading blank lines, no trailing whitespace; the first content line's
  indentation kept, since an opening code block is content) is applied by
  `Parse` to what it reads and by the store on every write path (`Create`,
  `Import`, `Update`), so the JSON route, the markdown route, the CLI and
  `show` agree byte for byte and the comparison is a string comparison. A
  body with nothing but whitespace is refused as empty.
- **A SOP's lineage id is a valid source pointer.** A SOP authored in-product
  has no source. `show` prints `reckon://sop/<id>` (`SOPSourcePrefix`) for
  it, and `Import` resolves that pointer to the lineage when no entry
  carries the url, so the shown document imported again revises the SOP
  instead of opening a second lineage keyed on the file path; the revision
  then carries the pointer and later imports match it by url. A pointer to a
  lineage that does not exist is an ordinary new source.
- **`SOP` carries `revision`.** The number the substrate already keeps, now
  on the facade's shape (`GET /api/sops`, `GET /api/sops/{id}`), so the list
  and the show command name the revision a recall hit and a section read
  name. Additive on the wire.
- **Three CLI subcommands, thin clients over the existing routes.**
  `reckon knowledge list [--include-retired] [--json]` prints id, revision,
  status, title, tags, source url, importer and time; the source url is
  shown because it is the lineage key an operator reads to predict what a
  re-import will revise; a retired SOP is in the same list with `retired` in
  its status column, nothing else. `reckon knowledge show <id> [--json]`
  prints the current revision as the frontmatter + markdown document import
  reads (`sopdoc.Render`, the parser's inverse, carrying exactly the fields
  the import compares), with the ingest provenance on stderr, so
  `show > f.md && import f.md` is an unchanged re-import; that round trip is
  the test. Flags may come before or after the id (the flag package stops at
  the first non-flag, so the rest is parsed in a second pass). `reckon knowledge retire <id> [--json]` is `DELETE
  /api/sops/{id}` followed by a read-back that names what was retired. No
  confirmation and no dry run: retire is soft, the content stays
  hash-addressable, and a re-import revives the lineage. `--json` prints the
  server's JSON for the object(s) touched; the human form is the default.
- **One login.** `backendLogin(ctx, cfg, clientID, user, pass)` in
  `cmd/reckon/login.go` is the dev-auth sign-in for every CLI command that
  calls the backend (`knowledge`, `demo seed`, `investigate`, both of the
  latter's clients), owning the empty-password default and the one hint
  naming `dev-auth`; `loginFlags` declares the `-user`/`-password` pair once;
  `envOr` and `localBackendURL` sit beside it. The three copies are gone.
- **`getSOP` separates not-found from failure** (#294): `ErrNotFound` is 404;
  anything else is `writeInternalError` (500, logged, with a reference), as
  for the routes beside it.

## Alternatives rejected

- **Compare the whole entry meta, `imported_at` included.** Every re-import
  would still differ; the stamp is why the hash always moved.
- **Drop `imported_at` from the meta so the hash alone decides.** The
  importer and time are attribution design/06 §2.4 requires on the record;
  they belong on the revision that brought the content, not nowhere.
- **Record a no-op import as a revision with no content change.** It
  re-cuts, re-embeds and breaks every pin for a document nobody changed;
  the issue says nothing in the record changes.
- **A workbench library view with retire.** `design/13 §4` phases the
  library at v1; the CLI is the operator's tool and reaches the same routes.
- **Confirmation prompt or dry run on retire.** Retire is reversible by
  re-import and loses nothing; a prompt is ceremony around a soft action.
- **A separate `revision_count` route or field.** The substrate's revision
  number already answers it, and the hit and the section read use the same
  number.

## Consequences

- An operator can list, read and retire the corpus without curl; `show`
  output is a valid import document, so editing a procedure is show, edit,
  import. The list client reads the whole response: `GET /api/sops` carries
  every current SOP with its body and is not paged, so a cap would hand a
  truncated document to the decoder; paging the list is a route change for
  the day a corpus needs it.
- A re-run of an import directory is a no-op that says so, and a running
  agent session's section pins survive it.
- `GET /api/sops/{id}` can no longer report an outage as a missing
  procedure.
- `ImportOutcome` is a wire word three clients read (CLI, workbench panel,
  draft accept); a fourth value is a change to all three, and until it is
  made the panel and the draft accept report the unknown word as a failure
  rather than guess (the draft is kept).
- `Import` still lists the whole corpus to find a source url, as before;
  with `unchanged` the common outcome of a re-run directory import, that
  scan is the cost of a no-op. An indexed lineage key is a store change
  with its own record.
- The retrieved-bytes question #97 raises is unaffected: `revision` on the
  SOP shape is the number a historical read will take.
