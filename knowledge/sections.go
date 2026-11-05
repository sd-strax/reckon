package knowledge

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/knowledge/substrate"
)

// SOPSection is one section of a procedure (design/06 §4.2): its place in
// the document and, when read singly, its text. Runes is the text's length
// whether or not the text is carried, so a reader can budget before reading.
type SOPSection struct {
	Idx         int      `json:"idx"`
	Of          int      `json:"of"`
	HeadingPath []string `json:"heading_path"`
	Part        int      `json:"part,omitempty"`
	Parts       int      `json:"parts,omitempty"`
	Location    string   `json:"location"`
	Runes       int      `json:"runes"`
	Text        string   `json:"text,omitempty"`
}

// SOPOutline is a procedure's table of contents: every section without its
// text (GET /api/sops/{id}/sections).
type SOPOutline struct {
	SOPID    uuid.UUID    `json:"sop_id"`
	Title    string       `json:"title"`
	Revision int          `json:"revision"`
	Status   string       `json:"status"`
	Sections []SOPSection `json:"sections"`
}

// SOPSectionRead is one section read whole, with the outline around it
// (GET /api/sops/{id}/sections/{n}), so one call answers "what does this part
// say and what else is there".
type SOPSectionRead struct {
	SOPID    uuid.UUID  `json:"sop_id"`
	Title    string     `json:"title"`
	Revision int        `json:"revision"`
	Status   string     `json:"status"`
	Section  SOPSection `json:"section"`
	Outline  []string   `json:"outline"`
}

// Sections returns the current revision's outline for a SOP by its lineage id.
func (s *Store) Sections(ctx context.Context, tenantID, id uuid.UUID) (SOPOutline, error) {
	e, secs, err := s.currentSections(ctx, tenantID, id)
	if err != nil {
		return SOPOutline{}, err
	}
	out := SOPOutline{SOPID: id, Title: e.Title, Revision: e.Revision, Status: sopStatus(e.Status), Sections: make([]SOPSection, 0, len(secs))}
	for _, sec := range secs {
		out.Sections = append(out.Sections, sopSectionOf(sec, len(secs), false))
	}
	return out, nil
}

// Section returns one section of a SOP's current revision, by its 1-based
// index, with the outline. An index the document does not have is
// ErrNotFound.
func (s *Store) Section(ctx context.Context, tenantID, id uuid.UUID, idx int) (SOPSectionRead, error) {
	e, secs, err := s.currentSections(ctx, tenantID, id)
	if err != nil {
		return SOPSectionRead{}, err
	}
	out := SOPSectionRead{SOPID: id, Title: e.Title, Revision: e.Revision, Status: sopStatus(e.Status), Outline: make([]string, 0, len(secs))}
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

func (s *Store) currentSections(ctx context.Context, tenantID, id uuid.UUID) (substrate.Entry, []substrate.Section, error) {
	e, err := s.resolveCurrent(ctx, tenantID, id, true)
	if err != nil {
		return substrate.Entry{}, nil, err
	}
	secs, err := s.sub.Sections(ctx, tenantID.String(), CorpusProcedures, e.ID)
	if err != nil {
		return substrate.Entry{}, nil, fmt.Errorf("sop sections: %w", err)
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
