package evidence

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sd-strax/reckon/internal/stixschema"
	"github.com/sd-strax/reckon/stix"
)

// TestAssertedEdgePayload: an asserted edge is a STIX relationship that keeps
// its assertion in reckon-assertion. withdrawn_at is left out while the
// assertion stands, and written once it is withdrawn (adr/0357).
func TestAssertedEdgePayload(t *testing.T) {
	at := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	e := assertedEdge{
		ID: "relationship--00000000-0000-4000-8000-000000000001", Type: relAliases,
		SourceRef: "reckon-host--00000000-0000-4000-8000-000000000002", TargetRef: "reckon-host--00000000-0000-4000-8000-000000000003",
		Created: at,
		Assertion: Assertion{AssertedBy: "u-1", AssertedByName: "Ana Analyst", AssertedAt: at, Note: "checked",
			History: []AssertionEvent{{Act: actAsserted, By: "u-1", At: at}}},
	}
	read := func(raw []byte) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	raw, err := e.payload(at)
	if err != nil {
		t.Fatal(err)
	}
	m := read(raw)
	if m["type"] != "relationship" || m["relationship_type"] != relAliases || m["source_ref"] != e.SourceRef || m["created"] != "2026-01-03T00:00:00.000Z" {
		t.Errorf("relationship = %v", m)
	}
	for _, k := range []string{"asserted_by", "withdrawn_at", "history", "note"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s is a top-level property", k)
		}
	}
	ext := stix.ExtensionFrom(m, stix.ExtAssertion)
	if ext["asserted_by"] != "u-1" || ext["note"] != "checked" {
		t.Errorf("reckon-assertion = %v", ext)
	}
	if _, ok := ext["withdrawn_at"]; ok {
		t.Errorf("a standing assertion has withdrawn_at: %v", ext)
	}
	back, err := decodeAssertedEdge(raw)
	if err != nil || back.AssertedBy != "u-1" || back.WithdrawnAt != nil || !back.Created.Equal(at) || back.SourceRef != e.SourceRef || len(back.History) != 1 {
		t.Errorf("decoded = %+v, %v", back, err)
	}

	w := at.Add(time.Hour)
	e.WithdrawnAt = &w
	raw, err = e.payload(w)
	if err != nil {
		t.Fatal(err)
	}
	m = read(raw)
	if got := stix.AssertionWithdrawnAt.String(m); got != "2026-01-03T01:00:00Z" {
		t.Errorf("withdrawn_at = %q", got)
	}
	if m["created"] != "2026-01-03T00:00:00.000Z" || m["modified"] != "2026-01-03T01:00:00.000Z" {
		t.Errorf("created %v, modified %v", m["created"], m["modified"])
	}
	if back, err := decodeAssertedEdge(raw); err != nil || back.WithdrawnAt == nil || !back.WithdrawnAt.Equal(w) {
		t.Errorf("decoded withdrawal = %+v, %v", back.WithdrawnAt, err)
	}
}

// TestAssertedEdgeIsValidSTIX checks an asserted relationship, standing and
// withdrawn, against the STIX 2.1 and reckon schemas (adr/0357 rule 8), for
// each relationship type a person can assert.
func TestAssertedEdgeIsValidSTIX(t *testing.T) {
	at := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	w := at.Add(time.Hour)
	var report stixschema.Report
	for _, typ := range []string{relAliases, relDistinctFrom, relBelongsTo} {
		e := assertedEdge{
			ID: "relationship--00000000-0000-4000-8000-000000000001", Type: typ,
			SourceRef: "user-account--00000000-0000-4000-8000-000000000002", TargetRef: "identity--00000000-0000-4000-8000-000000000003",
			Created: at,
			Assertion: Assertion{AssertedBy: "u-1", AssertedByName: "Ana Analyst", AssertedAt: at, Note: "checked",
				History: []AssertionEvent{{Act: actAsserted, By: "u-1", ByName: "Ana Analyst", At: at, Note: "checked"}}},
		}
		raw, err := e.payload(at)
		if err != nil {
			t.Fatal(err)
		}
		report.Add(typ+", standing", raw)

		e.WithdrawnAt = &w
		e.History = append(e.History, AssertionEvent{Act: actWithdrawn, By: "u-2", At: w})
		raw, err = e.payload(w)
		if err != nil {
			t.Fatal(err)
		}
		report.Add(typ+", withdrawn", raw)
	}
	if report.Failed() {
		t.Errorf("asserted relationships that are not valid STIX 2.1:\n%s", report.String())
	}
}
