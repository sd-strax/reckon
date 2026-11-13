package main

import (
	"encoding/json"
	"strings"
	"testing"

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
		problems := ocsfschema.ValidateEvent(e.Raw)
		if e.Time.IsZero() {
			// OCSF requires a time. The vendor's record gave none, and the
			// adapter does not make one up (ocsf.SetTime).
			problems = ocsfschema.WithoutRequired(problems, "time")
		}
		for _, p := range problems {
			t.Errorf("event %d, class %d: %s", i, e.ClassUID, p)
		}
	}
}

// unmapped is the event's own fields, where OCSF has none for them.
func unmapped(t *testing.T, e ocsfEvent) map[string]any {
	t.Helper()
	um, ok := e.Raw["unmapped"].(map[string]any)
	if !ok {
		t.Fatalf("no unmapped object in %v", e.Raw)
	}
	return um
}

// Fixtures follow the vendor's shipped v3 response schema (0.5.4):
// classification/actor/last_seen/tags nested under
// internet_scanner_intelligence; tags are objects with a name.

func TestNormalizeMaliciousToDetectionFinding(t *testing.T) {
	text := `{"ip":"45.83.64.1","internet_scanner_intelligence":{"seen":true,"classification":"malicious","actor":"Mirai","last_seen":"2026-08-01","tags":[{"id":"t1","slug":"mirai","name":"Mirai","category":"actor"},{"id":"t2","slug":"ssh-bruteforcer","name":"SSH Bruteforcer","category":"activity"}]},"business_service_intelligence":{"found":false}}`
	evs, err := normalize("lookup-ip-context", text)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	e := evs[0]
	if e.ClassUID != 2004 {
		t.Fatalf("class_uid = %d, want 2004 (detection_finding)", e.ClassUID)
	}
	finding, _ := e.Raw["finding_info"].(map[string]any)
	if finding["uid"] != "greynoise:45.83.64.1" {
		t.Errorf("finding_info.uid = %v", finding["uid"])
	}
	if title, _ := finding["title"].(string); !strings.Contains(title, "Mirai") {
		t.Errorf("finding_info.title = %q, want it to name the actor", title)
	}
	if types, _ := finding["types"].([]string); len(types) != 2 || types[0] != "Mirai" {
		t.Errorf("finding_info.types = %v, want the tag NAMES", finding["types"])
	}
	if e.Time.IsZero() {
		t.Error("date-only last_seen must parse")
	}
	// The event's own time is OCSF's epoch milliseconds, with the same
	// instant as an RFC 3339 string beside it (adr/0356).
	if e.Raw["time"] != int64(1785542400000) || e.Raw["time_dt"] != "2026-08-01T00:00:00Z" {
		t.Errorf("time = %v, time_dt = %v; want 1785542400000 and 2026-08-01T00:00:00Z", e.Raw["time"], e.Raw["time_dt"])
	}
	if e.Raw["confidence_id"] != 3 || e.Raw["confidence"] != "High" {
		t.Errorf("confidence = %v / %v; want 3 / High", e.Raw["confidence_id"], e.Raw["confidence"])
	}
	// The one evidence artifact carries the IP so the engine mints an
	// ipv4-addr SCO.
	artifacts, _ := e.Raw["evidences"].([]any)
	if len(artifacts) != 1 {
		t.Fatalf("evidences = %v; want one artifact", e.Raw["evidences"])
	}
	se, _ := artifacts[0].(map[string]any)["src_endpoint"].(map[string]any)
	if se["ip"] != "45.83.64.1" {
		t.Errorf("evidences[0].src_endpoint.ip = %v, want the IP", se["ip"])
	}
	// OCSF has no field for a pattern or for the GreyNoise record.
	um := unmapped(t, e)
	if um["pattern"] != "[ipv4-addr:value = '45.83.64.1']" {
		t.Errorf("unmapped.pattern = %v", um["pattern"])
	}
	if raw, _ := um["greynoise"].(map[string]any); raw["ip"] != "45.83.64.1" {
		t.Errorf("unmapped.greynoise = %v; want the GreyNoise record", um["greynoise"])
	}
	for _, k := range []string{"finding", "evidence", "greynoise"} {
		if _, has := e.Raw[k]; has {
			t.Errorf("the event carries %s, which OCSF does not define", k)
		}
	}
	assertOCSF(t, evs)
}

// TestEngineReadsTheFinding (adr/0356): the engine's detection normalizer
// reads what the adapter writes. The finding becomes an Indicator with the
// pattern and confidence, attributed to GreyNoise, sighted against the
// address.
func TestEngineReadsTheFinding(t *testing.T) {
	evs, err := normalize("lookup-ip-context", `{"ip":"45.83.64.1","internet_scanner_intelligence":{"classification":"malicious","actor":"Mirai","last_seen":"2026-08-01","tags":[{"name":"Mirai"}]}}`)
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
		ID: uuid.Must(uuid.NewV7()), ClassUID: evs[0].ClassUID, Time: evs[0].Time, SourceTool: "greynoise:lookup-ip-context", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Indicators) != 1 || len(res.Sightings) != 1 {
		t.Fatalf("%d indicators, %d sightings; want one of each", len(res.Indicators), len(res.Sightings))
	}
	ind := res.Indicators[0]
	if ind.Name != "GreyNoise: malicious (Mirai)" || ind.Pattern != "[ipv4-addr:value = '45.83.64.1']" || ind.Confidence == nil || *ind.Confidence != 85 || ind.Provenance.Tool != "GreyNoise" {
		t.Errorf("indicator = %+v", ind)
	}
	if len(ind.IndicatorTypes) != 1 || ind.IndicatorTypes[0] != "Mirai" {
		t.Errorf("indicator types = %v", ind.IndicatorTypes)
	}
	// The address is in the observation the sighting names. It is not in
	// where_sighted_refs, where STIX allows only identity and location objects.
	od := res.ObservedData[0]
	if want := ids.IPv4Addr("45.83.64.1"); len(od.ObjectRefs) != 1 || od.ObjectRefs[0] != want {
		t.Errorf("observed %v; want the address %s", od.ObjectRefs, want)
	}
	if s := res.Sightings[0]; len(s.WhereSighted) != 0 || len(s.ObservedDataRefs) != 1 || s.ObservedDataRefs[0] != od.ID {
		t.Errorf("sighting where %v, observed-data %v; want the observation alone", s.WhereSighted, s.ObservedDataRefs)
	}
}

// TestFindingPatternNamesTheAddressType: an IPv6 address is an ipv6-addr in
// the pattern.
func TestFindingPatternNamesTheAddressType(t *testing.T) {
	evs, _ := normalize("lookup-ip-context", `{"ip":"2001:db8::1","internet_scanner_intelligence":{"classification":"malicious"}}`)
	if got := unmapped(t, evs[0])["pattern"]; got != "[ipv6-addr:value = '2001:db8::1']" {
		t.Errorf("pattern = %v", got)
	}
	// With no tags, no empty types list is written.
	if fi, _ := evs[0].Raw["finding_info"].(map[string]any); fi["types"] != nil {
		t.Errorf("finding_info.types = %v; want it left out", fi["types"])
	}
	assertOCSF(t, evs)
}

func TestNormalizeBenignToOpaqueNoIndicator(t *testing.T) {
	text := `{"ip":"8.8.8.8","internet_scanner_intelligence":{"seen":false,"classification":"benign"},"business_service_intelligence":{"found":true,"name":"Google Public DNS","trust_level":"1"}}`
	evs, err := normalize("lookup-ip-context", text)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(evs) != 1 || evs[0].ClassUID != 0 {
		t.Fatalf("benign must be opaque (class 0), got %+v", evs)
	}
	um := unmapped(t, evs[0])
	if um["reputation"] != "benign" || um["ip"] != "8.8.8.8" {
		t.Errorf("unmapped reputation %v, ip %v; want benign, 8.8.8.8", um["reputation"], um["ip"])
	}
	assertOCSF(t, evs)
}

func TestNormalizeGnqlWrapperUnwraps(t *testing.T) {
	text := `{"data":[{"ip":"1.2.3.4","internet_scanner_intelligence":{"classification":"malicious"}}],"request_metadata":{"count":1}}`
	evs, err := normalize("gnql-query", text)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(evs) != 1 || evs[0].ClassUID != 2004 {
		t.Fatalf("gnql data wrapper must unwrap to a finding, got %+v", evs)
	}
	assertOCSF(t, evs)
}

func TestNormalizeBSIIsBenignEnrichment(t *testing.T) {
	text := `{"ip":"8.8.8.8","business_service_intelligence":{"found":true,"category":"public_dns","name":"Google Public DNS","trust_level":"1"}}`
	evs, err := normalize("bsi-lookup", text)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(evs) != 1 || evs[0].ClassUID != 0 {
		t.Fatalf("BSI must be opaque enrichment, got %+v", evs)
	}
	if got := unmapped(t, evs[0])["reputation"]; got != "benign" {
		t.Errorf("BSI reputation = %v, want benign", got)
	}
	assertOCSF(t, evs)
}
