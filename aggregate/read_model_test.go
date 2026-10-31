package aggregate

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestApprovalStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		auth Authorization
		want string
	}{
		"solo":                {Authorization{Mode: AuthModeManual, Stage: AuthStageSolo}, ActionStatusApproved},
		"two-party primary":   {Authorization{Mode: AuthModeTwoParty, Stage: AuthStagePrimary}, ActionStatusPendingSecondary},
		"two-party secondary": {Authorization{Mode: AuthModeTwoParty, Stage: AuthStageSecondary}, ActionStatusApproved},
	} {
		if got := approvalStatus(tc.auth); got != tc.want {
			t.Errorf("%s: %q; want %q", name, got, tc.want)
		}
	}
}

// TestResultStatus: nothing but a stated success reads as one, in the fold and
// the projection alike.
func TestResultStatus(t *testing.T) {
	for outcome, want := range map[string]string{
		"SUCCEEDED": ActionStatusSucceeded,
		"PARTIAL":   ActionStatusSucceeded,
		"FAILED":    ActionStatusFailed,
		"TIMEOUT":   ActionStatusFailed,
		"NOVEL":     ActionStatusFailed,
	} {
		if got := resultStatus(outcome); got != want {
			t.Errorf("%s: %q; want %q", outcome, got, want)
		}
	}
}

func TestHistoryStatus(t *testing.T) {
	id, other := uuid.New(), uuid.New()
	for name, tc := range map[string]struct {
		event any
		want  string
	}{
		"requested":          {ActionRequested{}, ActionStatusRequested},
		"policy evaluated":   {ActionPolicyEvaluated{}, ""},
		"edited":             {ActionEdited{}, ActionStatusRequested},
		"primary approval":   {ActionApproved{Authorization: Authorization{Mode: AuthModeTwoParty, Stage: AuthStagePrimary}}, ActionStatusPendingSecondary},
		"rejected":           {ActionRejected{}, ActionStatusRejected},
		"expired":            {ActionExpired{}, ActionStatusExpired},
		"dispatched":         {ActionDispatched{}, ActionStatusExecuting},
		"partial stays":      {ActionResulted{FinalOutcome: "PARTIAL"}, "PARTIAL"},
		"timeout stays":      {ActionResulted{FinalOutcome: "TIMEOUT"}, "TIMEOUT"},
		"reversed":           {ActionReversed{OriginalActionID: id}, ActionStatusReversed},
		"reversed, as saga":  {ActionReversed{OriginalActionID: other}, ""},
		"reversal attempted": {ActionReversalAttempted{OriginalActionID: id}, ""},
		"outcome unknown":    {ActionOutcomeUnknown{}, ActionStatusOutcomeUnknown},
		"outcome resolved":   {ActionOutcomeResolved{Outcome: ActionStatusFailed}, ActionStatusFailed},
		"uninterpreted":      {nil, ""},
	} {
		if got := historyStatus(tc.event, id); got != tc.want {
			t.Errorf("%s: %q; want %q", name, got, tc.want)
		}
	}
}

func TestDecodeActionEvent(t *testing.T) {
	if _, err := decodeActionEvent(EventTypeActionApproved, []byte(`{"authorization":"not an object"}`)); err == nil {
		t.Error("a corrupt payload decoded")
	}
	if ev, err := decodeActionEvent("action.novel", []byte(`{"anything":1}`)); err != nil || ev != nil {
		t.Errorf("an uninterpreted type = %v, %v; want nil, nil", ev, err)
	}
	ev, err := decodeActionEvent(EventTypeActionRejected, []byte(`{"reason":"no"}`))
	if p, ok := ev.(ActionRejected); err != nil || !ok || p.Reason != "no" {
		t.Errorf("decoded %#v, %v", ev, err)
	}
}

// TestActionHistory: the record shows the outcome the tool reported, while the
// action's status is the folded one.
func TestActionHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	h := newTestHandler()
	aggID := mustActivate(t, h)
	actionID := uuid.New()

	mustHandle(t, h, cmdEnv(aggID), RequestAction{
		ActionID: actionID, ActionType: "host.isolate", Tier: TierT2,
		Targets:      []TargetSpec{{EntityRef: "x-host--1", ResolvedIdentifier: "WIN-DC01"}},
		EvidenceRefs: []string{"observed-data--od1"}, ExpiresAt: time.Now().Add(time.Hour), Rationale: "contain",
	})
	mustHandle(t, h, cmdEnv(aggID), ApproveAction{
		ActionID: actionID,
		Authorization: Authorization{
			Mode: AuthModeManual, Stage: AuthStageSolo, PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now(),
		},
	})
	mustHandle(t, h, sysEnv2(aggID), DispatchAction{ActionID: actionID, Adapter: "edr", AdapterRequestID: "wf-1"})
	mustHandle(t, h, sysEnv2(aggID), ResultAction{
		ActionID: actionID, FinalOutcome: "PARTIAL", Attempts: 1,
		PerTargetResults: map[string]string{"WIN-DC01": "SUCCEEDED", "WIN-DC02": "FAIL"},
	})

	history, err := testReads().ActionHistory(ctx, aggID, actionID)
	if err != nil {
		t.Fatalf("ActionHistory: %v", err)
	}
	want := []struct{ typ, status string }{
		{EventTypeActionRequested, ActionStatusRequested},
		{EventTypeActionApproved, ActionStatusApproved},
		{EventTypeActionDispatched, ActionStatusExecuting},
		{EventTypeActionResulted, "PARTIAL"},
	}
	if len(history) != len(want) {
		t.Fatalf("history = %+v; want %d entries", history, len(want))
	}
	for i, w := range want {
		if history[i].Type != w.typ || history[i].Status != w.status {
			t.Errorf("entry %d = %s %q; want %s %q", i, history[i].Type, history[i].Status, w.typ, w.status)
		}
	}
	if p, ok := history[1].Event.(ActionApproved); !ok || p.Authorization.PrimaryApproverRef != "alice" {
		t.Errorf("approved entry carried %#v", history[1].Event)
	}
	ac, err := testReads().LoadActionCurrent(ctx, actionID)
	if err != nil {
		t.Fatal(err)
	}
	if ac.Status != ActionStatusSucceeded {
		t.Errorf("status = %q; PARTIAL folds to SUCCEEDED", ac.Status)
	}
}

// TestActionHistory_CorruptPayload: an event that does not decode fails the
// read rather than showing as a blank row.
func TestActionHistory_CorruptPayload(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	h := newTestHandler()
	aggID := mustActivate(t, h)
	actionID := uuid.New()
	mustHandle(t, h, cmdEnv(aggID), RequestAction{
		ActionID: actionID, ActionType: "host.isolate", Tier: TierT2,
		Targets:      []TargetSpec{{EntityRef: "x-host--1", ResolvedIdentifier: "WIN-DC01"}},
		EvidenceRefs: []string{"observed-data--od1"}, ExpiresAt: time.Now().Add(time.Hour), Rationale: "contain",
	})
	corrupt, _ := json.Marshal(map[string]any{"action_id": actionID, "targets": "not a list"})
	if _, err := testDB.Exec(`UPDATE events SET payload = $1 WHERE aggregate_id = $2 AND event_type = $3`,
		corrupt, aggID, EventTypeActionRequested); err != nil {
		t.Fatalf("corrupt the event: %v", err)
	}
	if _, err := testReads().ActionHistory(context.Background(), aggID, actionID); err == nil {
		t.Error("a corrupt event read as history")
	}
}

func TestReads_MissingIsErrNoRecord(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	reads := testReads()
	id := uuid.New()
	_, actionErr := reads.LoadActionCurrent(ctx, id)
	_, invErr := reads.LoadInvestigationCurrent(ctx, id)
	_, interpErr := reads.LoadInterpretation(ctx, id)
	_, bodyErr := reads.TranscriptBody(ctx, testTenantID, "no-such-hash")
	_, caseErr := reads.InvestigationForCase(ctx, "INC000")
	_, _, openErr := reads.InvestigationOpening(ctx, id)
	for name, err := range map[string]error{
		"action": actionErr, "investigation": invErr, "interpretation": interpErr,
		"transcript": bodyErr, "case": caseErr, "opening": openErr,
	} {
		if !errors.Is(err, ErrNoRecord) {
			t.Errorf("%s: %v; want ErrNoRecord", name, err)
		}
	}
}
