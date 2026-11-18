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
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/module"
	"github.com/sd-strax/reckon/stix"
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
	// Entities are the objects EntityRefs name, as the record holds them, so a
	// reading surface can render the event with its entities (adr/0090).
	Entities []Object
	// UserAccounts maps each user object's place in the event to the strong
	// accounts the normalizer minted for it, the one shown first, from the
	// observations' user_accounts (adr/0398).
	UserAccounts map[string][]string
}

// EventFilter narrows an investigation's events. Entity keeps events whose
// observed-data names the ref or a member of its alias set (adr/0211), a person
// widened to the accounts it owns (adr/0324); Category keeps one OCSF category (auth,
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
// thousands digit of class_uid (3002 Authentication is category 3, IAM). An
// extension's class carries the extension's number above the category
// (201002 is the Windows extension's 1002, category 1), so the filter reads
// the category the way ocsfview.Category does: class_uid % 100000 / 1000.
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
		q.where("(e.class_uid % 100000) / 1000 = " + q.arg(cat))
	} else if f.Category != "" {
		return Events{Rows: []EventRow{}}, nil // an unknown category matches nothing
	}
	if f.Entity != "" {
		// The entity reads with its alias set (adr/0211): an event a source
		// attributed only to a weak host is listed under the host it aliases.
		// A person reads with the accounts it owns, each with its alias set
		// (adr/0324).
		set, err := s.entitySets(ctx, []string{f.Entity})
		if err != nil {
			return Events{}, err
		}
		if len(set) == 0 {
			set = []string{f.Entity} // not a STIX ref: matches what names it, which is nothing
		}
		q.where(fmt.Sprintf(`EXISTS (
			SELECT 1 FROM observed_data_events ode JOIN stix_objects o ON o.id = ode.observed_data_id
			WHERE ode.ocsf_event_id = e.id AND %s)`, observationNamesAny("o.payload", q.arg(textArray(set))+"::text[]")))
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
// of its entities, each widened to its alias set; the anchor itself is
// included. An anchor that is not a
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
		if strings.HasPrefix(r, identity.TypeHost+"--") {
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
	// The anchor's own entities, each with its alias set (adr/0211), a
	// person with its accounts (adr/0324): an event sharing any of them is
	// "around" it.
	shared, err = s.entitySets(ctx, shared)
	if err != nil {
		return Events{}, err
	}
	if len(shared) == 0 {
		return Events{Rows: []EventRow{}}, nil
	}
	q.where(`EXISTS (SELECT 1 FROM observed_data_events ode JOIN stix_objects o ON o.id = ode.observed_data_id WHERE ode.ocsf_event_id = e.id AND ` + observationNamesAny("o.payload", q.arg(textArray(shared))+"::text[]") + `)`)
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
		SELECT DISTINCT ON (e.id) e.id, e.class_uid, e.class_name, e.time, e.recorded_at, e.source_tool, e.payload, c.id AS call_id, c.verb, c.coverage
		FROM ocsf_events e JOIN call_events ce ON ce.ocsf_event_id = e.id JOIN capability_calls c ON c.id = ce.call_id
		WHERE ` + strings.Join(q.preds, " AND ") + `
		ORDER BY e.id, c.started_at, c.id
	) e`
}

const selectEvents = `SELECT e.id, e.class_uid, e.class_name, e.time, e.recorded_at, e.source_tool, e.payload, e.call_id, e.verb, e.coverage FROM `

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
		if err := rows.Scan(&r.ID, &r.ClassUID, &r.ClassName, &r.Time, &r.RecordedAt, &r.SourceTool, &r.Payload, &r.CallID, &r.Verb, &r.Coverage); err != nil {
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
// derived-from join, and the entity objects those refs name, in two queries
// over the page.
func (s *Store) attachDerived(ctx context.Context, rows []EventRow) error {
	if err := s.attachDerivedRefs(ctx, rows); err != nil {
		return err
	}
	return s.attachEntities(ctx, rows)
}

func (s *Store) attachDerivedRefs(ctx context.Context, rows []EventRow) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	index := map[uuid.UUID]int{}
	for i, r := range rows {
		ids = append(ids, r.ID)
		index[r.ID] = i
	}
	// #nosec G202 -- the joined fragments are fixed JSON paths from the stix package, never input.
	res, err := s.db.QueryContext(ctx, `
		SELECT ode.ocsf_event_id, ode.observed_data_id, o.payload->'object_refs', COALESCE(`+stix.ObservationPersonRef.SQL("o.payload")+`, ''),
			`+stix.ObservationUserAccounts.JSONSQL("o.payload")+`
		FROM observed_data_events ode JOIN stix_objects o ON o.id = ode.observed_data_id
		WHERE ode.ocsf_event_id = ANY($1)
		ORDER BY ode.ocsf_event_id, ode.observed_data_id
	`, uuidArray(ids))
	if err != nil {
		return fmt.Errorf("query derived objects: %w", err)
	}
	defer res.Close()
	conflicts := map[rowPlace]bool{}
	for res.Next() {
		var eventID, odID uuid.UUID
		var refsJSON, accountsJSON []byte
		var personRef string
		if err := res.Scan(&eventID, &odID, &refsJSON, &personRef, &accountsJSON); err != nil {
			return fmt.Errorf("scan derived object: %w", err)
		}
		i, ok := index[eventID]
		if !ok {
			continue
		}
		rows[i].ObservedDataRefs = append(rows[i].ObservedDataRefs, "observed-data--"+odID.String())
		addUserAccounts(rows, i, accountsJSON, conflicts)
		var refs []string
		_ = json.Unmarshal(refsJSON, &refs)
		// A directory record names its person in person_ref, which
		// object_refs cannot hold (adr/0324, adr/0357).
		if personRef != "" {
			refs = append(refs, personRef)
		}
		for _, r := range refs {
			// An observation may list a relationship, such as a directory
			// record's belongs-to, or the artifact that stands for a record
			// with nothing to extract (adr/0357). Neither is an entity the
			// event names.
			if capability.IsEntityRef(r) && !contains(rows[i].EntityRefs, r) {
				rows[i].EntityRefs = append(rows[i].EntityRefs, r)
			}
		}
	}
	return res.Err()
}

// rowPlace is one user object's place in one row's event.
type rowPlace struct {
	row   int
	place string
}

// addUserAccounts adds one observation's user_accounts to row i. Two
// observations of one event that name different accounts at one place leave
// that place unlinked, since a missed link is better than a wrong one.
func addUserAccounts(rows []EventRow, i int, raw []byte, conflicts map[rowPlace]bool) {
	var m map[string][]string
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return
	}
	for place, accts := range m {
		k := rowPlace{i, place}
		if conflicts[k] {
			continue
		}
		if rows[i].UserAccounts == nil {
			rows[i].UserAccounts = map[string][]string{}
		}
		if got, ok := rows[i].UserAccounts[place]; ok && !slices.Equal(got, accts) {
			delete(rows[i].UserAccounts, place)
			conflicts[k] = true
			continue
		}
		rows[i].UserAccounts[place] = accts
	}
}

// attachEntities reads the objects the rows' entity refs name, once per
// distinct ref, and hands each row its own in ref order. A ref the object
// store does not hold is left out, never invented.
func (s *Store) attachEntities(ctx context.Context, rows []EventRow) error {
	ids := make([]uuid.UUID, 0, 16)
	seen := map[uuid.UUID]bool{}
	for _, r := range rows {
		for _, ref := range r.EntityRefs {
			if id, ok := refUUID(ref); ok && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	res, err := s.db.QueryContext(ctx, `SELECT id, type, payload FROM stix_objects WHERE id = ANY($1)`, uuidArray(ids))
	if err != nil {
		return fmt.Errorf("query entities: %w", err)
	}
	defer res.Close()
	objects := make(map[uuid.UUID]Object, len(ids))
	for res.Next() {
		var id uuid.UUID
		var o Object
		if err := res.Scan(&id, &o.Type, &o.Payload); err != nil {
			return fmt.Errorf("scan entity: %w", err)
		}
		o.Ref = o.Type + "--" + id.String()
		objects[id] = o
	}
	if err := res.Err(); err != nil {
		return err
	}
	for i := range rows {
		for _, ref := range rows[i].EntityRefs {
			if id, ok := refUUID(ref); ok {
				if o, ok := objects[id]; ok {
					rows[i].Entities = append(rows[i].Entities, o)
				}
			}
		}
	}
	return nil
}

// Derived is what the record derived from one event: the observations made
// from it, the objects those observations name, as the record holds them, and
// the account minted for each user object (EventRow.UserAccounts).
type Derived struct {
	// ObservedDataRefs are the observations made from the event. An event
	// the normalizer could not read has none, and so cannot be cited
	// (adr/0364).
	ObservedDataRefs []string
	Entities         []Object
	UserAccounts     map[string][]string
}

// EventDerived returns what the record derived from one event.
func (s *Store) EventDerived(ctx context.Context, eventID uuid.UUID) (Derived, error) {
	rows := []EventRow{{Event: Event{ID: eventID}}}
	if err := s.attachDerived(ctx, rows); err != nil {
		return Derived{}, err
	}
	return Derived{ObservedDataRefs: rows[0].ObservedDataRefs, Entities: rows[0].Entities, UserAccounts: rows[0].UserAccounts}, nil
}

// ObservationEvents returns the raw events one observation was made from, in
// event time: the path back from a cited observation to its telemetry
// (adr/0364). Each call stores its own records, so one event that several
// calls returned is several records. They are one event under adr/0348's
// rule (sameEventRecords), and the event is named once, by its first
// retrieved record. A ref that is not an observed-data id is ErrMalformedRef;
// an observation with no recorded event returns none.
func (s *Store) ObservationEvents(ctx context.Context, ref string) ([]string, error) {
	typ, _, _ := strings.Cut(ref, "--")
	id, ok := refUUID(ref)
	if !ok || typ != "observed-data" {
		return nil, ErrMalformedRef
	}
	// The store is single-tenant (Persist writes module.SingleTenantUUID);
	// the read is scoped to it as the identity reads are. Each record comes
	// with its call and the observations made from it, which the same-event
	// rule compares.
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.class_uid, e.time, e.recorded_at,
			COALESCE((SELECT min(ce.call_id::text) FROM call_events ce WHERE ce.ocsf_event_id = e.id), e.id::text),
			(SELECT string_agg(o2.observed_data_id::text, ',' ORDER BY o2.observed_data_id::text)
				FROM observed_data_events o2 WHERE o2.ocsf_event_id = e.id)
		FROM observed_data_events ode
		JOIN stix_objects o ON o.id = ode.observed_data_id AND o.tenant_id = $2
		JOIN ocsf_events e ON e.id = ode.ocsf_event_id AND e.tenant_id = $2
		WHERE ode.observed_data_id = $1
		ORDER BY e.time, e.recorded_at, e.id
	`, id, module.SingleTenantUUID)
	if err != nil {
		return nil, fmt.Errorf("query observation events: %w", err)
	}
	defer rows.Close()
	var recs []sameEventRecord
	for rows.Next() {
		var r sameEventRecord
		var eventID uuid.UUID
		var at time.Time
		if err := rows.Scan(&eventID, &r.classUID, &at, &r.recordedAt, &r.call, &r.observations); err != nil {
			return nil, fmt.Errorf("scan observation event: %w", err)
		}
		r.id, r.time = eventID.String(), at
		recs = append(recs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate observation events: %w", err)
	}
	return sameEventRecords(recs), nil
}

// sameEventRecord is one stored record of an event, with what adr/0348's
// same-event rule compares.
type sameEventRecord struct {
	id           string
	classUID     int
	time         time.Time
	recordedAt   time.Time
	call         string
	observations string // the observed-data ids made from it, sorted and joined
}

// sameEventRecords names each event once among records given in event time,
// by its first retrieved record. The rule is adr/0348's, as the workbench's
// timeline applies it (mergeSameEvents): records are the same event when
// their class, their event time and their observations (as a set, non-empty)
// are equal and they came from different calls. Within one call, records stay
// separate, so the event is named as many times as the most copies any one
// call returned.
func sameEventRecords(recs []sameEventRecord) []string {
	type bucket struct {
		byCall map[string][]sameEventRecord
		calls  []string
	}
	var order []string // a bucket key, or "#<id>" for a record that is its own event
	buckets := map[string]*bucket{}
	for _, r := range recs {
		if r.observations == "" {
			order = append(order, "#"+r.id)
			continue
		}
		k := fmt.Sprintf("%d|%s|%s", r.classUID, r.time.UTC().Format(time.RFC3339Nano), r.observations)
		b, ok := buckets[k]
		if !ok {
			b = &bucket{byCall: map[string][]sameEventRecord{}}
			buckets[k] = b
			order = append(order, k)
		}
		if _, seen := b.byCall[r.call]; !seen {
			b.calls = append(b.calls, r.call)
		}
		b.byCall[r.call] = append(b.byCall[r.call], r)
	}
	var out []string
	for _, k := range order {
		if id, own := strings.CutPrefix(k, "#"); own {
			out = append(out, id)
			continue
		}
		b := buckets[k]
		for _, c := range b.calls {
			slices.SortStableFunc(b.byCall[c], func(x, y sameEventRecord) int { return x.recordedAt.Compare(y.recordedAt) })
		}
		// The calls in the order they first retrieved the event.
		slices.SortStableFunc(b.calls, func(x, y string) int { return b.byCall[x][0].recordedAt.Compare(b.byCall[y][0].recordedAt) })
		copies := 0
		for _, c := range b.calls {
			copies = max(copies, len(b.byCall[c]))
		}
		for n := range copies {
			for _, c := range b.calls {
				if n < len(b.byCall[c]) {
					out = append(out, b.byCall[c][n].id)
					break
				}
			}
		}
	}
	return out
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

// observationNamesAny is a Postgres condition that holds when the observation
// in the jsonb column payload names one of the refs in arr, a text array: in
// its object_refs, or as the person a directory record describes in
// reckon-observation's person_ref (adr/0324, adr/0357). Every query that asks
// which records are about an entity uses it.
func observationNamesAny(payload, arr string) string {
	return "(" + payload + "->'object_refs' ?| " + arr + " OR " + stix.ObservationPersonRef.SQL(payload) + " = ANY(" + arr + "))"
}
