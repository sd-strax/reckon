// Package evidence is the store behind citations: the raw OCSF telemetry and
// normalized STIX objects a capability invocation produced, and the reads that
// open them. Every invocation's results are persisted before its refs leave the
// engine (03 §4.13 eager promotion): raw events into ocsf_events (append-only
// ground truth), normalized objects into stix_objects / stix_edges
// (deterministic ids, so re-seeing the same entity is a no-op).
//
// The reasoning nodes that also live in stix_objects are written by the
// aggregate's projector, inside the event transaction; they are read here, not
// written.
package evidence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/module"
)

var (
	// ErrNotFound reports a well-formed ref with nothing persisted behind it.
	ErrNotFound = errors.New("evidence: not found")
	// ErrMalformedRef reports a ref that is not `<type>--<uuid>`.
	ErrMalformedRef = errors.New("evidence: malformed STIX ref")
)

// Store reads and writes evidence.
type Store struct {
	db *sql.DB
}

// NewStore wraps db, the reckon_main database.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Object is one persisted STIX object.
type Object struct {
	Ref     string
	Type    string
	Payload []byte
}

// Event is one persisted raw telemetry record.
type Event struct {
	ID         uuid.UUID
	ClassUID   int
	ClassName  string
	Time       time.Time
	RecordedAt time.Time
	SourceTool string
	Payload    []byte
}

// Neighbourhood is a detection finding's surroundings in the object store.
type Neighbourhood struct {
	Sightings []Object
	Identity  *Object
	Evidence  []Object
}

// Persist writes one invocation's telemetry and objects in a single
// transaction. A failure must fail the invocation: returning refs that were
// never persisted would mint citations that cannot open.
func (s *Store) Persist(ctx context.Context, res capability.CapabilityResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	tenant := module.SingleTenantUUID
	for _, e := range res.Events {
		payload, err := json.Marshal(e.Payload)
		if err != nil {
			return fmt.Errorf("marshal ocsf payload: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ocsf_events (id, tenant_id, class_uid, class_name, time, recorded_at, source_tool, payload)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO NOTHING
		`, e.ID, tenant, e.ClassUID, e.ClassName, e.Time, e.RecordedAt, e.SourceTool, payload); err != nil {
			return fmt.Errorf("insert ocsf event: %w", err)
		}
	}

	stix := func(id string, created time.Time, obj any) error {
		return upsertObject(ctx, tx, tenant, id, created, obj)
	}
	if n := res.NullObservation; n != nil {
		// The absence is a citeable object like any observation (adr/0061).
		if err := stix(string(n.ID), n.ObservedAt, n); err != nil {
			return err
		}
	}
	for _, n := range res.Normalized {
		for _, od := range n.ObservedData {
			if err := stix(string(od.ID), od.FirstObserved, od); err != nil {
				return err
			}
		}
		for _, s := range n.SCOs {
			if err := stix(string(s.ID), time.Time{}, s); err != nil {
				return err
			}
		}
		for _, i := range n.Indicators {
			if err := stix(string(i.ID), i.ValidFrom, i); err != nil {
				return err
			}
		}
		for _, s := range n.Sightings {
			if err := stix(string(s.ID), time.Time{}, s); err != nil {
				return err
			}
		}
		for _, id := range n.Identities {
			if err := stix(string(id.ID), time.Time{}, id); err != nil {
				return err
			}
		}
		for _, rel := range n.Relationships {
			srcUUID, sok := refUUID(string(rel.SourceRef))
			tgtUUID, tok := refUUID(string(rel.TargetRef))
			relUUID, rok := refUUID(string(rel.ID))
			if !sok || !tok || !rok {
				continue
			}
			payload, err := json.Marshal(rel)
			if err != nil {
				return fmt.Errorf("marshal relationship: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO stix_edges (id, tenant_id, source_ref, target_ref, relationship_type, payload, created, modified)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
				ON CONFLICT (id) DO NOTHING
			`, relUUID, tenant, srcUUID, tgtUUID, rel.Type, payload, now()); err != nil {
				return fmt.Errorf("insert stix edge: %w", err)
			}
		}
	}
	return tx.Commit()
}

func upsertObject(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, stixID string, created time.Time, obj any) error {
	id, ok := refUUID(stixID)
	if !ok {
		return fmt.Errorf("normalizer emitted a malformed STIX id %q", stixID)
	}
	typ, _, _ := strings.Cut(stixID, "--")
	payload, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", typ, err)
	}
	if created.IsZero() {
		created = now()
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO stix_objects (id, tenant_id, type, spec_version, payload, created, modified)
		VALUES ($1, $2, $3, '2.1', $4, $5, $5)
		ON CONFLICT (id) DO NOTHING
	`, id, tenant, typ, payload, created); err != nil {
		return fmt.Errorf("insert stix %s: %w", typ, err)
	}
	return nil
}

// Object reads one STIX object by its `<type>--<uuid>` ref.
func (s *Store) Object(ctx context.Context, ref string) (Object, error) {
	id, ok := refUUID(ref)
	if !ok {
		return Object{}, ErrMalformedRef
	}
	o := Object{Ref: ref}
	err := s.db.QueryRowContext(ctx,
		`SELECT type, payload FROM stix_objects WHERE id = $1`, id).Scan(&o.Type, &o.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return Object{}, ErrNotFound
	}
	if err != nil {
		return Object{}, fmt.Errorf("query stix object: %w", err)
	}
	return o, nil
}

// Event reads one raw telemetry record.
func (s *Store) Event(ctx context.Context, id uuid.UUID) (Event, error) {
	e := Event{ID: id}
	err := s.db.QueryRowContext(ctx, `
		SELECT class_uid, class_name, time, recorded_at, source_tool, payload
		FROM ocsf_events WHERE id = $1
	`, id).Scan(&e.ClassUID, &e.ClassName, &e.Time, &e.RecordedAt, &e.SourceTool, &e.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("query ocsf event: %w", err)
	}
	return e, nil
}

// IndicatorNeighbourhood assembles a detection finding's surroundings: every
// sighting whose sighting_of_ref is the indicator, the vendor identity named by
// created_by_ref, and the observed-data each sighting cites. Missing neighbours
// are gaps, never errors: a finding whose vendor identity was not persisted
// still opens.
func (s *Store) IndicatorNeighbourhood(ctx context.Context, indicator Object) (Neighbourhood, error) {
	out := Neighbourhood{Sightings: []Object{}, Evidence: []Object{}}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, payload FROM stix_objects WHERE type = 'sighting' AND payload->>'sighting_of_ref' = $1 ORDER BY created`, indicator.Ref)
	if err != nil {
		return Neighbourhood{}, fmt.Errorf("query sightings: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	var evidenceRefs []string
	for rows.Next() {
		var id uuid.UUID
		var p []byte
		if err := rows.Scan(&id, &p); err != nil {
			return Neighbourhood{}, fmt.Errorf("scan sighting: %w", err)
		}
		out.Sightings = append(out.Sightings, Object{Ref: "sighting--" + id.String(), Type: "sighting", Payload: p})
		var sighting struct {
			ObservedDataRefs []string `json:"observed_data_refs"`
		}
		_ = json.Unmarshal(p, &sighting)
		for _, r := range sighting.ObservedDataRefs {
			if r != "" && !seen[r] {
				seen[r] = true
				evidenceRefs = append(evidenceRefs, r)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return Neighbourhood{}, fmt.Errorf("iterate sightings: %w", err)
	}

	var ind struct {
		CreatedByRef string `json:"created_by_ref"`
	}
	_ = json.Unmarshal(indicator.Payload, &ind)
	if ind.CreatedByRef != "" {
		o, ok, err := s.neighbour(ctx, ind.CreatedByRef)
		if err != nil {
			return Neighbourhood{}, err
		}
		if ok {
			out.Identity = &o
		}
	}
	for _, r := range evidenceRefs {
		o, ok, err := s.neighbour(ctx, r)
		if err != nil {
			return Neighbourhood{}, err
		}
		if ok {
			out.Evidence = append(out.Evidence, o)
		}
	}
	return out, nil
}

// neighbour reads a cited object; ok is false when the ref is malformed or not
// persisted.
func (s *Store) neighbour(ctx context.Context, ref string) (Object, bool, error) {
	o, err := s.Object(ctx, ref)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrMalformedRef) {
		return Object{}, false, nil
	}
	if err != nil {
		return Object{}, false, err
	}
	return o, true, nil
}

// refUUID extracts the uuid of a `<type>--<uuid>` STIX ref.
func refUUID(ref string) (uuid.UUID, bool) {
	_, rest, found := strings.Cut(ref, "--")
	if !found {
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return uuid.UUID{}, false
	}
	return id, true
}

func now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }
