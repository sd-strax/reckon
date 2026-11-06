// Package knowledge is the institutional-memory service (design/06) — the
// consumer-side half of the split with the memory substrate
// (knowledge/design/00-substrate.md): the substrate owns storage, recall,
// attestation, and similarity as a host-free mechanism; this package maps
// reckon's SOP domain onto it and translates results back. It reaches the
// substrate only through the substrate.Store interface, so the mechanism stays
// unaware of SOPs, tenants, or STIX.
//
// SOPs live in the substrate's `procedures` CURATED corpus. A SOP's external
// id is a stable lineage id carried in a reserved tag, so an edit becomes a
// substrate revision (prior content stays hash-addressable) while the id the
// server and audit trail reference never moves.
package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/internal/sopdoc"
	"github.com/sd-strax/reckon/knowledge/substrate"
)

// CorpusProcedures is the substrate corpus SOPs live in (CURATED, lightweight
// governance for v0 — published on write; gated authoring is the paid module).
const CorpusProcedures = "procedures"

// CorpusCaseSummaries is the DERIVED corpus of concluded-investigation
// summaries (design/06 §3) — populated by SummarizeForKnowledgeIndex,
// declared here so both corpora are registered at construction.
const CorpusCaseSummaries = "case-summaries"

// lineageTagPrefix marks the reserved tag carrying a SOP's stable external id.
// It rides in the entry's tags (so it survives revision and is returned by
// recall, which does not carry meta), and is stripped from every user-facing
// tag list.
const lineageTagPrefix = "__sop_lineage:"

// SOP statuses (design/06 §2.1). Stored lowercase in the SOP shape; mapped to
// the substrate's uppercase entry status. Lightweight governance goes straight
// to published; gated governance (paid) walks draft → in_review → published.
const (
	StatusDraft     = "draft"
	StatusInReview  = "in_review"
	StatusPublished = "published"
	StatusRetired   = "retired"
)

// SOPSource points at the system of origin for imported institutional
// knowledge (a Confluence page, a wiki doc, a file). URL is the lineage key
// for re-imports: importing the same URL again revises the SOP in place, the
// prior version staying hash-addressable.
type SOPSource struct {
	System  string `json:"system,omitempty"`  // e.g. confluence, sharepoint, file
	URL     string `json:"url"`               // the stable pointer; the re-import lineage key
	Version string `json:"version,omitempty"` // the origin system's own version label
}

// SOP is one runbook entry — procedural guidance the LLM consults during
// reasoning (design/06 §2). The body is unparsed prose; the system never
// executes it. Identity is a tenant-scoped random UUID (SOPs are documents, not
// observable facts — deterministic identity doesn't apply, §2.1).
type SOP struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	Tags           []string   `json:"tags"`
	Status         string     `json:"status"`
	Recommendation string     `json:"recommendation,omitempty"`
	AuthorID       string     `json:"author_id,omitempty"`
	SignerID       string     `json:"signer_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	RetiredAt      *time.Time `json:"retired_at,omitempty"`

	// Import attribution (design/06 §2.4): three distinct facts, never
	// collapsed — the ORIGINAL author (AuthorID above, from the document),
	// the source pointer (where it came from), and the importer + time
	// (provenance of the ingest, not the content).
	Source     *SOPSource `json:"source,omitempty"`
	ImportedBy string     `json:"imported_by,omitempty"`
	ImportedAt *time.Time `json:"imported_at,omitempty"`

	// Revision counts the lineage's revisions, 1 for a fresh import; a
	// re-import that changes content moves it, one that changes nothing does
	// not. It is the revision a recall hit and a section read name.
	Revision int `json:"revision"`
}

// ImportOutcome says what an Import did to the lineage its source names.
type ImportOutcome string

// The three outcomes. Unchanged means the current revision already carries the
// imported content, so nothing was written: no revision, no re-cut, no
// re-embedding, and a section pin taken against the current revision holds.
const (
	ImportCreated   ImportOutcome = "created"
	ImportRevised   ImportOutcome = "revised"
	ImportUnchanged ImportOutcome = "unchanged"
)

// Store is the SOP corpus, backed by the memory substrate.
type Store struct {
	sub           substrate.Store
	excerptBudget int // runes; 0 = the substrate's section budget
}

// Option configures NewStore.
type Option func(*Store)

// WithExcerptBudget bounds a recall hit's excerpt in runes (design/06 §4.2)
// when the request does not say. Zero leaves it to the substrate, whose
// section budget is the ceiling either way.
func WithExcerptBudget(runes int) Option { return func(s *Store) { s.excerptBudget = runes } }

// NewStore constructs a Store over a substrate whose `procedures` corpus is
// declared.
func NewStore(sub substrate.Store, opts ...Option) *Store {
	s := &Store{sub: sub}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ErrInvalidRequest marks a request the store cannot act on as written (an
// unknown recall unit); the server answers 400.
var ErrInvalidRequest = errors.New("knowledge: invalid request")

// ErrRevisionChanged marks a section read pinned to a revision that is no
// longer the SOP's current one: section indexes belong to a revision, so the
// caller recalls again rather than reading a different section under the same
// number. The server answers 409.
var ErrRevisionChanged = errors.New("knowledge: sop revision changed since recall")

// ErrCutChanged marks a section read pinned to a cut that is no longer the
// one stored: a changed section budget or splitter re-cuts a revision and
// renumbers its sections without a new revision, so the pin names both. The
// server answers 409, as for ErrRevisionChanged.
var ErrCutChanged = errors.New("knowledge: sop sections re-cut since recall")

// Reindex brings the tenant's knowledge up to date with the substrate's
// configuration (section rule, embedding model): the host runs it at startup
// and an operator on demand. Returns the number of entries touched.
func (s *Store) Reindex(ctx context.Context, tenantID uuid.UUID) (int, error) {
	n, err := s.sub.Reindex(ctx, tenantID.String())
	if err != nil {
		return n, fmt.Errorf("reindex knowledge: %w", err)
	}
	return n, nil
}

// ErrNotFound is returned when a SOP id doesn't exist (or is already retired
// for mutating ops).
var ErrNotFound = errors.New("sop not found")

// Create inserts a new SOP as revision 1 of a fresh lineage and returns its
// stable external id. Lightweight governance publishes on write; the passed
// Status is advisory in v0 (the procedures corpus is lightweight). AuthorID,
// if a Keycloak subject, is recorded as the entry's opaque author.
func (s *Store) Create(ctx context.Context, sop SOP) (uuid.UUID, error) {
	if sop.TenantID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("Create: tenant_id required")
	}
	sop.Body = sopdoc.CanonicalBody(sop.Body)
	if sop.Title == "" || sop.Body == "" {
		return uuid.Nil, fmt.Errorf("Create: title and body required")
	}
	lineage := uuid.New()
	_, err := s.sub.Put(ctx, sop.TenantID.String(), CorpusProcedures, substrate.Entry{
		Title:      sop.Title,
		Body:       sop.Body,
		Tags:       withLineage(sop.Tags, lineage),
		Advice:     sop.Recommendation,
		AuthoredBy: sop.AuthorID,
		Meta:       sopMeta(sop),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create sop: %w", err)
	}
	return lineage, nil
}

// Import upserts institutional knowledge from an external source (design/06
// §2.4): keyed on Source.URL, importing the same document again becomes a
// revision of the existing SOP (its lineage id stable, the prior version
// hash-addressable) instead of a duplicate. Returns the SOP id and what was
// done: created, revised, or unchanged when the current revision already
// carries the content (sameContent), in which case nothing is written.
func (s *Store) Import(ctx context.Context, sop SOP) (uuid.UUID, ImportOutcome, error) {
	if sop.TenantID == uuid.Nil {
		return uuid.Nil, "", fmt.Errorf("Import: tenant_id required")
	}
	sop.Body = sopdoc.CanonicalBody(sop.Body)
	if sop.Title == "" || sop.Body == "" {
		return uuid.Nil, "", fmt.Errorf("Import: title and body required")
	}
	if sop.Source == nil || sop.Source.URL == "" {
		return uuid.Nil, "", fmt.Errorf("Import: source.url required (the re-import lineage key)")
	}
	existing, err := s.findBySourceURL(ctx, sop.TenantID, sop.Source.URL)
	if err != nil {
		return uuid.Nil, "", err
	}
	if existing == nil {
		// A SOP authored in-product has no source. Its lineage id is its
		// pointer (SOPSourcePrefix, what `show` prints for it), so a document
		// shown and imported again revises that lineage instead of opening a
		// second one beside it; the revision then carries the source, and
		// later imports match it by url.
		if id, ok := lineageFromSourceURL(sop.Source.URL); ok {
			e, err := s.resolveCurrent(ctx, sop.TenantID, id, true)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return uuid.Nil, "", err
			}
			if err == nil {
				existing = &e
			}
		}
	}
	if existing == nil {
		id, err := s.Create(ctx, sop)
		if err != nil {
			return uuid.Nil, "", err
		}
		return id, ImportCreated, nil
	}
	lineage, _ := lineageOf(existing.Tags)
	if existing.Status != substrate.StatusRetired && sameContent(*existing, sop) {
		return lineage, ImportUnchanged, nil
	}
	if _, err := s.sub.Revise(ctx, sop.TenantID.String(), CorpusProcedures, existing.ID, substrate.Revision{
		Title:      sop.Title,
		Body:       sop.Body,
		Tags:       withLineage(sop.Tags, lineage),
		Advice:     sop.Recommendation,
		AuthoredBy: sop.AuthorID,
		Meta:       sopMeta(sop),
	}); err != nil {
		return uuid.Nil, "", fmt.Errorf("re-import sop: %w", err)
	}
	return lineage, ImportRevised, nil
}

// SOPSourcePrefix is the source url of a SOP that has no external source: its
// own lineage id. `show` prints it, and Import resolves it to the lineage.
const SOPSourcePrefix = "reckon://sop/"

// lineageFromSourceURL reads a lineage id out of a SOPSourcePrefix url.
func lineageFromSourceURL(url string) (uuid.UUID, bool) {
	rest, ok := strings.CutPrefix(url, SOPSourcePrefix)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	return id, err == nil
}

// sameContent reports whether the current revision already carries what the
// import brings: title, body, tags, recommendation, the document's author and
// the source's system and version. The importer and the import time are the
// provenance of the ingest, not the content (design/06 §2.4), so an identical
// document imported again by someone else is unchanged and leaves no trace.
// The comparison mirrors what a revision would write: the body in its
// canonical form on both sides (every write path now stores it so, and a row
// written before that is compared canonical without touching its stored,
// hash-addressed bytes), tags as the substrate stores them (reserved ones
// stripped, duplicates folded, order ignored), and an import that names no
// author keeps the revision's, so it compares equal.
// A retired entry is never unchanged: the re-import revives the lineage.
func sameContent(e substrate.Entry, sop SOP) bool {
	src, _ := e.Meta["source"].(map[string]any)
	return e.Title == sop.Title &&
		sopdoc.CanonicalBody(e.Body) == sop.Body &&
		e.Advice == sop.Recommendation &&
		(sop.AuthorID == "" || e.AuthoredBy == sop.AuthorID) &&
		sameTags(stripReserved(e.Tags), stripReserved(sop.Tags)) &&
		str(src["system"]) == sop.Source.System &&
		str(src["version"]) == sop.Source.Version
}

// sameTags compares two tag lists as sets: neither the order a document lists
// its tags in nor a tag repeated is content.
func sameTags(a, b []string) bool {
	set := func(tags []string) map[string]bool {
		m := make(map[string]bool, len(tags))
		for _, t := range tags {
			m[t] = true
		}
		return m
	}
	sa, sb := set(a), set(b)
	if len(sa) != len(sb) {
		return false
	}
	for t := range sa {
		if !sb[t] {
			return false
		}
	}
	return true
}

// findBySourceURL locates the current SOP entry whose source pointer matches —
// the re-import lineage lookup. Nil when none exists. Retired entries are
// included on purpose: one source is one lineage forever, so re-importing a
// retired SOP revives it as a new published revision (Revise clears the
// retirement) rather than opening a second lineage beside the retired one.
func (s *Store) findBySourceURL(ctx context.Context, tenantID uuid.UUID, url string) (*substrate.Entry, error) {
	entries, err := s.sub.List(ctx, tenantID.String(), CorpusProcedures, substrate.ListFilter{IncludeRetired: true})
	if err != nil {
		return nil, fmt.Errorf("resolve import source: %w", err)
	}
	for i := range entries {
		src, _ := entries[i].Meta["source"].(map[string]any)
		if src != nil && src["url"] == url {
			return &entries[i], nil
		}
	}
	return nil, nil
}

// sopMeta builds the entry's attribution metadata. Nil when there is nothing
// to attribute (plain in-product authoring).
func sopMeta(sop SOP) map[string]any {
	if sop.Source == nil && sop.ImportedBy == "" {
		return nil
	}
	m := map[string]any{}
	if sop.Source != nil {
		m["source"] = map[string]any{"system": sop.Source.System, "url": sop.Source.URL, "version": sop.Source.Version}
	}
	if sop.ImportedBy != "" {
		m["imported_by"] = sop.ImportedBy
	}
	m["imported_at"] = time.Now().UTC().Format(time.RFC3339)
	return m
}

// Get returns the current revision of one SOP by its lineage id.
func (s *Store) Get(ctx context.Context, tenantID, id uuid.UUID) (SOP, error) {
	e, err := s.resolveCurrent(ctx, tenantID, id, true)
	if err != nil {
		return SOP{}, err
	}
	return toSOP(e, tenantID, id), nil
}

// List returns a tenant's SOPs (current revisions), newest first. Retired SOPs
// are excluded unless includeRetired.
func (s *Store) List(ctx context.Context, tenantID uuid.UUID, includeRetired bool) ([]SOP, error) {
	entries, err := s.sub.List(ctx, tenantID.String(), CorpusProcedures, substrate.ListFilter{IncludeRetired: includeRetired})
	if err != nil {
		return nil, fmt.Errorf("list sops: %w", err)
	}
	out := make([]SOP, 0, len(entries))
	for _, e := range entries {
		lineage, ok := lineageOf(e.Tags)
		if !ok {
			continue // an entry not written through this facade — skip, don't guess an id
		}
		out = append(out, toSOP(e, tenantID, lineage))
	}
	return out, nil
}

// Update replaces the editable fields of a SOP as a new revision, keeping its
// lineage id (the prior revision's content stays hash-addressable). A retired
// SOP cannot be updated.
func (s *Store) Update(ctx context.Context, tenantID, id uuid.UUID, title, body string, tags []string, recommendation string) error {
	e, err := s.resolveCurrent(ctx, tenantID, id, false)
	if err != nil {
		return err
	}
	_, err = s.sub.Revise(ctx, tenantID.String(), CorpusProcedures, e.ID, substrate.Revision{
		Title:      title,
		Body:       sopdoc.CanonicalBody(body),
		Tags:       withLineage(tags, id),
		Advice:     recommendation,
		AuthoredBy: e.AuthoredBy, // carry the original author forward
	})
	if err != nil {
		return fmt.Errorf("update sop: %w", err)
	}
	return nil
}

// Retire soft-deletes a SOP (status → retired). Retired SOPs stay retrievable
// only with include_retired for audit; the content stays hash-addressable.
func (s *Store) Retire(ctx context.Context, tenantID, id uuid.UUID) error {
	e, err := s.resolveCurrent(ctx, tenantID, id, false)
	if err != nil {
		return err
	}
	if err := s.sub.Transition(ctx, tenantID.String(), CorpusProcedures, e.ID, substrate.Transition{To: substrate.StatusRetired}); err != nil {
		return fmt.Errorf("retire sop: %w", err)
	}
	return nil
}

// resolveCurrent finds the current-revision entry for a lineage id via its
// reserved tag (an indexed hard-filter). includeRetired governs whether a
// retired SOP resolves — false for mutating ops, which then report ErrNotFound
// for a missing or retired SOP, matching the pre-substrate behavior.
func (s *Store) resolveCurrent(ctx context.Context, tenantID, id uuid.UUID, includeRetired bool) (substrate.Entry, error) {
	entries, err := s.sub.List(ctx, tenantID.String(), CorpusProcedures, substrate.ListFilter{
		Tag:            lineageTagPrefix + id.String(),
		IncludeRetired: includeRetired,
	})
	if err != nil {
		return substrate.Entry{}, fmt.Errorf("resolve sop: %w", err)
	}
	if len(entries) == 0 {
		return substrate.Entry{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return entries[0], nil
}

// withLineage appends the reserved lineage tag to a user tag list, dropping any
// reserved tags the caller may have echoed back from a prior read.
func withLineage(tags []string, lineage uuid.UUID) []string {
	out := stripReserved(tags)
	return append(out, lineageTagPrefix+lineage.String())
}

// stripReserved returns tags with reserved (double-underscore) entries removed
// — the user-facing view never sees the lineage carrier.
func stripReserved(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if !strings.HasPrefix(t, "__") {
			out = append(out, t)
		}
	}
	return out
}

// lineageOf extracts the lineage id from an entry's reserved tag.
func lineageOf(tags []string) (uuid.UUID, bool) {
	for _, t := range tags {
		if rest, ok := strings.CutPrefix(t, lineageTagPrefix); ok {
			if id, err := uuid.Parse(rest); err == nil {
				return id, true
			}
		}
	}
	return uuid.Nil, false
}

// sopStatus maps a substrate entry status to the SOP status string.
func sopStatus(st substrate.Status) string {
	switch st {
	case substrate.StatusPublished:
		return StatusPublished
	case substrate.StatusRetired:
		return StatusRetired
	case substrate.StatusInReview:
		return StatusInReview
	default:
		return StatusDraft
	}
}

// toSOP maps a substrate entry back to the SOP shape under its lineage id.
func toSOP(e substrate.Entry, tenantID, lineage uuid.UUID) SOP {
	sop := SOP{
		ID:             lineage,
		TenantID:       tenantID,
		Title:          e.Title,
		Body:           e.Body,
		Tags:           stripReserved(e.Tags),
		Status:         sopStatus(e.Status),
		Recommendation: e.Advice,
		AuthorID:       e.AuthoredBy,
		CreatedAt:      e.Created,
		UpdatedAt:      e.Updated,
		PublishedAt:    e.PublishedAt,
		RetiredAt:      e.RetiredAt,
		Revision:       e.Revision,
	}
	if len(e.SignedOffBy) > 0 {
		sop.SignerID = e.SignedOffBy[0]
	}
	if src, ok := e.Meta["source"].(map[string]any); ok {
		sop.Source = &SOPSource{
			System:  str(src["system"]),
			URL:     str(src["url"]),
			Version: str(src["version"]),
		}
	}
	sop.ImportedBy = str(e.Meta["imported_by"])
	if at, err := time.Parse(time.RFC3339, str(e.Meta["imported_at"])); err == nil {
		sop.ImportedAt = &at
	}
	return sop
}

// str extracts a string from decoded-JSON meta, "" for absent/non-string.
func str(v any) string {
	s, _ := v.(string)
	return s
}
