package server

// The investigation's narrative (adr/0103): a written account of the record
// so far, read with its authorship and history, and appended as a revision.
// The actor on a write is the token's, as on every command: a delegate's
// draft is attributed to the model under the signed-in person, a person's
// edit to them. Nothing is overwritten.

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
)

// NarrativeAuthorView is who recorded a revision.
type NarrativeAuthorView struct {
	Kind string `json:"kind"` // HUMAN | AI_DELEGATED
	Name string `json:"name,omitempty"`
	// Model is the drafting model, on an AI_DELEGATED revision.
	Model string `json:"model,omitempty"`
}

// NarrativeRevisionView is one revision of the history, newest first.
type NarrativeRevisionView struct {
	Revision   int                 `json:"revision"`
	SequenceNo int64               `json:"sequence_no"`
	OccurredAt time.Time           `json:"occurred_at"`
	Author     NarrativeAuthorView `json:"author"`
	Body       string              `json:"body"`
}

// NarrativeView is the current narrative: its body, who recorded this
// revision, who drafted the first one, the event it was written at (so a
// reader can say how far the record has moved since), and the history.
type NarrativeView struct {
	InvestigationRef string                  `json:"investigation_ref"`
	Revision         int                     `json:"revision"`
	Body             string                  `json:"body"`
	SequenceNo       int64                   `json:"sequence_no"`
	OccurredAt       time.Time               `json:"occurred_at"`
	Author           NarrativeAuthorView     `json:"author"`
	DraftedBy        NarrativeAuthorView     `json:"drafted_by"`
	Revisions        []NarrativeRevisionView `json:"revisions"`
}

// NarrativeRequestBody appends a revision. Model is set by a delegate's
// draft only; the aggregate refuses it on a human actor.
type NarrativeRequestBody struct {
	Body  string `json:"body"`
	Model string `json:"model,omitempty"`
}

func narrativeAuthorView(a aggregate.NarrativeAuthor) NarrativeAuthorView {
	name := a.Name
	if name == "" {
		name = a.PrincipalID
	}
	return NarrativeAuthorView{Kind: a.Kind, Name: name, Model: a.Model}
}

func narrativeView(id uuid.UUID, n aggregate.Narrative) NarrativeView {
	out := NarrativeView{
		InvestigationRef: id.String(),
		Revision:         n.Current.Revision,
		Body:             n.Current.Body,
		SequenceNo:       n.Current.SequenceNo,
		OccurredAt:       n.Current.OccurredAt,
		Author:           narrativeAuthorView(n.Current.Author),
		DraftedBy:        narrativeAuthorView(n.DraftedBy),
		Revisions:        make([]NarrativeRevisionView, 0, len(n.Revisions)),
	}
	for _, r := range n.Revisions {
		out.Revisions = append(out.Revisions, NarrativeRevisionView{Revision: r.Revision, SequenceNo: r.SequenceNo, OccurredAt: r.OccurredAt, Author: narrativeAuthorView(r.Author), Body: r.Body})
	}
	return out
}

// getNarrative handles GET /api/investigations/{id}/narrative. 404 when the
// investigation has none yet (an honest absence, not an error).
func (b *Backend) getNarrative(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	n, err := b.cfg.Reads.Narrative(r.Context(), id)
	if errors.Is(err, aggregate.ErrNoRecord) {
		writeJSONError(w, http.StatusNotFound, "no narrative recorded")
		return
	}
	if err != nil {
		writeInternalError(w, "narrative", err)
		return
	}
	writeJSON(w, http.StatusOK, narrativeView(id, n))
}

// recordNarrative handles POST /api/investigations/{id}/narrative: one more
// revision. Analyst role; the actor kind comes from the token, never the
// body, so a draft is a delegate's and an edit a person's by the record's
// own rule. Shape-guard only; the aggregate owns the body bound and the
// model rule.
func (b *Backend) recordNarrative(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	claims, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth context missing")
		return
	}
	var body NarrativeRequestBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		writeJSONError(w, http.StatusBadRequest, "body is required")
		return
	}
	env := newEnvelope(id, actorFromRequest(r, claims), commandNow())
	if _, err := b.cfg.Handler.Handle(r.Context(), env, aggregate.RecordNarrative{Body: body.Body, Model: body.Model}); err != nil {
		writeCommandError(w, "narrative", err)
		return
	}
	n, err := b.cfg.Reads.Narrative(r.Context(), id)
	if err != nil {
		writeInternalError(w, "narrative", err)
		return
	}
	writeJSON(w, http.StatusOK, narrativeView(id, n))
}

// narrativeOnDetail is the narrative as the investigation detail carries it
// (the Summary lens renders from the snapshot): nil when none was recorded,
// an error when the read failed. A failed read must not look like an
// absence, or an edit over it would drop the text on the record.
func (b *Backend) narrativeOnDetail(r *http.Request, id uuid.UUID) (*NarrativeView, error) {
	n, err := b.cfg.Reads.Narrative(r.Context(), id)
	if errors.Is(err, aggregate.ErrNoRecord) {
		return nil, nil //nolint:nilnil // an absence is the honest answer here
	}
	if err != nil {
		return nil, err
	}
	v := narrativeView(id, n)
	return &v, nil
}
