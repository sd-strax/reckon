package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sd-strax/reckon/aggregate"
)

// TestInvestigationView_StageInputsAndStart: the list rows carry the stage
// inputs the work queue derives from (design/ui 08 §8.3: has_verdict,
// open_actions, open_notifications, the seed) and the detail carries the
// start and who opened it (adr/0077) — the §8.0 view additions.
func TestInvestigationView_StageInputsAndStart(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t) // manual fall-through: the request stays REQUESTED
	token := mintToken(t, map[string]any{"preferred_username": "opener"})
	if resp, _ := postAction(t, b, token, RequestActionBody{
		ActionType:       "host.isolate",
		Targets:          []aggregate.TargetSpec{{EntityRef: "x-host--1", ResolvedIdentifier: "WIN-A"}},
		EvidenceRefs:     []string{"observed-data--od1"},
		Rationale:        "contain",
		InvestigationRef: invID.String(),
	}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("request = %d", resp.StatusCode)
	}

	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	get := func(path string, into any) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d", path, resp.StatusCode)
		}
		_ = json.NewDecoder(resp.Body).Decode(into)
	}

	var list struct {
		Investigations []InvestigationView `json:"investigations"`
	}
	get("/api/investigations", &list)
	if len(list.Investigations) != 1 {
		t.Fatalf("list = %d rows", len(list.Investigations))
	}
	row := list.Investigations[0]
	if row.HasVerdict || row.OpenActions != 1 || row.OpenNotifications != 0 || row.PendingActions != 1 {
		t.Errorf("stage inputs = verdict:%v open:%d notify:%d pending:%d; want false/1/0/1", row.HasVerdict, row.OpenActions, row.OpenNotifications, row.PendingActions)
	}

	var detail InvestigationView
	get("/api/investigations/"+invID.String(), &detail)
	if detail.CreatedAt == nil || detail.CreatedAt.IsZero() {
		t.Errorf("detail lacks created_at: %+v", detail)
	}
	// The draft was created by the test helper's bare actor (no display name),
	// so the principal id is the honest fallback.
	if detail.OpenedByName == "" {
		t.Errorf("detail lacks opened_by_name")
	}
}
