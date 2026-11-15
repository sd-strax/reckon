package stix_test

import (
	"encoding/json"
	"testing"

	"github.com/sd-strax/reckon/internal/stixschema"
	"github.com/sd-strax/reckon/stix"
)

// TestDefinitionsAreValidSTIX checks every extension definition against the
// OASIS extension-definition schema, and the publisher identity against the
// identity schema (adr/0357 rule 8).
func TestDefinitionsAreValidSTIX(t *testing.T) {
	var report stixschema.Report
	for _, d := range stix.Definitions() {
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		report.Add("definition "+d.Name, raw)
	}
	raw, err := json.Marshal(stix.Publisher())
	if err != nil {
		t.Fatal(err)
	}
	report.Add("publisher", raw)
	if report.Failed() {
		t.Errorf("objects that are not valid STIX 2.1:\n%s", report.String())
	}
	if got := report.Counts(); got["extension-definition"] != len(stix.Definitions()) || got["identity"] != 1 {
		t.Errorf("checked %v", got)
	}
}
