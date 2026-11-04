package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/capability/ocsfview"
	"github.com/sd-strax/reckon/evidence"
)

// The investigation's retrieved record (adr/0092): the events a capability
// call brought in, listable by entity, category and around one event, and the
// calls themselves. Reads over persisted tables, never a capability call.

// EventRowView is one retrieved event without its payload (GET
// /api/evidence/{id} serves that): the record, the call that retrieved it, the
// interpretation-layer refs derived from it, and its rendering (adr/0090).
type EventRowView struct {
	ID               string    `json:"id"`
	ClassUID         int       `json:"class_uid"`
	ClassName        string    `json:"class_name"`
	Time             time.Time `json:"time"`
	RecordedAt       time.Time `json:"recorded_at"`
	SourceTool       string    `json:"source_tool"`
	CallID           string    `json:"call_id"`
	Verb             string    `json:"verb"`
	Coverage         string    `json:"coverage"`
	ObservedDataRefs []string  `json:"observed_data_refs"`
	EntityRefs       []string  `json:"entity_refs"`
	// View is the sentence, the curated fields and the category word, rendered
	// at read time from the payload and the record's entities (adr/0090).
	View ocsfview.View `json:"view"`
}

// InvestigationEventsResponse is a page of events with the unpaged count and,
// when more remain in the direction of travel, the cursor of the next page.
// With `group_by` (adr/0095) the read is the newest MaxEventLimit events
// instead of a page: Groups folds them by class and key, Keys lists the slots
// each class present can key on, and FoldedOver says how many of Total the
// fold covered.
type InvestigationEventsResponse struct {
	Events     []EventRowView       `json:"events"`
	Total      int                  `json:"total"`
	NextCursor string               `json:"next_cursor,omitempty"`
	Groups     []ocsfview.Group     `json:"groups,omitempty"`
	Keys       []EventClassKeysView `json:"keys,omitempty"`
	FoldedOver int                  `json:"folded_over,omitempty"`
}

// EventClassKeysView is what a class present in a grouped read can key on:
// its slots, the default key, and the key this read used.
type EventClassKeysView struct {
	ClassUID  int                 `json:"class_uid"`
	ClassName string              `json:"class_name"`
	Slots     []ocsfview.SlotSpec `json:"slots"`
	Default   []string            `json:"default"`
	Key       []string            `json:"key"`
}

// CapabilityCallView is one call as a surface opens it: which binding
// answered, with what, how it went, and what it left behind.
type CapabilityCallView struct {
	CallID           string          `json:"call_id"`
	InvestigationID  string          `json:"investigation_id,omitempty"`
	Verb             string          `json:"verb"`
	Adapter          string          `json:"adapter,omitempty"`
	Operation        string          `json:"operation,omitempty"`
	Priority         int             `json:"priority"`
	ParamsSent       json.RawMessage `json:"params_sent,omitempty"`
	Coverage         string          `json:"coverage"`
	BindingsAnswered int             `json:"bindings_answered"`
	BindingsTotal    int             `json:"bindings_total"`
	NullObservation  string          `json:"null_observation_ref,omitempty"`
	DegradationNotes []string        `json:"degradation_notes"`
	StartedAt        time.Time       `json:"started_at"`
	DurationMS       int             `json:"duration_ms"`
	// Outcomes is every binding the resolver walked, in priority order, with
	// what each did; adapter, operation, priority and params_sent summarize the
	// first that answered.
	Outcomes []BindingOutcomeView `json:"outcomes"`
	// Produced is present on the single-call read only.
	Produced *CallProducedView `json:"produced,omitempty"`
	// Bindings lists every binding configured for the verb, so the inspector
	// can show the other ways it could have been answered, including ones whose
	// adapter is installed but not enabled. Single-call read only.
	Bindings []VerbBindingView `json:"bindings,omitempty"`
}

// CallProducedView is the evidence a call left behind.
type CallProducedView struct {
	EventIDs         []string `json:"event_ids"`
	ObservedDataRefs []string `json:"observed_data_refs"`
	EntityRefs       []string `json:"entity_refs"`
}

// BindingOutcomeView is what one binding did on a call.
type BindingOutcomeView struct {
	Adapter   string          `json:"adapter"`
	Operation string          `json:"operation"`
	Priority  int             `json:"priority"`
	Params    json.RawMessage `json:"params,omitempty"`
	Answered  bool            `json:"answered"`
	Skipped   string          `json:"skipped,omitempty"`
}

// VerbBindingView is one configured binding of a verb.
type VerbBindingView struct {
	Adapter   string `json:"adapter"`
	Operation string `json:"operation"`
	Priority  int    `json:"priority"`
	Enabled   bool   `json:"enabled"`
}

// InvestigationCallsResponse lists an investigation's calls, oldest first.
type InvestigationCallsResponse struct {
	Calls []CapabilityCallView `json:"calls"`
}

// listInvestigationEvents serves GET /api/investigations/{id}/events?entity=&
// category=&around=&limit=&order=&cursor=&group_by=: the events retrieved for
// the investigation, in event time, oldest first or newest first
// (`order=newest`), `limit` to a page and `cursor` (the `next_cursor` of the
// previous page) continuing it. `around` centres the page on one event (its
// id), `limit` events each side, and is a 404 when the event is not in the
// investigation. `group_by` (`default`, or `class:slot,slot;…`) folds the
// newest MaxEventLimit matching events by class and key (adr/0095); `entity`
// is then the pivot, and its slot leaves the key. It excludes `around` and
// `cursor`.
func (b *Backend) listInvestigationEvents(w http.ResponseWriter, r *http.Request, invID uuid.UUID) {
	if b.cfg.Evidence == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "evidence store not configured")
		return
	}
	q := r.URL.Query()
	f := evidence.EventFilter{Entity: q.Get("entity"), Category: q.Get("category")}
	groupBy, grouped := q.Get("group_by"), q.Has("group_by")
	var keys ocsfview.Keys
	if grouped {
		if q.Get("around") != "" || q.Get("cursor") != "" {
			writeJSONError(w, http.StatusBadRequest, "group_by cannot be combined with around or cursor")
			return
		}
		k, err := ocsfview.ParseKeys(groupBy)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "group_by: "+err.Error())
			return
		}
		keys = k
		f.Limit, f.Newest = evidence.MaxEventLimit, true
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeJSONError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		f.Limit = n
	}
	if v := q.Get("around"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "around is not an event id")
			return
		}
		f.Around = id
	}
	switch q.Get("order") {
	case "", "oldest":
	case "newest":
		f.Newest = true
	default:
		writeJSONError(w, http.StatusBadRequest, "order must be oldest or newest")
		return
	}
	if v := q.Get("cursor"); v != "" {
		c, ok := parseEventCursor(v)
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "cursor is not one this read returned")
			return
		}
		f.Cursor = &c
	}
	page, err := b.cfg.Evidence.InvestigationEvents(r.Context(), invID, f)
	if errors.Is(err, evidence.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "no persisted telemetry record "+f.Around.String())
		return
	}
	if err != nil {
		writeInternalError(w, "list events", err)
		return
	}
	out := InvestigationEventsResponse{Events: make([]EventRowView, 0, len(page.Rows)), Total: page.Total}
	for _, row := range page.Rows {
		out.Events = append(out.Events, eventRowView(row))
	}
	if grouped {
		out.Groups, out.Keys = foldEvents(out.Events, keys, f.Entity)
		out.FoldedOver = len(out.Events)
		writeJSON(w, http.StatusOK, out)
		return
	}
	if page.NextCursor != nil {
		out.NextCursor = eventCursor(*page.NextCursor)
	}
	writeJSON(w, http.StatusOK, out)
}

// foldEvents groups the rendered rows by class and key (adr/0095) and lists,
// per class present, what it can key on. The rows' views carry the slot
// values the templates registered.
func foldEvents(rows []EventRowView, keys ocsfview.Keys, pivot string) ([]ocsfview.Group, []EventClassKeysView) {
	items := make([]ocsfview.GroupItem, 0, len(rows))
	classes := map[int]string{}
	for _, r := range rows {
		items = append(items, ocsfview.GroupItem{ID: r.ID, ClassUID: r.ClassUID, Time: r.Time, Slots: r.View.Slots})
		classes[r.ClassUID] = r.ClassName
	}
	groups := ocsfview.Fold(items, keys, pivot)
	if groups == nil {
		groups = []ocsfview.Group{}
	}
	uids := make([]int, 0, len(classes))
	for uid := range classes {
		uids = append(uids, uid)
	}
	sort.Ints(uids)
	out := make([]EventClassKeysView, 0, len(uids))
	for _, uid := range uids {
		key := keys[uid]
		if key == nil {
			key = ocsfview.DefaultKey(uid)
		}
		slots := ocsfview.Slots(uid)
		if slots == nil {
			slots = []ocsfview.SlotSpec{}
		}
		out = append(out, EventClassKeysView{ClassUID: uid, ClassName: classes[uid], Slots: slots, Default: nonNilStrings(ocsfview.DefaultKey(uid)), Key: nonNilStrings(key)})
	}
	return groups, out
}

// eventCursor renders a position as the wire carries it: the event time in
// RFC 3339 with nanoseconds, a comma, the event id.
func eventCursor(c evidence.EventCursor) string {
	return c.Time.UTC().Format(time.RFC3339Nano) + "," + c.ID.String()
}

func parseEventCursor(v string) (evidence.EventCursor, bool) {
	at, id, ok := strings.Cut(v, ",")
	if !ok {
		return evidence.EventCursor{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return evidence.EventCursor{}, false
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return evidence.EventCursor{}, false
	}
	return evidence.EventCursor{Time: t, ID: u}, true
}

func eventRowView(row evidence.EventRow) EventRowView {
	return EventRowView{
		ID:               row.ID.String(),
		ClassUID:         row.ClassUID,
		ClassName:        row.ClassName,
		Time:             row.Time,
		RecordedAt:       row.RecordedAt,
		SourceTool:       row.SourceTool,
		CallID:           row.CallID.String(),
		Verb:             row.Verb,
		Coverage:         row.Coverage,
		ObservedDataRefs: nonNilStrings(row.ObservedDataRefs),
		EntityRefs:       nonNilStrings(row.EntityRefs),
		View:             eventView(row.Event, row.Entities),
	}
}

// eventView renders one event with the entities the record derived from it.
func eventView(e evidence.Event, entities []evidence.Object) ocsfview.View {
	var payload map[string]any
	_ = json.Unmarshal(e.Payload, &payload)
	ents := make([]ocsfview.Entity, 0, len(entities))
	for _, o := range entities {
		ents = append(ents, ocsfview.EntityFromObject(o.Ref, o.Type, o.Payload))
	}
	return ocsfview.Render(ocsfview.Event{ClassUID: e.ClassUID, ClassName: e.ClassName, Payload: payload}, ents)
}

// listInvestigationCalls serves GET /api/investigations/{id}/calls.
func (b *Backend) listInvestigationCalls(w http.ResponseWriter, r *http.Request, invID uuid.UUID) {
	if b.cfg.Evidence == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "evidence store not configured")
		return
	}
	calls, err := b.cfg.Evidence.Calls(r.Context(), invID)
	if err != nil {
		writeInternalError(w, "list calls", err)
		return
	}
	out := InvestigationCallsResponse{Calls: make([]CapabilityCallView, 0, len(calls))}
	for _, c := range calls {
		out.Calls = append(out.Calls, callView(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// getInvestigationCall serves GET /api/investigations/{id}/calls/{call_id}:
// the call with what it produced and the verb's configured bindings.
func (b *Backend) getInvestigationCall(w http.ResponseWriter, r *http.Request, invID uuid.UUID) {
	if b.cfg.Evidence == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "evidence store not configured")
		return
	}
	callID, err := uuid.Parse(r.PathValue("call_id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "call_id in path is not a valid id")
		return
	}
	c, err := b.cfg.Evidence.Call(r.Context(), invID, callID)
	if errors.Is(err, evidence.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "no capability call "+callID.String()+" in this investigation")
		return
	}
	if err != nil {
		writeInternalError(w, "read call", err)
		return
	}
	v := callView(c)
	ids := make([]string, 0, len(c.Produced.EventIDs))
	for _, id := range c.Produced.EventIDs {
		ids = append(ids, id.String())
	}
	v.Produced = &CallProducedView{EventIDs: ids, ObservedDataRefs: nonNilStrings(c.Produced.ObservedDataRefs), EntityRefs: nonNilStrings(c.Produced.EntityRefs)}
	v.Bindings = b.verbBindings(c.Verb)
	writeJSON(w, http.StatusOK, v)
}

func callView(c evidence.CallRecord) CapabilityCallView {
	v := CapabilityCallView{
		CallID:           c.ID.String(),
		Verb:             c.Verb,
		Adapter:          c.Adapter,
		Operation:        c.Operation,
		Priority:         c.Priority,
		ParamsSent:       c.ParamsSent,
		Coverage:         c.Coverage,
		BindingsAnswered: c.BindingsAnswered,
		BindingsTotal:    c.BindingsTotal,
		NullObservation:  c.NullObservation,
		DegradationNotes: nonNilStrings(c.DegradationNotes),
		StartedAt:        c.StartedAt,
		DurationMS:       c.DurationMS,
		Outcomes:         make([]BindingOutcomeView, 0, len(c.Bindings)),
	}
	for _, o := range c.Bindings {
		v.Outcomes = append(v.Outcomes, BindingOutcomeView{Adapter: o.Adapter, Operation: o.Operation, Priority: o.Priority, Params: o.Params, Answered: o.Answered, Skipped: o.Skipped})
	}
	if c.InvestigationID != nil {
		v.InvestigationID = c.InvestigationID.String()
	}
	return v
}

// verbBindings lists the bindings configured for verb. The tenant config file
// is the authority when the backend has one, because it also names bindings
// on adapters that are installed but not enabled; otherwise the live resolver
// knows only the enabled ones.
func (b *Backend) verbBindings(verb string) []VerbBindingView {
	out := []VerbBindingView{}
	if b.cfg.CapabilityConfigPath != "" {
		if tc, err := capability.LoadTenantConfig(b.cfg.CapabilityConfigPath); err == nil {
			for _, bd := range tc.Bindings[verb] {
				spec, ok := tc.Adapters[bd.Adapter]
				out = append(out, VerbBindingView{Adapter: bd.Adapter, Operation: bd.Operation, Priority: bd.Priority, Enabled: ok && spec.Enabled})
			}
			return out
		}
	}
	resolver, _ := b.capabilitySurface()
	if resolver == nil {
		return out
	}
	for _, bd := range resolver.Bindings(verb) {
		out = append(out, VerbBindingView{Adapter: bd.Adapter, Operation: bd.Operation, Priority: bd.Priority, Enabled: true})
	}
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
