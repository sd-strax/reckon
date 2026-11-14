package ocsfview

import (
	"sort"
	"testing"

	"github.com/sd-strax/reckon/internal/ocsfschema"
)

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Every OCSF enum a template holds in a table agrees with the schema checked
// in (adr/0356): a table by number in every release, and each activity name a
// verb table is keyed by is an activity OCSF gives that class.
func TestEnumTablesMatchSchema(t *testing.T) {
	for _, problem := range ocsfschema.CheckCaptions(classNetwork, "activity_id", networkActivities) {
		t.Errorf("networkActivities: %s", problem)
	}

	byName := []struct {
		name    string
		classes []int
		names   []string
	}{
		{"processVerbs", []int{classProcess}, keysOf(processVerbs)},
		{"networkVerbs", []int{classNetwork}, keysOf(networkVerbs)},
		{"fileVerbs", []int{classFileActivity}, keysOf(fileVerbs)},
		{"registryVerbs", []int{classRegistryKey, classRegistryValue}, keysOf(registryVerbs)},
		{"jobVerbs", []int{classScheduledJob}, keysOf(jobVerbs)},
		{"accountVerbs", []int{classAccountChange, classUserAccess, classGroupManagement}, keysOf(accountVerbs)},
		{"groupLeads", []int{classGroupManagement}, keysOf(groupLeads)},
		{"emailURLVerbs", []int{classEmailURL}, keysOf(emailURLVerbs)},
		{"emailFileVerbs", []int{classEmailFile}, keysOf(emailFileVerbs)},
	}
	for _, tt := range byName {
		for _, problem := range ocsfschema.CheckCaptionNames(tt.classes, "activity_id", tt.names) {
			t.Errorf("%s: %s", tt.name, problem)
		}
	}
}

// Each logonWords key is an OCSF logon type, written as logonKey writes it.
func TestLogonWordsAreOCSFLogonTypes(t *testing.T) {
	versions, err := ocsfschema.Versions()
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, v := range versions {
		schema, err := ocsfschema.Load(v)
		if err != nil {
			t.Fatal(err)
		}
		enum, ok := schema.Enum(classAuthentication, "logon_type_id")
		if !ok {
			t.Fatalf("OCSF %s: logon_type_id is not an enum", v)
		}
		for _, caption := range enum {
			known[logonKey(caption)] = true
		}
	}
	for _, key := range keysOf(logonWords) {
		if !known[key] {
			t.Errorf("logonWords has %q, which is no OCSF logon type", key)
		}
	}
}

// A logon type reads the same in OCSF's caption and without its spaces.
func TestLogonKey(t *testing.T) {
	for _, in := range []string{"Remote Interactive", "RemoteInteractive", "remote interactive"} {
		if got := logonWords[logonKey(in)]; got != "RDP" {
			t.Errorf("logonWords[logonKey(%q)] = %q; want RDP", in, got)
		}
	}
	if got := logonWords[logonKey("OS Service")]; got != "service" {
		t.Errorf("OS Service reads %q; want service", got)
	}
}
