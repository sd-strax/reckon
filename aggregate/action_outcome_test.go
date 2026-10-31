package aggregate

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestRecordOutcomeUnknown: SYSTEM-only, legal only from EXECUTING, and the
// action folds to OUTCOME_UNKNOWN (adr/0216).
func TestRecordOutcomeUnknown(t *testing.T) {
	id := uuid.New()
	sys := newTestEnvelope("alice")
	sys.Actor.Kind = ActorSystem
	executing := activeStateWithAction(sys, id, ActionStatusExecuting)

	events, err := applyCommand(sys, RecordOutcomeUnknown{ActionID: id, Detail: "the run stopped"}, executing, testIDs())
	if err != nil {
		t.Fatalf("record outcome unknown: %v", err)
	}
	if events[0].Type != EventTypeActionOutcomeUnknown {
		t.Errorf("domain type = %q; want %q", events[0].Type, EventTypeActionOutcomeUnknown)
	}
	if got := foldInto(executing.Actions, events)[id].Status; got != ActionStatusOutcomeUnknown {
		t.Errorf("status = %q; want OUTCOME_UNKNOWN", got)
	}

	for _, from := range []string{ActionStatusApproved, ActionStatusSucceeded, ActionStatusFailed, ActionStatusOutcomeUnknown} {
		if _, err := applyCommand(sys, RecordOutcomeUnknown{ActionID: id}, activeStateWithAction(sys, id, from), testIDs()); err == nil {
			t.Errorf("outcome unknown from %s should be rejected (EXECUTING only)", from)
		}
	}

	if _, err := applyCommand(newTestEnvelope("alice"), RecordOutcomeUnknown{ActionID: id}, executing, testIDs()); err == nil {
		t.Error("a human RecordOutcomeUnknown should be rejected (system-only)")
	}
}

// TestResolveActionOutcome: only a person resolves, only from OUTCOME_UNKNOWN,
// to SUCCEEDED or FAILED, saying what they checked (adr/0216).
func TestResolveActionOutcome(t *testing.T) {
	id := uuid.New()
	human := newTestEnvelope("alice")
	unknown := activeStateWithAction(human, id, ActionStatusOutcomeUnknown)
	resolve := func(outcome string) ResolveActionOutcome {
		return ResolveActionOutcome{ActionID: id, Outcome: outcome, Note: "EDR console shows the host isolated"}
	}

	for _, outcome := range []string{ActionStatusSucceeded, ActionStatusFailed} {
		events, err := applyCommand(human, resolve(outcome), unknown, testIDs())
		if err != nil {
			t.Fatalf("resolve %s: %v", outcome, err)
		}
		if events[0].Type != EventTypeActionOutcomeResolved {
			t.Errorf("domain type = %q; want %q", events[0].Type, EventTypeActionOutcomeResolved)
		}
		if got := foldInto(unknown.Actions, events)[id].Status; got != outcome {
			t.Errorf("status after resolving %s = %q", outcome, got)
		}
	}

	rejected := map[string]struct {
		env   Envelope
		cmd   ResolveActionOutcome
		state aggregateState
	}{
		"from EXECUTING": {human, resolve(ActionStatusSucceeded), activeStateWithAction(human, id, ActionStatusExecuting)},
		"from FAILED":    {human, resolve(ActionStatusSucceeded), activeStateWithAction(human, id, ActionStatusFailed)},
		"outcome PARTIAL": {human, func() ResolveActionOutcome {
			c := resolve(ActionStatusSucceeded)
			c.Outcome = "PARTIAL"
			return c
		}(), unknown},
		"no note": {human, func() ResolveActionOutcome { c := resolve(ActionStatusFailed); c.Note = "  "; return c }(), unknown},
		"artifact link carrying a credential": {human, func() ResolveActionOutcome {
			c := resolve(ActionStatusSucceeded)
			c.Artifact = &ArtifactLocation{Location: "https://bucket.example/cap.png?X-Amz-Signature=abc"}
			return c
		}(), unknown},
		"by the system":     {func() Envelope { e := human; e.Actor.Kind = ActorSystem; return e }(), resolve(ActionStatusSucceeded), unknown},
		"by an AI delegate": {func() Envelope { e := human; e.Actor.Kind = ActorAIDelegated; return e }(), resolve(ActionStatusSucceeded), unknown},
	}
	for name, tc := range rejected {
		if _, err := applyCommand(tc.env, tc.cmd, tc.state, testIDs()); err == nil {
			t.Errorf("%s: want rejected, got accepted", name)
		}
	}
}

// TestOutcomeUnknown_NoBlindRerun: an action whose outcome is unknown can be
// neither re-requested nor reversed; once a person resolves it, the usual
// lineage applies — FAILED may be re-requested, SUCCEEDED reversed (adr/0216).
func TestOutcomeUnknown_NoBlindRerun(t *testing.T) {
	env := newTestEnvelope("alice")
	orig := uuid.New()
	state := func(status string) aggregateState {
		return aggregateState{Seq: 4, Exists: true, TenantID: env.TenantID, Status: StatusActive,
			Actions: map[uuid.UUID]actionState{orig: {Status: status, Tier: TierT2, ActionType: "host.isolate"}}}
	}
	retry := sampleRequest(uuid.New())
	retry.RetryOf = orig
	reversal := sampleRequest(uuid.New())
	reversal.ActionType, reversal.IsReversal, reversal.ReversalOfRef = "host.unisolate", true, orig

	if _, err := applyCommand(env, retry, state(ActionStatusOutcomeUnknown), testIDs()); err == nil {
		t.Error("re-requesting an action whose outcome is unknown should be rejected")
	}
	if _, err := applyCommand(env, reversal, state(ActionStatusOutcomeUnknown), testIDs()); err == nil {
		t.Error("reversing an action whose outcome is unknown should be rejected")
	}
	if _, err := applyCommand(env, retry, state(ActionStatusFailed), testIDs()); err != nil {
		t.Errorf("re-request after resolving FAILED: %v", err)
	}
	if _, err := applyCommand(env, reversal, state(ActionStatusSucceeded), testIDs()); err != nil {
		t.Errorf("reversal after resolving SUCCEEDED: %v", err)
	}

	// A reversal whose own outcome is unknown blocks another reversal: nobody
	// knows whether the first undo happened.
	pending := state(ActionStatusSucceeded)
	pending.Actions[uuid.New()] = actionState{Status: ActionStatusOutcomeUnknown, ActionType: "host.unisolate", ReversalOf: orig}
	if _, err := applyCommand(env, reversal, pending, testIDs()); err == nil {
		t.Error("a second reversal while the first's outcome is unknown should be rejected")
	}
}

// TestOutcomeUnknown_ProjectionAndSweep: the projection carries the status and
// its reason, the recorded outcome is readable after a resolution, and the
// stranded-action listing holds APPROVED and EXECUTING actions but not one
// waiting for a person (adr/0216).
func TestOutcomeUnknown_ProjectionAndSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	h := newTestHandler()
	aggID := mustActivate(t, h)

	approved := func() uuid.UUID {
		id := uuid.New()
		mustHandle(t, h, cmdEnv(aggID), RequestAction{
			ActionID: id, ActionType: "host.isolate", Tier: TierT2,
			Targets:   []TargetSpec{{EntityRef: "x-host--1", ResolvedIdentifier: "WIN-DC01"}},
			ExpiresAt: time.Now().Add(time.Hour), Rationale: "contain",
		})
		mustHandle(t, h, cmdEnv(aggID), ApproveAction{ActionID: id, Authorization: Authorization{
			Mode: AuthModeManual, Stage: AuthStageSolo, PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now(),
		}})
		return id
	}
	waiting, executing, unknown := approved(), approved(), approved()
	for _, id := range []uuid.UUID{executing, unknown} {
		mustHandle(t, h, sysEnv2(aggID), DispatchAction{ActionID: id, Adapter: "fx", AdapterRequestID: "wf-" + id.String()})
	}
	mustHandle(t, h, sysEnv2(aggID), RecordOutcomeUnknown{ActionID: unknown, Detail: "the run stopped"})

	ac, err := testReads().LoadActionCurrent(ctx, unknown)
	if err != nil {
		t.Fatalf("LoadActionCurrent: %v", err)
	}
	if ac.Status != ActionStatusOutcomeUnknown || ac.ErrorDetail != "the run stopped" {
		t.Errorf("projected status=%q detail=%q; want OUTCOME_UNKNOWN with its reason", ac.Status, ac.ErrorDetail)
	}

	stranded, err := testReads().ListStrandedActionCurrents(ctx)
	if err != nil {
		t.Fatalf("ListStrandedActionCurrents: %v", err)
	}
	got := map[uuid.UUID]bool{}
	for _, a := range stranded {
		got[a.ActionID] = true
	}
	if !got[waiting] || !got[executing] || got[unknown] || len(got) != 2 {
		t.Errorf("stranded = %v; want the APPROVED and the EXECUTING action only", got)
	}

	mustHandle(t, h, cmdEnv(aggID), ResolveActionOutcome{ActionID: unknown, Outcome: ActionStatusFailed, Note: "the console shows no isolation"})
	ac, err = testReads().LoadActionCurrent(ctx, unknown)
	if err != nil {
		t.Fatalf("LoadActionCurrent after resolve: %v", err)
	}
	if ac.Status != ActionStatusFailed || ac.ErrorDetail != "the console shows no isolation" {
		t.Errorf("after resolve status=%q detail=%q; want FAILED with the person's finding", ac.Status, ac.ErrorDetail)
	}
	if outcome, err := testReads().LoadRecordedOutcome(ctx, unknown); err != nil || outcome != ActionStatusFailed {
		t.Errorf("recorded outcome = %q, %v; want FAILED", outcome, err)
	}
	if outcome, err := testReads().LoadRecordedOutcome(ctx, waiting); err != nil || outcome != "" {
		t.Errorf("recorded outcome of an undispatched action = %q, %v; want empty", outcome, err)
	}
}

// TestStrandedReversal: a reversing action that SUCCEEDED while its original
// was never marked is listed so its saga can finish; a PARTIAL reversal, which
// leaves the original untouched by design, is not (adr/0216).
func TestStrandedReversal(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	h := newTestHandler()
	aggID := mustActivate(t, h)

	run := func(req RequestAction, outcome string) {
		mustHandle(t, h, cmdEnv(aggID), req)
		mustHandle(t, h, cmdEnv(aggID), ApproveAction{ActionID: req.ActionID, Authorization: Authorization{
			Mode: AuthModeManual, Stage: AuthStageSolo, PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now(),
		}})
		mustHandle(t, h, sysEnv2(aggID), DispatchAction{ActionID: req.ActionID, Adapter: "fx", AdapterRequestID: "wf-" + req.ActionID.String()})
		mustHandle(t, h, sysEnv2(aggID), ResultAction{ActionID: req.ActionID, FinalOutcome: outcome, Attempts: 1})
	}
	reverse := func(orig uuid.UUID) RequestAction {
		r := sampleRequest(uuid.New())
		r.ActionType, r.IsReversal, r.ReversalOfRef = "host.unisolate", true, orig
		return r
	}

	origA, origB := sampleRequest(uuid.New()), sampleRequest(uuid.New())
	run(origA, "SUCCEEDED")
	run(origB, "SUCCEEDED")
	revA, revB := reverse(origA.ActionID), reverse(origB.ActionID)
	run(revA, "SUCCEEDED")
	run(revB, "PARTIAL")

	stranded, err := testReads().ListStrandedActionCurrents(ctx)
	if err != nil {
		t.Fatalf("ListStrandedActionCurrents: %v", err)
	}
	if len(stranded) != 1 || stranded[0].ActionID != revA.ActionID {
		ids := make([]uuid.UUID, 0, len(stranded))
		for _, a := range stranded {
			ids = append(ids, a.ActionID)
		}
		t.Fatalf("stranded = %v; want only the SUCCEEDED reversal %v", ids, revA.ActionID)
	}

	// Once the saga marks the original, the reversal is no longer stranded.
	mustHandle(t, h, sysEnv2(aggID), ReverseAction{OriginalActionID: origA.ActionID, ReversingActionID: revA.ActionID})
	if stranded, err = testReads().ListStrandedActionCurrents(ctx); err != nil || len(stranded) != 0 {
		t.Errorf("after the original is marked: stranded = %d, %v; want none", len(stranded), err)
	}
}
