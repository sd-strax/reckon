package capability

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability/ocsfview"
	"github.com/sd-strax/reckon/identity"
)

// inventoryEvent is an inventory_info record (class 5001) from tool.
func inventoryEvent(tool string, dev map[string]any) OcsfEvent {
	return OcsfEvent{
		ID: uuid.Must(uuid.NewV7()), ClassUID: 5001, ClassName: "Device Inventory Info",
		Time: time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC), RecordedAt: time.Now(), SourceTool: tool,
		Payload: map[string]any{"class_uid": 5001, "device": dev},
	}
}

// TestHostIdentityConformance is the host identity conformance check
// (adr/0211, `make conformance`): one machine as three tools' inventories
// report it normalizes to one strong x-host, each record that carries a
// device id also yields the weak host that id alone mints, aliased to the
// strong host and kept out of the observation's object_refs, and a later
// activity record that names the machine by its device id alone lands on that
// weak host, so it reads under the strong one through the alias. An adapter
// for a live source is held to the same properties over its own captures.
func TestHostIdentityConformance(t *testing.T) {
	reg := testRegistry()
	ids := reg.Identity()

	cases := []struct {
		name   string
		tool   string
		device map[string]any
		uid    string // the device.uid the record carries, "" for none
	}{
		{
			name: "client id, short hostname, DNS domain",
			tool: "forensics-client",
			device: map[string]any{
				"uid": "C.4f2a9e1b7c3d", "hostname": "WS1", "domain": "corp.example",
			},
			uid: "C.4f2a9e1b7c3d",
		},
		{
			name:   "agent id, agent name, no domain",
			tool:   "host-ids",
			device: map[string]any{"uid": "007", "hostname": "ws1"},
			uid:    "007",
		},
		{
			name: "FQDN as the hostname, hardware uuid",
			tool: "fleet-manager",
			device: map[string]any{
				"hostname": "WS1.corp.example", "uid_alt": "4C4C4544-0042-3510-8052-B3C04F333231",
			},
		},
	}

	strong := ids.Host(identity.HostIdentity{Hostname: "ws1"})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := reg.Normalize(inventoryEvent(c.tool, c.device))
			if err != nil {
				t.Fatal(err)
			}
			refs := res.ObservedData[0].ObjectRefs
			var hosts []identity.STIXID
			for _, r := range refs {
				if s, ok := scoByID(res.SCOs, r); ok && s.Type == identity.TypeHost {
					hosts = append(hosts, r)
				}
			}
			if len(hosts) != 1 || hosts[0] != strong {
				t.Fatalf("object_refs hosts = %v; want exactly the strong host %s", hosts, strong)
			}

			var aliases []Relationship
			for _, r := range res.Relationships {
				if r.Type == RelAliases {
					aliases = append(aliases, r)
				}
			}
			if c.uid == "" {
				if len(aliases) != 0 {
					t.Errorf("a record with no device id emitted aliases: %+v", aliases)
				}
				return
			}
			weak := ids.Host(identity.HostIdentity{SourceTool: c.tool, SourceUID: c.uid})
			if len(aliases) != 1 || aliases[0].SourceRef != weak || aliases[0].TargetRef != strong {
				t.Fatalf("aliases = %+v; want one %s → %s", aliases, weak, strong)
			}
			if s, ok := scoByID(res.SCOs, weak); !ok || s.Properties["weak"] != true {
				t.Errorf("weak host %s not emitted as a weak SCO", weak)
			}
			if refsContain(refs, weak) {
				t.Errorf("weak host %s is in object_refs", weak)
			}
		})
	}

	// Inventory first, activity after: the second tool's inventory named the
	// hostname beside its agent id (above); its process record naming the
	// agent id alone mints the weak host that inventory aliased, so it joins
	// the strong host through that alias.
	res, err := reg.Normalize(hostEvent("host-ids", map[string]any{"uid": "007"}))
	if err != nil {
		t.Fatal(err)
	}
	weak := ids.Host(identity.HostIdentity{SourceTool: "host-ids", SourceUID: "007"})
	if got := scoOfType(res.SCOs, identity.TypeHost); got != weak {
		t.Errorf("device-id-only process record minted %s; want the aliased weak host %s", got, weak)
	}
	if !refsContain(res.ObservedData[0].ObjectRefs, weak) {
		t.Error("the device-id-only record's host is not in its object_refs")
	}
}

// TestHostIdentityPlaceholders (adr/0211): two machines whose inventories
// both report the zero MAC beside their hostnames are two strong hosts, with
// no weak host on the zero MAC to alias them into one and no "mac" property;
// a network record from 0.0.0.0 mints no address.
func TestHostIdentityPlaceholders(t *testing.T) {
	reg := testRegistry()
	ids := reg.Identity()
	zeroWeak := ids.Host(identity.HostIdentity{MAC: "00:00:00:00:00:00"})
	if zeroWeak != "" {
		t.Fatalf("the zero MAC minted a host: %s", zeroWeak)
	}
	var strongs []identity.STIXID
	for _, name := range []string{"WS1", "WS2"} {
		res, err := reg.Normalize(inventoryEvent("fleet-manager", map[string]any{
			"hostname": name, "mac": "00:00:00:00:00:00", "uid": "00000000-0000-0000-0000-000000000000",
		}))
		if err != nil {
			t.Fatal(err)
		}
		var hosts []SCO
		for _, s := range res.SCOs {
			if s.Type == identity.TypeHost {
				hosts = append(hosts, s)
			}
		}
		strong := ids.Host(identity.HostIdentity{Hostname: name})
		if len(hosts) != 1 || hosts[0].ID != strong {
			t.Fatalf("%s: hosts = %+v; want only the strong host %s", name, hosts, strong)
		}
		for _, k := range []string{"mac", "source_ids", "device_uid"} {
			if _, ok := hosts[0].Properties[k]; ok {
				t.Errorf("%s: a placeholder was kept as the %q property: %v", name, k, hosts[0].Properties)
			}
		}
		for _, r := range res.Relationships {
			if r.Type == RelAliases {
				t.Errorf("%s: a placeholder emitted an alias: %+v", name, r)
			}
		}
		strongs = append(strongs, strong)
	}
	if strongs[0] == strongs[1] {
		t.Error("two hostnames read as one host")
	}

	evt := makeEvent(map[string]any{
		"class_uid": 4001, "time": ms("2026-04-20T14:32:11Z"),
		"src_endpoint": map[string]any{"ip": "0.0.0.0"},
		"dst_endpoint": map[string]any{"ip": "10.0.0.7", "port": 445},
	})
	res, err := reg.Normalize(evt)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.SCOs {
		if s.Type == identity.TypeIPv4Addr && s.Properties["value"] == "0.0.0.0" {
			t.Errorf("0.0.0.0 minted an address: %+v", s)
		}
	}
	for _, r := range res.ObservedData[0].ObjectRefs {
		if r == "" {
			t.Error("object_refs holds an empty ref")
		}
	}
	if !refsContain(res.ObservedData[0].ObjectRefs, ids.IPv4Addr("10.0.0.7")) {
		t.Error("the real destination address is missing")
	}
}

// TestWeakHostKeepsFacts (adr/0211): a record whose hostname is a placeholder
// mints the weak host on its MAC, and that host keeps what the record says:
// the domain, the device id by source and the agents, beside its MAC and the
// weak mark. A uid weak host carries no MAC.
func TestWeakHostKeepsFacts(t *testing.T) {
	reg := testRegistry()
	ids := reg.Identity()
	res, err := reg.Normalize(inventoryEvent("edr", map[string]any{
		"hostname": "-", "mac": "02:00:00:00:00:07", "uid": "agent-7", "domain": "CORP",
		"agent_list": []any{map[string]any{"uid": "sensor-7", "name": "edr-sensor"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	weak := ids.Host(identity.HostIdentity{MAC: "02:00:00:00:00:07"})
	h, ok := scoByID(res.SCOs, weak)
	if !ok {
		t.Fatalf("no MAC weak host %s in %+v", weak, res.SCOs)
	}
	p := h.Properties
	ids7, _ := p["source_ids"].(map[string]any)
	agents, _ := p["agents"].([]any)
	if p["weak"] != true || p["mac"] != "02:00:00:00:00:07" || p["domain"] != "CORP" || ids7["edr"] != "agent-7" || len(agents) != 1 {
		t.Errorf("weak host properties = %v", p)
	}
	if _, ok := p["hostname"]; ok {
		t.Errorf("the placeholder hostname was kept: %v", p)
	}

	res, err = reg.Normalize(inventoryEvent("edr", map[string]any{"hostname": "-", "mac": "00:00:00:00:00:00", "uid": "agent-8", "domain": "CORP"}))
	if err != nil {
		t.Fatal(err)
	}
	u, ok := scoByID(res.SCOs, ids.Host(identity.HostIdentity{SourceTool: "edr", SourceUID: "agent-8"}))
	if !ok || u.Properties["device_uid"] != "agent-8" || u.Properties["source_tool"] != "edr" || u.Properties["domain"] != "CORP" {
		t.Fatalf("uid weak host = %+v (found %v)", u, ok)
	}
	if _, ok := u.Properties["mac"]; ok {
		t.Errorf("the uid weak host carries a MAC: %v", u.Properties)
	}
}

// TestMACWeakHostAliasesItsDeviceID (adr/0211): a hostname-less record with a
// MAC and a device id mints the MAC weak host and aliases the device id's weak
// host to it, outside object_refs, so a later record naming the device id
// alone lands on a host aliased to the MAC host.
func TestMACWeakHostAliasesItsDeviceID(t *testing.T) {
	reg := testRegistry()
	ids := reg.Identity()
	macHost := ids.Host(identity.HostIdentity{MAC: "02:00:00:00:00:09"})
	uidHost := ids.Host(identity.HostIdentity{SourceTool: "edr", SourceUID: "agent-7"})

	res, err := reg.Normalize(inventoryEvent("edr", map[string]any{"mac": "02:00:00:00:00:09", "uid": "agent-7"}))
	if err != nil {
		t.Fatal(err)
	}
	refs := res.ObservedData[0].ObjectRefs
	if !refsContain(refs, macHost) || refsContain(refs, uidHost) {
		t.Errorf("object_refs = %v; want the MAC host %s and not the uid host %s", refs, macHost, uidHost)
	}
	var found bool
	for _, r := range res.Relationships {
		found = found || (r.Type == RelAliases && r.SourceRef == uidHost && r.TargetRef == macHost)
	}
	if !found {
		t.Errorf("no aliases %s → %s in %+v", uidHost, macHost, res.Relationships)
	}
	if s, ok := scoByID(res.SCOs, uidHost); !ok || s.Properties["weak"] != true {
		t.Errorf("uid weak host %s not emitted as a weak SCO", uidHost)
	}

	res, err = reg.Normalize(hostEvent("edr", map[string]any{"uid": "agent-7"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := scoOfType(res.SCOs, identity.TypeHost); got != uidHost {
		t.Errorf("the device-id-only record minted %s; want the aliased uid host %s", got, uidHost)
	}
}

// TestPlaceholderDomainDropped: a placeholder domain is no property, on a
// strong host or a weak one.
func TestPlaceholderDomainDropped(t *testing.T) {
	reg := testRegistry()
	for _, dev := range []map[string]any{
		{"hostname": "WS1", "domain": "-"},
		{"hostname": "WS1.unknown"},
		{"mac": "02:00:00:00:00:0a", "domain": "N/A"},
	} {
		res, err := reg.Normalize(inventoryEvent("edr", dev))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range res.SCOs {
			if _, ok := s.Properties["domain"]; ok && s.Type == identity.TypeHost {
				t.Errorf("%v: host %s keeps a placeholder domain: %v", dev, s.ID, s.Properties)
			}
		}
	}

	// A domain is judged whole: unknown.example is a real one.
	res, err := reg.Normalize(inventoryEvent("edr", map[string]any{"hostname": "WS1.unknown.example"}))
	if err != nil {
		t.Fatal(err)
	}
	if h, ok := scoByID(res.SCOs, reg.Identity().Host(identity.HostIdentity{Hostname: "ws1"})); !ok || h.Properties["domain"] != "unknown.example" {
		t.Errorf("host = %+v (found %v); want domain unknown.example", h, ok)
	}
}

// TestDeviceInventoryFixture: the scenario's host-context record (class 5001)
// mints the WIN-FILE01 host the scenario's other events mint, with its
// properties and the inventory facts in the extensions, and still renders
// bound to it.
func TestDeviceInventoryFixture(t *testing.T) {
	raw, err := os.ReadFile("../fixtures/lateral-movement-via-rdp/host-context-1.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		OCSF map[string]any `json:"ocsf"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	reg := testRegistry()
	evt := makeEvent(f.OCSF)
	res, err := reg.Normalize(evt)
	if err != nil {
		t.Fatal(err)
	}
	host := reg.Identity().Host(identity.HostIdentity{Hostname: "WIN-FILE01"})
	h, ok := scoByID(res.SCOs, host)
	if !ok || h.Properties["hostname"] != "WIN-FILE01" || h.Properties["domain"] != "CONTOSO" {
		t.Fatalf("inventory host = %+v (found %v); want WIN-FILE01 in CONTOSO", h, ok)
	}
	if res.ObservedData[0].Provenance.Normalizer != "inventory_info" || !refsContain(res.ObservedData[0].ObjectRefs, host) {
		t.Errorf("observed-data = %+v", res.ObservedData[0])
	}
	ext := res.ObservedData[0].Extensions
	if ext["os_name"] != "Windows Server 2022" || ext["owner_name"] != "File Services" || ext["criticality"] != "high" {
		t.Errorf("extensions = %v", ext)
	}

	ents := make([]ocsfview.Entity, 0, len(res.SCOs))
	for _, s := range res.SCOs {
		ents = append(ents, ocsfview.Entity{Ref: string(s.ID), Type: s.Type, Props: s.Properties})
	}
	v := ocsfview.Render(ocsfview.Event{ClassUID: 5001, Payload: f.OCSF}, ents)
	if len(v.Sentence) == 0 || v.Sentence[0].T != "entity" || v.Sentence[0].Ref != string(host) {
		t.Errorf("rendered sentence = %+v; want it to open on the host", v.Sentence)
	}
}
