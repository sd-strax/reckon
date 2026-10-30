package aggregate

import "fmt"

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
