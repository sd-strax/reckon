package substrate

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := testDB.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPutWritesSectionsAndSectionVectors(t *testing.T) {
	fake := &fakeEmbedder{}
	st := newTestStore(t, WithEmbedder(fake))
	ctx := context.Background()

	e, err := st.Put(ctx, testNS, "procedures", Entry{Title: "Containment", Body: procedureDoc})
	if err != nil {
		t.Fatal(err)
	}
	secs, err := st.Sections(ctx, testNS, "procedures", e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 4 {
		t.Fatalf("got %d sections, want 4:\n%s", len(secs), dumpSections(secs))
	}
	if want := SplitSections(procedureDoc, DefaultSectionBudgetRunes); len(want) != len(secs) || secs[2].Text != want[2].Text || secs[2].Location() != want[2].Location() {
		t.Errorf("stored sections differ from the splitter's cut:\n%s", dumpSections(secs))
	}
	assertOffsets(t, procedureDoc, secs)

	// One embedder call for the document and every section, five rows.
	if fake.calls != 1 {
		t.Errorf("embedder called %d times, want 1", fake.calls)
	}
	if n := countRows(t, `SELECT count(*) FROM substrate_embeddings WHERE entry_id = $1 AND model = 'fake-embed-1'`, e.ID); n != 5 {
		t.Errorf("got %d vectors, want 5 (document + 4 sections)", n)
	}
	if n := countRows(t, `SELECT count(*) FROM substrate_embeddings WHERE entry_id = $1 AND section = 0`, e.ID); n != 1 {
		t.Errorf("document vector rows = %d", n)
	}
	if n := countRows(t, `SELECT count(*) FROM substrate_sections WHERE entry_id = $1 AND splitter_version = $2 AND budget_runes = $3`, e.ID, SplitterVersion, DefaultSectionBudgetRunes); n != 4 {
		t.Errorf("section rows under the current rule = %d", n)
	}
}

func TestKeywordOnlyWriteStillCutsSections(t *testing.T) {
	st := newTestStore(t) // no embedder
	ctx := context.Background()
	e, err := st.Put(ctx, testNS, "procedures", Entry{Title: "Containment", Body: procedureDoc})
	if err != nil {
		t.Fatal(err)
	}
	secs, err := st.Sections(ctx, testNS, "procedures", e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 4 {
		t.Fatalf("got %d sections, want 4", len(secs))
	}
	if n := countRows(t, `SELECT count(*) FROM substrate_embeddings WHERE entry_id = $1`, e.ID); n != 0 {
		t.Errorf("keyword-only store wrote %d vectors", n)
	}
}

func TestReviseRecutsSectionsAndKeepsThePriorCut(t *testing.T) {
	st := newTestStore(t, WithEmbedder(&fakeEmbedder{}))
	ctx := context.Background()
	r1, err := st.Put(ctx, testNS, "procedures", Entry{Title: "Containment", Body: procedureDoc})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := st.Revise(ctx, testNS, "procedures", r1.ID, Revision{Title: "Containment", Body: "# Only one\n\nNow a single section."})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := st.Sections(ctx, testNS, "procedures", r2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2) != 1 || s2[0].HeadingPath[0] != "Only one" {
		t.Errorf("revision's sections:\n%s", dumpSections(s2))
	}
	// Revisions are rows: the prior revision's cut stays with it.
	s1, err := st.Sections(ctx, testNS, "procedures", r1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(s1) != 4 {
		t.Errorf("prior revision lost its sections: %d", len(s1))
	}
}

func TestReindexRebuildsStaleSections(t *testing.T) {
	st := newTestStore(t, WithEmbedder(&fakeEmbedder{}))
	ctx := context.Background()
	e, err := st.Put(ctx, testNS, "procedures", Entry{Title: "Containment", Body: procedureDoc})
	if err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, `SELECT count(*) FROM substrate_sections WHERE entry_id = $1`, e.ID); n != 4 {
		t.Fatalf("sections before = %d", n)
	}

	// A smaller budget on the same model makes every stored cut stale.
	small, err := NewPostgres(testDB, testCorpora(), WithEmbedder(&fakeEmbedder{}), WithSectionBudget(120))
	if err != nil {
		t.Fatal(err)
	}
	count, err := small.Reindex(ctx, testNS)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("Reindex touched %d entries, want 1", count)
	}
	secs, err := small.Sections(ctx, testNS, "procedures", e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) <= 4 {
		t.Fatalf("a 120-rune budget should cut parts; got %d sections", len(secs))
	}
	for _, s := range secs {
		if n := len([]rune(s.Text)); n > 120 {
			t.Errorf("section %d is %d runes under a 120 budget", s.Idx, n)
		}
	}
	if n := countRows(t, `SELECT count(*) FROM substrate_sections WHERE entry_id = $1 AND budget_runes = 120`, e.ID); n != len(secs) {
		t.Errorf("rows under the new budget = %d, want %d", n, len(secs))
	}
	// Every new section has a vector, the old section vectors are gone, and
	// the document vector survived.
	if n := countRows(t, `SELECT count(*) FROM substrate_embeddings WHERE entry_id = $1 AND model = 'fake-embed-1'`, e.ID); n != 1+len(secs) {
		t.Errorf("vectors after = %d, want %d", n, 1+len(secs))
	}
	if count, err = small.Reindex(ctx, testNS); err != nil || count != 0 {
		t.Fatalf("second Reindex should be a no-op: %d, %v", count, err)
	}
}

func TestReindexEmbedsSectionsWrittenWithoutAnEmbedder(t *testing.T) {
	plain := newTestStore(t)
	ctx := context.Background()
	e, err := plain.Put(ctx, testNS, "procedures", Entry{Title: "Containment", Body: procedureDoc})
	if err != nil {
		t.Fatal(err)
	}
	withVec, err := NewPostgres(testDB, testCorpora(), WithEmbedder(&fakeEmbedder{}))
	if err != nil {
		t.Fatal(err)
	}
	if count, err := withVec.Reindex(ctx, testNS); err != nil || count != 1 {
		t.Fatalf("Reindex: %d, %v", count, err)
	}
	if n := countRows(t, `SELECT count(*) FROM substrate_embeddings WHERE entry_id = $1 AND model = 'fake-embed-1'`, e.ID); n != 5 {
		t.Errorf("vectors after Reindex = %d, want 5", n)
	}
	if count, err := withVec.Reindex(ctx, testNS); err != nil || count != 0 {
		t.Fatalf("second Reindex should be a no-op: %d, %v", count, err)
	}
}

func TestSectionsScopedToNamespaceAndCorpus(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	e, err := st.Put(ctx, testNS, "procedures", Entry{Title: "T", Body: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sections(ctx, "tenant-b", "procedures", e.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-namespace sections must not resolve: %v", err)
	}
	if _, err := st.Sections(ctx, testNS, "case-summaries", e.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-corpus sections must not resolve: %v", err)
	}
	if _, err := st.Sections(ctx, testNS, "procedures", uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown entry: %v", err)
	}
	if _, err := NewPostgres(testDB, testCorpora(), WithSectionBudget(0)); err == nil {
		t.Error("a zero section budget must be rejected")
	}
}

func TestReindexWithoutAnEmbedderRecutsSections(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	e, err := st.Put(ctx, testNS, "procedures", Entry{Title: "Containment", Body: procedureDoc})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := st.Reindex(ctx, testNS); err != nil || count != 0 {
		t.Fatalf("a current keyword-only corpus should be a no-op: %d, %v", count, err)
	}
	small, err := NewPostgres(testDB, testCorpora(), WithSectionBudget(120))
	if err != nil {
		t.Fatal(err)
	}
	if count, err := small.Reindex(ctx, testNS); err != nil || count != 1 {
		t.Fatalf("Reindex under a new budget: %d, %v", count, err)
	}
	secs, err := small.Sections(ctx, testNS, "procedures", e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) <= 4 {
		t.Errorf("sections were not re-cut: %d", len(secs))
	}
	if n := countRows(t, `SELECT count(*) FROM substrate_embeddings WHERE entry_id = $1`, e.ID); n != 0 {
		t.Errorf("a keyword-only reindex wrote %d vectors", n)
	}
}
