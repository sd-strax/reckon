package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/stix"
)

// writeEventCitedError answers a refused raw event citation (adr/0364) with
// one 422 that names the field and each event it held, with the observations
// made from each, so the caller knows what to cite instead. An event with no
// observation was not read, and the answer says it cannot be cited. It
// reports false, writing nothing, when err is not an *aggregate.EventCitedError.
func (b *Backend) writeEventCitedError(ctx context.Context, w http.ResponseWriter, action string, err error) bool {
	var cited *aggregate.EventCitedError
	if !errors.As(err, &cited) {
		return false
	}
	writeJSONError(w, http.StatusUnprocessableEntity, action+": "+b.eventCitedMessage(ctx, cited))
	return true
}

// eventCitedMessage says what a refused event citation should have named,
// for each bare id the list held: the observations made from a stored event;
// that a stored event with no observation was not read and cannot be cited;
// that an id the store holds no event for is neither a STIX id nor a known
// event. Without an evidence store, or when a lookup fails, it names the id
// and says to cite the observation made from it.
func (b *Backend) eventCitedMessage(ctx context.Context, cited *aggregate.EventCitedError) string {
	var msg strings.Builder
	msg.WriteString(cited.Field + " holds ids that are not STIX ids. A citation names an observation, and an event is cited through the observation made from it.")
	for _, ref := range cited.Refs {
		msg.WriteString(" " + b.eventCitedLine(ctx, ref))
	}
	return msg.String()
}

// eventCitedLine is the sentence eventCitedMessage gives one bare id.
func (b *Backend) eventCitedLine(ctx context.Context, ref string) string {
	unknown := "Event " + ref + ": cite the observation made from it instead."
	id, err := uuid.Parse(ref)
	if b.cfg.Evidence == nil || err != nil {
		return unknown
	}
	if _, err := b.cfg.Evidence.Event(ctx, id); errors.Is(err, evidence.ErrNotFound) {
		return ref + " is neither a STIX id nor a known event."
	} else if err != nil {
		slog.Warn("event citation refused without its observations", "event", ref, "error", err)
		return unknown
	}
	derived, err := b.cfg.Evidence.EventDerived(ctx, id)
	if err != nil {
		slog.Warn("event citation refused without its observations", "event", ref, "error", err)
		return unknown
	}
	if len(derived.ObservedDataRefs) == 0 {
		return "Event " + ref + " was not read into an observation, so it cannot be cited."
	}
	return "Event " + ref + ": cite an observation made from it instead: " + strings.Join(derived.ObservedDataRefs, ", ") + "."
}

// citableRefs is the part of an older record's citations a new request may
// carry: its STIX ids. A re-request restates the original's grounding, and a
// raw event id the original cited is left out, since a new citation names a
// STIX object (adr/0364). The original record keeps what it cited.
func citableRefs(refs []string) []string {
	var out []string
	for _, r := range refs {
		if stix.IsIdentifier(r) {
			out = append(out, r)
		}
	}
	return out
}
