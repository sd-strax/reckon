package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"regexp"
	"slices"
	"strings"
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
	// The weak host both claim is contested: it joins neither machine and
	// neither machine reads it, so its events are attributed to no one until
	// the analyst says which; nothing breaks the tie.
	for _, from := range []identity.STIXID{a, b, c} {
		if got, _ := s.AliasSet(ctx, string(from)); !sameSet(got, from) {
			t.Errorf("AliasSet(%s) = %v; want it alone", from, got)
		}
	}
	// The identity views still list every edge, so the dispute is visible.
	for from, want := range map[identity.STIXID]int{a: 1, b: 2, c: 1} {
		v, err := s.HostIdentity(ctx, string(from))
		if err != nil || len(v.Aliases) != want {
			t.Errorf("%s's identity view aliases = %+v, %v; want %d", from, v.Aliases, err, want)
		}
	}
	if v, _ := s.HostIdentity(ctx, string(a)); len(v.Aliases) != 1 || v.Aliases[0].OtherRef != string(b) {
		t.Errorf("a's identity view = %+v; want its alias to b listed", v.Aliases)
	}

	// The analyst says b is not a: b is c's, and reads with it.
	assertEdge(t, "distinct-from", a, b, false)
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a) {
		t.Errorf("AliasSet(a) = %v; want a alone", got)
	}
	if got, _ := s.AliasSet(ctx, string(b)); !sameSet(got, b, c) {
		t.Errorf("AliasSet(b) = %v; want {b, c}", got)
	}
	if got, _ := s.AliasSet(ctx, string(c)); !sameSet(got, c, b) {
		t.Errorf("AliasSet(c) = %v; want {c, b}", got)
	}
}

// TestAliasSet_DisputedSeed: a weak host aliased to both ends of a live
// distinct-from pair is contested whichever end the question starts from: asked
// for itself it reads alone, and it does not carry a third alias across.
func TestAliasSet_DisputedSeed(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	w, a, b, c := stixID("x-host"), stixID("x-host"), stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(w, ""), hostSCO(a, "ws1"), hostSCO(b, "ws2"), hostSCO(c, "ws3")},
		observedAlias(w, a, "edr"), observedAlias(w, b, "edr"), observedAlias(w, c, "edr"))

	// Withdrawn: the four read as one from any of them.
	assertEdge(t, "distinct-from", a, b, true)
	for _, from := range []identity.STIXID{w, a, b, c} {
		if got, _ := s.AliasSet(ctx, string(from)); !sameSet(got, w, a, b, c) || got[0] != string(from) {
			t.Errorf("AliasSet(%s) = %v; want {w, a, b, c} with it first", from, got)
		}
	}

	assertEdge(t, "distinct-from", a, b, false)
	for _, from := range []identity.STIXID{w, a, b, c} {
		if got, _ := s.AliasSet(ctx, string(from)); !sameSet(got, from) {
			t.Errorf("AliasSet(%s) = %v; want it alone", from, got)
		}
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
		// Two weak hosts no record joins: one on a MAC, one on a source's id.
		event("lab-edr", 5, map[string]any{"mac": "AA-BB-CC-DD-EE-FF", "uid": "zz9", "uid_alt": "hw-9", "domain": "LAB", "ip": "10.9.9.9"}),
		event("lab-ids", 6, map[string]any{"uid": "u-1", "ip": "10.8.8.8"}),
		// The MAC host again, its MAC in the dotted form: one host for
		// identity, and matched by seen-as.
		event("lab-switch", 7, map[string]any{"mac": "aabb.ccdd.eeff", "ip": "10.9.9.10"}),
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

	// Seen as: both domain forms, every id by source, and the addresses of the
	// records naming ws1, the one naming it by its agent id alone included;
	// ws2's record contributes nothing.
	want := SeenAs{
		Domains: []string{"corp", "corp.example"},
		Agents:  []SourceID{{"forensics-client", "C.1"}, {"host-ids", "007"}, {"host-ids", "007-sensor"}},
		IPs:     []string{"10.0.0.5", "10.0.0.6"},
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
	// An unjoined weak host shows its own records' facts: matched by its MAC,
	// or by the source's id it was minted on.
	for _, c := range []struct {
		ref  identity.STIXID
		want SeenAs
	}{
		{ids.Host(identity.HostIdentity{MAC: "aa:bb:cc:dd:ee:ff"}), SeenAs{Domains: []string{"lab"}, Agents: []SourceID{{"lab-edr", "hw-9"}, {"lab-edr", "zz9"}}, IPs: []string{"10.9.9.10", "10.9.9.9"}}},
		{ids.Host(identity.HostIdentity{SourceTool: "lab-ids", SourceUID: "u-1"}), SeenAs{Domains: []string{}, Agents: []SourceID{{"lab-ids", "u-1"}}, IPs: []string{"10.8.8.8"}}},
	} {
		got, err := s.HostIdentity(ctx, string(c.ref))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.SeenAs.Domains, c.want.Domains) || !slices.Equal(got.SeenAs.Agents, c.want.Agents) || !slices.Equal(got.SeenAs.IPs, c.want.IPs) {
			t.Errorf("unjoined weak host %s seen as = %+v; want %+v", c.ref, got.SeenAs, c.want)
		}
	}

	// The weak host's own event is under the strong host.
	byHost, err := s.InvestigationEvents(ctx, inv, EventFilter{Entity: string(strong)})
	if err != nil || byHost.Total != 3 {
		t.Errorf("events by the strong host = %d, %v; want 3", byHost.Total, err)
	}
}

// TestHostIdentity_MatchesByFirstTier: a host set lends seen-as only the
// identifiers its hosts were minted on, and a device object is matched on its
// own first tier only. A weak host cut out of a set takes its uid with it, and
// a device object with a hostname (or a MAC) is never matched on its uid.
func TestHostIdentity_MatchesByFirstTier(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	reg := capability.NewRegistry(identity.NewResolver(uuid.MustParse("44444444-4444-4444-8444-444444444444")))
	ids := reg.Identity()
	base := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	event := func(classUID, minute int, payload map[string]any) capability.OcsfEvent {
		payload["class_uid"] = classUID
		return capability.OcsfEvent{
			ID: uuid.Must(uuid.NewV7()), ClassUID: classUID, Time: base.Add(time.Duration(minute) * time.Minute),
			RecordedAt: base, SourceTool: "edr", Payload: payload,
		}
	}
	user := map[string]any{"name": "alice"}
	evts := []capability.OcsfEvent{
		// Two inventories from one tool naming one device id: the weak host on
		// (edr, 007) is aliased to both machines.
		event(5001, 1, map[string]any{"device": map[string]any{"hostname": "hosta", "uid": "007", "ip": "10.0.0.1"}}),
		event(5001, 2, map[string]any{"device": map[string]any{"hostname": "hostc", "uid": "007", "ip": "10.0.0.19"}}),
		// A logon to hosta from hostc: the source endpoint is hostc's fact,
		// whatever uid it carries.
		event(3002, 3, map[string]any{"actor": map[string]any{"user": user},
			"dst_endpoint": map[string]any{"hostname": "hosta", "uid": "007"},
			"src_endpoint": map[string]any{"hostname": "hostc", "uid": "007", "domain": "other.example", "ip": "10.0.0.9"}}),
		// The device id alone: the weak host's own record.
		event(5001, 4, map[string]any{"device": map[string]any{"uid": "007", "ip": "10.0.0.7"}}),
		// A logon to hosta from a hostname-less endpoint with a MAC no host in
		// the set was minted on: matched by MAC only. Its device id is its
		// own: a record with a MAC and a uid aliases the uid's weak host to
		// the MAC host, so a shared 007 here would join the MAC host to the
		// set by the record's own word.
		event(3002, 5, map[string]any{"actor": map[string]any{"user": user},
			"dst_endpoint": map[string]any{"hostname": "hosta"},
			"src_endpoint": map[string]any{"mac": "02:00:00:00:00:02", "uid": "m-2", "ip": "10.0.0.8"}}),
		// The MAC host's own inventory.
		event(5001, 6, map[string]any{"device": map[string]any{"mac": "02:00:00:00:00:02", "uid": "m-2", "ip": "10.0.0.18"}}),
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

	a := ids.Host(identity.HostIdentity{Hostname: "hosta"})
	c := ids.Host(identity.HostIdentity{Hostname: "hostc"})
	w := ids.Host(identity.HostIdentity{SourceTool: "edr", SourceUID: "007"})
	w2 := ids.Host(identity.HostIdentity{MAC: "02:00:00:00:00:02"})

	seen := func(ref identity.STIXID) SeenAs {
		t.Helper()
		v, err := s.HostIdentity(ctx, string(ref))
		if err != nil {
			t.Fatalf("HostIdentity: %v", err)
		}
		return v.SeenAs
	}

	// No assertion yet: the shared device id joins the two machines, so
	// hostc's facts read under hosta. The MAC endpoint is still not matched
	// through the uid.
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a, w, c) {
		t.Fatalf("AliasSet(a) = %v; want {a, w, c}", got)
	}
	if got := seen(a); !slices.Equal(got.IPs, []string{"10.0.0.1", "10.0.0.19", "10.0.0.7", "10.0.0.9"}) || !slices.Contains(got.Domains, "other.example") {
		t.Errorf("joined seen as = %+v; want both machines' facts and the weak host's, not the MAC endpoint's", got)
	}

	// Asserted apart: the weak host is contested and hosta reads alone; the
	// uid it was minted on is no longer hosta's to match on.
	assertEdge(t, "distinct-from", a, c, false)
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a) {
		t.Fatalf("AliasSet(a) = %v; want a alone", got)
	}
	if got := seen(a); !slices.Equal(got.IPs, []string{"10.0.0.1"}) || slices.Contains(got.Domains, "other.example") {
		t.Errorf("seen as after distinct-from = %+v; want hosta's own address only, no other.example", got)
	}

	// The analyst says the weak host is not hostc's: it joins hosta again and
	// its uid matches its own record, but a device object with a hostname or
	// a MAC is matched on that alone, so hostc's inventory and both source
	// endpoints stay out.
	assertEdge(t, "distinct-from", w, c, false)
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a, w) {
		t.Fatalf("AliasSet(a) = %v; want {a, w}", got)
	}
	if got := seen(a); !slices.Equal(got.IPs, []string{"10.0.0.1", "10.0.0.7"}) || slices.Contains(got.Domains, "other.example") {
		t.Errorf("seen as with w joined = %+v; want 10.0.0.1 and 10.0.0.7 only, no other.example", got)
	}

	// The MAC host reads its own inventory, matched by its MAC.
	if got := seen(w2); !slices.Equal(got.IPs, []string{"10.0.0.18"}) {
		t.Errorf("MAC host seen as = %+v; want 10.0.0.18", got)
	}
}

// TestHostIdentity_Placeholders (adr/0211): a placeholder is no discriminator
// in the seen-as view, and a device object's tier skips one as identity.Host
// does: a localhost hostname falls to the MAC, a zero MAC to the uid.
func TestHostIdentity_Placeholders(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	reg := capability.NewRegistry(identity.NewResolver(uuid.MustParse("55555555-5555-4555-8555-555555555555")))
	ids := reg.Identity()
	base := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	event := func(minute int, device map[string]any) capability.OcsfEvent {
		return capability.OcsfEvent{
			ID: uuid.Must(uuid.NewV7()), ClassUID: 5001, Time: base.Add(time.Duration(minute) * time.Minute),
			RecordedAt: base, SourceTool: "edr", Payload: map[string]any{"class_uid": 5001, "device": device},
		}
	}
	evts := []capability.OcsfEvent{
		event(1, map[string]any{"hostname": "WS9", "domain": "-", "ip": "0.0.0.0", "uid": "0"}),
		event(2, map[string]any{"hostname": "ws9", "domain": "corp", "ip": "10.1.1.1"}),
		event(3, map[string]any{"hostname": "localhost", "mac": "02:00:00:00:00:03", "ip": "10.2.2.2"}),
		event(4, map[string]any{"mac": "00:00:00:00:00:00", "uid": "u-9", "ip": "10.3.3.3"}),
		// localhost.localdomain names no host, and its remainder no domain.
		event(5, map[string]any{"hostname": "localhost.localdomain", "mac": "02:00:00:00:00:04", "ip": "10.4.4.4"}),
		// A loopback address in the hostname field names no host either.
		event(6, map[string]any{"hostname": "127.0.0.1", "mac": "02:00:00:00:00:05", "ip": "10.5.5.5"}),
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

	seen := func(ref identity.STIXID) SeenAs {
		t.Helper()
		if ref == "" {
			t.Fatal("no host minted")
		}
		v, err := s.HostIdentity(ctx, string(ref))
		if err != nil {
			t.Fatalf("HostIdentity: %v", err)
		}
		return v.SeenAs
	}
	ws9 := seen(ids.Host(identity.HostIdentity{Hostname: "ws9"}))
	if !slices.Equal(ws9.Domains, []string{"corp"}) || !slices.Equal(ws9.IPs, []string{"10.1.1.1"}) || len(ws9.Agents) != 0 {
		t.Errorf("ws9 seen as = %+v; want domain corp and 10.1.1.1 only, no placeholder", ws9)
	}
	if got := seen(ids.Host(identity.HostIdentity{MAC: "02:00:00:00:00:03"})); !slices.Equal(got.IPs, []string{"10.2.2.2"}) {
		t.Errorf("MAC host behind a localhost hostname seen as = %+v; want 10.2.2.2", got)
	}
	if got := seen(ids.Host(identity.HostIdentity{SourceTool: "edr", SourceUID: "u-9"})); !slices.Equal(got.IPs, []string{"10.3.3.3"}) {
		t.Errorf("uid host behind a zero MAC seen as = %+v; want 10.3.3.3", got)
	}
	if got := seen(ids.Host(identity.HostIdentity{MAC: "02:00:00:00:00:04"})); len(got.Domains) != 0 || !slices.Equal(got.IPs, []string{"10.4.4.4"}) {
		t.Errorf("MAC host behind localhost.localdomain seen as = %+v; want 10.4.4.4 and no domain", got)
	}
	if got := seen(ids.Host(identity.HostIdentity{MAC: "02:00:00:00:00:05"})); !slices.Equal(got.IPs, []string{"10.5.5.5"}) {
		t.Errorf("MAC host behind a 127.0.0.1 hostname seen as = %+v; want 10.5.5.5", got)
	}
}

// TestLiteralPlaceholderAgrees: for an address literal in the hostname field,
// the seen-as query's placeholder test (literalPlaceholderSQL, in Postgres)
// agrees with identity.PlaceholderHostname.
func TestLiteralPlaceholderAgrees(t *testing.T) {
	s := newStore(t)
	values := []string{
		"127.0.0.1", "127.9.9.9", "0.0.0.0", "255.255.255.255", "::", "::1", "0:0:0:0:0:0:0:1",
		"::ffff:127.0.0.1", "::ffff:0.0.0.0", "::ffff:255.255.255.255", "::FFFF:7F00:1",
		"10.0.0.5", "169.254.1.1", "fe80::1", "2001:db8::1", "::ffff:10.0.0.1",
		"1.2.3", "999.1.1.1", "127.1", "127.0.0.01", "127.0.0.1/8", "::1/128", "ws1.corp:3389", "fe80::1%eth0",
		"::01", "::ffff:127.0.0.01", "::ffff:127.000.0.1", "0.0.0.00",
		"WS1", "localhost", "",
	}
	// The values come from a set-returning function, so the planner cannot
	// fold the cast as a constant, as it cannot over the query's own rows.
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT n, `+literalPlaceholderSQL+` FROM (
			SELECT n, h, (h ~ '^[0-9.]+$' OR h LIKE '%:%') AS literal
			FROM (SELECT n, lower(rtrim(btrim(v), '.')) AS h FROM unnest($1::text[]) WITH ORDINALITY AS u(v, n)) a
		) b ORDER BY n`, textArray(values))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n int
		var got bool
		if err := rows.Scan(&n, &got); err != nil {
			t.Fatal(err)
		}
		v := values[n-1]
		short, _ := identity.SplitHostname(v)
		want := net.ParseIP(short) != nil && identity.PlaceholderHostname(v)
		if got != want {
			t.Errorf("%q: literalPlaceholderSQL = %v; identity says an address placeholder: %v", v, got, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// TestMACFormAgreesWithFoldMAC: the seen-as query's MAC form accepts exactly
// what identity.FoldMAC accepts, in Go's regexp and in Postgres, and its fold
// of an accepted form is FoldMAC's, so its tier choice is
// identity.PlaceholderMAC's.
func TestMACFormAgreesWithFoldMAC(t *testing.T) {
	s := newStore(t)
	re := regexp.MustCompile(macForm)
	cases := []struct {
		in     string
		accept bool
	}{
		{"00:1a:2b:3c:4d:5e", true},
		{"00-1A-2B-3C-4D-5E", true},
		{"001a.2b3c.4d5e", true},
		{"001A2B3C4D5E", true},
		{"00:1a:2b:3c:4d:5e:6f:70", true},
		{"00-1a-2b-3c-4d-5e-6f-70", true},
		{"001a2b3c4d5e6f70", true},
		{"00:00:00:00:00:00", true}, // a form; the placeholder rule is separate
		{"00:1a-2b:3c:4d:5e", false},
		{"001a.2b3c.4d5e.6f70", false},
		{"001a2b3c4d5", false},
		{"001a2b3c4d5e6", false},
		{"unknown", false},
		{"", false},
		{"00:1a:2b:3c:4d", false},
		{"00:1a:2b:3c:4d:5e:", false},
		{"0:1a:2b:3c:4d:5e", false},
		{"00:1a:2b:3c:4d:5g", false},
		{"00.1a.2b.3c.4d.5e", false},
		{"00:00:5e:10:00:00:00:00:fe:80:00:00:00:00:00:00:02:00:5e:10", false}, // 20-byte form: not accepted
	}
	for _, c := range cases {
		folded := strings.ToLower(strings.TrimSpace(c.in))
		var sqlMatch bool
		var sqlFold string
		if err := s.db.QueryRowContext(context.Background(),
			`SELECT $1 ~ $2, regexp_replace($1, '[^0-9a-f]', '', 'g')`, folded, macForm).Scan(&sqlMatch, &sqlFold); err != nil {
			t.Fatal(err)
		}
		goFold := identity.FoldMAC(c.in)
		if (goFold != "") != c.accept || re.MatchString(folded) != c.accept || sqlMatch != c.accept {
			t.Errorf("%q: FoldMAC = %q, Go regexp %v, Postgres %v; want accepted = %v", c.in, goFold, re.MatchString(folded), sqlMatch, c.accept)
		}
		if c.accept && sqlFold != goFold {
			t.Errorf("%q: SQL fold %q, FoldMAC %q", c.in, sqlFold, goFold)
		}
	}
}

// TestHostIdentity_DetectionEvidence (adr/0356): a detection's evidence is the
// list evidences[]. The device objects of its artifacts are read for seen-as
// like the finding's own device, and an entry that is not an artifact is
// passed over.
func TestHostIdentity_DetectionEvidence(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	reg := capability.NewRegistry(identity.NewResolver(uuid.MustParse("44444444-4444-4444-8444-444444444444")))
	ids := reg.Identity()
	base := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	evt := capability.OcsfEvent{
		ID: uuid.Must(uuid.NewV7()), ClassUID: 2004, ClassName: "Detection Finding", Time: base, RecordedAt: base, SourceTool: "edr",
		Payload: map[string]any{
			"class_uid":    2004,
			"finding_info": map[string]any{"uid": "det-1", "title": "Beacon"},
			"device":       map[string]any{"hostname": "hosta", "ip": "10.0.0.1"},
			"evidences": []any{
				// An artifact with its own device object.
				map[string]any{"device": map[string]any{"hostname": "hosta.corp.example", "uid": "a-1", "ip": "10.0.0.2"}},
				// An artifact with no device: the host is the finding's, and its
				// endpoint names the same machine.
				map[string]any{
					"actor":        map[string]any{"user": map[string]any{"name": "alice"}},
					"src_endpoint": map[string]any{"hostname": "hosta", "ip": "10.0.0.3"},
				},
				"not an artifact",
			},
		},
	}
	nr, err := reg.Normalize(evt)
	if err != nil {
		t.Fatal(err)
	}
	nr.EventID = evt.ID
	inv := uuid.New()
	persistCall(t, s, Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &inv, Verb: "v", StartedAt: base},
		capability.CapabilityResult{Coverage: capability.CoverageComplete, Events: []capability.OcsfEvent{evt}, Normalized: []capability.NormalizationResult{nr}})

	v, err := s.HostIdentity(ctx, string(ids.Host(identity.HostIdentity{Hostname: "hosta"})))
	if err != nil {
		t.Fatalf("HostIdentity: %v", err)
	}
	want := SeenAs{
		Domains: []string{"corp.example"},
		Agents:  []SourceID{{"edr", "a-1"}},
		IPs:     []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
	}
	if !slices.Equal(v.SeenAs.Domains, want.Domains) || !slices.Equal(v.SeenAs.Agents, want.Agents) || !slices.Equal(v.SeenAs.IPs, want.IPs) {
		t.Errorf("seen as = %+v; want %+v", v.SeenAs, want)
	}
}
