package aggregate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ErrNoRecord reports that a read found nothing to return. ErrNotFound is the
// write side's: a command addressed to an investigation that does not exist.
var ErrNoRecord = errors.New("aggregate: no such record")

// ReadModel is every read of the aggregate's tables: the projections, the
// events, and the side stores. Callers outside this package hold a ReadModel,
// never the database (adr/0218).
type ReadModel struct {
	db    *sql.DB
	store *Store
}

// notFound turns sql.ErrNoRows into ErrNoRecord and leaves other errors alone.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoRecord
	}
	return err
}

// LoadStream returns one aggregate's events in sequence order.
func (r *ReadModel) LoadStream(ctx context.Context, aggID AggregateID) ([]Event, error) {
	return r.store.LoadStream(ctx, aggID)
}

// InvestigationRow is one investigation in the list, with the counts the
// queue orders and badges by.
type InvestigationRow struct {
	AggregateID       uuid.UUID
	Title             string
	Status            string
	LastEventSequence int64
	SeedSummary       string
	UpdatedAt         time.Time
	PendingActions    int
	NearestExpiry     *time.Time
	HasVerdict        bool
	OpenActions       int
	OpenNotifications int
	Seed              *Seed
}

// ListInvestigations returns the 200 most recently updated investigations. The
// pending counts feed the tree's needs-me-first ordering (ui/06): REQUESTED and
// PENDING_SECONDARY are the statuses still awaiting a human.
func (r *ReadModel) ListInvestigations(ctx context.Context) ([]InvestigationRow, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT i.aggregate_id, i.title, i.status, i.last_event_sequence,
		        COALESCE(i.seed_summary, ''), i.updated_at,
		        (SELECT COUNT(*) FROM action_current a
		          WHERE a.aggregate_id = i.aggregate_id
		            AND a.status IN ('REQUESTED', 'PENDING_SECONDARY')),
		        (SELECT MIN(a.expires_at) FROM action_current a
		          WHERE a.aggregate_id = i.aggregate_id
		            AND a.status IN ('REQUESTED', 'PENDING_SECONDARY')),
		        COALESCE(i.verdict_disposition, '') <> '',
		        (SELECT COUNT(*) FROM action_current a
		          WHERE a.aggregate_id = i.aggregate_id
		            AND a.action_type NOT LIKE 'notify.%'
		            AND a.status NOT IN ('SUCCEEDED', 'PARTIAL', 'FAILED', 'TIMEOUT', 'REJECTED', 'EXPIRED', 'REVERSED')),
		        (SELECT COUNT(*) FROM action_current a
		          WHERE a.aggregate_id = i.aggregate_id
		            AND a.action_type LIKE 'notify.%'
		            AND a.status NOT IN ('SUCCEEDED', 'PARTIAL', 'FAILED', 'TIMEOUT', 'REJECTED', 'EXPIRED', 'REVERSED')),
		        i.seed
		 FROM investigation_current i
		 ORDER BY i.updated_at DESC LIMIT 200`)
	if err != nil {
		return nil, fmt.Errorf("query investigations: %w", err)
	}
	defer rows.Close()

	var out []InvestigationRow
	for rows.Next() {
		var v InvestigationRow
		var nearest sql.NullTime
		var seedRaw []byte
		if err := rows.Scan(&v.AggregateID, &v.Title, &v.Status, &v.LastEventSequence, &v.SeedSummary,
			&v.UpdatedAt, &v.PendingActions, &nearest, &v.HasVerdict, &v.OpenActions, &v.OpenNotifications, &seedRaw); err != nil {
			return nil, fmt.Errorf("scan investigation: %w", err)
		}
		if len(seedRaw) > 0 {
			var seed Seed
			if json.Unmarshal(seedRaw, &seed) == nil && seed.Type != "" {
				v.Seed = &seed
			}
		}
		if nearest.Valid {
			t := nearest.Time
			v.NearestExpiry = &t
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// InvestigationOpening returns when an investigation was opened and by whom:
// its creating act, sequence 1.
func (r *ReadModel) InvestigationOpening(ctx context.Context, aggID AggregateID) (time.Time, Actor, error) {
	var at time.Time
	var actorRaw []byte
	err := r.db.QueryRowContext(ctx,
		`SELECT occurred_at, actor FROM events WHERE aggregate_id = $1 AND sequence_no = 1`, aggID).Scan(&at, &actorRaw)
	if err != nil {
		return time.Time{}, Actor{}, notFound(err)
	}
	var actor Actor
	if err := json.Unmarshal(actorRaw, &actor); err != nil {
		return time.Time{}, Actor{}, fmt.Errorf("decode actor: %w", err)
	}
	return at, actor, nil
}

// CountInvestigations returns how many investigations exist.
func (r *ReadModel) CountInvestigations(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM investigation_current`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count investigations: %w", err)
	}
	return n, nil
}

// CaseInvestigation is an investigation rooted on a case.
type CaseInvestigation struct {
	ID     string
	Title  string
	Status string
}

// InvestigationsForCases returns, for each case id, the investigation that
// answers it. Every investigation of the case counts (#158): a concluded one
// still answers it, since reopening it is the way back. An open one wins over a
// concluded one, newest first within each. A discarded draft does not count.
func (r *ReadModel) InvestigationsForCases(ctx context.Context, caseIDs []string) (map[string]CaseInvestigation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT aggregate_id, title, status, seed->>'case_id'
		   FROM investigation_current
		  WHERE seed_type = $1
		    AND seed->>'case_id' = ANY($2)
		    AND status <> $5
		  ORDER BY (status IN ($3, $4)), updated_at DESC`,
		SeedCase, pq.Array(caseIDs), StatusConcluded, StatusArchived, StatusDiscarded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]CaseInvestigation{}
	for rows.Next() {
		var ref CaseInvestigation
		var caseID string
		if err := rows.Scan(&ref.ID, &ref.Title, &ref.Status, &caseID); err != nil {
			return nil, err
		}
		if _, seen := out[caseID]; !seen {
			out[caseID] = ref
		}
	}
	return out, rows.Err()
}

// InvestigationForCase returns the investigation already rooted on a case
// (#158, adr/0161), or ErrNoRecord.
func (r *ReadModel) InvestigationForCase(ctx context.Context, caseID string) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx,
		`SELECT aggregate_id FROM investigation_current
		  WHERE seed_type = $1 AND seed->>'case_id' = $2 AND status <> $5
		  ORDER BY (status IN ($3, $4)), updated_at DESC
		  LIMIT 1`,
		SeedCase, caseID, StatusConcluded, StatusArchived, StatusDiscarded).Scan(&id)
	return id, notFound(err)
}

// ThreadAct is one reasoning act in an investigation's thread.
type ThreadAct struct {
	SequenceNo     int64
	OccurredAt     time.Time
	Actor          Actor
	Interpretation InterpretationRecorded
	// Superseded reports that a later interpretation.superseded retracted the act.
	Superseded bool
}

// Thread returns an investigation's reasoning acts in sequence order, the
// aggregate's authoritative order. Every domain transition pairs an
// interpretation in the same transaction (01, 02 §3), so nothing that changed
// the investigation is missing.
func (r *ReadModel) Thread(ctx context.Context, aggID AggregateID) ([]ThreadAct, error) {
	superseded, err := r.supersededIDs(ctx, aggID)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT sequence_no, occurred_at, actor, payload, event_version
		 FROM events
		 WHERE aggregate_id = $1 AND event_type = $2
		 ORDER BY sequence_no`,
		aggID, EventTypeInterpretationRecorded)
	if err != nil {
		return nil, fmt.Errorf("query thread: %w", err)
	}
	defer rows.Close()

	var out []ThreadAct
	for rows.Next() {
		var a ThreadAct
		var actorRaw, payloadRaw []byte
		var version int
		if err := rows.Scan(&a.SequenceNo, &a.OccurredAt, &actorRaw, &payloadRaw, &version); err != nil {
			return nil, fmt.Errorf("scan thread: %w", err)
		}
		if err := CheckEventVersion(EventTypeInterpretationRecorded, version); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(actorRaw, &a.Actor); err != nil {
			return nil, fmt.Errorf("decode actor: %w", err)
		}
		if err := json.Unmarshal(payloadRaw, &a.Interpretation); err != nil {
			return nil, fmt.Errorf("decode interpretation: %w", err)
		}
		a.Superseded = superseded[a.Interpretation.InterpretationID]
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread: %w", err)
	}
	return out, nil
}

func (r *ReadModel) supersededIDs(ctx context.Context, aggID AggregateID) (map[uuid.UUID]bool, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT payload, event_version FROM events
		 WHERE aggregate_id = $1 AND event_type = $2`,
		aggID, EventTypeInterpretationSuperseded)
	if err != nil {
		return nil, fmt.Errorf("query supersessions: %w", err)
	}
	defer rows.Close()

	out := map[uuid.UUID]bool{}
	for rows.Next() {
		var payloadRaw []byte
		var version int
		if err := rows.Scan(&payloadRaw, &version); err != nil {
			return nil, fmt.Errorf("scan supersession: %w", err)
		}
		if err := CheckEventVersion(EventTypeInterpretationSuperseded, version); err != nil {
			return nil, err
		}
		var p InterpretationSupersededPayload
		if err := json.Unmarshal(payloadRaw, &p); err != nil {
			return nil, fmt.Errorf("decode supersession: %w", err)
		}
		out[p.SupersededID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate supersessions: %w", err)
	}
	return out, nil
}

// LoadInterpretation returns one recorded interpretation, or ErrNoRecord.
func (r *ReadModel) LoadInterpretation(ctx context.Context, id uuid.UUID) (InterpretationRecorded, error) {
	var payload []byte
	var version int
	err := r.db.QueryRowContext(ctx, `
		SELECT payload, event_version FROM events
		WHERE event_type = $1 AND payload->>'interpretation_id' = $2
		LIMIT 1
	`, EventTypeInterpretationRecorded, id.String()).Scan(&payload, &version)
	if err != nil {
		return InterpretationRecorded{}, notFound(err)
	}
	if err := CheckEventVersion(EventTypeInterpretationRecorded, version); err != nil {
		return InterpretationRecorded{}, err
	}
	var rec InterpretationRecorded
	if err := json.Unmarshal(payload, &rec); err != nil {
		return InterpretationRecorded{}, fmt.Errorf("decode interpretation: %w", err)
	}
	return rec, nil
}

// TranscriptBody returns a committed transcript by content hash, or ErrNoRecord.
func (r *ReadModel) TranscriptBody(ctx context.Context, tenantID uuid.UUID, hash string) ([]byte, error) {
	var body []byte
	err := r.db.QueryRowContext(ctx,
		`SELECT body FROM ai_transcripts WHERE tenant_id = $1 AND hash = $2`, tenantID, hash).Scan(&body)
	return body, notFound(err)
}

// ToolCallRow is one recorded tool dispatch.
type ToolCallRow struct {
	Args           []byte
	TranscriptHash string
}

// ListToolCalls returns an investigation's tool calls, oldest first.
func (r *ReadModel) ListToolCalls(ctx context.Context, aggID AggregateID) ([]ToolCallRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT tool_args, COALESCE(transcript_hash, '') FROM ai_tool_calls
		WHERE investigation_id = $1 ORDER BY occurred_at, id
	`, aggID)
	if err != nil {
		return nil, fmt.Errorf("query ai_tool_calls: %w", err)
	}
	defer rows.Close()

	var out []ToolCallRow
	for rows.Next() {
		var c ToolCallRow
		if err := rows.Scan(&c.Args, &c.TranscriptHash); err != nil {
			return nil, fmt.Errorf("scan tool call: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
