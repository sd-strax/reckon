package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/comms"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/knowledge"
)

var errStoreDown = errors.New("store is down")

// fakeCommands records the last command and answers with err.
type fakeCommands struct {
	env aggregate.Envelope
	cmd aggregate.Command
	err error
}

func (f *fakeCommands) Handle(_ context.Context, env aggregate.Envelope, cmd aggregate.Command) (aggregate.Result, error) {
	f.env, f.cmd = env, cmd
	return aggregate.Result{}, f.err
}

// fakeReads finds nothing, except the action it is given.
type fakeReads struct {
	action    aggregate.ActionCurrent
	actionErr error
}

func (f *fakeReads) LoadActionCurrent(context.Context, uuid.UUID) (aggregate.ActionCurrent, error) {
	if f.actionErr != nil {
		return aggregate.ActionCurrent{}, f.actionErr
	}
	if f.action.ActionID == uuid.Nil {
		return aggregate.ActionCurrent{}, aggregate.ErrNoRecord
	}
	return f.action, nil
}
func (*fakeReads) ActionHistory(context.Context, aggregate.AggregateID, uuid.UUID) ([]aggregate.ActionHistoryEntry, error) {
	return nil, nil
}
func (*fakeReads) CountInvestigations(context.Context) (int, error) { return 0, nil }
func (*fakeReads) InvestigationForCase(context.Context, string) (string, error) {
	return "", aggregate.ErrNoRecord
}
func (*fakeReads) InvestigationOpening(context.Context, aggregate.AggregateID) (time.Time, aggregate.Actor, error) {
	return time.Time{}, aggregate.Actor{}, aggregate.ErrNoRecord
}
func (*fakeReads) InvestigationsForCases(context.Context, []string) (map[string]aggregate.CaseInvestigation, error) {
	return nil, nil
}
func (*fakeReads) ListActionCurrents(context.Context, uuid.UUID) ([]aggregate.ActionCurrent, error) {
	return nil, nil
}
func (*fakeReads) ListEvidencePins(context.Context, uuid.UUID) ([]aggregate.EvidencePin, error) {
	return nil, nil
}
func (*fakeReads) ListHypotheses(context.Context, uuid.UUID) ([]aggregate.HypothesisCurrent, error) {
	return nil, nil
}
func (*fakeReads) ListInvestigations(context.Context) ([]aggregate.InvestigationRow, error) {
	return nil, nil
}
func (*fakeReads) ListPendingActionCurrents(context.Context) ([]aggregate.ActionCurrent, error) {
	return nil, nil
}
func (*fakeReads) ListPredictions(context.Context, uuid.UUID) ([]aggregate.PredictionCurrent, error) {
	return nil, nil
}
func (*fakeReads) ListRefAppearances(context.Context, string) ([]aggregate.RefAppearance, error) {
	return nil, nil
}
func (*fakeReads) ListStrandedActionCurrents(context.Context) ([]aggregate.ActionCurrent, error) {
	return nil, nil
}
func (*fakeReads) LoadInterpretation(context.Context, uuid.UUID) (aggregate.InterpretationRecorded, error) {
	return aggregate.InterpretationRecorded{}, aggregate.ErrNoRecord
}
func (*fakeReads) LoadInvestigationCurrent(context.Context, aggregate.AggregateID) (aggregate.InvestigationCurrent, error) {
	return aggregate.InvestigationCurrent{}, aggregate.ErrNoRecord
}
func (*fakeReads) Thread(context.Context, aggregate.AggregateID) ([]aggregate.ThreadAct, error) {
	return nil, nil
}
func (*fakeReads) TranscriptBody(context.Context, uuid.UUID, string) ([]byte, error) {
	return nil, aggregate.ErrNoRecord
}

// fakeEvidence answers every ref with object and err.
type fakeEvidence struct {
	object evidence.Object
	err    error
}

func (f *fakeEvidence) Persist(context.Context, capability.CapabilityResult) error { return f.err }
func (f *fakeEvidence) Object(context.Context, string) (evidence.Object, error) {
	return f.object, f.err
}
func (f *fakeEvidence) Event(context.Context, uuid.UUID) (evidence.Event, error) {
	return evidence.Event{}, evidence.ErrNotFound
}
func (f *fakeEvidence) IndicatorNeighbourhood(context.Context, evidence.Object) (evidence.Neighbourhood, error) {
	return evidence.Neighbourhood{}, f.err
}

// fakeComms records who acted on which thread and answers with err.
type fakeComms struct {
	thread    uuid.UUID
	principal string
	err       error
}

func (f *fakeComms) act(thread uuid.UUID, principal string) error {
	f.thread, f.principal = thread, principal
	return f.err
}
func (f *fakeComms) List(context.Context, uuid.UUID, time.Time) ([]comms.Thread, error) {
	return nil, f.err
}
func (f *fakeComms) Inbound(context.Context, uuid.UUID, string, string, time.Time) error {
	return f.err
}
func (f *fakeComms) Ack(_ context.Context, id uuid.UUID, principal string, _ time.Time) error {
	return f.act(id, principal)
}
func (f *fakeComms) Done(_ context.Context, id uuid.UUID, principal string, _ time.Time) error {
	return f.act(id, principal)
}
func (f *fakeComms) Snooze(_ context.Context, id uuid.UUID, principal string, _ int, _ time.Time) error {
	return f.act(id, principal)
}

// fakeKnowledge has one case summary, and fails everything else with err.
type fakeKnowledge struct {
	summary knowledge.CaseSummaryView
	err     error
}

func (f *fakeKnowledge) Create(context.Context, knowledge.SOP) (uuid.UUID, error) {
	return uuid.Nil, f.err
}
func (f *fakeKnowledge) Get(context.Context, uuid.UUID, uuid.UUID) (knowledge.SOP, error) {
	return knowledge.SOP{}, knowledge.ErrNotFound
}
func (f *fakeKnowledge) Import(context.Context, knowledge.SOP) (uuid.UUID, bool, error) {
	return uuid.Nil, false, f.err
}
func (f *fakeKnowledge) List(context.Context, uuid.UUID, bool) ([]knowledge.SOP, error) {
	return nil, f.err
}
func (f *fakeKnowledge) Retire(context.Context, uuid.UUID, uuid.UUID) error { return f.err }
func (f *fakeKnowledge) Update(context.Context, uuid.UUID, uuid.UUID, string, string, []string, string) error {
	return f.err
}
func (f *fakeKnowledge) RecallSOPs(context.Context, uuid.UUID, knowledge.RecallRequest) (knowledge.RecallResult, error) {
	return knowledge.RecallResult{}, f.err
}
func (f *fakeKnowledge) RecallSimilar(context.Context, uuid.UUID, knowledge.SimilarRequest) (knowledge.SimilarResult, error) {
	return knowledge.SimilarResult{}, f.err
}
func (f *fakeKnowledge) PriorByEntity(context.Context, uuid.UUID, knowledge.PriorRequest) (knowledge.PriorResult, error) {
	return knowledge.PriorResult{}, f.err
}
func (f *fakeKnowledge) WriteSummary(context.Context, uuid.UUID, knowledge.Summary) (uuid.UUID, error) {
	return uuid.Nil, f.err
}
func (f *fakeKnowledge) EnrichSummary(context.Context, uuid.UUID, string, string, string) (uuid.UUID, error) {
	return uuid.Nil, f.err
}
func (f *fakeKnowledge) ListSummaries(context.Context, uuid.UUID, int) ([]knowledge.CaseSummaryView, error) {
	return nil, f.err
}
func (f *fakeKnowledge) SummaryByInvestigation(context.Context, uuid.UUID, string) (knowledge.CaseSummaryView, error) {
	return f.summary, f.err
}

// newFakeBackend is a backend whose stores are fakes: no Postgres.
func newFakeBackend(t *testing.T) *Backend {
	t.Helper()
	return &Backend{
		cfg: BackendConfig{
			Handler:   &fakeCommands{err: aggregate.ErrNotFound},
			Reads:     &fakeReads{},
			Evidence:  &fakeEvidence{err: evidence.ErrNotFound},
			Comms:     &fakeComms{},
			Knowledge: &fakeKnowledge{err: knowledge.ErrNoSummary},
		},
		verifier: testVerifier(t),
	}
}

// send drives one request through the router as a token holder.
func send(t *testing.T, b *Backend, method, path, token, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	b.buildRouter(b.verifier).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestRejectAction_OnFakes: the actor comes from the token and the command names
// the action; a delegate never reaches the handler's command; a failed read is
// a 500, not a 404.
func TestRejectAction_OnFakes(t *testing.T) {
	b := newFakeBackend(t)
	commands := &fakeCommands{}
	reads := &fakeReads{action: aggregate.ActionCurrent{ActionID: uuid.New(), AggregateID: uuid.New()}}
	b.cfg.Handler, b.cfg.Reads = commands, reads
	path := "/api/actions/" + reads.action.ActionID.String() + "/reject"

	if code, body := send(t, b, "POST", path, mintToken(t, nil), `{"reason":"too broad"}`); code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	cmd, ok := commands.cmd.(aggregate.RejectAction)
	if !ok || cmd.ActionID != reads.action.ActionID || cmd.Reason != "too broad" {
		t.Errorf("command = %+v", commands.cmd)
	}
	if a := commands.env.Actor; a.PrincipalID != "test-subject" || a.Kind != aggregate.ActorHuman {
		t.Errorf("actor = %+v", a)
	}

	commands.cmd = nil
	delegate := mintToken(t, map[string]any{"delegate_kind": "claude"})
	if code, _ := send(t, b, "POST", path, delegate, "{}"); code != http.StatusForbidden || commands.cmd != nil {
		t.Errorf("delegate: status %d, command %+v", code, commands.cmd)
	}

	reads.actionErr = errStoreDown
	if code, _ := send(t, b, "POST", path, mintToken(t, nil), "{}"); code != http.StatusInternalServerError {
		t.Errorf("read failure: status %d; want 500", code)
	}
}

// TestGetEvidence_OnFakes: each way the store can answer maps to its status.
func TestGetEvidence_OnFakes(t *testing.T) {
	b := newFakeBackend(t)
	store := &fakeEvidence{object: evidence.Object{Ref: "note--1", Type: "note", Payload: []byte(`{"id":"note--1"}`)}}
	b.cfg.Evidence = store

	for err, want := range map[error]int{
		nil:                      http.StatusOK,
		evidence.ErrNotFound:     http.StatusNotFound,
		evidence.ErrMalformedRef: http.StatusBadRequest,
		errStoreDown:             http.StatusInternalServerError,
	} {
		store.err = err
		if code, body := send(t, b, "GET", "/api/evidence/note--1", mintToken(t, nil), ""); code != want {
			t.Errorf("store answers %v: status %d; want %d: %s", err, code, want, body)
		}
	}
}

// TestCommsAck_OnFakes: the act is the token's subject's on the path's thread,
// and a refused act is a 422.
func TestCommsAck_OnFakes(t *testing.T) {
	b := newFakeBackend(t)
	store := &fakeComms{}
	b.cfg.Comms = store
	id := uuid.New()

	if code, body := send(t, b, "POST", "/api/comms/"+id.String()+"/ack", mintToken(t, nil), ""); code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	if store.thread != id || store.principal != "test-subject" {
		t.Errorf("act on %s by %q", store.thread, store.principal)
	}

	store.err = errStoreDown
	if code, _ := send(t, b, "POST", "/api/comms/"+id.String()+"/done", mintToken(t, nil), ""); code != http.StatusUnprocessableEntity {
		t.Errorf("refused act: status %d; want 422", code)
	}
}

// TestGetCaseSummary_OnFakes: a summary not written yet is the 404 clients retry
// on, and a store failure is not.
func TestGetCaseSummary_OnFakes(t *testing.T) {
	b := newFakeBackend(t)
	store := &fakeKnowledge{}
	b.cfg.Knowledge = store

	for err, want := range map[error]int{
		nil:                    http.StatusOK,
		knowledge.ErrNoSummary: http.StatusNotFound,
		errStoreDown:           http.StatusInternalServerError,
	} {
		store.err = err
		if code, body := send(t, b, "GET", "/api/knowledge/case_summaries/r1", mintToken(t, nil), ""); code != want {
			t.Errorf("store answers %v: status %d; want %d: %s", err, code, want, body)
		}
	}
}

// TestNewBackend_HandlerAndReadsTogether: handlers take a non-nil Handler to
// mean the aggregate is there, reads included, so a backend with one and not
// the other stops at construction rather than panicking on a request or in a
// sweep (adr/0226).
func TestNewBackend_HandlerAndReadsTogether(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg   BackendConfig
		wants bool
	}{
		"neither":      {BackendConfig{}, false},
		"both":         {BackendConfig{Handler: &fakeCommands{}, Reads: &fakeReads{}}, false},
		"handler only": {BackendConfig{Handler: &fakeCommands{}}, true},
		"reads only":   {BackendConfig{Reads: &fakeReads{}}, true},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if got := recover() != nil; got != tc.wants {
					t.Errorf("panicked = %v; want %v", got, tc.wants)
				}
			}()
			NewBackend(tc.cfg, nil)
		})
	}
}
