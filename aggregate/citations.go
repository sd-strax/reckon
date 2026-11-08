package aggregate

// Cross-investigation knowledge citations (design/06 §6.1; design/ui binding
// §6 gap 22; adr/0109): the fold across investigations of what each reasoning
// act consulted. The sibling of ref_appearances for knowledge: one row per
// consulted entry on an InterpretationRecorded event, a procedure (kind sop,
// ref = the SOP's lineage id) or a past case (kind case, ref = the summary's
// investigation ref), at the revision and cut the act had in front of it.
// Nothing is folded at write time: one act may carry one procedure at two
// revisions or two cuts (adr/0303), and the record never loses a fact it
// carried. The reads collapse to one row per investigation.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// Citation kinds: what a knowledge_citations ref names.
const (
	CitationKindSOP  = "sop"
	CitationKindCase = "case"
)

// KnowledgeCitationProjector maintains knowledge_citations in the append
// transaction.
type KnowledgeCitationProjector struct{}

// Name returns "knowledge_citations".
func (KnowledgeCitationProjector) Name() string { return "knowledge_citations" }

// Apply folds one event's consulted knowledge. Only InterpretationRecorded
// carries any; every other event is a no-op.
func (KnowledgeCitationProjector) Apply(ctx context.Context, tx *sql.Tx, evt Event) error {
	if evt.Type != EventTypeInterpretationRecorded {
		return nil
	}
	var rec InterpretationRecorded
	if err := json.Unmarshal(evt.Payload, &rec); err != nil {
		return fmt.Errorf("unmarshal InterpretationRecorded: %w", err)
	}
	for _, c := range rec.ConsultedSOPs {
		if c.SOPID == "" {
			continue
		}
		if err := insertCitation(ctx, tx, evt, rec, c.SOPID, CitationKindSOP, c.Revision, c.Cut, c.Cited); err != nil {
			return err
		}
	}
	for _, c := range rec.ConsultedSimilar {
		if c.InvestigationRef == "" {
			continue
		}
		if err := insertCitation(ctx, tx, evt, rec, c.InvestigationRef, CitationKindCase, 0, "", c.Cited); err != nil {
			return err
		}
	}
	return nil
}

// insertCitation writes one consulted entry. The aggregate refuses one
// procedure twice at one revision and cut in one act; a past case has no such
// rule (two tools in one act may both return it), and replay re-applies every
// row. In both cases a conflict keeps the citing fact (bool_or), never loses
// it.
func insertCitation(ctx context.Context, tx *sql.Tx, evt Event, rec InterpretationRecorded, ref, kind string, revision int, cut string, cited bool) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO knowledge_citations
			(ref, kind, aggregate_id, tenant_id, interpretation_id, sequence_no, revision, cut, cited, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (ref, aggregate_id, interpretation_id, revision, cut) DO UPDATE SET
			cited = knowledge_citations.cited OR EXCLUDED.cited
	`, ref, kind, evt.AggregateID, evt.TenantID, rec.InterpretationID, evt.SequenceNo, revision, cut, cited, evt.OccurredAt); err != nil {
		return fmt.Errorf("upsert knowledge_citations: %w", err)
	}
	return nil
}

// Reset clears the projection for replay.
func (KnowledgeCitationProjector) Reset(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `TRUNCATE knowledge_citations`); err != nil {
		return fmt.Errorf("truncate knowledge_citations: %w", err)
	}
	return nil
}

// KnowledgeCitation is one investigation that consulted a ref, collapsed over
// every act in it: Cited when any act cited it (adr/0236: the agent's text
// named it by ref), LastAt the latest such act, Acts how many acts carried it,
// Revisions the distinct SOP revisions those acts had in front of them (empty
// for a case, or for records that named none). Title and status are joined
// from investigation_current.
type KnowledgeCitation struct {
	AggregateID AggregateID
	Title       string
	Status      string
	Cited       bool
	LastAt      time.Time
	Acts        int
	Revisions   []int
}

// ListKnowledgeCitations returns every investigation that consulted a ref,
// one row each, most recently touched first.
func (r *ReadModel) ListKnowledgeCitations(ctx context.Context, ref string) ([]KnowledgeCitation, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT c.aggregate_id, i.title, i.status,
		       bool_or(c.cited), max(c.occurred_at), count(DISTINCT c.interpretation_id),
		       array_remove(array_agg(DISTINCT c.revision ORDER BY c.revision), 0)
		FROM knowledge_citations c
		JOIN investigation_current i ON i.aggregate_id = c.aggregate_id
		WHERE c.ref = $1
		GROUP BY c.aggregate_id, i.title, i.status
		ORDER BY max(c.occurred_at) DESC
	`, ref)
	if err != nil {
		return nil, fmt.Errorf("query knowledge_citations: %w", err)
	}
	defer rows.Close()

	var out []KnowledgeCitation
	for rows.Next() {
		var c KnowledgeCitation
		var revs []int64
		if err := rows.Scan(&c.AggregateID, &c.Title, &c.Status, &c.Cited, &c.LastAt, &c.Acts, pq.Array(&revs)); err != nil {
			return nil, fmt.Errorf("scan knowledge_citations: %w", err)
		}
		c.Revisions = make([]int, 0, len(revs))
		for _, v := range revs {
			c.Revisions = append(c.Revisions, int(v))
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CitationCount is how many distinct investigations consulted one ref:
// CitedIn those with a citing act, RecalledIn those that only recalled it.
type CitationCount struct {
	CitedIn    int
	RecalledIn int
}

// CitationCounts folds the citations of many refs in one query, for the
// library lists ("cited in N investigations"). A ref no act consulted is
// absent from the map.
func (r *ReadModel) CitationCounts(ctx context.Context, refs []string) (map[string]CitationCount, error) {
	out := map[string]CitationCount{}
	if len(refs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT ref,
		       count(DISTINCT aggregate_id) FILTER (WHERE cited),
		       count(DISTINCT aggregate_id)
		FROM knowledge_citations
		WHERE ref = ANY($1)
		GROUP BY ref
	`, pq.Array(refs))
	if err != nil {
		return nil, fmt.Errorf("count knowledge_citations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var cited, total int
		if err := rows.Scan(&ref, &cited, &total); err != nil {
			return nil, fmt.Errorf("scan knowledge_citations counts: %w", err)
		}
		out[ref] = CitationCount{CitedIn: cited, RecalledIn: total - cited}
	}
	return out, rows.Err()
}
