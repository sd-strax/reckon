package aggregate

// The eight x-action lifecycle event types (04 §3.1, 02 §3) — plus the separate
// policy_evaluated audit event at the end of this block, which is NOT a
// lifecycle transition (it never moves status). The x-action is a
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

// x-action status values (04 §3.1). The status field is a projection; this is
// the state machine the events drive.
const (
	ActionStatusRequested        = "REQUESTED"
	ActionStatusPendingSecondary = "PENDING_SECONDARY"
	ActionStatusApproved         = "APPROVED"
	ActionStatusRejected         = "REJECTED"
	ActionStatusExpired          = "EXPIRED"
	ActionStatusExecuting        = "EXECUTING"
	ActionStatusSucceeded        = "SUCCEEDED"
	ActionStatusFailed           = "FAILED"
	ActionStatusReversed         = "REVERSED"
	// ActionStatusOutcomeUnknown: dispatched, but the run that made the call
	// stopped before recording a result; it waits for a person (adr/0216).
	ActionStatusOutcomeUnknown = "OUTCOME_UNKNOWN"
)

// Trust tiers (04 §1). T0/T1 don't produce x-actions (they stay in the
// interpretation layer); write actions are T2 or T3.
const (
	TierT1 = "T1"
	TierT2 = "T2"
	TierT3 = "T3"
)

// Authorization modes and stages (04 §3.3).
const (
	AuthModeManual            = "MANUAL"
	AuthModeAutoPolicy        = "AUTO_POLICY"
	AuthModeTwoParty          = "TWO_PARTY"
	AuthModeExternalDelegated = "EXTERNAL_DELEGATED"

	AuthStageSolo      = "SOLO"
	AuthStagePrimary   = "PRIMARY"
	AuthStageSecondary = "SECONDARY"
)
