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
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/knowledge/substrate"
)

// CorpusProcedures is the substrate corpus SOPs live in (CURATED, lightweight
// governance for v0 — published on write; gated authoring is the paid module).
const CorpusProcedures = "procedures"

// CorpusCaseSummaries is the DERIVED corpus of concluded-investigation
// summaries (design/06 §3) — populated by SummarizeForKnowledgeIndex (K3),
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

	// ContentHash is the current revision's content address (RFC 8785 over the
	// hashed content); a retrieval names the revision it read by it (adr/0097).
	ContentHash string `json:"content_hash,omitempty"`
	// Format is the imported document's format ("markdown") when the import
	// recorded one; Original is the retained original (input on import, read
	// back by Original()).
	Format   string       `json:"format,omitempty"`
	Original *SOPOriginal `json:"-"`
	// RevisionCount is the substrate revision number of the current entry
	// (monotonic within the lineage, 1 for a fresh SOP): the "rev N" label the
	// Knowledge view shows when the source system recorded no version of its
	// own.
	RevisionCount int `json:"revision_count"`
	// RetiredReason and RetiredBy are the knowledge-domain facts of a
	// retirement (ui/09 §9.3 "Retire (with a reason)"): the substrate's
	// transition carries principals only, so the facade records them in the
	// entry meta of the revision the retirement is applied to.
	RetiredReason string `json:"retired_reason,omitempty"`
	RetiredBy     string `json:"retired_by,omitempty"`
}

// SOPOriginal is the file a procedure was imported from, retained so a
// citation can open what existed at retrieval time (06 §2.4; adr/0097).
type SOPOriginal struct {
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	Bytes     []byte `json:"bytes"`
}

// SOPRevision is one revision of a procedure's lineage (06 §2.2): the content
// hash a retrieval names, when, by whom.
type SOPRevision struct {
	Revision      int        `json:"revision"`
	ContentHash   string     `json:"content_hash"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	AuthorID      string     `json:"author_id,omitempty"`
	ImportedBy    string     `json:"imported_by,omitempty"`
	SourceVersion string     `json:"source_version,omitempty"`
	SupersededAt  *time.Time `json:"superseded_at,omitempty"`
	HasOriginal   bool       `json:"has_original"`
}

// RevisionContent is one revision's content, read back by hash (06 §6.2).
type RevisionContent struct {
	ContentHash string   `json:"content_hash"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Tags        []string `json:"tags"`
	Purged      bool     `json:"purged"`
}

// Store is the SOP corpus, backed by the memory substrate.
type Store struct {
	sub substrate.Store
}

// NewStore constructs a Store over a substrate whose `procedures` corpus is
// declared.
func NewStore(sub substrate.Store) *Store { return &Store{sub: sub} }

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
// hash-addressable) instead of a duplicate. Returns the SOP id and whether a
// new SOP was created (false = revised).
func (s *Store) Import(ctx context.Context, sop SOP) (uuid.UUID, bool, error) {
	if sop.TenantID == uuid.Nil {
		return uuid.Nil, false, fmt.Errorf("Import: tenant_id required")
	}
	if sop.Title == "" || sop.Body == "" {
		return uuid.Nil, false, fmt.Errorf("Import: title and body required")
	}
	if sop.Source == nil || sop.Source.URL == "" {
		return uuid.Nil, false, fmt.Errorf("Import: source.url required (the re-import lineage key)")
	}
	existing, err := s.findBySourceURL(ctx, sop.TenantID, sop.Source.URL)
	if err != nil {
		return uuid.Nil, false, err
	}
	if existing == nil {
		id, err := s.Create(ctx, sop)
		return id, true, err
	}
	lineage, _ := lineageOf(existing.Tags)
	if _, err := s.sub.Revise(ctx, sop.TenantID.String(), CorpusProcedures, existing.ID, substrate.Revision{
		Title:      sop.Title,
		Body:       sop.Body,
		Tags:       withLineage(sop.Tags, lineage),
		Advice:     sop.Recommendation,
		AuthoredBy: sop.AuthorID,
		Meta:       sopMeta(sop),
	}); err != nil {
		return uuid.Nil, false, fmt.Errorf("re-import sop: %w", err)
	}
	return lineage, false, nil
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
	if sop.Format != "" {
		m["format"] = sop.Format
	}
	if o := sop.Original; o != nil && len(o.Bytes) > 0 {
		// The retained original rides the revision's meta (adr/0097): it is
		// part of the hashed content, so the hash a retrieval names covers the
		// bytes the import saw.
		m["original"] = map[string]any{"filename": o.Filename, "media_type": o.MediaType, "bytes_b64": base64.StdEncoding.EncodeToString(o.Bytes)}
	}
	return m
}

// Revisions lists a procedure's lineage, newest first (06 §2.2): every
// revision's content hash, time, author and importer, retired or superseded
// ones included.
func (s *Store) Revisions(ctx context.Context, tenantID, id uuid.UUID) ([]SOPRevision, error) {
	entries, err := s.sub.List(ctx, tenantID.String(), CorpusProcedures, substrate.ListFilter{
		Tag: lineageTagPrefix + id.String(), IncludeRetired: true, IncludeSuperseded: true,
	})
	if err != nil {
		return nil, fmt.Errorf("sop revisions: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	out := make([]SOPRevision, 0, len(entries))
	for _, e := range entries {
		r := SOPRevision{Revision: e.Revision, ContentHash: string(e.ContentHash), Title: e.Title, Status: sopStatus(e.Status), CreatedAt: e.Created, AuthorID: e.AuthoredBy, ImportedBy: str(e.Meta["imported_by"]), SupersededAt: e.SupersededAt}
		if src, ok := e.Meta["source"].(map[string]any); ok {
			r.SourceVersion = str(src["version"])
		}
		_, r.HasOriginal = e.Meta["original"].(map[string]any)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision > out[j].Revision })
	return out, nil
}

// RevisionContent reads one revision by its content hash (06 §6.2): the bytes
// a past retrieval cited, or the purge tombstone.
func (s *Store) RevisionContent(ctx context.Context, tenantID uuid.UUID, hash string) (RevisionContent, error) {
	snap, err := s.sub.Snapshot(ctx, tenantID.String(), substrate.ContentHash(hash))
	if err != nil {
		return RevisionContent{}, fmt.Errorf("sop revision %s: %w", hash, err)
	}
	return RevisionContent{ContentHash: hash, Title: snap.Content.Title, Body: snap.Content.Body, Tags: stripReserved(snap.Content.Tags), Purged: snap.Purged}, nil
}

// Original returns the retained original of a revision (the current one when
// hash is empty); ok is false when the import kept none.
func (s *Store) Original(ctx context.Context, tenantID, id uuid.UUID, hash string) (SOPOriginal, bool, error) {
	var meta map[string]any
	if hash == "" {
		e, err := s.resolveCurrent(ctx, tenantID, id, true)
		if err != nil {
			return SOPOriginal{}, false, err
		}
		meta = e.Meta
	} else {
		snap, err := s.sub.Snapshot(ctx, tenantID.String(), substrate.ContentHash(hash))
		if err != nil {
			return SOPOriginal{}, false, fmt.Errorf("sop revision %s: %w", hash, err)
		}
		meta = snap.Content.Meta
	}
	o, ok := meta["original"].(map[string]any)
	if !ok {
		return SOPOriginal{}, false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(str(o["bytes_b64"]))
	if err != nil {
		return SOPOriginal{}, false, fmt.Errorf("sop original: %w", err)
	}
	return SOPOriginal{Filename: str(o["filename"]), MediaType: str(o["media_type"]), Bytes: raw}, true, nil
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
// lineage id (the prior revision's content stays hash-addressable) and its
// attribution meta (the source pointer must survive an edit, or a later
// re-import of the same document would open a second lineage). A retired SOP
// cannot be updated.
func (s *Store) Update(ctx context.Context, tenantID, id uuid.UUID, title, body string, tags []string, recommendation string) error {
	e, err := s.resolveCurrent(ctx, tenantID, id, false)
	if err != nil {
		return err
	}
	_, err = s.sub.Revise(ctx, tenantID.String(), CorpusProcedures, e.ID, substrate.Revision{
		Title:      title,
		Body:       body,
		Tags:       withLineage(tags, id),
		Advice:     recommendation,
		Meta:       e.Meta,       // the substrate replaces meta on a revision; the attribution is ours to carry
		AuthoredBy: e.AuthoredBy, // carry the original author forward
	})
	if err != nil {
		return fmt.Errorf("update sop: %w", err)
	}
	return nil
}

// Retire soft-deletes a SOP (status → retired) with the reason and the
// principal recorded in the entry meta (ui/09 §9.3). Retired SOPs stay
// retrievable only with include_retired for audit; the content stays
// hash-addressable. The reason rides a revision of the current content (the
// substrate never mutates a row's meta), so a retirement is one revision plus
// the transition.
func (s *Store) Retire(ctx context.Context, tenantID, id uuid.UUID, reason, by string) error {
	e, err := s.resolveCurrent(ctx, tenantID, id, false)
	if err != nil {
		return err
	}
	target := e
	if reason != "" || by != "" {
		meta := make(map[string]any, len(e.Meta)+2)
		for k, v := range e.Meta {
			meta[k] = v
		}
		if reason != "" {
			meta["retired_reason"] = reason
		}
		if by != "" {
			meta["retired_by"] = by
		}
		target, err = s.sub.Revise(ctx, tenantID.String(), CorpusProcedures, e.ID, substrate.Revision{
			Title: e.Title, Body: e.Body, Tags: e.Tags, Advice: e.Advice, Meta: meta, AuthoredBy: e.AuthoredBy,
		})
		if err != nil {
			return fmt.Errorf("retire sop: record reason: %w", err)
		}
	}
	if err := s.sub.Transition(ctx, tenantID.String(), CorpusProcedures, target.ID, substrate.Transition{To: substrate.StatusRetired, Principals: principals(by)}); err != nil {
		return fmt.Errorf("retire sop: %w", err)
	}
	return nil
}

// LookupBySource resolves the SOP an import of the given source URL would
// revise — the dry-run read behind the Knowledge view's import preview (ui/09
// §9.5). Retired SOPs resolve too, because re-importing a retired source
// revives its lineage (see findBySourceURL). ok is false when none exists.
func (s *Store) LookupBySource(ctx context.Context, tenantID uuid.UUID, url string) (SOP, bool, error) {
	e, err := s.findBySourceURL(ctx, tenantID, url)
	if err != nil || e == nil {
		return SOP{}, false, err
	}
	lineage, ok := lineageOf(e.Tags)
	if !ok {
		return SOP{}, false, nil
	}
	return toSOP(*e, tenantID, lineage), true, nil
}

func principals(by string) []string {
	if by == "" {
		return nil
	}
	return []string{by}
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
		RevisionCount:  e.Revision,
		RetiredReason:  str(e.Meta["retired_reason"]),
		RetiredBy:      str(e.Meta["retired_by"]),
		ContentHash:    string(e.ContentHash),
		Format:         str(e.Meta["format"]),
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
