package aggregate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// An action whose outcome nobody knows (adr/0216). The dispatch was recorded,
// so the call may have left reckon, but the workflow that made it stopped
// before recording a result. reckon cannot tell whether the tool acted, so it
// says so and waits for a person: someone checks the tool and resolves the
// action to what it shows. Nothing reruns an action whose effect is unknown.
const (
	// EventTypeActionOutcomeUnknown records EXECUTING → OUTCOME_UNKNOWN.
	// System-emitted, by the workflow run that finds the dispatch recorded
	// and no result.
	EventTypeActionOutcomeUnknown = "action.outcome_unknown"
	// EventTypeActionOutcomeResolved records a person's resolution of an
	// OUTCOME_UNKNOWN action to SUCCEEDED or FAILED.
	EventTypeActionOutcomeResolved = "action.outcome_resolved"

	InterpretationActionResolution = "action-resolution"
)

// Bound on a resolution note: what was checked and what it showed.
const resolutionNoteMaxRunes = 2000

// ActionOutcomeUnknown records that an action's outcome is unknown. Detail
// says why, in words the analyst reads on the card.
type ActionOutcomeUnknown struct {
	ActionID               uuid.UUID `json:"action_id"`
	Detail                 string    `json:"detail"`
	ResultInterpretationID uuid.UUID `json:"result_interpretation_id"`
}

// ActionOutcomeResolved records what a person found when they checked the
// tool: the action took effect (SUCCEEDED) or it did not (FAILED). Note says
// what they checked; Reference and Artifact point at the evidence, never
// carry it (adr/0139).
type ActionOutcomeResolved struct {
	ActionID                   uuid.UUID         `json:"action_id"`
	Outcome                    string            `json:"outcome"` // SUCCEEDED | FAILED
	Note                       string            `json:"note"`
	Reference                  string            `json:"reference,omitempty"`
	Artifact                   *ArtifactLocation `json:"artifact,omitempty"`
	ResolutionInterpretationID uuid.UUID         `json:"resolution_interpretation_id"`
}

// RecordOutcomeUnknown moves an EXECUTING action to OUTCOME_UNKNOWN.
// SYSTEM-only (systemOnly): the workflow is the only party that can know a
// dispatch was recorded with no run left to finish it.
type RecordOutcomeUnknown struct {
	ActionID uuid.UUID `json:"action_id"`
	Detail   string    `json:"detail"`
}

// Kind returns "RecordOutcomeUnknown".
func (RecordOutcomeUnknown) Kind() string { return "RecordOutcomeUnknown" }

// Validate checks the envelope and the action id.
func (c RecordOutcomeUnknown) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	if c.ActionID == (uuid.UUID{}) {
		return errors.New("RecordOutcomeUnknown: ActionID is zero")
	}
	return nil
}

// ResolveActionOutcome records a person's resolution of an OUTCOME_UNKNOWN
// action. Human-only: an AI delegate is not in the command allowlist, and a
// SYSTEM actor is refused in applyResolveOutcome — only someone who looked at
// the tool can say what it shows.
type ResolveActionOutcome struct {
	ActionID  uuid.UUID         `json:"action_id"`
	Outcome   string            `json:"outcome"` // SUCCEEDED | FAILED
	Note      string            `json:"note"`
	Reference string            `json:"reference,omitempty"`
	Artifact  *ArtifactLocation `json:"artifact,omitempty"`
}

// Kind returns "ResolveActionOutcome".
func (ResolveActionOutcome) Kind() string { return "ResolveActionOutcome" }

// Validate checks the envelope, the outcome vocabulary, the note and the
// evidence pointers.
func (c ResolveActionOutcome) Validate(env Envelope) error {
	if err := validateEnvelope(env); err != nil {
		return err
	}
	if c.ActionID == (uuid.UUID{}) {
		return errors.New("ResolveActionOutcome: ActionID is zero")
	}
	switch c.Outcome {
	case ActionStatusSucceeded, ActionStatusFailed:
	default:
		return fmt.Errorf("ResolveActionOutcome: outcome must be %s or %s, got %q", ActionStatusSucceeded, ActionStatusFailed, c.Outcome)
	}
	if strings.TrimSpace(c.Note) == "" {
		return errors.New("ResolveActionOutcome: note is required — what was checked, and what it showed")
	}
	if utf8.RuneCountInString(c.Note) > resolutionNoteMaxRunes {
		return fmt.Errorf("ResolveActionOutcome: note exceeds %d chars", resolutionNoteMaxRunes)
	}
	if utf8.RuneCountInString(c.Reference) > artifactLocationMaxRunes {
		return fmt.Errorf("ResolveActionOutcome: reference exceeds %d chars", artifactLocationMaxRunes)
	}
	return validateArtifact("ResolveActionOutcome: artifact", trimArtifact(c.Artifact))
}

// applyOutcomeUnknown builds the (ActionOutcomeUnknown, action-result
// Interpretation) pair. Legal only from EXECUTING.
func applyOutcomeUnknown(env Envelope, state aggregateState, c RecordOutcomeUnknown, newID idSource) ([]Event, error) {
	act, ok := state.Actions[c.ActionID]
	if !ok {
		return nil, fmt.Errorf("RecordOutcomeUnknown rejected: action %s does not exist", c.ActionID)
	}
	if act.Status != ActionStatusExecuting {
		return nil, fmt.Errorf("RecordOutcomeUnknown rejected: action %s is %s, not EXECUTING", c.ActionID, act.Status)
	}
	interpID := newID()
	payload, err := json.Marshal(ActionOutcomeUnknown{
		ActionID:               c.ActionID,
		Detail:                 c.Detail,
		ResultInterpretationID: interpID,
	})
	if err != nil {
		return nil, err
	}
	summary := act.label() + ": outcome unknown"
	if c.Detail != "" {
		summary += " — " + c.Detail
	}
	domain := lifecycleDomainEvent(env, state.Seq+1, EventTypeActionOutcomeUnknown, payload)
	interp, err := interpretationEvent(env, state.Seq+2, interpID, InterpretationActionResult, summary, nil)
	if err != nil {
		return nil, err
	}
	return []Event{domain, interp}, nil
}

// applyResolveOutcome builds the (ActionOutcomeResolved, action-resolution
// Interpretation) pair. Legal only from OUTCOME_UNKNOWN, and only for a
// person.
func applyResolveOutcome(env Envelope, state aggregateState, c ResolveActionOutcome, newID idSource) ([]Event, error) {
	if env.Actor.Kind == ActorSystem {
		return nil, errors.New("ResolveActionOutcome rejected: only a person who checked the tool can resolve an outcome")
	}
	act, ok := state.Actions[c.ActionID]
	if !ok {
		return nil, fmt.Errorf("ResolveActionOutcome rejected: action %s does not exist", c.ActionID)
	}
	if act.Status != ActionStatusOutcomeUnknown {
		return nil, fmt.Errorf("ResolveActionOutcome rejected: %s is %s; only an action whose outcome is unknown can be resolved", act.label(), act.Status)
	}
	interpID := newID()
	note := strings.TrimSpace(c.Note)
	payload, err := json.Marshal(ActionOutcomeResolved{
		ActionID:                   c.ActionID,
		Outcome:                    c.Outcome,
		Note:                       note,
		Reference:                  strings.TrimSpace(c.Reference),
		Artifact:                   trimArtifact(c.Artifact),
		ResolutionInterpretationID: interpID,
	})
	if err != nil {
		return nil, err
	}
	summary := fmt.Sprintf("%s: resolved %s after checking the tool — %s", act.label(), c.Outcome, note)
	domain := lifecycleDomainEvent(env, state.Seq+1, EventTypeActionOutcomeResolved, payload)
	interp, err := interpretationEvent(env, state.Seq+2, interpID, InterpretationActionResolution, summary, nil)
	if err != nil {
		return nil, err
	}
	return []Event{domain, interp}, nil
}
