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

// inventoryEvent is a device_inventory_info record (class 5001) from tool.
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
	if res.ObservedData[0].Provenance.Normalizer != "device_inventory_info" || !refsContain(res.ObservedData[0].ObjectRefs, host) {
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
