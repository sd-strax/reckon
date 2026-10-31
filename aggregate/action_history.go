package aggregate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ActionHistoryEntry is one action.* event on an action, decoded.
type ActionHistoryEntry struct {
	SequenceNo int64
	Type       string
	OccurredAt time.Time
	Actor      Actor
	// Status is the state the record shows after this event; empty when the
	// event moved nothing. A result shows its final outcome verbatim, so PARTIAL
	// and TIMEOUT stay visible where the action's own status has folded them
	// (08 §6c).
	Status string
	// Event is the decoded payload: one of the Action* event types, or nil for
	// an action event this build does not interpret.
	Event any
}

// ActionHistory returns an action's own events in the aggregate's order. A
// reversal's events name the original as original_action_id and belong to the
// original's history too. A payload that does not decode is an error, never a
// blank row.
func (r *ReadModel) ActionHistory(ctx context.Context, aggID AggregateID, actionID uuid.UUID) ([]ActionHistoryEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT sequence_no, event_type, occurred_at, actor, payload, event_version
		FROM events
		WHERE aggregate_id = $1
		  AND event_type LIKE 'action.%'
		  AND (payload->>'action_id' = $2 OR payload->>'original_action_id' = $2)
		ORDER BY sequence_no`, aggID, actionID.String())
	if err != nil {
		return nil, fmt.Errorf("query action events: %w", err)
	}
	defer rows.Close()

	var out []ActionHistoryEntry
	for rows.Next() {
		var e ActionHistoryEntry
		var actorRaw, payloadRaw []byte
		var version int
		if err := rows.Scan(&e.SequenceNo, &e.Type, &e.OccurredAt, &actorRaw, &payloadRaw, &version); err != nil {
			return nil, fmt.Errorf("scan action event: %w", err)
		}
		if err := CheckEventVersion(e.Type, version); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(actorRaw, &e.Actor); err != nil {
			return nil, fmt.Errorf("decode actor seq %d: %w", e.SequenceNo, err)
		}
		if e.Event, err = decodeActionEvent(e.Type, payloadRaw); err != nil {
			return nil, fmt.Errorf("decode %s seq %d: %w", e.Type, e.SequenceNo, err)
		}
		e.Status = historyStatus(e.Event, actionID)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate action events: %w", err)
	}
	return out, nil
}

func decodeActionEvent(eventType string, raw []byte) (any, error) {
	switch eventType {
	case EventTypeActionRequested:
		return decodeAs[ActionRequested](raw)
	case EventTypeActionPolicyEvaluated:
		return decodeAs[ActionPolicyEvaluated](raw)
	case EventTypeActionApproved:
		return decodeAs[ActionApproved](raw)
	case EventTypeActionEdited:
		return decodeAs[ActionEdited](raw)
	case EventTypeActionRejected:
		return decodeAs[ActionRejected](raw)
	case EventTypeActionExpired:
		return decodeAs[ActionExpired](raw)
	case EventTypeActionDispatched:
		return decodeAs[ActionDispatched](raw)
	case EventTypeActionResulted:
		return decodeAs[ActionResulted](raw)
	case EventTypeActionReversed:
		return decodeAs[ActionReversed](raw)
	case EventTypeActionReversalAttempted:
		return decodeAs[ActionReversalAttempted](raw)
	case EventTypeActionOutcomeUnknown:
		return decodeAs[ActionOutcomeUnknown](raw)
	case EventTypeActionOutcomeResolved:
		return decodeAs[ActionOutcomeResolved](raw)
	}
	return nil, nil
}

func decodeAs[T any](raw []byte) (any, error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func historyStatus(event any, actionID uuid.UUID) string {
	switch p := event.(type) {
	case ActionRequested, ActionEdited:
		return ActionStatusRequested
	case ActionApproved:
		return approvalStatus(p.Authorization)
	case ActionRejected:
		return ActionStatusRejected
	case ActionExpired:
		return ActionStatusExpired
	case ActionDispatched:
		return ActionStatusExecuting
	case ActionResulted:
		return p.FinalOutcome
	case ActionReversed:
		if p.OriginalActionID == actionID {
			return ActionStatusReversed
		}
	case ActionOutcomeUnknown:
		return ActionStatusOutcomeUnknown
	case ActionOutcomeResolved:
		return p.Outcome
	}
	return ""
}
