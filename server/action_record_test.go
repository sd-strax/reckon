package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
)

func getActionRecord(t *testing.T, b *Backend, token, id string) (int, ActionRecordView) {
	t.Helper()
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/actions/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out ActionRecordView
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestActionRecord_HistoryAndAuthorization: GET /api/actions/{id} is the
// action's record (design/ui 08 §8.6.1): every action.* event as a history
// entry with its actor and the status it moved to, the frozen Gate 2
// requirement with how it was met (the approver by display name), and the
// lineage links. A manual request → reject reads as two transitions plus the
// policy evaluation between them.
func TestActionRecord_HistoryAndAuthorization(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t) // no policies: manual fall-through

	token := mintToken(t, map[string]any{"preferred_username": "analyst-one"})
	resp, out := postAction(t, b, token, RequestActionBody{
		ActionType:       "host.isolate",
		Targets:          []aggregate.TargetSpec{{EntityRef: "x-host--1", ResolvedIdentifier: "WIN-A"}},
		EvidenceRefs:     []string{"observed-data--od1"},
		Rationale:        "contain",
		InvestigationRef: invID.String(),
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("request = %d", resp.StatusCode)
	}

	code, rec := getActionRecord(t, b, token, out.ActionID)
	if code != http.StatusOK {
		t.Fatalf("GET record = %d", code)
	}
	if rec.Action.ActionID != out.ActionID || rec.Action.Status != aggregate.ActionStatusRequested {
		t.Errorf("action view = %+v; want the REQUESTED action", rec.Action)
	}
	types := make([]string, 0, len(rec.History))
	for _, h := range rec.History {
		types = append(types, h.EventType)
	}
	if len(types) != 2 || types[0] != aggregate.EventTypeActionRequested || types[1] != aggregate.EventTypeActionPolicyEvaluated {
		t.Fatalf("history = %v; want requested + policy_evaluated", types)
	}
	if rec.History[0].Status != aggregate.ActionStatusRequested || rec.History[0].Actor.PrincipalName != "analyst-one" {
		t.Errorf("requested entry = %+v; want status REQUESTED by analyst-one", rec.History[0])
	}
	if len(rec.History[0].Actor.Roles) != 1 || rec.History[0].Actor.Roles[0] != "analyst" {
		t.Errorf("requested entry roles = %v; want the token's [analyst] (adr/0105)", rec.History[0].Actor.Roles)
	}
	if rec.History[1].Detail != "manual approval required · no policy pre-authorizes this" {
		t.Errorf("policy line = %q", rec.History[1].Detail)
	}
	if rec.Authorization.RequiredMode != aggregate.AuthModeManual || rec.Authorization.PrimaryApprover != nil {
		t.Errorf("authorization = %+v; want MANUAL, no approver yet", rec.Authorization)
	}
	if rec.Links.RequestingInterpretationID == "" {
		t.Errorf("links lack the requesting interpretation: %+v", rec.Links)
	}

	// Approve as a different, named principal: the approval entry carries the
	// approver's display name and the authorization names them.
	approver := mintToken(t, map[string]any{"sub": "approver-sub", "preferred_username": "approver-two"})
	if r, _ := postActionDecision(t, b, approver, out.ActionID, "approve", map[string]any{}); r.StatusCode != http.StatusOK {
		t.Fatalf("approve = %d", r.StatusCode)
	}
	code, rec = getActionRecord(t, b, token, out.ActionID)
	if code != http.StatusOK {
		t.Fatalf("GET record after approve = %d", code)
	}
	last := rec.History[len(rec.History)-1]
	if last.EventType != aggregate.EventTypeActionApproved || last.Status != aggregate.ActionStatusApproved {
		t.Errorf("last entry = %+v; want APPROVED", last)
	}
	if rec.Authorization.PrimaryApprover == nil || rec.Authorization.PrimaryApprover.Principal != "approver-sub" || rec.Authorization.PrimaryApprover.Name != "approver-two" {
		t.Errorf("primary approver = %+v; want approver-sub / approver-two", rec.Authorization.PrimaryApprover)
	}
	if last.Detail != "approved by approver-two" {
		t.Errorf("approval line = %q", last.Detail)
	}

	// Unknown id is an honest 404; a malformed one a 400.
	if code, _ := getActionRecord(t, b, token, "00000000-0000-0000-0000-000000000000"); code != http.StatusNotFound {
		t.Errorf("unknown = %d; want 404", code)
	}
	if code, _ := getActionRecord(t, b, token, "nope"); code != http.StatusBadRequest {
		t.Errorf("malformed = %d; want 400", code)
	}
}

// TestActionRecord_AutoPolicy: an auto-approved action's record names the
// policy that decided and the accountable human it was attributed to.
func TestActionRecord_AutoPolicy(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t, action.Policy{
		ID: "policy/auto-isolate/1.0.0", ActionMatch: []string{"host.isolate"},
		Effect: action.EffectAutoApprove, Predicate: "true", SignedOffBy: []string{"secops-lead"},
	})
	token := mintToken(t, nil)
	_, out := postAction(t, b, token, RequestActionBody{
		ActionType:       "host.isolate",
		Targets:          []aggregate.TargetSpec{{EntityRef: "x-host--1", ResolvedIdentifier: "WIN-A"}},
		EvidenceRefs:     []string{"observed-data--od1"},
		Rationale:        "contain",
		InvestigationRef: invID.String(),
	})
	code, rec := getActionRecord(t, b, token, out.ActionID)
	if code != http.StatusOK {
		t.Fatalf("GET record = %d", code)
	}
	if rec.Authorization.Mode != aggregate.AuthModeAutoPolicy || rec.Authorization.MatchedPolicyRef != "policy/auto-isolate/1.0.0" {
		t.Errorf("authorization = %+v; want AUTO_POLICY by policy/auto-isolate/1.0.0", rec.Authorization)
	}
	if rec.Authorization.PrimaryApprover == nil || rec.Authorization.PrimaryApprover.Principal != "secops-lead" {
		t.Errorf("approver = %+v; want the policy's accountable human", rec.Authorization.PrimaryApprover)
	}
	// The baseline DENY (AI-no-T3) is always evaluated first; the matched
	// policy is the one that fired.
	var matched *aggregate.PolicyEvaluationRecord
	for i := range rec.Authorization.Evaluations {
		if rec.Authorization.Evaluations[i].PolicyRef == rec.Authorization.MatchedPolicyRef {
			matched = &rec.Authorization.Evaluations[i]
		}
	}
	if matched == nil || matched.Effect != string(action.EffectAutoApprove) || !matched.WouldHaveFired {
		t.Errorf("evaluations = %+v; want the matched policy AUTO_APPROVE and fired", rec.Authorization.Evaluations)
	}
	if last := rec.History[len(rec.History)-1]; last.Detail != "auto-approved by policy policy/auto-isolate/1.0.0 · accountable: secops-lead" {
		t.Errorf("approval line = %q", last.Detail)
	}
}
