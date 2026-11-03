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
	// Bindings is every binding the resolver walked, in priority order.
	// Adapter, Operation, Priority and ParamsSent summarize the first that
	// answered; a fan-out call is read from here.
	Bindings []BindingOutcome
	// Produced is filled by Call, not by Calls: the ids are a second query.
	Produced CallProduced
}

// BindingOutcome is what one binding did on a call, as stored: the parameters
// as rendered (without the verb the resolver injects for the adapter), and
// whether it answered or why it was skipped.
type BindingOutcome struct {
	Adapter   string          `json:"adapter"`
	Operation string          `json:"operation"`
	Priority  int             `json:"priority"`
	Params    json.RawMessage `json:"params,omitempty"`
	Answered  bool            `json:"answered"`
	Skipped   string          `json:"skipped,omitempty"`
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
// Without Around the list is a page in event time: oldest first, or newest
// first when Newest is set, continuing past Cursor when one is given.
type EventFilter struct {
	Entity   string
	Category string
	Around   uuid.UUID
	Limit    int
	Newest   bool
	Cursor   *EventCursor
}

// EventCursor is a position in event time, (time, id), so a page continues
// exactly where the previous one stopped.
type EventCursor struct {
	Time time.Time
	ID   uuid.UUID
}

// Events is a page of an investigation's events with the unpaged count, and
// the cursor of the next page when there is one.
type Events struct {
	Rows       []EventRow
	Total      int
	NextCursor *EventCursor
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
	outcomes := make([]BindingOutcome, 0, len(res.Bindings))
	for _, b := range res.Bindings {
		params, err := sentParams(b.Params)
		if err != nil {
			return err
		}
		outcomes = append(outcomes, BindingOutcome{Adapter: b.Adapter, Operation: b.Operation, Priority: b.Priority, Params: params, Answered: b.Answered, Skipped: b.Skipped})
	}
	bindings, err := json.Marshal(outcomes)
	if err != nil {
		return fmt.Errorf("marshal binding outcomes: %w", err)
	}
	// The summary columns: the first binding that answered.
	var adapter, operation string
	var priority int
	var params []byte
	for _, o := range outcomes {
		if o.Answered {
			adapter, operation, priority, params = o.Adapter, o.Operation, o.Priority, o.Params
			break
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
			id, tenant_id, investigation_id, verb, adapter, operation, priority, params_sent, bindings,
			coverage, bindings_answered, bindings_total, null_observation_ref, degradation_notes,
			started_at, duration_ms
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`, call.ID, tenant, inv, call.Verb, adapter, operation, priority, nullBytes(params), bindings,
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
	q := &eventQuery{}
	q.where("c.investigation_id = " + q.arg(investigationID))
	if cat, ok := categoryBounds[strings.ToLower(f.Category)]; ok {
		q.where(fmt.Sprintf("e.class_uid BETWEEN %s AND %s", q.arg(cat*1000), q.arg(cat*1000+999)))
	} else if f.Category != "" {
		return Events{Rows: []EventRow{}}, nil // an unknown category matches nothing
	}
	if f.Entity != "" {
		q.where(fmt.Sprintf(`EXISTS (
			SELECT 1 FROM observed_data_events ode JOIN stix_objects o ON o.id = ode.observed_data_id
			WHERE ode.ocsf_event_id = e.id AND o.payload->'object_refs' ? %s)`, q.arg(f.Entity)))
	}

	if f.Around != (uuid.UUID{}) {
		return s.eventsAround(ctx, investigationID, q, f.Around, limit)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+q.from(), q.args...).Scan(&total); err != nil {
		return Events{}, fmt.Errorf("count events: %w", err)
	}
	order := "e.time, e.id"
	if f.Newest {
		order = "e.time DESC, e.id DESC"
	}
	if f.Cursor != nil {
		at, id := q.arg(f.Cursor.Time.UTC()), q.arg(f.Cursor.ID)
		if f.Newest {
			q.where(fmt.Sprintf("(e.time < %s OR (e.time = %s AND e.id < %s))", at, at, id))
		} else {
			q.where(fmt.Sprintf("(e.time > %s OR (e.time = %s AND e.id > %s))", at, at, id))
		}
	}
	// One past the page says whether there is a next one.
	rows, err := s.eventRows(ctx, selectEvents+q.from()+" ORDER BY "+order+" LIMIT "+q.arg(limit+1), q.args...)
	if err != nil {
		return Events{}, err
	}
	out := Events{Rows: rows, Total: total}
	if len(rows) > limit {
		out.Rows = rows[:limit]
		last := out.Rows[limit-1]
		out.NextCursor = &EventCursor{Time: last.Time, ID: last.ID}
	}
	return out, nil
}

// eventsAround lists limit events before and after the anchor, within the
// same filter, on the anchor's host entities when it names any, else on any
// of its entities; the anchor itself is included. An anchor that is not a
// member of the investigation is ErrNotFound; one that names no entity has an
// empty window, since "around" means on the same entity. Total is the count
// of the window.
func (s *Store) eventsAround(ctx context.Context, investigationID uuid.UUID, q *eventQuery, anchor uuid.UUID, limit int) (Events, error) {
	var at time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT e.time FROM ocsf_events e JOIN call_events ce ON ce.ocsf_event_id = e.id JOIN capability_calls c ON c.id = ce.call_id
		WHERE e.id = $1 AND c.investigation_id = $2 LIMIT 1`, anchor, investigationID).Scan(&at)
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
	if len(shared) == 0 {
		return Events{Rows: []EventRow{}}, nil
	}
	// The anchor's own entities: an event sharing any of them is "around" it.
	ors := make([]string, 0, len(shared))
	for _, r := range shared {
		ors = append(ors, "o.payload->'object_refs' ? "+q.arg(r))
	}
	q.where(`EXISTS (SELECT 1 FROM observed_data_events ode JOIN stix_objects o ON o.id = ode.observed_data_id WHERE ode.ocsf_event_id = e.id AND (` + strings.Join(ors, " OR ") + `))`)
	atArg, idArg := q.arg(at), q.arg(anchor)
	before := q.clone()
	before.where(fmt.Sprintf("(e.time < %s OR (e.time = %s AND e.id < %s))", atArg, atArg, idArg))
	rowsBefore, err := s.eventRows(ctx, selectEvents+before.from()+" ORDER BY e.time DESC, e.id DESC LIMIT "+before.arg(limit), before.args...)
	if err != nil {
		return Events{}, err
	}
	// The anchor itself is in the after window, so one more than limit.
	after := q.clone()
	after.where(fmt.Sprintf("(e.time > %s OR (e.time = %s AND e.id >= %s))", atArg, atArg, idArg))
	rowsAfter, err := s.eventRows(ctx, selectEvents+after.from()+" ORDER BY e.time, e.id LIMIT "+after.arg(limit+1), after.args...)
	if err != nil {
		return Events{}, err
	}
	out := make([]EventRow, 0, len(rowsBefore)+len(rowsAfter))
	for i := len(rowsBefore) - 1; i >= 0; i-- {
		out = append(out, rowsBefore[i])
	}
	out = append(out, rowsAfter...)
	return Events{Rows: out, Total: len(out)}, nil
}

// eventQuery accumulates the per-event predicates and their arguments of one
// events read. Every predicate is over e (the event) or c (its call), so all
// of them sit inside the membership subquery.
type eventQuery struct {
	preds []string
	args  []any
}

// arg appends an argument and returns its placeholder.
func (q *eventQuery) arg(v any) string {
	q.args = append(q.args, v)
	return fmt.Sprintf("$%d", len(q.args))
}

func (q *eventQuery) where(pred string) { q.preds = append(q.preds, pred) }

func (q *eventQuery) clone() *eventQuery {
	return &eventQuery{preds: slices.Clone(q.preds), args: slices.Clone(q.args)}
}

// from is the membership subquery, aliased e: one row per event, under the
// earliest call that retrieved it for the investigation, so an event
// persisted again by a later call is listed once.
func (q *eventQuery) from() string {
	return `(
		SELECT DISTINCT ON (e.id) e.id, e.class_uid, e.class_name, e.time, e.recorded_at, e.source_tool, c.id AS call_id, c.verb, c.coverage
		FROM ocsf_events e JOIN call_events ce ON ce.ocsf_event_id = e.id JOIN capability_calls c ON c.id = ce.call_id
		WHERE ` + strings.Join(q.preds, " AND ") + `
		ORDER BY e.id, c.started_at, c.id
	) e`
}

const selectEvents = `SELECT e.id, e.class_uid, e.class_name, e.time, e.recorded_at, e.source_tool, e.call_id, e.verb, e.coverage FROM `

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
	SELECT id, investigation_id, verb, adapter, operation, priority, params_sent, bindings, coverage,
	       bindings_answered, bindings_total, null_observation_ref, degradation_notes, started_at, duration_ms
	FROM capability_calls`

type scanner interface{ Scan(dest ...any) error }

func scanCall(row scanner) (CallRecord, error) {
	var c CallRecord
	var inv, nullRef sql.NullString
	var params, bindings, notes []byte
	if err := row.Scan(&c.ID, &inv, &c.Verb, &c.Adapter, &c.Operation, &c.Priority, &params, &bindings, &c.Coverage,
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
	c.Bindings = []BindingOutcome{}
	_ = json.Unmarshal(bindings, &c.Bindings)
	return c, nil
}

// sentParams renders a binding's parameters as sent, without the verb the
// resolver injects for the adapter's benefit (capability.ParamVerb): it is the
// call's verb column, not a parameter anyone wrote. nil params stay null.
func sentParams(params map[string]any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	sent := make(map[string]any, len(params))
	for k, v := range params {
		if k != capability.ParamVerb {
			sent[k] = v
		}
	}
	b, err := json.Marshal(sent)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	return b, nil
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
