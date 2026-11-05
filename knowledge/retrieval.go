package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/knowledge/substrate"
)

// Coverage is retrieval's coarse outcome (design/06 §4.3), mirroring the
// substrate's verdict. EMPTY is meaningful evidence-of-absence: "we have no
// institutional procedure for this."
const (
	CoverageComplete = "COMPLETE"
	CoverageEmpty    = "EMPTY"
)

// Recall units (design/06 §4.2): what one hit stands for. A procedure answers
// "which procedure applies"; a section answers "what do our procedures say
// about this", and may return two pieces of one procedure.
const (
	UnitProcedure = "procedure"
	UnitSection   = "section"
)

// RecallRequest is the recall_sops query (design/06 §4). A present query drives
// semantic (or, on the degraded backend, keyword) ranking; tags hard-filter
// before ranking; an empty query browses by tag, newest first.
type RecallRequest struct {
	Query          string   `json:"query"`
	Tags           []string `json:"tags,omitempty"`
	Limit          int      `json:"limit"`
	IncludeRetired bool     `json:"include_retired"`
	// Unit is UnitProcedure (default) or UnitSection.
	Unit string `json:"unit,omitempty"`
	// ExcerptBudget bounds a hit's excerpt in runes; zero means the store's
	// configured budget. It can shrink an excerpt, never grow a section.
	ExcerptBudget int `json:"excerpt_budget,omitempty"`
}

// SectionRef locates the section a hit carries within its document (design/06
// §4.2): where in the source the excerpt came from.
type SectionRef struct {
	Idx         int      `json:"idx"`
	Of          int      `json:"of"`
	HeadingPath []string `json:"heading_path"`
	Part        int      `json:"part,omitempty"`
	Parts       int      `json:"parts,omitempty"`
	Location    string   `json:"location"`
}

// SOPMatch is one ranked retrieval result (design/06 §4). Excerpt is the text
// of Section — the piece that matched — up to the budget; Outline is one
// location per section of the document, so the reader knows what else it
// holds and can read it by section.
type SOPMatch struct {
	SOPID          uuid.UUID  `json:"sop_id"`
	Title          string     `json:"title"`
	Excerpt        string     `json:"excerpt"`
	Truncated      bool       `json:"truncated,omitempty"`
	Section        SectionRef `json:"section"`
	Outline        []string   `json:"outline"`
	Score          float64    `json:"score"`
	MatchRationale string     `json:"match_rationale"`
	Tags           []string   `json:"tags,omitempty"`
	Recommendation string     `json:"recommendation,omitempty"`
	Status         string     `json:"status"`
	// Backend attributes the ranker that produced this result (substrate §5.2)
	// — "vector-cosine/<model>" or the degraded "pg-fts", so a consumer can tell
	// semantic recall from the keyword fallback.
	Backend string `json:"backend,omitempty"`
}

// RecallResult is the recall_sops response envelope.
type RecallResult struct {
	Results     []SOPMatch `json:"results"`
	Coverage    string     `json:"coverage"`
	RetrievalAt time.Time  `json:"retrieval_at"`
}

const defaultRecallLimit = 5

// RecallSOPs ranks the tenant's SOPs against the request (design/06 §4) by
// delegating to the substrate's semantic recall over the `procedures` corpus,
// then mapping entries back to the SOP shape. An empty query is not a substrate
// recall (which requires one) — it browses by tag, newest first.
func (s *Store) RecallSOPs(ctx context.Context, tenantID uuid.UUID, req RecallRequest) (RecallResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	unit, err := substrateUnit(req.Unit)
	if err != nil {
		return RecallResult{}, fmt.Errorf("recall_sops: %w", err)
	}
	if strings.TrimSpace(req.Query) == "" {
		return s.browseByTag(ctx, tenantID, req, limit)
	}

	res, err := s.sub.Recall(ctx, tenantID.String(), substrate.RecallQuery{
		Corpus:         CorpusProcedures,
		Mode:           substrate.ModeRank,
		Unit:           unit,
		Query:          req.Query,
		Tags:           req.Tags,
		Limit:          limit,
		IncludeRetired: req.IncludeRetired,
		ExcerptBudget:  s.budgetFor(req.ExcerptBudget),
	})
	if err != nil {
		return RecallResult{}, fmt.Errorf("recall_sops: %w", err)
	}
	out := RecallResult{
		Coverage:    string(res.Coverage),
		RetrievalAt: res.RetrievalAt,
		Results:     make([]SOPMatch, 0, len(res.Results)),
	}
	backend := rankerLabel(res.Ranker)
	for _, h := range res.Results {
		lineage, _ := lineageOf(h.Tags)
		out.Results = append(out.Results, SOPMatch{
			SOPID:          lineage,
			Title:          h.Title,
			Excerpt:        h.Excerpt,
			Truncated:      h.Truncated,
			Section:        sectionRefOf(h.Section),
			Outline:        h.Outline,
			Score:          h.Score,
			MatchRationale: h.MatchRationale,
			Tags:           stripReserved(h.Tags),
			Recommendation: h.Advice,
			Status:         sopStatus(h.Status),
			Backend:        backend,
		})
	}
	return out, nil
}

// browseByTag is the empty-query path: current SOPs, tag-overlap filtered,
// newest first. Preserves the pre-substrate "no query, just tags" behavior the
// substrate's recall (which requires a query) intentionally doesn't cover.
// With no query there is no matched section: the hit carries the first
// section and the outline, cut by the same rule the substrate stores.
func (s *Store) browseByTag(ctx context.Context, tenantID uuid.UUID, req RecallRequest, limit int) (RecallResult, error) {
	entries, err := s.sub.List(ctx, tenantID.String(), CorpusProcedures, substrate.ListFilter{IncludeRetired: req.IncludeRetired})
	if err != nil {
		return RecallResult{}, fmt.Errorf("recall_sops (browse): %w", err)
	}
	res := RecallResult{Coverage: CoverageEmpty, RetrievalAt: time.Now().UTC()}
	for _, e := range entries {
		if len(req.Tags) > 0 && len(intersect(req.Tags, stripReserved(e.Tags))) == 0 {
			continue
		}
		lineage, ok := lineageOf(e.Tags)
		if !ok {
			continue
		}
		excerpt, truncated, section, outline := firstSection(e.Body, s.budgetFor(req.ExcerptBudget))
		res.Results = append(res.Results, SOPMatch{
			SOPID:          lineage,
			Title:          e.Title,
			Excerpt:        excerpt,
			Truncated:      truncated,
			Section:        section,
			Outline:        outline,
			MatchRationale: browseRationale(req.Tags, stripReserved(e.Tags)),
			Tags:           stripReserved(e.Tags),
			Recommendation: e.Advice,
			Status:         sopStatus(e.Status),
		})
	}
	// List already returns newest-created first; a revised SOP's new row keeps
	// that ordering meaningful.
	if len(res.Results) > limit {
		res.Results = res.Results[:limit]
	}
	if len(res.Results) > 0 {
		res.Coverage = CoverageComplete
	}
	return res, nil
}

// substrateUnit maps the request's unit onto the substrate's, which is
// host-free and says ENTRY where reckon says procedure.
func substrateUnit(unit string) (substrate.Unit, error) {
	switch unit {
	case "", UnitProcedure:
		return substrate.UnitEntry, nil
	case UnitSection:
		return substrate.UnitSection, nil
	default:
		return "", fmt.Errorf("%w: unit %q (want %q or %q)", ErrInvalidRequest, unit, UnitProcedure, UnitSection)
	}
}

// budgetFor is the excerpt budget a request gets: its own when it says, the
// store's otherwise. The substrate caps either at its section budget.
func (s *Store) budgetFor(requested int) int {
	if requested > 0 {
		return requested
	}
	return s.excerptBudget
}

func sectionRefOf(r substrate.SectionRef) SectionRef {
	path := r.HeadingPath
	if path == nil {
		path = []string{}
	}
	return SectionRef{Idx: r.Idx, Of: r.Of, HeadingPath: path, Part: r.Part, Parts: r.Parts, Location: r.Location}
}

// firstSection renders a hit for a document no query matched (browse): the
// first section within the budget, the section ref and the outline, under
// the same cut the substrate stores. budget <= 0 means uncut; a body with no
// sections (blank) yields an empty excerpt.
func firstSection(body string, budget int) (excerpt string, truncated bool, ref SectionRef, outline []string) {
	secs := substrate.SplitSections(body, 0)
	outline = make([]string, 0, len(secs))
	for _, s := range secs {
		outline = append(outline, s.Location())
	}
	if len(secs) == 0 {
		return "", false, SectionRef{HeadingPath: []string{}}, outline
	}
	first := secs[0]
	excerpt = first.Text
	if r := []rune(excerpt); budget > 0 && len(r) > budget {
		excerpt, truncated = string(r[:budget])+"…", true
	}
	path := first.HeadingPath
	if path == nil {
		path = []string{}
	}
	ref = SectionRef{Idx: first.Idx, Of: len(secs), HeadingPath: path, Part: first.Part, Parts: first.Parts, Location: first.Location()}
	return excerpt, truncated, ref, outline
}

func rankerLabel(r substrate.Ranker) string {
	if r.BackendVersion == "" {
		return r.Backend
	}
	return r.Backend + "/" + r.BackendVersion
}

func browseRationale(reqTags, sopTags []string) string {
	if overlap := intersect(reqTags, sopTags); len(overlap) > 0 {
		return "tags " + strings.Join(overlap, ", ") + " (hard filter); recency"
	}
	return "recency (no query)"
}

func intersect(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, x := range b {
		set[x] = true
	}
	var out []string
	for _, x := range a {
		if set[x] {
			out = append(out, x)
		}
	}
	return out
}
