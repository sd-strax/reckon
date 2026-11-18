package server

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
)

// GET /api/investigations/{id}/thread — the chronological reasoning thread
// (design/13 §4: "interpretations chronological, with rationale and cited
// evidence"). The thread is reassembled from interpretation.recorded events
// alone: every domain transition (lifecycle, action.*) pairs one in the same
// transaction (01, 02 §3), so nothing that changed the investigation is
// missing, and nothing is duplicated. Ordered by sequence_no — the aggregate's
// authoritative order — never occurred_at (caller-supplied wall clock).

// listInvestigationThread serves the thread. Any authenticated reader
// (viewer/analyst/auditor — enforced by the caller).
func (b *Backend) listInvestigationThread(w http.ResponseWriter, r *http.Request, invID uuid.UUID) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	out, err := b.loadThread(r.Context(), invID)
	if err != nil {
		writeInternalError(w, "load thread", err)
		return
	}
	writeJSON(w, http.StatusOK, ThreadResponse{Thread: out})
}

// loadThread reassembles the thread views for one investigation — shared by
// the thread endpoint and the markdown export (one behavior, two renderings).
func (b *Backend) loadThread(ctx context.Context, invID uuid.UUID) ([]ThreadEntryView, error) {
	acts, err := b.cfg.Reads.Thread(ctx, invID)
	if err != nil {
		return nil, err
	}
	out := make([]ThreadEntryView, 0, len(acts))
	for _, a := range acts {
		rec := a.Interpretation
		disposition := ""
		if rec.Verdict != nil {
			disposition = rec.Verdict.Disposition
		}
		out = append(out, ThreadEntryView{
			SequenceNo:         a.SequenceNo,
			OccurredAt:         a.OccurredAt,
			Actor:              threadActor(a.Actor),
			InterpretationID:   rec.InterpretationID.String(),
			InterpretationType: rec.InterpretationType,
			Summary:            rec.Summary,
			Confidence:         aggregate.Confidence(rec.Confidence),
			InputRefs:          rec.InputRefs,
			OutputRefs:         rec.OutputRefs,
			ToolCalls:          len(rec.ToolCallRefs),
			HasTranscript:      rec.TranscriptRef != nil,
			Superseded:         a.Superseded,
			Disposition:        aggregate.VerdictDisposition(disposition),
			ConsultedSOPs:      rec.ConsultedSOPs,
			ConsultedSimilar:   rec.ConsultedSimilar,
		})
	}
	return out, nil
}

func threadActor(a aggregate.Actor) ThreadActorView {
	v := ThreadActorView{Principal: a.PrincipalID, PrincipalName: a.PrincipalName, Roles: a.Roles, RequiredRoles: a.RequiredRoles, Kind: a.Kind}
	if v.Kind == "" {
		v.Kind = aggregate.ActorHuman
	}
	if a.Delegate != nil {
		v.Model = a.Delegate.Model
	}
	return v
}
