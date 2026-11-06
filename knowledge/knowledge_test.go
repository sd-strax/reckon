package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/sd-strax/reckon/internal/pgmigrate"
	"github.com/sd-strax/reckon/internal/sopdoc"
	"github.com/sd-strax/reckon/internal/testpg"
	"github.com/sd-strax/reckon/knowledge/substrate"
)

// testPg is a single embedded-postgres shared across the knowledge tests. It
// runs both migration sets on reckon_knowledge exactly as runtime does — the
// host's knowledge migrations under the default table and the substrate's
// under its own — so the two independently-versioned schemas coexist. Port
// differs from other packages' test Pg so parallel `go test ./...` doesn't
// collide.
var (
	testPg    *embeddedpostgres.EmbeddedPostgres
	testDB    *sql.DB
	testReady bool
)

var testTenant = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	dir, err := os.MkdirTemp("", "knowledge-test-pg-*")
	if err != nil {
		log.Fatalf("temp dir: %v", err)
	}
	cleanupDir := func() { _ = os.RemoveAll(dir) }

	port, err := testpg.FreePort()
	if err != nil {
		cleanupDir()
		log.Fatalf("%v", err)
	}
	testPg = embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Port(port).
		RuntimePath(filepath.Join(dir, "runtime")).
		DataPath(filepath.Join(dir, "data")).
		Username("test").Password("test").Database("reckon_knowledge"))

	if err := testPg.Start(); err != nil {
		cleanupDir()
		log.Fatalf("embedded postgres start: %v", err)
	}
	dsn := testpg.DSN(port, "reckon_knowledge")
	if err := pgmigrate.Run(dsn, Migrations(), "reckon_knowledge"); err != nil {
		_ = testPg.Stop()
		cleanupDir()
		log.Fatalf("knowledge migrate: %v", err)
	}
	if err := pgmigrate.RunWithTable(dsn, substrate.Migrations(), "reckon_knowledge_substrate", "substrate_schema_migrations"); err != nil {
		_ = testPg.Stop()
		cleanupDir()
		log.Fatalf("substrate migrate: %v", err)
	}
	testDB, err = sql.Open("postgres", dsn)
	if err != nil {
		_ = testPg.Stop()
		cleanupDir()
		log.Fatalf("sql.Open: %v", err)
	}

	testReady = true
	code := m.Run()
	_ = testDB.Close()
	_ = testPg.Stop()
	cleanupDir()
	os.Exit(code)
}

// reset truncates the substrate tables and returns a fresh facade store over a
// keyword-only substrate (no embedder) unless opts add one.
func reset(t *testing.T, opts ...substrate.Option) *Store {
	t.Helper()
	if !testReady {
		t.Skip("embedded postgres not available")
	}
	if _, err := testDB.Exec(`TRUNCATE substrate_entries, substrate_sections, substrate_embeddings, substrate_tombstones`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	sub, err := substrate.NewPostgres(testDB, []substrate.CorpusDef{
		{Name: CorpusProcedures, Archetype: substrate.Curated, Governance: substrate.Lightweight},
		{Name: CorpusCaseSummaries, Archetype: substrate.Derived},
	}, opts...)
	if err != nil {
		t.Fatalf("substrate: %v", err)
	}
	return NewStore(sub)
}

// TestSOP_CRUD walks create → get → list → update → retire and the retired
// visibility rules. Update goes through a substrate revision under the hood,
// but the SOP's lineage id must stay stable across it.
func TestSOP_CRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()

	id, err := s.Create(ctx, SOP{
		TenantID: testTenant, Title: "Ransomware containment",
		Body: "Isolate first, preserve volatile evidence, notify the on-call lead.",
		Tags: []string{"ransomware", "T1486"}, Recommendation: "isolate",
		AuthorID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.Get(ctx, testTenant, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusPublished || got.Recommendation != "isolate" || got.PublishedAt == nil {
		t.Errorf("created SOP: status=%q rec=%q published=%v; want published/isolate/set", got.Status, got.Recommendation, got.PublishedAt)
	}
	// The reserved lineage tag must never leak into the user-facing tags.
	if len(got.Tags) != 2 {
		t.Errorf("tags = %v; want 2 (no reserved lineage tag)", got.Tags)
	}
	for _, tag := range got.Tags {
		if len(tag) >= 2 && tag[:2] == "__" {
			t.Errorf("reserved tag leaked to the SOP view: %q", tag)
		}
	}

	if err := s.Update(ctx, testTenant, id, "Ransomware containment", "Updated body.", []string{"ransomware"}, "require-secondary"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.Get(ctx, testTenant, id) // SAME id after a revision
	if got.ID != id {
		t.Errorf("lineage id moved on update: %s -> %s", id, got.ID)
	}
	if got.Body != "Updated body." || got.Recommendation != "require-secondary" || len(got.Tags) != 1 {
		t.Errorf("update not applied: %+v", got)
	}

	// Exactly one current SOP after an edit — the prior revision is superseded,
	// not a second list entry.
	list, _ := s.List(ctx, testTenant, false)
	if len(list) != 1 {
		t.Errorf("edit produced a duplicate list entry: %d", len(list))
	}

	// List excludes retired.
	if err := s.Retire(ctx, testTenant, id); err != nil {
		t.Fatalf("retire: %v", err)
	}
	list, _ = s.List(ctx, testTenant, false)
	if len(list) != 0 {
		t.Errorf("retired SOP visible in default list: %d", len(list))
	}
	list, _ = s.List(ctx, testTenant, true)
	if len(list) != 1 || list[0].Status != StatusRetired || list[0].ID != id {
		t.Errorf("include-retired list wrong: %+v", list)
	}

	// A retired SOP can't be updated.
	if err := s.Update(ctx, testTenant, id, "x", "y", nil, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("update of retired SOP: err=%v; want ErrNotFound", err)
	}
}

// TestImportSOP covers the institutional-knowledge seeding path (design/06
// §2.4): attribution round-trips, and re-importing the same source URL revises
// the existing SOP instead of duplicating.
func TestImportSOP(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()

	doc := SOP{
		TenantID: testTenant,
		Title:    "Ransomware Containment Runbook",
		Body:     "Isolate first, preserve volatile evidence.",
		Tags:     []string{"ransomware", "T1486"},
		AuthorID: "Jane Okoro", // the ORIGINAL author, from the document
		Source: &SOPSource{
			System: "confluence", URL: "https://wiki.example.com/RUNBOOK-42", Version: "3.2",
		},
		ImportedBy: "sam",
	}
	id, outcome, err := s.Import(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != ImportCreated {
		t.Errorf("first import: %s, want created", outcome)
	}
	got, err := s.Get(ctx, testTenant, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthorID != "Jane Okoro" || got.ImportedBy != "sam" || got.ImportedAt == nil || got.Revision != 1 {
		t.Errorf("attribution not round-tripped: %+v", got)
	}
	if got.Source == nil || got.Source.URL != doc.Source.URL || got.Source.System != "confluence" || got.Source.Version != "3.2" {
		t.Errorf("source not round-tripped: %+v", got.Source)
	}

	// Re-import of the SAME source URL is a revision — same id, no duplicate.
	doc.Body = "Isolate first, preserve volatile evidence, and notify legal."
	doc.Source.Version = "3.3"
	id2, outcome, err := s.Import(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != ImportRevised || id2 != id {
		t.Errorf("re-import should revise the same SOP: %s id %s vs %s", outcome, id2, id)
	}
	got, _ = s.Get(ctx, testTenant, id)
	if !strings.Contains(got.Body, "notify legal") || got.Source.Version != "3.3" || got.Revision != 2 {
		t.Errorf("re-import content not applied: %+v", got)
	}
	list, _ := s.List(ctx, testTenant, false)
	if len(list) != 1 {
		t.Errorf("re-import duplicated the SOP: %d entries", len(list))
	}

	// A DIFFERENT source URL is a distinct SOP.
	other := doc
	other.Title = "BEC Response"
	other.Source = &SOPSource{URL: "https://wiki.example.com/RUNBOOK-77"}
	if _, outcome, err := s.Import(ctx, other); err != nil || outcome != ImportCreated {
		t.Errorf("distinct source should create: %s err=%v", outcome, err)
	}

	// Missing source.url is refused — it is the lineage key.
	bad := doc
	bad.Source = nil
	if _, _, err := s.Import(ctx, bad); err == nil {
		t.Error("import without source.url accepted")
	}

	// Retire, then re-import the same source: one source is one lineage, so
	// the retired SOP is REVIVED (same id, a new published revision) — never a
	// second lineage beside the retired one.
	if err := s.Retire(ctx, testTenant, id); err != nil {
		t.Fatal(err)
	}
	if live, _ := s.List(ctx, testTenant, false); len(live) != 1 { // only RUNBOOK-77 remains
		t.Fatalf("after retire: %d live SOPs, want 1", len(live))
	}
	doc.Body = "Isolate first, preserve volatile evidence, notify legal, and brief the IR lead."
	id3, outcome, err := s.Import(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != ImportRevised || id3 != id {
		t.Errorf("re-import of a retired source should revive the lineage: %s id %s vs %s", outcome, id3, id)
	}
	got, err = s.Get(ctx, testTenant, id)
	if err != nil || !strings.Contains(got.Body, "IR lead") {
		t.Errorf("revived SOP not current: err=%v body=%q", err, got.Body)
	}
	live, _ := s.List(ctx, testTenant, false)
	all, _ := s.List(ctx, testTenant, true)
	if len(live) != 2 || len(all) != 2 {
		t.Errorf("revive should restore one live SOP, not add a lineage: live=%d all=%d, want 2/2", len(live), len(all))
	}
}

func TestGetUnknownSOP(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	if _, err := s.Get(context.Background(), testTenant, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("get unknown: err=%v; want ErrNotFound", err)
	}
}

// TestRecallSOPs covers keyword ranking (the degraded backend), the tag
// hard-filter, empty-query browse, retired exclusion, and EMPTY coverage.
func TestRecallSOPs(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t) // keyword backend (no embedder)
	ctx := context.Background()

	mk := func(title, body string, tags ...string) uuid.UUID {
		id, err := s.Create(ctx, SOP{TenantID: testTenant, Title: title, Body: body, Tags: tags})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	ransom := mk("Ransomware playbook", "On ransomware detection, isolate the host immediately and preserve shadow copies.", "ransomware", "T1486")
	mk("Phishing triage", "For phishing reports, quarantine the message and check click telemetry.", "phishing", "T1566")
	mk("BEC response", "Business email compromise: reset the mailbox and review inbox rules.", "bec")

	// Empty-query, no filter → recency browse (all non-retired).
	all, err := s.RecallSOPs(ctx, testTenant, RecallRequest{})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if all.Coverage != CoverageComplete || len(all.Results) != 3 {
		t.Errorf("recall-all: coverage=%q n=%d; want COMPLETE/3", all.Coverage, len(all.Results))
	}

	// Keyword query → ransomware SOP ranks top, attributed to the fallback.
	q, _ := s.RecallSOPs(ctx, testTenant, RecallRequest{Query: "ransomware isolate host"})
	if len(q.Results) == 0 || q.Results[0].SOPID != ransom {
		t.Errorf("query did not rank the ransomware SOP first: %+v", q.Results)
	}
	if q.Results[0].Score <= 0 {
		t.Errorf("keyword score = %v; want > 0", q.Results[0].Score)
	}
	if q.Results[0].Backend != "pg-fts/1" {
		t.Errorf("backend attribution = %q; want pg-fts/1", q.Results[0].Backend)
	}
	// Reserved lineage tag must not surface in match results.
	for _, tag := range q.Results[0].Tags {
		if len(tag) >= 2 && tag[:2] == "__" {
			t.Errorf("reserved tag leaked into a recall result: %q", tag)
		}
	}

	// Tag hard-filter via empty-query browse.
	tagged, _ := s.RecallSOPs(ctx, testTenant, RecallRequest{Tags: []string{"phishing"}})
	if len(tagged.Results) != 1 || tagged.Results[0].Title != "Phishing triage" {
		t.Errorf("tag filter wrong: %+v", tagged.Results)
	}

	// No match → EMPTY (evidence-of-absence).
	none, _ := s.RecallSOPs(ctx, testTenant, RecallRequest{Query: "cryptomining kubernetes"})
	if none.Coverage != CoverageEmpty || len(none.Results) != 0 {
		t.Errorf("no-match: coverage=%q n=%d; want EMPTY/0", none.Coverage, len(none.Results))
	}

	// Retired excluded by default.
	if err := s.Retire(ctx, testTenant, ransom); err != nil {
		t.Fatal(err)
	}
	after, _ := s.RecallSOPs(ctx, testTenant, RecallRequest{Query: "ransomware"})
	if len(after.Results) != 0 {
		t.Errorf("retired SOP returned by default recall: %+v", after.Results)
	}
}

// TestRecallSOPs_Sections proves the facade carries the matched section, the
// outline and the unit through to the SOP shape (design/06 §4.2), and that a
// browse hit carries the first section.
func TestRecallSOPs_Sections(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t) // keyword backend
	ctx := context.Background()
	const body = "# Lateral Movement — Containment\n\nScope: a remote logon from an internal host.\n\n## Triage\nConfirm the session.\n\n## Containment\nIsolate the destination host; prefer network isolation.\n\n## Do not\nDo not reimage the destination before collection.\n"
	id, err := s.Create(ctx, SOP{TenantID: testTenant, Title: "RDP containment", Body: body, Tags: []string{"rdp"}})
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.RecallSOPs(ctx, testTenant, RecallRequest{Query: "isolate destination host"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 || res.Results[0].SOPID != id {
		t.Fatalf("results %+v", res.Results)
	}
	h := res.Results[0]
	if h.Section.Idx != 3 || h.Section.Of != 4 || h.Section.Location != "Lateral Movement — Containment > Containment" {
		t.Errorf("section %+v", h.Section)
	}
	if !strings.HasPrefix(h.Excerpt, "## Containment") || strings.Contains(h.Excerpt, "Scope:") || h.Truncated {
		t.Errorf("excerpt %q truncated=%v", h.Excerpt, h.Truncated)
	}
	if len(h.Outline) != 4 || h.Outline[3] != "Lateral Movement — Containment > Do not" {
		t.Errorf("outline %q", h.Outline)
	}

	// Section unit: two pieces of one procedure.
	res, err = s.RecallSOPs(ctx, testTenant, RecallRequest{Query: "destination", Unit: UnitSection})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 2 || res.Results[0].SOPID != id || res.Results[1].SOPID != id || res.Results[0].Section.Idx == res.Results[1].Section.Idx {
		t.Errorf("section unit: %+v", res.Results)
	}

	// A requested budget cuts the excerpt and says so.
	res, err = s.RecallSOPs(ctx, testTenant, RecallRequest{Query: "isolate destination host", ExcerptBudget: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Results[0].Truncated || len([]rune(res.Results[0].Excerpt)) != 11 {
		t.Errorf("budget: %q truncated=%v", res.Results[0].Excerpt, res.Results[0].Truncated)
	}

	// An unknown unit is the caller's error.
	if _, err := s.RecallSOPs(ctx, testTenant, RecallRequest{Query: "x", Unit: "paragraph"}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("unknown unit: %v", err)
	}

	// Browse carries the first section and the outline, from the stored cut,
	// and every hit names the revision its indexes belong to.
	res, err = s.RecallSOPs(ctx, testTenant, RecallRequest{Tags: []string{"rdp"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 || res.Results[0].Section.Idx != 1 || len(res.Results[0].Outline) != 4 || !strings.HasPrefix(res.Results[0].Excerpt, "# Lateral Movement") || res.Results[0].Revision != 1 {
		t.Errorf("browse: %+v", res.Results)
	}
	stored, err := s.Sections(ctx, testTenant, id, SectionPin{})
	if err != nil {
		t.Fatal(err)
	}
	for i, loc := range res.Results[0].Outline {
		if stored.Sections[i].Location != loc {
			t.Errorf("browse outline %d = %q; stored cut says %q", i, loc, stored.Sections[i].Location)
		}
	}
}

// TestSOPSections covers the outline and single-section reads behind
// GET /api/sops/{id}/sections[/{n}].
func TestSOPSections(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()
	const body = "# RDP containment\n\nScope line.\n\n## Triage\nConfirm the session.\n\n## Containment\nIsolate the destination host.\n\n## Do not\nDo not reimage first.\n"
	id, err := s.Create(ctx, SOP{TenantID: testTenant, Title: "RDP containment", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	outline, err := s.Sections(ctx, testTenant, id, SectionPin{})
	if err != nil {
		t.Fatal(err)
	}
	if outline.SOPID != id || outline.Revision != 1 || outline.Cut != "1/16000" || len(outline.Sections) != 4 {
		t.Fatalf("outline %+v", outline)
	}
	if o := outline.Sections[2]; o.Idx != 3 || o.Of != 4 || o.Location != "RDP containment > Containment" || o.Text != "" || o.Runes == 0 {
		t.Errorf("outline entry %+v", o)
	}
	read, err := s.Section(ctx, testTenant, id, 3, SectionPin{Revision: 1}) // pinned to the revision recall returned
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(read.Section.Text, "## Containment") || read.Section.Runes != len([]rune(read.Section.Text)) || len(read.Outline) != 4 || read.Cut != "1/16000" {
		t.Errorf("section read %+v", read)
	}
	if _, err := s.Section(ctx, testTenant, id, 9, SectionPin{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("out-of-range section: %v", err)
	}
	if _, err := s.Sections(ctx, testTenant, uuid.New(), SectionPin{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown sop: %v", err)
	}
	// An update re-cuts: the new revision's outline is read, not the old, and
	// a read pinned to the recalled revision is refused rather than answered
	// with a different section under the same index.
	if err := s.Update(ctx, testTenant, id, "RDP containment", "# Only\n\nOne section now.", nil, ""); err != nil {
		t.Fatal(err)
	}
	outline, err = s.Sections(ctx, testTenant, id, SectionPin{})
	if err != nil || outline.Revision != 2 || len(outline.Sections) != 1 {
		t.Errorf("after update: %v %+v", err, outline)
	}
	if _, err := s.Section(ctx, testTenant, id, 3, SectionPin{Revision: 1}); !errors.Is(err, ErrRevisionChanged) {
		t.Errorf("read pinned to revision 1 after revision 2: %v; want ErrRevisionChanged", err)
	}
	if _, err := s.Sections(ctx, testTenant, id, SectionPin{Revision: 1}); !errors.Is(err, ErrRevisionChanged) {
		t.Errorf("outline pinned to revision 1 after revision 2: %v; want ErrRevisionChanged", err)
	}
}

// TestRecallSemantic proves the facade drives the vector backend and resolves
// SOP ids from the lineage tag on a recall hit (which carries no meta).
func TestRecallSemantic(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	emb := &stubEmbedder{vec: map[string][]float32{}}
	s := reset(t, substrate.WithEmbedder(emb))
	ctx := context.Background()

	// Two SOPs on orthogonal axes; the query aligns with the first.
	emb.set("Ransomware isolation\n\nIsolate the host, preserve memory.", 1, 0)
	emb.set("Phishing triage\n\nCheck headers, detonate attachments.", 0, 1)
	emb.set("what do we do when a machine is ransomed?", 1, 0)

	rid, err := s.Create(ctx, SOP{TenantID: testTenant, Title: "Ransomware isolation", Body: "Isolate the host, preserve memory.", Tags: []string{"ransomware"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, SOP{TenantID: testTenant, Title: "Phishing triage", Body: "Check headers, detonate attachments.", Tags: []string{"phishing"}}); err != nil {
		t.Fatal(err)
	}

	res, err := s.RecallSOPs(ctx, testTenant, RecallRequest{Query: "what do we do when a machine is ransomed?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) == 0 || res.Results[0].SOPID != rid {
		t.Fatalf("semantic recall did not rank/resolve the ransomware SOP: %+v", res.Results)
	}
	if res.Results[0].Backend != "vector-cosine/1/stub-embed" {
		t.Errorf("backend = %q; want vector-cosine/1/stub-embed", res.Results[0].Backend)
	}
}

// stubEmbedder returns fixed 2-D vectors per exact text; unmapped text is
// orthogonal to both axes so it never falsely matches.
type stubEmbedder struct{ vec map[string][]float32 }

func (e *stubEmbedder) Model() string { return "stub-embed" }
func (e *stubEmbedder) set(text string, x, y float32) {
	e.vec[text] = []float32{x, y}
}
func (e *stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, s := range texts {
		if v, ok := e.vec[s]; ok {
			out[i] = v
		} else {
			out[i] = []float32{0, 0.0001} // near-zero, effectively orthogonal
		}
	}
	return out, nil
}

// TestSOPSections_CutPinned: a smaller section budget re-cuts a procedure
// under the same revision, so a read pinned to the revision alone would
// answer a different piece under the same index. The pin names the cut too,
// and a read under the old cut is refused.
func TestSOPSections_CutPinned(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()
	const body = "# RDP containment\n\nScope line, long enough to be cut into parts under a small budget when the time comes.\n\n## Triage\nConfirm the session.\n\n## Containment\nIsolate the destination host.\n"
	id, err := s.Create(ctx, SOP{TenantID: testTenant, Title: "RDP containment", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	hits, err := s.RecallSOPs(ctx, testTenant, RecallRequest{Query: "isolate the destination host"})
	if err != nil || len(hits.Results) != 1 {
		t.Fatalf("recall: %v %+v", err, hits)
	}
	hit := hits.Results[0]
	if hit.Revision != 1 || hit.Cut != "1/16000" || hit.Section.Idx != 3 {
		t.Fatalf("hit %+v", hit)
	}
	pin := SectionPin{Revision: hit.Revision, Cut: hit.Cut}
	if read, err := s.Section(ctx, testTenant, id, hit.Section.Idx, pin); err != nil || !strings.HasPrefix(read.Section.Text, "## Containment") {
		t.Fatalf("pinned read: %v %+v", err, read)
	}

	// The operator lowers the budget and the startup reconciliation re-cuts
	// the stored sections: same revision, new numbering.
	small, err := substrate.NewPostgres(testDB, []substrate.CorpusDef{
		{Name: CorpusProcedures, Archetype: substrate.Curated, Governance: substrate.Lightweight},
		{Name: CorpusCaseSummaries, Archetype: substrate.Derived},
	}, substrate.WithSectionBudget(60))
	if err != nil {
		t.Fatal(err)
	}
	s2 := NewStore(small)
	if n, err := s2.Reindex(ctx, testTenant); err != nil || n != 1 {
		t.Fatalf("reindex: %d %v", n, err)
	}
	outline, err := s2.Sections(ctx, testTenant, id, SectionPin{Revision: 1})
	if err != nil || outline.Revision != 1 || outline.Cut != "1/60" || len(outline.Sections) <= 3 {
		t.Fatalf("outline after re-cut: %v %+v", err, outline)
	}
	if _, err := s2.Section(ctx, testTenant, id, hit.Section.Idx, pin); !errors.Is(err, ErrCutChanged) {
		t.Errorf("read under the old cut: %v; want ErrCutChanged", err)
	}
	if _, err := s2.Sections(ctx, testTenant, id, pin); !errors.Is(err, ErrCutChanged) {
		t.Errorf("outline under the old cut: %v; want ErrCutChanged", err)
	}
	if _, err := s2.Section(ctx, testTenant, id, 1, SectionPin{Revision: 1, Cut: "1/60"}); err != nil {
		t.Errorf("read under the new cut: %v", err)
	}
}

// TestImportSOP_Unchanged: a re-import that brings the content the current
// revision already carries writes nothing. The importer and the import time
// are provenance, not content, so a second person importing the same document
// is unchanged too; the order of the tags is not content; any compared field
// moving is a revision; and a retired lineage is revived, never unchanged.
func TestImportSOP_Unchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()
	doc := SOP{
		TenantID:       testTenant,
		Title:          "Ransomware Containment Runbook",
		Body:           "# Scope\n\nIsolate first.\n\n# Triage\n\nPreserve volatile evidence.",
		Tags:           []string{"ransomware", "T1486"},
		Recommendation: "isolate",
		AuthorID:       "Jane Okoro",
		Source:         &SOPSource{System: "confluence", URL: "https://wiki.example.com/RUNBOOK-42", Version: "3.2"},
		ImportedBy:     "sam",
	}
	id, outcome, err := s.Import(ctx, doc)
	if err != nil || outcome != ImportCreated {
		t.Fatalf("first import: %s %v", outcome, err)
	}
	first, _ := s.Get(ctx, testTenant, id)

	same := doc
	same.ImportedBy = "alex" // a different importer, the same document
	same.Tags = []string{"T1486", "ransomware"}
	if id2, outcome, err := s.Import(ctx, same); err != nil || outcome != ImportUnchanged || id2 != id {
		t.Fatalf("identical re-import: %s %s %v; want unchanged under %s", id2, outcome, err, id)
	}
	after, _ := s.Get(ctx, testTenant, id)
	if after.Revision != 1 || after.ImportedBy != "sam" || !after.ImportedAt.Equal(*first.ImportedAt) {
		t.Errorf("unchanged import wrote something: %+v", after)
	}
	if list, _ := s.List(ctx, testTenant, false); len(list) != 1 || list[0].Revision != 1 {
		t.Errorf("unchanged import changed the list: %+v", list)
	}

	// Each compared field moving on its own is a revision.
	for name, change := range map[string]func(*SOP){
		"title":          func(x *SOP) { x.Title += " (v2)" },
		"body":           func(x *SOP) { x.Body += "\n\nNotify legal." },
		"tags":           func(x *SOP) { x.Tags = append(x.Tags, "legal") },
		"recommendation": func(x *SOP) { x.Recommendation = "escalate" },
		"author":         func(x *SOP) { x.AuthorID = "Sam Lee" },
		"source system":  func(x *SOP) { x.Source = &SOPSource{System: "wiki", URL: x.Source.URL, Version: x.Source.Version} },
		"source version": func(x *SOP) { x.Source = &SOPSource{System: x.Source.System, URL: x.Source.URL, Version: "3.3"} },
	} {
		changed := doc
		change(&changed)
		if _, outcome, err := s.Import(ctx, changed); err != nil || outcome != ImportRevised {
			t.Errorf("%s changed: %s %v; want revised", name, outcome, err)
		}
		if _, outcome, err := s.Import(ctx, doc); err != nil || outcome != ImportRevised {
			t.Errorf("%s restored: %s %v; want revised", name, outcome, err)
		}
	}

	// What a revision would write anyway is not a change: a repeated tag
	// folds, a reserved tag echoed back is stripped, and an import naming no
	// author keeps the revision's author.
	before, _ := s.Get(ctx, testTenant, id)
	for name, change := range map[string]func(*SOP){
		"repeated tag":      func(x *SOP) { x.Tags = []string{"ransomware", "T1486", "ransomware"} },
		"reserved tag echo": func(x *SOP) { x.Tags = []string{"ransomware", "T1486", "__sop_lineage:" + id.String()} },
		"no author":         func(x *SOP) { x.AuthorID = "" },
	} {
		same := doc
		change(&same)
		if _, outcome, err := s.Import(ctx, same); err != nil || outcome != ImportUnchanged {
			t.Errorf("%s: %s %v; want unchanged", name, outcome, err)
		}
	}
	if got, _ := s.Get(ctx, testTenant, id); got.Revision != before.Revision {
		t.Errorf("a no-op import wrote a revision: %d -> %d", before.Revision, got.Revision)
	}

	// A body is stored in its canonical form on every write path, so what
	// `show` prints and `import` reads back compares equal to what a JSON
	// import stored with CRLFs and a trailing newline; the indentation of an
	// opening code block is content and is kept.
	coded := doc
	coded.Body = "    Get-Process\r\n\r\nThen isolate.\r\n"
	if _, outcome, err := s.Import(ctx, coded); err != nil || outcome != ImportRevised {
		t.Fatalf("CRLF body: %s %v", outcome, err)
	}
	if got, _ := s.Get(ctx, testTenant, id); got.Body != "    Get-Process\n\nThen isolate." {
		t.Errorf("stored body not canonical: %q", got.Body)
	}
	coded.Body = "    Get-Process\n\nThen isolate."
	if _, outcome, err := s.Import(ctx, coded); err != nil || outcome != ImportUnchanged {
		t.Errorf("canonical re-import of the CRLF body: %s %v; want unchanged", outcome, err)
	}
	if _, outcome, err := s.Import(ctx, doc); err != nil || outcome != ImportRevised {
		t.Fatalf("restore: %s %v", outcome, err)
	}

	// A retired lineage re-imported with the same content is revived, so the
	// import is a revision: the record changes.
	if err := s.Retire(ctx, testTenant, id); err != nil {
		t.Fatal(err)
	}
	if _, outcome, err := s.Import(ctx, doc); err != nil || outcome != ImportRevised {
		t.Errorf("retired then re-imported: %s %v; want revised", outcome, err)
	}
	if got, _ := s.Get(ctx, testTenant, id); got.Status != StatusPublished {
		t.Errorf("revived SOP status %s", got.Status)
	}
}

// TestImportSOP_LineageAsSource: a SOP authored in-product has no source; its
// lineage id is its pointer (what `show` prints), so importing the shown
// document revises that lineage, never opens a second one, and the revision
// then carries the pointer so the next identical import is unchanged.
func TestImportSOP_LineageAsSource(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()
	id, err := s.Create(ctx, SOP{TenantID: testTenant, Title: "Authored here", Body: "Step one.", Tags: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	shown := SOP{
		TenantID: testTenant, Title: "Authored here", Body: "Step one.", Tags: []string{"x"},
		Source: &SOPSource{System: "reckon", URL: SOPSourcePrefix + id.String()},
	}
	got, outcome, err := s.Import(ctx, shown)
	if err != nil || outcome != ImportRevised || got != id {
		t.Fatalf("first import of the shown document: %s %s %v; want revised under %s", got, outcome, err, id)
	}
	if _, outcome, err := s.Import(ctx, shown); err != nil || outcome != ImportUnchanged {
		t.Errorf("second import: %s %v; want unchanged", outcome, err)
	}
	if list, _ := s.List(ctx, testTenant, false); len(list) != 1 || list[0].Revision != 2 || list[0].Source == nil || list[0].Source.URL != SOPSourcePrefix+id.String() {
		t.Errorf("want one lineage at revision 2 carrying its pointer: %+v", list)
	}
	// A pointer to a lineage that does not exist is an ordinary new source.
	fresh := shown
	fresh.Source = &SOPSource{URL: SOPSourcePrefix + uuid.NewString()}
	if _, outcome, err := s.Import(ctx, fresh); err != nil || outcome != ImportCreated {
		t.Errorf("unknown lineage pointer: %s %v; want created", outcome, err)
	}
}

// TestImportSOP_UnchangedAgainstARawRow: a row written before bodies had one
// stored form (CRLFs, a trailing newline, as the JSON route once passed them
// through) is compared in canonical form, so an untouched show-then-import of
// it is unchanged and its stored bytes, which the content hash covers, are
// left as they are.
func TestImportSOP_UnchangedAgainstARawRow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()
	lineage := uuid.New()
	raw := "    Get-Process\r\n\r\nThen isolate.\r\n"
	src := &SOPSource{System: "confluence", URL: "https://wiki.example.com/RAW-1", Version: "1"}
	if _, err := s.sub.Put(ctx, testTenant.String(), CorpusProcedures, substrate.Entry{
		Title: "Raw", Body: raw, Tags: withLineage([]string{"x"}, lineage), AuthoredBy: "Jane Okoro",
		Meta: sopMeta(SOP{Source: src, ImportedBy: "sam"}),
	}); err != nil {
		t.Fatal(err)
	}

	shown, err := s.Get(ctx, testTenant, lineage)
	if err != nil || shown.Body != raw {
		t.Fatalf("the raw row is served as stored: %q %v", shown.Body, err)
	}
	again := SOP{TenantID: testTenant, Title: "Raw", Body: sopdoc.CanonicalBody(shown.Body), Tags: shown.Tags, AuthorID: shown.AuthorID, Source: src, ImportedBy: "alex"}
	if id, outcome, err := s.Import(ctx, again); err != nil || outcome != ImportUnchanged || id != lineage {
		t.Errorf("untouched re-import of a raw row: %s %s %v; want unchanged", id, outcome, err)
	}
	// The identical original payload, CRLFs and all, is unchanged too.
	again.Body = raw
	if _, outcome, err := s.Import(ctx, again); err != nil || outcome != ImportUnchanged {
		t.Errorf("original raw payload again: %s %v; want unchanged", outcome, err)
	}
	if got, _ := s.Get(ctx, testTenant, lineage); got.Revision != 1 || got.Body != raw {
		t.Errorf("the stored row moved: rev %d body %q", got.Revision, got.Body)
	}
}
