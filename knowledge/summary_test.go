package knowledge

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/knowledge/substrate"
)

// TestWriteSummary covers the DERIVED-corpus mapping and re-derivation-as-
// revision idempotency (design/06 §3.1).
func TestWriteSummary(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()

	sum := Summary{
		InvestigationRef:   "grouping--" + uuid.New().String(),
		Title:              "RDP lateral movement",
		ConclusionAt:       time.Now().UTC(),
		SummaryText:        "Investigation concluded. Verdict: MALICIOUS. Techniques: T1021.001.",
		ExtractorVersion:   "1",
		SeedKind:           "entity",
		SeedValue:          "WIN-FILE01",
		Techniques:         []string{"T1021.001", "T1078"},
		ActionsTaken:       []SummaryAction{{ActionType: "host.isolate", TargetCount: 1, Outcome: "SUCCEEDED"}},
		ConclusionOutcome:  "succeeded",
		DurationHours:      6,
		VerdictDisposition: "MALICIOUS",
		VerdictRationale:   "Confirmed hands-on-keyboard.",
	}

	id1, err := s.WriteSummary(ctx, testTenant, sum)
	if err != nil {
		t.Fatal(err)
	}

	// It lands in case-summaries as a DERIVED entry with provenance + the
	// structured meta, and is recallable — with the reserved source tag hidden.
	res, err := s.sub.Recall(ctx, testTenant.String(), substrate.RecallQuery{
		Corpus: CorpusCaseSummaries, Query: "lateral movement", // present in the title
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("summary not recallable: %+v", res)
	}
	// The facets are hard-filterable (the reserved source tag also rides here at
	// the substrate level; the facade strips it on the recall path).
	hit := res.Results[0]
	if !hasTag(hit.Tags, "T1021.001") || !hasTag(hit.Tags, "seed:entity") || !hasTag(hit.Tags, "disposition:MALICIOUS") {
		t.Errorf("expected facets missing from tags: %v", hit.Tags)
	}
	got, err := s.sub.Get(ctx, testTenant.String(), CorpusCaseSummaries, id1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provenance == nil || got.Provenance.Producer != "case-summarizer" || got.Provenance.SourceRef != sum.InvestigationRef {
		t.Errorf("provenance wrong: %+v", got.Provenance)
	}
	if got.Meta["conclusion_outcome"] != "succeeded" {
		t.Errorf("structured meta not carried: %+v", got.Meta)
	}

	// Re-derivation of the SAME source is a revision, not a duplicate.
	sum.SummaryText = "Re-derived: MALICIOUS, more detail."
	id2, err := s.WriteSummary(ctx, testTenant, sum)
	if err != nil {
		t.Fatal(err)
	}
	if id2 == id1 {
		t.Error("re-derivation should produce a new revision id")
	}
	list, err := s.sub.List(ctx, testTenant.String(), CorpusCaseSummaries, substrate.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("re-derivation duplicated the summary: %d current entries", len(list))
	}
	if list[0].Revision != 2 {
		t.Errorf("re-derivation should be revision 2, got %d", list[0].Revision)
	}
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// TestEnrichSummary covers the client-side narrative tier (design/06 §3.2):
// the enrichment revises the structured baseline — body replaced, structured
// tags/meta carried, generator model stamped — and fails honestly when no
// baseline exists yet.
func TestEnrichSummary(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()

	ref := "grouping--" + uuid.New().String()
	if _, err := s.WriteSummary(ctx, testTenant, Summary{
		InvestigationRef: ref, Title: "RDP lateral movement",
		SummaryText: "Deterministic baseline.", ExtractorVersion: "1",
		SeedKind: "entity", Techniques: []string{"T1021.001"}, ConclusionOutcome: "succeeded",
	}); err != nil {
		t.Fatal(err)
	}

	narrative := "The analyst confirmed hands-on-keyboard lateral movement over RDP from WIN-FILE01 and isolated the host."
	id, err := s.EnrichSummary(ctx, testTenant, ref, narrative, "claude-opus-4-8")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.sub.Get(ctx, testTenant.String(), CorpusCaseSummaries, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != narrative {
		t.Errorf("body not replaced by the narrative: %q", got.Body)
	}
	if got.Revision != 2 {
		t.Errorf("enrichment should be a revision, got rev %d", got.Revision)
	}
	if got.Provenance == nil || got.Provenance.GeneratorModel != "claude-opus-4-8" || got.Provenance.Producer != "case-summarizer" {
		t.Errorf("provenance stamp wrong: %+v", got.Provenance)
	}
	if !hasTag(got.Tags, "T1021.001") || got.Meta["conclusion_outcome"] != "succeeded" {
		t.Errorf("structured facets must carry over: tags=%v meta=%v", got.Tags, got.Meta)
	}
	// Exactly one current summary — the enrichment superseded the baseline.
	list, err := s.sub.List(ctx, testTenant.String(), CorpusCaseSummaries, substrate.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("enrichment duplicated the summary: %d current entries", len(list))
	}

	// No baseline yet → ErrNoSummary (the client's retry-then-give-up signal).
	if _, err := s.EnrichSummary(ctx, testTenant, "grouping--"+uuid.New().String(), "n", "m"); !errors.Is(err, ErrNoSummary) {
		t.Errorf("want ErrNoSummary, got %v", err)
	}
}

// TestRecallSimilar covers the similarity recall over case-summaries: the
// source investigation ref is recovered from the reserved tag, reserved tags
// are stripped, and bands ride through (via the vector backend).
func TestRecallSimilar(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	emb := &stubEmbedder{vec: map[string][]float32{}}
	s := reset(t, substrate.WithEmbedder(emb))
	ctx := context.Background()

	// Two prior cases on orthogonal axes; the query aligns with the RDP one.
	ref1 := "grouping--" + uuid.New().String()
	ref2 := "grouping--" + uuid.New().String()
	emb.set("RDP lateral movement\n\nAttacker pivoted via RDP from WIN-FILE01.", 1, 0)
	emb.set("Phishing payload\n\nUser opened a malicious attachment.", 0, 1)
	emb.set("host pivoted over remote desktop", 1, 0)

	mk := func(ref, title, body string, techniques ...string) {
		if _, err := s.WriteSummary(ctx, testTenant, Summary{
			InvestigationRef: ref, Title: title, SummaryText: body, ExtractorVersion: "1",
			SeedKind: "entity", Techniques: techniques, VerdictDisposition: "MALICIOUS",
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk(ref1, "RDP lateral movement", "Attacker pivoted via RDP from WIN-FILE01.", "T1021.001")
	mk(ref2, "Phishing payload", "User opened a malicious attachment.", "T1566")

	res, err := s.RecallSimilar(ctx, testTenant, SimilarRequest{Query: "host pivoted over remote desktop"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Coverage != CoverageComplete || len(res.Results) == 0 {
		t.Fatalf("expected a similar case, got %+v", res)
	}
	top := res.Results[0]
	if top.InvestigationRef != ref1 {
		t.Errorf("top match ref = %q; want %q (the RDP case)", top.InvestigationRef, ref1)
	}
	if top.Band != string(substrate.BandNearDuplicate) {
		t.Errorf("aligned query should band NEAR_DUPLICATE, got %q", top.Band)
	}
	if top.Backend != "vector-cosine/1/stub-embed" {
		t.Errorf("backend = %q", top.Backend)
	}
	for _, tag := range top.Tags {
		if len(tag) >= 2 && tag[:2] == "__" {
			t.Errorf("reserved tag leaked into similar recall: %q", tag)
		}
	}
	if !hasTag(top.Tags, "T1021.001") {
		t.Errorf("expected technique facet in tags: %v", top.Tags)
	}
}

// TestSummaryByInvestigation: the inspector's read of one summary by its
// source investigation (design/ui 08 §8.6.1) returns the structured fields
// the producer recorded and the current body — the narrative once enriched —
// with reserved tags stripped; a ref with no summary is ErrNoSummary.
func TestSummaryByInvestigation(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()
	ref := "grouping--" + uuid.New().String()
	sum := Summary{
		InvestigationRef: ref, Title: "Prior case", ConclusionAt: time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SummaryText: "baseline body", ExtractorVersion: "1",
		SeedKind: "entity", SeedValue: "HOST-A", Techniques: []string{"T1021.001"}, PrimaryEntities: []string{"HOST-A", "user-a"},
		ActionsTaken:      []SummaryAction{{ActionType: "host.isolate", TargetCount: 1, Outcome: "SUCCEEDED"}},
		ConclusionOutcome: "succeeded", DurationHours: 6.5, VerdictDisposition: "MALICIOUS", VerdictRationale: "confirmed",
	}
	if _, err := s.WriteSummary(ctx, testTenant, sum); err != nil {
		t.Fatal(err)
	}
	v, err := s.SummaryByInvestigation(ctx, testTenant, ref)
	if err != nil {
		t.Fatal(err)
	}
	if v.InvestigationRef != ref || v.Title != "Prior case" || v.Body != "baseline body" || v.GeneratorModel != "" {
		t.Errorf("view = %+v", v)
	}
	if v.ConclusionAt != "2026-05-14T10:00:00Z" || v.SeedValue != "HOST-A" || v.DurationHours != 6.5 || v.VerdictDisposition != "MALICIOUS" {
		t.Errorf("structured fields = %+v", v)
	}
	if len(v.Techniques) != 1 || len(v.PrimaryEntities) != 2 || len(v.ActionsTaken) != 1 || v.ActionsTaken[0].Outcome != "SUCCEEDED" || v.ActionsTaken[0].TargetCount != 1 {
		t.Errorf("lists = %+v", v)
	}
	for _, tag := range v.Tags {
		if strings.HasPrefix(tag, summarySourcePrefix) {
			t.Errorf("reserved tag leaked: %v", v.Tags)
		}
	}
	// The client tier's narrative replaces the body on the next read.
	if _, err := s.EnrichSummary(ctx, testTenant, ref, "the narrative", "model-x"); err != nil {
		t.Fatal(err)
	}
	v, err = s.SummaryByInvestigation(ctx, testTenant, ref)
	if err != nil {
		t.Fatal(err)
	}
	if v.Body != "the narrative" || v.GeneratorModel != "model-x" || v.Revision != 2 {
		t.Errorf("enriched view = %+v", v)
	}
	if _, err := s.SummaryByInvestigation(ctx, testTenant, "grouping--none"); !errors.Is(err, ErrNoSummary) {
		t.Errorf("missing = %v; want ErrNoSummary", err)
	}
}

// TestPriorByEntity: the exact lookup finds every concluded investigation that
// recorded the entity, case-insensitively and most recent first, and is EMPTY
// for an entity none recorded (adr/0163).
func TestPriorByEntity(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()
	older, newer, other := "grouping--"+uuid.New().String(), "grouping--"+uuid.New().String(), "grouping--"+uuid.New().String()
	mk := func(ref string, at time.Time, entities ...string) {
		if _, err := s.WriteSummary(ctx, testTenant, Summary{
			InvestigationRef: ref, Title: "case " + ref, SummaryText: "body", ExtractorVersion: "2",
			ConclusionAt: at, PrimaryEntities: entities, VerdictDisposition: "MALICIOUS",
		}); err != nil {
			t.Fatal(err)
		}
	}
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	mk(older, day, "WIN-FILE01")
	mk(newer, day.AddDate(0, 0, 7), "win-file01", "CONTOSO\\svc_backup")
	mk(other, day, "WORKSTATION-22")

	res, err := s.PriorByEntity(ctx, testTenant, PriorRequest{Entity: " Win-File01 "})
	if err != nil {
		t.Fatal(err)
	}
	if res.Coverage != "COMPLETE" || len(res.Investigations) != 2 {
		t.Fatalf("want two prior investigations, got %+v", res)
	}
	if res.Investigations[0].InvestigationRef != newer || res.Investigations[1].InvestigationRef != older {
		t.Errorf("order = %s, %s; want newest first", res.Investigations[0].InvestigationRef, res.Investigations[1].InvestigationRef)
	}
	none, err := s.PriorByEntity(ctx, testTenant, PriorRequest{Entity: "DC-99"})
	if err != nil {
		t.Fatal(err)
	}
	if none.Coverage != "EMPTY" || len(none.Investigations) != 0 {
		t.Errorf("unknown entity: %+v", none)
	}
	if _, err := s.PriorByEntity(ctx, testTenant, PriorRequest{}); err == nil {
		t.Error("an empty entity should be refused")
	}
}

// TestRecallSimilar_EmptyUnderFilter: a recall emptied by a filter tag no
// summary carries says so, instead of reading as "nothing alike" (adr/0163).
func TestRecallSimilar_EmptyUnderFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()
	if _, err := s.WriteSummary(ctx, testTenant, Summary{
		InvestigationRef: "grouping--" + uuid.New().String(), Title: "RDP lateral movement", SummaryText: "Attacker pivoted via RDP.",
		ExtractorVersion: "2", SeedKind: "case",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := s.RecallSimilar(ctx, testTenant, SimilarRequest{Query: "lateral movement", Tags: []string{"seed:WIN-FILE01"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Coverage != CoverageEmptyUnderFilter || len(res.UnmatchedTags) != 1 || res.UnmatchedTags[0] != "seed:WIN-FILE01" {
		t.Errorf("want EMPTY_UNDER_FILTER naming the tag, got %+v", res)
	}
	ok, err := s.RecallSimilar(ctx, testTenant, SimilarRequest{Query: "lateral movement", Tags: []string{"seed:case"}})
	if err != nil {
		t.Fatal(err)
	}
	if ok.Coverage == CoverageEmptyUnderFilter {
		t.Errorf("a filter every tag of which exists is not flagged: %+v", ok)
	}
}

// TestListSummaries: the library lists past cases newest first (adr/0129).
func TestListSummaries(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i, title := range []string{"older", "newer"} {
		if _, err := s.WriteSummary(ctx, testTenant, Summary{
			InvestigationRef: "grouping--" + uuid.New().String(), Title: title, SummaryText: "b", ExtractorVersion: "2",
			ConclusionAt: day.AddDate(0, 0, i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListSummaries(ctx, testTenant, SummaryFilter{Limit: 10})
	if err != nil || len(list.Summaries) != 2 || list.Summaries[0].Title != "newer" || list.Total != 2 {
		t.Fatalf("list = %+v %v; want newer first", list, err)
	}
}

// TestListSummaries_Filtered: technique, disposition and the conclusion range
// narrow the listing (adr/0107); techniques are ANDed, the disposition is the
// verdict's word in any case, the range is [after, before), total counts
// before the limit, and the facets count only what is present.
func TestListSummaries_Filtered(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		title       string
		techniques  []string
		disposition string
		at          time.Time
	}{
		{"rdp", []string{"T1021.001", "T1078"}, "MALICIOUS", day(1)},
		{"valid account", []string{"T1078"}, "SUSPICIOUS", day(5)},
		{"benign admin", []string{"T1021.001"}, "BENIGN", day(9)},
	} {
		if _, err := s.WriteSummary(ctx, testTenant, Summary{
			InvestigationRef: "grouping--" + uuid.New().String(), Title: c.title, SummaryText: "b", ExtractorVersion: "2",
			ConclusionAt: c.at, Techniques: c.techniques, VerdictDisposition: c.disposition,
		}); err != nil {
			t.Fatal(err)
		}
	}
	titles := func(f SummaryFilter) ([]string, int) {
		t.Helper()
		got, err := s.ListSummaries(ctx, testTenant, f)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(got.Summaries))
		for _, v := range got.Summaries {
			out = append(out, v.Title)
		}
		return out, got.Total
	}
	check := func(name string, f SummaryFilter, want []string, total int) {
		t.Helper()
		got, n := titles(f)
		if !reflect.DeepEqual(got, want) || n != total {
			t.Errorf("%s: %v (total %d); want %v (total %d)", name, got, n, want, total)
		}
	}
	check("technique", SummaryFilter{Techniques: []string{"T1078"}}, []string{"valid account", "rdp"}, 2)
	check("two techniques AND", SummaryFilter{Techniques: []string{"T1078", "T1021.001"}}, []string{"rdp"}, 1)
	check("disposition any case", SummaryFilter{Disposition: "benign"}, []string{"benign admin"}, 1)
	check("range [after, before)", SummaryFilter{ConcludedAfter: day(1), ConcludedBefore: day(9)}, []string{"valid account", "rdp"}, 2)
	check("after only", SummaryFilter{ConcludedAfter: day(2)}, []string{"benign admin", "valid account"}, 2)
	check("limit keeps the total", SummaryFilter{Limit: 1}, []string{"benign admin"}, 3)
	check("nothing matches", SummaryFilter{Techniques: []string{"T9999"}}, []string{}, 0)

	facets, err := s.CaseSummaryFacets(ctx, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	wantT := []FacetCount{{"T1021.001", 2}, {"T1078", 2}}
	wantD := []FacetCount{{"BENIGN", 1}, {"MALICIOUS", 1}, {"SUSPICIOUS", 1}}
	if !reflect.DeepEqual(facets.Techniques, wantT) || !reflect.DeepEqual(facets.Dispositions, wantD) {
		t.Errorf("facets = %+v; want %v %v", facets, wantT, wantD)
	}

	// A summary written without a conclusion time has none: it lists last
	// with an empty time and never matches a range.
	if _, err := s.WriteSummary(ctx, testTenant, Summary{
		InvestigationRef: "grouping--" + uuid.New().String(), Title: "undated", SummaryText: "b", ExtractorVersion: "2", VerdictDisposition: "benign",
	}); err != nil {
		t.Fatal(err)
	}
	check("before only excludes the undated", SummaryFilter{ConcludedBefore: day(30)}, []string{"benign admin", "valid account", "rdp"}, 3)
	all, err := s.ListSummaries(ctx, testTenant, SummaryFilter{})
	if err != nil || all.Summaries[len(all.Summaries)-1].Title != "undated" || all.Summaries[len(all.Summaries)-1].ConclusionAt != "" {
		t.Errorf("undated summary: %v %+v", err, all.Summaries)
	}
	// The disposition matches whatever case the entry was written in.
	check("lower-case written, upper-case asked", SummaryFilter{Disposition: "BENIGN"}, []string{"benign admin", "undated"}, 2)
}
