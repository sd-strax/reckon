package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
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
		EvidenceRefs: []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"},
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
	ac, err := testHandler.Reads().LoadActionCurrent(context.Background(), uuid.MustParse(id))
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

// TestEditAction_EvaluatesAsRequested: an edit re-runs Gate 2 as the requester
// asked for the action, not as the editor (adr/0374). A person editing the
// agent's ticket does not make the requester policy match it, and a person
// editing the agent's T3 request still meets the AI-no-T3 baseline.
func TestEditAction_EvaluatesAsRequested(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	const ownTicket = "policy/ticket-own-auto-approve/1.0.0"
	b := actionBackend(t, action.Policy{
		ID: ownTicket, ActionMatch: []string{"ticket.*"},
		Effect: action.EffectAutoApprove, Accountable: action.AccountableRequester,
		Predicate:  `ctx.action.requested_by.kind == "HUMAN" && ctx.action.tier == "T2"`,
		AuthoredBy: "system",
	})
	human := mintToken(t, nil)
	agent := mintToken(t, map[string]any{"delegate_kind": "claude"})

	// editEvaluation requests as the agent, edits as the person, and returns
	// the policy evaluation the edit recorded.
	editEvaluation := func(t *testing.T, body RequestActionBody, edited map[string]any) aggregate.ActionPolicyEvaluated {
		t.Helper()
		resp, out := postAction(t, b, agent, body)
		if resp.StatusCode != http.StatusCreated || out.Status != "PENDING_MANUAL" {
			t.Fatalf("agent request: %d status=%q; want 201 PENDING_MANUAL", resp.StatusCode, out.Status)
		}
		if r, _ := postActionDecision(t, b, human, out.ActionID, "edit", map[string]any{"parameters": edited}); r.StatusCode != http.StatusOK {
			t.Fatalf("edit: %d", r.StatusCode)
		}
		id := uuid.MustParse(out.ActionID)
		history, err := testHandler.Reads().ActionHistory(context.Background(), invID, id)
		if err != nil {
			t.Fatal(err)
		}
		var last *aggregate.ActionPolicyEvaluated
		var sawEdit bool
		for _, e := range history {
			switch ev := e.Event.(type) {
			case aggregate.ActionEdited:
				sawEdit = true
			case aggregate.ActionPolicyEvaluated:
				if sawEdit {
					last = &ev
				}
			}
		}
		if last == nil {
			t.Fatal("the edit recorded no policy evaluation")
		}
		return *last
	}

	t.Run("agent ticket edited by a person", func(t *testing.T) {
		pe := editEvaluation(t, RequestActionBody{
			ActionType:       "ticket.create",
			Targets:          []aggregate.TargetSpec{{EntityRef: "reckon-queue--1", ResolvedIdentifier: "SEC"}},
			Parameters:       json.RawMessage(`{"summary":"contain WIN-A"}`),
			Rationale:        "hand off",
			EvidenceRefs:     []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"},
			InvestigationRef: invID.String(),
		}, map[string]any{"summary": "contain WIN-A now"})
		if pe.MatchedPolicyRef == ownTicket {
			t.Errorf("edit matched %s; the agent's request never matches a requester policy", ownTicket)
		}
		for _, e := range pe.Evaluations {
			if e.PolicyRef == ownTicket && e.WouldHaveFired {
				t.Errorf("%s would_have_fired on the edit of an agent's request", ownTicket)
			}
		}
	})

	t.Run("agent T3 edited by a person", func(t *testing.T) {
		pe := editEvaluation(t, RequestActionBody{
			ActionType:       "email.purge",
			Targets:          []aggregate.TargetSpec{{EntityRef: "email-message--1", ResolvedIdentifier: "<msg@x>"}},
			Parameters:       json.RawMessage(`{}`),
			Rationale:        "phish",
			EvidenceRefs:     []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"},
			InvestigationRef: invID.String(),
		}, map[string]any{})
		fired := false
		for _, e := range pe.Evaluations {
			if e.PolicyRef == action.BaselineDenyPolicyID && e.WouldHaveFired {
				fired = true
			}
		}
		if !fired {
			t.Errorf("baseline %s did not fire on the edit of an agent's T3 request: %+v", action.BaselineDenyPolicyID, pe.Evaluations)
		}
	})
}
