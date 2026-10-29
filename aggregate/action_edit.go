package aggregate

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Editing a requested action before it is approved (adr/0127). The action keeps
// its id, targets and tier; only its parameters change — the text a message or
// ticket write carries — and the change is on the record with what it replaced.
const (
	EventTypeActionEdited    = "action.edited"
	InterpretationActionEdit = "action-edit"
)

// ActionEdited records new parameters on a REQUESTED action. PriorParameters
// keeps what the edit replaced, so the record shows the change, not only the
// result. RequiredMode is the approval requirement after the policies were
// evaluated again over the edited request; it never becomes an automatic
// approval.
type ActionEdited struct {
	ActionID              uuid.UUID       `json:"action_id"`
	Parameters            json.RawMessage `json:"parameters"`
	PriorParameters       json.RawMessage `json:"prior_parameters,omitempty"`
	Reason                string          `json:"reason,omitempty"`
	RequiredMode          string          `json:"required_mode,omitempty"`
	SecondaryApproverPool []string        `json:"secondary_approver_pool,omitempty"`
	EditInterpretationID  uuid.UUID       `json:"edit_interpretation_id"`
}

// EditAction changes a REQUESTED action's parameters before anyone approved
// it. Human-only: an AI delegate is not in the command allowlist, and the
// analyst — not the requester — decides what goes out under their approval.
type EditAction struct {
	ActionID              uuid.UUID                `json:"action_id"`
	Parameters            json.RawMessage          `json:"parameters"`
	PriorParameters       json.RawMessage          `json:"prior_parameters,omitempty"`
	Reason                string                   `json:"reason,omitempty"`
	RequiredMode          string                   `json:"required_mode,omitempty"`
	SecondaryApproverPool []string                 `json:"secondary_approver_pool,omitempty"`
	PolicyEvaluations     []PolicyEvaluationRecord `json:"policy_evaluations,omitempty"`
	MatchedPolicyRef      string                   `json:"matched_policy_ref,omitempty"`
}

// Kind returns "EditAction".
func (EditAction) Kind() string { return "EditAction" }

// Validate checks the envelope, the action id and that there is something to set.
func (c EditAction) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	if c.ActionID == (uuid.UUID{}) {
		return errors.New("EditAction: ActionID is zero")
	}
	if len(c.Parameters) == 0 {
		return errors.New("EditAction: parameters required")
	}
	return nil
}

// applyEditAction builds the (ActionEdited, action-edit Interpretation) pair,
// plus the policy re-evaluation's audit event in the same transaction. Legal
// only while the action is REQUESTED: once a first approver signed (two-party
// PENDING_SECONDARY) the content they approved must not change under them.
func applyEditAction(env Envelope, state aggregateState, c EditAction) ([]Event, error) {
	act, ok := state.Actions[c.ActionID]
	if !ok {
		return nil, fmt.Errorf("EditAction rejected: action %s does not exist", c.ActionID)
	}
	if act.Status != ActionStatusRequested {
		return nil, fmt.Errorf("EditAction rejected: %s is %s; only a request nobody has approved can be edited", act.label(), act.Status)
	}
	interpID := uuid.New()
	payload, err := json.Marshal(ActionEdited{
		ActionID:              c.ActionID,
		Parameters:            c.Parameters,
		PriorParameters:       c.PriorParameters,
		Reason:                c.Reason,
		RequiredMode:          c.RequiredMode,
		SecondaryApproverPool: c.SecondaryApproverPool,
		EditInterpretationID:  interpID,
	})
	if err != nil {
		return nil, err
	}
	summary := act.label() + ": edited before approval"
	if c.Reason != "" {
		summary += ": " + c.Reason
	}
	domain := lifecycleDomainEvent(env, state.Seq+1, EventTypeActionEdited, payload)
	interp, err := interpretationEvent(env, state.Seq+2, interpID, InterpretationActionEdit, summary, nil)
	if err != nil {
		return nil, err
	}
	events := []Event{domain, interp}
	if len(c.PolicyEvaluations) > 0 {
		pe, err := json.Marshal(ActionPolicyEvaluated{ActionID: c.ActionID, Evaluations: c.PolicyEvaluations, MatchedPolicyRef: c.MatchedPolicyRef})
		if err != nil {
			return nil, err
		}
		events = append(events, lifecycleDomainEvent(env, state.Seq+3, EventTypeActionPolicyEvaluated, pe))
	}
	return events, nil
}
