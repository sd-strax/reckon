package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/evidence"
)

// The investigation's retrieved record (adr/0092): the events a capability
// call brought in, listable by entity, category and around one event, and the
// calls themselves. Reads over persisted tables, never a capability call.

// EventRowView is one retrieved event without its payload (GET
// /api/evidence/{id} serves that): the record, the call that retrieved it, and
// the interpretation-layer refs derived from it.
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
}

// InvestigationEventsResponse is a page of events with the unpaged count.
type InvestigationEventsResponse struct {
	Events []EventRowView `json:"events"`
	Total  int            `json:"total"`
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
// category=&around=&limit=: the events retrieved for the investigation, in
// event time. `around` centres the page on one event (its id), `limit` events
// each side, and is a 404 when the event is not persisted.
func (b *Backend) listInvestigationEvents(w http.ResponseWriter, r *http.Request, invID uuid.UUID) {
	if b.cfg.Evidence == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "evidence store not configured")
		return
	}
	q := r.URL.Query()
	f := evidence.EventFilter{Entity: q.Get("entity"), Category: q.Get("category")}
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
	writeJSON(w, http.StatusOK, out)
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
	}
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
