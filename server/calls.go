package server

// Capability calls as addressable records, and the events of one
// investigation (adr/0092; binding gaps 13 and 20). A call is one invocation
// of a verb for an investigation, written by the invoke endpoint in the same
// transaction as the telemetry it returned; the events endpoint is a read
// over those persisted tables, never a capability call.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/capability/ocsfview"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/module"
)

// callRecord is what the invoke endpoint knows about a call beyond the
// resolver's result: its minted id, the investigation it serves (nil for an
// ad-hoc call), and the verb.
type callRecord struct {
	ID          uuid.UUID
	AggregateID *uuid.UUID
	Verb        string
}

// persistCall writes the call row and its event joins inside the invoke
// transaction (adr/0092): capability_calls, call_events for every event the
// call returned, observed_data_events for every derived-from edge the
// normalizer produced.
func persistCall(ctx context.Context, tx *sql.Tx, call callRecord, res capability.CapabilityResult) error {
	var answered *capability.BindingAttempt
	for i := range res.Attempts {
		if res.Attempts[i].Answered {
			answered = &res.Attempts[i]
			break
		}
	}
	var binding, adapter, operation sql.NullString
	var priority sql.NullInt64
	var params []byte
	if answered != nil {
		binding, adapter, operation = sql.NullString{String: answered.Adapter, Valid: true}, sql.NullString{String: answered.Adapter, Valid: true}, sql.NullString{String: answered.Operation, Valid: true}
		priority = sql.NullInt64{Int64: int64(answered.Priority), Valid: true}
		if answered.Params != nil {
			var err error
			if params, err = json.Marshal(answered.Params); err != nil {
				return fmt.Errorf("marshal call params: %w", err)
			}
		}
	}
	notes, err := json.Marshal(res.DegradationNotes)
	if err != nil {
		return fmt.Errorf("marshal degradation notes: %w", err)
	}
	attempts, err := json.Marshal(res.Attempts)
	if err != nil {
		return fmt.Errorf("marshal attempts: %w", err)
	}
	var nullRef sql.NullString
	if res.NullObservation != nil {
		nullRef = sql.NullString{String: string(res.NullObservation.ID), Valid: true}
	}
	var aggregateID any
	if call.AggregateID != nil {
		aggregateID = *call.AggregateID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO capability_calls (
			id, tenant_id, aggregate_id, verb, binding, adapter, operation, priority, params_sent,
			coverage, bindings_answered, bindings_total, null_observation_ref, started_at, duration_ms,
			degradation_notes, attempts
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`, call.ID, module.SingleTenantUUID, aggregateID, call.Verb, binding, adapter, operation, priority, nullJSON(params),
		string(res.Coverage), res.BindingsAnswered, res.BindingsTotal, nullRef, res.StartedAt, res.Duration.Milliseconds(),
		notes, attempts); err != nil {
		return fmt.Errorf("insert capability_calls: %w", err)
	}
	for _, e := range res.Events {
		if _, err := tx.ExecContext(ctx, `INSERT INTO call_events (call_id, ocsf_event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, call.ID, e.ID); err != nil {
			return fmt.Errorf("insert call_events: %w", err)
		}
	}
	for _, n := range res.Normalized {
		for _, edge := range n.Edges {
			if edge.Type != capability.EdgeDerivedFrom {
				continue
			}
			odUUID, ok := stixRefUUID(string(edge.SourceRef))
			evUUID, err := uuid.Parse(edge.OcsfEventID)
			if !ok || err != nil {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO observed_data_events (observed_data_id, ocsf_event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, odUUID, evUUID); err != nil {
				return fmt.Errorf("insert observed_data_events: %w", err)
			}
		}
	}
	return nil
}

func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// EventRow is one persisted event of an investigation as the events endpoint
// lists it (adr/0092): the record's facts, the call that retrieved it, its
// rendering, and its interpretation-layer joins. Payloads are not included;
// the evidence endpoint serves them.
type EventRow struct {
	ID         string              `json:"id"`
	ClassUID   int                 `json:"class_uid"`
	ClassName  string              `json:"class_name"`
	Time       time.Time           `json:"time"`
	RecordedAt time.Time           `json:"recorded_at"`
	SourceTool string              `json:"source_tool"`
	CallID     string              `json:"call_id"`
	Verb       string              `json:"verb"`
	Coverage   string              `json:"coverage"`
	View       *ocsfview.EventView `json:"view,omitempty"`
	// Pattern is the class\'s pattern key (adr/0095): the fields a baseline
	// groups by. Nil for a class with no key.
	Pattern          map[string]string   `json:"pattern,omitempty"`
	ObservedDataRefs []string            `json:"observed_data_refs"`
	EntityRefs       []string            `json:"entity_refs"`
	Relationships    []EventRelationship `json:"relationships"`
}

// EventRelationship is a normalizer relationship among an event's entities.
type EventRelationship struct {
	Type      string `json:"type"`
	SourceRef string `json:"source_ref"`
	TargetRef string `json:"target_ref"`
}

// EventsResponse is the events listing; Total counts before the limit.
type EventsResponse struct {
	Events []EventRow `json:"events"`
	Total  int        `json:"total"`
}

// CallView is a capability call as listed (adr/0092).
type CallView struct {
	ID                 string    `json:"id"`
	Verb               string    `json:"verb"`
	Binding            string    `json:"binding,omitempty"`
	Adapter            string    `json:"adapter,omitempty"`
	Operation          string    `json:"operation,omitempty"`
	Priority           *int      `json:"priority,omitempty"`
	Coverage           string    `json:"coverage"`
	BindingsAnswered   int       `json:"bindings_answered"`
	BindingsTotal      int       `json:"bindings_total"`
	NullObservationRef string    `json:"null_observation_ref,omitempty"`
	StartedAt          time.Time `json:"started_at"`
	DurationMs         int       `json:"duration_ms"`
	EventCount         int       `json:"event_count"`
}

// CallDetail is one call whole: the view plus what was sent, what came back,
// what it produced, and the verb's other bindings from the live resolver.
type CallDetail struct {
	CallView
	ParamsSent       json.RawMessage             `json:"params_sent,omitempty"`
	DegradationNotes []string                    `json:"degradation_notes"`
	Attempts         []capability.BindingAttempt `json:"attempts"`
	Produced         CallProduced                `json:"produced"`
	OtherBindings    []capability.BindingStatus  `json:"other_bindings"`
	// ClosableBy names installed-but-disabled adapters that could serve the
	// verb (the enablement view's word), when the tenant config is readable.
	ClosableBy []string `json:"closable_by,omitempty"`
}

// CallProduced is what a call put into the record.
type CallProduced struct {
	OcsfEventRefs      []string `json:"ocsf_event_refs"`
	ObservedDataRefs   []string `json:"observed_data_refs"`
	EntityRefs         []string `json:"entity_refs"`
	NullObservationRef string   `json:"null_observation_ref,omitempty"`
	// Events are the produced events as rows, grouped by the client (the
	// bundle view), each with its view.
	Events []EventRow `json:"events"`
}

// investigationCallsRoute dispatches /investigations/{id}/calls and
// /investigations/{id}/calls/{call_id} (any reader).
func (b *Backend) investigationCallsRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	b.requireRolesOrDeny(w, r, anyReader, func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// investigations / {id} / calls [/ {call_id}]
		if len(parts) < 3 || parts[0] != "investigations" || parts[2] != "calls" {
			writeJSONError(w, http.StatusBadRequest, "path must be /investigations/{id}/calls[/{call_id}]")
			return
		}
		invID, err := uuid.Parse(parts[1])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid investigation id in path")
			return
		}
		if b.cfg.Handler == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
			return
		}
		db := b.cfg.Handler.DB()
		if len(parts) == 3 {
			calls, err := listCalls(r.Context(), db, invID)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "list calls: "+err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"calls": calls})
			return
		}
		callID, err := uuid.Parse(parts[3])
		if err != nil || len(parts) != 4 {
			writeJSONError(w, http.StatusBadRequest, "invalid call id in path")
			return
		}
		detail, err := b.callDetail(r.Context(), db, invID, callID)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "no such call in this investigation")
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "call: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})
}

// investigationEventsRoute serves GET /investigations/{id}/events (adr/0092):
// the events the investigation's calls retrieved, ordered by event time, with
// ?entity=<ref> (events whose observed-data cite the entity), ?category=,
// ?around=<event_id> (the window on the same host entity) and ?limit=.
func (b *Backend) investigationEventsRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	b.requireRolesOrDeny(w, r, anyReader, func(w http.ResponseWriter, r *http.Request) {
		invID, ok := investigationSubresourceID(r.URL.Path, "events")
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid investigation id in path")
			return
		}
		if b.cfg.Handler == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
			return
		}
		q := r.URL.Query()
		limit := 500
		if v := q.Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
				limit = n
			}
		}
		rows, err := b.investigationEvents(r.Context(), b.cfg.Handler.DB(), invID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "events: "+err.Error())
			return
		}
		rows = filterEvents(rows, q.Get("entity"), q.Get("category"), q.Get("around"), limit)
		total := len(rows)
		if len(rows) > limit {
			rows = rows[:limit]
		}
		writeJSON(w, http.StatusOK, EventsResponse{Events: rows, Total: total})
	})
}

// filterEvents applies the listing's selectors to time-ordered rows. around
// takes the window of ±limit/2 events on the same host entity as the named
// event (the first entity when the event names no host).
func filterEvents(rows []EventRow, entity, category, around string, limit int) []EventRow {
	out := rows
	if entity != "" {
		out = keep(out, func(e EventRow) bool { return containsRef(e.EntityRefs, entity) })
	}
	if category != "" {
		out = keep(out, func(e EventRow) bool { return e.View != nil && e.View.Category == category })
	}
	if around != "" {
		var anchor *EventRow
		for i := range rows {
			if rows[i].ID == around {
				anchor = &rows[i]
				break
			}
		}
		if anchor == nil {
			return []EventRow{}
		}
		key := ""
		for _, ref := range anchor.EntityRefs {
			if strings.HasPrefix(ref, identity.TypeHost+"--") {
				key = ref
				break
			}
		}
		if key == "" && len(anchor.EntityRefs) > 0 {
			key = anchor.EntityRefs[0]
		}
		same := keep(out, func(e EventRow) bool { return key == "" || containsRef(e.EntityRefs, key) })
		idx := -1
		for i := range same {
			if same[i].ID == around {
				idx = i
				break
			}
		}
		if idx < 0 {
			return []EventRow{*anchor}
		}
		half := limit / 2
		if half < 1 {
			half = 1
		}
		lo, hi := idx-half, idx+half+1
		if lo < 0 {
			lo = 0
		}
		if hi > len(same) {
			hi = len(same)
		}
		out = same[lo:hi]
	}
	return out
}

func keep(rows []EventRow, f func(EventRow) bool) []EventRow {
	out := make([]EventRow, 0, len(rows))
	for _, e := range rows {
		if f(e) {
			out = append(out, e)
		}
	}
	return out
}

func containsRef(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// investigationEvents reads every event the investigation's calls retrieved,
// with its joins, ordered by event time then id.
func (b *Backend) investigationEvents(ctx context.Context, db *sql.DB, invID uuid.UUID) ([]EventRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT e.id, e.class_uid, e.class_name, e.time, e.recorded_at, e.source_tool, e.payload, c.id, c.verb, c.coverage
		FROM capability_calls c
		JOIN call_events ce ON ce.call_id = c.id
		JOIN ocsf_events e ON e.id = ce.ocsf_event_id
		WHERE c.aggregate_id = $1
		ORDER BY e.time, e.id
	`, invID)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	var out []EventRow
	seen := map[string]bool{}
	for rows.Next() {
		var e EventRow
		var id, callID uuid.UUID
		var payload []byte
		if err := rows.Scan(&id, &e.ClassUID, &e.ClassName, &e.Time, &e.RecordedAt, &e.SourceTool, &payload, &callID, &e.Verb, &e.Coverage); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.ID, e.CallID = id.String(), callID.String()
		if seen[e.ID] {
			continue // the same event retrieved by two calls lists once, under the first
		}
		seen[e.ID] = true
		e.View = b.eventView(capability.OcsfEvent{ID: id, ClassUID: e.ClassUID, ClassName: e.ClassName, Time: e.Time, RecordedAt: e.RecordedAt, SourceTool: e.SourceTool}, payload)
		var m map[string]any
		if json.Unmarshal(payload, &m) == nil {
			e.Pattern = ocsfview.PatternKey(e.ClassUID, m)
		}
		e.ObservedDataRefs, e.EntityRefs, e.Relationships = []string{}, []string{}, []EventRelationship{}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []EventRow{}
	}
	if err := joinEventObjects(ctx, db, out); err != nil {
		return nil, err
	}
	return out, nil
}

// joinEventObjects fills each row's observed-data, entity refs and the
// relationships among those entities from the persisted joins.
func joinEventObjects(ctx context.Context, db *sql.DB, events []EventRow) error {
	if len(events) == 0 {
		return nil
	}
	byID := make(map[string]*EventRow, len(events))
	ids := make([]uuid.UUID, 0, len(events))
	for i := range events {
		byID[events[i].ID] = &events[i]
		ids = append(ids, uuid.MustParse(events[i].ID))
	}
	rows, err := db.QueryContext(ctx, `
		SELECT oe.ocsf_event_id, oe.observed_data_id, s.payload -> 'object_refs'
		FROM observed_data_events oe
		LEFT JOIN stix_objects s ON s.id = oe.observed_data_id
		WHERE oe.ocsf_event_id = ANY($1)
	`, uuidArray(ids))
	if err != nil {
		return fmt.Errorf("query observed-data joins: %w", err)
	}
	defer rows.Close()
	entityUUIDs := map[uuid.UUID]string{}
	for rows.Next() {
		var evID, odID uuid.UUID
		var refs []byte
		if err := rows.Scan(&evID, &odID, &refs); err != nil {
			return fmt.Errorf("scan observed-data join: %w", err)
		}
		e := byID[evID.String()]
		if e == nil {
			continue
		}
		e.ObservedDataRefs = append(e.ObservedDataRefs, identity.TypeObservedData+"--"+odID.String())
		var objectRefs []string
		if len(refs) > 0 {
			_ = json.Unmarshal(refs, &objectRefs)
		}
		for _, ref := range objectRefs {
			if !containsRef(e.EntityRefs, ref) {
				e.EntityRefs = append(e.EntityRefs, ref)
			}
			if u, ok := stixRefUUID(ref); ok {
				entityUUIDs[u] = ref
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(entityUUIDs) == 0 {
		return nil
	}
	all := make([]uuid.UUID, 0, len(entityUUIDs))
	for u := range entityUUIDs {
		all = append(all, u)
	}
	edges, err := db.QueryContext(ctx, `
		SELECT source_ref, target_ref, relationship_type FROM stix_edges
		WHERE source_ref = ANY($1) AND target_ref = ANY($1)
	`, uuidArray(all))
	if err != nil {
		return fmt.Errorf("query relationships: %w", err)
	}
	defer edges.Close()
	type rel struct{ src, tgt, typ string }
	var rels []rel
	for edges.Next() {
		var s, t uuid.UUID
		var typ string
		if err := edges.Scan(&s, &t, &typ); err != nil {
			return fmt.Errorf("scan relationship: %w", err)
		}
		rels = append(rels, rel{entityUUIDs[s], entityUUIDs[t], typ})
	}
	if err := edges.Err(); err != nil {
		return err
	}
	for i := range events {
		e := &events[i]
		for _, r := range rels {
			if containsRef(e.EntityRefs, r.src) && containsRef(e.EntityRefs, r.tgt) {
				e.Relationships = append(e.Relationships, EventRelationship{Type: r.typ, SourceRef: r.src, TargetRef: r.tgt})
			}
		}
		sort.Slice(e.Relationships, func(a, b int) bool {
			x, y := e.Relationships[a], e.Relationships[b]
			return x.Type+x.SourceRef+x.TargetRef < y.Type+y.SourceRef+y.TargetRef
		})
	}
	return nil
}

// uuidArray renders uuids as a Postgres uuid[] literal parameter.
func uuidArray(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func listCalls(ctx context.Context, db *sql.DB, invID uuid.UUID) ([]CallView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.id, c.verb, c.binding, c.adapter, c.operation, c.priority, c.coverage, c.bindings_answered, c.bindings_total,
		       c.null_observation_ref, c.started_at, c.duration_ms, (SELECT count(*) FROM call_events ce WHERE ce.call_id = c.id)
		FROM capability_calls c WHERE c.aggregate_id = $1 ORDER BY c.started_at, c.id
	`, invID)
	if err != nil {
		return nil, fmt.Errorf("query calls: %w", err)
	}
	defer rows.Close()
	out := []CallView{}
	for rows.Next() {
		v, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanCall(r rowScanner) (CallView, error) {
	var v CallView
	var id uuid.UUID
	var binding, adapter, operation, nullRef sql.NullString
	var priority sql.NullInt64
	if err := r.Scan(&id, &v.Verb, &binding, &adapter, &operation, &priority, &v.Coverage, &v.BindingsAnswered, &v.BindingsTotal,
		&nullRef, &v.StartedAt, &v.DurationMs, &v.EventCount); err != nil {
		return v, fmt.Errorf("scan call: %w", err)
	}
	v.ID, v.Binding, v.Adapter, v.Operation, v.NullObservationRef = id.String(), binding.String, adapter.String, operation.String, nullRef.String
	if priority.Valid {
		p := int(priority.Int64)
		v.Priority = &p
	}
	return v, nil
}

func (b *Backend) callDetail(ctx context.Context, db *sql.DB, invID, callID uuid.UUID) (CallDetail, error) {
	row := db.QueryRowContext(ctx, `
		SELECT c.id, c.verb, c.binding, c.adapter, c.operation, c.priority, c.coverage, c.bindings_answered, c.bindings_total,
		       c.null_observation_ref, c.started_at, c.duration_ms, (SELECT count(*) FROM call_events ce WHERE ce.call_id = c.id)
		FROM capability_calls c WHERE c.id = $1 AND c.aggregate_id = $2
	`, callID, invID)
	view, err := scanCall(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CallDetail{}, sql.ErrNoRows
		}
		return CallDetail{}, err
	}
	d := CallDetail{CallView: view, DegradationNotes: []string{}, Attempts: []capability.BindingAttempt{}, OtherBindings: []capability.BindingStatus{}}
	var params, notes, attempts []byte
	if err := db.QueryRowContext(ctx, `SELECT params_sent, degradation_notes, attempts FROM capability_calls WHERE id = $1`, callID).Scan(&params, &notes, &attempts); err != nil {
		return d, fmt.Errorf("call payload: %w", err)
	}
	if len(params) > 0 {
		d.ParamsSent = params
	}
	if len(notes) > 0 {
		_ = json.Unmarshal(notes, &d.DegradationNotes)
	}
	if len(attempts) > 0 {
		_ = json.Unmarshal(attempts, &d.Attempts)
	}
	events, err := b.investigationEvents(ctx, db, invID)
	if err != nil {
		return d, err
	}
	d.Produced = CallProduced{OcsfEventRefs: []string{}, ObservedDataRefs: []string{}, EntityRefs: []string{}, NullObservationRef: view.NullObservationRef, Events: []EventRow{}}
	for _, e := range events {
		if e.CallID != view.ID {
			continue
		}
		d.Produced.Events = append(d.Produced.Events, e)
		d.Produced.OcsfEventRefs = append(d.Produced.OcsfEventRefs, e.ID)
		for _, r := range e.ObservedDataRefs {
			if !containsRef(d.Produced.ObservedDataRefs, r) {
				d.Produced.ObservedDataRefs = append(d.Produced.ObservedDataRefs, r)
			}
		}
		for _, r := range e.EntityRefs {
			if !containsRef(d.Produced.EntityRefs, r) {
				d.Produced.EntityRefs = append(d.Produced.EntityRefs, r)
			}
		}
	}
	if resolver, _ := b.capabilitySurface(); resolver != nil {
		d.OtherBindings = resolver.VerbBindings(view.Verb)
	}
	if b.cfg.CapabilityConfigPath != "" {
		if tc, err := capability.LoadTenantConfig(b.cfg.CapabilityConfigPath); err == nil {
			for _, v := range buildEnablementView(b.cfg.CapabilityConfigPath, tc).Verbs {
				if v.Verb == view.Verb {
					d.ClosableBy = v.ClosableBy
				}
			}
		}
	}
	return d, nil
}

// EntityBaselineResponse is the baseline of one entity within one
// investigation (adr/0095): the fold over the retrieved events that cite it.
type EntityBaselineResponse struct {
	Entity string `json:"entity"`
	ocsfview.BaselineView
}

// investigationBaselineRoute serves GET /investigations/{id}/entities/{ref}/baseline:
// the patterns and deviations the retrieved record holds for the entity, or
// 204 when nothing repeats — a surface shows the section only then.
func (b *Backend) investigationBaselineRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	b.requireRolesOrDeny(w, r, anyReader, func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// investigations / {id} / entities / {ref} / baseline
		if len(parts) != 5 || parts[0] != "investigations" || parts[2] != "entities" || parts[4] != "baseline" {
			writeJSONError(w, http.StatusBadRequest, "path must be /investigations/{id}/entities/{ref}/baseline")
			return
		}
		invID, err := uuid.Parse(parts[1])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid investigation id in path")
			return
		}
		if b.cfg.Handler == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
			return
		}
		rows, err := b.investigationEvents(r.Context(), b.cfg.Handler.DB(), invID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "events: "+err.Error())
			return
		}
		ref := parts[3]
		var in []ocsfview.BaselineEvent
		for _, e := range rows {
			if !containsRef(e.EntityRefs, ref) {
				continue
			}
			in = append(in, ocsfview.BaselineEvent{ID: e.ID, ClassUID: e.ClassUID, ClassName: e.ClassName, Time: e.Time, Key: e.Pattern})
		}
		view := ocsfview.Baseline(in)
		if len(view.Patterns) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, EntityBaselineResponse{Entity: ref, BaselineView: view})
	})
}

// callIDForEvent answers the call that retrieved an event, "" when none
// (an event persisted before adr/0092, or outside any call).
func callIDForEvent(ctx context.Context, db *sql.DB, eventID uuid.UUID) string {
	var id uuid.UUID
	err := db.QueryRowContext(ctx, `SELECT call_id FROM call_events WHERE ocsf_event_id = $1 ORDER BY call_id LIMIT 1`, eventID).Scan(&id)
	if err != nil {
		return ""
	}
	return id.String()
}

// RoleReaders is the reader role set, named for the call routes.
var _ = authz.RoleViewer
