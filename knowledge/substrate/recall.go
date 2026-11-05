package substrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Mode selects between the two recall shapes (§5): RANK for a short free-text
// query, SIMILARITY for a whole document ("here is a 3-page draft; what do we
// already have like it?").
type Mode string

const (
	ModeRank       Mode = "RANK"
	ModeSimilarity Mode = "SIMILARITY"
)

// Unit is what one RANK hit stands for (§5.1). ENTRY answers "which document
// applies": the ranking is over sections, aggregated to the entry by its best
// section, one hit per entry carrying that section. SECTION answers "what do
// we hold on this": one hit per section, so a strong entry may appear more
// than once. SIMILARITY mode compares whole documents and ignores the unit.
type Unit string

const (
	UnitEntry   Unit = "ENTRY"
	UnitSection Unit = "SECTION"
)

// Band classifies a SIMILARITY result (§5.4) — the contract; the numeric
// cutoffs behind it are calibrated per {backend, backend_version} and shift
// when the backend does. Consumers persist bands with the ranker attribution,
// never raw scores as thresholds.
type Band string

const (
	// BandNearDuplicate: substantially the same thing; a consolidation candidate.
	BandNearDuplicate Band = "NEAR_DUPLICATE"
	// BandRelated: same problem space; worth reading before proceeding.
	BandRelated Band = "RELATED"
	// BandDistinct: returned only to show the nearest neighbors are far away.
	BandDistinct Band = "DISTINCT"
)

// Coverage is the §5.5 verdict. Failures (backend down, corpus unknown,
// malformed query) are errors, never EMPTY — consumers are entitled to treat
// EMPTY as evidence of absence.
type Coverage string

const (
	CoverageComplete Coverage = "COMPLETE"
	CoverageEmpty    Coverage = "EMPTY"
)

// RecallQuery is one recall call (§5.1): one corpus, one mode.
type RecallQuery struct {
	Corpus         string
	Mode           Mode   // default RANK
	Unit           Unit   // RANK only; default ENTRY
	Query          string // RANK: short free text. SIMILARITY: a whole document.
	Tags           []string
	Limit          int // default 5
	IncludeRetired bool
	// ExcerptBudget bounds a hit's excerpt in runes (§5.6). Zero means the
	// store's section budget, so a section arrives whole; a smaller value cuts
	// it and the hit says so.
	ExcerptBudget int
}

// SectionRef locates the section a hit carries within its entry (§5.2).
type SectionRef struct {
	Idx         int      // 1-based position in the body
	Of          int      // how many sections the body has
	HeadingPath []string // outermost heading first; empty before the first heading
	Part, Parts int      // set when the section was cut into parts at write time
	Location    string   // the heading path rendered, with the part when cut
}

// RecallHit is one ranked result (§5.2). Score is opaque and orders this
// response only. Excerpt is the text of Section — the piece that matched in
// RANK mode, the first section in SIMILARITY mode — up to the query's budget;
// Outline is one location line per section of the entry, so a reader knows
// what else the document holds.
type RecallHit struct {
	EntryID        uuid.UUID
	Revision       int
	Title          string
	Excerpt        string
	Truncated      bool // the excerpt was cut by the budget
	Section        SectionRef
	Outline        []string
	Score          float64
	Band           Band // SIMILARITY mode only
	MatchRationale string
	ContentHash    ContentHash
	HashVersion    int
	Tags           []string
	Advice         string
	Status         Status
}

// Ranker attributes a result set to the backend that produced it, for audit
// trails and band interpretation.
type Ranker struct {
	Backend        string
	BackendVersion string
}

// RecallResult is the §5.2 envelope.
type RecallResult struct {
	Results     []RecallHit
	Coverage    Coverage
	Ranker      Ranker
	RetrievalAt time.Time
}

// BandCutoffs are the cosine floors for the vector backend's bands. Part of
// the backend version's calibration (§5.4) — deployment-tunable via WithBands.
type BandCutoffs struct {
	NearDuplicate float64
	Related       float64
}

// DefaultBands is the vector backend's v1 calibration point: conservative
// floors for the OpenAI-compatible embedding families. Honest coarseness
// beats false precision — tune per deployment if a model's cosine
// distribution warrants it.
var DefaultBands = BandCutoffs{NearDuplicate: 0.92, Related: 0.75}

const (
	defaultRecallLimit = 5
	// keywordSimilarityTerms bounds the tsquery a SIMILARITY document is
	// reduced to on the keyword backend — plainto_tsquery ANDs terms, so a
	// whole document would match nothing. Crude by design (§10 v0).
	keywordSimilarityTerms = 12
)

// Recall executes one query over one corpus (§5). With an embedder the
// vector backend ranks by cosine similarity under the active model; without
// one it degrades to the keyword backend and says so in the attribution.
func (p *Postgres) Recall(ctx context.Context, ns string, q RecallQuery) (RecallResult, error) {
	if err := validNamespace(ns); err != nil {
		return RecallResult{}, err
	}
	if _, err := p.corpus(q.Corpus); err != nil {
		return RecallResult{}, err
	}
	if strings.TrimSpace(q.Query) == "" {
		return RecallResult{}, fmt.Errorf("substrate: recall query must not be empty")
	}
	switch q.Mode {
	case "":
		q.Mode = ModeRank
	case ModeRank, ModeSimilarity:
	default:
		return RecallResult{}, fmt.Errorf("substrate: unknown recall mode %q", q.Mode)
	}
	switch q.Unit {
	case "":
		q.Unit = UnitEntry
	case UnitEntry, UnitSection:
	default:
		return RecallResult{}, fmt.Errorf("substrate: unknown recall unit %q", q.Unit)
	}
	if q.Limit <= 0 {
		q.Limit = defaultRecallLimit
	}
	if q.ExcerptBudget <= 0 || q.ExcerptBudget > p.sectionBudget {
		q.ExcerptBudget = p.sectionBudget
	}
	if p.embedder != nil {
		return p.recallVector(ctx, ns, q)
	}
	return p.recallKeyword(ctx, ns, q)
}

// recallVector is the §10 v1 backend: exact cosine over the tag-filtered
// candidate set, in process. RANK mode ranks sections (one vector each) and
// aggregates per the unit; SIMILARITY mode compares whole documents (section
// 0). Corpora are hundreds-to-thousands of entries by design; brute force at
// that scale is microseconds, and an approximate index slots in behind this
// same signature if a corpus ever outgrows it.
func (p *Postgres) recallVector(ctx context.Context, ns string, q RecallQuery) (RecallResult, error) {
	qv, err := p.embedder.Embed(ctx, []string{q.Query})
	if err != nil {
		return RecallResult{}, fmt.Errorf("substrate: embed recall query: %w", err)
	}
	cands, err := p.candidates(ctx, ns, q)
	if err != nil {
		return RecallResult{}, err
	}
	ranker := Ranker{Backend: "vector-cosine", BackendVersion: "1/" + p.embedder.Model()}
	if len(cands) == 0 {
		return RecallResult{Coverage: CoverageEmpty, Ranker: ranker, RetrievalAt: p.now().UTC()}, nil
	}
	ids := make([]uuid.UUID, len(cands))
	for i, e := range cands {
		ids[i] = e.ID
	}
	sections, err := loadSectionsFor(ctx, p.db, ids)
	if err != nil {
		return RecallResult{}, err
	}
	vecs, err := p.vectorsFor(ctx, ids)
	if err != nil {
		return RecallResult{}, err
	}
	// A published candidate the backend cannot rank is an error, never a
	// silent gap (§5.5) — Reindex is the remedy, after a model switch or a
	// section-rule change.
	for _, e := range cands {
		if len(sections[e.ID]) == 0 {
			return RecallResult{}, fmt.Errorf("%w: entry %s", ErrNoSections, e.ID)
		}
		if _, ok := vecs[e.ID][0]; !ok {
			return RecallResult{}, fmt.Errorf("%w: entry %s under model %s", ErrNoEmbedding, e.ID, p.embedder.Model())
		}
		if q.Mode == ModeRank {
			for _, s := range sections[e.ID] {
				if _, ok := vecs[e.ID][s.Idx]; !ok {
					return RecallResult{}, fmt.Errorf("%w: entry %s section %d under model %s", ErrNoEmbedding, e.ID, s.Idx, p.embedder.Model())
				}
			}
		}
	}

	type scored struct {
		e   Entry
		sec Section
		sim float64
	}
	var ranked []scored
	switch {
	case q.Mode == ModeSimilarity:
		for _, e := range cands {
			ranked = append(ranked, scored{e: e, sec: sections[e.ID][0], sim: cosine(qv[0], vecs[e.ID][0])})
		}
	case q.Unit == UnitEntry:
		for _, e := range cands {
			best := scored{e: e, sim: -2}
			for _, s := range sections[e.ID] {
				if sim := cosine(qv[0], vecs[e.ID][s.Idx]); sim > best.sim {
					best.sec, best.sim = s, sim
				}
			}
			ranked = append(ranked, best)
		}
	default: // UnitSection
		for _, e := range cands {
			for _, s := range sections[e.ID] {
				ranked = append(ranked, scored{e: e, sec: s, sim: cosine(qv[0], vecs[e.ID][s.Idx])})
			}
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].sim != ranked[j].sim {
			return ranked[i].sim > ranked[j].sim
		}
		if !ranked[i].e.Created.Equal(ranked[j].e.Created) {
			return ranked[i].e.Created.After(ranked[j].e.Created) // recency tiebreak
		}
		return ranked[i].sec.Idx < ranked[j].sec.Idx // document order within one entry
	})
	if len(ranked) > q.Limit {
		ranked = ranked[:q.Limit]
	}
	hits := make([]RecallHit, 0, len(ranked))
	for _, s := range ranked {
		h := hitFrom(s.e, s.sec, sections[s.e.ID], s.sim, q.ExcerptBudget)
		matched := intersect(q.Tags, s.e.Tags)
		switch {
		case q.Mode == ModeSimilarity:
			h.Band = p.band(s.sim)
			h.MatchRationale = rationaleText(fmt.Sprintf("cosine %.2f under %s — %s band", s.sim, p.embedder.Model(), h.Band), matched)
		case q.Unit == UnitEntry:
			h.MatchRationale = rationaleText(fmt.Sprintf("cosine %.2f to the query under %s; best section %s", s.sim, p.embedder.Model(), h.Section.describe()), matched)
		default:
			h.MatchRationale = rationaleText(fmt.Sprintf("cosine %.2f to the query under %s; section %s", s.sim, p.embedder.Model(), h.Section.describe()), matched)
		}
		hits = append(hits, h)
	}
	return RecallResult{Results: hits, Coverage: CoverageComplete, Ranker: ranker, RetrievalAt: p.now().UTC()}, nil
}

// recallKeyword is the degraded §10 v0 backend: Postgres full-text. RANK mode
// ranks sections by ts_rank over the section text and aggregates per the
// unit; SIMILARITY mode reduces the document to its leading terms, ranks
// entries, and bands deliberately coarsely — RELATED at best, never
// NEAR_DUPLICATE (§13: honest coarseness beats false precision).
func (p *Postgres) recallKeyword(ctx context.Context, ns string, q RecallQuery) (RecallResult, error) {
	ranker := Ranker{Backend: "pg-fts", BackendVersion: "1"}
	type scored struct {
		e     Entry
		idx   int // 0 in SIMILARITY mode: the first section stands for the document
		score float64
	}
	var ranked []scored
	if q.Mode == ModeSimilarity {
		words := strings.Fields(q.Query)
		if len(words) > keywordSimilarityTerms {
			words = words[:keywordSimilarityTerms]
		}
		text := strings.Join(words, " ")
		query := `SELECT ` + entryCols + `, ts_rank(fts, tsq) AS score
			 FROM substrate_entries, plainto_tsquery('english', $3) tsq
			 WHERE namespace = $1 AND corpus = $2 AND superseded_at IS NULL AND fts @@ tsq`
		args := []any{ns, q.Corpus, text}
		query += statusClause(q.IncludeRetired)
		if len(q.Tags) > 0 {
			args = append(args, pq.Array(q.Tags))
			query += fmt.Sprintf(" AND tags && $%d", len(args))
		}
		args = append(args, q.Limit)
		query += fmt.Sprintf(" ORDER BY score DESC, created DESC LIMIT $%d", len(args)) //nolint:gosec // G202: appends a positional placeholder; every value is parameterized
		rs, err := p.db.QueryContext(ctx, query, args...)
		if err != nil {
			return RecallResult{}, fmt.Errorf("substrate: keyword recall: %w", err)
		}
		defer rs.Close() //nolint:errcheck // read-side close
		for rs.Next() {
			var score float64
			e, err := scanEntry(rs, &score)
			if err != nil {
				return RecallResult{}, err
			}
			ranked = append(ranked, scored{e: e, score: score})
		}
		if err := rs.Err(); err != nil {
			return RecallResult{}, err
		}
	} else {
		// The ranking starts from the sections, so an eligible entry without a
		// cut would vanish and EMPTY would be claimed where recall is simply
		// not possible (§5.5); it is an error here as on the vector backend.
		if id, missing, err := p.firstUnsectioned(ctx, ns, q); err != nil {
			return RecallResult{}, err
		} else if missing {
			return RecallResult{}, fmt.Errorf("%w: entry %s", ErrNoSections, id)
		}
		// Sections carry the rank; the entry columns ride along so one query
		// answers both units. ENTRY keeps each entry's best section
		// (DISTINCT ON, ordered by rank within the entry) and orders the
		// survivors by rank; SECTION orders every matching section.
		eCols := "e." + strings.ReplaceAll(entryCols, ", ", ", e.")
		// The keyword surface of a section is its text (indexed), its heading
		// path and the entry's title (joined here).
		const surface = `(s.fts || to_tsvector('english', array_to_string(s.heading_path, ' ') || ' ' || e.title))`
		inner := `SELECT ` + eCols + `, s.idx AS sidx, ts_rank(` + surface + `, tsq) AS score
			 FROM substrate_sections s
			 JOIN substrate_entries e ON e.id = s.entry_id,
			      plainto_tsquery('english', $3) tsq
			 WHERE e.namespace = $1 AND e.corpus = $2 AND e.superseded_at IS NULL
			   AND ` + surface + ` @@ tsq`
		args := []any{ns, q.Corpus, q.Query}
		inner += strings.ReplaceAll(statusClause(q.IncludeRetired), " status", " e.status")
		if len(q.Tags) > 0 {
			args = append(args, pq.Array(q.Tags))
			inner += fmt.Sprintf(" AND e.tags && $%d", len(args))
		}
		var query string
		if q.Unit == UnitEntry {
			query = `SELECT * FROM (SELECT DISTINCT ON (id) * FROM (` + inner + `) m ORDER BY id, score DESC, sidx) best`
		} else {
			query = `SELECT * FROM (` + inner + `) m`
		}
		args = append(args, q.Limit)
		query += fmt.Sprintf(" ORDER BY score DESC, created DESC, sidx LIMIT $%d", len(args)) //nolint:gosec // G202: appends a positional placeholder; every value is parameterized
		rs, err := p.db.QueryContext(ctx, query, args...)
		if err != nil {
			return RecallResult{}, fmt.Errorf("substrate: keyword recall: %w", err)
		}
		defer rs.Close() //nolint:errcheck // read-side close
		for rs.Next() {
			var (
				idx   int
				score float64
			)
			e, err := scanEntry(rs, &idx, &score)
			if err != nil {
				return RecallResult{}, err
			}
			ranked = append(ranked, scored{e: e, idx: idx, score: score})
		}
		if err := rs.Err(); err != nil {
			return RecallResult{}, err
		}
	}
	if len(ranked) == 0 {
		return RecallResult{Coverage: CoverageEmpty, Ranker: ranker, RetrievalAt: p.now().UTC()}, nil
	}
	ids := make([]uuid.UUID, 0, len(ranked))
	seen := map[uuid.UUID]bool{}
	for _, s := range ranked {
		if !seen[s.e.ID] {
			seen[s.e.ID] = true
			ids = append(ids, s.e.ID)
		}
	}
	sections, err := loadSectionsFor(ctx, p.db, ids)
	if err != nil {
		return RecallResult{}, err
	}
	hits := make([]RecallHit, 0, len(ranked))
	for _, s := range ranked {
		secs := sections[s.e.ID]
		if len(secs) == 0 {
			return RecallResult{}, fmt.Errorf("%w: entry %s", ErrNoSections, s.e.ID)
		}
		sec := secs[0]
		for _, c := range secs {
			if c.Idx == s.idx {
				sec = c
			}
		}
		h := hitFrom(s.e, sec, secs, s.score, q.ExcerptBudget)
		matched := intersect(q.Tags, s.e.Tags)
		switch {
		case q.Mode == ModeSimilarity:
			h.Band = BandRelated // the keyword backend never claims NEAR_DUPLICATE
			h.MatchRationale = rationaleText(fmt.Sprintf("keyword overlap (ts_rank %.3f) with the document's leading terms — RELATED band (coarse keyword backend)", s.score), matched)
		case q.Unit == UnitEntry:
			h.MatchRationale = rationaleText(fmt.Sprintf("keyword match (ts_rank %.3f); best section %s", s.score, h.Section.describe()), matched)
		default:
			h.MatchRationale = rationaleText(fmt.Sprintf("keyword match (ts_rank %.3f); section %s", s.score, h.Section.describe()), matched)
		}
		hits = append(hits, h)
	}
	return RecallResult{Results: hits, Coverage: CoverageComplete, Ranker: ranker, RetrievalAt: p.now().UTC()}, nil
}

// candidates loads the recallable entries: current revisions, published (plus
// retired only on request), tag hard-filtered (overlap) before ranking.
func (p *Postgres) candidates(ctx context.Context, ns string, q RecallQuery) ([]Entry, error) {
	query := `SELECT ` + entryCols + ` FROM substrate_entries
		 WHERE namespace = $1 AND corpus = $2 AND superseded_at IS NULL`
	args := []any{ns, q.Corpus}
	query += statusClause(q.IncludeRetired)
	if len(q.Tags) > 0 {
		args = append(args, pq.Array(q.Tags))
		query += fmt.Sprintf(" AND tags && $%d", len(args)) //nolint:gosec // G202: appends a positional placeholder; every value is parameterized
	}
	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("substrate: recall candidates: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-side close
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// firstUnsectioned finds one eligible candidate (same filters as candidates)
// that has no section rows at all.
func (p *Postgres) firstUnsectioned(ctx context.Context, ns string, q RecallQuery) (uuid.UUID, bool, error) {
	query := `SELECT e.id FROM substrate_entries e
		 WHERE e.namespace = $1 AND e.corpus = $2 AND e.superseded_at IS NULL
		   AND NOT EXISTS (SELECT 1 FROM substrate_sections s WHERE s.entry_id = e.id)`
	args := []any{ns, q.Corpus}
	query += strings.ReplaceAll(statusClause(q.IncludeRetired), " status", " e.status")
	if len(q.Tags) > 0 {
		args = append(args, pq.Array(q.Tags))
		query += fmt.Sprintf(" AND e.tags && $%d", len(args)) //nolint:gosec // G202: appends a positional placeholder; every value is parameterized
	}
	query += ` LIMIT 1`
	var id uuid.UUID
	err := p.db.QueryRowContext(ctx, query, args...).Scan(&id)
	switch {
	case err == nil:
		return id, true, nil
	case errors.Is(err, sql.ErrNoRows):
		return uuid.Nil, false, nil
	default:
		return uuid.Nil, false, fmt.Errorf("substrate: recall sections check: %w", err)
	}
}

func statusClause(includeRetired bool) string {
	if includeRetired {
		return ` AND status IN ('PUBLISHED','RETIRED')`
	}
	return ` AND status = 'PUBLISHED'`
}

// vectorsFor fetches the active model's vectors for the candidate set, keyed
// by entry then section (0 = the whole document).
func (p *Postgres) vectorsFor(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]map[int][]float32, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT entry_id, section, vec FROM substrate_embeddings WHERE model = $1 AND entry_id = ANY($2)`,
		p.embedder.Model(), pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("substrate: load vectors: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-side close
	out := make(map[uuid.UUID]map[int][]float32, len(ids))
	for rows.Next() {
		var (
			id      uuid.UUID
			section int
			vec     pq.Float32Array
		)
		if err := rows.Scan(&id, &section, &vec); err != nil {
			return nil, fmt.Errorf("substrate: scan vector: %w", err)
		}
		if out[id] == nil {
			out[id] = map[int][]float32{}
		}
		out[id][section] = vec
	}
	return out, rows.Err()
}

// loadSectionsFor loads the sections of several entries, in body order,
// keyed by entry.
func loadSectionsFor(ctx context.Context, q rowQuerier, ids []uuid.UUID) (map[uuid.UUID][]Section, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT entry_id, `+sectionCols+` FROM substrate_sections WHERE entry_id = ANY($1) ORDER BY entry_id, idx`,
		pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("substrate: load sections: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-side close
	out := make(map[uuid.UUID][]Section, len(ids))
	for rows.Next() {
		var (
			id uuid.UUID
			s  Section
		)
		if err := rows.Scan(&id, &s.Idx, pq.Array(&s.HeadingPath), &s.Part, &s.Parts, &s.Start, &s.End, &s.Text); err != nil {
			return nil, fmt.Errorf("substrate: scan section: %w", err)
		}
		out[id] = append(out[id], s)
	}
	return out, rows.Err()
}

func (p *Postgres) band(sim float64) Band {
	switch {
	case sim >= p.bands.NearDuplicate:
		return BandNearDuplicate
	case sim >= p.bands.Related:
		return BandRelated
	default:
		return BandDistinct
	}
}

// hitFrom builds the hit for one entry around one of its sections: the
// excerpt is that section's text within the budget (§5.6), the outline is
// every section's location.
func hitFrom(e Entry, sec Section, all []Section, score float64, budget int) RecallHit {
	excerpt, truncated := excerptOf(sec.Text, budget)
	outline := make([]string, len(all))
	for i, s := range all {
		outline[i] = s.Location()
	}
	return RecallHit{
		EntryID:   e.ID,
		Revision:  e.Revision,
		Title:     e.Title,
		Excerpt:   excerpt,
		Truncated: truncated,
		Section: SectionRef{
			Idx:         sec.Idx,
			Of:          len(all),
			HeadingPath: sec.HeadingPath,
			Part:        sec.Part,
			Parts:       sec.Parts,
			Location:    sec.Location(),
		},
		Outline:     outline,
		Score:       score,
		ContentHash: e.ContentHash,
		HashVersion: e.HashVersion,
		Tags:        e.Tags,
		Advice:      e.Advice,
		Status:      e.Status,
	}
}

// describe renders the section for rationale text: its location and place.
func (r SectionRef) describe() string {
	return fmt.Sprintf("%q (%d of %d)", r.Location, r.Idx, r.Of)
}

// excerptOf is a rune-budgeted view over a section's text (§5.6): the
// attestation claim attaches to the content the hash covers, not the excerpt.
func excerptOf(text string, budget int) (string, bool) {
	runes := []rune(text)
	if budget <= 0 || len(runes) <= budget {
		return text, false
	}
	return string(runes[:budget]) + "…", true
}

func rationaleText(core string, matchedTags []string) string {
	if len(matchedTags) == 0 {
		return core
	}
	return core + fmt.Sprintf("; tags [%s] matched (hard filter)", strings.Join(matchedTags, ", "))
}

func intersect(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	inB := make(map[string]bool, len(b))
	for _, s := range b {
		inB[s] = true
	}
	var out []string
	for _, s := range a {
		if inB[s] {
			out = append(out, s)
		}
	}
	return out
}

// cosine is exact cosine similarity; zero-norm inputs score 0.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
