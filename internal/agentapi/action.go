package agentapi

import (
	"encoding/json"
	"time"

	"github.com/sd-strax/reckon/internal/catalog"
)

// Reversibility classifications (04 §7). The ordered scale is
// IRREVERSIBLE < BEST_EFFORT < REVERSIBLE; per-binding overrides (deferred)
// may move along it under the §7.3 rules.
const (
	// ReversibilityReversible: RELIABLE. The tool cleanly undoes the effect;
	// a successful reversal marks the original REVERSED.
	ReversibilityReversible = "reversible"
	// ReversibilityBestEffort: an inverse action exists but its effect is
	// tooling-dependent; the affordance and forward linkage stay live but the
	// original is never marked REVERSED (04 §7.1 Position C).
	ReversibilityBestEffort = "best_effort"
	// ReversibilityIrreversible: no inverse exists; reversal is structurally
	// impossible.
	ReversibilityIrreversible = "irreversible"
)

// ActionDescriptor is the write analog of CapabilityDescriptor (08 §3): the
// action surface the LLM sees plus the auth/taxonomy fields it declares for
// discoverability. DefaultTier/Reversibility/D3FEND are surfaced here but
// OWNED by 04 §2; this must not diverge from that categorization.
type ActionDescriptor struct {
	ActionType string `json:"action_type"` // e.g. "host.isolate"
	Intent     string `json:"intent"`      // LLM-facing: effect + when to use
	// Inputs is the declared schema for request_action.parameters. Entity-typed
	// inputs ride the targets list; the rest are parameter keys the backend
	// validates at request time. The agent renders them as the top-level
	// inputs of the type's action tool (adr/0380).
	Inputs      []InputParam `json:"inputs"`
	DefaultTier Tier         `json:"default_tier"` // T2 | T3 (04 §2)
	// Reversibility is the baseline classification (04 §7): "reversible"
	// (RELIABLE: the tool cleanly undoes the effect), "best_effort" (an inverse
	// action exists but its effect is tooling-dependent, e.g. ioc.block against a
	// propagating denylist), or "irreversible" (no inverse). It gates the REVERSED
	// status claim: only a "reversible" original flips to REVERSED on a successful
	// reversal (Position C); "best_effort" keeps the affordance + forward linkage
	// but the original stays SUCCEEDED. A per-binding override may upgrade
	// best_effort→reversible when the wired tool is trusted (04 §7; deferred).
	Reversibility string `json:"reversibility"`
	// ReversibleBy is the inverse action type (04 §7); empty when irreversible.
	// A reversal request creates a reckon-action of this type against the original's
	// targets, at the SAME tier (reversing is never lower-tier than the original).
	ReversibleBy string `json:"reversible_by,omitempty"`
	D3FEND       string `json:"d3fend,omitempty"`
	// Name is the short human name a surface titles the action by ("Isolate
	// host"); the action_type stays the machine id (#123).
	Name string `json:"name"`
	// Category is what the action does to the incident, so a surface groups it
	// without guessing from the id: containment | eradication | recovery |
	// notification | case_record (a write to the system of record, never
	// containment) (#123).
	Category string `json:"category"`
	// TargetTypes are the STIX object types this action acts on (adr/0354).
	// Every target of a request must carry an entity_ref of one of these
	// types. Empty for an action whose target is a destination (a ticket, a
	// channel, an address), whose entity_ref is not read.
	TargetTypes []string `json:"target_types,omitempty"`
}

// ReliablyReversible reports whether a fully-successful dispatch of this
// action's inverse may mark it REVERSED (04 §7.1 Position C). Only the
// RELIABLE classification qualifies; a best_effort original keeps the reversal
// affordance and forward linkage but honestly stays SUCCEEDED.
func (d ActionDescriptor) ReliablyReversible() bool {
	return d.Reversibility == ReversibilityReversible
}

// ActionSummary pairs an action descriptor with its current availability.
type ActionSummary struct {
	Descriptor ActionDescriptor           `json:"descriptor"`
	Status     catalog.ActionAvailability `json:"status"`
	Bindings   int                        `json:"bindings"`
}

// ActionTypesResponse is GET /api/action-types.
type ActionTypesResponse struct {
	ActionTypes []ActionSummary `json:"action_types"`
}

// TargetSpec is one thing an action acts on (04 §3.1). Resolution is frozen at
// request time: resolved_identifier is what the write adapter sends downstream;
// it is never re-resolved.
//
// It is also the stored event format: action events carry it as is, so a tag
// change changes how old events replay.
type TargetSpec struct {
	EntityRef          string `json:"entity_ref"`
	ResolvedIdentifier string `json:"resolved_identifier"`
	AssetCriticality   string `json:"asset_criticality,omitempty"`
}

// RequestActionBody is the request_action tool call over HTTP (08 §2).
type RequestActionBody struct {
	ActionType       string          `json:"action_type"`
	Targets          []TargetSpec    `json:"targets"`
	Parameters       json.RawMessage `json:"parameters,omitempty"`
	EvidenceRefs     []string        `json:"evidence_refs,omitempty"`
	Rationale        string          `json:"rationale"`
	InvestigationRef string          `json:"investigation_ref"`
	// ReversalOfRef, when set, makes this request a reversal of that original
	// action (04 §7): action_type should be the inverse, and auto-approval
	// triggers the ReversalSaga instead of a plain dispatch.
	ReversalOfRef string `json:"reversal_of_ref,omitempty"`
	// RetryOf records lineage to the prior FAILED/EXPIRED action this request
	// replaces (design/ui binding §2.3).
	RetryOf string `json:"retry_of,omitempty"`
}

// RequestActionResponse reports the created reckon-action and how authorization
// resolved.
type RequestActionResponse struct {
	ActionID         string              `json:"action_id"`
	Tier             Tier                `json:"tier"`
	Status           ActionRequestStatus `json:"status"`
	Mode             AuthMode            `json:"mode"`
	MatchedPolicyRef string              `json:"matched_policy_ref,omitempty"`
	WorkflowID       string              `json:"workflow_id,omitempty"`
}

// ApproveActionBody is the optional body of an approve request. ChallengeResponse
// carries the typed-challenge value for higher-tier confirmations (04 §5.5); it
// is recorded on the Authorization for audit.
type ApproveActionBody struct {
	ChallengeResponse string `json:"challenge_response,omitempty"`
}

// RejectActionBody is the body of a reject request.
type RejectActionBody struct {
	Reason string `json:"reason"`
}

// ActionView is one row of GET /api/investigations/{id}/actions: the action
// review queue + audit list a surface renders to offer approvals. Status is the
// projection's raw lifecycle status (REQUESTED/PENDING_SECONDARY/APPROVED/…);
// required_mode says what approval the request still needs (MANUAL/TWO_PARTY).
// The agent's fallback path, used when the list cannot be read, builds rows
// from the request-time answers, so their Status may also carry the
// request-time PENDING_* labels.
type ActionView struct {
	ActionID     string       `json:"action_id"`
	ActionType   string       `json:"action_type"`
	Tier         Tier         `json:"tier"`
	Status       ActionStatus `json:"status"`
	RequiredMode AuthMode     `json:"required_mode,omitempty"`
	// Outcome is the recorded final outcome (SUCCEEDED | FAILED | PARTIAL |
	// TIMEOUT), or a person's resolution of an unknown one. Empty until one
	// is recorded. Status folds PARTIAL into SUCCEEDED and TIMEOUT into
	// FAILED (08 §7.1), so a surface that tells the analyst what happened
	// reads this field (adr/0361).
	Outcome string `json:"outcome,omitempty"`
	// SecondaryApproverPool is who may complete a two-party approval, frozen
	// at request time by the policy that required it (04 §4.1); empty for a
	// manual action, and for a two-party one whose policy named no pool (the
	// on-call rotation then). The card tells the first approver who finishes
	// it (adr/0099).
	SecondaryApproverPool []string `json:"secondary_approver_pool,omitempty"`
	// PrimaryApproverRef is who gave the first approval, once one is in; a
	// two-party card awaiting its second approval leaves them out of the
	// candidates, since the aggregate requires two distinct humans.
	PrimaryApproverRef string       `json:"primary_approver_ref,omitempty"`
	Mode               AuthMode     `json:"mode,omitempty"`
	IsReversal         bool         `json:"is_reversal,omitempty"`
	Targets            []TargetSpec `json:"targets,omitempty"`
	EvidenceRefs       []string     `json:"evidence_refs,omitempty"`
	// ExpiresAt is the approval deadline frozen at request time (04). The
	// status may still read REQUESTED past it until the expiry timer fires
	// (its start is best-effort); the engine refuses at the approve attempt
	// regardless. So every surface, and the model through the list_actions
	// tool result, must judge approvability from this field, not the status.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Parameters are the request's frozen parameters: the pre-send preview
	// for notify.* actions renders the message body from here (binding §4:
	// the preview IS the approval surface, so the approver must see exactly
	// what will be sent).
	Parameters json.RawMessage `json:"parameters,omitempty"`

	// Reversibility is the classification frozen at request time (04 §7):
	// REVERSIBLE | BEST_EFFORT | IRREVERSIBLE. The decision-grade card states
	// it honestly BEFORE approval (design/ui 03 §3.3).
	Reversibility string `json:"reversibility,omitempty"`
	// TierEscalated: the blast-radius escalator raised this above the type's
	// default tier (04 §1), so the card says why it is T3.
	TierEscalated bool `json:"tier_escalated,omitempty"`
	// EditedAt is when the request was last edited before approval (adr/0127).
	EditedAt *time.Time `json:"edited_at,omitempty"`
	// RequestedAt is when the action was requested; the Response lens orders
	// its timeline by it (adr/0361).
	RequestedAt *time.Time `json:"requested_at,omitempty"`
	// ApprovedAt is when the first approval was recorded. A two-party action
	// carries it while it still awaits its second approval, so it says when,
	// never whether: the status says whether. Absent before any approval, and
	// on rows approved before the time was recorded (adr/0361).
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	// Inputs are the type's declared non-entity parameters (what an edit may
	// set), so a surface can offer the fields the request did not fill.
	Inputs []EditableInput `json:"inputs,omitempty"`
	// Name and Category come from the action catalog (#123): what a surface
	// titles and groups the action by, so no surface guesses from the id.
	Name     string `json:"name,omitempty"`
	Category string `json:"category,omitempty"`
	// RetryOf: lineage to the action this one replaces (zero when not a retry).
	RetryOf string `json:"retry_of,omitempty"`
	// RequestingInterpretationID: the act that requested it, so the thread's
	// request line can name the action (#153).
	RequestingInterpretationID string `json:"requesting_interpretation_id,omitempty"`
	// ReversalOfRef: the action this one reverses (zero when not a reversal),
	// so a surface can show an original's reversal on the original.
	ReversalOfRef string `json:"reversal_of_ref,omitempty"`
	// Adapter is the tool that dispatched the action (write-side provenance):
	// which of several possible tools actually acted. Empty until dispatched.
	Adapter string `json:"adapter,omitempty"`
	// PlannedAdapter is the tool the resolver WOULD dispatch to if approved
	// now: the pre-approval preview, so the analyst sees which system of
	// record an external action will hit before committing. Set only while
	// pending (superseded by Adapter once dispatched).
	PlannedAdapter string `json:"planned_adapter,omitempty"`
	// ErrorDetail is the reason a FAILED action failed (08 §6c), so the ledger
	// explains the outcome instead of a bare FAILED. Empty on success.
	ErrorDetail string `json:"error_detail,omitempty"`
	// ResultRef is the operational reference the dispatch returned (e.g. the
	// created ServiceNow incident number): the analyst's handle into the
	// external system of record.
	ResultRef string `json:"result_ref,omitempty"`
}

// EditableInput is one parameter an edit may set (adr/0127).
type EditableInput struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
}

// ActionsResponse is GET /api/investigations/{id}/actions.
type ActionsResponse struct {
	Actions []ActionView `json:"actions"`
}

// Expired reports whether the approval window has elapsed: the engine will
// refuse an approve regardless of Status.
func (a ActionView) Expired(now time.Time) bool {
	return a.ExpiresAt != nil && now.After(*a.ExpiresAt)
}

// ApprovalLapsed is the one definition of an expired action (adr/0339): an
// action is expired when it still awaits approval and its approval deadline
// has passed; a decided action is not, whatever its deadline. The agent's
// engine-state block and its list_actions tool both read it, so they never
// disagree.
func (a ActionView) ApprovalLapsed(now time.Time) bool {
	return a.Pending() && a.Expired(now)
}

// Pending reports whether the action still awaits a human approval.
func (a ActionView) Pending() bool {
	switch a.Status {
	case ActionStatusRequested, ActionStatusPendingSecondary,
		ActionStatus(RequestPendingManual), ActionStatus(RequestPendingTwoParty):
		return true
	}
	return false
}

// PendingLabel renders the awaiting state in the request-time vocabulary
// (PENDING_MANUAL / PENDING_TWO_PARTY) surfaces already speak.
func (a ActionView) PendingLabel() PendingStatus {
	switch {
	case a.Status == ActionStatusRequested && a.RequiredMode == AuthModeTwoParty:
		return PendingStatus(RequestPendingTwoParty)
	case a.Status == ActionStatusRequested:
		return PendingStatus(RequestPendingManual)
	case a.Status == ActionStatusPendingSecondary:
		return PendingStatus(RequestPendingTwoParty)
	}
	return PendingStatus(a.Status)
}
