package evidence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
)

// A capability call is an addressable record (adr/0092): one row per
// invocation, every event it returned joined to it, and the investigation it
// was made for on the row. Membership of an event in an investigation is that
// join, recorded at ingest and never inferred. The reads here serve the Story
// timeline, the inspector's "around it" strip and the Call inspector; none of
// them makes a capability call.

// Call is what the invoke path knows about one invocation before and after
// the resolver ran; the binding outcomes and the refs come from the result.
type Call struct {
	ID uuid.UUID
	// InvestigationID is nil for a call made outside an investigation: it keeps
	// its id but is listed under none.
	InvestigationID *uuid.UUID
	Verb            string
	StartedAt       time.Time
	Duration        time.Duration
}

// CallRecord is one persisted call with what it produced.
type CallRecord struct {
	ID               uuid.UUID
	InvestigationID  *uuid.UUID
	Verb             string
	Adapter          string
	Operation        string
	Priority         int
	ParamsSent       json.RawMessage
	Coverage         string
	BindingsAnswered int
	BindingsTotal    int
	NullObservation  string
	DegradationNotes []string
	StartedAt        time.Time
	DurationMS       int
	// Produced is filled by Call, not by Calls: the ids are a second query.
	Produced CallProduced
}

// CallProduced is the evidence a call left behind.
type CallProduced struct {
	EventIDs         []uuid.UUID
	ObservedDataRefs []string
	EntityRefs       []string
}

// EventRow is one retrieved event as the investigation reads list it: the
// record without its payload (the evidence endpoint serves that), the call
// that retrieved it, and the interpretation-layer refs derived from it.
type EventRow struct {
	Event
	CallID           uuid.UUID
	Verb             string
	Coverage         string
	ObservedDataRefs []string
	EntityRefs       []string
}

// EventFilter narrows an investigation's events. Entity keeps events whose
// observed-data names the ref; Category keeps one OCSF category (auth,
// process, network, detection); Around centres the list on one event: Limit
// events before and after it on the same host, else on the same entities.
type EventFilter struct {
	Entity   string
	Category string
	Around   uuid.UUID
	Limit    int
}

// Events is a page of an investigation's events with the unpaged count.
type Events struct {
	Rows  []EventRow
	Total int
}

// DefaultEventLimit is how many events a list returns when the caller names
// no limit, and how many fall on each side of an anchor.
const DefaultEventLimit = 200

// MaxEventLimit caps a caller's limit.
const MaxEventLimit = 1000

// categoryBounds maps a category word to its OCSF category_uid, which is the
// thousands digit of class_uid (3002 Authentication is category 3, IAM).
var categoryBounds = map[string]int{"process": 1, "detection": 2, "auth": 3, "network": 4}

// persistCallTx writes the call row and the joins from the call to its events
// and from each observed-data to the event it was normalized from, inside tx.
func persistCallTx(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, call Call, res capability.CapabilityResult) error {
	if call.ID == (uuid.UUID{}) {
		return errors.New("evidence: a call needs an id")
	}
	var answered *capability.BindingOutcome
	if a := res.Answered(); len(a) > 0 {
		answered = &a[0]
	}
	var adapter, operation string
	var priority int
	var params []byte
	if answered != nil {
		adapter, operation, priority = answered.Adapter, answered.Operation, answered.Priority
		var err error
		if params, err = json.Marshal(answered.Params); err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
	}
	notes, err := json.Marshal(nonNil(res.DegradationNotes))
	if err != nil {
		return fmt.Errorf("marshal degradation notes: %w", err)
	}
	var nullRef sql.NullString
	if res.NullObservation != nil {
		nullRef = sql.NullString{String: string(res.NullObservation.ID), Valid: true}
	}
	var inv sql.NullString
	if call.InvestigationID != nil {
		inv = sql.NullString{String: call.InvestigationID.String(), Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO capability_calls (
			id, tenant_id, investigation_id, verb, adapter, operation, priority, params_sent,
			coverage, bindings_answered, bindings_total, null_observation_ref, degradation_notes,
			started_at, duration_ms
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`, call.ID, tenant, inv, call.Verb, adapter, operation, priority, nullBytes(params),
		string(res.Coverage), len(res.Answered()), res.Applicable(), nullRef, notes,
		call.StartedAt.UTC(), call.Duration.Milliseconds()); err != nil {
		return fmt.Errorf("insert capability call: %w", err)
	}
	for _, e := range res.Events {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO call_events (call_id, ocsf_event_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, call.ID, e.ID); err != nil {
			return fmt.Errorf("insert call event: %w", err)
		}
	}
	for _, n := range res.Normalized {
		if n.EventID == (uuid.UUID{}) {
			continue
		}
		for _, od := range n.ObservedData {
			odID, ok := refUUID(string(od.ID))
			if !ok {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO observed_data_events (observed_data_id, ocsf_event_id) VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, odID, n.EventID); err != nil {
				return fmt.Errorf("insert observed-data event: %w", err)
			}
		}
	}
	return nil
}

// InvestigationEvents lists the events retrieved for one investigation, in
// event time, narrowed by f. Payloads are not read.
func (s *Store) InvestigationEvents(ctx context.Context, investigationID uuid.UUID, f EventFilter) (Events, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultEventLimit
	}
	if limit > MaxEventLimit {
		limit = MaxEventLimit
	}
	where := []string{"c.investigation_id = $1"}
	args := []any{investigationID}
	if cat, ok := categoryBounds[strings.ToLower(f.Category)]; ok {
		args = append(args, cat*1000, cat*1000+999)
		where = append(where, fmt.Sprintf("e.class_uid BETWEEN $%d AND $%d", len(args)-1, len(args)))
	} else if f.Category != "" {
		return Events{Rows: []EventRow{}}, nil // an unknown category matches nothing
	}
	if f.Entity != "" {
		args = append(args, f.Entity)
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM observed_data_events ode JOIN stix_objects o ON o.id = ode.observed_data_id
			WHERE ode.ocsf_event_id = e.id AND o.payload->'object_refs' ? $%d)`, len(args)))
	}
	from := `FROM ocsf_events e JOIN call_events ce ON ce.ocsf_event_id = e.id JOIN capability_calls c ON c.id = ce.call_id WHERE ` + strings.Join(where, " AND ")

	if f.Around != (uuid.UUID{}) {
		return s.eventsAround(ctx, from, args, f.Around, limit)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) `+from, args...).Scan(&total); err != nil {
		return Events{}, fmt.Errorf("count events: %w", err)
	}
	args = append(args, limit)
	rows, err := s.eventRows(ctx, selectEvents+from+fmt.Sprintf(" ORDER BY e.time, e.id LIMIT $%d", len(args)), args...)
	if err != nil {
		return Events{}, err
	}
	return Events{Rows: rows, Total: total}, nil
}

// eventsAround lists limit events before and after the anchor, within the
// same filter, on the anchor's host entities when it names any, else on any
// of its entities, else unrestricted; the anchor itself is included. Total is
// the count of the window.
func (s *Store) eventsAround(ctx context.Context, from string, args []any, anchor uuid.UUID, limit int) (Events, error) {
	var at time.Time
	err := s.db.QueryRowContext(ctx, `SELECT time FROM ocsf_events WHERE id = $1`, anchor).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return Events{}, ErrNotFound
	}
	if err != nil {
		return Events{}, fmt.Errorf("query anchor: %w", err)
	}
	refs, err := s.eventEntityRefs(ctx, anchor)
	if err != nil {
		return Events{}, err
	}
	var hosts []string
	for _, r := range refs {
		if strings.HasPrefix(r, "x-host--") {
			hosts = append(hosts, r)
		}
	}
	shared := hosts
	if len(shared) == 0 {
		shared = refs
	}
	if len(shared) > 0 {
		// The anchor's own entities: an event sharing any of them is "around" it.
		var ors []string
		for _, r := range shared {
			args = append(args, r)
			ors = append(ors, fmt.Sprintf("o.payload->'object_refs' ? $%d", len(args)))
		}
		from += ` AND EXISTS (SELECT 1 FROM observed_data_events ode JOIN stix_objects o ON o.id = ode.observed_data_id WHERE ode.ocsf_event_id = e.id AND (` + strings.Join(ors, " OR ") + `))`
	}
	args = append(args, at, anchor)
	n := len(args) + 1 // the limit, last in both queries
	withLimit := func(l int) []any { return append(slices.Clone(args), l) }
	before, err := s.eventRows(ctx, selectEvents+from+fmt.Sprintf(" AND (e.time < $%d OR (e.time = $%d AND e.id < $%d)) ORDER BY e.time DESC, e.id DESC LIMIT $%d", n-2, n-2, n-1, n), withLimit(limit)...)
	if err != nil {
		return Events{}, err
	}
	// The anchor itself is in the after window, so one more than limit.
	after, err := s.eventRows(ctx, selectEvents+from+fmt.Sprintf(" AND (e.time > $%d OR (e.time = $%d AND e.id >= $%d)) ORDER BY e.time, e.id LIMIT $%d", n-2, n-2, n-1, n), withLimit(limit+1)...)
	if err != nil {
		return Events{}, err
	}
	out := make([]EventRow, 0, len(before)+len(after))
	for i := len(before) - 1; i >= 0; i-- {
		out = append(out, before[i])
	}
	out = append(out, after...)
	return Events{Rows: out, Total: len(out)}, nil
}

const selectEvents = `SELECT e.id, e.class_uid, e.class_name, e.time, e.recorded_at, e.source_tool, c.id, c.verb, c.coverage `

// eventRows runs a selectEvents query and attaches each row's derived refs.
func (s *Store) eventRows(ctx context.Context, query string, args ...any) ([]EventRow, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	out := []EventRow{}
	for rows.Next() {
		var r EventRow
		if err := rows.Scan(&r.ID, &r.ClassUID, &r.ClassName, &r.Time, &r.RecordedAt, &r.SourceTool, &r.CallID, &r.Verb, &r.Coverage); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		r.ObservedDataRefs, r.EntityRefs = []string{}, []string{}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	if err := s.attachDerived(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// attachDerived fills each row's observed-data and entity refs from the
// derived-from join, in one query over the page.
func (s *Store) attachDerived(ctx context.Context, rows []EventRow) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	index := map[uuid.UUID]int{}
	for i, r := range rows {
		ids = append(ids, r.ID)
		index[r.ID] = i
	}
	res, err := s.db.QueryContext(ctx, `
		SELECT ode.ocsf_event_id, ode.observed_data_id, o.payload->'object_refs'
		FROM observed_data_events ode JOIN stix_objects o ON o.id = ode.observed_data_id
		WHERE ode.ocsf_event_id = ANY($1)
		ORDER BY ode.ocsf_event_id, ode.observed_data_id
	`, uuidArray(ids))
	if err != nil {
		return fmt.Errorf("query derived objects: %w", err)
	}
	defer res.Close()
	for res.Next() {
		var eventID, odID uuid.UUID
		var refsJSON []byte
		if err := res.Scan(&eventID, &odID, &refsJSON); err != nil {
			return fmt.Errorf("scan derived object: %w", err)
		}
		i, ok := index[eventID]
		if !ok {
			continue
		}
		rows[i].ObservedDataRefs = append(rows[i].ObservedDataRefs, "observed-data--"+odID.String())
		var refs []string
		_ = json.Unmarshal(refsJSON, &refs)
		for _, r := range refs {
			if r != "" && !contains(rows[i].EntityRefs, r) {
				rows[i].EntityRefs = append(rows[i].EntityRefs, r)
			}
		}
	}
	return res.Err()
}

// eventEntityRefs returns the entity refs one event's observed-data name.
func (s *Store) eventEntityRefs(ctx context.Context, eventID uuid.UUID) ([]string, error) {
	rows := []EventRow{{Event: Event{ID: eventID}}}
	if err := s.attachDerived(ctx, rows); err != nil {
		return nil, err
	}
	return rows[0].EntityRefs, nil
}

// Calls lists an investigation's capability calls, oldest first.
func (s *Store) Calls(ctx context.Context, investigationID uuid.UUID) ([]CallRecord, error) {
	rows, err := s.db.QueryContext(ctx, selectCalls+` WHERE investigation_id = $1 ORDER BY started_at, id`, investigationID)
	if err != nil {
		return nil, fmt.Errorf("query capability calls: %w", err)
	}
	defer rows.Close()
	out := []CallRecord{}
	for rows.Next() {
		c, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Call reads one call of an investigation with what it produced. A call id
// that exists under another investigation, or none, is ErrNotFound.
func (s *Store) Call(ctx context.Context, investigationID, callID uuid.UUID) (CallRecord, error) {
	row := s.db.QueryRowContext(ctx, selectCalls+` WHERE id = $1 AND investigation_id = $2`, callID, investigationID)
	c, err := scanCall(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CallRecord{}, ErrNotFound
	}
	if err != nil {
		return CallRecord{}, err
	}
	events, err := s.db.QueryContext(ctx, `
		SELECT ce.ocsf_event_id FROM call_events ce JOIN ocsf_events e ON e.id = ce.ocsf_event_id
		WHERE ce.call_id = $1 ORDER BY e.time, e.id`, callID)
	if err != nil {
		return CallRecord{}, fmt.Errorf("query call events: %w", err)
	}
	defer events.Close()
	c.Produced = CallProduced{EventIDs: []uuid.UUID{}, ObservedDataRefs: []string{}, EntityRefs: []string{}}
	for events.Next() {
		var id uuid.UUID
		if err := events.Scan(&id); err != nil {
			return CallRecord{}, fmt.Errorf("scan call event: %w", err)
		}
		c.Produced.EventIDs = append(c.Produced.EventIDs, id)
	}
	if err := events.Err(); err != nil {
		return CallRecord{}, fmt.Errorf("iterate call events: %w", err)
	}
	rows := make([]EventRow, 0, len(c.Produced.EventIDs))
	for _, id := range c.Produced.EventIDs {
		rows = append(rows, EventRow{Event: Event{ID: id}})
	}
	if err := s.attachDerived(ctx, rows); err != nil {
		return CallRecord{}, err
	}
	for _, r := range rows {
		for _, od := range r.ObservedDataRefs {
			if !contains(c.Produced.ObservedDataRefs, od) {
				c.Produced.ObservedDataRefs = append(c.Produced.ObservedDataRefs, od)
			}
		}
		for _, e := range r.EntityRefs {
			if !contains(c.Produced.EntityRefs, e) {
				c.Produced.EntityRefs = append(c.Produced.EntityRefs, e)
			}
		}
	}
	return c, nil
}

const selectCalls = `
	SELECT id, investigation_id, verb, adapter, operation, priority, params_sent, coverage,
	       bindings_answered, bindings_total, null_observation_ref, degradation_notes, started_at, duration_ms
	FROM capability_calls`

type scanner interface{ Scan(dest ...any) error }

func scanCall(row scanner) (CallRecord, error) {
	var c CallRecord
	var inv, nullRef sql.NullString
	var params, notes []byte
	if err := row.Scan(&c.ID, &inv, &c.Verb, &c.Adapter, &c.Operation, &c.Priority, &params, &c.Coverage,
		&c.BindingsAnswered, &c.BindingsTotal, &nullRef, &notes, &c.StartedAt, &c.DurationMS); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CallRecord{}, err
		}
		return CallRecord{}, fmt.Errorf("scan capability call: %w", err)
	}
	if inv.Valid {
		if id, err := uuid.Parse(inv.String); err == nil {
			c.InvestigationID = &id
		}
	}
	c.NullObservation = nullRef.String
	c.ParamsSent = params
	c.DegradationNotes = []string{}
	_ = json.Unmarshal(notes, &c.DegradationNotes)
	return c, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// uuidArray renders ids as a Postgres uuid[] literal for ANY($1).
func uuidArray(ids []uuid.UUID) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id.String())
	}
	return "{" + strings.Join(parts, ",") + "}"
}
