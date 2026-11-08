package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/knowledge"
	"github.com/sd-strax/reckon/module"
)

// Knowledge citations across investigations (design/06 §6.1, design/ui binding
// §6 gap 22, adr/0109): the fold of every act's consulted_sops and
// consulted_similar_investigations, read per procedure or past case. The words
// are adr/0236's: an investigation "cited" a procedure when the agent's text
// named it by ref, or "recalled, not cited"; never "applied" or "followed".

// CitationView is one investigation that consulted a procedure or a past case.
type CitationView struct {
	InvestigationID string    `json:"investigation_id"`
	Title           string    `json:"title"`
	Status          string    `json:"status"`
	Cited           bool      `json:"cited"`
	LastAt          time.Time `json:"last_at"`
	// Acts is how many reasoning acts in the investigation carried the ref.
	Acts int `json:"acts"`
	// Revisions are the distinct SOP revisions those acts had in front of
	// them; empty for a past case.
	Revisions []int `json:"revisions"`
}

// KnowledgeCitationsResponse answers GET /api/sops/{id}/citations and
// GET /api/knowledge/case_summaries/{ref}/citations: distinct investigations
// with a citing act (cited_in), those that only recalled (recalled_in), and
// one row per investigation, most recently touched first.
type KnowledgeCitationsResponse struct {
	Ref            string         `json:"ref"`
	CitedIn        int            `json:"cited_in"`
	RecalledIn     int            `json:"recalled_in"`
	Investigations []CitationView `json:"investigations"`
}

// SOPRow is one procedure in GET /api/sops: the facade's SOP plus the count
// the server composes from the citation projection.
type SOPRow struct {
	knowledge.SOP
	CitedIn int `json:"cited_in"`
}

// CaseSummaryRow is one past case in GET /api/knowledge/case_summaries, with
// the same composed count.
type CaseSummaryRow struct {
	knowledge.CaseSummaryView
	CitedIn int `json:"cited_in"`
}

// LearnedProcedure is a SOP accepted into the library from a concluded
// investigation (source url reckon://investigation/<id>, adr/0156).
type LearnedProcedure struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// CaseSummaryDetail answers GET /api/knowledge/case_summaries/{ref}: the
// summary plus the procedures learned from that case (design/ui 09 §9.4).
type CaseSummaryDetail struct {
	knowledge.CaseSummaryView
	LearnedProcedures []LearnedProcedure `json:"learned_procedures"`
}

// listSOPCitations serves GET /api/sops/{id}/citations. Any reader. It reads
// the projection alone, so an unknown id answers an empty list: the corpus is
// not consulted, and a retired or purged procedure keeps its history.
func (b *Backend) listSOPCitations(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	b.knowledgeCitations(w, r, id.String())
}

// listCaseCitations serves GET /api/knowledge/case_summaries/{ref}/citations.
// Any reader.
func (b *Backend) listCaseCitations(w http.ResponseWriter, r *http.Request) {
	b.knowledgeCitations(w, r, r.PathValue("ref"))
}

func (b *Backend) knowledgeCitations(w http.ResponseWriter, r *http.Request, ref string) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	rows, err := b.cfg.Reads.ListKnowledgeCitations(r.Context(), ref)
	if err != nil {
		writeInternalError(w, "list citations", err)
		return
	}
	out := KnowledgeCitationsResponse{Ref: ref, Investigations: []CitationView{}}
	for _, c := range rows {
		if c.Cited {
			out.CitedIn++
		} else {
			out.RecalledIn++
		}
		revs := c.Revisions
		if revs == nil {
			revs = []int{}
		}
		out.Investigations = append(out.Investigations, CitationView{
			InvestigationID: c.AggregateID.String(),
			Title:           c.Title,
			Status:          c.Status,
			Cited:           c.Cited,
			LastAt:          c.LastAt,
			Acts:            c.Acts,
			Revisions:       revs,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// citationCounts folds the counts for a list's refs, or answers an empty map
// when no aggregate handler is configured (a knowledge-only deployment lists
// its corpus with every count zero).
func (b *Backend) citationCounts(ctx context.Context, refs []string) (map[string]aggregate.CitationCount, error) {
	if b.cfg.Handler == nil {
		return map[string]aggregate.CitationCount{}, nil
	}
	return b.cfg.Reads.CitationCounts(ctx, refs)
}

// learnedProcedures lists the SOPs whose source is the investigation behind a
// summary's ref. A summary's ref is the investigation's id, or its grouping
// ref (grouping--<id>) for a case seeded without a local aggregate.
func (b *Backend) learnedProcedures(ctx context.Context, investigationRef string) ([]LearnedProcedure, error) {
	id := strings.TrimPrefix(investigationRef, "grouping--")
	sops, err := b.cfg.Knowledge.ListBySource(ctx, module.SingleTenantUUID, knowledge.InvestigationSourcePrefix+id)
	if err != nil {
		return nil, err
	}
	out := make([]LearnedProcedure, 0, len(sops))
	for _, s := range sops {
		out = append(out, LearnedProcedure{ID: s.ID.String(), Title: s.Title})
	}
	return out, nil
}
