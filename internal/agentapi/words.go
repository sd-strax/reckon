package agentapi

import "slices"

// The status words the shapes in this package carry (adr/0278). Each named
// type lists its words through WireValues, in the order its constants are
// declared. The engine packages that use the words re-declare the constants
// from these, so engine code is unchanged.

// reckon-action status values (04 §3.1). The status field is a projection;
// this is the state machine the events drive.
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

// ActionStatus is a reckon-action's lifecycle status as a served view carries
// it. The words are the untyped constants above.
type ActionStatus string

// WireValues lists every action status, in the order declared above.
func (ActionStatus) WireValues() []string {
	return []string{
		ActionStatusRequested, ActionStatusPendingSecondary, ActionStatusApproved,
		ActionStatusRejected, ActionStatusExpired, ActionStatusExecuting,
		ActionStatusSucceeded, ActionStatusFailed, ActionStatusReversed,
		ActionStatusOutcomeUnknown,
	}
}

// Trust tiers (04 §1). T0/T1 don't produce reckon-actions (they stay in the
// interpretation layer); write actions are T2 or T3.
const (
	TierT1 = "T1"
	TierT2 = "T2"
	TierT3 = "T3"
)

// Tier is an action's trust tier as a served view carries it.
type Tier string

// WireValues lists every trust tier, in the order declared above.
func (Tier) WireValues() []string {
	return []string{TierT1, TierT2, TierT3}
}

// Authorization modes (04 §3.3).
const (
	AuthModeManual            = "MANUAL"
	AuthModeAutoPolicy        = "AUTO_POLICY"
	AuthModeTwoParty          = "TWO_PARTY"
	AuthModeExternalDelegated = "EXTERNAL_DELEGATED"
)

// AuthMode is how an action is, or must be, authorized, as a served view
// carries it.
type AuthMode string

// WireValues lists every authorization mode, in the order declared above.
func (AuthMode) WireValues() []string {
	return []string{AuthModeManual, AuthModeAutoPolicy, AuthModeTwoParty, AuthModeExternalDelegated}
}

// ActionRequestStatus is where a request left the new action: waiting for a
// person, waiting for two, or approved by a policy.
type ActionRequestStatus string

// The statuses a request answers with.
const (
	RequestPendingManual   ActionRequestStatus = "PENDING_MANUAL"
	RequestPendingTwoParty ActionRequestStatus = "PENDING_TWO_PARTY"
	RequestApproved        ActionRequestStatus = "APPROVED"
)

// WireValues lists every request status, in the order declared above.
func (ActionRequestStatus) WireValues() []string {
	return []string{string(RequestPendingManual), string(RequestPendingTwoParty), string(RequestApproved)}
}

// PendingStatus is an action's status as the agent labels it
// (ActionView.PendingLabel): a request still waiting for a person is
// PENDING_MANUAL or PENDING_TWO_PARTY, and any other status passes through.
type PendingStatus string

// WireValues lists the labels PendingLabel gives every action status under
// every mode a request can require, in the order they first appear: the two
// pending labels, then every status the label passes through.
func (PendingStatus) WireValues() []string {
	modes := append([]string{""}, AuthMode("").WireValues()...)
	var out []string
	for _, s := range ActionStatus("").WireValues() {
		for _, m := range modes {
			label := string(ActionView{Status: ActionStatus(s), RequiredMode: AuthMode(m)}.PendingLabel())
			if !slices.Contains(out, label) {
				out = append(out, label)
			}
		}
	}
	return out
}

// Investigation lifecycle states (design/01-domain-model.md §Extension 2).
// Stored lowercase in the investigation_current projection; the spec writes
// them uppercase in prose.
const (
	InvestigationDraft     = "draft"
	InvestigationActive    = "active"
	InvestigationPaused    = "paused"
	InvestigationConcluded = "concluded"
	InvestigationArchived  = "archived"
	InvestigationDiscarded = "discarded"
)

// InvestigationStatus is an investigation's lifecycle status as a served view
// carries it. The words are the untyped constants above, so code that holds a
// status as a string is unchanged.
type InvestigationStatus string

// WireValues lists every investigation status, in the order declared above.
func (InvestigationStatus) WireValues() []string {
	return []string{
		InvestigationDraft, InvestigationActive, InvestigationPaused,
		InvestigationConcluded, InvestigationArchived, InvestigationDiscarded,
	}
}

// Verdict dispositions (01). No PENDING value: an investigation with no
// verdict act has no verdict.
const (
	VerdictBenign     = "BENIGN"
	VerdictSuspicious = "SUSPICIOUS"
	VerdictMalicious  = "MALICIOUS"
)

// VerdictDisposition is a verdict's disposition as a served view carries it.
type VerdictDisposition string

// WireValues lists every disposition, in the order declared above.
func (VerdictDisposition) WireValues() []string {
	return []string{VerdictBenign, VerdictSuspicious, VerdictMalicious}
}

// Confidence values (01-domain-model.md §Interpretation schema). Optional.
const (
	ConfidenceHigh   = "HIGH"
	ConfidenceMedium = "MEDIUM"
	ConfidenceLow    = "LOW"
)

// Confidence is an act's confidence as a served view carries it.
type Confidence string

// WireValues lists every confidence, in the order declared above.
func (Confidence) WireValues() []string {
	return []string{ConfidenceHigh, ConfidenceMedium, ConfidenceLow}
}

// Coverage is the structured outcome of a capability call (03 §2, §6.1). The
// agent loop branches on it without parsing prose.
type Coverage string

const (
	CoverageComplete             Coverage = "COMPLETE"              // all eligible bindings invoked and succeeded
	CoveragePartial              Coverage = "PARTIAL"               // fan-out: some succeeded, some failed
	CoverageUnavailableTenant    Coverage = "UNAVAILABLE_TENANT"    // no configured/applicable binding
	CoverageUnavailableTransient Coverage = "UNAVAILABLE_TRANSIENT" // applicable bindings exist but all unhealthy
	CoverageFailed               Coverage = "FAILED"                // healthy bindings called, all errored
)

// WireValues lists every coverage, in the order declared above.
func (Coverage) WireValues() []string {
	return []string{
		string(CoverageComplete), string(CoveragePartial), string(CoverageUnavailableTenant),
		string(CoverageUnavailableTransient), string(CoverageFailed),
	}
}

// A knowledge recall's coverage (design/06 §4.3). EMPTY is evidence of
// absence. EMPTY_UNDER_FILTER means a filter tag matches nothing, which is
// not evidence that nothing alike exists.
const (
	RecallComplete         = "COMPLETE"
	RecallEmpty            = "EMPTY"
	RecallEmptyUnderFilter = "EMPTY_UNDER_FILTER"
)

// RecallCoverage is a recall's outcome as a served result carries it. The
// words are the untyped constants above. It is a type of its own, not
// Coverage, because a recall and a capability call answer with different
// words.
type RecallCoverage string

// WireValues lists every recall coverage, in the order declared above.
func (RecallCoverage) WireValues() []string {
	return []string{RecallComplete, RecallEmpty, RecallEmptyUnderFilter}
}
