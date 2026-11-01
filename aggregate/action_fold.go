package aggregate

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// actionState is one x-action's folded state within the aggregate.
// PrimaryApprover is retained from the primary approval so the secondary stage
// can enforce two-party integrity (distinct approvers, consistent audit record)
// against authoritative state rather than trusting the caller's payload.
type actionState struct {
	Status                string
	Tier                  string
	IsReversal            bool
	PrimaryApprover       string
	RequiredMode          string   // Gate 2 requirement, frozen at request time
	SecondaryApproverPool []string // who may be the secondary approver (TWO_PARTY)
	ActionType            string   // e.g. "notify.slack" — for human-readable summaries
	// RequestedBy is the principal of the request: the named human an expiry,
	// which nobody approved, is recorded on behalf of (adr/0217).
	RequestedBy string
	TargetLabel string    // e.g. "#it-operations (+2)" — the target set, condensed
	RetryOf     uuid.UUID // the action this one re-requests (lineage, adr/0152)
	ExpiresAt   time.Time // the approval deadline, for a re-request of a lazily expired action
	ReversalOf  uuid.UUID // the action this one reverses (lineage, adr/0152)
	// ReversalAttemptedBy is the reversing action whose unverified attempt is
	// recorded on this one (04 §7.1 Position C); zero when none. Status stays
	// SUCCEEDED, so this is what lets a repeat attempt be recognised.
	ReversalAttemptedBy uuid.UUID
}

// label renders the action as "<type> → <target>" (or just the type when
// target-less), so lifecycle summaries read as the act rather than a UUID.
func (s actionState) label() string {
	if s.TargetLabel == "" {
		return s.ActionType
	}
	return s.ActionType + " → " + s.TargetLabel
}

// targetLabel condenses a target set to its first resolved identifier plus a
// count of the rest ("#it-operations (+2)"), for summaries.
func targetLabel(targets []TargetSpec) string {
	if len(targets) == 0 {
		return ""
	}
	first := targets[0].ResolvedIdentifier
	if first == "" {
		first = targets[0].EntityRef
	}
	if len(targets) > 1 {
		return fmt.Sprintf("%s (+%d)", first, len(targets)-1)
	}
	return first
}

// approvalStatus is the status an approval leaves an action in: a two-party
// action's primary approval waits for the second approver.
func approvalStatus(a Authorization) string {
	if a.Mode == AuthModeTwoParty && a.Stage == AuthStagePrimary {
		return ActionStatusPendingSecondary
	}
	return ActionStatusApproved
}

// resultStatus maps a final outcome to the action's status, per 08 §7.1:
// PARTIAL terminates SUCCEEDED (with the partial detail on the event); TIMEOUT
// terminates FAILED with no success inference (04 §6.2). Anything else is
// FAILED for the same reason.
func resultStatus(finalOutcome string) string {
	switch finalOutcome {
	case "SUCCEEDED", "PARTIAL":
		return ActionStatusSucceeded
	default: // FAILED, TIMEOUT
		return ActionStatusFailed
	}
}

// foldActionEvent applies one action event to the per-action state map. Kept
// separate from foldState so the switch there stays readable. Every action.*
// type the projectors see is folded here: action.reversal_attempted leaves the
// original SUCCEEDED (04 §7.1) but records the attempt, so a repeat is
// recognised rather than recorded twice.
func foldActionEvent(actions map[uuid.UUID]actionState, e Event) error {
	get := func(id uuid.UUID) actionState { return actions[id] }
	set := func(id uuid.UUID, s actionState) { actions[id] = s }

	switch e.Type {
	case EventTypeActionRequested:
		var p ActionRequested
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("fold action.requested seq %d: %w", e.SequenceNo, err)
		}
		set(p.ActionID, actionState{
			Status: ActionStatusRequested, Tier: p.Tier, IsReversal: p.IsReversal, RequestedBy: e.Actor.PrincipalID,
			RequiredMode: p.RequiredMode, SecondaryApproverPool: p.SecondaryApproverPool,
			ActionType: p.ActionType, TargetLabel: targetLabel(p.Targets),
			RetryOf: p.RetryOf, ReversalOf: p.ReversalOfRef, ExpiresAt: p.ExpiresAt,
		})
	case EventTypeActionApproved:
		var p ActionApproved
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("fold action.approved seq %d: %w", e.SequenceNo, err)
		}
		s := get(p.ActionID)
		s.PrimaryApprover = p.Authorization.PrimaryApproverRef
		s.Status = approvalStatus(p.Authorization)
		set(p.ActionID, s)
	case EventTypeActionEdited:
		var p ActionEdited
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("fold action.edited seq %d: %w", e.SequenceNo, err)
		}
		s := get(p.ActionID)
		s.RequiredMode, s.SecondaryApproverPool = p.RequiredMode, p.SecondaryApproverPool
		set(p.ActionID, s)
	case EventTypeActionRejected:
		var p ActionRejected
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("fold action.rejected seq %d: %w", e.SequenceNo, err)
		}
		s := get(p.ActionID)
		s.Status = ActionStatusRejected
		set(p.ActionID, s)
	case EventTypeActionExpired:
		var p ActionExpired
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("fold action.expired seq %d: %w", e.SequenceNo, err)
		}
		s := get(p.ActionID)
		s.Status = ActionStatusExpired
		set(p.ActionID, s)
	case EventTypeActionDispatched:
		var p ActionDispatched
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("fold action.dispatched seq %d: %w", e.SequenceNo, err)
		}
		s := get(p.ActionID)
		s.Status = ActionStatusExecuting
		set(p.ActionID, s)
	case EventTypeActionResulted:
		var p ActionResulted
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("fold action.resulted seq %d: %w", e.SequenceNo, err)
		}
		s := get(p.ActionID)
		s.Status = resultStatus(p.FinalOutcome)
		set(p.ActionID, s)
	case EventTypeActionReversed:
		var p ActionReversed
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("fold action.reversed seq %d: %w", e.SequenceNo, err)
		}
		s := get(p.OriginalActionID)
		s.Status = ActionStatusReversed
		set(p.OriginalActionID, s)
	case EventTypeActionReversalAttempted:
		var p ActionReversalAttempted
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("fold action.reversal_attempted seq %d: %w", e.SequenceNo, err)
		}
		s := get(p.OriginalActionID)
		s.ReversalAttemptedBy = p.ReversingActionID
		set(p.OriginalActionID, s)
	case EventTypeActionOutcomeUnknown:
		var p ActionOutcomeUnknown
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("fold action.outcome_unknown seq %d: %w", e.SequenceNo, err)
		}
		s := get(p.ActionID)
		s.Status = ActionStatusOutcomeUnknown
		set(p.ActionID, s)
	case EventTypeActionOutcomeResolved:
		var p ActionOutcomeResolved
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("fold action.outcome_resolved seq %d: %w", e.SequenceNo, err)
		}
		s := get(p.ActionID)
		s.Status = p.Outcome
		set(p.ActionID, s)
	}
	return nil
}
