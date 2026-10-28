package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/knowledge"
	"github.com/sd-strax/reckon/module"
)

// Procedure candidates (adr/0112; design/ui 09 §9.7): the agent's drafts at
// conclusion, stored for review, accepted into the procedure library through
// the import path or discarded with a reason. Routes:
//
//	POST /api/knowledge/candidates                 (analyst) store a draft
//	GET  /api/knowledge/candidates?status=         (reader)  list, newest first
//	GET  /api/knowledge/candidates/{id}            (reader)  one, with the closest existing procedure
//	POST /api/knowledge/candidates/{id}/accept     (analyst) import the (edited) body as a procedure
//	POST /api/knowledge/candidates/{id}/discard    (analyst) with a reason

// CandidateBody is the draft the conclude hook posts (the sidecar's
// SOPCandidate plus the investigation it came from).
type CandidateBody struct {
	InvestigationID string   `json:"investigation_id"`
	Title           string   `json:"title"`
	Body            string   `json:"body"`
	Tags            []string `json:"tags"`
	Recommendation  string   `json:"recommendation"`
	Rationale       string   `json:"rationale"`
	GeneratorModel  string   `json:"generator_model"`
}

// CandidateDetail is one candidate with the closest existing procedure for
// comparison (one recall over the draft's text, computed on read, never
// stored).
type CandidateDetail struct {
	knowledge.Candidate
	Closest *knowledge.SOPMatch `json:"closest,omitempty"`
}

// CandidateAcceptBody carries the body as edited in the review.
type CandidateAcceptBody struct {
	Body string `json:"body"`
}

// CandidateDiscardBody carries the reviewer's reason.
type CandidateDiscardBody struct {
	Reason string `json:"reason"`
}

func (b *Backend) candidatesUnavailable(w http.ResponseWriter) bool {
	if b.cfg.Candidates == nil || b.cfg.Knowledge == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "the knowledge layer is not configured")
		return true
	}
	return false
}

// candidatesCollection routes /api/knowledge/candidates.
func (b *Backend) candidatesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, b.createCandidate)
	case http.MethodGet:
		b.requireRolesOrDeny(w, r, anyReader, b.listCandidates)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (b *Backend) createCandidate(w http.ResponseWriter, r *http.Request) {
	if b.candidatesUnavailable(w) {
		return
	}
	var body CandidateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad request body: "+err.Error())
		return
	}
	invID, err := uuid.Parse(strings.TrimPrefix(body.InvestigationID, "grouping--"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "investigation_id must be a UUID")
		return
	}
	claims, _ := authz.FromContext(r.Context())
	c, err := b.cfg.Candidates.Create(r.Context(), knowledge.Candidate{
		TenantID: module.SingleTenantUUID, InvestigationID: invID,
		Title: strings.TrimSpace(body.Title), Body: strings.TrimSpace(body.Body), Tags: body.Tags,
		Recommendation: body.Recommendation, Rationale: body.Rationale, GeneratorModel: body.GeneratorModel,
		DraftedBy: claims.Subject, DraftedByName: claims.PreferredUsername,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (b *Backend) listCandidates(w http.ResponseWriter, r *http.Request) {
	if b.candidatesUnavailable(w) {
		return
	}
	out, err := b.cfg.Candidates.List(r.Context(), module.SingleTenantUUID, r.URL.Query().Get("status"))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "candidates: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": out})
}

// candidateItem routes /api/knowledge/candidates/{id}[/accept|/discard].
func (b *Backend) candidateItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/knowledge/candidates/"), "/")
	parts := strings.Split(rest, "/")
	id, err := uuid.Parse(parts[0])
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "candidate id must be a UUID")
		return
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		b.requireRolesOrDeny(w, r, anyReader, func(w http.ResponseWriter, r *http.Request) { b.getCandidate(w, r, id) })
	case len(parts) == 2 && parts[1] == "accept" && r.Method == http.MethodPost:
		b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, func(w http.ResponseWriter, r *http.Request) { b.acceptCandidate(w, r, id) })
	case len(parts) == 2 && parts[1] == "discard" && r.Method == http.MethodPost:
		b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, func(w http.ResponseWriter, r *http.Request) { b.discardCandidate(w, r, id) })
	default:
		writeJSONError(w, http.StatusNotFound, "not found")
	}
}

func (b *Backend) getCandidate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if b.candidatesUnavailable(w) {
		return
	}
	c, err := b.cfg.Candidates.Get(r.Context(), module.SingleTenantUUID, id)
	if errors.Is(err, knowledge.ErrCandidateNotFound) {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "candidate: "+err.Error())
		return
	}
	out := CandidateDetail{Candidate: c}
	// The closest existing procedure, for the reviewer's comparison: one recall
	// over the draft's text; the accepted procedure itself is not "closest".
	if res, err := b.cfg.Knowledge.RecallSOPs(r.Context(), module.SingleTenantUUID, knowledge.RecallRequest{Query: c.Title + "\n" + c.Body, Limit: 2}); err == nil {
		for i := range res.Results {
			m := res.Results[i]
			if c.AcceptedSOPID != nil && m.SOPID == *c.AcceptedSOPID {
				continue
			}
			out.Closest = &m
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (b *Backend) acceptCandidate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if b.candidatesUnavailable(w) {
		return
	}
	var body CandidateAcceptBody
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	c, err := b.cfg.Candidates.Get(r.Context(), module.SingleTenantUUID, id)
	if errors.Is(err, knowledge.ErrCandidateNotFound) {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "candidate: "+err.Error())
		return
	}
	if c.Status == knowledge.CandidateDiscarded {
		writeJSONError(w, http.StatusConflict, "the candidate was discarded")
		return
	}
	text := strings.TrimSpace(body.Body)
	if text == "" {
		text = c.Body
	}
	claims, _ := authz.FromContext(r.Context())
	importer := claims.PreferredUsername
	if importer == "" {
		importer = claims.Subject
	}
	// The import path the "Import SOPs…" command uses, keyed by the
	// investigation's source URL so a re-accept revises rather than duplicates.
	sopID, created, err := b.cfg.Knowledge.Import(r.Context(), knowledge.SOP{
		TenantID: module.SingleTenantUUID,
		Title:    c.Title, Body: text, Tags: c.Tags, Recommendation: c.Recommendation,
		AuthorID:   "reckon (drafted at conclusion)",
		ImportedBy: importer,
		Source:     &knowledge.SOPSource{System: "reckon", URL: "reckon://investigation/" + c.InvestigationID.String()},
		Format:     "markdown",
		Original:   &knowledge.SOPOriginal{Filename: "candidate-" + c.ID.String() + ".md", MediaType: "text/markdown; charset=utf-8", Bytes: []byte(text)},
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "import candidate: "+err.Error())
		return
	}
	updated, err := b.cfg.Candidates.Accept(r.Context(), module.SingleTenantUUID, id, sopID, text, claims.Subject, claims.PreferredUsername, time.Now().UTC())
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	outcome := "revised"
	if created {
		outcome = "created"
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidate": updated, "sop_id": sopID.String(), "outcome": outcome})
}

func (b *Backend) discardCandidate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if b.candidatesUnavailable(w) {
		return
	}
	var body CandidateDiscardBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad request body: "+err.Error())
		return
	}
	claims, _ := authz.FromContext(r.Context())
	c, err := b.cfg.Candidates.Discard(r.Context(), module.SingleTenantUUID, id, strings.TrimSpace(body.Reason), claims.Subject, claims.PreferredUsername, time.Now().UTC())
	switch {
	case errors.Is(err, knowledge.ErrCandidateNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, knowledge.ErrCandidateDecided):
		writeJSONError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, c)
	}
}
