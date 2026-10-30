package aggregate

import (
	"fmt"

	"github.com/google/uuid"
)

// systemEventType is the domain event a system-emitted command records, so a
// repeat of the command can be matched against what is already on the stream.
// Empty for any command that is not system-emitted.
func systemEventType(cmd Command) string {
	switch cmd.(type) {
	case DispatchAction:
		return EventTypeActionDispatched
	case ResultAction:
		return EventTypeActionResulted
	case ExpireAction:
		return EventTypeActionExpired
	case ReverseAction:
		return EventTypeActionReversed
	case RecordReversalAttempt:
		return EventTypeActionReversalAttempted
	case RecordOutcomeUnknown:
		return EventTypeActionOutcomeUnknown
	default:
		return ""
	}
}

// alreadyApplied reports whether a SYSTEM command has already been recorded on
// the stream under the same correlation id. Temporal runs an activity at least
// once: a commit whose completion never reaches the server is retried, and the
// retry arrives here with the correlation id its activity derived the first
// time. Recognising it makes the retry a no-op instead of a rejection.
//
// Only SYSTEM envelopes are checked. Commands from people and delegates mint a
// fresh correlation id per request, so a match there would mean a reused id,
// and dropping that command silently is never the right answer.
//
// A match whose domain event is not the one this command records is an error:
// the id was reused for a different command.
func alreadyApplied(env Envelope, cmd Command, stream []Event) (bool, error) {
	if env.Actor.Kind != ActorSystem {
		return false, nil
	}
	for _, e := range stream {
		if e.CorrelationID != env.CorrelationID {
			continue
		}
		if want := systemEventType(cmd); e.Type != want {
			return false, fmt.Errorf("%s rejected: correlation id %s already recorded %s (seq %d)",
				cmd.Kind(), env.CorrelationID, e.Type, e.SequenceNo)
		}
		return true, nil
	}
	return false, nil
}

// turnAlreadyRecorded reports the interpretation an agent turn was already
// committed as, when cmd records a transcript whose turn id is on the stream.
// A session retries a turn commit whose response it never saw (adr/0231); the
// retry carries the same turn id and the same bytes, so it is recognised and
// nothing is appended. The same turn id with a different transcript is a
// rejection: the id was reused for a different turn.
func turnAlreadyRecorded(state aggregateState, cmd Command) (uuid.UUID, error) {
	ri, ok := cmd.(RecordInterpretation)
	if !ok || ri.Transcript == nil || ri.Transcript.TurnID == "" {
		return uuid.UUID{}, nil
	}
	prior, ok := state.Turns[ri.Transcript.TurnID]
	if !ok {
		return uuid.UUID{}, nil
	}
	if prior.ContentHash != HashContent(ri.Transcript.Body) {
		return uuid.UUID{}, fmt.Errorf("RecordInterpretation rejected: turn %s is already recorded as interpretation %s with a different transcript",
			ri.Transcript.TurnID, prior.InterpretationID)
	}
	return prior.InterpretationID, nil
}
