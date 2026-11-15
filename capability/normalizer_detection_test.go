package capability

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/stix"
)

// detectionPayload is a detection finding at the OCSF floor: the host is the
// finding's own device, and the one evidence artifact carries the process and
// the acting user.
func detectionPayload() map[string]any {
	return map[string]any{
		"class_uid":     2004,
		"time":          ms("2026-04-20T14:32:11Z"),
		"metadata":      map[string]any{"product": map[string]any{"vendor_name": "CrowdStrike"}},
		"severity_id":   4,
		"confidence_id": 3,
		"finding_info": map[string]any{
			"uid":   "det-123",
			"title": "Suspicious PowerShell",
			"types": []any{"T1059.001", "T1086"},
		},
		"device": map[string]any{"hostname": "WIN-DC01", "domain": "CONTOSO"},
		"evidences": []any{map[string]any{
			"process": map[string]any{
				"pid":          4242,
				"name":         "powershell.exe",
				"created_time": ms("2026-04-20T14:32:11Z"),
			},
			"actor": map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": "S-1-5-21-7-1101"}},
		}},
	}
}

// TestDetectionNormalizer covers §4.12: an Indicator + Sighting with INFERRED
// provenance attributed to a vendor Identity, on top of the DIRECT entities
// read from the evidence.
func TestDetectionNormalizer(t *testing.T) {
	res, err := testRegistry().Normalize(makeEvent(detectionPayload()))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	// Indicator: INFERRED, vendor-attributed, carries technique IDs.
	if len(res.Indicators) != 1 {
		t.Fatalf("indicators = %d; want 1", len(res.Indicators))
	}
	ind := res.Indicators[0]
	if ind.Provenance.DerivationMode != DerivationInferred {
		t.Errorf("indicator derivation_mode = %q; want INFERRED", ind.Provenance.DerivationMode)
	}
	if ind.Provenance.Tool != "CrowdStrike" {
		t.Errorf("indicator tool = %q; want CrowdStrike", ind.Provenance.Tool)
	}
	// The technique ids in finding_info.types are external references, not
	// indicator types (adr/0357).
	// The finding uid follows them, under the vendor's name.
	attack := []ExternalReference{{SourceName: "mitre-attack", ExternalID: "T1059.001"}, {SourceName: "mitre-attack", ExternalID: "T1086"}, {SourceName: "CrowdStrike", ExternalID: "det-123"}}
	if ind.Name != "Suspicious PowerShell" || ind.IndicatorTypes != nil || ind.Labels != nil || !reflect.DeepEqual(ind.ExternalReferences, attack) {
		t.Errorf("indicator name %q, types %v, references %v; want no types, the two techniques and the finding uid", ind.Name, ind.IndicatorTypes, ind.ExternalReferences)
	}
	if !confidenceIs(ind.Confidence, 85) {
		t.Errorf("confidence = %s; want 85 (confidence_id 3, High)", confidenceText(ind.Confidence))
	}
	// No vendor pattern: the pattern is the finding uid and its type the
	// tool's name. The id is minted from the stored vendor, pattern, type
	// and uid (adr/0357).
	if ind.ID != identity.NewResolver(testNS).Indicator(ind.CreatedByRef, "det-123", "CrowdStrike", "det-123") || ind.Pattern != "det-123" || ind.PatternType != "CrowdStrike" {
		t.Errorf("indicator %s pattern %q/%q; want the id of the stored pattern det-123/CrowdStrike", ind.ID, ind.Pattern, ind.PatternType)
	}

	// Vendor Identity is the created_by_ref target and is emitted.
	if len(res.Identities) != 1 || res.Identities[0].ID != ind.CreatedByRef {
		t.Errorf("vendor identity missing or not the created_by_ref: %+v", res.Identities)
	}

	// One DIRECT ObservedData for the detection event, naming the evidence
	// entities: the host from the finding's device, the process and the
	// account from the artifact.
	if len(res.ObservedData) != 1 {
		t.Fatalf("ObservedData = %d; want 1", len(res.ObservedData))
	}
	od := res.ObservedData[0]
	if od.Provenance.DerivationMode != DerivationDirect || od.Provenance.Normalizer != "detection_finding" {
		t.Errorf("observed-data provenance = %+v", od.Provenance)
	}
	ids := identity.NewResolver(testNS)
	host := ids.Host(identity.HostIdentity{Hostname: "WIN-DC01"})
	proc, _ := ids.Process(host, 4242, makeEvent(detectionPayload()).Time)
	account := ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: "S-1-5-21-7-1101"})
	for name, id := range map[string]identity.STIXID{"host": host, "process": proc, "account": account} {
		if !refsContain(od.ObjectRefs, id) {
			t.Errorf("object_refs lack the %s %s: %v", name, id, od.ObjectRefs)
		}
	}
	for _, sco := range res.SCOs {
		if sco.Provenance.DerivationMode != DerivationDirect {
			t.Errorf("evidence SCO %s is %q; want DIRECT", sco.Type, sco.Provenance.DerivationMode)
		}
	}

	// Sighting references the indicator and the observation. The entities
	// are the observation's object_refs, not where_sighted_refs, where STIX
	// allows only identity and location objects (adr/0356). The vendor saw
	// it, so where_sighted_refs is the vendor identity (adr/0357).
	if len(res.Sightings) != 1 {
		t.Fatalf("sightings = %d; want 1", len(res.Sightings))
	}
	s := res.Sightings[0]
	if s.SightingOfRef != ind.ID || !confidenceIs(s.Confidence, 85) {
		t.Errorf("sighting_of_ref = %q, confidence %s; want indicator %q at 85", s.SightingOfRef, confidenceText(s.Confidence), ind.ID)
	}
	if !reflect.DeepEqual(s.WhereSighted, []identity.STIXID{ind.CreatedByRef}) || !reflect.DeepEqual(s.ObservedDataRefs, []identity.STIXID{od.ID}) {
		t.Errorf("sighting where %v, observed-data %v; want the vendor %s and the observation", s.WhereSighted, s.ObservedDataRefs, ind.CreatedByRef)
	}
	// The login account the record aliases to the SID account is linked, not
	// sighted (adr/0324).
	if len(scosOf(res, identity.TypeUserAccount)) != 2 || len(accountsIn(res)) != 1 {
		t.Errorf("accounts: %d minted, %d in object_refs; want 2 and 1", len(scosOf(res, identity.TypeUserAccount)), len(accountsIn(res)))
	}
}

// TestDetectionNormalizerNoEvidence: a detection with no evidences[] still
// yields an Indicator + Sighting + vendor Identity, and its own device still
// names the host it fired on (adr/0356).
func TestDetectionNormalizerNoEvidence(t *testing.T) {
	res, err := testRegistry().Normalize(makeEvent(map[string]any{
		"class_uid":        2004,
		"time":             ms("2026-04-20T14:32:11Z"),
		"metadata":         map[string]any{"product": map[string]any{"vendor_name": "Defender"}},
		"finding_info":     map[string]any{"uid": "d-1", "title": "Malware"},
		"confidence_score": 42,
		"device":           map[string]any{"hostname": "WS1"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Indicators) != 1 || len(res.Sightings) != 1 || len(res.Identities) != 1 {
		t.Fatalf("want 1 indicator/sighting/identity; got %d/%d/%d",
			len(res.Indicators), len(res.Sightings), len(res.Identities))
	}
	if !confidenceIs(res.Indicators[0].Confidence, 42) {
		t.Errorf("confidence = %s; want 42 (confidence_score)", confidenceText(res.Indicators[0].Confidence))
	}
	// The finding's own device names the host, with no artifact to name it.
	host := identity.NewResolver(testNS).Host(identity.HostIdentity{Hostname: "WS1"})
	if len(res.SCOs) != 1 || res.SCOs[0].ID != host || res.SCOs[0].Provenance.DerivationMode != DerivationDirect {
		t.Errorf("SCOs = %+v; want the host WS1 alone, DIRECT", res.SCOs)
	}
	if refs := res.ObservedData[0].ObjectRefs; len(refs) != 1 || refs[0] != host {
		t.Errorf("object_refs = %v; want the host %s", refs, host)
	}
	// §4.12: the raw "this was seen" signal must survive even without evidence —
	// one ObservedData, referenced by the Sighting.
	if len(res.ObservedData) != 1 {
		t.Fatalf("ObservedData = %d; want 1 (raw signal preserved)", len(res.ObservedData))
	}
	if len(res.Sightings[0].ObservedDataRefs) != 1 || res.Sightings[0].ObservedDataRefs[0] != res.ObservedData[0].ID {
		t.Error("Sighting does not reference the detection's raw ObservedData")
	}
	if w := res.Sightings[0].WhereSighted; len(w) != 1 || w[0] != res.Identities[0].ID {
		t.Errorf("where_sighted = %v; want the vendor identity alone", w)
	}

	// With neither a device nor evidences[], nothing is named.
	// The observation's one object is then the record's artifact (adr/0357).
	bare := makeEvent(map[string]any{
		"class_uid": 2004, "time": ms("2026-04-20T14:32:11Z"), "finding_info": map[string]any{"uid": "d-1"},
	})
	res, err = testRegistry().Normalize(bare)
	if err != nil {
		t.Fatal(err)
	}
	recordArtifactOnly(t, bare, res)
}

// confidenceIs reports whether a confidence is known and equals want.
func confidenceIs(c *int, want int) bool { return c != nil && *c == want }

// confidenceText prints a confidence, "unknown" for nil.
func confidenceText(c *int) string {
	if c == nil {
		return "unknown"
	}
	return strconv.Itoa(*c)
}

// TestDetectionConfidence (§4.12, adr/0356): confidence_score is capped to
// 0–100, and a score of 0 is kept. Without a score, confidence_id maps to the
// Low/Medium/High values. Otherwise the confidence is unknown: it is nil and
// left out of the JSON, never written as 0. The confidence caption is not
// read.
func TestDetectionConfidence(t *testing.T) {
	const unknown = -1
	for _, tc := range []struct {
		name   string
		fields map[string]any
		want   int
	}{
		{"score", map[string]any{"confidence_score": 73}, 73},
		{"score as JSON decodes it", map[string]any{"confidence_score": float64(73)}, 73},
		{"score outranks id", map[string]any{"confidence_score": 73, "confidence_id": 1}, 73},
		{"id low", map[string]any{"confidence_id": 1}, 15},
		{"id medium", map[string]any{"confidence_id": 2}, 50},
		{"id high", map[string]any{"confidence_id": 3}, 85},
		{"id unknown", map[string]any{"confidence_id": 0}, unknown},
		{"id other", map[string]any{"confidence_id": 99}, unknown},
		{"a zero score is kept", map[string]any{"confidence_score": 0, "confidence_id": 2}, 0},
		{"a score above 100 is capped", map[string]any{"confidence_score": 250, "confidence_id": 3}, 100},
		{"a score below 0 is capped", map[string]any{"confidence_score": -5}, 0},
		{"a score that is not a whole number falls back to the id", map[string]any{"confidence_score": "high", "confidence_id": 3}, 85},
		{"caption alone", map[string]any{"confidence": "High"}, unknown},
		{"nothing", map[string]any{}, unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := map[string]any{"class_uid": 2004, "time": ms("2026-04-20T14:32:11Z"), "finding_info": map[string]any{"uid": "d-1"}}
			for k, v := range tc.fields {
				p[k] = v
			}
			res, err := testRegistry().Normalize(makeEvent(p))
			if err != nil {
				t.Fatal(err)
			}
			ind, sighting := res.Indicators[0], res.Sightings[0]
			for name, got := range map[string]*int{"indicator": ind.Confidence, "sighting": sighting.Confidence} {
				if tc.want == unknown && got != nil || tc.want != unknown && !confidenceIs(got, tc.want) {
					t.Errorf("%s confidence = %s; want %d (-1 is unknown)", name, confidenceText(got), tc.want)
				}
			}
			// An unknown confidence is left out of the stored object, and a
			// known one, 0 included, is written.
			for name, obj := range map[string]any{"indicator": ind, "sighting": sighting} {
				raw, err := json.Marshal(obj)
				if err != nil {
					t.Fatal(err)
				}
				var stored map[string]any
				if err := json.Unmarshal(raw, &stored); err != nil {
					t.Fatal(err)
				}
				got, written := stored["confidence"]
				if written != (tc.want != unknown) || written && got != float64(tc.want) {
					t.Errorf("%s JSON confidence = %v (written %v); want %d (-1 is left out)", name, got, written, tc.want)
				}
			}
		})
	}
}

// TestDetectionPattern (§4.12, adr/0356): OCSF has no field for a detection's
// pattern, so it is read from unmapped.pattern. It is part of the Indicator's
// identity, as before.
func TestDetectionPattern(t *testing.T) {
	p := detectionPayload()
	p["unmapped"] = map[string]any{"pattern": `process.cmd_line = "*-enc*"`}
	res, err := testRegistry().Normalize(makeEvent(p))
	if err != nil {
		t.Fatal(err)
	}
	ind := res.Indicators[0]
	if ind.Pattern != `process.cmd_line = "*-enc*"` || ind.PatternType != "vendor" {
		t.Errorf("pattern %q type %q; want the vendor's pattern", ind.Pattern, ind.PatternType)
	}
	if ind.ID != identity.NewResolver(testNS).Indicator(ind.CreatedByRef, `process.cmd_line = "*-enc*"`, "vendor", "det-123") {
		t.Errorf("indicator id %s is not keyed on the stored pattern and pattern_type", ind.ID)
	}
}

// TestDetectionPatternFallback (§4.12, adr/0357): STIX requires a pattern.
// Without the vendor's, the pattern is the finding uid, else its title, and
// the pattern type is the tool's name. The id is minted from what is stored,
// so two findings with different titles are two indicators (adr/0357).
func TestDetectionPatternFallback(t *testing.T) {
	p := detectionPayload()
	delete(p["finding_info"].(map[string]any), "uid")
	res, err := testRegistry().Normalize(makeEvent(p))
	if err != nil {
		t.Fatal(err)
	}
	ind := res.Indicators[0]
	if ind.Pattern != "Suspicious PowerShell" || ind.PatternType != "CrowdStrike" {
		t.Errorf("pattern %q type %q; want the title and the tool's name", ind.Pattern, ind.PatternType)
	}
	if ind.ID != identity.NewResolver(testNS).Indicator(ind.CreatedByRef, "Suspicious PowerShell", "CrowdStrike", "") {
		t.Errorf("indicator id %s; want the id of the stored pattern and pattern_type", ind.ID)
	}
	other := detectionPayload()
	delete(other["finding_info"].(map[string]any), "uid")
	other["finding_info"].(map[string]any)["title"] = "Credential dumping"
	res2, err := testRegistry().Normalize(makeEvent(other))
	if err != nil {
		t.Fatal(err)
	}
	if res2.Indicators[0].ID == ind.ID {
		t.Error("two findings with no uid and different titles share one indicator id")
	}

	// The tool's name is the vendor the normalizer attributes to: with no
	// product metadata, the source tool.
	p = detectionPayload()
	delete(p, "metadata")
	res, err = testRegistry().Normalize(makeEvent(p))
	if err != nil {
		t.Fatal(err)
	}
	if ind := res.Indicators[0]; ind.Pattern != "det-123" || ind.PatternType != "fixture:s" {
		t.Errorf("pattern %q type %q; want the uid and the source tool", ind.Pattern, ind.PatternType)
	}
}

// TestDetectionTechniquesAndTypes (§4.12, adr/0357): ATT&CK technique ids come
// from finding_info.types[] and from OCSF's ATT&CK objects on the finding and
// on finding_info, de-duplicated in reading order. indicator_types keeps only
// values of STIX's indicator-type-ov, and every other type is a label.
func TestDetectionTechniquesAndTypes(t *testing.T) {
	p := detectionPayload()
	fi := p["finding_info"].(map[string]any)
	fi["types"] = []any{"T1059.001", "Malicious-Activity", "Credential Access", "anomalous-activity", "T1003", "malicious-activity", "Credential Access"}
	fi["attacks"] = []any{map[string]any{
		"technique":     map[string]any{"uid": "T1003", "name": "OS Credential Dumping"},
		"sub_technique": map[string]any{"uid": "T1003.001", "name": "LSASS Memory"},
	}}
	p["attacks"] = []any{
		map[string]any{"technique": map[string]any{"uid": "T1021"}, "tactic": map[string]any{"uid": "TA0008"}},
		map[string]any{"technique": map[string]any{"uid": "AML.T0043"}},
	}
	res, err := testRegistry().Normalize(makeEvent(p))
	if err != nil {
		t.Fatal(err)
	}
	ind := res.Indicators[0]
	// The techniques come first, under mitre-attack; the last reference is
	// the finding uid, under the vendor's name (adr/0357).
	var got []string
	refs := ind.ExternalReferences
	if n := len(refs); n == 0 || refs[n-1] != (ExternalReference{SourceName: "CrowdStrike", ExternalID: "det-123"}) {
		t.Errorf("references = %+v; want the finding uid under CrowdStrike last", refs)
	} else {
		refs = refs[:n-1]
	}
	for _, r := range refs {
		if r.SourceName != "mitre-attack" {
			t.Errorf("reference %+v; want source_name mitre-attack", r)
		}
		got = append(got, r.ExternalID)
	}
	if want := []string{"T1059.001", "T1003", "T1021", "T1003.001"}; !reflect.DeepEqual(got, want) {
		t.Errorf("techniques = %v; want %v", got, want)
	}
	if want := []string{"malicious-activity", "anomalous-activity"}; !reflect.DeepEqual(ind.IndicatorTypes, want) {
		t.Errorf("indicator_types = %v; want %v", ind.IndicatorTypes, want)
	}
	// Anything else is a label, kept once in reading order.
	if want := []string{"Credential Access"}; !reflect.DeepEqual(ind.Labels, want) {
		t.Errorf("labels = %v; want %v", ind.Labels, want)
	}
	o := ind.STIXObject()
	o.Created = ind.ValidFrom
	if obj, err := stix.Build(o); err != nil || obj["labels"] == nil || obj["external_references"] == nil {
		t.Errorf("serialized indicator = %v, %v; want labels and external_references at the top level", obj, err)
	}
}

// TestDetectionEvidenceExtractors (§4.12, adr/0356): each artifact of
// evidences[] is read with the shared extractors, whatever it carries, and
// everything it names is in the observation and the sighting.
func TestDetectionEvidenceExtractors(t *testing.T) {
	ids := identity.NewResolver(testNS)
	when := ms("2026-04-20T14:32:11Z")
	p := map[string]any{
		"class_uid":    2004,
		"time":         when,
		"finding_info": map[string]any{"uid": "d-9", "title": "Everything"},
		"device":       map[string]any{"hostname": "FINDING-HOST"},
		"evidences": []any{
			// An artifact with its own device names that host, not the finding's.
			map[string]any{
				"device": map[string]any{"hostname": "WS7", "uid": "dev-7", "uid_alt": "asset-7", "os": map[string]any{"type_id": 100}},
				"process": map[string]any{
					"pid": 10, "name": "a.exe", "cmd_line": "a.exe -x", "created_time": when,
					"file":           map[string]any{"name": "a.exe", "hashes": []any{map[string]any{"algorithm": "SHA-256", "value": "961b6dd3ede3cb8ecbaacbd68de040cd78eb2ed5889130cceb4c49268ea4d506"}}},
					"parent_process": map[string]any{"pid": 9, "name": "p.exe", "created_time": when},
				},
				"actor": map[string]any{
					"process": map[string]any{"pid": 8, "name": "actor.exe", "created_time": when},
					"user":    map[string]any{"name": "jdoe", "domain": "CONTOSO"},
				},
				"user": map[string]any{"name": "target", "domain": "CONTOSO"},
			},
			// An artifact with no device falls back to the finding's.
			map[string]any{
				"file":         map[string]any{"name": "drop.dll", "path": `C:\Temp\drop.dll`},
				"src_endpoint": map[string]any{"ip": "10.0.0.5"},
				"dst_endpoint": map[string]any{"ip": "203.0.113.9", "domain": "c2.example"},
				"query":        map[string]any{"hostname": "lookup.example"},
				"url":          map[string]any{"url_string": "https://c2.example/x"},
				"actor":        map[string]any{"process": map[string]any{"pid": 77, "created_time": when}},
			},
			// An artifact whose device names no host (an address alone) falls
			// back to the finding's too.
			map[string]any{
				"device":  map[string]any{"ip": "10.0.0.9"},
				"process": map[string]any{"pid": 78, "name": "b.exe", "created_time": when},
			},
			"not an artifact",
		},
	}
	evt := makeEvent(p)
	evt.SourceTool = "edr"
	res, err := testRegistry().Normalize(evt)
	if err != nil {
		t.Fatal(err)
	}
	ws7 := ids.Host(identity.HostIdentity{Hostname: "WS7"})
	fallback := ids.Host(identity.HostIdentity{Hostname: "FINDING-HOST"})
	proc, _ := ids.Process(ws7, 10, evt.Time)
	parent, _ := ids.Process(ws7, 9, evt.Time)
	actorProc, _ := ids.Process(ws7, 8, evt.Time)
	fallbackProc, _ := ids.Process(fallback, 77, evt.Time)
	addressOnlyProc, _ := ids.Process(fallback, 78, evt.Time)
	want := map[string]identity.STIXID{
		"artifact host":       ws7,
		"finding host":        fallback,
		"process":             proc,
		"parent":              parent,
		"actor process":       actorProc,
		"process on fallback": fallbackProc,
		"process on fallback, device with an address alone": addressOnlyProc,
		"image":      ids.File(identity.FileIdentity{Hashes: map[string]string{"SHA-256": "961b6dd3ede3cb8ecbaacbd68de040cd78eb2ed5889130cceb4c49268ea4d506"}, Name: "a.exe"}),
		"file":       ids.File(identity.FileIdentity{Name: "drop.dll", ParentDirectory: `C:\Temp`}),
		"actor.user": ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, Login: `CONTOSO\jdoe`}),
		"user":       ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, Login: `CONTOSO\target`}),
		"src ip":     ids.IPv4Addr("10.0.0.5"),
		"dst ip":     ids.IPv4Addr("203.0.113.9"),
		"dst domain": ids.DomainName("c2.example"),
		"query":      ids.DomainName("lookup.example"),
		"url":        ids.URL("https://c2.example/x"),
	}
	od := res.ObservedData[0]
	for name, id := range want {
		if !refsContain(od.ObjectRefs, id) {
			t.Errorf("object_refs lack the %s %s", name, id)
		}
	}
	if len(od.ObjectRefs) != len(want) {
		t.Errorf("object_refs = %d entities; want %d: %v", len(od.ObjectRefs), len(want), countTypes(res.SCOs))
	}
	if s := res.Sightings[0]; !reflect.DeepEqual(s.WhereSighted, []identity.STIXID{s.CreatedByRef}) || !reflect.DeepEqual(s.ObservedDataRefs, []identity.STIXID{od.ID}) {
		t.Errorf("sighting where %v, observed-data %v; want the vendor and the observation", s.WhereSighted, s.ObservedDataRefs)
	}
	// The artifact's device is a device object: its uid_alt is kept, and its
	// device id's weak host is aliased to it, outside object_refs (adr/0211).
	host, _ := scoByID(res.SCOs, ws7)
	if host.Properties["device_uid_alt"] != "asset-7" {
		t.Errorf("artifact host = %v; want its uid_alt kept", host.Properties)
	}
	weak := ids.Host(identity.HostIdentity{SourceTool: "edr", SourceUID: "dev-7"})
	if rels := relsOf(res, RelAliases); len(rels) != 1 || rels[0] != [2]identity.STIXID{weak, ws7} || refsContain(od.ObjectRefs, weak) {
		t.Errorf("aliases = %v; want the device id's weak host aliased to WS7, outside object_refs", rels)
	}
	if !hasRel(res.Relationships, RelParentProcessOf) || !hasRel(res.Relationships, RelResolvesTo) {
		t.Errorf("relationships = %v; want parent-process-of and resolves-to", res.Relationships)
	}
	for _, sco := range res.SCOs {
		if sco.Provenance.DerivationMode != DerivationDirect || sco.Provenance.Normalizer != "detection_finding" {
			t.Errorf("%s provenance = %+v; want DIRECT from detection_finding", sco.Type, sco.Provenance)
		}
	}
}

// TestDetectionEvidenceIsNotDispatched (adr/0356): an artifact is not an
// event. One that carries a class_uid is still read as an artifact, and the
// old single embedded event under "evidence" is not read at all.
func TestDetectionEvidenceIsNotDispatched(t *testing.T) {
	p := detectionPayload()
	p["evidences"].([]any)[0].(map[string]any)["class_uid"] = 3002
	res, err := testRegistry().Normalize(makeEvent(p))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ObservedData) != 1 || res.ObservedData[0].Provenance.Normalizer != "detection_finding" || countTypes(res.SCOs)[identity.TypeProcess] != 1 {
		t.Errorf("an artifact with a class_uid was dispatched: %+v", res.ObservedData)
	}

	old := detectionPayload()
	old["evidence"] = old["evidences"].([]any)[0]
	delete(old, "evidences")
	res, err = testRegistry().Normalize(makeEvent(old))
	if err != nil {
		t.Fatal(err)
	}
	// Only the finding's own device is named.
	if got := countTypes(res.SCOs); len(res.SCOs) != 1 || got[identity.TypeHost] != 1 {
		t.Errorf("the embedded-event evidence was read: %v", got)
	}
}

// TestDetectionDeterministicIds: re-ingesting the same detection dedupes the
// Indicator to the same id.
func TestDetectionDeterministicIds(t *testing.T) {
	reg := testRegistry()
	a, _ := reg.Normalize(makeEvent(detectionPayload()))
	b, _ := reg.Normalize(makeEvent(detectionPayload()))
	if a.Indicators[0].ID != b.Indicators[0].ID {
		t.Errorf("indicator id not stable across ingests: %q vs %q", a.Indicators[0].ID, b.Indicators[0].ID)
	}
}
