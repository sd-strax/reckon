package aggregate

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestAlreadyApplied: a SYSTEM command whose correlation id is already on the
// stream is recognised as recorded when the event matches the command, and
// refused when it does not. Other actors are never deduplicated (#215).
func TestAlreadyApplied(t *testing.T) {
	sys := newTestEnvelope("alice")
	sys.Actor.Kind = ActorSystem
	other := uuid.New()
	stream := []Event{
		{SequenceNo: 1, Type: EventTypeCreated, CorrelationID: other},
		{SequenceNo: 2, Type: EventTypeActionDispatched, CorrelationID: sys.CorrelationID},
		{SequenceNo: 3, Type: EventTypeInterpretationRecorded, CorrelationID: sys.CorrelationID},
	}
	dispatch := DispatchAction{ActionID: uuid.New(), AdapterRequestID: "wf-1"}

	done, err := alreadyApplied(sys, dispatch, stream)
	if err != nil || !done {
		t.Errorf("repeat of a recorded dispatch: done=%v err=%v; want true, nil", done, err)
	}

	if _, err := alreadyApplied(sys, ResultAction{ActionID: dispatch.ActionID, FinalOutcome: "SUCCEEDED"}, stream); err == nil {
		t.Error("a correlation id reused by a different command should be rejected")
	}

	fresh := sys
	fresh.CorrelationID = uuid.New()
	if done, err := alreadyApplied(fresh, dispatch, stream); err != nil || done {
		t.Errorf("unseen correlation id: done=%v err=%v; want false, nil", done, err)
	}

	human := sys
	human.Actor.Kind = ActorHuman
	if done, err := alreadyApplied(human, dispatch, stream); err != nil || done {
		t.Errorf("HUMAN envelope: done=%v err=%v; want false, nil (never deduplicated)", done, err)
	}
}

// TestHandle_SystemRetryIsNoOp: the lost-ack retry. A dispatch committed, its
// completion never reached Temporal, and the activity runs again with the same
// correlation id: Handle succeeds, appends nothing, and the action stays
// EXECUTING with one dispatched event on the stream (#215).
func TestHandle_SystemRetryIsNoOp(t *testing.T) {
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
		Targets:   []TargetSpec{{EntityRef: "x-host--1", ResolvedIdentifier: "WIN-DC01"}},
		ExpiresAt: time.Now().Add(time.Hour), Rationale: "contain",
	})
	mustHandle(t, h, cmdEnv(aggID), ApproveAction{
		ActionID: actionID,
		Authorization: Authorization{
			Mode: AuthModeManual, Stage: AuthStageSolo,
			PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now(),
		},
	})

	sys := sysEnv2(aggID)
	dispatch := DispatchAction{ActionID: actionID, Adapter: "fx", AdapterRequestID: "wf-1"}
	first := mustHandle(t, h, sys, dispatch)

	retry, err := h.Handle(ctx, sys, dispatch)
	if err != nil {
		t.Fatalf("retry with the same correlation id: %v", err)
	}
	if len(retry.AppliedEvents) != 0 {
		t.Errorf("retry applied %d events; want 0", len(retry.AppliedEvents))
	}
	if retry.NewSequenceNo != first.NewSequenceNo {
		t.Errorf("retry sequence = %d; want %d (unchanged)", retry.NewSequenceNo, first.NewSequenceNo)
	}

	// A new correlation id is a different command: the state check refuses it.
	if _, err := h.Handle(ctx, sysEnv2(aggID), dispatch); err == nil {
		t.Error("a second dispatch under a new correlation id should be rejected (not APPROVED)")
	}

	ac, err := LoadActionCurrent(ctx, testDB, actionID)
	if err != nil {
		t.Fatalf("LoadActionCurrent: %v", err)
	}
	if ac.Status != ActionStatusExecuting {
		t.Errorf("status = %q; want EXECUTING", ac.Status)
	}
}
