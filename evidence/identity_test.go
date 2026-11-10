package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
)

// hostSCO is a persisted x-host with a hostname, or a weak one without.
func hostSCO(id identity.STIXID, hostname string) capability.SCO {
	props := map[string]any{"weak": true}
	if hostname != "" {
		props = map[string]any{"hostname": hostname}
	}
	return capability.SCO{ID: id, Type: identity.TypeHost, Properties: props}
}

// observedAlias is the aliases edge a normalizer emits from one record.
func observedAlias(src, tgt identity.STIXID, tool string) capability.Relationship {
	return capability.Relationship{
		ID: stixID("relationship"), Type: capability.RelAliases, SourceRef: src, TargetRef: tgt,
		Provenance: capability.Provenance{DerivationMode: capability.DerivationDirect, Tool: tool, ObservedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
	}
}

// persistObjects writes hosts and edges through Persist, as an invocation
// would.
func persistObjects(t *testing.T, s *Store, scos []capability.SCO, rels ...capability.Relationship) {
	t.Helper()
	persistCall(t, s, aCall(), capability.CapabilityResult{Normalized: []capability.NormalizationResult{{SCOs: scos, Relationships: rels}}})
}

// assertEdge writes an edge as a person's assertion is stored, its payload the
// relationship's fields beside an Assertion, withdrawn when withdrawn is set.
func assertEdge(t *testing.T, relType string, src, tgt identity.STIXID, withdrawn bool) identity.STIXID {
	t.Helper()
	id := stixID("relationship")
	at := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	a := Assertion{
		AssertedBy: "u-1", AssertedByName: "Ana Analyst", AssertedAt: at, Note: "checked the asset register",
		History: []AssertionEvent{{Act: "asserted", By: "u-1", ByName: "Ana Analyst", At: at, Note: "checked the asset register"}},
	}
	if withdrawn {
		w := at.Add(24 * time.Hour)
		a.WithdrawnAt = &w
		a.History = append(a.History, AssertionEvent{Act: "withdrawn", By: "u-1", At: w})
	}
	payload, err := json.Marshal(struct {
		Type      string          `json:"relationship_type"`
		SourceRef identity.STIXID `json:"source_ref"`
		TargetRef identity.STIXID `json:"target_ref"`
		Assertion
	}{relType, src, tgt, a})
	if err != nil {
		t.Fatal(err)
	}
	eid, _ := refUUID(string(id))
	sid, _ := refUUID(string(src))
	tid, _ := refUUID(string(tgt))
	if _, err := testDB.Exec(`INSERT INTO stix_edges (id, source_ref, target_ref, relationship_type, payload, created, modified) VALUES ($1, $2, $3, $4, $5, now(), now())`,
		eid, sid, tid, relType, payload); err != nil {
		t.Fatalf("insert edge: %v", err)
	}
	return id
}

func sameSet(got []string, want ...identity.STIXID) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		if !slices.Contains(got, string(w)) {
			return false
		}
	}
	return true
}

// TestAliasSet_Closure: the undirected closure over a chain, the asked-for ref
// first; a ref with no edges is its own set.
func TestAliasSet_Closure(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b, c, d := stixID("x-host"), stixID("x-host"), stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(a, ""), hostSCO(b, "ws1"), hostSCO(c, ""), hostSCO(d, "ws2")},
		observedAlias(a, b, "edr"), observedAlias(c, b, "ids"))

	for _, from := range []identity.STIXID{a, b, c} {
		got, err := s.AliasSet(ctx, string(from))
		if err != nil {
			t.Fatalf("AliasSet: %v", err)
		}
		if !sameSet(got, a, b, c) || got[0] != string(from) {
			t.Errorf("AliasSet(%s) = %v; want {a, b, c} with it first", from, got)
		}
	}
	if got, _ := s.AliasSet(ctx, string(d)); !sameSet(got, d) {
		t.Errorf("AliasSet(d) = %v; want d alone", got)
	}
	if _, err := s.AliasSet(ctx, "not-a-ref"); !errors.Is(err, ErrMalformedRef) {
		t.Errorf("malformed ref: %v", err)
	}
}

// TestAliasSet_DistinctCut: a live distinct-from separates its pair in either
// direction, however else they connect; a withdrawn one cuts nothing.
func TestAliasSet_DistinctCut(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b, c := stixID("x-host"), stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1"), hostSCO(b, ""), hostSCO(c, "ws1b")},
		observedAlias(b, a, "edr"), observedAlias(b, c, "edr"))

	// Withdrawn: the chain still joins all three.
	assertEdge(t, "distinct-from", c, a, true)
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a, b, c) {
		t.Errorf("a withdrawn distinct-from cut the set: %v", got)
	}

	assertEdge(t, "distinct-from", c, a, false)
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a, b) {
		t.Errorf("AliasSet(a) = %v; want {a, b}: c is asserted distinct from a", got)
	}
	if got, _ := s.AliasSet(ctx, string(c)); !sameSet(got, c, b) {
		t.Errorf("AliasSet(c) = %v; want {c, b}", got)
	}
	// The weak host both claim joins neither: no set holds both ends of a live
	// distinct-from pair and nothing breaks the tie, so b reads alone until
	// the analyst says which machine it is.
	if got, _ := s.AliasSet(ctx, string(b)); !sameSet(got, b) {
		t.Errorf("AliasSet(b) = %v; want b alone", got)
	}
	v, err := s.HostIdentity(ctx, string(b))
	if err != nil || len(v.Aliases) != 2 {
		t.Errorf("b's identity view = %+v, %v; want both aliases listed", v.Aliases, err)
	}

	// A distinct-from on an aliased pair removes that edge from the walk.
	assertEdge(t, "distinct-from", a, b, false)
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a) {
		t.Errorf("AliasSet(a) = %v; want a alone", got)
	}
	if got, _ := s.AliasSet(ctx, string(b)); !sameSet(got, b, c) {
		t.Errorf("AliasSet(b) = %v; want {b, c}", got)
	}
}

// TestAliasSet_WithdrawnAlias: a withdrawn aliases edge joins nothing.
func TestAliasSet_WithdrawnAlias(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b := stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1"), hostSCO(b, "ws2")})
	assertEdge(t, "aliases", a, b, true)
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a) {
		t.Errorf("AliasSet = %v; a withdrawn alias must not join", got)
	}
	assertEdge(t, "aliases", b, a, false)
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a, b) {
		t.Errorf("AliasSet = %v; a live asserted alias joins", got)
	}
}

// TestInvestigationEvents_FollowsAliases: asked for the strong host, the
// events filter and the window around an event include an event the source
// attributed only to the weak host aliased to it.
func TestInvestigationEvents_FollowsAliases(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	inv := uuid.New()
	strong, weak, other := stixID("x-host"), stixID("x-host"), stixID("x-host")
	base := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	e1, n1 := retrieved(1007, "Process Activity", base.Add(time.Minute), strong)
	n1.SCOs = []capability.SCO{hostSCO(strong, "ws1"), hostSCO(weak, "")}
	n1.Relationships = []capability.Relationship{observedAlias(weak, strong, "edr")}
	e2, n2 := retrieved(1007, "Process Activity", base.Add(2*time.Minute), weak)
	e3, n3 := retrieved(1007, "Process Activity", base.Add(3*time.Minute), other)
	n3.SCOs = []capability.SCO{hostSCO(other, "ws2")}
	persistCall(t, s, Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &inv, Verb: "v", StartedAt: base},
		capability.CapabilityResult{Coverage: capability.CoverageComplete, Events: []capability.OcsfEvent{e1, e2, e3}, Normalized: []capability.NormalizationResult{n1, n2, n3}})

	for _, ref := range []identity.STIXID{strong, weak} {
		got, err := s.InvestigationEvents(ctx, inv, EventFilter{Entity: string(ref)})
		if err != nil {
			t.Fatalf("InvestigationEvents: %v", err)
		}
		if got.Total != 2 || len(got.Rows) != 2 || got.Rows[0].ID != e1.ID || got.Rows[1].ID != e2.ID {
			t.Errorf("by %s = %v; want the strong and the weak host's events", ref, times(got.Rows))
		}
	}
	around, err := s.InvestigationEvents(ctx, inv, EventFilter{Around: e1.ID, Limit: 10})
	if err != nil || len(around.Rows) != 2 || around.Rows[1].ID != e2.ID {
		t.Errorf("around = %v, %v; want the anchor and the weak host's event", times(around.Rows), err)
	}
}

// TestHostIdentity: the identity view lists the edges with their origin and
// what the record has seen under the host, across its alias set.
func TestHostIdentity(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	reg := capability.NewRegistry(identity.NewResolver(uuid.MustParse("44444444-4444-4444-8444-444444444444")))
	ids := reg.Identity()
	base := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	event := func(tool string, minute int, device map[string]any) capability.OcsfEvent {
		return capability.OcsfEvent{
			ID: uuid.Must(uuid.NewV7()), ClassUID: 1007, ClassName: "Process Activity", Time: base.Add(time.Duration(minute) * time.Minute),
			RecordedAt: base, SourceTool: tool,
			Payload: map[string]any{"class_uid": 1007, "process": map[string]any{"pid": float64(minute), "name": "a.exe"}, "device": device},
		}
	}
	evts := []capability.OcsfEvent{
		event("forensics-client", 1, map[string]any{"hostname": "WS1", "domain": "CORP", "uid": "C.1", "ip": "10.0.0.5"}),
		event("host-ids", 2, map[string]any{"hostname": "ws1.corp.example", "uid": "007", "agent_list": []any{map[string]any{"uid": "007-sensor"}}}),
		event("host-ids", 3, map[string]any{"uid": "007", "ip": "10.0.0.6"}), // the agent id alone: lands on the weak host
		event("host-ids", 4, map[string]any{"hostname": "ws2", "domain": "OTHER", "uid": "008"}),
	}
	res := capability.CapabilityResult{Coverage: capability.CoverageComplete, Events: evts}
	for _, e := range evts {
		nr, err := reg.Normalize(e)
		if err != nil {
			t.Fatal(err)
		}
		nr.EventID = e.ID
		res.Normalized = append(res.Normalized, nr)
	}
	inv := uuid.New()
	persistCall(t, s, Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &inv, Verb: "v", StartedAt: base}, res)

	strong := ids.Host(identity.HostIdentity{Hostname: "ws1"})
	weakHostAgent := ids.Host(identity.HostIdentity{SourceTool: "host-ids", SourceUID: "007"})
	weakClient := ids.Host(identity.HostIdentity{SourceTool: "forensics-client", SourceUID: "C.1"})
	ws2 := ids.Host(identity.HostIdentity{Hostname: "ws2"})
	asserted := assertEdge(t, "distinct-from", strong, ws2, true)

	v, err := s.HostIdentity(ctx, string(strong))
	if err != nil {
		t.Fatalf("HostIdentity: %v", err)
	}
	if len(v.Aliases) != 2 {
		t.Fatalf("aliases = %+v; want the two observed weak hosts", v.Aliases)
	}
	sources := map[string]string{}
	for _, a := range v.Aliases {
		if a.Kind != IdentityObserved || a.Withdrawn || a.At.IsZero() {
			t.Errorf("alias %+v; want observed, live, timed", a)
		}
		sources[a.OtherRef] = a.Source
	}
	if sources[string(weakHostAgent)] != "host-ids" || sources[string(weakClient)] != "forensics-client" {
		t.Errorf("alias sources = %v", sources)
	}
	if len(v.Distinct) != 1 {
		t.Fatalf("distinct = %+v", v.Distinct)
	}
	d := v.Distinct[0]
	if d.EdgeID != string(asserted) || d.OtherRef != string(ws2) || d.Kind != IdentityAsserted || d.By != "u-1" || d.ByName != "Ana Analyst" ||
		d.Note != "checked the asset register" || !d.Withdrawn || !d.At.Equal(time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("distinct edge = %+v", d)
	}

	// Seen as: both domain forms, every id by source, the address of the
	// records naming ws1; ws2's record and the hostless record contribute
	// nothing a hostname matches.
	want := SeenAs{
		Domains: []string{"corp", "corp.example"},
		Agents:  []SourceID{{"forensics-client", "C.1"}, {"host-ids", "007"}, {"host-ids", "007-sensor"}},
		IPs:     []string{"10.0.0.5"},
	}
	if !slices.Equal(v.SeenAs.Domains, want.Domains) || !slices.Equal(v.SeenAs.Agents, want.Agents) || !slices.Equal(v.SeenAs.IPs, want.IPs) {
		t.Errorf("seen as = %+v; want %+v", v.SeenAs, want)
	}

	// The weak host reads the same seen-as through its alias.
	wv, err := s.HostIdentity(ctx, string(weakHostAgent))
	if err != nil {
		t.Fatal(err)
	}
	if len(wv.Aliases) != 1 || wv.Aliases[0].OtherRef != string(strong) || !slices.Equal(wv.SeenAs.Domains, want.Domains) {
		t.Errorf("weak host view = %+v", wv)
	}
	// The weak host's own event is under the strong host.
	byHost, err := s.InvestigationEvents(ctx, inv, EventFilter{Entity: string(strong)})
	if err != nil || byHost.Total != 3 {
		t.Errorf("events by the strong host = %d, %v; want 3", byHost.Total, err)
	}
}
