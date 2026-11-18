package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/module"
)

// TestEventCitation_RefusedWithItsObservation: a new citation names a STIX
// object (adr/0364). A pin, a prediction outcome and an action request that
// cite a raw event are each refused with a 422 that names the field, the
// event and the observation made from it. An event with no observation is
// refused as unreadable. The observation's evidence view leads back to the
// event.
func TestEventCitation_RefusedWithItsObservation(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	if _, err := testDB.Exec(`TRUNCATE ai_tool_calls, call_events, observed_data_events, capability_calls, ocsf_events, stix_edges`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	invID := activeInvestigation(t)
	b := actionBackend(t)
	b.cfg.CapabilityResolver, b.cfg.CapabilityCatalog = buildInvokeCapability(t)
	token := mintToken(t, nil)

	resp, out := postCapability(t, b, token, "enumerate_logons", CapabilityInvokeBody{})
	if resp.StatusCode != http.StatusOK || len(out.OcsfEventRefs) == 0 {
		t.Fatalf("invoke = %d with %d events; want 200 and an event", resp.StatusCode, len(out.OcsfEventRefs))
	}
	event := out.OcsfEventRefs[0]
	derived, err := b.cfg.Evidence.EventDerived(t.Context(), uuid.MustParse(event))
	if err != nil || len(derived.ObservedDataRefs) == 0 {
		t.Fatalf("EventDerived(%s) = %v, %v; want its observation", event, derived.ObservedDataRefs, err)
	}
	observation := derived.ObservedDataRefs[0]
	// An event the record holds with no observation made from it, as one
	// the normalizer could not read.
	unread := uuid.NewString()
	if _, err := testDB.Exec(`INSERT INTO ocsf_events (id, tenant_id, class_uid, class_name, time, recorded_at, source_tool, payload)
		VALUES ($1, $2, 3002, 'Authentication', now(), now(), 'edr', '{}')`, unread, module.SingleTenantUUID); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	post := func(path string, body any) (int, string) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return resp.StatusCode, e.Error
	}
	refused := func(what string, code int, msg, field, ev string, wants ...string) {
		t.Helper()
		if code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d (%s); want 422", what, code, msg)
			return
		}
		for _, w := range append([]string{field, ev}, wants...) {
			if !strings.Contains(msg, w) {
				t.Errorf("%s: error %q does not name %q", what, msg, w)
			}
		}
	}
	pin := func(ref string) (int, string) {
		return post("/api/interpretations", map[string]any{
			"investigation_ref": invID.String(), "interpretation_type": "evidence-pin",
			"input_refs": []string{ref}, "rationale": "the logon",
		})
	}

	code, msg := pin(event)
	refused("pin of an event", code, msg, "input_refs", event, observation)
	code, msg = pin(unread)
	refused("pin of an unread event", code, msg, "input_refs", unread, "was not read into an observation")
	// A bare id the store holds no event for (a call id, an action id) is
	// not called an unread event.
	stray := uuid.NewString()
	code, msg = pin(stray)
	refused("pin of an unknown id", code, msg, "input_refs", stray, "neither a STIX id nor a known event")
	if strings.Contains(msg, "not read") {
		t.Errorf("pin of an unknown id: error %q calls it an unread event", msg)
	}
	// Every event in the list is named in one answer, each with what to cite.
	code, msg = post("/api/interpretations", map[string]any{
		"investigation_ref": invID.String(), "interpretation_type": "evidence-pin",
		"input_refs": []string{event, unread}, "rationale": "the logons",
	})
	refused("pin of two events", code, msg, "input_refs", event, observation, unread, "was not read into an observation")
	if code, msg := pin(observation); code != http.StatusCreated {
		t.Errorf("pin of the observation = %d (%s); want 201", code, msg)
	}

	// A prediction outcome cites its test result the same way.
	hypID, predID := uuid.New(), uuid.New()
	humanHandle(t, invID, aggregate.RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: aggregate.InterpretationHypothesis, Rationale: "r",
		Hypothesis: &aggregate.HypothesisNode{ID: hypID, Statement: "an interactive logon"},
	})
	humanHandle(t, invID, aggregate.RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: aggregate.InterpretationPrediction, Rationale: "r",
		Prediction: &aggregate.PredictionNode{ID: predID, HypothesisRef: aggregate.HypothesisSTIXID(hypID), Statement: "a logon is recorded"},
	})
	code, msg = post("/api/interpretations", map[string]any{
		"investigation_ref": invID.String(), "interpretation_type": "prediction",
		"prediction_ref": aggregate.PredictionSTIXID(predID), "prediction_status": aggregate.PredictionConfirmed,
		"test_result_refs": []string{event}, "rationale": "the logon is there",
	})
	refused("outcome citing an event", code, msg, "test_result_refs", event, observation)

	// So does an action request.
	code, msg = post("/api/actions", RequestActionBody{
		ActionType:       "host.isolate",
		Targets:          []aggregate.TargetSpec{{EntityRef: "reckon-host--" + uuid.NewString(), ResolvedIdentifier: "WIN-A"}},
		Rationale:        "contain",
		EvidenceRefs:     []string{event},
		InvestigationRef: invID.String(),
	})
	refused("action citing an event", code, msg, "evidence_refs", event, observation)

	// The observation leads back to the event it was made from.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/evidence/"+observation, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var v EvidenceView
	_ = json.NewDecoder(r.Body).Decode(&v)
	if r.StatusCode != http.StatusOK || !slices.Contains(v.SourceEventIDs, event) {
		t.Errorf("open %s = %d, source_event_ids %v; want it to list %s", observation, r.StatusCode, v.SourceEventIDs, event)
	}
}

// TestEventCitation_WithoutEvidenceStore: with no evidence store the 422
// still names the field and the event.
func TestEventCitation_WithoutEvidenceStore(t *testing.T) {
	b := &Backend{}
	event := uuid.NewString()
	w := httptest.NewRecorder()
	err := &aggregate.RejectedError{Err: &aggregate.EventCitedError{Field: "input_refs", Refs: []string{event}}}
	if !b.writeEventCitedError(t.Context(), w, "record interpretation", err) {
		t.Fatal("an EventCitedError was not answered")
	}
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "input_refs") || !strings.Contains(w.Body.String(), event) {
		t.Errorf("answer = %d %s; want 422 naming input_refs and %s", w.Code, w.Body.String(), event)
	}
	if b.writeEventCitedError(t.Context(), httptest.NewRecorder(), "x", &aggregate.RejectedError{Err: errors.New("refused")}) {
		t.Error("another rejection was answered as an event citation")
	}
}

// TestCitableRefs: a re-request carries the original's STIX ids and leaves
// out a raw event id it cited (adr/0364).
func TestCitableRefs(t *testing.T) {
	od := "observed-data--" + uuid.NewString()
	if got := citableRefs([]string{uuid.NewString(), od}); !slices.Equal(got, []string{od}) {
		t.Errorf("citableRefs = %v; want [%s]", got, od)
	}
}

// TestRerequestAction_OlderEventCitation: a re-request of an action recorded
// before adr/0364 carries the original's STIX ids and leaves its raw event ids
// out. An original that cites raw events only is refused with a 422 saying a
// new request must cite their observations.
func TestRerequestAction_OlderEventCitation(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t)
	od, event := "observed-data--"+uuid.NewString(), uuid.NewString()
	// The older action is written as the record held it: the request check
	// that refuses a raw event id is not on this path.
	older := func(refs ...string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		env := aggregate.Envelope{
			AggregateID: invID, TenantID: module.SingleTenantUUID, CorrelationID: uuid.New(),
			Actor: aggregate.Actor{PrincipalID: "test-subject"}, OccurredAt: time.Now().UTC(),
		}
		if _, err := testHandler.Handle(t.Context(), env, aggregate.RequestAction{
			ActionID: id, ActionType: "host.isolate", Tier: aggregate.TierT2,
			Targets:   []aggregate.TargetSpec{{EntityRef: "reckon-host--" + uuid.NewString(), ResolvedIdentifier: "WIN-L"}},
			ExpiresAt: time.Now().Add(-time.Minute), Rationale: "contain", EvidenceRefs: refs,
		}); err != nil {
			t.Fatalf("request: %v", err)
		}
		return id
	}

	status, out := postRaw(t, b, "/api/actions/"+older(event).String()+"/rerequest", RerequestBody{Rationale: "again"})
	if status != http.StatusUnprocessableEntity || !strings.Contains(out, "raw events only") || !strings.Contains(out, event) {
		t.Errorf("re-request of a raw-only original = %d (%s); want 422 saying it cites raw events only", status, out)
	}

	status, out = postRaw(t, b, "/api/actions/"+older(od, event).String()+"/rerequest", RerequestBody{Rationale: "again"})
	if status != http.StatusCreated {
		t.Fatalf("re-request of a mixed original = %d (%s); want 201", status, out)
	}
	var res RequestActionResponse
	_ = json.Unmarshal([]byte(out), &res)
	ac, err := testHandler.Reads().LoadActionCurrent(t.Context(), uuid.MustParse(res.ActionID))
	if err != nil || !slices.Equal(ac.EvidenceRefs, []string{od}) {
		t.Errorf("re-requested evidence_refs = %v, %v; want [%s]", ac.EvidenceRefs, err, od)
	}
}

// TestEvidence_ObservationDegradesWithoutSourceEvents: a failed read of an
// observation's source events leaves the observation readable, with no
// source_event_ids, as the identity view degrades.
func TestEvidence_ObservationDegradesWithoutSourceEvents(t *testing.T) {
	b := newFakeBackend(t)
	od := "observed-data--" + uuid.NewString()
	b.cfg.Evidence = &fakeEvidence{
		objects:         map[string]evidence.Object{od: {Ref: od, Type: "observed-data", Payload: []byte(`{}`)}},
		sourceEventsErr: errors.New("down"),
	}
	code, body := send(t, b, http.MethodGet, "/api/evidence/"+od, mintToken(t, nil), "")
	if code != http.StatusOK || strings.Contains(body, "source_event_ids") {
		t.Errorf("open %s with the lookup down = %d %s; want 200 without source_event_ids", od, code, body)
	}
}
