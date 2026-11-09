package knowledge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/knowledge/substrate"
)

// ErrBadCut marks a historical section read whose cut is malformed or names a
// splitter version this build has no rule for: a bad request, not lost data.
// The server answers 400.
var ErrBadCut = errors.New("knowledge: unknown section cut")

// SOPRevision is one revision of a procedure's lineage: who wrote it, when it
// was superseded, and whether it is the current one. The content is read by
// revision through RevisionSections and RevisionSection; the hash attests it.
type SOPRevision struct {
	Revision     int        `json:"revision"`
	ContentHash  string     `json:"content_hash"`
	CreatedAt    time.Time  `json:"created_at"`
	SupersededAt *time.Time `json:"superseded_at,omitempty"`
	AuthoredBy   string     `json:"authored_by,omitempty"`
	ImportedBy   string     `json:"imported_by,omitempty"`
	ImportedAt   *time.Time `json:"imported_at,omitempty"`
	Source       *SOPSource `json:"source,omitempty"`
	Current      bool       `json:"current"`
}

// SOPRevisions is a procedure's revision history, newest first
// (GET /api/sops/{id}/revisions). Current is the revision number the
// procedure stands at; a retired procedure still has one.
type SOPRevisions struct {
	SOPID     uuid.UUID     `json:"sop_id"`
	Current   int           `json:"current"`
	Revisions []SOPRevision `json:"revisions"`
}

// Revisions lists every revision of a SOP by its lineage id, newest first,
// retired lineages included. An unknown lineage is ErrNotFound.
func (s *Store) Revisions(ctx context.Context, tenantID, id uuid.UUID) (SOPRevisions, error) {
	rows, err := s.lineageRows(ctx, tenantID, id)
	if err != nil {
		return SOPRevisions{}, err
	}
	out := SOPRevisions{SOPID: id, Revisions: make([]SOPRevision, 0, len(rows))}
	for _, e := range rows {
		sop := toSOP(e, tenantID, id)
		r := SOPRevision{
			Revision:     e.Revision,
			ContentHash:  string(e.ContentHash),
			CreatedAt:    e.Created,
			SupersededAt: e.SupersededAt,
			AuthoredBy:   e.AuthoredBy,
			ImportedBy:   sop.ImportedBy,
			ImportedAt:   sop.ImportedAt,
			Source:       sop.Source,
			Current:      e.SupersededAt == nil,
		}
		if r.Current {
			out.Current = e.Revision
		}
		out.Revisions = append(out.Revisions, r)
	}
	return out, nil
}

// RevisionSections returns the outline of one revision of a SOP under a cut,
// the current revision included. See revisionSections for how the cut is
// resolved; an unknown revision is ErrNotFound.
func (s *Store) RevisionSections(ctx context.Context, tenantID, id uuid.UUID, revision int, cut string) (SOPOutline, error) {
	e, secs, err := s.revisionSections(ctx, tenantID, id, revision, cut)
	if err != nil {
		return SOPOutline{}, err
	}
	return outlineOf(id, e, secs), nil
}

// RevisionSection returns one section of one revision of a SOP under a cut,
// with the outline around it: the text an act was shown, readable after the
// procedure moved on. An unknown revision or index is ErrNotFound.
func (s *Store) RevisionSection(ctx context.Context, tenantID, id uuid.UUID, revision, idx int, cut string) (SOPSectionRead, error) {
	e, secs, err := s.revisionSections(ctx, tenantID, id, revision, cut)
	if err != nil {
		return SOPSectionRead{}, err
	}
	return sectionReadOf(id, e, secs, idx)
}

// revisionSections resolves the row at revision and its sections under cut.
// The stored sections are served when they carry the requested cut (or when
// no cut is requested); otherwise the cut is recomputed from the row's body
// under the splitter version and budget the cut names, which is the same
// text the store once held: the body is attested by the content hash and
// every splitter version stays in code. A row with no stored sections and
// no requested cut is cut under the current version and default budget. A
// malformed cut, or one naming a splitter this build lacks, is ErrBadCut.
// The read never refuses a cut that was once valid.
func (s *Store) revisionSections(ctx context.Context, tenantID, id uuid.UUID, revision int, cut string) (substrate.Entry, []substrate.Section, error) {
	rows, err := s.lineageRows(ctx, tenantID, id)
	if err != nil {
		return substrate.Entry{}, nil, err
	}
	var e substrate.Entry
	found := false
	for _, row := range rows {
		if row.Revision == revision {
			e, found = row, true
			break
		}
	}
	if !found {
		return substrate.Entry{}, nil, fmt.Errorf("%w: %s revision %d", ErrNotFound, id, revision)
	}
	version, budget := substrate.SplitterVersion, substrate.DefaultSectionBudgetRunes
	if cut != "" {
		version, budget, err = substrate.ParseCut(cut)
		if err != nil {
			return substrate.Entry{}, nil, fmt.Errorf("%w: %w", ErrBadCut, err)
		}
	}
	stored, err := s.sub.Sections(ctx, tenantID.String(), CorpusProcedures, e.ID)
	if err != nil {
		return substrate.Entry{}, nil, fmt.Errorf("sop revision sections: %w", err)
	}
	if len(stored) > 0 && (cut == "" || stored[0].Cut() == cut) {
		return e, stored, nil
	}
	secs, err := substrate.SplitSectionsAt(version, e.Body, budget)
	if err != nil {
		return substrate.Entry{}, nil, fmt.Errorf("%w: %w", ErrBadCut, err)
	}
	if len(secs) == 0 {
		return substrate.Entry{}, nil, fmt.Errorf("sop revision sections: %w: entry %s", substrate.ErrNoSections, e.ID)
	}
	return e, secs, nil
}

// lineageRows lists every row of a lineage, newest revision first, superseded
// and retired rows included. An unknown lineage is ErrNotFound.
func (s *Store) lineageRows(ctx context.Context, tenantID, id uuid.UUID) ([]substrate.Entry, error) {
	rows, err := s.sub.List(ctx, tenantID.String(), CorpusProcedures, substrate.ListFilter{
		Tags:              []string{lineageTagPrefix + id.String()},
		IncludeRetired:    true,
		IncludeSuperseded: true,
	})
	if err != nil {
		return nil, fmt.Errorf("sop revisions: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Revision > rows[j].Revision })
	return rows, nil
}
