package stixschema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sd-strax/reckon/stix"
)

// Every OASIS file resolves through the loader, with no network.
func TestEveryOASISSchemaResolves(t *testing.T) {
	files, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 50 {
		t.Fatalf("found %d schema files under %s; want the whole OASIS tree", len(files), Dir)
	}
	for _, f := range files {
		if err := Resolve(f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

// Every reckon definition's schema resolves too.
func TestEveryReckonSchemaResolves(t *testing.T) {
	for _, d := range stix.Definitions() {
		if _, err := reckon(d); err != nil {
			t.Errorf("%s: %v", d.Name, err)
		}
	}
}

const (
	ind = "indicator--8e2e2d2b-17d4-4cbf-938f-98ee46b3cd3f"
	ip  = "ipv4-addr--ff26c055-6336-5bc5-b98d-13d6226742dd"
	ts  = "2026-01-02T03:04:05.000Z"
)

func sighting(of string) map[string]any {
	return map[string]any{
		"type": "sighting", "spec_version": "2.1", "id": "sighting--ee20065d-2555-424f-ad9e-0f8428623c75",
		"created": ts, "modified": ts, "sighting_of_ref": of,
	}
}

func decode(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func wantProblem(t *testing.T, name string, problems []Problem, want string) {
	t.Helper()
	var all []string
	for _, p := range problems {
		all = append(all, p.String())
	}
	if !strings.Contains(strings.Join(all, "\n"), want) {
		t.Errorf("%s: problems = %q; want one containing %q", name, all, want)
	}
}

func TestCheck(t *testing.T) {
	if p := Check(decode(t, sighting(ind))); len(p) != 0 {
		t.Errorf("a valid sighting: %v", p)
	}

	wantProblem(t, "sighting of an SCO", Check(decode(t, sighting(ip))), "does not name an SDO")

	extra := sighting(ind)
	extra["provenance"] = map[string]any{"tool": "edr"}
	wantProblem(t, "custom top-level property", Check(decode(t, extra)), `top-level property "provenance"`)

	unknown := sighting(ind)
	unknown["extensions"] = map[string]any{"extension-definition--00000000-0000-4000-8000-000000000000": map[string]any{"extension_type": "property-extension"}}
	wantProblem(t, "unknown extension", Check(decode(t, unknown)), "is not a reckon extension definition")

	wrongType := sighting(ind)
	wrongType["extensions"] = map[string]any{stix.Key(stix.ExtProvenance): map[string]any{"extension_type": "new-sdo"}}
	wantProblem(t, "wrong extension_type", Check(decode(t, wrongType)), `the definition declares "property-extension"`)
	wantProblem(t, "property-extension schema", Check(decode(t, wrongType)), "reckon-provenance/1.0.0.json")

	traffic := map[string]any{
		"type": "network-traffic", "spec_version": "2.1", "id": "network-traffic--2568d22a-8998-58eb-99ec-3c8ca74f527d",
		"src_ref": ip, "protocols": "tcp",
	}
	wantProblem(t, "OASIS schema", Check(decode(t, traffic)), "observables/network-traffic.json")

	host := map[string]any{"type": "reckon-host", "spec_version": "2.1", "id": "reckon-host--7aa9a12c-441c-41e1-ab22-8bb028fd44e1", "hostname": "ws-1"}
	wantProblem(t, "custom type without its definition", Check(decode(t, host)), "does not carry its own definition")
}

func TestReportGroupsBySource(t *testing.T) {
	var r Report
	bad, _ := json.Marshal(sighting(ip))
	good, _ := json.Marshal(sighting(ind))
	r.Add("normalizer detection_finding v8", bad)
	r.Add("normalizer detection_finding v8", bad)
	r.Add("normalizer detection_finding v8", good)
	if !r.Failed() || r.Total() != 3 || r.Counts()["sighting"] != 3 {
		t.Fatalf("failed %v, counts %v", r.Failed(), r.Counts())
	}
	out := r.String()
	for _, want := range []string{"normalizer detection_finding v8:", "sighting sighting--", "and 1 more", "does not name an SDO"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func sco(typ string, props map[string]any) map[string]any {
	m := map[string]any{"type": typ, "spec_version": "2.1", "id": typ + "--2568d22a-8998-58eb-99ec-3c8ca74f527d"}
	for k, v := range props {
		m[k] = v
	}
	return m
}

// rawOASIS validates against the vendored schema alone, with no correction,
// to show where a correction is needed.
func rawOASIS(t *testing.T, rel string, obj map[string]any) error {
	t.Helper()
	r, err := oasis(rel)
	if err != nil {
		t.Fatal(err)
	}
	return r.Validate(decode(t, obj))
}

// The validator does not check format, so the test does (adr/0357 rule 8).
// Each format catches a value that breaks it and passes one that keeps it.
func TestFormatsAreChecked(t *testing.T) {
	for _, c := range []struct {
		name string
		obj  map[string]any
		bad  string // "" when the object is valid
	}{
		{"uri", sco("url", map[string]any{"value": "https://example.com/login"}), ""},
		{"uri, no scheme", sco("url", map[string]any{"value": "www.example.com/login"}), "format uri"},
		{"uri, a space", sco("url", map[string]any{"value": "http://example.com/a b"}), "format uri"},
		{"email", sco("email-addr", map[string]any{"value": "jdoe@example.com"}), ""},
		{"email, display name", sco("email-addr", map[string]any{"value": "Bob <bob@example.com>"}), "format email"},
		{"email, unquoted comma", sco("email-addr", map[string]any{"value": "a,b@example.com"}), "format email"},
		{"idn-hostname", sco("domain-name", map[string]any{"value": "bücher.example"}), ""},
		{"idn-hostname, underscore label kept", sco("domain-name", map[string]any{"value": "_ldap._tcp.corp.example"}), ""},
		{"idn-hostname, a backslash", sco("domain-name", map[string]any{"value": `CONTOSO\`}), "format idn-hostname"},
		{"idn-hostname, a space", sco("domain-name", map[string]any{"value": "not a name"}), "format idn-hostname"},
		{"uri in an external reference", map[string]any{
			"type": "indicator", "spec_version": "2.1", "id": ind, "created": ts, "modified": ts,
			"pattern": "x", "pattern_type": "vendor", "valid_from": ts,
			"external_references": []any{map[string]any{"source_name": "s", "url": "not a uri"}},
		}, "external_references[0].url"},
		{"date-time in a reckon extension", sighting(ind), ""},
	} {
		obj := c.obj
		if c.name == "date-time in a reckon extension" {
			obj["extensions"] = map[string]any{stix.Key(stix.ExtProvenance): map[string]any{
				"extension_type": "property-extension", "derivation_mode": "DIRECT", "tool": "t",
				"normalizer": "n", "normalizer_version": 1, "observed_at": "2026-01-02 03:04:05",
			}}
			c.bad = "format date-time"
		}
		problems := Check(decode(t, obj))
		if c.bad == "" {
			if len(problems) != 0 {
				t.Errorf("%s: %v", c.name, problems)
			}
			continue
		}
		wantProblem(t, c.name, problems, c.bad)
	}
}

// Each correction catches what the spec forbids, and passes what the spec
// allows though the vendored pattern refuses it (adr/0357 rule 8). The rawOASIS
// lines show the vendored schema's own verdict, which is why the correction
// exists.
func TestCorrections(t *testing.T) {
	// ipv6-addr, §6.9.1.
	embedded := sco("ipv6-addr", map[string]any{"value": "64:ff9b::192.0.2.1"})
	if rawOASIS(t, "observables/ipv6-addr.json", embedded) == nil {
		t.Error("the vendored ipv6-addr pattern now accepts an embedded dotted quad; the correction may be obsolete")
	}
	for _, v := range []string{"64:ff9b::192.0.2.1", "2001:db8::/96", "fe80::1"} {
		if p := Check(decode(t, sco("ipv6-addr", map[string]any{"value": v}))); len(p) != 0 {
			t.Errorf("ipv6 %q: %v", v, p)
		}
	}
	for _, v := range []string{"10.0.0.1", "fe80::1%eth0", "not an address", " ::1"} {
		wantProblem(t, "ipv6 "+v, Check(decode(t, sco("ipv6-addr", map[string]any{"value": v}))), "§6.9.1")
	}

	// windows-registry-key key, §6.17.1.
	tools := sco("windows-registry-key", map[string]any{"key": `HKEY_CURRENT_USER\Software\HKCUTools`})
	if rawOASIS(t, "observables/windows-registry-key.json", tools) == nil {
		t.Error("the vendored registry key pattern now accepts HKCU after the root; the correction may be obsolete")
	}
	if p := Check(decode(t, tools)); len(p) != 0 {
		t.Errorf("a later HKCU: %v", p)
	}
	for _, k := range []string{`HKLM\Software\Run`, `Software\Run`, `\REGISTRY\A\{01234567-89ab-cdef-0123-456789abcdef}\x`, `hkcu\x`} {
		wantProblem(t, "key "+k, Check(decode(t, sco("windows-registry-key", map[string]any{"key": k}))), "fully expanded hive")
	}

	// windows-registry-key anyOf and process anyOf, §6.17.1 and §6.13.1.
	modified := sco("windows-registry-key", map[string]any{"modified_time": ts})
	if rawOASIS(t, "observables/windows-registry-key.json", modified) == nil {
		t.Error("the vendored registry anyOf now names modified_time; the correction may be obsolete")
	}
	if p := Check(decode(t, modified)); len(p) != 0 {
		t.Errorf("a key with modified_time alone: %v", p)
	}
	wantProblem(t, "an empty key", Check(decode(t, sco("windows-registry-key", nil))), "§6.17.1")
	started := sco("process", map[string]any{"created_time": ts})
	if rawOASIS(t, "observables/process.json", started) == nil {
		t.Error("the vendored process anyOf now names created_time; the correction may be obsolete")
	}
	if p := Check(decode(t, started)); len(p) != 0 {
		t.Errorf("a process with created_time alone: %v", p)
	}
	wantProblem(t, "an empty process", Check(decode(t, sco("process", nil))), "§6.13.1")

	// Property names, §3.1: the vendored pattern admits upper case after the
	// third character.
	extra := sighting(ind)
	extra["extensions"] = map[string]any{stix.Key(stix.ExtProvenance): map[string]any{"extension_type": "property-extension", "toolName": "x"}}
	wantProblem(t, "an upper case name", Check(decode(t, extra)), `extension property "toolName"`)
	upper := sighting(ind)
	upper["countX"] = 1
	wantProblem(t, "an upper case top-level name", Check(decode(t, upper)), `property "countX" is not a valid property name`)
}
