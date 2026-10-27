package server

// The two-tier investigation summary at any state (adr/0103; design/06 §3.2;
// design/ui/08 §8.4 Summary): GET /investigations/{id}/summary folds the
// projections into facts and carries the latest narrative with its
// authorship; PUT /investigations/{id}/narrative appends a revision — the
// AI's draft from the client plane (agent token → AI_DELEGATED) or the
// analyst's edit (HUMAN). Nothing here holds a model key.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/module"
	"github.com/sd-strax/reckon/summary"
)

// SummaryResponse is the facts plus the narrative lineage.
type SummaryResponse struct {
	summary.Facts
	Narrative  *summary.Narrative  `json:"narrative,omitempty"`
	Authorship summary.Authorship  `json:"authorship"`
	Revisions  []summary.Narrative `json:"revisions"`
	// HandoffNote is the rendered handoff text (08 §8.4), built here so the
	// clipboard copy and the export agree.
	HandoffNote string `json:"handoff_note"`
}

// NarrativeBody is the PUT payload.
type NarrativeBody struct {
	Body           string `json:"body"`
	GeneratorModel string `json:"generator_model,omitempty"`
}

// investigationSummaryRoute serves GET /investigations/{id}/summary (any reader).
func (b *Backend) investigationSummaryRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	b.requireRolesOrDeny(w, r, anyReader, func(w http.ResponseWriter, r *http.Request) {
		invID, ok := investigationSubresourceID(r.URL.Path, "summary")
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid investigation id in path")
			return
		}
		if b.cfg.Handler == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
			return
		}
		out, err := b.buildSummary(r, invID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "summary: "+err.Error())
			return
		}
		if out == nil {
			writeJSONError(w, http.StatusNotFound, "no investigation "+invID.String())
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func (b *Backend) buildSummary(r *http.Request, invID uuid.UUID) (*SummaryResponse, error) {
	ctx := r.Context()
	db := b.cfg.Handler.DB()
	ic, err := aggregate.LoadInvestigationCurrent(ctx, db, invID)
	if err != nil {
		if errors.Is(err, errNoRows()) || strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, err
	}
	hyps, err := aggregate.ListHypotheses(ctx, db, invID)
	if err != nil {
		return nil, err
	}
	preds, err := aggregate.ListPredictions(ctx, db, invID)
	if err != nil {
		return nil, err
	}
	actions, err := aggregate.ListActionCurrents(ctx, db, invID)
	if err != nil {
		return nil, err
	}
	thread, err := b.loadThread(ctx, invID)
	if err != nil {
		return nil, err
	}
	in := summary.Inputs{Investigation: ic, Hypotheses: hyps, Predictions: preds, Actions: actions}
	for _, e := range thread {
		in.Thread = append(in.Thread, summary.ThreadFact{InterpretationID: e.InterpretationID, ActorName: firstNonEmpty(e.Actor.PrincipalName, e.Actor.Principal), ConsultedSOPs: e.ConsultedSOPs})
	}
	if len(thread) > 0 {
		in.CreatedAt = thread[0].OccurredAt
	}
	if b.cfg.Comms != nil {
		if n, err := b.cfg.Comms.OpenCount(ctx, invID); err == nil {
			in.AwaitingReplies = n
		}
	}
	if b.cfg.OwnerItems != nil {
		if items, err := b.cfg.OwnerItems.List(ctx, invID); err == nil {
			for _, it := range items {
				in.OwnerItems = append(in.OwnerItems, summary.OwnerFact{Title: it.Title, Owner: it.Owner, Status: it.Status})
			}
		}
	}
	out := &SummaryResponse{Facts: summary.BuildFacts(in), Revisions: []summary.Narrative{}}
	if b.cfg.Narratives != nil {
		revs, err := b.cfg.Narratives.List(ctx, invID)
		if err != nil {
			return nil, err
		}
		out.Revisions = revs
		if len(revs) > 0 {
			latest := revs[len(revs)-1]
			out.Narrative = &latest
		}
		out.Authorship = summary.AuthorshipOf(revs, ic.LastEventSequence)
	}
	body := ""
	if out.Narrative != nil {
		body = out.Narrative.Body
	}
	out.HandoffNote = summary.HandoffNote(out.Facts, body)
	return out, nil
}

// investigationNarrativeRoute serves PUT /investigations/{id}/narrative (analyst).
func (b *Backend) investigationNarrativeRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w, "PUT")
		return
	}
	b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, func(w http.ResponseWriter, r *http.Request) {
		invID, ok := investigationSubresourceID(r.URL.Path, "narrative")
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid investigation id in path")
			return
		}
		if b.cfg.Narratives == nil || b.cfg.Handler == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "narratives not configured")
			return
		}
		var body NarrativeBody
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad request body: "+err.Error())
			return
		}
		ic, err := aggregate.LoadInvestigationCurrent(r.Context(), b.cfg.Handler.DB(), invID)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "no investigation "+invID.String())
			return
		}
		claims, _ := authz.FromContext(r.Context())
		kind := summary.AuthorHuman
		if claims.DelegateKind != "" {
			kind = summary.AuthorAI
		}
		n, err := b.cfg.Narratives.Put(r.Context(), module.SingleTenantUUID, invID, summary.Narrative{
			Body: body.Body, GeneratorModel: body.GeneratorModel, AsOfSequence: ic.LastEventSequence,
			AuthorKind: kind, AuthorID: claims.Subject, AuthorName: claims.PreferredUsername, CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, n)
	})
}
