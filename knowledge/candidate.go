package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Candidate statuses.
const (
	CandidateNew       = "NEW"
	CandidateAccepted  = "ACCEPTED"
	CandidateDiscarded = "DISCARDED"
)

// ErrCandidateNotFound is returned for an unknown candidate id.
var ErrCandidateNotFound = errors.New("candidate not found")

// ErrCandidateDecided is returned when a decision is applied to a candidate
// that already has one (other than accepting an accepted one again, which
// revises the same procedure).
var ErrCandidateDecided = errors.New("candidate already decided")

// Candidate is a procedure the agent drafted at an investigation's conclusion
// (adr/0112; design/ui 09 §9.7), held for review. A CRUD record, never a
// corpus entry: an unreviewed draft must not be recallable.
type Candidate struct {
	ID              uuid.UUID  `json:"id"`
	TenantID        uuid.UUID  `json:"tenant_id"`
	InvestigationID uuid.UUID  `json:"investigation_id"`
	Title           string     `json:"title"`
	Body            string     `json:"body"`
	Tags            []string   `json:"tags"`
	Recommendation  string     `json:"recommendation,omitempty"`
	Rationale       string     `json:"rationale,omitempty"`
	GeneratorModel  string     `json:"generator_model,omitempty"`
	DraftedAt       time.Time  `json:"drafted_at"`
	DraftedBy       string     `json:"drafted_by"`
	DraftedByName   string     `json:"drafted_by_name,omitempty"`
	Status          string     `json:"status"`
	DecidedBy       string     `json:"decided_by,omitempty"`
	DecidedByName   string     `json:"decided_by_name,omitempty"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
	DiscardReason   string     `json:"discard_reason,omitempty"`
	AcceptedSOPID   *uuid.UUID `json:"accepted_sop_id,omitempty"`
}

// CandidateStore is the CRUD store over procedure_candidates in the
// reckon_knowledge database.
type CandidateStore struct {
	db *sql.DB
}

// NewCandidateStore constructs the store over the knowledge database.
func NewCandidateStore(db *sql.DB) *CandidateStore { return &CandidateStore{db: db} }

const candidateCols = `id, tenant_id, investigation_id, title, body, tags, recommendation, rationale, generator_model,
	drafted_at, drafted_by, drafted_by_name, status, decided_by, decided_by_name, decided_at, discard_reason, accepted_sop_id`

// Create records a NEW candidate. The id is minted here when zero.
func (s *CandidateStore) Create(ctx context.Context, c Candidate) (Candidate, error) {
	if c.TenantID == uuid.Nil || c.InvestigationID == uuid.Nil {
		return Candidate{}, fmt.Errorf("candidate: tenant_id and investigation_id are required")
	}
	if strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Body) == "" {
		return Candidate{}, fmt.Errorf("candidate: title and body are required")
	}
	if c.DraftedBy == "" {
		return Candidate{}, fmt.Errorf("candidate: drafted_by is required")
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.DraftedAt.IsZero() {
		c.DraftedAt = time.Now().UTC()
	}
	if c.Tags == nil {
		c.Tags = []string{}
	}
	c.Status = CandidateNew
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO procedure_candidates (id, tenant_id, investigation_id, title, body, tags, recommendation, rationale, generator_model, drafted_at, drafted_by, drafted_by_name, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, c.ID, c.TenantID, c.InvestigationID, c.Title, c.Body, pq.Array(c.Tags), c.Recommendation, c.Rationale, c.GeneratorModel, c.DraftedAt, c.DraftedBy, c.DraftedByName, c.Status)
	if err != nil {
		return Candidate{}, fmt.Errorf("candidate: insert: %w", err)
	}
	return c, nil
}

// Get reads one candidate.
func (s *CandidateStore) Get(ctx context.Context, tenantID, id uuid.UUID) (Candidate, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+candidateCols+` FROM procedure_candidates WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	c, err := scanCandidate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, ErrCandidateNotFound
	}
	return c, err
}

// List returns candidates newest first, narrowed to a status when one is given.
func (s *CandidateStore) List(ctx context.Context, tenantID uuid.UUID, status string) ([]Candidate, error) {
	q := `SELECT ` + candidateCols + ` FROM procedure_candidates WHERE tenant_id = $1`
	args := []any{tenantID}
	if status != "" {
		args = append(args, strings.ToUpper(status))
		q += ` AND status = $2`
	}
	q += ` ORDER BY drafted_at DESC, id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("candidate: list: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-side close
	out := []Candidate{}
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountNew returns how many candidates await review.
func (s *CandidateStore) CountNew(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM procedure_candidates WHERE tenant_id = $1 AND status = $2`, tenantID, CandidateNew).Scan(&n)
	return n, err
}

// Accept marks the candidate ACCEPTED with the procedure the import produced
// and the (possibly edited) body that was imported. Accepting an ACCEPTED
// candidate again is legal — it revised the same procedure. A DISCARDED one
// cannot be accepted.
func (s *CandidateStore) Accept(ctx context.Context, tenantID, id, sopID uuid.UUID, body, by, byName string, at time.Time) (Candidate, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE procedure_candidates
		SET status = $3, accepted_sop_id = $4, body = $5, decided_by = $6, decided_by_name = $7, decided_at = $8, discard_reason = NULL
		WHERE tenant_id = $1 AND id = $2 AND status <> $9
	`, tenantID, id, CandidateAccepted, sopID, body, by, byName, at, CandidateDiscarded)
	if err != nil {
		return Candidate{}, fmt.Errorf("candidate: accept: %w", err)
	}
	return s.afterDecision(ctx, tenantID, id, res)
}

// Discard marks a NEW candidate DISCARDED with the reviewer's reason.
func (s *CandidateStore) Discard(ctx context.Context, tenantID, id uuid.UUID, reason, by, byName string, at time.Time) (Candidate, error) {
	if strings.TrimSpace(reason) == "" {
		return Candidate{}, fmt.Errorf("candidate: a discard carries a reason")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE procedure_candidates
		SET status = $3, discard_reason = $4, decided_by = $5, decided_by_name = $6, decided_at = $7
		WHERE tenant_id = $1 AND id = $2 AND status = $8
	`, tenantID, id, CandidateDiscarded, reason, by, byName, at, CandidateNew)
	if err != nil {
		return Candidate{}, fmt.Errorf("candidate: discard: %w", err)
	}
	return s.afterDecision(ctx, tenantID, id, res)
}

func (s *CandidateStore) afterDecision(ctx context.Context, tenantID, id uuid.UUID, res sql.Result) (Candidate, error) {
	if n, _ := res.RowsAffected(); n == 0 {
		c, err := s.Get(ctx, tenantID, id)
		if err != nil {
			return Candidate{}, err
		}
		return c, fmt.Errorf("%w: %s", ErrCandidateDecided, c.Status)
	}
	return s.Get(ctx, tenantID, id)
}

type rowScanner interface{ Scan(dest ...any) error }

func scanCandidate(r rowScanner) (Candidate, error) {
	var c Candidate
	var tags pq.StringArray
	var decidedBy, decidedByName, reason sql.NullString
	var decidedAt sql.NullTime
	var sopID uuid.NullUUID
	if err := r.Scan(&c.ID, &c.TenantID, &c.InvestigationID, &c.Title, &c.Body, &tags, &c.Recommendation, &c.Rationale, &c.GeneratorModel,
		&c.DraftedAt, &c.DraftedBy, &c.DraftedByName, &c.Status, &decidedBy, &decidedByName, &decidedAt, &reason, &sopID); err != nil {
		return Candidate{}, err
	}
	c.Tags = []string(tags)
	if c.Tags == nil {
		c.Tags = []string{}
	}
	c.DecidedBy, c.DecidedByName, c.DiscardReason = decidedBy.String, decidedByName.String, reason.String
	if decidedAt.Valid {
		t := decidedAt.Time
		c.DecidedAt = &t
	}
	if sopID.Valid {
		u := sopID.UUID
		c.AcceptedSOPID = &u
	}
	return c, nil
}
