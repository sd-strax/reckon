package substrate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// sectionEmbedder maps each section of the procedure document, the document
// itself and two queries onto controlled axes, so section ranking is exact.
// The "Do not" section leans toward the containment axis (cos 0.8) to show
// SECTION unit returning two pieces of one document.
func sectionEmbedder() *fakeEmbedder {
	const title = "Containment"
	secs := SplitSections(procedureDoc, DefaultSectionBudgetRunes)
	return &fakeEmbedder{byText: map[string][]float32{
		embedText(title, procedureDoc):               {0.5, 0.5, 0.5, 0.5},
		sectionEmbedText(title, secs[0]):             {1, 0, 0, 0},
		sectionEmbedText(title, secs[1]):             {0, 1, 0, 0},
		sectionEmbedText(title, secs[2]):             {0, 0, 1, 0},
		sectionEmbedText(title, secs[3]):             {0, 0.6, 0.8, 0},
		"how do I isolate the destination host?":     {0, 0, 1, 0},
		"a draft that reads like the whole document": {0.5, 0.5, 0.5, 0.5},
	}}
}

func seedProcedureAndDistractor(t *testing.T, st *Postgres) Entry {
	t.Helper()
	ctx := context.Background()
	e, err := st.Put(ctx, testNS, "procedures", Entry{Title: "Containment", Body: procedureDoc, Tags: []string{"containment"}})
	if err != nil {
		t.Fatal(err)
	}
	// Unmapped texts embed on the fourth axis: orthogonal to every query.
	if _, err := st.Put(ctx, testNS, "procedures", Entry{Title: "Phishing triage", Body: "Check headers and detonate attachments.", Tags: []string{"phishing"}}); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestSectionRecall_EntryUnitCarriesTheBestSection(t *testing.T) {
	st := newTestStore(t, WithEmbedder(sectionEmbedder()))
	e := seedProcedureAndDistractor(t, st)

	res, err := st.Recall(context.Background(), testNS, RecallQuery{Corpus: "procedures", Query: "how do I isolate the destination host?"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Coverage != CoverageComplete || len(res.Results) != 2 {
		t.Fatalf("coverage %s, %d results", res.Coverage, len(res.Results))
	}
	h := res.Results[0]
	if h.EntryID != e.ID {
		t.Fatalf("first hit is %q, want the procedure", h.Title)
	}
	if h.Section.Idx != 3 || h.Section.Of != 4 || h.Section.Location != "Lateral Movement — Containment > Containment" {
		t.Errorf("section ref %+v", h.Section)
	}
	if !strings.HasPrefix(h.Excerpt, "## Containment") || !strings.Contains(h.Excerpt, "Isolate the destination host") || h.Truncated {
		t.Errorf("excerpt is not the containment section whole:\n%s", h.Excerpt)
	}
	if strings.Contains(h.Excerpt, "Scope:") {
		t.Error("excerpt carries the preamble; the opening of the body must not be what the model reads")
	}
	if len(h.Outline) != 4 || h.Outline[1] != "Lateral Movement — Containment > Triage" {
		t.Errorf("outline %q", h.Outline)
	}
	if !strings.Contains(h.MatchRationale, `best section "Lateral Movement — Containment > Containment" (3 of 4)`) {
		t.Errorf("rationale %q", h.MatchRationale)
	}
	if h.Score < 0.99 {
		t.Errorf("score %.3f: the best section is a unit vector on the query's axis", h.Score)
	}
	// The distractor's single section embeds as the document and scores 0.
	if d := res.Results[1]; d.Section.Idx != 1 || d.Section.Of != 1 || d.Excerpt != "Check headers and detonate attachments." {
		t.Errorf("single-section hit %+v", d.Section)
	}
}

func TestSectionRecall_SectionUnitMayRepeatAnEntry(t *testing.T) {
	st := newTestStore(t, WithEmbedder(sectionEmbedder()))
	e := seedProcedureAndDistractor(t, st)

	res, err := st.Recall(context.Background(), testNS, RecallQuery{Corpus: "procedures", Unit: UnitSection, Query: "how do I isolate the destination host?", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 2 {
		t.Fatalf("%d results", len(res.Results))
	}
	for i, want := range []int{3, 4} {
		h := res.Results[i]
		if h.EntryID != e.ID || h.Section.Idx != want {
			t.Errorf("hit %d: entry %s section %d, want the procedure's section %d", i, h.Title, h.Section.Idx, want)
		}
		if !strings.Contains(h.MatchRationale, "; section ") {
			t.Errorf("hit %d rationale %q", i, h.MatchRationale)
		}
	}
	if res.Results[0].Score <= res.Results[1].Score {
		t.Error("scores must order the response")
	}
}

func TestSectionRecall_ExcerptBudgetCutsAndSaysSo(t *testing.T) {
	st := newTestStore(t, WithEmbedder(sectionEmbedder()))
	seedProcedureAndDistractor(t, st)

	res, err := st.Recall(context.Background(), testNS, RecallQuery{Corpus: "procedures", Query: "how do I isolate the destination host?", ExcerptBudget: 20, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	h := res.Results[0]
	if !h.Truncated || len([]rune(h.Excerpt)) != 21 || !strings.HasSuffix(h.Excerpt, "…") {
		t.Errorf("excerpt %q truncated=%v", h.Excerpt, h.Truncated)
	}
	// A budget over the store's section budget is the section budget.
	res, err = st.Recall(context.Background(), testNS, RecallQuery{Corpus: "procedures", Query: "how do I isolate the destination host?", ExcerptBudget: 1 << 30, Limit: 1})
	if err != nil || res.Results[0].Truncated {
		t.Errorf("oversized budget: %v truncated=%v", err, res.Results[0].Truncated)
	}
}

func TestSectionRecall_SimilarityComparesDocuments(t *testing.T) {
	st := newTestStore(t, WithEmbedder(sectionEmbedder()))
	e := seedProcedureAndDistractor(t, st)

	res, err := st.Recall(context.Background(), testNS, RecallQuery{Corpus: "procedures", Mode: ModeSimilarity, Unit: UnitSection, Query: "a draft that reads like the whole document"})
	if err != nil {
		t.Fatal(err)
	}
	h := res.Results[0]
	if h.EntryID != e.ID || h.Band != BandNearDuplicate {
		t.Fatalf("hit %s band %s", h.Title, h.Band)
	}
	// The unit is ignored; the excerpt is the first section with the outline.
	if h.Section.Idx != 1 || len(h.Outline) != 4 || !strings.HasPrefix(h.Excerpt, "# Lateral Movement") {
		t.Errorf("similarity hit section %+v excerpt %q", h.Section, h.Excerpt)
	}
}

func TestSectionRecall_UnknownUnitRejected(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Recall(context.Background(), testNS, RecallQuery{Corpus: "procedures", Query: "x", Unit: "PARAGRAPH"}); err == nil {
		t.Fatal("unknown unit must be rejected")
	}
}

func TestSectionRecall_MissingSectionsFailLoudly(t *testing.T) {
	st := newTestStore(t, WithEmbedder(sectionEmbedder()))
	e := seedProcedureAndDistractor(t, st)
	if _, err := testDB.Exec(`DELETE FROM substrate_sections WHERE entry_id = $1`, e.ID); err != nil {
		t.Fatal(err)
	}
	_, err := st.Recall(context.Background(), testNS, RecallQuery{Corpus: "procedures", Query: "how do I isolate the destination host?"})
	if !errors.Is(err, ErrNoSections) {
		t.Fatalf("want ErrNoSections, got %v", err)
	}
	// Reindex rebuilds the cut and its vectors; recall works again.
	if _, err := st.Reindex(context.Background(), testNS); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Recall(context.Background(), testNS, RecallQuery{Corpus: "procedures", Query: "how do I isolate the destination host?"}); err != nil {
		t.Fatal(err)
	}
	// A section without a vector under the model is the embedding error.
	if _, err := testDB.Exec(`DELETE FROM substrate_embeddings WHERE entry_id = $1 AND section = 3`, e.ID); err != nil {
		t.Fatal(err)
	}
	_, err = st.Recall(context.Background(), testNS, RecallQuery{Corpus: "procedures", Query: "how do I isolate the destination host?"})
	if !errors.Is(err, ErrNoEmbedding) {
		t.Fatalf("want ErrNoEmbedding, got %v", err)
	}
}

func TestKeywordSectionRecall(t *testing.T) {
	st := newTestStore(t) // no embedder → keyword backend over sections
	e := seedProcedureAndDistractor(t, st)
	ctx := context.Background()

	res, err := st.Recall(ctx, testNS, RecallQuery{Corpus: "procedures", Query: "isolate destination host"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ranker.Backend != "pg-fts" || len(res.Results) != 1 {
		t.Fatalf("ranker %+v, %d results", res.Ranker, len(res.Results))
	}
	h := res.Results[0]
	if h.EntryID != e.ID || h.Section.Idx != 3 || h.Section.Of != 4 || len(h.Outline) != 4 {
		t.Errorf("ENTRY unit hit: section %+v outline %d", h.Section, len(h.Outline))
	}
	if !strings.HasPrefix(h.Excerpt, "## Containment") || !strings.Contains(h.MatchRationale, "best section") {
		t.Errorf("excerpt %q rationale %q", h.Excerpt, h.MatchRationale)
	}

	// SECTION unit: the containment and the "do not" sections both mention
	// the destination; two hits from one entry.
	res, err = st.Recall(ctx, testNS, RecallQuery{Corpus: "procedures", Unit: UnitSection, Query: "destination"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) < 2 || res.Results[0].EntryID != e.ID || res.Results[1].EntryID != e.ID {
		t.Fatalf("SECTION unit: %d results", len(res.Results))
	}
	if res.Results[0].Section.Idx == res.Results[1].Section.Idx {
		t.Error("SECTION unit returned one section twice")
	}

	// The title is still part of the keyword surface.
	res, err = st.Recall(ctx, testNS, RecallQuery{Corpus: "procedures", Query: "phishing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 || res.Results[0].Title != "Phishing triage" {
		t.Errorf("title words must match: %+v", res.Results)
	}

	// Keyword SIMILARITY still ranks entries, with the first section as excerpt.
	res, err = st.Recall(ctx, testNS, RecallQuery{Corpus: "procedures", Mode: ModeSimilarity, Query: "Isolate the destination host if hands-on-keyboard activity is confirmed."})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) == 0 || res.Results[0].Band != BandRelated || res.Results[0].Section.Idx != 1 {
		t.Errorf("keyword similarity: %+v", res.Results)
	}
}
