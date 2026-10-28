package aggregate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Citation kinds: what the knowledge_ref names.
const (
	CitationKindSOP  = "sop"
	CitationKindCase = "case"
)

// KnowledgeCitationProjector folds every interpretation.recorded event's
// consulted_sops and consulted_similar_investigations into knowledge_citations
// (adr/0109): that a procedure or a past case was consulted in an
// investigation, by which act, and whether the act applied it. It runs in the
// command's transaction like every projector, so "cited in N investigations"
// can never disagree with the event log, and Reset + replay rebuilds it. The
// retrieval side store (adr/0097) records what text was read; this records
// that the knowledge was consulted: two facts, two stores, one event behind
// both. A superseded act keeps its citation — the consultation happened.
type KnowledgeCitationProjector struct{}

// Name returns "knowledge_citations".
func (KnowledgeCitationProjector) Name() string { return "knowledge_citations" }

// Apply projects one event; anything but an interpretation is a no-op.
func (KnowledgeCitationProjector) Apply(ctx context.Context, tx *sql.Tx, evt Event) error {
	if evt.Type != EventTypeInterpretationRecorded {
		return nil
	}
	var rec InterpretationRecorded
	if err := json.Unmarshal(evt.Payload, &rec); err != nil {
		return fmt.Errorf("unmarshal InterpretationRecorded: %w", err)
	}
	insert := func(kind, ref string, used bool) error {
		if ref == "" {
			return nil
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO knowledge_citations (tenant_id, kind, knowledge_ref, aggregate_id, interpretation_id, used, recorded_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (kind, knowledge_ref, interpretation_id) DO UPDATE SET used = knowledge_citations.used OR EXCLUDED.used
		`, evt.TenantID, kind, ref, evt.AggregateID, rec.InterpretationID, used, evt.OccurredAt)
		if err != nil {
			return fmt.Errorf("insert knowledge citation %s %s: %w", kind, ref, err)
		}
		return nil
	}
	for _, c := range rec.ConsultedSOPs {
		if err := insert(CitationKindSOP, c.SOPID, c.Used); err != nil {
			return err
		}
	}
	for _, c := range rec.ConsultedSimilar {
		if err := insert(CitationKindCase, c.InvestigationRef, c.Used); err != nil {
			return err
		}
	}
	return nil
}

// Reset wipes the projection for a replay.
func (KnowledgeCitationProjector) Reset(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `TRUNCATE knowledge_citations`); err != nil {
		return fmt.Errorf("reset knowledge_citations: %w", err)
	}
	return nil
}

// KnowledgeCitation is one investigation's use of a piece of knowledge: the
// citing investigation (with its current title and status from
// investigation_current), whether any act applied it, how many acts cited
// it, and the first and last time.
type KnowledgeCitation struct {
	AggregateID     AggregateID `json:"investigation_id"`
	Title           string      `json:"title"`
	Status          string      `json:"status"`
	Applied         bool        `json:"applied"`
	Interpretations int         `json:"interpretations"`
	FirstAt         time.Time   `json:"first_at"`
	LastAt          time.Time   `json:"last_at"`
}

// ListKnowledgeCitations returns the investigations that consulted one piece
// of knowledge, most recently cited first.
func ListKnowledgeCitations(ctx context.Context, db *sql.DB, tenantID uuid.UUID, kind, ref string) ([]KnowledgeCitation, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.aggregate_id, COALESCE(ic.title, ''), COALESCE(ic.status, ''),
		       bool_or(c.used), count(*)::int, min(c.recorded_at), max(c.recorded_at)
		FROM knowledge_citations c
		LEFT JOIN investigation_current ic ON ic.aggregate_id = c.aggregate_id
		WHERE c.tenant_id = $1 AND c.kind = $2 AND c.knowledge_ref = $3
		GROUP BY c.aggregate_id, ic.title, ic.status
		ORDER BY max(c.recorded_at) DESC, c.aggregate_id
	`, tenantID, kind, ref)
	if err != nil {
		return nil, fmt.Errorf("list knowledge citations: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-side close
	out := []KnowledgeCitation{}
	for rows.Next() {
		var c KnowledgeCitation
		if err := rows.Scan(&c.AggregateID, &c.Title, &c.Status, &c.Applied, &c.Interpretations, &c.FirstAt, &c.LastAt); err != nil {
			return nil, fmt.Errorf("scan knowledge citation: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountKnowledgeCitations returns, per knowledge ref of one kind, the number of
// distinct investigations that consulted it (applied or only consulted).
func CountKnowledgeCitations(ctx context.Context, db *sql.DB, tenantID uuid.UUID, kind string) (map[string]int, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT knowledge_ref, count(DISTINCT aggregate_id)::int
		FROM knowledge_citations WHERE tenant_id = $1 AND kind = $2
		GROUP BY knowledge_ref
	`, tenantID, kind)
	if err != nil {
		return nil, fmt.Errorf("count knowledge citations: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-side close
	out := map[string]int{}
	for rows.Next() {
		var ref string
		var n int
		if err := rows.Scan(&ref, &n); err != nil {
			return nil, fmt.Errorf("scan knowledge citation count: %w", err)
		}
		out[ref] = n
	}
	return out, rows.Err()
}
