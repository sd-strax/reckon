package summary

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Narrative is one revision of an investigation's narrative (adr/0103): the
// "what happened" prose of the two-tier summary (06 §3.2), with its authorship.
type Narrative struct {
	ID             uuid.UUID `json:"id"`
	Revision       int       `json:"revision"`
	Body           string    `json:"body"`
	GeneratorModel string    `json:"generator_model,omitempty"`
	AsOfSequence   int64     `json:"as_of_sequence"`
	AuthorKind     string    `json:"author_kind"` // AI_DELEGATED | HUMAN
	AuthorID       string    `json:"author_id"`
	AuthorName     string    `json:"author_name,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// Author kinds.
const (
	AuthorAI    = "AI_DELEGATED"
	AuthorHuman = "HUMAN"
)

// NarrativeStore is the revisions table over the main DB.
type NarrativeStore struct{ db *sql.DB }

// NewNarrativeStore wraps the main DB.
func NewNarrativeStore(db *sql.DB) *NarrativeStore { return &NarrativeStore{db: db} }

// Put appends a revision. The AI draft is never overwritten: an analyst edit
// is a HUMAN revision above it; a second draft is a new AI revision.
func (s *NarrativeStore) Put(ctx context.Context, tenantID, investigationID uuid.UUID, n Narrative) (Narrative, error) {
	body := strings.TrimSpace(n.Body)
	if body == "" {
		return Narrative{}, errors.New("narrative: body is required")
	}
	if n.AuthorKind != AuthorAI && n.AuthorKind != AuthorHuman {
		return Narrative{}, fmt.Errorf("narrative: author_kind %q is not AI_DELEGATED|HUMAN", n.AuthorKind)
	}
	if n.AuthorID == "" {
		return Narrative{}, errors.New("narrative: author_id is required")
	}
	n.ID = uuid.Must(uuid.NewV7())
	n.Body = body
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO investigation_narratives (id, tenant_id, investigation_id, revision, body, generator_model, as_of_sequence, author_kind, author_id, author_name, created_at)
		VALUES ($1, $2, $3, (SELECT COALESCE(MAX(revision), 0) + 1 FROM investigation_narratives WHERE investigation_id = $3), $4, $5, $6, $7, $8, $9, $10)
		RETURNING revision
	`, n.ID, tenantID, investigationID, n.Body, n.GeneratorModel, n.AsOfSequence, n.AuthorKind, n.AuthorID, n.AuthorName, n.CreatedAt).Scan(&n.Revision)
	if err != nil {
		return Narrative{}, fmt.Errorf("insert investigation_narratives: %w", err)
	}
	return n, nil
}

// List returns every revision, oldest first.
func (s *NarrativeStore) List(ctx context.Context, investigationID uuid.UUID) ([]Narrative, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, revision, body, generator_model, as_of_sequence, author_kind, author_id, author_name, created_at
		FROM investigation_narratives WHERE investigation_id = $1 ORDER BY revision
	`, investigationID)
	if err != nil {
		return nil, fmt.Errorf("query investigation_narratives: %w", err)
	}
	defer rows.Close()
	out := []Narrative{}
	for rows.Next() {
		var n Narrative
		if err := rows.Scan(&n.ID, &n.Revision, &n.Body, &n.GeneratorModel, &n.AsOfSequence, &n.AuthorKind, &n.AuthorID, &n.AuthorName, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan narrative: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Authorship is the line the Summary lens shows: who drafted, who last edited.
type Authorship struct {
	DraftedBy       string `json:"drafted_by,omitempty"` // "reckon" for an AI draft, else the author's name
	DraftedModel    string `json:"drafted_model,omitempty"`
	EditedBy        string `json:"edited_by,omitempty"` // the latest HUMAN revision's author, when one exists above the draft
	Revisions       int    `json:"revisions"`
	LatestAsOf      int64  `json:"latest_as_of_sequence"`
	LatestIsCurrent bool   `json:"latest_is_current"` // the latest revision describes the record at its current sequence
}

// AuthorshipOf reads the lineage into the authorship line; currentSeq is the
// record's last event sequence.
func AuthorshipOf(revs []Narrative, currentSeq int64) Authorship {
	a := Authorship{Revisions: len(revs)}
	if len(revs) == 0 {
		return a
	}
	first := revs[0]
	if first.AuthorKind == AuthorAI {
		a.DraftedBy, a.DraftedModel = "reckon", first.GeneratorModel
	} else {
		a.DraftedBy = firstNonEmpty(first.AuthorName, first.AuthorID)
	}
	for i := len(revs) - 1; i > 0; i-- {
		if revs[i].AuthorKind == AuthorHuman {
			a.EditedBy = firstNonEmpty(revs[i].AuthorName, revs[i].AuthorID)
			break
		}
	}
	latest := revs[len(revs)-1]
	a.LatestAsOf = latest.AsOfSequence
	a.LatestIsCurrent = latest.AsOfSequence >= currentSeq
	return a
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
