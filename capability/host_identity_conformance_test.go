package capability

import (
	"testing"

	"github.com/sd-strax/reckon/identity"
)

// TestHostIdentityConformance is the host identity conformance check
// (adr/0211, `make conformance`): one machine as three tools report it
// normalizes to one strong x-host, each record that carries a device id also
// yields the weak host that id alone mints, aliased to the strong host and kept
// out of the observation's object_refs, and a later record that names the
// machine by its device id alone lands on that weak host, so it reads under
// the strong one through the alias. An adapter for a live source is held to
// the same properties over its own captures.
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
			res, err := reg.Normalize(hostEvent(c.tool, c.device))
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

	// A record from the second tool naming the machine by its agent id alone
	// mints the weak host the second record aliased, so it joins the strong
	// host through that alias.
	res, err := reg.Normalize(hostEvent("host-ids", map[string]any{"uid": "007"}))
	if err != nil {
		t.Fatal(err)
	}
	weak := ids.Host(identity.HostIdentity{SourceTool: "host-ids", SourceUID: "007"})
	if got := scoOfType(res.SCOs, identity.TypeHost); got != weak {
		t.Errorf("device-id-only record minted %s; want the aliased weak host %s", got, weak)
	}
	if !refsContain(res.ObservedData[0].ObjectRefs, weak) {
		t.Error("the device-id-only record's host is not in its object_refs")
	}
}
