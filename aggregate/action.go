package aggregate

import "github.com/sd-strax/reckon/internal/agentapi"

// The eight reckon-action lifecycle event types (04 §3.1, 02 §3) — plus the separate
// policy_evaluated audit event at the end of this block, which is NOT a
// lifecycle transition (it never moves status). The reckon-action is a
// sibling primitive tracked *inside* the investigation aggregate — its status is
// a projection over these events, never stored state. Each transition is paired
// with an action-* Interpretation in the same transaction (shared
// correlation_id), so the reasoning thread stays intact.
const (
	EventTypeActionRequested  = "action.requested"
	EventTypeActionApproved   = "action.approved"
	EventTypeActionRejected   = "action.rejected"
	EventTypeActionExpired    = "action.expired"
	EventTypeActionDispatched = "action.dispatched" // system-emitted
	EventTypeActionResulted   = "action.resulted"   // system-emitted
	EventTypeActionReversed   = "action.reversed"   // system-emitted

	// EventTypeActionReversalAttempted records a reversing dispatch that fully
	// succeeded but whose effect could not be verified (04 §7.1 Position C —
	// BEST_EFFORT originals). The original's status does NOT change; only
	// reversal_attempted_by_ref is recorded, so the attempt is queryable from
	// the original's side without over-claiming the undo. System-emitted by the
	// ReversalSaga.
	EventTypeActionReversalAttempted = "action.reversal_attempted"

	// EventTypeActionPolicyEvaluated records the Gate 2 policy evaluation
	// (04 §4, 02 §3). Recorded once per action-request in the SAME transaction
	// as ActionRequested — the shared-tx requirement is why the evaluation
	// result travels on the RequestAction command rather than being a separate
	// command. Audit-only: it does not move the action's status.
	EventTypeActionPolicyEvaluated = "action.policy_evaluated"
)

// The action-* Interpretation type tags (04 §3.2), members of the canonical
// interpretation_type enum alongside the reasoning types.
const (
	InterpretationActionRequest   = "action-request"
	InterpretationActionApproval  = "action-approval"
	InterpretationActionRejection = "action-rejection"
	InterpretationActionExpiry    = "action-expiry"
	InterpretationActionDispatch  = "action-dispatch"
	InterpretationActionResult    = "action-result"
	InterpretationActionReversal  = "action-reversal"
)

// reckon-action status values (04 §3.1). The status field is a projection;
// this is the state machine the events drive. The words are defined once in
// internal/agentapi, which the agent shares (adr/0278).
const (
	ActionStatusRequested        = agentapi.ActionStatusRequested
	ActionStatusPendingSecondary = agentapi.ActionStatusPendingSecondary
	ActionStatusApproved         = agentapi.ActionStatusApproved
	ActionStatusRejected         = agentapi.ActionStatusRejected
	ActionStatusExpired          = agentapi.ActionStatusExpired
	ActionStatusExecuting        = agentapi.ActionStatusExecuting
	ActionStatusSucceeded        = agentapi.ActionStatusSucceeded
	ActionStatusFailed           = agentapi.ActionStatusFailed
	ActionStatusReversed         = agentapi.ActionStatusReversed
	// ActionStatusOutcomeUnknown: dispatched, but the run that made the call
	// stopped before recording a result; it waits for a person (adr/0216).
	ActionStatusOutcomeUnknown = agentapi.ActionStatusOutcomeUnknown
)

// ActionStatus is a reckon-action's lifecycle status as a served view carries
// it. The words are the untyped constants above.
type ActionStatus = agentapi.ActionStatus

// Trust tiers (04 §1). T0/T1 don't produce reckon-actions (they stay in the
// interpretation layer); write actions are T2 or T3.
const (
	TierT1 = agentapi.TierT1
	TierT2 = agentapi.TierT2
	TierT3 = agentapi.TierT3
)

// Tier is an action's trust tier as a served view carries it.
type Tier = agentapi.Tier

// Authorization modes (04 §3.3).
const (
	AuthModeManual            = agentapi.AuthModeManual
	AuthModeAutoPolicy        = agentapi.AuthModeAutoPolicy
	AuthModeTwoParty          = agentapi.AuthModeTwoParty
	AuthModeExternalDelegated = agentapi.AuthModeExternalDelegated
)

// Authorization stages (04 §3.3).
const (
	AuthStageSolo      = "SOLO"
	AuthStagePrimary   = "PRIMARY"
	AuthStageSecondary = "SECONDARY"
)

// AuthMode is how an action is, or must be, authorized, as a served view
// carries it.
type AuthMode = agentapi.AuthMode

// AuthStage is which approval an authorization records, as a served view
// carries it.
type AuthStage string

// WireValues lists every authorization stage, in the order declared above.
func (AuthStage) WireValues() []string {
	return []string{AuthStageSolo, AuthStagePrimary, AuthStageSecondary}
}
