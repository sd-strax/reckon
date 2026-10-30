package aggregate

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// activeStateWithAction returns a folded state for an ACTIVE investigation that
// optionally already carries one action in the given status.
func activeStateWithAction(env Envelope, actionID uuid.UUID, status string) aggregateState {
	s := aggregateState{
		Seq: 4, Exists: true, TenantID: env.TenantID, Status: StatusActive,
		Actions: map[uuid.UUID]actionState{},
	}
	if status != "" {
		s.Actions[actionID] = actionState{Status: status, Tier: TierT2}
	}
	return s
}

func sampleRequest(id uuid.UUID) RequestAction {
	return RequestAction{
		ActionID:   id,
		ActionType: "host.isolate",
		Tier:       TierT2,
		Targets:    []TargetSpec{{EntityRef: "x-host--1", ResolvedIdentifier: "WIN-DC01"}},
		ExpiresAt:  time.Now().Add(time.Hour),
		Rationale:  "contain lateral movement",
	}
}

// TestRequestAction_ProducesPairedEvents: a request emits (ActionRequested,
// action-request Interpretation) sharing a correlation_id, with the domain
// event pointing at the producing interpretation.
func TestRequestAction_ProducesPairedEvents(t *testing.T) {
	env := newTestEnvelope("alice")
	id := uuid.New()
	events, err := applyCommand(env, sampleRequest(id), activeStateWithAction(env, id, ""), testIDs())
	if err != nil {
		t.Fatalf("applyCommand: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("produced %d events; want 2 (domain + interpretation)", len(events))
	}
	domain, interp := events[0], events[1]
	if domain.Type != EventTypeActionRequested {
		t.Errorf("domain type = %q; want %q", domain.Type, EventTypeActionRequested)
	}
	if interp.Type != EventTypeInterpretationRecorded {
		t.Errorf("paired type = %q; want %q", interp.Type, EventTypeInterpretationRecorded)
	}
	if domain.CorrelationID != env.CorrelationID || interp.CorrelationID != env.CorrelationID {
		t.Error("paired events do not share the command's correlation_id")
	}

	var req ActionRequested
	if err := json.Unmarshal(domain.Payload, &req); err != nil {
		t.Fatal(err)
	}
	var ir InterpretationRecorded
	if err := json.Unmarshal(interp.Payload, &ir); err != nil {
		t.Fatal(err)
	}
	if ir.InterpretationType != InterpretationActionRequest {
		t.Errorf("interpretation_type = %q; want %q", ir.InterpretationType, InterpretationActionRequest)
	}
	if req.RequestingInterpretationID != ir.InterpretationID {
		t.Errorf("action's requesting_interpretation_id %s != interpretation id %s",
			req.RequestingInterpretationID, ir.InterpretationID)
	}
	if req.ActionID != id {
		t.Errorf("action_id = %s; want %s", req.ActionID, id)
	}
}

// TestRequestAction_EmitsPolicyEvaluated: when Gate 2 ran (evaluations present),
// the PolicyEvaluated audit event is written in the SAME transaction as
// ActionRequested (02 §3), sharing the correlation_id, and is audit-only (does
// not move the action off REQUESTED).
func TestRequestAction_EmitsPolicyEvaluated(t *testing.T) {
	env := newTestEnvelope("alice")
	id := uuid.New()
	cmd := sampleRequest(id)
	cmd.PolicyEvaluations = []PolicyEvaluationRecord{
		{PolicyRef: "policy/ai-no-tier3/1.0.0", PolicyVersion: "abc", WouldHaveFired: false, Effect: "DENY"},
	}
	cmd.MatchedPolicyRef = ""

	events, err := applyCommand(env, cmd, activeStateWithAction(env, id, ""), testIDs())
	if err != nil {
		t.Fatalf("applyCommand: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("produced %d events; want 3 (requested + interpretation + policy_evaluated)", len(events))
	}
	pe := events[2]
	if pe.Type != EventTypeActionPolicyEvaluated {
		t.Errorf("third event = %q; want %q", pe.Type, EventTypeActionPolicyEvaluated)
	}
	if pe.CorrelationID != env.CorrelationID {
		t.Error("policy_evaluated does not share the request's correlation_id")
	}

	// Audit-only: folding the whole batch leaves the action REQUESTED.
	folded := foldInto(map[uuid.UUID]actionState{}, events)
	if folded[id].Status != ActionStatusRequested {
		t.Errorf("after request+policy_evaluated, status = %q; want REQUESTED", folded[id].Status)
	}
}

// TestRequestAction_StatePreconditions: allowed on ACTIVE or DRAFT (invisible
// activate — the request is recorded, activation happens at first approval), or
// CONCLUDED for a reversal.
func TestRequestAction_StatePreconditions(t *testing.T) {
	env := newTestEnvelope("alice")
	id := uuid.New()

	// A DRAFT investigation accepts an action REQUEST (01 §Extension 2, invisible
	// activate): it is recorded, and the investigation stays DRAFT until the
	// action is approved.
	draft := aggregateState{Seq: 1, Exists: true, TenantID: env.TenantID, Status: StatusDraft, Actions: map[uuid.UUID]actionState{}}
	if _, err := applyCommand(env, sampleRequest(id), draft, testIDs()); err != nil {
		t.Errorf("RequestAction on DRAFT should be allowed (invisible activate): %v", err)
	}

	concluded := aggregateState{Seq: 1, Exists: true, TenantID: env.TenantID, Status: StatusConcluded, Actions: map[uuid.UUID]actionState{}}
	if _, err := applyCommand(env, sampleRequest(id), concluded, testIDs()); err == nil {
		t.Error("non-reversal RequestAction on CONCLUDED should be rejected")
	}
	rev := sampleRequest(id)
	rev.IsReversal = true
	if _, err := applyCommand(env, rev, concluded, testIDs()); err != nil {
		t.Errorf("reversal RequestAction on CONCLUDED should be allowed: %v", err)
	}
}

// TestApproveAction_InvisibleActivate: approving the first action of a DRAFT
// investigation activates it (01 §Extension 2, action boundary) — but only a
// FULLY-approved action; a two-party primary (still PENDING_SECONDARY) does not.
func TestApproveAction_InvisibleActivate(t *testing.T) {
	env := newTestEnvelope("alice")
	id := uuid.New()

	draftWithRequested := func(mode string) aggregateState {
		return aggregateState{
			Seq: 4, Exists: true, TenantID: env.TenantID, Status: StatusDraft,
			Actions: map[uuid.UUID]actionState{id: {Status: ActionStatusRequested, Tier: TierT2, RequiredMode: mode}},
		}
	}
	activated := func(events []Event) bool {
		for _, e := range events {
			if e.Type == EventTypeStatusChanged {
				var p InvestigationStatusChanged
				if err := json.Unmarshal(e.Payload, &p); err == nil && p.From == StatusDraft && p.To == StatusActive {
					return true
				}
			}
		}
		return false
	}

	// A solo approval fully approves → activates.
	solo := ApproveAction{ActionID: id, Authorization: Authorization{
		Mode: AuthModeManual, Stage: AuthStageSolo, PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now(),
	}}
	events, err := applyCommand(env, solo, draftWithRequested(""), testIDs())
	if err != nil {
		t.Fatalf("solo approve on draft: %v", err)
	}
	if !activated(events) {
		t.Error("solo approval of the first action should activate the DRAFT investigation")
	}

	// A two-party PRIMARY only reaches PENDING_SECONDARY — not cleared to act, so
	// it must NOT activate.
	primary := ApproveAction{ActionID: id, Authorization: Authorization{
		Mode: AuthModeTwoParty, Stage: AuthStagePrimary, PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now(),
	}}
	ev2, err := applyCommand(env, primary, draftWithRequested(AuthModeTwoParty), testIDs())
	if err != nil {
		t.Fatalf("two-party primary on draft: %v", err)
	}
	if activated(ev2) {
		t.Error("a two-party primary approval (still PENDING_SECONDARY) must not activate the draft")
	}
}

// TestApproveAction_ActorApproverInvariant: the acting principal must be the
// approver (04 §3.3).
func TestApproveAction_ActorApproverInvariant(t *testing.T) {
	env := newTestEnvelope("alice")
	id := uuid.New()
	state := activeStateWithAction(env, id, ActionStatusRequested)

	// Approver != acting principal → rejected.
	mismatch := ApproveAction{ActionID: id, Authorization: Authorization{
		Mode: AuthModeManual, Stage: AuthStageSolo, PrimaryApproverRef: "bob", PrimaryApprovedAt: time.Now(),
	}}
	if _, err := applyCommand(env, mismatch, state, testIDs()); err == nil {
		t.Error("approval by a non-approver principal should be rejected")
	}

	// Approver == acting principal → APPROVED.
	ok := ApproveAction{ActionID: id, Authorization: Authorization{
		Mode: AuthModeManual, Stage: AuthStageSolo, PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now(),
	}}
	events, err := applyCommand(env, ok, state, testIDs())
	if err != nil {
		t.Fatalf("valid approval rejected: %v", err)
	}
	if events[0].Type != EventTypeActionApproved {
		t.Errorf("domain type = %q; want %q", events[0].Type, EventTypeActionApproved)
	}
}

// TestApproveAction_TwoPartyStages: a TWO_PARTY primary approval pends secondary;
// the secondary (a different approver) finalizes.
func TestApproveAction_TwoPartyStages(t *testing.T) {
	env := newTestEnvelope("alice")
	id := uuid.New()

	// Primary approval on a REQUESTED action.
	reqState := activeStateWithAction(env, id, ActionStatusRequested)
	primary := ApproveAction{ActionID: id, Authorization: Authorization{
		Mode: AuthModeTwoParty, Stage: AuthStagePrimary, PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now(),
	}}
	events, err := applyCommand(env, primary, reqState, testIDs())
	if err != nil {
		t.Fatalf("primary approval: %v", err)
	}
	// Fold the emitted events: status should be PENDING_SECONDARY.
	folded := foldInto(reqState.Actions, events)
	if folded[id].Status != ActionStatusPendingSecondary {
		t.Fatalf("after primary, status = %q; want PENDING_SECONDARY", folded[id].Status)
	}

	// Secondary approval by a different principal on the pending action.
	env2 := newTestEnvelope("bob")
	env2.AggregateID = env.AggregateID
	pendState := aggregateState{Seq: 6, Exists: true, TenantID: env.TenantID, Status: StatusActive, Actions: folded}
	secTime := time.Now()
	secondary := ApproveAction{ActionID: id, Authorization: Authorization{
		Mode: AuthModeTwoParty, Stage: AuthStageSecondary,
		PrimaryApproverRef: "alice", SecondaryApproverRef: "bob", SecondaryApprovedAt: &secTime,
	}}
	events2, err := applyCommand(env2, secondary, pendState, testIDs())
	if err != nil {
		t.Fatalf("secondary approval: %v", err)
	}
	if foldInto(pendState.Actions, events2)[id].Status != ActionStatusApproved {
		t.Error("after secondary, status should be APPROVED")
	}
}

// TestAIWriteProtection: an AI_DELEGATED actor may request an action but can
// never approve, reject, or expire one (04 §5.6).
func TestAIWriteProtection(t *testing.T) {
	env := newTestEnvelope("alice")
	env.Actor.Kind = ActorAIDelegated
	id := uuid.New()

	// Request is permitted for the AI.
	if _, err := applyCommand(env, sampleRequest(id), activeStateWithAction(env, id, ""), testIDs()); err != nil {
		t.Errorf("AI RequestAction should be allowed: %v", err)
	}

	// Approve/Reject/Expire are all forbidden for the AI.
	state := activeStateWithAction(env, id, ActionStatusRequested)
	forbidden := []Command{
		ApproveAction{ActionID: id, Authorization: Authorization{Mode: AuthModeManual, Stage: AuthStageSolo, PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now()}},
		RejectAction{ActionID: id, Reason: "no"},
		ExpireAction{ActionID: id},
	}
	for _, cmd := range forbidden {
		if _, err := applyCommand(env, cmd, state, testIDs()); err == nil {
			t.Errorf("AI_DELEGATED %s should be rejected", cmd.Kind())
		}
	}
}

// TestDispatchResult_SystemOnlyAndTransitions: dispatch/result require a SYSTEM
// actor and the right source state (APPROVED → dispatch → EXECUTING → result).
func TestDispatchResult_SystemOnlyAndTransitions(t *testing.T) {
	id := uuid.New()

	// A human cannot dispatch — system-only guard.
	human := newTestEnvelope("alice")
	approved := activeStateWithAction(human, id, ActionStatusApproved)
	if _, err := applyCommand(human, DispatchAction{ActionID: id}, approved, testIDs()); err == nil {
		t.Error("human DispatchAction should be rejected (system-only)")
	}

	// SYSTEM dispatches an APPROVED action → EXECUTING.
	sys := newTestEnvelope("alice")
	sys.Actor.Kind = ActorSystem
	events, err := applyCommand(sys, DispatchAction{ActionID: id, Adapter: "fx", AdapterRequestID: "wf-1"}, approved, testIDs())
	if err != nil {
		t.Fatalf("system dispatch: %v", err)
	}
	if events[0].Type != EventTypeActionDispatched {
		t.Errorf("domain type = %q; want dispatched", events[0].Type)
	}
	executing := foldInto(approved.Actions, events)
	if executing[id].Status != ActionStatusExecuting {
		t.Fatalf("after dispatch status = %q; want EXECUTING", executing[id].Status)
	}

	// Dispatch requires APPROVED — from REQUESTED it's rejected.
	req := activeStateWithAction(sys, id, ActionStatusRequested)
	if _, err := applyCommand(sys, DispatchAction{ActionID: id}, req, testIDs()); err == nil {
		t.Error("DispatchAction from REQUESTED should be rejected")
	}

	// SYSTEM records the result on the EXECUTING action → SUCCEEDED.
	execState := aggregateState{Seq: 8, Exists: true, TenantID: sys.TenantID, Status: StatusActive, Actions: executing}
	revents, err := applyCommand(sys, ResultAction{ActionID: id, FinalOutcome: "SUCCEEDED", PerTargetResults: map[string]string{"0": "OK"}, Attempts: 1}, execState, testIDs())
	if err != nil {
		t.Fatalf("system result: %v", err)
	}
	if foldInto(executing, revents)[id].Status != ActionStatusSucceeded {
		t.Error("after SUCCEEDED result, status should be SUCCEEDED")
	}
}

// TestReverseAction: SYSTEM-only, legal only from SUCCEEDED, moves the original
// to REVERSED (04 §7).
func TestReverseAction(t *testing.T) {
	origID, revID := uuid.New(), uuid.New()

	sys := newTestEnvelope("alice")
	sys.Actor.Kind = ActorSystem
	succeeded := activeStateWithAction(sys, origID, ActionStatusSucceeded)

	events, err := applyCommand(sys, ReverseAction{OriginalActionID: origID, ReversingActionID: revID}, succeeded, testIDs())
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if events[0].Type != EventTypeActionReversed {
		t.Errorf("domain type = %q; want reversed", events[0].Type)
	}
	if foldInto(succeeded.Actions, events)[origID].Status != ActionStatusReversed {
		t.Error("original status should be REVERSED")
	}

	// Only a SUCCEEDED action can be reversed.
	req := activeStateWithAction(sys, origID, ActionStatusRequested)
	if _, err := applyCommand(sys, ReverseAction{OriginalActionID: origID, ReversingActionID: revID}, req, testIDs()); err == nil {
		t.Error("reversing a non-SUCCEEDED action should be rejected")
	}

	// System-only: a human cannot forge a reversal.
	human := newTestEnvelope("alice")
	if _, err := applyCommand(human, ReverseAction{OriginalActionID: origID, ReversingActionID: revID}, succeeded, testIDs()); err == nil {
		t.Error("human ReverseAction should be rejected (system-only)")
	}
}

// TestRecordReversalAttempt: SYSTEM-only, legal only from SUCCEEDED, and — the
// core of 04 §7.1 Position C — the original's status does NOT change: the
// unverified attempt is a back-reference on the projection, never a REVERSED
// claim.
func TestRecordReversalAttempt(t *testing.T) {
	origID, revID := uuid.New(), uuid.New()

	sys := newTestEnvelope("alice")
	sys.Actor.Kind = ActorSystem
	succeeded := activeStateWithAction(sys, origID, ActionStatusSucceeded)

	events, err := applyCommand(sys, RecordReversalAttempt{OriginalActionID: origID, ReversingActionID: revID}, succeeded, testIDs())
	if err != nil {
		t.Fatalf("record attempt: %v", err)
	}
	if events[0].Type != EventTypeActionReversalAttempted {
		t.Errorf("domain type = %q; want reversal_attempted", events[0].Type)
	}
	// The honest-state assertion: folding the attempt leaves the original
	// SUCCEEDED; it records only who attempted the reversal.
	attempted := foldInto(succeeded.Actions, events)[origID]
	if attempted.Status != ActionStatusSucceeded {
		t.Errorf("original status after attempt = %q; must stay SUCCEEDED", attempted.Status)
	}
	if attempted.ReversalAttemptedBy != revID {
		t.Errorf("ReversalAttemptedBy = %v; want %v", attempted.ReversalAttemptedBy, revID)
	}

	// Only a SUCCEEDED action has an effect to attempt to reverse.
	req := activeStateWithAction(sys, origID, ActionStatusRequested)
	if _, err := applyCommand(sys, RecordReversalAttempt{OriginalActionID: origID, ReversingActionID: revID}, req, testIDs()); err == nil {
		t.Error("attempt on a non-SUCCEEDED action should be rejected")
	}

	// System-only: a human cannot forge an attempt record.
	human := newTestEnvelope("alice")
	if _, err := applyCommand(human, RecordReversalAttempt{OriginalActionID: origID, ReversingActionID: revID}, succeeded, testIDs()); err == nil {
		t.Error("human RecordReversalAttempt should be rejected (system-only)")
	}
}

// TestRecordReversalAttempt_Repeat: the write side folds the attempt, so a
// retried or re-run ReversalSaga repeating its own write appends nothing, and
// an attempt by a different reversing action is refused (#214).
func TestRecordReversalAttempt_Repeat(t *testing.T) {
	origID, revID := uuid.New(), uuid.New()

	sys := newTestEnvelope("alice")
	sys.Actor.Kind = ActorSystem
	state := activeStateWithAction(sys, origID, ActionStatusSucceeded)

	first, err := applyCommand(sys, RecordReversalAttempt{OriginalActionID: origID, ReversingActionID: revID}, state, testIDs())
	if err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	state.Actions = foldInto(state.Actions, first)

	again, err := applyCommand(sys, RecordReversalAttempt{OriginalActionID: origID, ReversingActionID: revID}, state, testIDs())
	if err != nil {
		t.Fatalf("repeat of the same attempt should be accepted as already recorded: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("repeat of the same attempt appended %d events; want 0", len(again))
	}

	if _, err := applyCommand(sys, RecordReversalAttempt{OriginalActionID: origID, ReversingActionID: uuid.New()}, state, testIDs()); err == nil {
		t.Error("an attempt by a different reversing action should be rejected")
	}
}

// TestAIAllowlist: the AI guard is an ALLOWLIST (04 §5.6) — T1-annotate
// commands and RequestAction pass; conclude/archive and any unlisted command
// default to denied.
func TestAIAllowlist(t *testing.T) {
	env := newTestEnvelope("alice")
	env.Actor.Kind = ActorAIDelegated

	// T1: lifecycle draft→active is AI-permitted (04 §1).
	draft := aggregateState{Seq: 1, Exists: true, TenantID: env.TenantID, Status: StatusDraft, Actions: map[uuid.UUID]actionState{}}
	if _, err := applyCommand(env, ActivateInvestigation{}, draft, testIDs()); err != nil {
		t.Errorf("AI Activate (T1) should be allowed: %v", err)
	}

	// Concluding an investigation is a human act — denied by default.
	active := aggregateState{Seq: 1, Exists: true, TenantID: env.TenantID, Status: StatusActive, Actions: map[uuid.UUID]actionState{}}
	if _, err := applyCommand(env, ConcludeInvestigation{ReportRef: "r", Summary: "s"}, active, testIDs()); err == nil {
		t.Error("AI ConcludeInvestigation should be denied (not in allowlist)")
	}
}

// TestApproveAction_ModeStageConsistency: 04 §3.3 pairs are enforced at
// Validate — TWO_PARTY+SOLO (the one-step secondary bypass) and MANUAL+PRIMARY
// are both malformed.
func TestApproveAction_ModeStageConsistency(t *testing.T) {
	env := newTestEnvelope("alice")
	id := uuid.New()
	state := activeStateWithAction(env, id, ActionStatusRequested)

	bypass := ApproveAction{ActionID: id, Authorization: Authorization{
		Mode: AuthModeTwoParty, Stage: AuthStageSolo, PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now(),
	}}
	if _, err := applyCommand(env, bypass, state, testIDs()); err == nil {
		t.Error("TWO_PARTY with stage SOLO must be rejected (one-step secondary bypass)")
	}

	malformed := ApproveAction{ActionID: id, Authorization: Authorization{
		Mode: AuthModeManual, Stage: AuthStagePrimary, PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now(),
	}}
	if _, err := applyCommand(env, malformed, state, testIDs()); err == nil {
		t.Error("MANUAL with stage PRIMARY must be rejected")
	}
}

// TestApproveAction_TwoPartyIntegrity: the secondary approver must differ from
// the primary, and the secondary event's primary_approver_ref must match the
// recorded primary approval (folded state, not the caller's payload).
func TestApproveAction_TwoPartyIntegrity(t *testing.T) {
	env := newTestEnvelope("alice")
	id := uuid.New()
	pending := activeStateWithAction(env, id, ActionStatusPendingSecondary)
	act := pending.Actions[id]
	act.PrimaryApprover = "alice"
	pending.Actions[id] = act

	// Same human approving both stages → rejected.
	self := newTestEnvelope("alice")
	self.AggregateID = env.AggregateID
	sameHuman := ApproveAction{ActionID: id, Authorization: Authorization{
		Mode: AuthModeTwoParty, Stage: AuthStageSecondary,
		PrimaryApproverRef: "alice", SecondaryApproverRef: "alice",
	}}
	if _, err := applyCommand(self, sameHuman, pending, testIDs()); err == nil {
		t.Error("secondary approver == primary approver must be rejected (TWO_PARTY requires two humans)")
	}

	// A secondary event rewriting who the primary was → rejected.
	bob := newTestEnvelope("bob")
	bob.AggregateID = env.AggregateID
	rewrite := ApproveAction{ActionID: id, Authorization: Authorization{
		Mode: AuthModeTwoParty, Stage: AuthStageSecondary,
		PrimaryApproverRef: "mallory", SecondaryApproverRef: "bob",
	}}
	if _, err := applyCommand(bob, rewrite, pending, testIDs()); err == nil {
		t.Error("primary_approver_ref mismatching the recorded primary must be rejected")
	}
}

// TestFoldDispatchLifecycle: the system-emitted events (C.4's emitters) fold the
// rest of the machine — EXECUTING on dispatch, SUCCEEDED/FAILED per outcome
// (PARTIAL→SUCCEEDED, TIMEOUT→FAILED), and REVERSED on the original.
func TestFoldDispatchLifecycle(t *testing.T) {
	id := uuid.New()
	mk := func(eventType string, payload any) Event {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return Event{Type: eventType, Payload: b}
	}

	cases := []struct {
		name    string
		events  []Event
		want    string
		checkID uuid.UUID
	}{
		{"dispatched → EXECUTING",
			[]Event{mk(EventTypeActionDispatched, ActionDispatched{ActionID: id, Adapter: "fx"})},
			ActionStatusExecuting, id},
		{"resulted SUCCEEDED",
			[]Event{mk(EventTypeActionResulted, ActionResulted{ActionID: id, FinalOutcome: "SUCCEEDED"})},
			ActionStatusSucceeded, id},
		{"resulted PARTIAL → SUCCEEDED",
			[]Event{mk(EventTypeActionResulted, ActionResulted{ActionID: id, FinalOutcome: "PARTIAL"})},
			ActionStatusSucceeded, id},
		{"resulted TIMEOUT → FAILED",
			[]Event{mk(EventTypeActionResulted, ActionResulted{ActionID: id, FinalOutcome: "TIMEOUT"})},
			ActionStatusFailed, id},
		{"reversed → REVERSED on the original",
			[]Event{mk(EventTypeActionReversed, ActionReversed{OriginalActionID: id, ReversingActionID: uuid.New()})},
			ActionStatusReversed, id},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actions := map[uuid.UUID]actionState{id: {Status: ActionStatusApproved, Tier: TierT2}}
			for _, e := range tc.events {
				if err := foldActionEvent(actions, e); err != nil {
					t.Fatal(err)
				}
			}
			if actions[tc.checkID].Status != tc.want {
				t.Errorf("status = %q; want %q", actions[tc.checkID].Status, tc.want)
			}
		})
	}
}

// foldInto applies events onto a copy of the given action state map, returning
// the result (test helper for pure fold assertions).
func foldInto(base map[uuid.UUID]actionState, events []Event) map[uuid.UUID]actionState {
	out := make(map[uuid.UUID]actionState, len(base))
	for k, v := range base {
		out[k] = v
	}
	for _, e := range events {
		_ = foldActionEvent(out, e)
	}
	return out
}

// TestRequestAction_Lineage: a chain of re-requests and reversals stays a line
// (adr/0152) — one successor per action, built only from the state that allows
// it, and only within this investigation.
func TestRequestAction_Lineage(t *testing.T) {
	env := newTestEnvelope("alice")
	orig, sibling := uuid.New(), uuid.New()
	state := func(actions map[uuid.UUID]actionState) aggregateState {
		return aggregateState{Seq: 4, Exists: true, TenantID: env.TenantID, Status: StatusActive, Actions: actions}
	}
	isolate := func(status string) actionState {
		return actionState{Status: status, Tier: TierT2, ActionType: "host.isolate", TargetLabel: "WIN-DC01"}
	}
	retry := func() RequestAction { r := sampleRequest(uuid.New()); r.RetryOf = orig; return r }
	reversal := func() RequestAction {
		r := sampleRequest(uuid.New())
		r.ActionType, r.IsReversal, r.ReversalOfRef = "host.unisolate", true, orig
		return r
	}

	cases := []struct {
		name    string
		actions map[uuid.UUID]actionState
		cmd     RequestAction
		ok      bool
	}{
		{"re-request an expired action", map[uuid.UUID]actionState{orig: isolate(ActionStatusExpired)}, retry(), true},
		{"re-request a failed action", map[uuid.UUID]actionState{orig: isolate(ActionStatusFailed)}, retry(), true},
		{"re-request an action not in this investigation", map[uuid.UUID]actionState{}, retry(), false},
		{"re-request a succeeded action", map[uuid.UUID]actionState{orig: isolate(ActionStatusSucceeded)}, retry(), false},
		{"re-request a rejected action", map[uuid.UUID]actionState{orig: isolate(ActionStatusRejected)}, retry(), true},
		{"re-request a pending action whose window elapsed", map[uuid.UUID]actionState{orig: {Status: ActionStatusRequested, ActionType: "host.isolate", ExpiresAt: time.Now().Add(-time.Minute)}}, retry(), true},
		{"re-request a pending action still in its window", map[uuid.UUID]actionState{orig: {Status: ActionStatusRequested, ActionType: "host.isolate", ExpiresAt: time.Now().Add(time.Hour)}}, retry(), false},
		{"re-request as another type", map[uuid.UUID]actionState{orig: isolate(ActionStatusExpired)},
			func() RequestAction { r := retry(); r.ActionType = "ioc.block"; return r }(), false},
		{"re-request an action already re-requested", map[uuid.UUID]actionState{
			orig: isolate(ActionStatusExpired), sibling: {Status: ActionStatusRejected, ActionType: "host.isolate", RetryOf: orig},
		}, retry(), false},
		{"reverse a succeeded action", map[uuid.UUID]actionState{orig: isolate(ActionStatusSucceeded)}, reversal(), true},
		{"reverse an action not succeeded", map[uuid.UUID]actionState{orig: isolate(ActionStatusFailed)}, reversal(), false},
		{"reverse while a reversal is pending", map[uuid.UUID]actionState{
			orig: isolate(ActionStatusSucceeded), sibling: {Status: ActionStatusRequested, ActionType: "host.unisolate", ReversalOf: orig},
		}, reversal(), false},
		{"reverse after a reversal succeeded (best effort leaves it SUCCEEDED)", map[uuid.UUID]actionState{
			orig: isolate(ActionStatusSucceeded), sibling: {Status: ActionStatusSucceeded, ActionType: "host.unisolate", ReversalOf: orig},
		}, reversal(), false},
		{"reverse again after a reversal failed", map[uuid.UUID]actionState{
			orig: isolate(ActionStatusSucceeded), sibling: {Status: ActionStatusFailed, ActionType: "host.unisolate", ReversalOf: orig},
		}, reversal(), true},
		{"a fresh request is never limited", map[uuid.UUID]actionState{
			orig: isolate(ActionStatusSucceeded), sibling: {Status: ActionStatusSucceeded, ActionType: "host.isolate"},
		}, sampleRequest(uuid.New()), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := applyCommand(env, tc.cmd, state(tc.actions), testIDs())
			if tc.ok && err != nil {
				t.Errorf("want accepted, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Error("want rejected, got accepted")
			}
		})
	}
}

// TestFoldAction_KeepsLineage: the folded state remembers what each action
// re-requests or reverses, so the lineage check sees the whole chain.
func TestFoldAction_KeepsLineage(t *testing.T) {
	orig, retryID, revID := uuid.New(), uuid.New(), uuid.New()
	actions := map[uuid.UUID]actionState{}
	for _, p := range []ActionRequested{
		{ActionID: retryID, ActionType: "host.isolate", RetryOf: orig},
		{ActionID: revID, ActionType: "host.unisolate", IsReversal: true, ReversalOfRef: orig},
	} {
		raw, _ := json.Marshal(p)
		if err := foldActionEvent(actions, Event{Type: EventTypeActionRequested, Payload: raw}); err != nil {
			t.Fatal(err)
		}
	}
	if actions[retryID].RetryOf != orig || actions[revID].ReversalOf != orig {
		t.Errorf("lineage not folded: retry=%v reversal=%v", actions[retryID].RetryOf, actions[revID].ReversalOf)
	}
}

// TestDispatchSummary: a dispatch names its tool, and one that reached no
// tool never reads "dispatched via" with nothing after it (#153).
func TestDispatchSummary(t *testing.T) {
	if got := dispatchSummary("host.isolate → WIN-FILE01", "fixture_write"); got != "host.isolate → WIN-FILE01: dispatched via fixture_write" {
		t.Errorf("with adapter: %q", got)
	}
	if got := dispatchSummary("host.unisolate → WIN-FILE01", ""); got != "host.unisolate → WIN-FILE01: dispatched" {
		t.Errorf("without adapter: %q", got)
	}
}

// TestExpireAction_PrincipalIsRequester: the fold remembers who requested an
// action, and an expiry, which nobody approved, is recorded only on the
// requester's behalf (adr/0217).
func TestExpireAction_PrincipalIsRequester(t *testing.T) {
	alice := newTestEnvelope("alice")
	id := uuid.New()
	events, err := applyCommand(alice, sampleRequest(id), activeStateWithAction(alice, id, ""), testIDs())
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	actions := map[uuid.UUID]actionState{}
	if err := foldActionEvent(actions, events[0]); err != nil {
		t.Fatalf("fold: %v", err)
	}
	if actions[id].RequestedBy != "alice" {
		t.Fatalf("folded requester %q; want alice", actions[id].RequestedBy)
	}

	state := activeStateWithAction(alice, id, "")
	state.Actions[id] = actions[id]
	sys := func(principal string) Envelope {
		env := alice
		env.Actor = Actor{PrincipalID: principal, Kind: ActorSystem}
		return env
	}
	if _, err := applyCommand(sys("system"), ExpireAction{ActionID: id}, state, testIDs()); err == nil {
		t.Error("an expiry naming no human was accepted")
	}
	if _, err := applyCommand(sys("bob"), ExpireAction{ActionID: id}, state, testIDs()); err == nil {
		t.Error("an expiry on behalf of someone other than the requester was accepted")
	}
	expired, err := applyCommand(sys("alice"), ExpireAction{ActionID: id}, state, testIDs())
	if err != nil {
		t.Fatalf("expire on the requester's behalf: %v", err)
	}
	for _, e := range expired {
		if e.Actor.PrincipalID != "alice" || e.Actor.Kind != ActorSystem {
			t.Errorf("%s actor %+v; want SYSTEM on alice's behalf", e.Type, e.Actor)
		}
	}
}
