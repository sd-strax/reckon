package server

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
)

// Pinned evidence + supersession (01 §Pinned evidence): the read surface for
// the pin fold, and the un-pin/retraction path. Pins are recorded through the
// ordinary POST /api/interpretations (type=evidence-pin).

// PinView is one pinned-evidence row. Superseded pins are served with the
// flag set — what was once considered load-bearing stays visible; the UI
// renders them struck, never absent.
type PinView struct {
	InterpretationID string    `json:"interpretation_id"`
	Finding          string    `json:"finding"`
	InputRefs        []string  `json:"input_refs"`
	Actor            string    `json:"actor"` // HUMAN | AI_DELEGATED | SYSTEM
	PinnedAt         time.Time `json:"pinned_at"`
	Superseded       bool      `json:"superseded,omitempty"`
}

// listInvestigationPins serves GET /api/investigations/{id}/pins. Any
// authenticated reader (enforced by the caller).
func (b *Backend) listInvestigationPins(w http.ResponseWriter, r *http.Request, invID uuid.UUID) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	pins, err := b.cfg.Handler.Reads().ListEvidencePins(r.Context(), invID)
	if err != nil {
		writeInternalError(w, "list pins", err)
		return
	}
	out := []PinView{}
	for _, p := range pins {
		kind := p.Actor.Kind
		if kind == "" {
			kind = aggregate.ActorHuman
		}
		out = append(out, PinView{
			InterpretationID: p.InterpretationID.String(),
			Finding:          p.Finding,
			InputRefs:        p.InputRefs,
			Actor:            kind,
			PinnedAt:         p.PinnedAt.Time,
			Superseded:       p.Superseded,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"pins": out})
}

// SupersedeBody is the POST /api/interpretations/{id}/supersede request.
type SupersedeBody struct {
	InvestigationRef string `json:"investigation_ref"`
	Reason           string `json:"reason"`
}

func (b *Backend) supersedeInterpretation(w http.ResponseWriter, r *http.Request, interpID uuid.UUID) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate not configured")
		return
	}
	claims, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth context missing")
		return
	}
	var body SupersedeBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	invID, err := uuid.Parse(body.InvestigationRef)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "investigation_ref is not a valid id")
		return
	}

	env := newEnvelope(invID, actorFromClaims(claims), commandNow())
	res, err := b.cfg.Handler.Handle(r.Context(), env, aggregate.SupersedeInterpretation{
		SupersededID: interpID,
		Reason:       body.Reason,
	})
	if err != nil {
		writeCommandError(w, "supersede interpretation", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"superseded_id": interpID.String(),
		"sequence_no":   res.NewSequenceNo,
	})
}
