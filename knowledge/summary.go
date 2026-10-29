package knowledge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/knowledge/substrate"
)

// summarySourcePrefix marks the reserved tag carrying a summary's source
// investigation ref, so a re-derivation of the same concluded investigation
// becomes a substrate revision (design/06 §3.1) rather than a duplicate — which
// also makes the post-conclusion pipeline safely retriable.
const summarySourcePrefix = "__case_source:"

// SummaryAction is one state-changing action a summary records (design/06
// §3.2 actions_taken).
type SummaryAction struct {
	ActionType  string `json:"action_type"`
	TargetCount int    `json:"target_count"`
	Outcome     string `json:"outcome"`
}

// Summary is the domain shape of a concluded-investigation summary (design/06
// §3.1) — assembled by the producer (temporal/summary_activities.go) from the
// aggregate, then mapped here onto the substrate's DERIVED case-summaries
// corpus. It is derived, never authored: identity is the substrate's, and a
// re-derivation revises the prior entry.
type Summary struct {
	InvestigationRef string    // grouping--<uuid>, the source investigation
	Title            string    // the investigation title
	ConclusionAt     time.Time // when the source investigation concluded

	// SummaryText is the recallable body. v0 renders it deterministically from
	// the structured fields; a server-side LLM narrative replaces it later, at
	// which point GeneratorModel names the model (empty = deterministic).
	SummaryText      string
	GeneratorModel   string
	ExtractorVersion string

	// Structured fields (design/06 §3.2), surfaced as tags/meta for recall.
	SeedKind           string
	SeedValue          string
	Techniques         []string // MITRE ATT&CK technique ids
	PrimaryEntities    []string // entity identifiers/refs
	ActionsTaken       []SummaryAction
	ConclusionOutcome  string // succeeded | failed | abandoned | inconclusive
	DurationHours      float64
	VerdictDisposition string
	VerdictRationale   string
}

// WriteSummary maps a domain Summary onto a substrate DERIVED entry in the
// case-summaries corpus and writes it. A re-derivation of the same source
// investigation (resolved via the reserved source tag) is a revision, so the
// pipeline is idempotent under retry. Returns the substrate entry id.
func (s *Store) WriteSummary(ctx context.Context, tenantID uuid.UUID, sum Summary) (uuid.UUID, error) {
	if sum.InvestigationRef == "" {
		return uuid.Nil, fmt.Errorf("WriteSummary: investigation_ref required")
	}
	if sum.Title == "" || sum.SummaryText == "" {
		return uuid.Nil, fmt.Errorf("WriteSummary: title and summary_text required")
	}
	ns := tenantID.String()
	entry := substrate.Entry{
		Title: sum.Title,
		Body:  sum.SummaryText,
		Tags:  summaryTags(sum),
		Meta:  summaryMeta(sum),
		Provenance: &substrate.Provenance{
			Producer:        "case-summarizer",
			ProducerVersion: sum.ExtractorVersion,
			GeneratorModel:  sum.GeneratorModel,
			SourceRef:       sum.InvestigationRef,
		},
	}

	// Re-derivation is a revision (design/06 §3.1): find a prior summary of this
	// source and revise it, else write a fresh one.
	prior, err := s.sub.List(ctx, ns, CorpusCaseSummaries, substrate.ListFilter{
		Tag:            summarySourcePrefix + sum.InvestigationRef,
		IncludeRetired: true,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve prior summary: %w", err)
	}
	if len(prior) > 0 {
		e, err := s.sub.Revise(ctx, ns, CorpusCaseSummaries, prior[0].ID, substrate.Revision{
			Title:      entry.Title,
			Body:       entry.Body,
			Tags:       entry.Tags,
			Meta:       entry.Meta,
			Provenance: entry.Provenance,
		})
		if err != nil {
			return uuid.Nil, fmt.Errorf("revise summary: %w", err)
		}
		return e.ID, nil
	}
	e, err := s.sub.Put(ctx, ns, CorpusCaseSummaries, entry)
	if err != nil {
		return uuid.Nil, fmt.Errorf("write summary: %w", err)
	}
	return e.ID, nil
}

// SimilarRequest is the recall_similar_investigations query (06 §4): a whole
// document (the current investigation's state/seed) matched by similarity
// against the case-summaries corpus.
type SimilarRequest struct {
	Query          string   `json:"query"`
	Tags           []string `json:"tags,omitempty"`
	Limit          int      `json:"limit"`
	IncludeRetired bool     `json:"include_retired"`
}

// SimilarMatch is one ranked similar-investigation result. InvestigationRef is
// the source investigation's grouping ref (recovered from the reserved source
// tag), so the agent can cite the prior case it learned from.
type SimilarMatch struct {
	InvestigationRef string   `json:"investigation_ref"`
	Title            string   `json:"title"`
	Excerpt          string   `json:"excerpt"`
	Score            float64  `json:"score"`
	Band             string   `json:"band"`
	MatchRationale   string   `json:"match_rationale"`
	Tags             []string `json:"tags,omitempty"`
	ContentHash      string   `json:"content_hash,omitempty"`
	Backend          string   `json:"backend,omitempty"`
}

// SimilarResult is the recall_similar_investigations response envelope.
type SimilarResult struct {
	Results     []SimilarMatch `json:"results"`
	Coverage    string         `json:"coverage"`
	RetrievalAt time.Time      `json:"retrieval_at"`
	// UnmatchedTags names the filter tags no summary carries, when they are
	// why the answer is empty (Coverage EMPTY_UNDER_FILTER).
	UnmatchedTags []string `json:"unmatched_tags,omitempty"`
}

// CoverageEmptyUnderFilter means the recall found nothing because a filter tag
// matches no summary — not evidence that nothing alike exists.
const CoverageEmptyUnderFilter = "EMPTY_UNDER_FILTER"

// RecallSimilar runs SIMILARITY recall over the case-summaries corpus (06 §4):
// "have we handled something like this before?" The query is the current
// investigation's state as a document; results carry substrate similarity bands
// (NEAR_DUPLICATE/RELATED/DISTINCT). Each match's InvestigationRef is recovered
// from the reserved source tag (recall hits carry no provenance), and reserved
// tags are stripped from the user-facing view.
func (s *Store) RecallSimilar(ctx context.Context, tenantID uuid.UUID, req SimilarRequest) (SimilarResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	res, err := s.sub.Recall(ctx, tenantID.String(), substrate.RecallQuery{
		Corpus:         CorpusCaseSummaries,
		Mode:           substrate.ModeSimilarity,
		Query:          req.Query,
		Tags:           req.Tags,
		Limit:          limit,
		IncludeRetired: req.IncludeRetired,
	})
	if err != nil {
		return SimilarResult{}, fmt.Errorf("recall_similar_investigations: %w", err)
	}
	out := SimilarResult{
		Coverage:    string(res.Coverage),
		RetrievalAt: res.RetrievalAt,
		Results:     make([]SimilarMatch, 0, len(res.Results)),
	}
	backend := rankerLabel(res.Ranker)
	if len(res.Results) == 0 && len(req.Tags) > 0 {
		// An empty answer under a filter is only an absence if every filter tag
		// exists; a tag no summary carries emptied it (adr/0163).
		unmatched, err := s.unmatchedTags(ctx, tenantID, req.Tags)
		if err != nil {
			return SimilarResult{}, fmt.Errorf("recall_similar_investigations: %w", err)
		}
		if len(unmatched) > 0 {
			out.Coverage = CoverageEmptyUnderFilter
			out.UnmatchedTags = unmatched
		}
	}
	for _, h := range res.Results {
		out.Results = append(out.Results, SimilarMatch{
			InvestigationRef: sourceRefOf(h.Tags),
			Title:            h.Title,
			Excerpt:          h.Excerpt,
			Score:            h.Score,
			Band:             string(h.Band),
			MatchRationale:   h.MatchRationale,
			Tags:             stripReserved(h.Tags),
			ContentHash:      string(h.ContentHash),
			Backend:          backend,
		})
	}
	return out, nil
}

// ListSummaries returns the concluded investigations' summaries, most recently
// concluded first — the library's past cases (adr/0129).
func (s *Store) ListSummaries(ctx context.Context, tenantID uuid.UUID, limit int) ([]CaseSummaryView, error) {
	entries, err := s.sub.List(ctx, tenantID.String(), CorpusCaseSummaries, substrate.ListFilter{})
	if err != nil {
		return nil, fmt.Errorf("list case summaries: %w", err)
	}
	out := make([]CaseSummaryView, 0, len(entries))
	for _, e := range entries {
		out = append(out, caseSummaryView(e))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ConclusionAt > out[j].ConclusionAt })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// PriorRequest asks which concluded investigations involved an entity.
type PriorRequest struct {
	Entity string `json:"entity"`
	Limit  int    `json:"limit,omitempty"`
}

// PriorResult answers PriorRequest exactly: every concluded investigation whose
// summary records the entity, most recently concluded first. Coverage is
// COMPLETE with results, EMPTY with none — over the summaries that record
// entities (those written since adr/0163, plus the seeded prior cases).
type PriorResult struct {
	Entity         string            `json:"entity"`
	Coverage       string            `json:"coverage"`
	Investigations []CaseSummaryView `json:"investigations"`
}

// PriorByEntity is the exact lookup "have we investigated this before?"
// (adr/0163): a tag filter over the case-summaries corpus, usable from the
// seed alone, before anything has been observed. Similarity recall stays the
// question "have we seen this pattern before?".
func (s *Store) PriorByEntity(ctx context.Context, tenantID uuid.UUID, req PriorRequest) (PriorResult, error) {
	tag := EntityTag(req.Entity)
	if tag == "" {
		return PriorResult{}, fmt.Errorf("prior_investigations: entity required")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	entries, err := s.sub.List(ctx, tenantID.String(), CorpusCaseSummaries, substrate.ListFilter{Tag: tag})
	if err != nil {
		return PriorResult{}, fmt.Errorf("prior_investigations: %w", err)
	}
	out := PriorResult{Entity: req.Entity, Coverage: "EMPTY", Investigations: []CaseSummaryView{}}
	for _, e := range entries {
		out.Investigations = append(out.Investigations, caseSummaryView(e))
	}
	sort.SliceStable(out.Investigations, func(i, j int) bool {
		return out.Investigations[i].ConclusionAt > out.Investigations[j].ConclusionAt
	})
	if len(out.Investigations) > limit {
		out.Investigations = out.Investigations[:limit]
	}
	if len(out.Investigations) > 0 {
		out.Coverage = "COMPLETE"
	}
	return out, nil
}

// unmatchedTags returns the filter tags no summary carries. A similarity
// recall whose filter names such a tag came back empty because of the
// filter, not because the corpus has nothing alike.
func (s *Store) unmatchedTags(ctx context.Context, tenantID uuid.UUID, tags []string) ([]string, error) {
	var out []string
	for _, t := range tags {
		hits, err := s.sub.List(ctx, tenantID.String(), CorpusCaseSummaries, substrate.ListFilter{Tag: t, Limit: 1})
		if err != nil {
			return nil, err
		}
		if len(hits) == 0 {
			out = append(out, t)
		}
	}
	return out, nil
}

// sourceRefOf recovers a summary's source investigation ref from its reserved
// source tag.
func sourceRefOf(tags []string) string {
	for _, t := range tags {
		if rest, ok := strings.CutPrefix(t, summarySourcePrefix); ok {
			return rest
		}
	}
	return ""
}

// ErrNoSummary is returned by EnrichSummary when no summary exists yet for the
// investigation — the client retries briefly (the post-conclusion pipeline may
// still be writing the structured baseline) and then gives up gracefully.
var ErrNoSummary = errors.New("no summary exists for this investigation yet")

// maxNarrativeRunes bounds an enrichment narrative (design/06 §3.1 bounds
// summary_text by design; the narrative is a recallable digest, not a report).
const maxNarrativeRunes = 4000

// EnrichSummary replaces a summary's recallable body with the client-authored
// LLM narrative (design/06 §3.2, two-tier): the server wrote the structured
// baseline deterministically at conclusion; the analyst's own client — the
// only place the BYOK model key lives — enriches it with prose written from
// the full conversation context. The structured fields (tags, meta, title)
// carry over unchanged; only the body and the generator-model stamp move. A
// second enrichment (re-conclude, retry) revises again — idempotent in effect.
func (s *Store) EnrichSummary(ctx context.Context, tenantID uuid.UUID, investigationRef, narrative, generatorModel string) (uuid.UUID, error) {
	narrative = strings.TrimSpace(narrative)
	if investigationRef == "" || narrative == "" {
		return uuid.Nil, fmt.Errorf("EnrichSummary: investigation_ref and narrative required")
	}
	if r := []rune(narrative); len(r) > maxNarrativeRunes {
		narrative = string(r[:maxNarrativeRunes])
	}
	ns := tenantID.String()
	prior, err := s.sub.List(ctx, ns, CorpusCaseSummaries, substrate.ListFilter{
		Tag:            summarySourcePrefix + investigationRef,
		IncludeRetired: true,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve summary: %w", err)
	}
	if len(prior) == 0 {
		return uuid.Nil, fmt.Errorf("%w: %s", ErrNoSummary, investigationRef)
	}
	p := prior[0]
	prov := p.Provenance
	if prov == nil {
		prov = &substrate.Provenance{Producer: "case-summarizer"}
	}
	prov.GeneratorModel = generatorModel
	e, err := s.sub.Revise(ctx, ns, CorpusCaseSummaries, p.ID, substrate.Revision{
		Title:      p.Title,
		Body:       narrative,
		Tags:       p.Tags,
		Meta:       p.Meta,
		Provenance: prov,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("enrich summary: %w", err)
	}
	return e.ID, nil
}

// summaryTags are the hard-filterable facets: techniques, the seed kind, the
// disposition, and the reserved source tag for idempotent re-derivation.
func summaryTags(sum Summary) []string {
	tags := make([]string, 0, len(sum.Techniques)+3)
	tags = append(tags, summarySourcePrefix+sum.InvestigationRef)
	if sum.SeedKind != "" {
		tags = append(tags, "seed:"+sum.SeedKind)
	}
	if sum.VerdictDisposition != "" {
		tags = append(tags, "disposition:"+sum.VerdictDisposition)
	}
	tags = append(tags, sum.Techniques...)
	for _, e := range sum.PrimaryEntities {
		if t := EntityTag(e); t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

// entityTagPrefix marks the entities a summary involved, one tag each, so an
// exact lookup by entity is a tag filter rather than a similarity guess
// (adr/0163).
const entityTagPrefix = "entity:"

// EntityTag is the tag a summary carries for one entity: the identifier
// trimmed and lowercased (host names, accounts and addresses compare without
// case), or "" for an empty identifier.
func EntityTag(entity string) string {
	e := strings.ToLower(strings.TrimSpace(entity))
	if e == "" {
		return ""
	}
	return entityTagPrefix + e
}

// summaryMeta carries the structured fields as opaque metadata (design/06
// §3.1 structured) for downstream analytics and CEL projection (06 §12).
func summaryMeta(sum Summary) map[string]any {
	actions := make([]map[string]any, 0, len(sum.ActionsTaken))
	for _, a := range sum.ActionsTaken {
		actions = append(actions, map[string]any{
			"action_type":  a.ActionType,
			"target_count": a.TargetCount,
			"outcome":      a.Outcome,
		})
	}
	return map[string]any{
		"investigation_ref":  sum.InvestigationRef,
		"conclusion_at":      sum.ConclusionAt.UTC().Format(time.RFC3339),
		"seed_kind":          sum.SeedKind,
		"seed_value":         sum.SeedValue,
		"techniques":         sum.Techniques,
		"primary_entities":   sum.PrimaryEntities,
		"actions_taken":      actions,
		"conclusion_outcome": sum.ConclusionOutcome,
		"duration_hours":     sum.DurationHours,
		"verdict":            map[string]any{"disposition": sum.VerdictDisposition, "rationale": sum.VerdictRationale},
	}
}

// CaseSummaryView is one concluded investigation's summary read back by its
// source investigation (design/ui 08 §8.6.1 "Similar past case"): the
// structured fields the producer recorded plus the recallable body — the LLM
// narrative once the client tier wrote one, the deterministic baseline until
// then (06 §3.2). Reserved tags are stripped.
type CaseSummaryView struct {
	InvestigationRef   string          `json:"investigation_ref"`
	Title              string          `json:"title"`
	Body               string          `json:"body"`
	GeneratorModel     string          `json:"generator_model,omitempty"`
	ConclusionAt       string          `json:"conclusion_at,omitempty"`
	SeedKind           string          `json:"seed_kind,omitempty"`
	SeedValue          string          `json:"seed_value,omitempty"`
	Techniques         []string        `json:"techniques"`
	PrimaryEntities    []string        `json:"primary_entities"`
	ActionsTaken       []SummaryAction `json:"actions_taken"`
	ConclusionOutcome  string          `json:"conclusion_outcome,omitempty"`
	DurationHours      float64         `json:"duration_hours,omitempty"`
	VerdictDisposition string          `json:"verdict_disposition,omitempty"`
	VerdictRationale   string          `json:"verdict_rationale,omitempty"`
	Tags               []string        `json:"tags"`
	Revision           int             `json:"revision"`
	ContentHash        string          `json:"content_hash,omitempty"`
}

// SummaryByInvestigation reads the current summary of one concluded
// investigation (ErrNoSummary when none exists). Recall hits carry no
// provenance or meta, so this is the read the inspector uses once it knows the
// source ref.
func (s *Store) SummaryByInvestigation(ctx context.Context, tenantID uuid.UUID, investigationRef string) (CaseSummaryView, error) {
	if investigationRef == "" {
		return CaseSummaryView{}, fmt.Errorf("SummaryByInvestigation: investigation_ref required")
	}
	entries, err := s.sub.List(ctx, tenantID.String(), CorpusCaseSummaries, substrate.ListFilter{
		Tag: summarySourcePrefix + investigationRef,
	})
	if err != nil {
		return CaseSummaryView{}, fmt.Errorf("resolve summary: %w", err)
	}
	if len(entries) == 0 {
		return CaseSummaryView{}, fmt.Errorf("%w: %s", ErrNoSummary, investigationRef)
	}
	return caseSummaryView(entries[0]), nil
}

func caseSummaryView(e substrate.Entry) CaseSummaryView {
	v := CaseSummaryView{
		InvestigationRef: sourceRefOf(e.Tags),
		Title:            e.Title,
		Body:             e.Body,
		Techniques:       []string{},
		PrimaryEntities:  []string{},
		ActionsTaken:     []SummaryAction{},
		Tags:             stripReserved(e.Tags),
		Revision:         e.Revision,
		ContentHash:      string(e.ContentHash),
	}
	if e.Provenance != nil {
		v.GeneratorModel = e.Provenance.GeneratorModel
	}
	m := e.Meta
	str := func(k string) string {
		if s, ok := m[k].(string); ok {
			return s
		}
		return ""
	}
	strs := func(k string) []string {
		out := []string{}
		switch raw := m[k].(type) {
		case []string:
			out = append(out, raw...)
		case []any:
			for _, x := range raw {
				if s, ok := x.(string); ok {
					out = append(out, s)
				}
			}
		}
		return out
	}
	v.ConclusionAt = str("conclusion_at")
	v.SeedKind, v.SeedValue = str("seed_kind"), str("seed_value")
	v.Techniques = strs("techniques")
	v.PrimaryEntities = strs("primary_entities")
	v.ConclusionOutcome = str("conclusion_outcome")
	if d, ok := m["duration_hours"].(float64); ok {
		v.DurationHours = d
	}
	if verdict, ok := m["verdict"].(map[string]any); ok {
		if s, ok := verdict["disposition"].(string); ok {
			v.VerdictDisposition = s
		}
		if s, ok := verdict["rationale"].(string); ok {
			v.VerdictRationale = s
		}
	}
	if acts, ok := m["actions_taken"].([]any); ok {
		for _, a := range acts {
			am, ok := a.(map[string]any)
			if !ok {
				continue
			}
			sa := SummaryAction{}
			if s, ok := am["action_type"].(string); ok {
				sa.ActionType = s
			}
			if s, ok := am["outcome"].(string); ok {
				sa.Outcome = s
			}
			if n, ok := am["target_count"].(float64); ok {
				sa.TargetCount = int(n)
			}
			v.ActionsTaken = append(v.ActionsTaken, sa)
		}
	}
	return v
}
