package server

import (
	"net/http"
	"strings"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/module"
)

// CitationsResponse answers GET /api/knowledge/citations?kind=sop|case&ref=
// (adr/0109): the investigations that consulted one piece of knowledge, each
// with whether any act applied it, and — for a past case — the procedures
// learned from it (SOPs whose source URL is reckon://investigation/<ref>).
type CitationsResponse struct {
	Kind           string                        `json:"kind"`
	Ref            string                        `json:"ref"`
	Investigations []aggregate.KnowledgeCitation `json:"investigations"`
	Learned        []LearnedProcedure            `json:"learned,omitempty"`
}

// LearnedProcedure names a procedure accepted from a candidate drafted at an
// investigation's conclusion (09 §9.7): the inverse read of its source URL.
type LearnedProcedure struct {
	SOPID string `json:"sop_id"`
	Title string `json:"title"`
}

// CitationCountsResponse answers GET /api/knowledge/citations/counts?kind=:
// per knowledge ref, the distinct investigations that consulted it.
type CitationCountsResponse struct {
	Kind   string         `json:"kind"`
	Counts map[string]int `json:"counts"`
}

func citationKind(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case aggregate.CitationKindSOP, "":
		return aggregate.CitationKindSOP, true
	case aggregate.CitationKindCase:
		return aggregate.CitationKindCase, true
	}
	return "", false
}

// citations handles GET /api/knowledge/citations?kind=&ref= (any reader).
func (b *Backend) citations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	b.requireRolesOrDeny(w, r, anyReader, func(w http.ResponseWriter, r *http.Request) {
		kind, ok := citationKind(r.URL.Query().Get("kind"))
		ref := strings.TrimSpace(r.URL.Query().Get("ref"))
		if !ok || ref == "" {
			writeJSONError(w, http.StatusBadRequest, "kind must be sop or case and ref is required")
			return
		}
		rows, err := aggregate.ListKnowledgeCitations(r.Context(), b.cfg.Handler.DB(), module.SingleTenantUUID, kind, ref)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "citations: "+err.Error())
			return
		}
		out := CitationsResponse{Kind: kind, Ref: ref, Investigations: rows}
		if kind == aggregate.CitationKindCase && b.cfg.Knowledge != nil {
			id := strings.TrimPrefix(ref, "grouping--")
			if sop, found, err := b.cfg.Knowledge.LookupBySource(r.Context(), module.SingleTenantUUID, "reckon://investigation/"+id); err == nil && found {
				out.Learned = []LearnedProcedure{{SOPID: sop.ID.String(), Title: sop.Title}}
			}
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// citationCounts handles GET /api/knowledge/citations/counts?kind= (any reader).
func (b *Backend) citationCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	b.requireRolesOrDeny(w, r, anyReader, func(w http.ResponseWriter, r *http.Request) {
		kind, ok := citationKind(r.URL.Query().Get("kind"))
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "kind must be sop or case")
			return
		}
		counts, err := aggregate.CountKnowledgeCitations(r.Context(), b.cfg.Handler.DB(), module.SingleTenantUUID, kind)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "citation counts: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, CitationCountsResponse{Kind: kind, Counts: counts})
	})
}
