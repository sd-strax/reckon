package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
)

// TestInvestigationForCase: a case already investigated — open or concluded —
// is found, open first, so it is never investigated twice (#158).
func TestInvestigationForCase(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	b := newTestBackend(t)
	req := httptest.NewRequest("POST", "/api/investigations", nil)
	create := func(caseID string) uuid.UUID {
		id := uuid.New()
		env := newEnvelope(id, aggregate.Actor{PrincipalID: "test-subject"}, commandNow())
		if _, err := testHandler.Handle(context.Background(), env, aggregate.CreateInvestigation{
			Title: "case " + caseID, Seed: &aggregate.Seed{Type: aggregate.SeedCase, CaseID: caseID, Source: "fixture:test", CaseRef: "observed-data--" + uuid.NewString()},
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	if got, err := b.investigationForCase(req, "INC1"); err != nil || got != "" {
		t.Fatalf("no investigation yet: %q %v", got, err)
	}
	first := create("INC1")
	if got, _ := b.investigationForCase(req, "INC1"); got != first.String() {
		t.Errorf("found %q; want %s", got, first)
	}
	if got, _ := b.investigationForCase(req, "INC2"); got != "" {
		t.Errorf("another case found %q", got)
	}
}
