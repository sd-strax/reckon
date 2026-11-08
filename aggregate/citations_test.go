package aggregate

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestKnowledgeCitations_Projection: every consulted entry on a reasoning act
// becomes one row keyed by ref, investigation, act, revision and cut, nothing
// folded on write; the reads collapse to one row per investigation with the
// citing fact kept, and the counts are distinct investigations.
func TestKnowledgeCitations_Projection(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	tenantID := uuid.New()
	h := NewHandler(NewStore(testDB), InvestigationCurrentProjector{}, KnowledgeCitationProjector{}).
		WithSideStore(NewSideStore(testDB))
	invA := activeInvestigation(t, h, tenantID)
	invB := activeInvestigation(t, h, tenantID)
	t0 := time.Now().UTC().Truncate(time.Microsecond)
	env := func(id AggregateID, at time.Time) Envelope {
		return Envelope{
			AggregateID: id, TenantID: tenantID, CorrelationID: uuid.New(),
			Actor: Actor{PrincipalID: "analyst-1"}, OccurredAt: at,
		}
	}
	record := func(e Envelope, sops []ConsultedSOP, cases []ConsultedSimilarInvestigation) {
		t.Helper()
		if _, err := h.Handle(ctx, e, RecordInterpretation{
			InterpretationID: uuid.New(), InterpretationType: InterpretationPivot,
			Rationale: "consulted the corpus", ConsultedSOPs: sops, ConsultedSimilar: cases,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	const sop1, sop2, prior = "sop-1", "sop-2", "grouping--prior-1"

	// Act 1 in A: sop-1 cited at revision 1, sop-2 recalled, the prior case recalled.
	record(env(invA, t0),
		[]ConsultedSOP{
			{SOPID: sop1, Revision: 1, Cut: "1/16000", Sections: []int{2}, Cited: true},
			{SOPID: sop2, Revision: 1, Cut: "1/16000"},
		},
		[]ConsultedSimilarInvestigation{{InvestigationRef: prior, Band: "RELATED"}})
	// Act 2 in A: sop-1 again, recalled only, under revision 2 (adr/0303: one
	// procedure at two revisions is two entries, and the aggregate accepts it).
	record(env(invA, t0.Add(time.Minute)),
		[]ConsultedSOP{{SOPID: sop1, Revision: 2, Cut: "1/16000"}}, nil)
	// Act 3 in B: sop-1 recalled, never cited there.
	record(env(invB, t0.Add(2*time.Minute)),
		[]ConsultedSOP{{SOPID: sop1, Revision: 2, Cut: "1/16000"}}, nil)

	var raw int
	if err := testDB.QueryRow(`SELECT count(*) FROM knowledge_citations`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != 5 {
		t.Errorf("rows = %d; want 5 (one per consulted entry, two revisions of sop-1 in A kept apart)", raw)
	}

	rm := h.Reads()
	got, err := rm.ListKnowledgeCitations(ctx, sop1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("sop-1 consulted in %d investigations; want 2: %+v", len(got), got)
	}
	// Most recently touched first: B's act is the latest.
	if got[0].AggregateID != invB || got[0].Cited || got[0].Acts != 1 || len(got[0].Revisions) != 1 || got[0].Revisions[0] != 2 {
		t.Errorf("B row = %+v; want recalled only, one act, revision [2]", got[0])
	}
	if got[1].AggregateID != invA || !got[1].Cited || got[1].Acts != 2 || len(got[1].Revisions) != 2 || got[1].Revisions[0] != 1 || got[1].Revisions[1] != 2 {
		t.Errorf("A row = %+v; want cited (kept from act 1), two acts, revisions [1 2]", got[1])
	}
	if !got[1].LastAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("A last_at = %s; want the later act %s", got[1].LastAt, t0.Add(time.Minute))
	}
	if got[1].Title == "" || got[1].Status == "" {
		t.Errorf("A row carries no title/status from investigation_current: %+v", got[1])
	}
	cases, err := rm.ListKnowledgeCitations(ctx, prior)
	if err != nil || len(cases) != 1 || cases[0].Cited || len(cases[0].Revisions) != 0 {
		t.Errorf("prior case rows = %+v, %v; want one recalled row with no revisions", cases, err)
	}

	counts, err := rm.CitationCounts(ctx, []string{sop1, sop2, prior, "never-consulted"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]CitationCount{
		sop1:  {CitedIn: 1, RecalledIn: 1},
		sop2:  {CitedIn: 0, RecalledIn: 1},
		prior: {CitedIn: 0, RecalledIn: 1},
	}
	if len(counts) != len(want) {
		t.Errorf("counts = %+v; want %+v", counts, want)
	}
	for ref, w := range want {
		if counts[ref] != w {
			t.Errorf("counts[%s] = %+v; want %+v", ref, counts[ref], w)
		}
	}
	if empty, err := rm.CitationCounts(ctx, nil); err != nil || len(empty) != 0 {
		t.Errorf("counts over no refs = %v, %v; want empty", empty, err)
	}

	// Reset empties the projection for replay.
	tx, err := testDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := (KnowledgeCitationProjector{}).Reset(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := testDB.QueryRow(`SELECT count(*) FROM knowledge_citations`).Scan(&raw); err != nil || raw != 0 {
		t.Errorf("after Reset rows = %d, %v; want 0", raw, err)
	}
}

// TestKnowledgeCitations_IgnoresOtherEvents: an act with nothing consulted and
// every non-interpretation event write no rows.
func TestKnowledgeCitations_IgnoresOtherEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	tenantID := uuid.New()
	h := NewHandler(NewStore(testDB), InvestigationCurrentProjector{}, KnowledgeCitationProjector{}).
		WithSideStore(NewSideStore(testDB))
	inv := activeInvestigation(t, h, tenantID)
	e := Envelope{AggregateID: inv, TenantID: tenantID, CorrelationID: uuid.New(), Actor: Actor{PrincipalID: "analyst-1"}, OccurredAt: time.Now().UTC()}
	if _, err := h.Handle(context.Background(), e, RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationPivot, Rationale: "no knowledge",
	}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := testDB.QueryRow(`SELECT count(*) FROM knowledge_citations`).Scan(&n); err != nil || n != 0 {
		t.Errorf("rows = %d, %v; want 0", n, err)
	}
}
