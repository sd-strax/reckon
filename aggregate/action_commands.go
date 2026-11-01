package aggregate

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RequestAction proposes a state-changing action, producing an x-action in
// REQUESTED plus its action-request Interpretation (08 §2). The AI may issue
// this; it carries no Authorization and cannot advance status.
type RequestAction struct {
	ActionID   uuid.UUID `json:"action_id"` // the x-action id, minted by the caller
	ActionType string    `json:"action_type"`
	Tier       string    `json:"tier"`
	// Reversibility is the action type's classification from the catalog
	// (04 §7), frozen onto the ActionRequested event — see that event's field.
	Reversibility string          `json:"reversibility,omitempty"`
	Targets       []TargetSpec    `json:"targets"`
	Parameters    json.RawMessage `json:"parameters,omitempty"`
	EvidenceRefs  []string        `json:"evidence_refs,omitempty"`
	ExpiresAt     time.Time       `json:"expires_at"`
	Rationale     string          `json:"rationale"`
	IsReversal    bool            `json:"is_reversal,omitempty"`
	// ReversalOfRef points at the original x-action this action reverses
	// (04 §7). Set only when IsReversal; drives the ReversalSaga.
	ReversalOfRef uuid.UUID `json:"reversal_of_ref,omitempty"`
	// RetryOf points at a prior FAILED/EXPIRED x-action this request replaces.
	// Lineage only (design/ui binding §2.3): the dispatch ledger makes
	// re-dispatching one id impossible, so a retry IS a new action — this ref
	// lets surfaces render the chain.
	RetryOf uuid.UUID `json:"retry_of,omitempty"`

	// RequiredMode + SecondaryApproverPool carry the Gate 2 authorization
	// requirement (04 §4), frozen onto the action so the approval surface can
	// enforce TWO_PARTY without re-evaluating. Set by action.ApplyDecision.
	RequiredMode          string   `json:"required_mode,omitempty"`
	SecondaryApproverPool []string `json:"secondary_approver_pool,omitempty"`

	// PolicyEvaluations + MatchedPolicyRef carry the Gate 2 result (04 §4), so
	// the PolicyEvaluated audit event is recorded in the same transaction as
	// ActionRequested (02 §3). Empty when Gate 2 did not run (e.g. a request
	// built before the endpoint wires it in).
	PolicyEvaluations []PolicyEvaluationRecord `json:"policy_evaluations,omitempty"`
	MatchedPolicyRef  string                   `json:"matched_policy_ref,omitempty"`
}

// Kind returns "RequestAction".
func (RequestAction) Kind() string { return "RequestAction" }

// Validate checks the envelope and the request shape.
func (c RequestAction) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	if c.ActionID == (uuid.UUID{}) {
		return errors.New("RequestAction: ActionID is zero")
	}
	if c.ActionType == "" {
		return errors.New("RequestAction: ActionType is empty")
	}
	if c.Tier != TierT2 && c.Tier != TierT3 {
		return fmt.Errorf("RequestAction: Tier %q must be T2 or T3", c.Tier)
	}
	if len(c.Targets) == 0 {
		return errors.New("RequestAction: at least one target required")
	}
	return nil
}

// ApproveAction advances a REQUESTED (or PENDING_SECONDARY) action to APPROVED,
// carrying the Authorization sub-record. Never AI-issuable (§5.6); the acting
// principal must be the approver.
type ApproveAction struct {
	ActionID      uuid.UUID     `json:"action_id"`
	Authorization Authorization `json:"authorization"`
}

// Kind returns "ApproveAction".
func (ApproveAction) Kind() string { return "ApproveAction" }

// Validate checks the envelope and the authorization shape, including the
// 04 §3.3 mode/stage pairing — without it, a TWO_PARTY approval sent with
// stage SOLO would fold straight to APPROVED, bypassing the secondary.
func (c ApproveAction) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	if c.ActionID == (uuid.UUID{}) {
		return errors.New("ApproveAction: ActionID is zero")
	}
	switch c.Authorization.Mode {
	case AuthModeManual, AuthModeAutoPolicy, AuthModeExternalDelegated:
		// Single-stage modes are always SOLO (04 §3.3).
		if c.Authorization.Stage != AuthStageSolo {
			return fmt.Errorf("ApproveAction: mode %s requires stage SOLO, got %q", c.Authorization.Mode, c.Authorization.Stage)
		}
	case AuthModeTwoParty:
		// TWO_PARTY is exactly PRIMARY then SECONDARY — SOLO would collapse the
		// two stages into one.
		if c.Authorization.Stage != AuthStagePrimary && c.Authorization.Stage != AuthStageSecondary {
			return fmt.Errorf("ApproveAction: mode TWO_PARTY requires stage PRIMARY or SECONDARY, got %q", c.Authorization.Stage)
		}
	default:
		return fmt.Errorf("ApproveAction: invalid authorization mode %q", c.Authorization.Mode)
	}
	if c.Authorization.PrimaryApproverRef == "" {
		return errors.New("ApproveAction: primary_approver_ref is empty")
	}
	return nil
}

// RejectAction declines a REQUESTED or PENDING_SECONDARY action. Never
// AI-issuable.
type RejectAction struct {
	ActionID uuid.UUID `json:"action_id"`
	Reason   string    `json:"reason"`
}

// Kind returns "RejectAction".
func (RejectAction) Kind() string { return "RejectAction" }

// Validate checks the envelope and the action id.
func (c RejectAction) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	if c.ActionID == (uuid.UUID{}) {
		return errors.New("RejectAction: ActionID is zero")
	}
	return nil
}

// ExpireAction lapses a REQUESTED/PENDING_SECONDARY action at expires_at.
// System-emitted (actor.kind = SYSTEM).
type ExpireAction struct {
	ActionID uuid.UUID `json:"action_id"`
}

// Kind returns "ExpireAction".
func (ExpireAction) Kind() string { return "ExpireAction" }

// Validate checks the envelope and the action id.
func (c ExpireAction) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	if c.ActionID == (uuid.UUID{}) {
		return errors.New("ExpireAction: ActionID is zero")
	}
	return nil
}

// DispatchAction records an APPROVED action being picked up for dispatch
// (02 §3). System-emitted by the ActionLifecycle workflow; the envelope
// principal is the action's approver (the workflow carries it, 05 §6.2). The
// adapter_request_id is the workflow id.
type DispatchAction struct {
	ActionID         uuid.UUID `json:"action_id"`
	Adapter          string    `json:"adapter"`
	AdapterRequestID string    `json:"adapter_request_id"`
}

// Kind returns "DispatchAction".
func (DispatchAction) Kind() string { return "DispatchAction" }

// Validate checks the envelope and the action id.
func (c DispatchAction) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	if c.ActionID == (uuid.UUID{}) {
		return errors.New("DispatchAction: ActionID is zero")
	}
	return nil
}

// ResultAction records the terminal outcome of a dispatched action (02 §3).
// System-emitted.
type ResultAction struct {
	ActionID         uuid.UUID         `json:"action_id"`
	FinalOutcome     string            `json:"final_outcome"` // SUCCEEDED | FAILED | PARTIAL | TIMEOUT
	PerTargetResults map[string]string `json:"per_target_results,omitempty"`
	Attempts         int               `json:"attempts"`
	RawResponseRef   string            `json:"raw_response_ref,omitempty"`
	ErrorDetail      string            `json:"error_detail,omitempty"`
	Adapter          string            `json:"adapter,omitempty"` // the tool actually used (corrects the planned record)
}

// Kind returns "ResultAction".
func (ResultAction) Kind() string { return "ResultAction" }

// Validate checks the envelope, action id, and outcome vocabulary.
func (c ResultAction) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	if c.ActionID == (uuid.UUID{}) {
		return errors.New("ResultAction: ActionID is zero")
	}
	switch c.FinalOutcome {
	case "SUCCEEDED", "FAILED", "PARTIAL", "TIMEOUT":
	default:
		return fmt.Errorf("ResultAction: invalid final_outcome %q", c.FinalOutcome)
	}
	return nil
}

// ReverseAction records the ORIGINAL action moving to REVERSED once its
// reversing action succeeded (02 §3, 04 §7). System-emitted by the ReversalSaga.
type ReverseAction struct {
	OriginalActionID  uuid.UUID `json:"original_action_id"`
	ReversingActionID uuid.UUID `json:"reversing_action_id"`
}

// Kind returns "ReverseAction".
func (ReverseAction) Kind() string { return "ReverseAction" }

// Validate checks the envelope and both action ids.
func (c ReverseAction) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	if c.OriginalActionID == (uuid.UUID{}) || c.ReversingActionID == (uuid.UUID{}) {
		return errors.New("ReverseAction: both action ids required")
	}
	return nil
}

// RecordReversalAttempt records that a reversing action fully succeeded but its
// effect could not be verified (04 §7.1 Position C — the original is
// BEST_EFFORT): the original stays SUCCEEDED and only reversal_attempted_by_ref
// is recorded. System-emitted by the ReversalSaga, like ReverseAction.
type RecordReversalAttempt struct {
	OriginalActionID  uuid.UUID `json:"original_action_id"`
	ReversingActionID uuid.UUID `json:"reversing_action_id"`
}

// Kind returns "RecordReversalAttempt".
func (RecordReversalAttempt) Kind() string { return "RecordReversalAttempt" }

// Validate checks the envelope and both action ids.
func (c RecordReversalAttempt) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	if c.OriginalActionID == (uuid.UUID{}) || c.ReversingActionID == (uuid.UUID{}) {
		return errors.New("RecordReversalAttempt: both action ids required")
	}
	return nil
}

// systemOnly reports whether a command may be issued only by a SYSTEM actor
// (the Temporal workflows / timers). Dispatch, result, expiry, and reversal are
// lifecycle transitions no human or AI issues directly — routing them through
// SYSTEM keeps the workflows the single emitter and prevents a spoofed human
// command from forging a dispatch, outcome, or reversal.
func systemOnly(cmd Command) bool {
	switch cmd.(type) {
	case DispatchAction, ResultAction, ExpireAction, ReverseAction, RecordReversalAttempt, RecordOutcomeUnknown:
		return true
	default:
		return false
	}
}
