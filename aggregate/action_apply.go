package aggregate

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// applyRequestAction builds the (ActionRequested, action-request Interpretation)
// pair. Permitted while the investigation is ACTIVE, or CONCLUDED when the
// request is a reversal (04 §9.1).
func applyRequestAction(env Envelope, state aggregateState, c RequestAction, newID idSource) ([]Event, error) {
	switch {
	case state.Status == StatusActive:
	case state.Status == StatusDraft: // invisible activate (01 §Extension 2): a
	// draft accepts an action REQUEST; the investigation activates when that
	// action is first approved (applyApproveAction), the reasoning→acting boundary.
	case state.Status == StatusConcluded && c.IsReversal:
	default:
		return nil, fmt.Errorf("RequestAction rejected: investigation %s is %s (actions require ACTIVE or DRAFT, or CONCLUDED for a reversal)", env.AggregateID, state.Status)
	}
	if _, exists := state.Actions[c.ActionID]; exists {
		return nil, fmt.Errorf("RequestAction rejected: action %s already exists", c.ActionID)
	}
	if err := checkLineage(env, state, c); err != nil {
		return nil, err
	}

	interpID := newID()
	events, err := actEvents(env, state.Seq, EventTypeActionRequested, ActionRequested{
		ActionID:                   c.ActionID,
		ActionType:                 c.ActionType,
		Tier:                       c.Tier,
		Reversibility:              c.Reversibility,
		Targets:                    c.Targets,
		Parameters:                 c.Parameters,
		EvidenceRefs:               c.EvidenceRefs,
		ExpiresAt:                  c.ExpiresAt,
		IsReversal:                 c.IsReversal,
		ReversalOfRef:              c.ReversalOfRef,
		RetryOf:                    c.RetryOf,
		RequiredMode:               c.RequiredMode,
		SecondaryApproverPool:      c.SecondaryApproverPool,
		RequestingInterpretationID: interpID,
	}, interpID, InterpretationActionRequest, c.Rationale, c.EvidenceRefs)
	if err != nil {
		return nil, err
	}

	// If Gate 2 ran, record its evaluation in the same transaction (02 §3).
	if len(c.PolicyEvaluations) > 0 {
		pePayload, err := json.Marshal(ActionPolicyEvaluated{
			ActionID:         c.ActionID,
			Evaluations:      c.PolicyEvaluations,
			MatchedPolicyRef: c.MatchedPolicyRef,
		})
		if err != nil {
			return nil, err
		}
		events = append(events, lifecycleDomainEvent(env, state.Seq+3, EventTypeActionPolicyEvaluated, pePayload))
	}
	return events, nil
}

// dispatchSummary names the tool an action went to; a dispatch that reached no
// tool says only that it was dispatched, never "via" nothing (#153).
func dispatchSummary(label, adapter string) string {
	if adapter == "" {
		return label + ": dispatched"
	}
	return label + ": dispatched via " + adapter
}

// checkLineage keeps every action chain a line (adr/0152). A re-request names
// an action of the same type in this investigation that is EXPIRED, FAILED or
// REJECTED — or still pending with its approval window elapsed (the expiry
// timer has not written EXPIRED yet) — and has not been re-requested already: each action has at most one successor, so the
// chain continues from its latest link and an answered request is never asked
// again from behind it. A reversal names a SUCCEEDED action that has no
// reversal pending or done; a failed, rejected or expired reversal may be tried
// again. Checked here, against the folded stream and inside the command's
// transaction, so two requests racing on one original cannot both pass. A
// fresh request (no lineage) is never limited: sending the same thing again
// on purpose is the analyst's call.
func checkLineage(env Envelope, state aggregateState, c RequestAction) error {
	if c.RetryOf != uuid.Nil {
		orig, ok := state.Actions[c.RetryOf]
		lapsed := (orig.Status == ActionStatusRequested || orig.Status == ActionStatusPendingSecondary) &&
			!orig.ExpiresAt.IsZero() && orig.ExpiresAt.Before(env.OccurredAt)
		switch {
		case !ok:
			return fmt.Errorf("RequestAction rejected: re-requested action %s is not in this investigation", c.RetryOf)
		case orig.Status != ActionStatusExpired && orig.Status != ActionStatusFailed && orig.Status != ActionStatusRejected && !lapsed:
			return fmt.Errorf("RequestAction rejected: %s is %s; only an EXPIRED, FAILED or REJECTED action, or one whose approval window has elapsed, can be re-requested", orig.label(), orig.Status)
		case orig.ActionType != c.ActionType:
			return fmt.Errorf("RequestAction rejected: a re-request of %s must be %s, not %s", orig.label(), orig.ActionType, c.ActionType)
		}
		for id, a := range state.Actions {
			if a.RetryOf == c.RetryOf {
				return fmt.Errorf("RequestAction rejected: %s was already re-requested as %s (%s)", orig.label(), id, a.Status)
			}
		}
	}
	if c.ReversalOfRef != uuid.Nil {
		orig, ok := state.Actions[c.ReversalOfRef]
		switch {
		case !ok:
			return fmt.Errorf("RequestAction rejected: action %s to reverse is not in this investigation", c.ReversalOfRef)
		case orig.Status != ActionStatusSucceeded:
			return fmt.Errorf("RequestAction rejected: %s is %s; only a SUCCEEDED action can be reversed", orig.label(), orig.Status)
		}
		for id, a := range state.Actions {
			if a.ReversalOf != c.ReversalOfRef {
				continue
			}
			switch a.Status {
			case ActionStatusFailed, ActionStatusRejected, ActionStatusExpired:
			default:
				return fmt.Errorf("RequestAction rejected: %s already has a reversal, %s (%s)", orig.label(), id, a.Status)
			}
		}
	}
	return nil
}

// applyApproveAction builds the (ActionApproved, action-approval Interpretation)
// pair, enforcing the actor/approver invariant (04 §3.3): the acting principal
// must equal the approver whose stage this approval records.
func applyApproveAction(env Envelope, state aggregateState, c ApproveAction, newID idSource) ([]Event, error) {
	act, ok := state.Actions[c.ActionID]
	if !ok {
		return nil, fmt.Errorf("ApproveAction rejected: action %s does not exist", c.ActionID)
	}

	stage := c.Authorization.Stage
	if err := checkApprover(env, c.Authorization, stage); err != nil {
		return nil, err
	}

	// Two-party requirement (04 §5.6, the Phase D approval seam): an action whose
	// frozen Gate 2 requirement is TWO_PARTY can NEVER be approved with a solo
	// (MANUAL/AUTO_POLICY) authorization — that would collapse the two-person
	// integrity the policy demanded into one. Enforced here at the aggregate
	// boundary, the single write path, so no approval surface can bypass it.
	if act.RequiredMode == AuthModeTwoParty && c.Authorization.Mode != AuthModeTwoParty {
		return nil, fmt.Errorf("ApproveAction rejected: action %s requires TWO_PARTY authorization, got a %s approval", c.ActionID, c.Authorization.Mode)
	}

	// Legal source states: REQUESTED for a solo/primary approval; PENDING_SECONDARY
	// for a two-party secondary approval.
	switch {
	case act.Status == ActionStatusRequested && stage != AuthStageSecondary:
	case act.Status == ActionStatusPendingSecondary && stage == AuthStageSecondary:
	default:
		return nil, fmt.Errorf("ApproveAction rejected: action %s is %s, cannot record a %s approval", c.ActionID, act.Status, stage)
	}

	// Two-party integrity (04 §3.2/§3.3): the secondary approver must be a
	// DIFFERENT human than the primary — one analyst approving both stages
	// defeats the mode — and the secondary event's primary_approver_ref must
	// match the primary approval actually recorded in the stream (checked
	// against folded state, never the caller's payload), so the audit chain
	// cannot be rewritten at the second stage.
	if stage == AuthStageSecondary {
		if c.Authorization.SecondaryApproverRef == c.Authorization.PrimaryApproverRef {
			return nil, fmt.Errorf("ApproveAction rejected: secondary approver %q must differ from the primary approver (TWO_PARTY requires two humans)", c.Authorization.SecondaryApproverRef)
		}
		if act.PrimaryApprover != "" && c.Authorization.PrimaryApproverRef != act.PrimaryApprover {
			return nil, fmt.Errorf("ApproveAction rejected: primary_approver_ref %q does not match the recorded primary approval by %q", c.Authorization.PrimaryApproverRef, act.PrimaryApprover)
		}
		// When the policy declared a secondary approver pool, the secondary must
		// be drawn from it (04 §5.6): the policy author bounded who can complete
		// the two-party approval, and that bound is frozen on the action.
		if len(act.SecondaryApproverPool) > 0 && !slices.Contains(act.SecondaryApproverPool, c.Authorization.SecondaryApproverRef) {
			return nil, fmt.Errorf("ApproveAction rejected: secondary approver %q is not in the action's secondary approver pool", c.Authorization.SecondaryApproverRef)
		}
	}

	// Invisible activate at the action boundary (01 §Extension 2): approving the
	// first action of a DRAFT investigation is the activation — the human
	// clearing the first external action clears the investigation to act on the
	// world. Only a FULLY-approved action activates (a two-party primary leaves
	// it PENDING_SECONDARY, still not cleared to dispatch). Attributed to the
	// approving human, in the same transaction, so no path activates without a
	// real approval. Auto-approve is suppressed while draft (server side), so
	// this is always human-driven.
	var events []Event
	seq := state.Seq
	fullyApproved := c.Authorization.Mode != AuthModeTwoParty || stage != AuthStagePrimary
	if fullyApproved && state.Status == StatusDraft {
		activate, err := statusTransition(env, state, StatusDraft, StatusActive, "first action approved", newID)
		if err != nil {
			return nil, err
		}
		events = append(events, activate...)
		seq += int64(len(activate))
	}

	interpID := newID()
	pair, err := actEvents(env, seq, EventTypeActionApproved, ActionApproved{
		ActionID:                 c.ActionID,
		Authorization:            c.Authorization,
		ApprovalInterpretationID: interpID,
	}, interpID, InterpretationActionApproval, fmt.Sprintf("%s: approved (%s/%s)", act.label(), c.Authorization.Mode, stage), nil)
	if err != nil {
		return nil, err
	}
	return append(events, pair...), nil
}

// checkApprover enforces the actor/approver invariant for the recorded stage.
// The AI-write-protection guard in applyCommand already ensures the actor is not
// AI_DELEGATED before we get here.
func checkApprover(env Envelope, auth Authorization, stage string) error {
	want := auth.PrimaryApproverRef
	if stage == AuthStageSecondary {
		want = auth.SecondaryApproverRef
	}
	if want == "" {
		return fmt.Errorf("ApproveAction rejected: no approver ref for stage %s", stage)
	}
	// AUTO_POLICY approvals are attributed to the policy's signed-off-by human,
	// who is the recorded principal; the invariant holds identically.
	if env.Actor.PrincipalID != want {
		return fmt.Errorf("ApproveAction rejected: actor principal %q is not the %s approver %q (04 §3.3 actor/approver invariant)", env.Actor.PrincipalID, stage, want)
	}
	return nil
}

// applyRejectAction builds the (ActionRejected, action-rejection Interpretation)
// pair.
func applyRejectAction(env Envelope, state aggregateState, c RejectAction, newID idSource) ([]Event, error) {
	act, ok := state.Actions[c.ActionID]
	if !ok {
		return nil, fmt.Errorf("RejectAction rejected: action %s does not exist", c.ActionID)
	}
	if act.Status != ActionStatusRequested && act.Status != ActionStatusPendingSecondary {
		return nil, fmt.Errorf("RejectAction rejected: action %s is %s (not pending)", c.ActionID, act.Status)
	}

	interpID := newID()
	return actEvents(env, state.Seq, EventTypeActionRejected, ActionRejected{
		ActionID:                  c.ActionID,
		Reason:                    c.Reason,
		RejectionInterpretationID: interpID,
	}, interpID, InterpretationActionRejection, c.Reason, nil)
}

// applyDispatchAction builds the (ActionDispatched, action-dispatch
// Interpretation) pair. Legal only from APPROVED. Its presence in the stream is
// the dispatch-ledger guard (08 §6b): a re-triggered workflow sees it and
// short-circuits re-issue.
func applyDispatchAction(env Envelope, state aggregateState, c DispatchAction, newID idSource) ([]Event, error) {
	act, ok := state.Actions[c.ActionID]
	if !ok {
		return nil, fmt.Errorf("DispatchAction rejected: action %s does not exist", c.ActionID)
	}
	if act.Status != ActionStatusApproved {
		return nil, fmt.Errorf("DispatchAction rejected: action %s is %s, not APPROVED", c.ActionID, act.Status)
	}

	interpID := newID()
	return actEvents(env, state.Seq, EventTypeActionDispatched, ActionDispatched{
		ActionID:                 c.ActionID,
		Adapter:                  c.Adapter,
		AdapterRequestID:         c.AdapterRequestID,
		DispatchedAt:             env.OccurredAt,
		DispatchInterpretationID: interpID,
	}, interpID, InterpretationActionDispatch, dispatchSummary(act.label(), c.Adapter), nil)
}

// applyResultAction builds the (ActionResulted, action-result Interpretation)
// pair. Legal only from EXECUTING.
func applyResultAction(env Envelope, state aggregateState, c ResultAction, newID idSource) ([]Event, error) {
	act, ok := state.Actions[c.ActionID]
	if !ok {
		return nil, fmt.Errorf("ResultAction rejected: action %s does not exist", c.ActionID)
	}
	if act.Status != ActionStatusExecuting {
		return nil, fmt.Errorf("ResultAction rejected: action %s is %s, not EXECUTING", c.ActionID, act.Status)
	}

	interpID := newID()
	rationale := fmt.Sprintf("%s: %s", act.label(), c.FinalOutcome)
	if c.ErrorDetail != "" {
		rationale += " — " + c.ErrorDetail
	}
	return actEvents(env, state.Seq, EventTypeActionResulted, ActionResulted{
		ActionID:               c.ActionID,
		FinalOutcome:           c.FinalOutcome,
		PerTargetResults:       c.PerTargetResults,
		Attempts:               c.Attempts,
		RawResponseRef:         c.RawResponseRef,
		ErrorDetail:            c.ErrorDetail,
		Adapter:                c.Adapter,
		ResultInterpretationID: interpID,
	}, interpID, InterpretationActionResult, rationale, nil)
}

// applyReverseAction builds the (ActionReversed, action-reversal Interpretation)
// pair on the ORIGINAL action. Legal only from SUCCEEDED — you can only reverse
// an action that actually took effect (04 §7).
func applyReverseAction(env Envelope, state aggregateState, c ReverseAction, newID idSource) ([]Event, error) {
	orig, ok := state.Actions[c.OriginalActionID]
	if !ok {
		return nil, fmt.Errorf("ReverseAction rejected: original action %s does not exist", c.OriginalActionID)
	}
	if orig.Status != ActionStatusSucceeded {
		return nil, fmt.Errorf("ReverseAction rejected: original action %s is %s, not SUCCEEDED", c.OriginalActionID, orig.Status)
	}

	interpID := newID()
	return actEvents(env, state.Seq, EventTypeActionReversed, ActionReversed{
		OriginalActionID:         c.OriginalActionID,
		ReversingActionID:        c.ReversingActionID,
		ReversalInterpretationID: interpID,
	}, interpID, InterpretationActionReversal, fmt.Sprintf("%s: reversed", orig.label()), nil)
}

// applyReversalAttempt builds the (ActionReversalAttempted, action-reversal
// Interpretation) pair on the ORIGINAL action. Same precondition as
// ReverseAction — only a SUCCEEDED action has an effect to attempt to reverse.
// The interpretation_type reuses the canonical action-reversal (01: the enum is
// closed); the domain event — not the interpretation — carries the
// verified/unverified distinction.
func applyReversalAttempt(env Envelope, state aggregateState, c RecordReversalAttempt, newID idSource) ([]Event, error) {
	orig, ok := state.Actions[c.OriginalActionID]
	if !ok {
		return nil, fmt.Errorf("RecordReversalAttempt rejected: original action %s does not exist", c.OriginalActionID)
	}
	if orig.Status != ActionStatusSucceeded {
		return nil, fmt.Errorf("RecordReversalAttempt rejected: original action %s is %s, not SUCCEEDED", c.OriginalActionID, orig.Status)
	}
	switch orig.ReversalAttemptedBy {
	case uuid.Nil:
	case c.ReversingActionID:
		// Already recorded: a retried or re-run ReversalSaga repeating its own
		// write. Nothing new happened, so nothing is appended.
		return nil, nil
	default:
		return nil, fmt.Errorf("RecordReversalAttempt rejected: original action %s already has a reversal attempt by %s", c.OriginalActionID, orig.ReversalAttemptedBy)
	}

	interpID := newID()
	return actEvents(env, state.Seq, EventTypeActionReversalAttempted, ActionReversalAttempted{
		OriginalActionID:         c.OriginalActionID,
		ReversingActionID:        c.ReversingActionID,
		ReversalInterpretationID: interpID,
	}, interpID, InterpretationActionReversal, fmt.Sprintf("%s: reversal attempted — effect unverified, original stays SUCCEEDED", orig.label()), nil)
}

// applyExpireAction builds the (ActionExpired, action-expiry Interpretation)
// pair for a pending action that lapsed.
func applyExpireAction(env Envelope, state aggregateState, c ExpireAction, newID idSource) ([]Event, error) {
	act, ok := state.Actions[c.ActionID]
	if !ok {
		return nil, fmt.Errorf("ExpireAction rejected: action %s does not exist", c.ActionID)
	}
	if act.Status != ActionStatusRequested && act.Status != ActionStatusPendingSecondary {
		return nil, fmt.Errorf("ExpireAction rejected: action %s is %s (not pending)", c.ActionID, act.Status)
	}
	// Nobody approved an expiry: it carries out the request's own deadline,
	// so its principal is the requester (the actor invariant, event.go).
	if env.Actor.PrincipalID != act.RequestedBy {
		return nil, fmt.Errorf("ExpireAction rejected: principal %q is not the requester %q", env.Actor.PrincipalID, act.RequestedBy)
	}

	interpID := newID()
	return actEvents(env, state.Seq, EventTypeActionExpired, ActionExpired{ActionID: c.ActionID, ExpiryInterpretationID: interpID},
		interpID, InterpretationActionExpiry, fmt.Sprintf("%s: expired (no approval before the window closed)", act.label()), nil)
}
