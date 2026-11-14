package capability

import (
	"reflect"
	"testing"

	"github.com/sd-strax/reckon/identity"
)

func casePayload() map[string]any {
	return map[string]any{
		"class_uid":      2005,
		"time":           ms("2026-04-20T14:32:11Z"),
		"status":         "In Progress",
		"priority":       "2 - High",
		"assignee_group": map[string]any{"name": "SOC-Tier3", "uid": "grp-77"},
		"finding_info_list": []any{map[string]any{
			"uid":   "INC0010042",
			"title": "Suspicious RDP from WIN-FILE01",
			"desc":  "Analyst flagged lateral movement via RDP.",
		}},
		"src_url":  "https://acme.service-now.com/nav_to.do?uri=incident.do?sys_id=abc",
		"unmapped": map[string]any{"sor": "servicenow"},
	}
}

// TestIncidentNormalizer covers §2.9: an incident_finding (2005) normalizes to
// a single ObservedData carrying case metadata in Extensions, with no SCOs and
// a DIRECT derivation (a plain observation, not the INFERRED Indicator path).
func TestIncidentNormalizer(t *testing.T) {
	res, err := testRegistry().Normalize(makeEvent(casePayload()))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	if len(res.SCOs) != 0 {
		t.Errorf("incident normalizer produced %d SCOs; want 0 (no entity extraction in v0)", len(res.SCOs))
	}
	if len(res.ObservedData) != 1 {
		t.Fatalf("got %d ObservedData; want 1", len(res.ObservedData))
	}
	od := res.ObservedData[0]
	if len(od.ObjectRefs) != 0 {
		t.Errorf("ObservedData.object_refs = %d; want 0", len(od.ObjectRefs))
	}
	if od.Provenance.DerivationMode != DerivationDirect || od.Provenance.Normalizer != "incident_finding" {
		t.Errorf("provenance = %+v; want DIRECT from incident_finding", od.Provenance)
	}

	want := map[string]any{
		"case_number":      "INC0010042",
		"title":            "Suspicious RDP from WIN-FILE01",
		"status":           "In Progress",
		"priority":         "2 - High",
		"summary":          "Analyst flagged lateral movement via RDP.",
		"assignment_group": "SOC-Tier3",
		"link":             "https://acme.service-now.com/nav_to.do?uri=incident.do?sys_id=abc",
		"sor":              "servicenow",
	}
	if !reflect.DeepEqual(od.Extensions, want) {
		t.Errorf("Extensions = %v; want %v", od.Extensions, want)
	}
}

// TestIncidentFields (§2.9, adr/0356): the first finding names the case and
// every finding's uid is kept; the group's uid stands in for a missing name;
// a numeric priority (OCSF 1.1.0) is kept; the pre-OCSF fields are not read.
func TestIncidentFields(t *testing.T) {
	normalize := func(edit func(p map[string]any)) map[string]any {
		t.Helper()
		p := casePayload()
		edit(p)
		res, err := testRegistry().Normalize(makeEvent(p))
		if err != nil {
			t.Fatal(err)
		}
		return res.ObservedData[0].Extensions
	}

	ext := normalize(func(p map[string]any) {
		p["finding_info_list"] = append(p["finding_info_list"].([]any),
			map[string]any{"uid": "det-1", "title": "A detection"}, map[string]any{"title": "no uid"}, "not a finding")
	})
	if ext["case_number"] != "INC0010042" || ext["title"] != "Suspicious RDP from WIN-FILE01" {
		t.Errorf("extensions = %v; want the first finding to name the case", ext)
	}
	if got, _ := ext["finding_uids"].([]string); !reflect.DeepEqual(got, []string{"INC0010042", "det-1"}) {
		t.Errorf("finding_uids = %v; want every finding's uid", ext["finding_uids"])
	}
	if ext := normalize(func(map[string]any) {}); ext["finding_uids"] != nil {
		t.Errorf("finding_uids = %v for one finding; want none", ext["finding_uids"])
	}

	ext = normalize(func(p map[string]any) { p["assignee_group"] = map[string]any{"uid": "grp-77"} })
	if ext["assignment_group"] != "grp-77" {
		t.Errorf("assignment_group = %v; want the group's uid", ext["assignment_group"])
	}
	ext = normalize(func(p map[string]any) { p["priority"] = 2 })
	if ext["priority"] != "2" {
		t.Errorf("priority = %v; want the number kept", ext["priority"])
	}

	// The shape reckon read before adr/0356 is not OCSF and is not read.
	ext = normalize(func(p map[string]any) {
		p["finding_info"] = p["finding_info_list"].([]any)[0]
		p["assignment_group"] = "SOC-Tier3"
		p["unmapped"] = map[string]any{"link": "https://x", "sor": "servicenow"}
		delete(p, "finding_info_list")
		delete(p, "assignee_group")
		delete(p, "src_url")
	})
	want := map[string]any{"status": "In Progress", "priority": "2 - High", "sor": "servicenow"}
	if !reflect.DeepEqual(ext, want) {
		t.Errorf("extensions = %v; want %v", ext, want)
	}

	// An event with none of the fields carries no extensions.
	res, err := testRegistry().Normalize(makeEvent(map[string]any{"class_uid": 2005, "time": ms("2026-04-20T14:32:11Z")}))
	if err != nil {
		t.Fatal(err)
	}
	if res.ObservedData[0].Extensions != nil {
		t.Errorf("extensions = %v; want none", res.ObservedData[0].Extensions)
	}
}

// TestIncidentNormalizerRegistered: class 2005 routes to the incident
// normalizer, not the opaque default.
func TestIncidentNormalizerRegistered(t *testing.T) {
	reg := NewRegistry(identity.NewResolver(testNS))
	var found bool
	for _, c := range reg.HandledClasses() {
		if c == 2005 {
			found = true
		}
	}
	if !found {
		t.Errorf("HandledClasses() = %v; want it to contain 2005", reg.HandledClasses())
	}
}
