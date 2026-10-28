package aggregate

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestKnowledgeCitationProjector: the consulted lists on an interpretation fold
// into knowledge_citations in the same commit; distinct investigations count
// once however many acts cite; "applied" is any act with used = true; a
// replay rebuilds the same rows (adr/0109).
func TestKnowledgeCitationProjector(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	tenantID := uuid.New()
	h := NewHandler(NewStore(testDB), InvestigationCurrentProjector{}, ActionCurrentProjector{}, KnowledgeCitationProjector{})
	invA := activeInvestigation(t, h, tenantID)
	invB := activeInvestigation(t, h, tenantID)
	sop := uuid.New().String()
	pastCase := "grouping--" + uuid.New().String()
	record := func(inv uuid.UUID, used bool, withCase bool) {
		t.Helper()
		cmd := RecordInterpretation{
			InterpretationID: uuid.New(), InterpretationType: InterpretationPivot, Rationale: "consulted",
			ConsultedSOPs: []ConsultedSOP{{SOPID: sop, Title: "Runbook", Used: used}},
		}
		if withCase {
			cmd.ConsultedSimilar = []ConsultedSimilarInvestigation{{InvestigationRef: pastCase, Title: "Prior", Band: "RELATED", Used: used}}
		}
		env := newTestEnvelope("analyst-1")
		env.AggregateID, env.TenantID, env.CorrelationID, env.OccurredAt = inv, tenantID, uuid.New(), time.Now()
		if _, err := h.Handle(context.Background(), env, cmd); err != nil {
			t.Fatalf("handle: %v", err)
		}
	}
	record(invA, false, true)
	record(invA, true, false)
	record(invB, false, true)

	check := func(label string) {
		t.Helper()
		counts, err := CountKnowledgeCitations(context.Background(), testDB, tenantID, CitationKindSOP)
		if err != nil {
			t.Fatal(err)
		}
		if counts[sop] != 2 {
			t.Errorf("%s: sop cited in %d investigations; want 2 (three acts, two investigations)", label, counts[sop])
		}
		rows, err := ListKnowledgeCitations(context.Background(), testDB, tenantID, CitationKindSOP, sop)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("%s: rows = %+v; want one per investigation", label, rows)
		}
		byInv := map[AggregateID]KnowledgeCitation{}
		for _, r := range rows {
			byInv[r.AggregateID] = r
		}
		if a := byInv[invA]; !a.Applied || a.Interpretations != 2 || a.Title != "INV" || a.Status != "active" || a.LastAt.Before(a.FirstAt) {
			t.Errorf("%s: investigation A = %+v; want applied, 2 acts, title and status from investigation_current", label, a)
		}
		if b := byInv[invB]; b.Applied || b.Interpretations != 1 {
			t.Errorf("%s: investigation B = %+v; want consulted-not-applied, 1 act", label, b)
		}
		cases, err := ListKnowledgeCitations(context.Background(), testDB, tenantID, CitationKindCase, pastCase)
		if err != nil {
			t.Fatal(err)
		}
		if len(cases) != 2 || cases[0].Applied || cases[1].Applied {
			t.Errorf("%s: case citations = %+v; want two consulting investigations, none applied", label, cases)
		}
	}
	check("fold")
	if err := h.Replay(context.Background()); err != nil {
		t.Fatalf("replay: %v", err)
	}
	check("replay")
}
