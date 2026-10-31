package server

import (
	"context"
	"net/http"
	"time"

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

// ThreadActorView is who authored one reasoning act — the accountability
// surface of "AI is a delegate, never a principal": Principal is always the
// human, Kind says whether the AI (or the system) acted on their behalf.
type ThreadActorView struct {
	Principal string `json:"principal"`
	// PrincipalName is the display name the identity provider presented at the
	// time of the act (adr/0077); empty on events recorded before it existed.
	PrincipalName string `json:"principal_name,omitempty"`
	Kind          string `json:"kind"` // HUMAN | AI_DELEGATED | SYSTEM
	// Model names the LLM for AI-delegated acts (actor.delegate).
	Model string `json:"model,omitempty"`
}

// ThreadEntryView is one reasoning act in the thread.
type ThreadEntryView struct {
	SequenceNo         int64           `json:"sequence_no"`
	OccurredAt         time.Time       `json:"occurred_at"`
	Actor              ThreadActorView `json:"actor"`
	InterpretationID   string          `json:"interpretation_id"`
	InterpretationType string          `json:"interpretation_type"`
	Summary            string          `json:"summary"`
	Confidence         string          `json:"confidence,omitempty"`
	InputRefs          []string        `json:"input_refs,omitempty"`
	OutputRefs         []string        `json:"output_refs,omitempty"`
	// ToolCalls counts the act's recorded tool dispatches; HasTranscript
	// reports a committed transcript in the side store. Presence indicators —
	// the bodies stay in their stores (GET .../transcript serves the body).
	ToolCalls     int  `json:"tool_calls,omitempty"`
	HasTranscript bool `json:"has_transcript,omitempty"`

	// Superseded reports that a later interpretation.superseded retracted this
	// act (e.g. a revised verdict, or an un-pinned finding). The act stays in the
	// thread — no deletion — but a surface renders it struck, not current.
	Superseded bool `json:"superseded,omitempty"`

	// Disposition is a verdict act's disposition (BENIGN | SUSPICIOUS |
	// MALICIOUS), so the thread can say what a verdict was, and what a
	// revision changed, without the rationale standing in for it.
	Disposition string `json:"disposition,omitempty"`

	// ConsultedSOPs is the act's knowledge-retrieval provenance
	// (design/ui 02 §2.11 renders the chips).
	ConsultedSOPs []aggregate.ConsultedSOP `json:"consulted_sops,omitempty"`

	// ConsultedSimilar is the act's similar-investigation provenance (06 §6).
	ConsultedSimilar []aggregate.ConsultedSimilarInvestigation `json:"consulted_similar_investigations,omitempty"`
}

// listInvestigationThread serves the thread. Any authenticated reader
// (viewer/analyst/auditor — enforced by the caller).
func (b *Backend) listInvestigationThread(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	invID, ok := investigationSubresourceID(r.URL.Path, "thread")
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid investigation id in path")
		return
	}
	out, err := b.loadThread(r.Context(), invID)
	if err != nil {
		writeInternalError(w, "load thread", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thread": out})
}

// loadThread reassembles the thread views for one investigation — shared by
// the thread endpoint and the markdown export (one behavior, two renderings).
func (b *Backend) loadThread(ctx context.Context, invID uuid.UUID) ([]ThreadEntryView, error) {
	acts, err := b.cfg.Handler.Reads().Thread(ctx, invID)
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
			Confidence:         rec.Confidence,
			InputRefs:          rec.InputRefs,
			OutputRefs:         rec.OutputRefs,
			ToolCalls:          len(rec.ToolCallRefs),
			HasTranscript:      rec.TranscriptRef != nil,
			Superseded:         a.Superseded,
			Disposition:        disposition,
			ConsultedSOPs:      rec.ConsultedSOPs,
			ConsultedSimilar:   rec.ConsultedSimilar,
		})
	}
	return out, nil
}

func threadActor(a aggregate.Actor) ThreadActorView {
	v := ThreadActorView{Principal: a.PrincipalID, PrincipalName: a.PrincipalName, Kind: a.Kind}
	if v.Kind == "" {
		v.Kind = aggregate.ActorHuman
	}
	if a.Delegate != nil {
		v.Model = a.Delegate.Model
	}
	return v
}
