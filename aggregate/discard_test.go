package aggregate

import (
	"testing"

	"github.com/google/uuid"
)

// TestDiscardInvestigation: only a draft can be discarded, not while a request
// awaits a decision, and nothing is accepted afterwards (adr/0161).
func TestDiscardInvestigation(t *testing.T) {
	env := newTestEnvelope("alice")
	draft := func(actions map[uuid.UUID]actionState) aggregateState {
		return aggregateState{Seq: 1, Exists: true, TenantID: env.TenantID, Status: StatusDraft, Actions: actions}
	}
	events, err := applyCommand(env, DiscardInvestigation{Reason: "mistyped host"}, draft(map[uuid.UUID]actionState{}), testIDs())
	if err != nil || len(events) != 2 || events[0].Type != EventTypeStatusChanged {
		t.Fatalf("discard a draft: %v %v", events, err)
	}
	pending := map[uuid.UUID]actionState{uuid.New(): {Status: ActionStatusRequested, ActionType: "host.isolate", TargetLabel: "WIN-FILE)1"}}
	if _, err := applyCommand(env, DiscardInvestigation{}, draft(pending), testIDs()); err == nil {
		t.Error("discard with a pending request accepted")
	}
	active := aggregateState{Seq: 1, Exists: true, TenantID: env.TenantID, Status: StatusActive, Actions: map[uuid.UUID]actionState{}}
	if _, err := applyCommand(env, DiscardInvestigation{}, active, testIDs()); err == nil {
		t.Error("discard of an active investigation accepted")
	}
	discarded := aggregateState{Seq: 3, Exists: true, TenantID: env.TenantID, Status: StatusDiscarded, Actions: map[uuid.UUID]actionState{}}
	if _, err := applyCommand(env, RenameInvestigation{Title: "x"}, discarded, testIDs()); err == nil {
		t.Error("a discarded investigation accepted a command")
	}
	if aiAllowed(DiscardInvestigation{}) {
		t.Error("an AI delegate must not discard an investigation")
	}
}
