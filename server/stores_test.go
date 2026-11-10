package server

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/sd-strax/reckon/dependency"
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

// fakeReads finds nothing, except the action and the investigation it is given.
type fakeReads struct {
	action        aggregate.ActionCurrent
	actionErr     error
	investigation aggregate.InvestigationCurrent
	citations     []aggregate.KnowledgeCitation
	narrative     aggregate.Narrative
	counts        map[string]aggregate.CitationCount
	citationsErr  error
	appearances   map[string][]aggregate.RefAppearance
}

func (f *fakeReads) ListKnowledgeCitations(context.Context, string) ([]aggregate.KnowledgeCitation, error) {
	return f.citations, f.citationsErr
}
func (f *fakeReads) CitationCounts(context.Context, []string) (map[string]aggregate.CitationCount, error) {
	return f.counts, f.citationsErr
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
func (f *fakeReads) Narrative(context.Context, aggregate.AggregateID) (aggregate.Narrative, error) {
	if f.narrative.Current.Revision == 0 {
		return aggregate.Narrative{}, aggregate.ErrNoRecord
	}
	return f.narrative, nil
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
func (f *fakeReads) ListRefAppearances(_ context.Context, ref string) ([]aggregate.RefAppearance, error) {
	return f.appearances[ref], nil
}
func (*fakeReads) ListStrandedActionCurrents(context.Context) ([]aggregate.ActionCurrent, error) {
	return nil, nil
}
func (*fakeReads) LoadInterpretation(context.Context, uuid.UUID) (aggregate.InterpretationRecorded, error) {
	return aggregate.InterpretationRecorded{}, aggregate.ErrNoRecord
}
func (f *fakeReads) LoadInvestigationCurrent(_ context.Context, id aggregate.AggregateID) (aggregate.InvestigationCurrent, error) {
	if f.investigation.AggregateID != id {
		return aggregate.InvestigationCurrent{}, aggregate.ErrNoRecord
	}
	return f.investigation, nil
}
func (*fakeReads) Thread(context.Context, aggregate.AggregateID) ([]aggregate.ThreadAct, error) {
	return nil, nil
}
func (*fakeReads) TranscriptBody(context.Context, uuid.UUID, string) ([]byte, error) {
	return nil, aggregate.ErrNoRecord
}

// fakeEvidence answers every ref with object and err; a host reads as
// identity, its alias set as aliases (the ref alone when nil).
type fakeEvidence struct {
	object   evidence.Object
	identity evidence.HostIdentityView
	aliases  []string
	err      error
}

func (f *fakeEvidence) Persist(context.Context, evidence.Call, capability.CapabilityResult) error {
	return f.err
}
func (f *fakeEvidence) InvestigationEvents(context.Context, uuid.UUID, evidence.EventFilter) (evidence.Events, error) {
	return evidence.Events{}, f.err
}
func (f *fakeEvidence) Calls(context.Context, uuid.UUID) ([]evidence.CallRecord, error) {
	return nil, f.err
}
func (f *fakeEvidence) Call(context.Context, uuid.UUID, uuid.UUID) (evidence.CallRecord, error) {
	return evidence.CallRecord{}, f.err
}
func (f *fakeEvidence) Object(context.Context, string) (evidence.Object, error) {
	return f.object, f.err
}
func (f *fakeEvidence) Event(context.Context, uuid.UUID) (evidence.Event, error) {
	return evidence.Event{}, evidence.ErrNotFound
}
func (f *fakeEvidence) EventEntities(context.Context, uuid.UUID) ([]evidence.Object, error) {
	return nil, f.err
}
func (f *fakeEvidence) IndicatorNeighbourhood(context.Context, evidence.Object) (evidence.Neighbourhood, error) {
	return evidence.Neighbourhood{}, f.err
}
func (f *fakeEvidence) AliasSet(_ context.Context, ref string) ([]string, error) {
	if errors.Is(f.err, evidence.ErrNotFound) {
		return []string{ref}, nil // the store never reports a ref unknown: it is its own set
	}
	if f.aliases == nil {
		return []string{ref}, f.err
	}
	return f.aliases, f.err
}
func (f *fakeEvidence) HostIdentity(context.Context, string) (evidence.HostIdentityView, error) {
	return f.identity, f.err
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

// fakeDependencies holds one dependency and records the last act's principal.
type fakeDependencies struct {
	dep       dependency.Dependency
	principal string
	err       error
}

func (f *fakeDependencies) List(context.Context, uuid.UUID) ([]dependency.Dependency, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []dependency.Dependency{f.dep}, nil
}
func (f *fakeDependencies) Get(context.Context, uuid.UUID) (dependency.Dependency, error) {
	return f.dep, f.err
}
func (f *fakeDependencies) Create(_ context.Context, c dependency.Create) (dependency.Dependency, error) {
	f.principal = c.By.ID
	f.dep = dependency.Dependency{ID: uuid.New(), AggregateID: c.AggregateID, Title: c.Title, Owner: c.Owner, Detail: c.Detail, Status: dependency.StatusOpen, CreatedBy: c.By.ID}
	return f.dep, f.err
}
func (f *fakeDependencies) Attach(_ context.Context, _, _ uuid.UUID, by dependency.Actor, _ time.Time) error {
	f.principal = by.ID
	return f.err
}
func (f *fakeDependencies) MarkSent(context.Context, uuid.UUID, time.Time) (int, error) {
	return 0, f.err
}
func (f *fakeDependencies) OpenRides(context.Context) ([]uuid.UUID, error) {
	return nil, f.err
}
func (f *fakeDependencies) Confirm(_ context.Context, _ uuid.UUID, by dependency.Actor, _, _ string, _ time.Time) error {
	f.principal = by.ID
	return f.err
}
func (f *fakeDependencies) Waive(_ context.Context, _ uuid.UUID, by dependency.Actor, _, _ string, _ time.Time) error {
	f.principal = by.ID
	return f.err
}

// fakeKnowledge has one case summary and one SOP, answers an import with
// outcome, and fails everything else with err.
type fakeKnowledge struct {
	summary   knowledge.CaseSummaryView
	summaries []knowledge.CaseSummaryView
	sop       knowledge.SOP
	sops      []knowledge.SOP
	learned   []knowledge.SOP
	revisions knowledge.SOPRevisions
	outline   knowledge.SOPOutline
	section   knowledge.SOPSectionRead
	facets    knowledge.SummaryFacets
	// lastFilter is the filter the last ListSummaries call was given.
	lastFilter knowledge.SummaryFilter
	outcome    knowledge.ImportOutcome
	err        error
}

func (f *fakeKnowledge) ListBySource(context.Context, uuid.UUID, string) ([]knowledge.SOP, error) {
	return f.learned, f.err
}

func (f *fakeKnowledge) Create(context.Context, knowledge.SOP) (uuid.UUID, error) {
	return uuid.Nil, f.err
}
func (f *fakeKnowledge) Get(_ context.Context, _, id uuid.UUID) (knowledge.SOP, error) {
	if f.err != nil {
		return knowledge.SOP{}, f.err
	}
	if id != f.sop.ID {
		return knowledge.SOP{}, knowledge.ErrNotFound
	}
	return f.sop, nil
}
func (f *fakeKnowledge) Import(context.Context, knowledge.SOP) (uuid.UUID, knowledge.ImportOutcome, error) {
	return f.sop.ID, f.outcome, f.err
}
func (f *fakeKnowledge) List(context.Context, uuid.UUID, bool) ([]knowledge.SOP, error) {
	return f.sops, f.err
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
func (f *fakeKnowledge) Reindex(context.Context, uuid.UUID) (int, error) { return 0, f.err }
func (f *fakeKnowledge) Sections(context.Context, uuid.UUID, uuid.UUID, knowledge.SectionPin) (knowledge.SOPOutline, error) {
	return knowledge.SOPOutline{}, f.err
}
func (f *fakeKnowledge) Section(context.Context, uuid.UUID, uuid.UUID, int, knowledge.SectionPin) (knowledge.SOPSectionRead, error) {
	return knowledge.SOPSectionRead{}, f.err
}
func (f *fakeKnowledge) Revisions(context.Context, uuid.UUID, uuid.UUID) (knowledge.SOPRevisions, error) {
	return f.revisions, f.err
}
func (f *fakeKnowledge) RevisionSections(context.Context, uuid.UUID, uuid.UUID, int, string) (knowledge.SOPOutline, error) {
	return f.outline, f.err
}
func (f *fakeKnowledge) RevisionSection(context.Context, uuid.UUID, uuid.UUID, int, int, string) (knowledge.SOPSectionRead, error) {
	return f.section, f.err
}
func (f *fakeKnowledge) WriteSummary(context.Context, uuid.UUID, knowledge.Summary) (uuid.UUID, error) {
	return uuid.Nil, f.err
}
func (f *fakeKnowledge) EnrichSummary(context.Context, uuid.UUID, string, string, string) (uuid.UUID, error) {
	return uuid.Nil, f.err
}
func (f *fakeKnowledge) ListSummaries(_ context.Context, _ uuid.UUID, filter knowledge.SummaryFilter) (knowledge.SummaryList, error) {
	f.lastFilter = filter
	return knowledge.SummaryList{Summaries: f.summaries, Total: len(f.summaries)}, f.err
}
func (f *fakeKnowledge) CaseSummaryFacets(context.Context, uuid.UUID) (knowledge.SummaryFacets, error) {
	return f.facets, f.err
}
func (f *fakeKnowledge) SummaryByInvestigation(context.Context, uuid.UUID, string) (knowledge.CaseSummaryView, error) {
	return f.summary, f.err
}

// newFakeBackend is a backend whose stores are fakes: no Postgres.
func newFakeBackend(t *testing.T) *Backend {
	t.Helper()
	return &Backend{
		cfg: BackendConfig{
			Handler:      &fakeCommands{err: aggregate.ErrNotFound},
			Reads:        &fakeReads{},
			Evidence:     &fakeEvidence{err: evidence.ErrNotFound},
			Comms:        &fakeComms{},
			Dependencies: &fakeDependencies{},
			Knowledge:    &fakeKnowledge{err: knowledge.ErrNoSummary},
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

// TestGetEvidence_HostIdentity: an x-host reads with its identity view in the
// API's shape (adr/0211); any other object carries none.
func TestGetEvidence_HostIdentity(t *testing.T) {
	b := newFakeBackend(t)
	at := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	store := &fakeEvidence{
		object: evidence.Object{Ref: "x-host--1", Type: "x-host", Payload: []byte(`{"id":"x-host--1"}`)},
		identity: evidence.HostIdentityView{
			Aliases: []evidence.IdentityEdge{{EdgeID: "relationship--a", OtherRef: "x-host--2", Kind: evidence.IdentityObserved, Source: "edr", At: at}},
			Distinct: []evidence.IdentityEdge{{EdgeID: "relationship--d", OtherRef: "x-host--3", Kind: evidence.IdentityAsserted,
				By: "u-1", ByName: "Ana Analyst", At: at, Note: "two machines", Withdrawn: true}},
			SeenAs: evidence.SeenAs{Domains: []string{"corp", "corp.example"}, Agents: []evidence.SourceID{{Tool: "edr", UID: "a-7"}}},
		},
	}
	b.cfg.Evidence = store

	code, body := send(t, b, "GET", "/api/evidence/x-host--1", mintToken(t, nil), "")
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	var got struct {
		Identity *struct {
			Aliases  []map[string]any `json:"aliases"`
			Distinct []map[string]any `json:"distinct"`
			SeenAs   struct {
				Domains []string            `json:"domains"`
				Agents  []map[string]string `json:"agents"`
				IPs     []string            `json:"ips"`
			} `json:"seen_as"`
		} `json:"identity"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil || got.Identity == nil {
		t.Fatalf("no identity on a host: %s (%v)", body, err)
	}
	id := got.Identity
	if len(id.Aliases) != 1 || id.Aliases[0]["edge_id"] != "relationship--a" || id.Aliases[0]["other_ref"] != "x-host--2" ||
		id.Aliases[0]["kind"] != "observed" || id.Aliases[0]["source"] != "edr" || id.Aliases[0]["withdrawn"] != false {
		t.Errorf("aliases = %v", id.Aliases)
	}
	if len(id.Distinct) != 1 || id.Distinct[0]["kind"] != "asserted" || id.Distinct[0]["by_name"] != "Ana Analyst" ||
		id.Distinct[0]["note"] != "two machines" || id.Distinct[0]["withdrawn"] != true || id.Distinct[0]["at"] != "2026-01-03T00:00:00Z" {
		t.Errorf("distinct = %v", id.Distinct)
	}
	if len(id.SeenAs.Domains) != 2 || len(id.SeenAs.Agents) != 1 || id.SeenAs.Agents[0]["tool"] != "edr" || id.SeenAs.Agents[0]["uid"] != "a-7" || id.SeenAs.IPs == nil {
		t.Errorf("seen_as = %+v", id.SeenAs)
	}

	store.err = errStoreDown
	if code, _ := send(t, b, "GET", "/api/evidence/x-host--1", mintToken(t, nil), ""); code != http.StatusInternalServerError {
		t.Errorf("store down: status %d; want 500", code)
	}

	store.err = nil
	store.object = evidence.Object{Ref: "note--1", Type: "note", Payload: []byte(`{"id":"note--1"}`)}
	if _, body := send(t, b, "GET", "/api/evidence/note--1", mintToken(t, nil), ""); strings.Contains(body, `"identity"`) {
		t.Errorf("a note carries an identity: %s", body)
	}
}

// TestRefAppearances_FollowAliases: a host's appearances are the union over
// its alias set, one row per investigation (adr/0211).
func TestRefAppearances_FollowAliases(t *testing.T) {
	b := newFakeBackend(t)
	inv1, inv2 := uuid.New(), uuid.New()
	day := func(d int) sql.NullTime {
		return sql.NullTime{Time: time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC), Valid: true}
	}
	b.cfg.Reads = &fakeReads{appearances: map[string][]aggregate.RefAppearance{
		"x-host--strong": {{AggregateID: inv1, Title: "one", FirstSeen: day(2), LastSeen: day(3), Mentions: 2}},
		"x-host--weak": {
			{AggregateID: inv2, Title: "two", FirstSeen: day(5), LastSeen: day(6), Mentions: 1},
			{AggregateID: inv1, Title: "one", FirstSeen: day(1), LastSeen: day(4), Mentions: 1},
		},
	}}
	b.cfg.Evidence = &fakeEvidence{aliases: []string{"x-host--strong", "x-host--weak"}}

	code, body := send(t, b, "GET", "/api/entities/x-host--strong/appearances", mintToken(t, nil), "")
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	var got struct {
		Ref         string           `json:"ref"`
		Appearances []AppearanceView `json:"appearances"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Ref != "x-host--strong" || len(got.Appearances) != 2 {
		t.Fatalf("appearances = %+v", got)
	}
	first, second := got.Appearances[0], got.Appearances[1]
	if first.InvestigationID != inv2.String() || second.InvestigationID != inv1.String() || second.Mentions != 3 ||
		!second.FirstSeen.Equal(day(1).Time) || !second.LastSeen.Equal(day(4).Time) {
		t.Errorf("merged = %+v, %+v; want inv2 then inv1 with 3 mentions over days 1..4", first, second)
	}

	// An evidence store error degrades to the ref alone, never a 500.
	b.cfg.Evidence = &fakeEvidence{err: errStoreDown}
	code, body = send(t, b, "GET", "/api/entities/x-host--strong/appearances", mintToken(t, nil), "")
	if err := json.Unmarshal([]byte(body), &got); code != http.StatusOK || err != nil || len(got.Appearances) != 1 || got.Appearances[0].Mentions != 2 {
		t.Errorf("evidence store down: status %d, %+v, %v; want the ref's own appearances", code, got.Appearances, err)
	}

	// No evidence store: the ref alone, as before.
	b.cfg.Evidence = nil
	_, body = send(t, b, "GET", "/api/entities/x-host--strong/appearances", mintToken(t, nil), "")
	if err := json.Unmarshal([]byte(body), &got); err != nil || len(got.Appearances) != 1 || got.Appearances[0].Mentions != 2 {
		t.Errorf("without an evidence store = %+v, %v", got.Appearances, err)
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

// TestDependencies_OnFakes: no store is a 503 on every route (the resolve
// routes included, whose method values would panic on a nil store); a create
// is the token subject's act on an open investigation, and refused for an AI
// delegate, an unknown investigation and a concluded one; the store's refusals
// map to their statuses (resolved 409, invalid 422).
func TestDependencies_OnFakes(t *testing.T) {
	b := newFakeBackend(t)
	inv := uuid.New()
	b.cfg.Reads = &fakeReads{investigation: aggregate.InvestigationCurrent{AggregateID: inv, Status: aggregate.StatusActive}}
	b.cfg.Dependencies = nil
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/investigations/" + inv.String() + "/dependencies"},
		{"POST", "/api/investigations/" + inv.String() + "/dependencies"},
		{"POST", "/api/dependencies/" + uuid.NewString() + "/attach"},
		{"POST", "/api/dependencies/" + uuid.NewString() + "/confirm"},
		{"POST", "/api/dependencies/" + uuid.NewString() + "/waive"},
	} {
		if code, _ := send(t, b, c.method, c.path, mintToken(t, nil), `{"note":"x"}`); code != http.StatusServiceUnavailable {
			t.Errorf("no store: %s %s = %d; want 503", c.method, c.path, code)
		}
	}
	store := &fakeDependencies{}
	b.cfg.Dependencies = store
	if code, _ := send(t, b, "POST", "/api/investigations/"+inv.String()+"/dependencies", mintToken(t, map[string]any{"delegate_kind": "claude"}), `{"title":"rotate","owner":"platform"}`); code != http.StatusForbidden {
		t.Errorf("delegate create: status %d; want 403", code)
	}
	if code, _ := send(t, b, "POST", "/api/investigations/"+uuid.NewString()+"/dependencies", mintToken(t, nil), `{"title":"rotate","owner":"platform"}`); code != http.StatusNotFound {
		t.Errorf("unknown investigation: status %d; want 404", code)
	}
	b.cfg.Reads = &fakeReads{investigation: aggregate.InvestigationCurrent{AggregateID: inv, Status: aggregate.StatusConcluded}}
	if code, _ := send(t, b, "POST", "/api/investigations/"+inv.String()+"/dependencies", mintToken(t, nil), `{"title":"rotate","owner":"platform"}`); code != http.StatusConflict {
		t.Errorf("concluded investigation: status %d; want 409", code)
	}
	b.cfg.Reads = &fakeReads{investigation: aggregate.InvestigationCurrent{AggregateID: inv, Status: aggregate.StatusActive}}
	if code, body := send(t, b, "POST", "/api/investigations/"+inv.String()+"/dependencies", mintToken(t, nil), `{"title":"rotate","owner":"platform"}`); code != http.StatusCreated {
		t.Fatalf("create: status %d: %s", code, body)
	}
	if store.principal != "test-subject" || store.dep.Title != "rotate" {
		t.Errorf("create recorded %q / %q", store.principal, store.dep.Title)
	}
	if code, _ := send(t, b, "POST", "/api/investigations/"+inv.String()+"/dependencies", mintToken(t, nil), `{"title":"rotate"}`); code != http.StatusBadRequest {
		t.Errorf("create without owner: status %d; want 400", code)
	}
	store.err = dependency.ErrResolved
	if code, _ := send(t, b, "POST", "/api/dependencies/"+store.dep.ID.String()+"/confirm", mintToken(t, nil), `{"note":"done"}`); code != http.StatusConflict {
		t.Errorf("resolved: status %d; want 409", code)
	}
	store.err = dependency.ErrInvalid
	if code, _ := send(t, b, "POST", "/api/dependencies/"+store.dep.ID.String()+"/waive", mintToken(t, nil), `{}`); code != http.StatusUnprocessableEntity {
		t.Errorf("invalid: status %d; want 422", code)
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

// TestReindexKnowledge_OnFakes: the POST starts a run and answers 202, the GET
// reports it until it is done or failed, and a second POST while running does
// not start another.
func TestReindexKnowledge_OnFakes(t *testing.T) {
	b := newFakeBackend(t)
	store := &fakeKnowledge{}
	b.cfg.Knowledge = store
	analyst := mintToken(t, nil) // the default claims carry the analyst role

	if code, body := send(t, b, "GET", "/api/knowledge/reindex", analyst, ""); code != http.StatusOK || !strings.Contains(body, `"state":"idle"`) {
		t.Fatalf("before any run: %d %s", code, body)
	}
	code, body := send(t, b, "POST", "/api/knowledge/reindex", analyst, "")
	if code != http.StatusAccepted || !strings.Contains(body, `"state":"running"`) {
		t.Fatalf("start: %d %s", code, body)
	}
	var started ReindexJob
	if err := json.Unmarshal([]byte(body), &started); err != nil || started.Run == "" {
		t.Fatalf("the POST names no run: %v %s", err, body)
	}
	awaitState := func(want string) ReindexJob {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			_, body := send(t, b, "GET", "/api/knowledge/reindex", analyst, "")
			var job ReindexJob
			if err := json.Unmarshal([]byte(body), &job); err != nil {
				t.Fatalf("GET: %v %s", err, body)
			}
			if job.State == want {
				return job
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("reindex never reached %s", want)
		return ReindexJob{}
	}
	if job := awaitState("done"); job.Run != started.Run {
		t.Errorf("done state belongs to run %q, polled %q", job.Run, started.Run)
	}

	// A second run has its own id, so a poller of the first cannot take the
	// second's outcome for its own.
	store.err = errStoreDown
	code, body = send(t, b, "POST", "/api/knowledge/reindex", analyst, "")
	if code != http.StatusAccepted {
		t.Fatalf("restart: %d", code)
	}
	var second ReindexJob
	if err := json.Unmarshal([]byte(body), &second); err != nil || second.Run == "" || second.Run == started.Run {
		t.Fatalf("second run should have its own id: %v %s", err, body)
	}
	if job := awaitState("failed"); job.Run != second.Run || !strings.Contains(job.Error, errStoreDown.Error()) {
		t.Errorf("failed run: %+v", job)
	}
}

// TestGetSOP_OnFakes: a missing SOP is the 404 the inspector shows as "no such
// procedure"; a store that cannot answer is a 500 with a reference, never a
// 404 (#294).
func TestGetSOP_OnFakes(t *testing.T) {
	b := newFakeBackend(t)
	store := &fakeKnowledge{sop: knowledge.SOP{ID: uuid.New(), Title: "Containment"}}
	b.cfg.Knowledge = store
	reader := mintToken(t, nil)

	if code, body := send(t, b, "GET", "/api/sops/"+store.sop.ID.String(), reader, ""); code != http.StatusOK || !strings.Contains(body, `"title":"Containment"`) {
		t.Errorf("present: %d %s", code, body)
	}
	if code, body := send(t, b, "GET", "/api/sops/"+uuid.NewString(), reader, ""); code != http.StatusNotFound || !strings.Contains(body, "sop not found") {
		t.Errorf("missing: %d %s", code, body)
	}
	store.err = errStoreDown
	code, body := send(t, b, "GET", "/api/sops/"+store.sop.ID.String(), reader, "")
	if code != http.StatusInternalServerError || strings.Contains(body, "not found") {
		t.Errorf("store down: %d %s; want 500 with a reference", code, body)
	}
}

// TestImportSOP_Outcomes: both import routes answer the store's outcome word;
// a created lineage is 201, a revision and an unchanged re-import 200.
func TestImportSOP_Outcomes(t *testing.T) {
	b := newFakeBackend(t)
	store := &fakeKnowledge{sop: knowledge.SOP{ID: uuid.New()}}
	b.cfg.Knowledge = store
	analyst := mintToken(t, nil)
	payload := `{"title":"Containment","body":"Isolate first.","source":{"url":"file:c.md"}}`
	markdown := `{"filename":"c.md","content":"# Containment\n\nIsolate first.\n"}`

	for outcome, want := range map[knowledge.ImportOutcome]int{
		knowledge.ImportCreated:   http.StatusCreated,
		knowledge.ImportRevised:   http.StatusOK,
		knowledge.ImportUnchanged: http.StatusOK,
	} {
		store.outcome = outcome
		for path, body := range map[string]string{"/api/sops/import": payload, "/api/sops/import_markdown": markdown} {
			code, resp := send(t, b, "POST", path, analyst, body)
			if code != want || !strings.Contains(resp, `"outcome":"`+string(outcome)+`"`) {
				t.Errorf("%s %s: %d %s; want %d with the outcome", path, outcome, code, resp, want)
			}
		}
	}
}
