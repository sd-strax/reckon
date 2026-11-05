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

func TestBlankBodyIsRejectedAndNeverIndexed(t *testing.T) {
	st := newTestStore(t, WithEmbedder(sectionEmbedder()))
	ctx := context.Background()
	if _, err := st.Put(ctx, testNS, "procedures", Entry{Title: "Blank", Body: " \n\n\t\n"}); !errors.Is(err, ErrBlankBody) {
		t.Fatalf("whitespace body accepted: %v", err)
	}
	e := seedProcedureAndDistractor(t, st)
	if _, err := st.Revise(ctx, testNS, "procedures", e.ID, Revision{Title: "Blank", Body: "   "}); !errors.Is(err, ErrBlankBody) {
		t.Fatalf("whitespace revision accepted: %v", err)
	}
	// Recall over the corpus is unaffected: nothing unrankable got in.
	if _, err := st.Recall(ctx, testNS, RecallQuery{Corpus: "procedures", Query: "how do I isolate the destination host?"}); err != nil {
		t.Fatal(err)
	}
	// A row made blank behind the store's back is reported by Reindex, never
	// silently re-cut to nothing.
	if _, err := testDB.Exec(`UPDATE substrate_entries SET body = ' ' WHERE id = $1`, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(`DELETE FROM substrate_sections WHERE entry_id = $1`, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Reindex(ctx, testNS); !errors.Is(err, ErrBlankBody) {
		t.Fatalf("Reindex over a blank body: %v; want ErrBlankBody", err)
	}
}

func TestReindexNewModelKeepsOtherModelsSectionVectors(t *testing.T) {
	a := newTestStore(t, WithEmbedder(&fakeEmbedder{model: "model-a"}))
	ctx := context.Background()
	e, err := a.Put(ctx, testNS, "procedures", Entry{Title: "Containment", Body: procedureDoc})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewPostgres(testDB, testCorpora(), WithEmbedder(&fakeEmbedder{model: "model-b"}))
	if err != nil {
		t.Fatal(err)
	}
	if count, err := b.Reindex(ctx, testNS); err != nil || count != 1 {
		t.Fatalf("Reindex under model-b: %d, %v", count, err)
	}
	for _, model := range []string{"model-a", "model-b"} {
		if n := countRows(t, `SELECT count(*) FROM substrate_embeddings WHERE entry_id = $1 AND model = $2`, e.ID, model); n != 5 {
			t.Errorf("%s has %d vectors after the backfill, want 5 (document + 4 sections)", model, n)
		}
	}
	// The cut was kept, not rewritten.
	if n := countRows(t, `SELECT count(*) FROM substrate_sections WHERE entry_id = $1`, e.ID); n != 4 {
		t.Errorf("sections after backfill = %d", n)
	}
	// A reader still on model-a ranks as before.
	if _, err := a.Recall(ctx, testNS, RecallQuery{Corpus: "procedures", Query: "anything"}); err != nil {
		t.Errorf("model-a recall after a model-b backfill: %v", err)
	}
	if count, err := b.Reindex(ctx, testNS); err != nil || count != 0 {
		t.Fatalf("second Reindex should be a no-op: %d, %v", count, err)
	}
}

func TestKeywordRecallMissingSectionsFailLoudly(t *testing.T) {
	st := newTestStore(t) // keyword backend
	e := seedProcedureAndDistractor(t, st)
	ctx := context.Background()
	if _, err := testDB.Exec(`DELETE FROM substrate_sections WHERE entry_id = $1`, e.ID); err != nil {
		t.Fatal(err)
	}
	// A query that would match the un-cut entry's title must not answer EMPTY.
	_, err := st.Recall(ctx, testNS, RecallQuery{Corpus: "procedures", Query: "containment"})
	if !errors.Is(err, ErrNoSections) {
		t.Fatalf("want ErrNoSections, got %v", err)
	}
	// Under a filter that excludes the un-cut entry, recall proceeds.
	res, err := st.Recall(ctx, testNS, RecallQuery{Corpus: "procedures", Query: "phishing", Tags: []string{"phishing"}})
	if err != nil || len(res.Results) != 1 {
		t.Fatalf("filtered recall: %v, %d results", err, len(res.Results))
	}
}
