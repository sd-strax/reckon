package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestCandidateStore: create → list NEW → accept (revisable) / discard with a
// reason; a discarded candidate cannot be accepted and a decided one cannot be
// discarded (adr/0112).
func TestCandidateStore(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	if !testReady {
		t.Skip("embedded postgres not available")
	}
	if _, err := testDB.Exec(`TRUNCATE procedure_candidates`); err != nil {
		t.Fatal(err)
	}
	s := NewCandidateStore(testDB)
	ctx := context.Background()
	inv := uuid.New()
	a, err := s.Create(ctx, Candidate{TenantID: testTenant, InvestigationID: inv, Title: "Contain a host", Body: "# Steps\n\nIsolate.", Tags: []string{"containment"}, Recommendation: "isolate", DraftedBy: "analyst-1", DraftedByName: "Analyst One", GeneratorModel: "model-x"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(ctx, Candidate{TenantID: testTenant, InvestigationID: inv, Title: "Reset an account", Body: "Reset.", DraftedBy: "analyst-1", DraftedAt: a.DraftedAt.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := s.List(ctx, testTenant, CandidateNew)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh) != 2 || fresh[0].ID != b.ID || fresh[0].Status != CandidateNew || len(fresh[1].Tags) != 1 {
		t.Errorf("new = %+v", fresh)
	}
	if n, _ := s.CountNew(ctx, testTenant); n != 2 {
		t.Errorf("count new = %d", n)
	}
	sop := uuid.New()
	acc, err := s.Accept(ctx, testTenant, a.ID, sop, "# Steps\n\nIsolate, then preserve.", "analyst-2", "Analyst Two", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if acc.Status != CandidateAccepted || acc.AcceptedSOPID == nil || *acc.AcceptedSOPID != sop || acc.Body != "# Steps\n\nIsolate, then preserve." || acc.DecidedByName != "Analyst Two" {
		t.Errorf("accepted = %+v", acc)
	}
	// Accepting again revises the same procedure: legal.
	if _, err := s.Accept(ctx, testTenant, a.ID, sop, "again", "analyst-2", "", time.Now()); err != nil {
		t.Errorf("re-accept: %v", err)
	}
	if _, err := s.Discard(ctx, testTenant, a.ID, "no", "analyst-2", "", time.Now()); !errors.Is(err, ErrCandidateDecided) {
		t.Errorf("discard an accepted candidate = %v; want ErrCandidateDecided", err)
	}
	if _, err := s.Discard(ctx, testTenant, b.ID, "", "analyst-2", "", time.Now()); err == nil {
		t.Error("discard without a reason accepted")
	}
	dis, err := s.Discard(ctx, testTenant, b.ID, "an existing procedure covers it", "analyst-2", "Analyst Two", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if dis.Status != CandidateDiscarded || dis.DiscardReason != "an existing procedure covers it" {
		t.Errorf("discarded = %+v", dis)
	}
	if _, err := s.Accept(ctx, testTenant, b.ID, sop, "x", "analyst-2", "", time.Now()); !errors.Is(err, ErrCandidateDecided) {
		t.Errorf("accept a discarded candidate = %v; want ErrCandidateDecided", err)
	}
	if n, _ := s.CountNew(ctx, testTenant); n != 0 {
		t.Errorf("count new after decisions = %d", n)
	}
	if _, err := s.Get(ctx, testTenant, uuid.New()); !errors.Is(err, ErrCandidateNotFound) {
		t.Errorf("missing = %v", err)
	}
}
