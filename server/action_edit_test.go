package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
)

// TestEditAction_Endpoint: an analyst edits a pending message before approving
// it; the parameters are checked against the type, an AI delegate is refused,
// and an approved action cannot be edited (adr/0127).
func TestEditAction_Endpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t)
	human := mintToken(t, nil)

	resp, out := postAction(t, b, human, RequestActionBody{
		ActionType: "notify.email", InvestigationRef: invID.String(), Rationale: "tell the CISO",
		Targets:      []aggregate.TargetSpec{{ResolvedIdentifier: "ciso@contoso.com"}},
		Parameters:   json.RawMessage(`{"subject":"Breach","body":"draft"}`),
		EvidenceRefs: []string{"observed-data--od1"},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("request: %d", resp.StatusCode)
	}
	id := out.ActionID

	if r, _ := postActionDecision(t, b, human, id, "edit", map[string]any{"parameters": map[string]any{"subject": "Breach", "bdy": "typo"}}); r.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown parameter: %d; want 400", r.StatusCode)
	}
	if r, _ := postActionDecision(t, b, mintToken(t, map[string]any{"delegate_kind": "claude"}), id, "edit", map[string]any{"parameters": map[string]any{"subject": "x", "body": "y"}}); r.StatusCode != http.StatusForbidden {
		t.Errorf("AI edit: %d; want 403", r.StatusCode)
	}
	if r, _ := postActionDecision(t, b, human, id, "edit", map[string]any{"parameters": map[string]any{"subject": "Breach", "body": "final wording"}, "reason": "tone"}); r.StatusCode != http.StatusOK {
		t.Fatalf("edit: %d", r.StatusCode)
	}
	ac, err := aggregate.LoadActionCurrent(context.Background(), testDB, uuid.MustParse(id))
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]string
	_ = json.Unmarshal(ac.Parameters, &params)
	if params["body"] != "final wording" || ac.EditedAt.IsZero() || ac.Status != aggregate.ActionStatusRequested {
		t.Errorf("after edit: params=%v edited=%v status=%s", params, ac.EditedAt, ac.Status)
	}

	if r, _ := postActionDecision(t, b, human, id, "approve", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("approve: %d", r.StatusCode)
	}
	if r, _ := postActionDecision(t, b, human, id, "edit", map[string]any{"parameters": map[string]any{"subject": "late", "body": "late"}}); r.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("edit after approval: %d; want 422", r.StatusCode)
	}
}

// TestParamChanges: the record says what an edit replaced, field by field.
func TestParamChanges(t *testing.T) {
	got := paramChanges(json.RawMessage(`{"subject":"Breach","body":"draft","priority":"low"}`), json.RawMessage(`{"subject":"Breach","body":"final wording"}`))
	if got != `body: “draft” → “final wording”; priority: “low” → —` {
		t.Errorf("paramChanges = %q", got)
	}
}
