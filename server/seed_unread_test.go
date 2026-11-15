package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
)

// A host inventory that answers with a record that cannot be normalized has
// not said "no such host" (adr/0357). The seed check says the record could not
// be read, with the reason, rather than that the host is unknown.
func TestUnknownHost_UnreadRecordIsNotAbsence(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// An inventory record with no time: OCSF requires one, so it does not
	// normalize.
	body := `{"fixture_meta":{"scenario":"s","matches":{"verb":"get_host_context","params":{}},"delay_ms":0},
	          "ocsf":{"class_uid":5001,"class_name":"Device Inventory Info","device":{"hostname":"WS1"}}}`
	if err := os.WriteFile(filepath.Join(dir, "inv.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tc := capability.TenantConfig{
		Tenant:   "t",
		Adapters: map[string]capability.AdapterSpec{"fixture": {Class: capability.ClassFixture, Enabled: true, Scenario: "s"}},
		Bindings: map[string][]capability.BindingSpec{"get_host_context": {{Adapter: "fixture", Operation: "replay", Priority: 100}}},
	}
	res, cat, err := capability.BuildResolver(tc, root, uuid.New(), true)
	if err != nil {
		t.Fatal(err)
	}
	b := newFakeBackend(t)
	b.cfg.CapabilityResolver, b.cfg.CapabilityCatalog = res, cat
	seed := seedResolution{Seed: aggregate.Seed{Type: aggregate.SeedEntity, EntityRef: string(identity.NewResolver(uuid.New()).Host(identity.HostIdentity{Hostname: "WS1"})), EntityIdentifier: "WS1"}}
	unknown, msg := b.unknownHost(httptest.NewRequest("POST", "/", nil), seed)
	if !unknown || strings.Contains(msg, "no host named") || !strings.Contains(msg, "could not be read") || !strings.Contains(msg, capability.ErrNoEventTime.Error()) {
		t.Errorf("unknownHost = %v, %q; want the unread record and its reason, not no host", unknown, msg)
	}
}
