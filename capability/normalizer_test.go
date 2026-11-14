package capability

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sd-strax/reckon/identity"
)

var testNS = uuid.MustParse("33333333-3333-4333-8333-333333333333")

func testRegistry() *Registry {
	return NewRegistry(identity.NewResolver(testNS))
}

// makeEvent builds an OcsfEvent from a raw OCSF payload, minting a telemetry id
// and reading class/time from the payload (as the adapter path would).
func makeEvent(payload map[string]any) OcsfEvent {
	return OcsfEvent{
		ID:         uuid.Must(uuid.NewV7()),
		ClassUID:   toInt(payload["class_uid"]),
		Time:       ocsfTime(payload, "time"),
		RecordedAt: time.Now(),
		SourceTool: "fixture:s",
		Payload:    payload,
	}
}

// ms is an RFC 3339 time as OCSF writes a timestamp_t: epoch milliseconds.
func ms(rfc3339 string) int64 {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		panic(err)
	}
	return t.UnixMilli()
}

func processPayload() map[string]any {
	return map[string]any{
		"class_uid": 1007,
		"time":      ms("2026-04-20T14:32:11Z"),
		"process": map[string]any{
			"pid":          4242,
			"name":         "powershell.exe",
			"cmd_line":     "powershell -enc ZQBjAGgAbwA=",
			"created_time": ms("2026-04-20T14:32:11Z"),
			"file": map[string]any{
				"path":   `C:\Windows\System32\powershell.exe`,
				"name":   "powershell.exe",
				"hashes": []any{map[string]any{"algorithm": "SHA-256", "value": "ABC123"}},
			},
			"parent_process": map[string]any{
				"pid":          1000,
				"name":         "explorer.exe",
				"created_time": ms("2026-04-20T14:00:00Z"),
			},
		},
		"device": map[string]any{"hostname": "WIN-DC01", "domain": "CONTOSO"},
		"actor":  map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": "S-1-5-21"}},
	}
}

// TestProcessNormalizer covers §4.1: five SCOs (process, parent, file, host,
// user), one ObservedData referencing all, and a parent-process-of relationship.
func TestProcessNormalizer(t *testing.T) {
	res, err := testRegistry().Normalize(makeEvent(processPayload()))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	byType := map[string]int{}
	for _, s := range res.SCOs {
		byType[s.Type]++
	}
	want := map[string]int{
		identity.TypeProcess: 2, // subject + parent
		identity.TypeFile:    1,
		identity.TypeHost:    1,
		// The SID account, and the CONTOSO\jdoe account the same record
		// aliases to it, outside object_refs (adr/0324).
		identity.TypeUserAccount: 2,
	}
	for typ, n := range want {
		if byType[typ] != n {
			t.Errorf("SCO type %s: got %d; want %d", typ, byType[typ], n)
		}
	}

	if len(res.ObservedData) != 1 {
		t.Fatalf("got %d ObservedData; want 1", len(res.ObservedData))
	}
	od := res.ObservedData[0]
	if len(od.ObjectRefs) != 5 {
		t.Errorf("ObservedData.object_refs = %d; want 5", len(od.ObjectRefs))
	}
	if od.NumberObserved != 1 {
		t.Errorf("number_observed = %d; want 1", od.NumberObserved)
	}
	if od.Provenance.DerivationMode != DerivationDirect {
		t.Errorf("derivation_mode = %q; want DIRECT", od.Provenance.DerivationMode)
	}

	// Exactly one parent-process-of relationship, parent → child.
	var rels int
	for _, r := range res.Relationships {
		if r.Type == RelParentProcessOf {
			rels++
		}
	}
	if rels != 1 {
		t.Errorf("parent-process-of relationships = %d; want 1", rels)
	}

	// Every SCO has an extracted-from edge; the ObservedData has a derived-from.
	var extracted, derived int
	for _, e := range res.Edges {
		switch e.Type {
		case EdgeExtractedFrom:
			extracted++
		case EdgeDerivedFrom:
			derived++
		}
	}
	if extracted != 6 || derived != 1 {
		t.Errorf("edges: extracted=%d derived=%d; want 6 and 1", extracted, derived)
	}
}

func authPayload(host string) map[string]any {
	return map[string]any{
		"class_uid":    3002,
		"time":         ms("2026-04-20T14:35:02Z"),
		"user":         map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": "S-1-5-21"},
		"dst_endpoint": map[string]any{"hostname": host, "domain": "CONTOSO"},
		"src_endpoint": map[string]any{"ip": "10.0.4.55"},
		"logon_type":   "RemoteInteractive",
		"status":       "SUCCESS",
	}
}

// TestAuthNormalizer covers §4.2: user, host, source-IP SCOs; an
// x-authenticated-to relationship; and logon_type/status in the ObservedData
// extensions.
func TestAuthNormalizer(t *testing.T) {
	res, err := testRegistry().Normalize(makeEvent(authPayload("WIN-FILE01")))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	byType := map[string]int{}
	for _, s := range res.SCOs {
		byType[s.Type]++
	}
	// The SID account and the login account aliased to it (adr/0324).
	if byType[identity.TypeUserAccount] != 2 || byType[identity.TypeHost] != 1 || byType[identity.TypeIPv4Addr] != 1 {
		t.Errorf("auth SCO types = %v; want 2 user accounts, 1 host, 1 ipv4", byType)
	}

	var authed int
	for _, r := range res.Relationships {
		if r.Type == RelAuthenticatedTo {
			authed++
		}
	}
	if authed != 1 {
		t.Errorf("x-authenticated-to relationships = %d; want 1", authed)
	}

	od := res.ObservedData[0]
	if od.Extensions["logon_type"] != "RemoteInteractive" || od.Extensions["status"] != "SUCCESS" {
		t.Errorf("ObservedData extensions = %v; want logon_type/status", od.Extensions)
	}
}

// TestAuthNormalizer_UserAndInitiator (adr/0356): the account that
// authenticated is the event's user. actor.user is whoever started the logon:
// a separate account, with no x-authenticated-to of its own.
func TestAuthNormalizer_UserAndInitiator(t *testing.T) {
	ids := identity.NewResolver(testNS)
	host := ids.Host(identity.HostIdentity{Hostname: "WIN-FILE01"})
	jdoe := ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: "S-1-5-21"})
	system := ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsLocal, UserID: "S-1-5-18", HostRef: host})

	authed := func(res NormalizationResult) []identity.STIXID {
		var out []identity.STIXID
		for _, r := range res.Relationships {
			if r.Type == RelAuthenticatedTo {
				if r.TargetRef != host {
					t.Errorf("x-authenticated-to targets %s; want the destination host", r.TargetRef)
				}
				out = append(out, r.SourceRef)
			}
		}
		return out
	}

	// Both: the user authenticated, the initiator did not.
	p := authPayload("WIN-FILE01")
	p["actor"] = map[string]any{"user": map[string]any{"name": "SYSTEM", "domain": "NT AUTHORITY", "uid": "S-1-5-18"}}
	res, err := testRegistry().Normalize(makeEvent(p))
	if err != nil {
		t.Fatal(err)
	}
	refs := res.ObservedData[0].ObjectRefs
	if !slices.Contains(refs, jdoe) || !slices.Contains(refs, system) {
		t.Errorf("object_refs = %v; want the user %s and the initiator %s", refs, jdoe, system)
	}
	if got := authed(res); !slices.Equal(got, []identity.STIXID{jdoe}) {
		t.Errorf("x-authenticated-to from %v; want only the user %s", got, jdoe)
	}

	// The initiator is the user: one account, one relationship.
	p = authPayload("WIN-FILE01")
	p["actor"] = map[string]any{"user": p["user"]}
	same, err := testRegistry().Normalize(makeEvent(p))
	if err != nil {
		t.Fatal(err)
	}
	alone, err := testRegistry().Normalize(makeEvent(authPayload("WIN-FILE01")))
	if err != nil {
		t.Fatal(err)
	}
	if len(same.SCOs) != len(alone.SCOs) || !slices.Equal(authed(same), []identity.STIXID{jdoe}) {
		t.Errorf("the user named again as the initiator: %d objects (want %d), x-authenticated-to from %v", len(same.SCOs), len(alone.SCOs), authed(same))
	}

	// Only an initiator: the event does not say who authenticated.
	p = authPayload("WIN-FILE01")
	p["actor"] = map[string]any{"user": p["user"]}
	delete(p, "user")
	res, err = testRegistry().Normalize(makeEvent(p))
	if err != nil {
		t.Fatal(err)
	}
	if got := authed(res); len(got) != 0 {
		t.Errorf("an event with no user: x-authenticated-to from %v; want none", got)
	}
	if !slices.Contains(res.ObservedData[0].ObjectRefs, jdoe) {
		t.Errorf("object_refs = %v; want the initiator %s kept", res.ObservedData[0].ObjectRefs, jdoe)
	}
}

// TestOpaqueFallback: an unregistered class yields a single opaque ObservedData
// with no object_refs and a derived-from edge (§4.13).
func TestOpaqueFallback(t *testing.T) {
	res, err := testRegistry().Normalize(makeEvent(map[string]any{
		"class_uid": 9999,
		"time":      ms("2026-04-20T14:32:11Z"),
		"weird":     "vendor payload",
	}))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(res.SCOs) != 0 {
		t.Errorf("opaque produced %d SCOs; want 0", len(res.SCOs))
	}
	if len(res.ObservedData) != 1 || len(res.ObservedData[0].ObjectRefs) != 0 {
		t.Errorf("opaque ObservedData not empty/singular: %+v", res.ObservedData)
	}
	if len(res.Edges) != 1 || res.Edges[0].Type != EdgeDerivedFrom {
		t.Errorf("opaque edges = %+v; want one derived-from", res.Edges)
	}
}

// TestCrossEventStitching is the whole point of the identity layer: the same
// host seen through a process event and an authentication event resolves to the
// SAME x-host SCO id — the graph stitches instead of fragmenting.
func TestCrossEventStitching(t *testing.T) {
	reg := testRegistry()

	procRes, err := reg.Normalize(makeEvent(processPayload()))
	if err != nil {
		t.Fatal(err)
	}
	authRes, err := reg.Normalize(makeEvent(authPayload("WIN-DC01")))
	if err != nil {
		t.Fatal(err)
	}

	procHost := scoOfType(procRes.SCOs, identity.TypeHost)
	authHost := scoOfType(authRes.SCOs, identity.TypeHost)
	if procHost == "" || authHost == "" {
		t.Fatalf("missing host SCO: proc=%q auth=%q", procHost, authHost)
	}
	if procHost != authHost {
		t.Errorf("same host did not stitch: process=%q auth=%q", procHost, authHost)
	}

	// The same user (CONTOSO\jdoe, same SID) stitches too. Note the contract:
	// user-account identity includes user_id, so tools must report a consistent
	// SID for stitching — a tool that omits it produces a distinct SCO (§7.2).
	if u1, u2 := scoOfType(procRes.SCOs, identity.TypeUserAccount), scoOfType(authRes.SCOs, identity.TypeUserAccount); u1 != u2 {
		t.Errorf("same user did not stitch: process=%q auth=%q", u1, u2)
	}
}

// TestNormalizeDeterminism: normalizing the same OCSF payload twice (distinct
// telemetry ids) yields the same ObservedData id — deduplicable across ingests.
func TestNormalizeDeterminism(t *testing.T) {
	reg := testRegistry()
	a, _ := reg.Normalize(makeEvent(processPayload()))
	b, _ := reg.Normalize(makeEvent(processPayload()))
	if a.ObservedData[0].ID != b.ObservedData[0].ID {
		t.Errorf("ObservedData id not stable across ingests: %q vs %q", a.ObservedData[0].ID, b.ObservedData[0].ID)
	}
}

// TestEndToEndFixtureToStix wires the fixture adapter to the registry: replay
// OCSF events, convert to OcsfEvents, normalize, and confirm STIX comes out.
func TestEndToEndFixtureToStix(t *testing.T) {
	a := newTestFixture(t, twoLogonsPlusOther)
	resp, err := a.Invoke(context.Background(), "replay", map[string]any{
		ParamVerb: "enumerate_logons",
		"target":  map[string]any{"hostname": "WIN-FILE01"},
		"outcome": "SUCCESS",
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := testRegistry()
	var totalOD, totalSCO int
	for _, evt := range resp.ToOcsfEvents(time.Now()) {
		res, err := reg.Normalize(evt)
		if err != nil {
			t.Fatalf("normalize %v: %v", evt.ClassUID, err)
		}
		totalOD += len(res.ObservedData)
		totalSCO += len(res.SCOs)
	}
	if totalOD != 2 {
		t.Errorf("got %d ObservedData from 2 logons; want 2", totalOD)
	}
	if totalSCO == 0 {
		t.Error("no SCOs produced from the fixture logons")
	}
}

func scoOfType(scos []SCO, typ string) identity.STIXID {
	for _, s := range scos {
		if s.Type == typ {
			return s.ID
		}
	}
	return ""
}

// hostEvent is a process event whose device object is dev, from tool.
func hostEvent(tool string, dev map[string]any) OcsfEvent {
	p := processPayload()
	p["device"] = dev
	evt := makeEvent(p)
	evt.SourceTool = tool
	return evt
}

func scoByID(scos []SCO, id identity.STIXID) (SCO, bool) {
	for _, s := range scos {
		if s.ID == id {
			return s, true
		}
	}
	return SCO{}, false
}

func refsContain(refs []identity.STIXID, id identity.STIXID) bool {
	for _, r := range refs {
		if r == id {
			return true
		}
	}
	return false
}

// TestHostShaping covers adr/0211's shaping of the device object: the strong
// host on the short hostname with every other identifier kept as a property,
// and the weak hosts a record with no hostname mints.
func TestHostShaping(t *testing.T) {
	reg := testRegistry()
	ids := reg.Identity()

	res, err := reg.Normalize(hostEvent("edr", map[string]any{
		"hostname":   "WS1.corp.example.",
		"domain":     "CORP",
		"uid":        "agent-7",
		"uid_alt":    "hw-uuid-1",
		"mac":        "00-1A-2B-3C-4D-5E",
		"agent_list": []any{map[string]any{"uid": "agent-7", "name": "sensor", "vendor_name": "Example", "version": "1"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	strong := ids.Host(identity.HostIdentity{Hostname: "ws1"})
	h, ok := scoByID(res.SCOs, strong)
	if !ok {
		t.Fatalf("no strong host %s among %+v", strong, res.SCOs)
	}
	want := map[string]any{
		"hostname":       "WS1",
		"fqdn":           "WS1.corp.example",
		"domain":         "CORP",
		"mac":            "00-1A-2B-3C-4D-5E",
		"device_uid_alt": "hw-uuid-1",
	}
	for k, v := range want {
		if h.Properties[k] != v {
			t.Errorf("strong host %s = %v; want %v", k, h.Properties[k], v)
		}
	}
	if src, _ := h.Properties["source_ids"].(map[string]any); src["edr"] != "agent-7" {
		t.Errorf("source_ids = %v; want {edr: agent-7}", h.Properties["source_ids"])
	}
	agents, _ := h.Properties["agents"].([]any)
	if len(agents) != 1 || agents[0].(map[string]any)["uid"] != "agent-7" || agents[0].(map[string]any)["version"] != nil {
		t.Errorf("agents = %v; want one {uid, name, vendor_name}", h.Properties["agents"])
	}
	if h.Properties["weak"] != nil {
		t.Error("a strong host is not weak")
	}

	// The FQDN remainder is the domain when device.domain is absent.
	res2, _ := reg.Normalize(hostEvent("edr", map[string]any{"hostname": "ws1.corp.example"}))
	if h2, _ := scoByID(res2.SCOs, strong); h2.Properties["domain"] != "corp.example" {
		t.Errorf("domain from the FQDN = %v; want corp.example", h2.Properties["domain"])
	}

	// No hostname: the MAC, else (tool, uid), mints a weak host in object_refs.
	macHost := ids.Host(identity.HostIdentity{MAC: "00:1a:2b:3c:4d:5e"})
	res3, _ := reg.Normalize(hostEvent("edr", map[string]any{"mac": "00:1A:2B:3C:4D:5E", "uid": "agent-7"}))
	if w, ok := scoByID(res3.SCOs, macHost); !ok || w.Properties["weak"] != true || !refsContain(res3.ObservedData[0].ObjectRefs, macHost) {
		t.Errorf("MAC-only record: weak host %s missing, unmarked or outside object_refs: %+v", macHost, res3.SCOs)
	}
	uidHost := ids.Host(identity.HostIdentity{SourceTool: "edr", SourceUID: "agent-7"})
	res4, _ := reg.Normalize(hostEvent("edr", map[string]any{"uid": "agent-7"}))
	w, ok := scoByID(res4.SCOs, uidHost)
	if !ok || w.Properties["weak"] != true || w.Properties["source_tool"] != "edr" || w.Properties["device_uid"] != "agent-7" {
		t.Errorf("uid-only record: weak host = %+v", w)
	}
	for _, r := range res4.Relationships { // the account's alias aside, no host alias
		if r.Type == RelAliases && strings.HasPrefix(string(r.SourceRef), identity.TypeHost+"--") {
			t.Errorf("a weak-only record emits no host alias: %+v", r)
		}
	}

	// An IP literal in the hostname field is the host whole: no FQDN, no
	// domain cut from it, and another address is another host.
	ipHost := ids.Host(identity.HostIdentity{Hostname: "10.0.0.5"})
	res6, _ := reg.Normalize(hostEvent("edr", map[string]any{"hostname": "10.0.0.5"}))
	if h6, ok := scoByID(res6.SCOs, ipHost); !ok || h6.Properties["hostname"] != "10.0.0.5" || h6.Properties["fqdn"] != nil || h6.Properties["domain"] != nil {
		t.Errorf("IP literal hostname: host = %+v", h6)
	}
	if ipHost == ids.Host(identity.HostIdentity{Hostname: "10.0.0.6"}) {
		t.Error("two addresses in the hostname field minted one host")
	}

	// A device uid from a record with no source tool is kept unkeyed, and with
	// no hostname still mints a weak host.
	res7, _ := reg.Normalize(hostEvent("", map[string]any{"hostname": "ws1", "uid": "agent-7"}))
	if h7, _ := scoByID(res7.SCOs, strong); h7.Properties["device_uid"] != "agent-7" || h7.Properties["source_ids"] != nil {
		t.Errorf("no source tool: host props = %v; want device_uid, no source_ids", h7.Properties)
	}
	res8, _ := reg.Normalize(hostEvent("", map[string]any{"uid": "agent-7"}))
	if scoOfType(res8.SCOs, identity.TypeHost) != ids.Host(identity.HostIdentity{SourceUID: "agent-7"}) {
		t.Error("a uid-only record with no source tool minted no weak host")
	}

	// Nothing to identify the device by: no host.
	res5, _ := reg.Normalize(hostEvent("edr", map[string]any{"domain": "CORP"}))
	if scoOfType(res5.SCOs, identity.TypeHost) != "" {
		t.Error("a device with no hostname, MAC or uid minted a host")
	}
}

// TestHostAliasEmission: a record with a hostname and a MAC and a device uid
// emits the weak host each identifier alone mints, outside object_refs, with an
// aliases edge from each to the strong host (adr/0211).
func TestHostAliasEmission(t *testing.T) {
	reg := testRegistry()
	ids := reg.Identity()
	res, err := reg.Normalize(hostEvent("edr", map[string]any{
		"hostname": "WS1", "mac": "00:1a:2b:3c:4d:5e", "uid": "agent-7",
	}))
	if err != nil {
		t.Fatal(err)
	}
	strong := ids.Host(identity.HostIdentity{Hostname: "ws1"})
	weak := []identity.STIXID{
		ids.Host(identity.HostIdentity{MAC: "00:1a:2b:3c:4d:5e"}),
		ids.Host(identity.HostIdentity{SourceTool: "edr", SourceUID: "agent-7"}),
	}
	refs := res.ObservedData[0].ObjectRefs
	if !refsContain(refs, strong) {
		t.Error("strong host missing from object_refs")
	}
	extracted := map[identity.STIXID]bool{}
	for _, e := range res.Edges {
		if e.Type == EdgeExtractedFrom {
			extracted[e.SourceRef] = true
		}
	}
	for _, wid := range weak {
		s, ok := scoByID(res.SCOs, wid)
		if !ok || s.Properties["weak"] != true {
			t.Errorf("weak host %s not emitted as a weak SCO", wid)
		}
		if refsContain(refs, wid) {
			t.Errorf("weak host %s is in object_refs", wid)
		}
		if !extracted[wid] {
			t.Errorf("weak host %s has no extracted-from edge", wid)
		}
		var found bool
		for _, r := range res.Relationships {
			if r.Type == RelAliases && r.SourceRef == wid && r.TargetRef == strong {
				found = r.Provenance.DerivationMode == DerivationDirect && r.Provenance.Tool == "edr"
			}
		}
		if !found {
			t.Errorf("no DIRECT aliases %s → %s from the record", wid, strong)
		}
	}
}

// TestNormalizerVersions pins every normalizer's version. The ObservedData id
// carries the version but not the normalizer's name, so a normalizer that
// takes over a class number starts above every version that number has had:
// the inventory classes (5001, 5003, 5023) and api_activity started at 2
// above the opaque default, and email_file_activity starts at 4 above the
// link-click normalizer that held 4011 before adr/0356.
//
// adr/0356 moved every normalizer whose input changed to its next version:
// the classes and fields are OCSF's, and file hashes are read in OCSF's list
// form only. dns_activity, user_inventory and cloud_resources_inventory_info
// read the same input as before and did not move.
func TestNormalizerVersions(t *testing.T) {
	reg := testRegistry()
	for _, c := range []struct{ class, version int }{
		{1007, 4}, {3002, 5}, {4001, 3}, {4003, 2}, {1001, 4}, {201001, 4}, {201002, 4}, {1006, 4}, {1005, 3},
		{3001, 4}, {3005, 4}, {3006, 4}, {4009, 3}, {4011, 4}, {4012, 4}, {2004, 6}, {2005, 2},
		{5001, 3}, {6003, 3}, {5023, 4}, {5003, 2}, {0, 1},
	} {
		n, ok := reg.byClass[c.class]
		if c.class == 0 {
			n, ok = reg.def, true
		}
		if !ok {
			t.Errorf("class %d not registered", c.class)
			continue
		}
		if n.Version() != c.version {
			t.Errorf("class %d version = %d; want %d", c.class, n.Version(), c.version)
		}
	}
}

// TestEndpointHostReadsNoUIDAlt (adr/0356): a network endpoint has no
// uid_alt in OCSF, so the host an authentication mints from dst_endpoint
// keeps none, while a device's is kept.
func TestEndpointHostReadsNoUIDAlt(t *testing.T) {
	object := map[string]any{"hostname": "WS1", "uid": "dev-1", "uid_alt": "asset-1"}
	auth := authPayload("WS1")
	auth["dst_endpoint"] = object
	proc := processPayload()
	proc["device"] = object
	for name, tc := range map[string]struct {
		payload map[string]any
		want    any
	}{
		"endpoint": {auth, nil},
		"device":   {proc, "asset-1"},
	} {
		res, err := testRegistry().Normalize(makeEvent(tc.payload))
		if err != nil {
			t.Fatal(err)
		}
		host := scosOf(res, identity.TypeHost)[0]
		if got := host.Properties["device_uid_alt"]; got != tc.want {
			t.Errorf("%s: device_uid_alt = %v; want %v", name, got, tc.want)
		}
		// Everything else the object says is kept either way.
		if host.Properties["hostname"] != "WS1" || len(relsOf(res, RelAliases)) == 0 {
			t.Errorf("%s: host = %v, aliases %v", name, host.Properties, relsOf(res, RelAliases))
		}
	}
}

// TestDeviceOSIsReadWhereTheClassHasADevice (adr/0356): a vendor name alone
// marks an identity provider only when no device object of the record names
// an OS. Each normalizer names the device objects its class has, and only
// those are read.
func TestDeviceOSIsReadWhereTheClassHasADevice(t *testing.T) {
	ids := identity.NewResolver(testNS)
	okta := map[string]any{"product": map[string]any{"vendor_name": "Okta"}}
	user := map[string]any{"name": "jdoe@contoso.example", "uid": "00u1AbCd"}
	linux := map[string]any{"type_id": 200}
	onLinux := map[string]any{"hostname": "WS1", "os": linux}
	cloud := ids.Account(identity.AccountIdentity{Type: identity.AccountCloud, UserID: "00u1AbCd"})

	for _, tc := range []struct {
		name    string
		payload map[string]any
		idp     bool
	}{
		{"authentication, no OS", map[string]any{"class_uid": 3002, "user": user}, true},
		{"authentication, OS on the source", map[string]any{"class_uid": 3002, "user": user, "src_endpoint": map[string]any{"os": linux}}, false},
		{"authentication, OS on the destination", map[string]any{"class_uid": 3002, "user": user, "dst_endpoint": map[string]any{"os": map[string]any{"name": "Ubuntu"}}}, false},
		{"authentication, OS on the device", map[string]any{"class_uid": 3002, "user": user, "device": map[string]any{"hostname": "WS1", "os": map[string]any{"type": "Linux"}}}, false},
		{"process, no OS", map[string]any{"class_uid": 1007, "actor": map[string]any{"user": user}}, true},
		{"process, OS on the device", map[string]any{"class_uid": 1007, "actor": map[string]any{"user": user}, "device": onLinux}, false},
		{"file, OS on the device", map[string]any{"class_uid": 1001, "actor": map[string]any{"user": user}, "device": onLinux}, false},
		{"scheduled job, OS on the device", map[string]any{"class_uid": 1006, "job": map[string]any{"name": "j", "user": user}, "device": onLinux}, false},
		{"account change, no OS", map[string]any{"class_uid": 3001, "user": user}, true},
		{"account change, OS on the source", map[string]any{"class_uid": 3001, "user": user, "src_endpoint": map[string]any{"os": linux}}, false},
		{"user access, OS on the device", map[string]any{"class_uid": 3005, "user": user, "device": onLinux}, false},
		{"user access, OS on the source", map[string]any{"class_uid": 3005, "user": user, "src_endpoint": map[string]any{"os": map[string]any{"name": "Ubuntu"}}}, false},
		{"group management, OS on the source", map[string]any{"class_uid": 3006, "user": user, "src_endpoint": map[string]any{"os": map[string]any{"type": "Linux"}}}, false},
		{"group management, OS on the device", map[string]any{"class_uid": 3006, "user": user, "device": onLinux}, false},
		{"email click, OS on the device", map[string]any{"class_uid": 4012, "actor": map[string]any{"user": user}, "device": onLinux}, false},
		{"email attachment, OS on the device", map[string]any{"class_uid": 4011, "actor": map[string]any{"user": user}, "device": onLinux}, false},
		{"detection evidence, no OS", map[string]any{"class_uid": 2004, "evidences": []any{map[string]any{"actor": map[string]any{"user": user}}}}, true},
		{"detection evidence, OS on the finding's device", map[string]any{"class_uid": 2004, "device": onLinux, "evidences": []any{map[string]any{"actor": map[string]any{"user": user}}}}, false},
		{"detection evidence, OS on the artifact's device", map[string]any{"class_uid": 2004, "evidences": []any{map[string]any{"user": user, "device": onLinux}}}, false},
		{"detection evidence, OS on the artifact's endpoint", map[string]any{"class_uid": 2004, "evidences": []any{map[string]any{"user": user, "dst_endpoint": map[string]any{"os": linux}}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.payload["metadata"] = okta
			tc.payload["time"] = ms("2026-04-20T14:32:11Z")
			res, err := testRegistry().Normalize(makeEvent(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			accounts := accountsIn(res)
			if len(accounts) != 1 {
				t.Fatalf("accounts = %v; want one", accounts)
			}
			if got := accounts[0] == cloud; got != tc.idp {
				t.Errorf("read as the identity provider's cloud account = %v; want %v", got, tc.idp)
			}
		})
	}
}

// TestOcsfTimes (adr/0356): an OCSF timestamp is epoch milliseconds, and the
// datetime profile's _dt twin is an RFC 3339 string. The number is read
// first and the twin as a fallback, so an event that carries either one, or
// both, gets the same event time and the same process id.
func TestOcsfTimes(t *testing.T) {
	const when = "2026-04-20T14:32:11Z"
	want, _ := time.Parse(time.RFC3339, when)
	event := func(timeFields, processFields map[string]any) OcsfEvent {
		p := map[string]any{
			"class_uid": 1007,
			"device":    map[string]any{"hostname": "WS1"},
			"process":   map[string]any{"pid": 4242, "name": "a.exe"},
		}
		for k, v := range timeFields {
			p[k] = v
		}
		for k, v := range processFields {
			p["process"].(map[string]any)[k] = v
		}
		payload := toOcsfPayload(p)
		return OcsfEvent{ID: uuid.Must(uuid.NewV7()), ClassUID: payload.ClassUID, Time: payload.Time, SourceTool: "fixture:s", Payload: p}
	}
	ids := identity.NewResolver(testNS)
	proc, _ := ids.Process(ids.Host(identity.HostIdentity{Hostname: "WS1"}), 4242, want)

	for name, tc := range map[string]struct{ time, process map[string]any }{
		"the number":               {map[string]any{"time": ms(when)}, map[string]any{"created_time": ms(when)}},
		"the number as JSON":       {map[string]any{"time": float64(ms(when))}, map[string]any{"created_time": float64(ms(when))}},
		"the _dt twin alone":       {map[string]any{"time_dt": when}, map[string]any{"created_time_dt": when}},
		"both":                     {map[string]any{"time": ms(when), "time_dt": when}, map[string]any{"created_time": ms(when), "created_time_dt": when}},
		"the number outranks":      {map[string]any{"time": ms(when), "time_dt": "2001-01-01T00:00:00Z"}, map[string]any{"created_time": ms(when), "created_time_dt": "2001-01-01T00:00:00Z"}},
		"the twin in another zone": {map[string]any{"time_dt": "2026-04-20T16:32:11+02:00"}, map[string]any{"created_time_dt": "2026-04-20T16:32:11+02:00"}},
	} {
		evt := event(tc.time, tc.process)
		if !evt.Time.Equal(want) {
			t.Errorf("%s: event time = %s; want %s", name, evt.Time, want)
		}
		res, err := testRegistry().Normalize(evt)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := scoByID(res.SCOs, proc); !ok {
			t.Errorf("%s: the process is not %s: %v", name, proc, scosOf(res, identity.TypeProcess))
		}
	}

	// A string in the number's field is not OCSF and is not read.
	evt := event(map[string]any{"time": when}, map[string]any{"created_time": when})
	if !evt.Time.IsZero() {
		t.Errorf("a string in time was read as %s", evt.Time)
	}
	res, err := testRegistry().Normalize(evt)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := scoByID(res.SCOs, proc); ok {
		t.Error("a string in created_time was read as the process's start")
	}
}
