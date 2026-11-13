package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// assertOCSF is check 3 of adr/0356 for this adapter: every event it emits
// holds only fields OCSF defines for its class at the version it declares,
// each in the shape and scalar type the schema gives it, and names the class
// it is sent as.
func assertOCSF(t *testing.T, evs []ocsfEvent) {
	t.Helper()
	for i, e := range evs {
		if got := e.Raw["class_uid"]; got != e.ClassUID {
			t.Errorf("event %d: sent as class %d, and the event says %v", i, e.ClassUID, got)
		}
		if meta, _ := e.Raw["metadata"].(map[string]any); meta["version"] != ocsfVersion {
			t.Errorf("event %d: metadata.version = %v; want %s", i, meta["version"], ocsfVersion)
		}
		for _, p := range ocsfschema.ValidateEvent(e.Raw) {
			t.Errorf("event %d, class %d: %s", i, e.ClassUID, p)
		}
	}
}

const detection = `{"resources":[{"composite_id":"abc:ind:123","description":"Credential theft via LSASS access","severity_name":"High","tactic":"Credential Access","technique":"OS Credential Dumping","device":{"hostname":"WIN-FILE01"},"created_timestamp":"2026-08-01T12:00:00Z"}]}`

func TestNormalizeDetectionsToFindings(t *testing.T) {
	evs, err := normalize("search_detections", detection)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(evs) != 1 || evs[0].ClassUID != 2004 {
		t.Fatalf("want one detection_finding, got %+v", evs)
	}
	f, _ := evs[0].Raw["finding_info"].(map[string]any)
	if f["uid"] != "falcon:abc:ind:123" {
		t.Errorf("uid = %v", f["uid"])
	}
	if title, _ := f["title"].(string); !strings.Contains(title, "Credential theft") {
		t.Errorf("title = %q", title)
	}
	if types, _ := f["types"].([]any); !slices.Equal(types, []any{"Credential Access", "OS Credential Dumping"}) {
		t.Errorf("types = %v, want tactic+technique", f["types"])
	}
	if evs[0].Raw["severity"] != "High" || evs[0].Raw["severity_id"] != 4 {
		t.Errorf("severity = %v / %v; want High / 4", evs[0].Raw["severity"], evs[0].Raw["severity_id"])
	}
	// Falcon's severity is not a confidence.
	for _, k := range []string{"confidence", "confidence_id", "confidence_score"} {
		if _, has := evs[0].Raw[k]; has {
			t.Errorf("%s = %v; Falcon's severity is not a confidence", k, evs[0].Raw[k])
		}
	}
	if device, _ := evs[0].Raw["device"].(map[string]any); device["hostname"] != "WIN-FILE01" {
		t.Errorf("device = %v", evs[0].Raw["device"])
	}
	if evs[0].Time.IsZero() {
		t.Error("created_timestamp must parse")
	}
	um, _ := evs[0].Raw["unmapped"].(map[string]any)
	if raw, _ := um["falcon"].(map[string]any); raw["composite_id"] != "abc:ind:123" {
		t.Errorf("unmapped.falcon = %v; want the Falcon record", um["falcon"])
	}
	for _, k := range []string{"finding", "falcon", "hostname"} {
		if _, has := evs[0].Raw[k]; has {
			t.Errorf("the event carries %s, which OCSF does not define", k)
		}
	}
	assertOCSF(t, evs)

	// A severity name OCSF has no number for is kept as the caption alone,
	// and a detection with no tactic writes no empty types list.
	evs, _ = normalize("search_detections", `{"resources":[{"id":"x","severity_name":"Elevated"}]}`)
	if evs[0].Raw["severity"] != "Elevated" || evs[0].Raw["severity_id"] != nil {
		t.Errorf("severity = %v / %v; want the caption and no id", evs[0].Raw["severity"], evs[0].Raw["severity_id"])
	}
	if f, _ := evs[0].Raw["finding_info"].(map[string]any); f["types"] != nil || f["title"] != "Falcon detection" {
		t.Errorf("finding_info = %v", f)
	}
	assertOCSF(t, evs)
}

// TestEngineReadsTheFinding (adr/0356): the engine's detection normalizer
// reads what the adapter writes. The finding becomes an Indicator attributed
// to CrowdStrike. The adapter writes no evidences[], and the engine reads the
// finding's device only as the host of an artifact, so no host is named.
func TestEngineReadsTheFinding(t *testing.T) {
	evs, err := normalize("search_detections", detection)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(evs[0].Raw)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(wire, &payload); err != nil {
		t.Fatal(err)
	}
	ids := identity.NewResolver(uuid.MustParse("33333333-3333-4333-8333-333333333333"))
	res, err := capability.NewRegistry(ids).Normalize(capability.OcsfEvent{
		ID: uuid.Must(uuid.NewV7()), ClassUID: evs[0].ClassUID, Time: evs[0].Time, SourceTool: "crowdstrike-falcon:search_detections", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Indicators) != 1 || len(res.Sightings) != 1 {
		t.Fatalf("%d indicators, %d sightings; want one of each", len(res.Indicators), len(res.Sightings))
	}
	ind := res.Indicators[0]
	if ind.Name != "Credential theft via LSASS access" || ind.Provenance.Tool != "CrowdStrike" || !slices.Equal(ind.IndicatorTypes, []string{"Credential Access", "OS Credential Dumping"}) {
		t.Errorf("indicator = %+v", ind)
	}
	if len(res.SCOs) != 0 || len(res.Sightings[0].WhereSighted) != 0 {
		t.Errorf("%d objects named, sighted against %v; want none without evidences[]", len(res.SCOs), res.Sightings[0].WhereSighted)
	}
}

func TestNormalizeHostsAreOpaque(t *testing.T) {
	text := `{"resources":[{"device_id":"d1","hostname":"WIN-FILE01","platform_name":"Windows"}]}`
	evs, err := normalize("search_hosts", text)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(evs) != 1 || evs[0].ClassUID != 0 {
		t.Fatalf("hosts must be opaque (class 0), got %+v", evs)
	}
	um, _ := evs[0].Raw["unmapped"].(map[string]any)
	if um["hostname"] != "WIN-FILE01" {
		t.Errorf("hostname not surfaced: %v", evs[0].Raw)
	}
	if raw, _ := um["falcon"].(map[string]any); raw["device_id"] != "d1" {
		t.Errorf("unmapped.falcon = %v; want the Falcon record", um["falcon"])
	}
	assertOCSF(t, evs)

	for _, op := range []string{"search_indicators", "anything_else"} {
		evs, err := normalize(op, `{"resources":[{"id":"i1","indicator":"evil.example.com"}]}`)
		if err != nil || len(evs) != 1 || evs[0].ClassUID != 0 {
			t.Fatalf("%s: %v, %+v", op, err, evs)
		}
		assertOCSF(t, evs)
	}
}

// TestEventTimes (adr/0356): an event's time is OCSF's epoch milliseconds,
// with the datetime profile's time_dt beside it as the same instant in RFC
// 3339. An event with no time carries neither.
func TestEventTimes(t *testing.T) {
	evs, err := normalize("search_detections", detection)
	if err != nil {
		t.Fatal(err)
	}
	if evs[0].Raw["time"] != int64(1785585600000) || evs[0].Raw["time_dt"] != "2026-08-01T12:00:00Z" {
		t.Errorf("time = %v, time_dt = %v; want 1785585600000 and 2026-08-01T12:00:00Z", evs[0].Raw["time"], evs[0].Raw["time_dt"])
	}
	if !evs[0].Time.Equal(time.UnixMilli(1785585600000)) {
		t.Errorf("the wire time %s is not the event's", evs[0].Time)
	}
	evs, _ = normalize("search_detections", `{"resources":[{"id":"x"}]}`)
	for _, k := range []string{"time", "time_dt"} {
		if _, has := evs[0].Raw[k]; has {
			t.Errorf("an event with no time carries %s = %v", k, evs[0].Raw[k])
		}
	}
	assertOCSF(t, evs)
}

func TestInferIOCType(t *testing.T) {
	cases := map[string]string{
		"44d88612fea8a8f36de82e1278abb02f":                                 "md5",
		"275a021bbfb6489e54d471899f7db9d1663fc695ec2fe2a2c4538aabf651fd0f": "sha256",
		"198.51.100.7":     "ipv4",
		"2001:db8::1":      "ipv6",
		"evil.example.com": "domain",
		"not_an_ioc":       "",
	}
	for in, want := range cases {
		if got := inferIOCType(in); got != want {
			t.Errorf("inferIOCType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIOCIDsExtraction(t *testing.T) {
	ids := iocIDs(`{"resources":["id-1","id-2"]}`)
	if len(ids) != 2 || ids[0] != "id-1" {
		t.Errorf("string ids = %v", ids)
	}
	ids = iocIDs(`{"resources":[{"id":"id-3","value":"evil.example.com"}]}`)
	if len(ids) != 1 || ids[0] != "id-3" {
		t.Errorf("object ids = %v", ids)
	}
	if ids := iocIDs(`{"resources":[]}`); len(ids) != 0 {
		t.Errorf("empty = %v", ids)
	}
}
