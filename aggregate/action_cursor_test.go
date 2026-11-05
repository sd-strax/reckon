package aggregate

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestActionStatusEvents_WriteTheInvestigationRow: every command that changes
// an action's status emits an interpretation record in the same transaction,
// and the investigation projector advances its cursor on that record, so every
// such transaction writes the investigation's investigation_current row. The
// dependency store (dependency/) relies on this: it takes a share lock on that
// row as its one serialization point against the aggregate and reads an
// action's status under it without locking action_current. A command that
// changed an action's status without an interpretation would silently take
// that guarantee away; this test is what fails instead.
func TestActionStatusEvents_WriteTheInvestigationRow(t *testing.T) {
	human := newTestEnvelope("alice")
	human.Actor.Kind = ActorHuman
	sys := newTestEnvelope("alice")
	sys.Actor.Kind = ActorSystem
	id, orig, reversing := uuid.New(), uuid.New(), uuid.New()
	manual := Authorization{Mode: AuthModeManual, Stage: AuthStageSolo, PrimaryApproverRef: "alice", PrimaryApprovedAt: time.Now()}

	draft := activeStateWithAction(human, id, ActionStatusRequested)
	draft.Status = StatusDraft
	expired := activeStateWithAction(human, id, ActionStatusRequested)
	a := expired.Actions[id]
	a.ExpiresAt, a.RequestedBy = time.Now().Add(-time.Hour), "alice"
	expired.Actions[id] = a
	reversal := activeStateWithAction(sys, orig, ActionStatusSucceeded)
	reversal.Actions[reversing] = actionState{Status: ActionStatusSucceeded, Tier: TierT2, IsReversal: true, ReversalOf: orig}

	cases := []struct {
		name  string
		env   Envelope
		cmd   Command
		state aggregateState
	}{
		{"request", human, sampleRequest(id), activeStateWithAction(human, id, "")},
		{"approve, active", human, ApproveAction{ActionID: id, Authorization: manual}, activeStateWithAction(human, id, ActionStatusRequested)},
		{"approve, draft (activates first)", human, ApproveAction{ActionID: id, Authorization: manual}, draft},
		{"reject", human, RejectAction{ActionID: id, Reason: "no"}, activeStateWithAction(human, id, ActionStatusRequested)},
		{"expire", sys, ExpireAction{ActionID: id}, expired},
		{"dispatch", sys, DispatchAction{ActionID: id, Adapter: "fx", AdapterRequestID: "wf-1"}, activeStateWithAction(sys, id, ActionStatusApproved)},
		{"result", sys, ResultAction{ActionID: id, FinalOutcome: ActionStatusSucceeded, Attempts: 1}, activeStateWithAction(sys, id, ActionStatusExecuting)},
		{"outcome unknown", sys, RecordOutcomeUnknown{ActionID: id, Detail: "the run stopped"}, activeStateWithAction(sys, id, ActionStatusExecuting)},
		{"outcome resolved", human, ResolveActionOutcome{ActionID: id, Outcome: ActionStatusSucceeded, Note: "checked"}, activeStateWithAction(human, id, ActionStatusOutcomeUnknown)},
		{"reversed", sys, ReverseAction{OriginalActionID: orig, ReversingActionID: reversing}, reversal},
	}
	for _, c := range cases {
		events, err := applyCommand(c.env, c.cmd, c.state, testIDs())
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		interp := false
		for _, e := range events {
			if e.Type == EventTypeInterpretationRecorded {
				interp = true
			}
		}
		if !interp {
			t.Errorf("%s: %d event(s) and no %s; the transaction would not write investigation_current", c.name, len(events), EventTypeInterpretationRecorded)
		}
	}
}
