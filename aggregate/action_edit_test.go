package aggregate

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestEditAction_Rules: only a request nobody approved can be edited, the edit
// records what it replaced, and an AI delegate may never edit (adr/0127).
func TestEditAction_Rules(t *testing.T) {
	env := newTestEnvelope("alice")
	id := uuid.New()
	state := func(status string) aggregateState {
		return aggregateState{Seq: 4, Exists: true, TenantID: env.TenantID, Status: StatusActive,
			Actions: map[uuid.UUID]actionState{id: {Status: status, Tier: TierT2, ActionType: "notify.email", TargetLabel: "ciso@contoso.com"}}}
	}
	edit := EditAction{ActionID: id, Parameters: json.RawMessage(`{"subject":"s","body":"new"}`), PriorParameters: json.RawMessage(`{"subject":"s","body":"old"}`), Reason: "real recipient wording", RequiredMode: "MANUAL",
		PolicyEvaluations: []PolicyEvaluationRecord{{PolicyRef: "p", Effect: "AUTO_APPROVE"}}}
	events, err := applyCommand(env, edit, state(ActionStatusRequested))
	if err != nil {
		t.Fatalf("edit of a REQUESTED action: %v", err)
	}
	if len(events) != 3 || events[0].Type != EventTypeActionEdited || events[2].Type != EventTypeActionPolicyEvaluated {
		t.Fatalf("events = %v; want edited, its interpretation, policy evaluated", events)
	}
	var p ActionEdited
	_ = json.Unmarshal(events[0].Payload, &p)
	if string(p.PriorParameters) != `{"subject":"s","body":"old"}` {
		t.Errorf("prior parameters not recorded: %s", p.PriorParameters)
	}
	var rec InterpretationRecorded
	_ = json.Unmarshal(events[1].Payload, &rec)
	if rec.InterpretationType != InterpretationActionEdit || rec.Summary != "notify.email → ciso@contoso.com: edited before approval: real recipient wording" {
		t.Errorf("interpretation = %q %q", rec.InterpretationType, rec.Summary)
	}
	for _, status := range []string{ActionStatusPendingSecondary, ActionStatusApproved, ActionStatusRejected} {
		if _, err := applyCommand(env, edit, state(status)); err == nil {
			t.Errorf("edit of a %s action accepted", status)
		}
	}
	if aiAllowed(edit) {
		t.Error("an AI delegate must not be allowed to edit a request")
	}
}

// TestEditAction_Projection: the projection carries the edited parameters and
// when the edit happened.
func TestEditAction_Projection(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	h := newTestHandler()
	aggID := mustActivate(t, h)
	actionID := uuid.New()
	mustHandle(t, h, cmdEnv(aggID), RequestAction{
		ActionID: actionID, ActionType: "notify.email", Tier: TierT2,
		Targets:      []TargetSpec{{ResolvedIdentifier: "ciso@contoso.com"}},
		Parameters:   json.RawMessage(`{"subject":"s","body":"old"}`),
		EvidenceRefs: []string{"observed-data--od1"}, ExpiresAt: time.Now().Add(time.Hour), Rationale: "tell the CISO",
	})
	mustHandle(t, h, cmdEnv(aggID), EditAction{ActionID: actionID, Parameters: json.RawMessage(`{"subject":"s","body":"new"}`), RequiredMode: "MANUAL"})
	ac, err := LoadActionCurrent(ctx, testDB, actionID)
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]string
	if err := json.Unmarshal(ac.Parameters, &params); err != nil || params["body"] != "new" {
		t.Errorf("parameters after edit = %s", ac.Parameters)
	}
	if ac.EditedAt.IsZero() || ac.Status != ActionStatusRequested {
		t.Errorf("edited_at=%v status=%q; want set and still REQUESTED", ac.EditedAt, ac.Status)
	}
}
