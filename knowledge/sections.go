package knowledge

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/knowledge/substrate"
)

// SectionPin is what a section index is read against: the revision and the
// cut a recall hit (or an earlier read) reported. Section indexes are stable
// only within one revision and one cut, so a pinned read is refused
// (ErrRevisionChanged, ErrCutChanged) when either has moved, rather than
// answered with a different section under the same number. The zero value is
// unpinned: an outline read that establishes the pin.
type SectionPin struct {
	Revision int
	Cut      string
}

// Sections returns the current revision's outline for a SOP by its lineage
// id, under pin.
func (s *Store) Sections(ctx context.Context, tenantID, id uuid.UUID, pin SectionPin) (SOPOutline, error) {
	e, secs, err := s.currentSections(ctx, tenantID, id, pin)
	if err != nil {
		return SOPOutline{}, err
	}
	return outlineOf(id, e, secs), nil
}

// Section returns one section of a SOP's current revision, by its 1-based
// index, with the outline. An index the document does not have is
// ErrNotFound; pin is as for Sections.
func (s *Store) Section(ctx context.Context, tenantID, id uuid.UUID, idx int, pin SectionPin) (SOPSectionRead, error) {
	e, secs, err := s.currentSections(ctx, tenantID, id, pin)
	if err != nil {
		return SOPSectionRead{}, err
	}
	return sectionReadOf(id, e, secs, idx)
}

// outlineOf renders a revision's sections as its outline. secs is non-empty.
func outlineOf(id uuid.UUID, e substrate.Entry, secs []substrate.Section) SOPOutline {
	out := SOPOutline{SOPID: id, Title: e.Title, Revision: e.Revision, Cut: secs[0].Cut(), Status: sopStatus(e.Status), Sections: make([]SOPSection, 0, len(secs))}
	for _, sec := range secs {
		out.Sections = append(out.Sections, sopSectionOf(sec, len(secs), false))
	}
	return out
}

// sectionReadOf renders one section of a revision whole, with the outline
// around it. An index the cut does not have is ErrNotFound.
func sectionReadOf(id uuid.UUID, e substrate.Entry, secs []substrate.Section, idx int) (SOPSectionRead, error) {
	out := SOPSectionRead{SOPID: id, Title: e.Title, Revision: e.Revision, Cut: secs[0].Cut(), Status: sopStatus(e.Status), Outline: make([]string, 0, len(secs))}
	found := false
	for _, sec := range secs {
		out.Outline = append(out.Outline, sec.Location())
		if sec.Idx == idx {
			out.Section = sopSectionOf(sec, len(secs), true)
			found = true
		}
	}
	if !found {
		return SOPSectionRead{}, fmt.Errorf("%w: section %d of %d", ErrNotFound, idx, len(secs))
	}
	return out, nil
}

func (s *Store) currentSections(ctx context.Context, tenantID, id uuid.UUID, pin SectionPin) (substrate.Entry, []substrate.Section, error) {
	e, err := s.resolveCurrent(ctx, tenantID, id, true)
	if err != nil {
		return substrate.Entry{}, nil, err
	}
	if pin.Revision > 0 && e.Revision != pin.Revision {
		return substrate.Entry{}, nil, fmt.Errorf("%w: revision %d requested, %d is current", ErrRevisionChanged, pin.Revision, e.Revision)
	}
	secs, err := s.sub.Sections(ctx, tenantID.String(), CorpusProcedures, e.ID)
	if err != nil {
		return substrate.Entry{}, nil, fmt.Errorf("sop sections: %w", err)
	}
	if len(secs) == 0 {
		return substrate.Entry{}, nil, fmt.Errorf("sop sections: %w: entry %s", substrate.ErrNoSections, e.ID)
	}
	if pin.Cut != "" && secs[0].Cut() != pin.Cut {
		return substrate.Entry{}, nil, fmt.Errorf("%w: cut %s requested, %s is stored", ErrCutChanged, pin.Cut, secs[0].Cut())
	}
	return e, secs, nil
}

func sopSectionOf(sec substrate.Section, of int, withText bool) SOPSection {
	path := sec.HeadingPath
	if path == nil {
		path = []string{}
	}
	out := SOPSection{Idx: sec.Idx, Of: of, HeadingPath: path, Part: sec.Part, Parts: sec.Parts, Location: sec.Location(), Runes: len([]rune(sec.Text))}
	if withText {
		out.Text = sec.Text
	}
	return out
}
