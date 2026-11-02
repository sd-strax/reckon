package aggregate

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// TargetSpec is one thing an action acts on (04 §3.1). Resolution is frozen at
// request time: resolved_identifier is what the write adapter sends downstream;
// it is never re-resolved.
type TargetSpec struct {
	EntityRef          string `json:"entity_ref"`
	ResolvedIdentifier string `json:"resolved_identifier"`
	AssetCriticality   string `json:"asset_criticality,omitempty"`
}

// Authorization is the auth sub-record recorded on ActionApproved (04 §3.3).
type Authorization struct {
	Mode                 string     `json:"mode"`
	Stage                string     `json:"stage"`
	PrimaryApproverRef   string     `json:"primary_approver_ref"`
	PrimaryApprovedAt    time.Time  `json:"primary_approved_at"`
	SecondaryApproverRef string     `json:"secondary_approver_ref,omitempty"`
	SecondaryApprovedAt  *time.Time `json:"secondary_approved_at,omitempty"`
	PolicyRef            string     `json:"policy_ref,omitempty"`
	PolicyVersion        string     `json:"policy_version,omitempty"`
	ChallengeResponse    string     `json:"challenge_response,omitempty"`
}

// PolicyEvaluationRecord is one policy's outcome in the PolicyEvaluated audit
// (02 §3). Mirrors action.PolicyEvaluation; kept as a plain aggregate type so
// the aggregate never imports the action package (which imports it).
type PolicyEvaluationRecord struct {
	PolicyRef      string `json:"policy_ref"`
	PolicyVersion  string `json:"policy_version"`
	WouldHaveFired bool   `json:"would_have_fired"`
	Effect         string `json:"effect"`
	Shadow         bool   `json:"shadow"`
}

// ActionPolicyEvaluated is the payload of the policy-evaluation audit event.
// matched_policy_ref is the policy whose effect drove authorization, or empty
// when all matching policies were shadow and the action fell through to manual.
type ActionPolicyEvaluated struct {
	ActionID         uuid.UUID                `json:"action_id"`
	Evaluations      []PolicyEvaluationRecord `json:"evaluations"`
	MatchedPolicyRef string                   `json:"matched_policy_ref,omitempty"`
}

// ActionRequested records a new x-action in REQUESTED. Paired with the
// producing action-request Interpretation.
type ActionRequested struct {
	ActionID   uuid.UUID `json:"action_id"`
	ActionType string    `json:"action_type"`
	Tier       string    `json:"tier"`
	// Reversibility freezes the action type's reversibility classification
	// (04 §7: reversible | best_effort | irreversible) at request time, so the
	// REVERSED-claim gate (04 §7.1) reads the value the analyst approved under —
	// a later catalog edit cannot re-classify an in-flight action.
	Reversibility              string          `json:"reversibility,omitempty"`
	Targets                    []TargetSpec    `json:"targets"`
	Parameters                 json.RawMessage `json:"parameters,omitempty"`
	EvidenceRefs               []string        `json:"evidence_refs,omitempty"`
	ExpiresAt                  time.Time       `json:"expires_at"`
	IsReversal                 bool            `json:"is_reversal,omitempty"`
	ReversalOfRef              uuid.UUID       `json:"reversal_of_ref,omitempty"`
	RetryOf                    uuid.UUID       `json:"retry_of,omitempty"`
	RequestingInterpretationID uuid.UUID       `json:"requesting_interpretation_id"`

	// RequiredMode + SecondaryApproverPool freeze the Gate 2 authorization
	// requirement at request time (08 §8.1 — resolution is frozen), so the
	// approval surface enforces it later without re-evaluating (which could
	// drift). RequiredMode == TWO_PARTY makes a solo approval illegal at the
	// aggregate boundary; the pool bounds who may be the secondary approver.
	RequiredMode          string   `json:"required_mode,omitempty"`
	SecondaryApproverPool []string `json:"secondary_approver_pool,omitempty"`
}

// ActionApproved advances an action to APPROVED (or PENDING_SECONDARY for a
// TWO_PARTY primary approval).
type ActionApproved struct {
	ActionID                 uuid.UUID     `json:"action_id"`
	Authorization            Authorization `json:"authorization"`
	ApprovalInterpretationID uuid.UUID     `json:"approval_interpretation_id"`
}

// ActionRejected records a declined action.
type ActionRejected struct {
	ActionID                  uuid.UUID `json:"action_id"`
	Reason                    string    `json:"reason"`
	RejectionInterpretationID uuid.UUID `json:"rejection_interpretation_id"`
}

// ActionExpired records a REQUESTED/PENDING_SECONDARY action lapsing at
// expires_at. System-emitted.
type ActionExpired struct {
	ActionID               uuid.UUID `json:"action_id"`
	ExpiryInterpretationID uuid.UUID `json:"expiry_interpretation_id"`
}

// ActionDispatched records the dispatcher picking up an APPROVED action
// (02 §3). System-emitted by the ActionLifecycle workflow; its presence
// in the stream is the dispatch-ledger guard (08 §6b) — a replayed workflow
// sees it and short-circuits re-issue.
type ActionDispatched struct {
	ActionID                 uuid.UUID `json:"action_id"`
	Adapter                  string    `json:"adapter"`
	AdapterRequestID         string    `json:"adapter_request_id"`
	DispatchedAt             time.Time `json:"dispatched_at"`
	DispatchInterpretationID uuid.UUID `json:"dispatch_interpretation_id"`
}

// ActionResulted records the terminal outcome of a dispatched action (02 §3).
// System-emitted. per_target_results is what makes PARTIAL outcomes auditable.
type ActionResulted struct {
	ActionID         uuid.UUID         `json:"action_id"`
	FinalOutcome     string            `json:"final_outcome"` // SUCCEEDED | FAILED | PARTIAL | TIMEOUT
	PerTargetResults map[string]string `json:"per_target_results,omitempty"`
	Attempts         int               `json:"attempts"`
	RawResponseRef   string            `json:"raw_response_ref,omitempty"`
	// ErrorDetail is the adapter/dispatch reason for a non-success outcome
	// (08 §6c) — what the analyst reads to know WHY a FAILED action failed.
	// Empty on SUCCEEDED.
	ErrorDetail string `json:"error_detail,omitempty"`
	// Adapter is the tool the dispatch ACTUALLY used. ActionDispatched recorded
	// the planned selection before the outbound call (08 §6b); a non-empty value
	// here corrects the record to the live choice. Empty when the dispatch never
	// chose a binding.
	Adapter                string    `json:"adapter,omitempty"`
	ResultInterpretationID uuid.UUID `json:"result_interpretation_id"`
}

// ActionReversed records the original action's status moving to REVERSED on the
// reversing action's success (02 §3). The reversing action is its own x-action
// with its own lifecycle events.
type ActionReversed struct {
	OriginalActionID         uuid.UUID `json:"original_action_id"`
	ReversingActionID        uuid.UUID `json:"reversing_action_id"`
	ReversalInterpretationID uuid.UUID `json:"reversal_interpretation_id"`
}

// ActionReversalAttempted records a fully-successful reversing dispatch whose
// effect could not be verified (04 §7.1 Position C): the original honestly
// stays SUCCEEDED — only the back-reference is recorded, so "was a reversal
// ever attempted?" is answerable from the original's side without claiming an
// undo we cannot verify.
type ActionReversalAttempted struct {
	OriginalActionID         uuid.UUID `json:"original_action_id"`
	ReversingActionID        uuid.UUID `json:"reversing_action_id"`
	ReversalInterpretationID uuid.UUID `json:"reversal_interpretation_id"`
}
