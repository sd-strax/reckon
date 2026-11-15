package stix

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sd-strax/reckon/identity"
)

var testTime = time.Date(2026, 1, 2, 3, 4, 5, 678901234, time.UTC)

func build(t *testing.T, o Object) map[string]any {
	t.Helper()
	b, err := Marshal(o)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMarshal_NativeSCO(t *testing.T) {
	got := build(t, Object{
		Type: identity.TypeFile,
		ID:   "file--a3f5c1d2-0000-4000-8000-000000000001",
		Properties: map[string]any{
			"name":   "evil.exe",
			"path":   `C:\Temp\evil.exe`,
			"hashes": map[string]any{"SHA-256": "ab"},
		},
	})
	for k, want := range map[string]any{"type": "file", "spec_version": "2.1", "id": "file--a3f5c1d2-0000-4000-8000-000000000001", "name": "evil.exe"} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
	if _, ok := got["hashes"].(map[string]any); !ok {
		t.Errorf("hashes are not at the top level: %v", got)
	}
	if _, ok := got["properties"]; ok {
		t.Error("an SCO keeps a nested properties key")
	}
	if _, ok := got["path"]; ok {
		t.Error("path, which STIX does not define on file, is at the top level")
	}
	ext := ExtensionFrom(got, ExtFile)
	if ext["path"] != `C:\Temp\evil.exe` || ext["extension_type"] != "property-extension" {
		t.Errorf("reckon-file = %v", ext)
	}
	if FilePath.String(got) != `C:\Temp\evil.exe` {
		t.Errorf("FilePath = %q", FilePath.String(got))
	}
	for _, k := range []string{"created", "modified"} {
		if _, ok := got[k]; ok {
			t.Errorf("an SCO has %s", k)
		}
	}
}

func TestMarshal_NativeSplit(t *testing.T) {
	cases := []struct {
		typ, id, ext   string
		native, custom map[string]any
	}{
		{identity.TypeUserAccount, "user-account--00000000-0000-4000-8000-000000000001", ExtAccount,
			map[string]any{"account_login": `CONTOSO\jdoe`, "user_id": "S-1-5-21-1", "account_type": "windows-domain", "display_name": "Jo Doe", "is_service_account": false},
			map[string]any{"domain": "CONTOSO", "principal_name": "jdoe@contoso.com", "source_ids": map[string]any{"edr": "1"}, "weak": true}},
		{identity.TypeIdentity, "identity--00000000-0000-4000-8000-000000000002", ExtPerson,
			map[string]any{"name": "Jo Doe", "identity_class": "individual"},
			map[string]any{"mail": "jdoe@contoso.com", "directory": "entra", "directory_id": "g-1", "enabled": true, "manager_ref": "identity--00000000-0000-4000-8000-000000000003"}},
		{identity.TypeProcess, "process--00000000-0000-4000-8000-000000000004", ExtProcess,
			map[string]any{"pid": 42, "command_line": "cmd /c whoami"},
			map[string]any{"name": "cmd.exe"}},
		{identity.TypeEmailMessage, "email-message--00000000-0000-4000-8000-000000000005", ExtEmailMessage,
			map[string]any{"is_multipart": false},
			map[string]any{"thread_uid": "t-1", "envelope_to_refs": []string{"email-addr--00000000-0000-4000-8000-000000000006"}}},
	}
	for _, c := range cases {
		props := map[string]any{}
		for k, v := range c.native {
			props[k] = v
		}
		for k, v := range c.custom {
			props[k] = v
		}
		o := Object{Type: c.typ, ID: c.id, Properties: props}
		if Timestamped(c.typ) {
			o.Created = testTime
		}
		got := build(t, o)
		ext := ExtensionFrom(got, c.ext)
		for k := range c.native {
			if _, ok := got[k]; !ok {
				t.Errorf("%s: native %s is not at the top level", c.typ, k)
			}
			if _, ok := ext[k]; ok {
				t.Errorf("%s: native %s is in %s", c.typ, k, c.ext)
			}
		}
		for k := range c.custom {
			if _, ok := got[k]; ok {
				t.Errorf("%s: custom %s is at the top level", c.typ, k)
			}
			if _, ok := ext[k]; !ok {
				t.Errorf("%s: custom %s is not in %s", c.typ, k, c.ext)
			}
		}
	}
}

// TestMarshal_PersonIdentityHasTimes: a person identity is an SDO, so it has
// created and modified (adr/0357).
func TestMarshal_PersonIdentityHasTimes(t *testing.T) {
	got := build(t, Object{Type: identity.TypeIdentity, ID: "identity--00000000-0000-4000-8000-000000000002", Created: testTime,
		Properties: map[string]any{"name": "Jo Doe", "identity_class": "individual"}})
	if got["created"] != "2026-01-02T03:04:05.678Z" || got["modified"] != "2026-01-02T03:04:05.678Z" {
		t.Errorf("created %v, modified %v", got["created"], got["modified"])
	}
}

func TestMarshal_CustomSCO(t *testing.T) {
	got := build(t, Object{
		Type:       identity.TypeHost,
		ID:         "reckon-host--00000000-0000-4000-8000-000000000006",
		Properties: map[string]any{"hostname": "ws1", "fqdn": "ws1.contoso.com", "agents": []any{map[string]any{"name": "edr"}}},
		Extensions: map[string]map[string]any{ExtProvenance: {"derivation_mode": "DIRECT", "tool": "edr", "normalizer": "process_activity", "normalizer_version": 4, "observed_at": testTime}},
	})
	if got["hostname"] != "ws1" || got["fqdn"] != "ws1.contoso.com" {
		t.Errorf("own properties are not at the top level: %v", got)
	}
	d, _ := Lookup(identity.TypeHost)
	exts, _ := got["extensions"].(map[string]any)
	def, _ := exts[d.ID].(map[string]any)
	if len(def) != 1 || def["extension_type"] != "new-sco" {
		t.Errorf("definition entry = %v", def)
	}
	prov := ExtensionFrom(got, ExtProvenance)
	if prov["tool"] != "edr" || prov["observed_at"] != "2026-01-02T03:04:05.678901234Z" {
		t.Errorf("reckon-provenance = %v", prov)
	}
}

func TestMarshal_CustomSDO(t *testing.T) {
	got := build(t, Object{
		Type:       identity.TypeHypothesis,
		ID:         "reckon-hypothesis--00000000-0000-4000-8000-000000000007",
		Created:    testTime,
		Modified:   testTime.Add(time.Hour),
		Properties: map[string]any{"statement": "lateral movement", "status": "OPEN", "labels": []string{"T1021"}},
	})
	if got["created"] != "2026-01-02T03:04:05.678Z" || got["modified"] != "2026-01-02T04:04:05.678Z" {
		t.Errorf("created %v, modified %v", got["created"], got["modified"])
	}
	if got["statement"] != "lateral movement" || got["status"] != "OPEN" {
		t.Errorf("own properties are not at the top level: %v", got)
	}
	d, _ := Lookup(identity.TypeHypothesis)
	exts, _ := got["extensions"].(map[string]any)
	if def, _ := exts[d.ID].(map[string]any); def["extension_type"] != "new-sdo" {
		t.Errorf("definition entry = %v", exts)
	}
}

func TestMarshal_Relationship(t *testing.T) {
	got := build(t, Object{
		Type:    "relationship",
		ID:      "relationship--00000000-0000-4000-8000-000000000008",
		Created: testTime,
		Properties: map[string]any{
			"relationship_type": "parent-process-of",
			"source_ref":        identity.STIXID("process--00000000-0000-4000-8000-000000000009"),
			"target_ref":        "process--00000000-0000-4000-8000-00000000000a",
		},
	})
	if got["type"] != "relationship" || got["relationship_type"] != "parent-process-of" ||
		got["source_ref"] != "process--00000000-0000-4000-8000-000000000009" || got["target_ref"] == nil {
		t.Errorf("relationship = %v", got)
	}
	if _, ok := got["extensions"]; ok {
		t.Errorf("a relationship with no assertion has extensions: %v", got)
	}
}

func TestMarshal_EmptyValuesAreLeftOut(t *testing.T) {
	var noTime *time.Time
	got := build(t, Object{
		Type: identity.TypeEmailMessage,
		ID:   "email-message--00000000-0000-4000-8000-00000000000b",
		Properties: map[string]any{
			"is_multipart": false, "message_id": "", "subject": nil, "to_refs": []string{},
			"date": noTime, "cc_refs": []string(nil),
		},
	})
	if got["is_multipart"] != false {
		t.Errorf("is_multipart false was dropped: %v", got)
	}
	for _, k := range []string{"message_id", "subject", "to_refs", "date", "cc_refs", "extensions"} {
		if _, ok := got[k]; ok {
			t.Errorf("empty %s was written", k)
		}
	}
}

func TestMarshal_Errors(t *testing.T) {
	cases := map[string]Object{
		"unlisted property": {Type: identity.TypeFile, ID: "file--00000000-0000-4000-8000-000000000001",
			Properties: map[string]any{"colour": "red"}},
		"unlisted property on a type with no extension": {Type: identity.TypeIPv4Addr, ID: "ipv4-addr--00000000-0000-4000-8000-000000000001",
			Properties: map[string]any{"value": "10.0.0.1", "asn": 1}},
		"unlisted property on a custom type": {Type: identity.TypeHost, ID: "reckon-host--00000000-0000-4000-8000-000000000001",
			Properties: map[string]any{"hostname": "ws1", "colour": "red"}},
		"unlisted extension property": {Type: identity.TypeObservedData, ID: "observed-data--00000000-0000-4000-8000-000000000001", Created: testTime,
			Extensions: map[string]map[string]any{ExtObservation: {"colour": "red"}}},
		"extension the type does not take": {Type: identity.TypeFile, ID: "file--00000000-0000-4000-8000-000000000001",
			Extensions: map[string]map[string]any{ExtAccount: {"domain": "x"}}},
		"property the serializer sets": {Type: identity.TypeFile, ID: "file--00000000-0000-4000-8000-000000000001",
			Properties: map[string]any{"type": "file"}},
		"unknown type":        {Type: "x-host", ID: "x-host--00000000-0000-4000-8000-000000000001"},
		"id of another type":  {Type: identity.TypeFile, ID: "process--00000000-0000-4000-8000-000000000001"},
		"SCO with created":    {Type: identity.TypeFile, ID: "file--00000000-0000-4000-8000-000000000001", Created: testTime},
		"SDO with no created": {Type: "indicator", ID: "indicator--00000000-0000-4000-8000-000000000001"},
		"property given twice": {Type: identity.TypeFile, ID: "file--00000000-0000-4000-8000-000000000001",
			Properties: map[string]any{"path": "/a"}, Extensions: map[string]map[string]any{ExtFile: {"path": "/b"}}},
	}
	for name, o := range cases {
		if _, err := Marshal(o); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestNativeAndExtensionListsDoNotOverlap: a type whose custom properties
// arrive among its own has none that STIX also defines, so the split never
// has to choose.
func TestNativeAndExtensionListsDoNotOverlap(t *testing.T) {
	for typ, s := range nativeTypes {
		if s.ext == "" || typ == identity.TypeObservedData {
			// observed-data's extension fields arrive apart, in Extensions,
			// because reckon-observation holds an OCSF labels list.
			continue
		}
		own, _ := TypeProperties(typ)
		for _, p := range ExtensionProperties(s.ext) {
			if slices.Contains(own, p) {
				t.Errorf("%s: %s is both a STIX property and in %s", typ, p, s.ext)
			}
		}
	}
}

func TestEveryDefinitionTypeIsWritable(t *testing.T) {
	for _, d := range Definitions() {
		if d.ExtensionTypes[0] == PropertyExtension {
			if len(ExtensionProperties(d.Name)) == 0 {
				t.Errorf("%s lists no properties", d.Name)
			}
			continue
		}
		k, ok := KindOf(d.Name)
		want := SDO
		if d.ExtensionTypes[0] == NewSCO {
			want = SCO
		}
		if !ok || k != want {
			t.Errorf("%s: kind %v, %v; want %v", d.Name, k, ok, want)
		}
	}
}

func TestTimestamp(t *testing.T) {
	loc := time.FixedZone("x", 3600)
	if got := Timestamp(time.Date(2026, 1, 2, 4, 4, 5, 0, loc)); got != "2026-01-02T03:04:05.000Z" {
		t.Errorf("Timestamp = %q", got)
	}
	if got := Truncate(testTime); !got.Equal(time.Date(2026, 1, 2, 3, 4, 5, 678000000, time.UTC)) {
		t.Errorf("Truncate = %v", got)
	}
}

func TestPropPaths(t *testing.T) {
	d, _ := Lookup(ExtAssertion)
	if got, want := AssertionWithdrawnAt.SQL("e.payload"), "e.payload->'extensions'->'"+d.ID+"'->>'withdrawn_at'"; got != want {
		t.Errorf("SQL = %q, want %q", got, want)
	}
	if got := HasExtensionSQL("payload", ExtAssertion); !strings.Contains(got, d.ID) || !strings.Contains(got, "?") {
		t.Errorf("HasExtensionSQL = %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("prop accepted a property its extension does not list")
		}
	}()
	prop(ExtAccount, "colour")
}

func TestDecodeExtension(t *testing.T) {
	b, err := Marshal(Object{Type: "relationship", ID: "relationship--00000000-0000-4000-8000-000000000008", Created: testTime,
		Properties: map[string]any{"relationship_type": "aliases", "source_ref": "reckon-host--00000000-0000-4000-8000-0000000000a1", "target_ref": "reckon-host--00000000-0000-4000-8000-0000000000b2"},
		Extensions: map[string]map[string]any{ExtAssertion: {"asserted_by": "u-1", "asserted_at": testTime}}})
	if err != nil {
		t.Fatal(err)
	}
	var a struct {
		AssertedBy  string     `json:"asserted_by"`
		WithdrawnAt *time.Time `json:"withdrawn_at"`
	}
	found, err := DecodeExtension(b, ExtAssertion, &a)
	if err != nil || !found || a.AssertedBy != "u-1" || a.WithdrawnAt != nil {
		t.Errorf("DecodeExtension = %v, %v, %+v", found, err, a)
	}
	if found, err := DecodeExtension(b, ExtProvenance, &a); found || err != nil {
		t.Errorf("DecodeExtension of a missing extension = %v, %v", found, err)
	}
}

// TestWorkbenchExtensionIDs: the workbench names the property-extensions' ids
// in one module. They must be the ids defined here.
func TestWorkbenchExtensionIDs(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "workbench", "src", "stixExtensions.ts"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range regexp.MustCompile(`"(reckon-[a-z-]+)": "(extension-definition--[0-9a-f-]+)"`).FindAllStringSubmatch(string(b), -1) {
		got[m[1]] = m[2]
	}
	want := map[string]string{}
	for _, d := range Definitions() {
		if d.ExtensionTypes[0] == PropertyExtension {
			want[d.Name] = d.ID
		}
	}
	if !maps.Equal(got, want) {
		t.Errorf("workbench ids = %v; want %v", got, want)
	}
}

// Class rule (adr/0357): the serializer refuses an object missing a
// property its schema requires, whether the type's own or an extension's,
// at runtime, so no caller can store one.
func TestMarshal_RefusesAMissingRequiredProperty(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	prov := func(tool string) map[string]map[string]any {
		return map[string]map[string]any{ExtProvenance: {"derivation_mode": "DIRECT", "tool": tool, "normalizer": "n", "normalizer_version": 1, "observed_at": at}}
	}
	for name, c := range map[string]struct {
		o    Object
		want string
	}{
		"empty provenance tool": {Object{Type: identity.TypeHost, ID: "reckon-host--00000000-0000-4000-8000-000000000001",
			Properties: map[string]any{"hostname": "ws1"}, Extensions: prov("")}, "reckon-provenance requires tool"},
		"empty hypothesis statement": {Object{Type: identity.TypeHypothesis, ID: "reckon-hypothesis--00000000-0000-4000-8000-000000000002", Created: at,
			Properties: map[string]any{"statement": "", "status": "OPEN"}}, "reckon-hypothesis requires statement"},
		"an indicator with no pattern": {Object{Type: "indicator", ID: "indicator--00000000-0000-4000-8000-000000000003", Created: at,
			Properties: map[string]any{"pattern_type": "vendor", "valid_from": at}}, "indicator requires pattern"},
		"a file with no name and no hash": {Object{Type: identity.TypeFile, ID: "file--00000000-0000-4000-8000-000000000004",
			Properties: map[string]any{"name": ""}}, "requires one of hashes, name"},
	} {
		if _, err := Marshal(c.o); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v; want one saying %q", name, err, c.want)
		}
	}
	ok := Object{Type: identity.TypeHost, ID: "reckon-host--00000000-0000-4000-8000-000000000001", Properties: map[string]any{"hostname": "ws1"}, Extensions: prov("edr")}
	if _, err := Marshal(ok); err != nil {
		t.Errorf("a complete object: %v", err)
	}
}

// Every _ref and _refs value is a STIX identifier, checked at runtime
// (adr/0357).
func TestMarshal_RefusesARefThatIsNotAnIdentifier(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rel := func(src string) Object {
		return Object{Type: "relationship", ID: "relationship--00000000-0000-4000-8000-000000000009", Created: at,
			Properties: map[string]any{"relationship_type": "aliases", "source_ref": src, "target_ref": "reckon-host--00000000-0000-4000-8000-0000000000b2"}}
	}
	if _, err := Marshal(rel("reckon-host--00000000-0000-4000-8000-0000000000a1")); err != nil {
		t.Errorf("a valid ref: %v", err)
	}
	for _, bad := range []string{"WS1", "a--1", "00000000-0000-4000-8000-0000000000a1"} {
		if _, err := Marshal(rel(bad)); err == nil || !strings.Contains(err.Error(), "not a STIX identifier") {
			t.Errorf("source_ref %q: err = %v", bad, err)
		}
	}
	od := Object{Type: identity.TypeObservedData, ID: "observed-data--00000000-0000-4000-8000-00000000000a", Created: at,
		Properties: map[string]any{"first_observed": at, "last_observed": at, "number_observed": 1, "object_refs": []string{"reckon-host--1"}}}
	if _, err := Marshal(od); err == nil {
		t.Error("an object_refs value that is not an identifier was accepted")
	}
}
