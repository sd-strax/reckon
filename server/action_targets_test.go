package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/stix"
)

// testWriteAdapter accepts every dispatch; the request tests never dispatch.
type testWriteAdapter struct{}

func (testWriteAdapter) Name() string                   { return "test_write" }
func (testWriteAdapter) Class() capability.AdapterClass { return capability.ClassCustom }
func (testWriteAdapter) SupportedActionOps() []string   { return []string{"op"} }
func (testWriteAdapter) Health() capability.HealthStatus {
	return capability.HealthStatus{Healthy: true}
}
func (testWriteAdapter) Dispatch(context.Context, string, map[string]any, string) (action.WriteResult, error) {
	return action.WriteResult{FinalOutcome: action.OutcomeSucceeded}, nil
}

// testIdentifierRule is the identifier rule of testActionResolver's typed
// bindings: the fake store's entities carry their identifier as name.
const testIdentifierRule = "${target.entity.name}"

// testActionResolver binds every catalog type to test_write. A typed
// binding derives each target's identifier by testIdentifierRule.
func testActionResolver(t testing.TB, cat *action.ActionCatalog) *action.ActionResolver {
	t.Helper()
	bindings := map[string][]action.ActionBinding{}
	for _, at := range cat.ActionTypes() {
		d, _ := cat.Descriptor(at)
		b := action.ActionBinding{ActionType: at, Adapter: "test_write", Operation: "op", Priority: 100,
			Params: map[string]any{"target": "${target.resolved_identifier}"}}
		if len(d.TargetTypes) > 0 {
			b.Identifier = map[string]string{}
			for _, tt := range d.TargetTypes {
				b.Identifier[tt] = testIdentifierRule
			}
		}
		bindings[at] = []action.ActionBinding{b}
	}
	if err := action.ValidateActionBindings(bindings, cat); err != nil {
		t.Fatalf("test bindings: %v", err)
	}
	return action.NewActionResolver(bindings, map[string]action.WriteAdapter{"test_write": testWriteAdapter{}})
}

// fakeTargetStore holds target entities for the request tests. An entity it
// was not given is found with its ref as its name, so a request for any
// well-typed ref plans; one listed in missing is not in the evidence. agents
// are the host identity view's device ids by ref.
type fakeTargetStore struct {
	mu       sync.Mutex
	entities map[string]map[string]any
	agents   map[string][]evidence.SourceID
	missing  map[string]bool
	// deviceErr, when set, fails HostDeviceIDs.
	deviceErr error
}

func newFakeTargetStore() *fakeTargetStore {
	return &fakeTargetStore{
		entities: map[string]map[string]any{
			"reckon-host--1": {"name": "WIN-FILE01"},
			"reckon-host--9": {"name": "WIN-9"},
		},
		agents:  map[string][]evidence.SourceID{},
		missing: map[string]bool{},
	}
}

// set gives ref its stored properties.
func (f *fakeTargetStore) set(ref string, props map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entities[ref] = props
}

// Object returns ref's entity as a stored STIX object: its properties at the
// top level, the custom ones in the type's property-extension.
func (f *fakeTargetStore) Object(_ context.Context, ref string) (evidence.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	typ, _, ok := strings.Cut(ref, "--")
	if !ok {
		return evidence.Object{}, evidence.ErrMalformedRef
	}
	if f.missing[ref] {
		return evidence.Object{}, evidence.ErrNotFound
	}
	props, ok := f.entities[ref]
	if !ok {
		props = map[string]any{"name": ref}
	}
	obj := map[string]any{"type": typ, "id": ref, "spec_version": "2.1"}
	for k, v := range props {
		obj[k] = v
	}
	payload, err := json.Marshal(obj)
	if err != nil {
		return evidence.Object{}, err
	}
	return evidence.Object{Ref: ref, Type: typ, Payload: payload}, nil
}

// HostDeviceIDs returns ref's device ids by source.
func (f *fakeTargetStore) HostDeviceIDs(_ context.Context, ref string) ([]evidence.SourceID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deviceErr != nil {
		return nil, f.deviceErr
	}
	return f.agents[ref], nil
}

// TestLoadTargetEntities_HostVendorIDs (adr/0406): a host's vendor ids come
// from the device ids its records reported (HostDeviceIDs), so a Falcon device id that only a later Falcon
// record carried is found though the stored host keeps its first source's
// ids; a weak host known only by its Falcon id has it too; two disagreeing
// Falcon ids leave falcon out with the conflict kept; a host no Falcon
// source reported has none.
func TestLoadTargetEntities_HostVendorIDs(t *testing.T) {
	store := newFakeTargetStore()
	store.set("reckon-host--a", map[string]any{"hostname": "WIN-FILE01", "source_ids": map[string]any{"fixture:s": "edr-01"}})
	store.agents["reckon-host--a"] = []evidence.SourceID{{Tool: "fixture:s", UID: "edr-01"}, {Tool: "falcon:search_hosts", UID: "aid-1"}}
	store.set("reckon-host--weak", map[string]any{"source_tool": "falcon:search_hosts", "device_uid": "aid-2", "weak": true})
	store.agents["reckon-host--weak"] = []evidence.SourceID{{Tool: "falcon:search_hosts", UID: "aid-2"}}
	store.set("reckon-host--two", map[string]any{"hostname": "WIN-APP02"})
	store.agents["reckon-host--two"] = []evidence.SourceID{{Tool: "falcon:search_hosts", UID: "aid-old"}, {Tool: "falcon:get_host_details", UID: "aid-new"}}
	store.set("reckon-host--none", map[string]any{"hostname": "BKP-01"})

	targets := []aggregate.TargetSpec{{EntityRef: "reckon-host--a"}, {EntityRef: "reckon-host--weak"}, {EntityRef: "reckon-host--two"}, {EntityRef: "reckon-host--none"}}
	ents, status, msg := loadTargetEntities(context.Background(), store, "host.isolate", targets, true)
	if status != 0 {
		t.Fatalf("load: %d %s", status, msg)
	}
	falcon := func(i int) any { return ents[i].Properties[action.VendorIDsKey].(map[string]any)["falcon"] }
	if falcon(0) != "aid-1" {
		t.Errorf("host seen by Falcon later: vendor_ids.falcon = %v; want aid-1", falcon(0))
	}
	if ents[0].Properties["hostname"] != "WIN-FILE01" {
		t.Errorf("stored properties not loaded: %+v", ents[0].Properties)
	}
	if falcon(1) != "aid-2" {
		t.Errorf("weak host: vendor_ids.falcon = %v; want aid-2", falcon(1))
	}
	if falcon(2) != nil || len(ents[2].VendorConflicts["falcon"]) != 2 {
		t.Errorf("disagreeing ids: vendor_ids.falcon = %v, conflicts %v; want falcon left out and both ids kept", falcon(2), ents[2].VendorConflicts)
	}
	if falcon(3) != nil {
		t.Errorf("host no Falcon source reported: vendor_ids.falcon = %v; want none", falcon(3))
	}
}

// TestLoadTargetEntities_StoredSourceIDs: an entity other than a host reads
// its vendor ids from its stored source_ids, and its property-extension
// properties are loaded beside its own.
func TestLoadTargetEntities_StoredSourceIDs(t *testing.T) {
	ext := stix.PropertyExtensionOf(identity.TypeUserAccount)
	if ext == "" {
		t.Skip("user-account has no property-extension")
	}
	payload, err := json.Marshal(map[string]any{
		"type": identity.TypeUserAccount, "id": "user-account--1", "spec_version": "2.1",
		"account_login": `CONTOSO\jdoe`,
		"extensions": map[string]any{stix.Key(ext): map[string]any{
			"extension_type": "property-extension", "domain": "CONTOSO",
			"source_ids": map[string]any{"okta:get_user": "00u1"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	props, err := entityProperties(evidence.Object{Ref: "user-account--1", Type: identity.TypeUserAccount, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if props["account_login"] != `CONTOSO\jdoe` || props["domain"] != "CONTOSO" {
		t.Errorf("props = %+v; want the login and the extension's domain", props)
	}
	if _, ok := props["extensions"]; ok {
		t.Errorf("extensions kept as a property: %+v", props)
	}
	vendorIDs, _ := action.VendorIDs(storedSourceIDs(props))
	if vendorIDs["okta"] != "00u1" {
		t.Errorf("vendor_ids = %v; want okta 00u1", vendorIDs)
	}
}

// TestLoadTargetEntities_NotInEvidence: a target whose entity the evidence
// does not hold, or whose uuid holds another type's object, is refused with
// 422.
func TestLoadTargetEntities_NotInEvidence(t *testing.T) {
	store := newFakeTargetStore()
	store.missing["reckon-host--gone"] = true
	for _, ref := range []string{"reckon-host--gone", "not-a-ref"} {
		_, status, msg := loadTargetEntities(context.Background(), store, "host.isolate", []aggregate.TargetSpec{{EntityRef: ref}}, true)
		if status != http.StatusUnprocessableEntity || !strings.Contains(msg, "not an entity in the evidence") {
			t.Errorf("%s: %d %q; want 422 naming it", ref, status, msg)
		}
	}
}

// TestPlanAction_DeviceIDsOnlyWhenRead (adr/0406): a host's device ids are
// read only when a binding that could be chosen reads vendor_ids. With a
// hostname rule, a store whose device-id read fails still plans; with a
// vendor_ids rule the read is made, and its failure fails the request.
func TestPlanAction_DeviceIDsOnlyWhenRead(t *testing.T) {
	store := newFakeTargetStore()
	store.set("reckon-host--1", map[string]any{"hostname": "WIN-FILE01"})
	store.deviceErr = errors.New("device ids unavailable")
	backend := func(rule string) *Backend {
		return &Backend{
			cfg: BackendConfig{
				ActionCatalog: defaultActionCatalog(t),
				ActionResolver: action.NewActionResolver(map[string][]action.ActionBinding{
					"host.isolate": {{ActionType: "host.isolate", Adapter: "test_write", Operation: "op", Priority: 100,
						Identifier: map[string]string{identity.TypeHost: rule},
						Params:     map[string]any{"host": "${target.resolved_identifier}"}}},
				}, map[string]action.WriteAdapter{"test_write": testWriteAdapter{}}),
			},
			targetEntities: store,
		}
	}
	cmd := aggregate.RequestAction{ActionType: "host.isolate", Targets: []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}}}
	if status, msg := backend("${target.entity.hostname}").planAction(context.Background(), &cmd, ""); status != 0 {
		t.Fatalf("hostname rule: %d %s; want planned without the device-id read", status, msg)
	}
	if cmd.Targets[0].ResolvedIdentifier != "WIN-FILE01" {
		t.Errorf("identifier = %q; want WIN-FILE01", cmd.Targets[0].ResolvedIdentifier)
	}

	cmd = aggregate.RequestAction{ActionType: "host.isolate", Targets: []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}}}
	if status, _ := backend("${target.entity.vendor_ids.falcon}").planAction(context.Background(), &cmd, ""); status != http.StatusInternalServerError {
		t.Errorf("vendor_ids rule with a failing read: status %d; want 500", status)
	}
}

// ticketBody is a ticket.create request to the IT-OPS queue.
func ticketBody(invID string) RequestActionBody {
	return RequestActionBody{
		ActionType: "ticket.create", InvestigationRef: invID, Rationale: "hand off",
		Targets:      []aggregate.TargetSpec{{ResolvedIdentifier: "IT-OPS"}},
		Parameters:   json.RawMessage(`{"summary":"Reimage"}`),
		EvidenceRefs: []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"},
	}
}

// countActions is how many actions the investigation has recorded.
func countActions(t *testing.T, invID uuid.UUID) int {
	t.Helper()
	acts, err := testHandler.Reads().ListActionCurrents(context.Background(), invID)
	if err != nil {
		t.Fatal(err)
	}
	return len(acts)
}

// TestRequestAction_NotConfiguredRecordsNothing (adr/0406): with no write
// bindings, or no evidence store for a typed action, a request is refused
// with 503 and nothing is recorded; an edit with no write bindings is refused
// with 503 and the parameters stand.
func TestRequestAction_NotConfiguredRecordsNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	isolate := RequestActionBody{
		ActionType: "host.isolate", Targets: []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}}, Rationale: "contain",
		EvidenceRefs: []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"}, InvestigationRef: invID.String(),
	}

	noBindings := actionBackend(t)
	noBindings.cfg.ActionResolver = nil
	if status, body := postRaw(t, noBindings, "/api/actions", isolate); status != http.StatusServiceUnavailable {
		t.Errorf("no write bindings: %d %s; want 503", status, body)
	}

	noEvidence := actionBackend(t)
	noEvidence.targetEntities = nil
	noEvidence.cfg.Evidence = nil
	if status, body := postRaw(t, noEvidence, "/api/actions", isolate); status != http.StatusServiceUnavailable {
		t.Errorf("no evidence store: %d %s; want 503", status, body)
	}
	if n := countActions(t, invID); n != 0 {
		t.Fatalf("%d actions recorded by refused requests; want none", n)
	}

	b := actionBackend(t)
	resp, out := postAction(t, b, mintToken(t, nil), ticketBody(invID.String()))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("request: %d", resp.StatusCode)
	}
	b.cfg.ActionResolver = nil
	if r, _ := postActionDecision(t, b, mintToken(t, nil), out.ActionID, "edit", map[string]any{"parameters": map[string]any{"summary": "changed"}}); r.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("edit with no write bindings: %d; want 503", r.StatusCode)
	}
	ac, err := testHandler.Reads().LoadActionCurrent(context.Background(), uuid.MustParse(out.ActionID))
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]any
	_ = json.Unmarshal(ac.Parameters, &params)
	if params["summary"] != "Reimage" || !ac.EditedAt.IsZero() {
		t.Errorf("after a refused edit: parameters %s, edited %v; want them unchanged", ac.Parameters, ac.EditedAt)
	}
}

// TestRequestAction_VendorConflictRefused (adr/0406): a host Falcon reported
// under two device ids is refused through POST /api/actions with 422 naming
// the conflict, and nothing is recorded.
func TestRequestAction_VendorConflictRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t)
	b.cfg.ActionResolver = action.NewActionResolver(map[string][]action.ActionBinding{
		"host.isolate": {{ActionType: "host.isolate", Adapter: "test_write", Operation: "op", Priority: 100,
			Identifier: map[string]string{identity.TypeHost: "${target.entity.vendor_ids.falcon}"},
			Params:     map[string]any{"device_id": "${target.resolved_identifier}"}}},
	}, map[string]action.WriteAdapter{"test_write": testWriteAdapter{}})
	store := b.targetEntities.(*fakeTargetStore)
	store.agents["reckon-host--1"] = []evidence.SourceID{{Tool: "falcon:search_hosts", UID: "aid-old"}, {Tool: "falcon:search_hosts", UID: "aid-new"}}

	status, body := postRaw(t, b, "/api/actions", RequestActionBody{
		ActionType: "host.isolate", Targets: []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}}, Rationale: "contain",
		EvidenceRefs: []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"}, InvestigationRef: invID.String(),
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "aid-new") || !strings.Contains(body, "aid-old") {
		t.Errorf("conflicting Falcon ids: %d %s; want 422 naming both", status, body)
	}
	if n := countActions(t, invID); n != 0 {
		t.Errorf("%d actions recorded; want none", n)
	}
}

// TestRerequestAction_KeepsTheDestination (adr/0406): a re-request of a
// destination action (ticket, notify) keeps the requester's destination; only
// a typed action derives its identifier afresh.
func TestRerequestAction_KeepsTheDestination(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t)
	resp, orig := postAction(t, b, mintToken(t, nil), ticketBody(invID.String()))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("request: %d", resp.StatusCode)
	}
	origID := uuid.MustParse(orig.ActionID)
	systemHandle(t, invID, aggregate.ExpireAction{ActionID: origID})

	status, body := postRaw(t, b, "/api/actions/"+orig.ActionID+"/rerequest", RerequestBody{Rationale: "still warranted"})
	if status != http.StatusCreated {
		t.Fatalf("rerequest: %d %s", status, body)
	}
	var out RequestActionResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	nc, err := testHandler.Reads().LoadActionCurrent(context.Background(), uuid.MustParse(out.ActionID))
	if err != nil {
		t.Fatal(err)
	}
	if len(nc.Targets) != 1 || nc.Targets[0].ResolvedIdentifier != "IT-OPS" || nc.Binding != testFrozenBinding {
		t.Errorf("re-requested ticket: targets %+v via %+v; want IT-OPS via %+v", nc.Targets, nc.Binding, testFrozenBinding)
	}
}
