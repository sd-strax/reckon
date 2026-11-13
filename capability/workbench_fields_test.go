package capability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The workbench reads the objects this package writes by field name, in
// TypeScript, where the compiler cannot see a Go field go away. This test
// is the join for the snapshot the workbench's tests render from (adr/0356).

// workbenchDir is the workbench, from this package's directory.
const workbenchDir = "../workbench"

// unfilledFields are fields a struct still declares and nothing fills, by
// the object's STIX type. They are not fields the workbench can be sent.
var unfilledFields = map[string][]string{
	"sighting": {"where_sighted_refs"},
}

// jsonFields lists a struct's JSON field names.
func jsonFields(v any) map[string]bool {
	out := map[string]bool{}
	typ := reflect.TypeOf(v)
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out[name] = true
		}
	}
	return out
}

// TestWorkbenchSnapshotHoldsOnlyFieldsTheEngineSends: the workbench's tests
// render from a hand-written snapshot. Every indicator, sighting and
// observation in it holds only fields the engine's own object has and fills,
// so a workbench test cannot pass on a field the engine never sends.
func TestWorkbenchSnapshotHoldsOnlyFieldsTheEngineSends(t *testing.T) {
	sent := map[string]map[string]bool{
		"indicator":     jsonFields(Indicator{}),
		"sighting":      jsonFields(Sighting{}),
		"observed-data": jsonFields(ObservedData{}),
	}
	for typ, names := range unfilledFields {
		for _, n := range names {
			if !sent[typ][n] {
				t.Errorf("unfilledFields lists %s.%s, which is not a field of that object", typ, n)
			}
			delete(sent[typ], n)
		}
	}
	// STIX's own type name, which the stored object carries beside the fields.
	for _, fields := range sent {
		fields["type"] = true
	}
	// The finding's severity: design/03 §4.12 reads it from the event and no
	// object carries it yet, so the inspector's Severity row has no source.
	// Reported on #356; remove this line when the Indicator carries it or the
	// row goes.
	sent["indicator"]["severity"] = true

	files, err := filepath.Glob(filepath.Join(workbenchDir, "harness", "snapshots", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workbench snapshots: %v", err)
	}
	checked := 0
	for _, f := range files {
		data, err := os.ReadFile(f) // #nosec G304 -- a checked-in snapshot
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		walkEvidenceDocs(doc, func(ref, typ string, payload map[string]any) {
			fields, ok := sent[typ]
			if !ok {
				return
			}
			checked++
			var extra []string
			for k := range payload {
				if !fields[k] {
					extra = append(extra, k)
				}
			}
			sort.Strings(extra)
			for _, k := range extra {
				t.Errorf("%s: %s holds %q, which the engine does not send on a %s", filepath.Base(f), ref, k, typ)
			}
		})
	}
	if checked == 0 {
		t.Error("no indicator, sighting or observation found in the workbench snapshots")
	}
}

// walkEvidenceDocs calls fn for every evidence document in a decoded
// snapshot: an object with a ref, kind "stix", a type and a payload object,
// at any depth (a finding's related sightings and evidence included).
func walkEvidenceDocs(v any, fn func(ref, typ string, payload map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		ref, _ := x["ref"].(string)
		typ, _ := x["type"].(string)
		payload, isObject := x["payload"].(map[string]any)
		if x["kind"] == "stix" && ref != "" && typ != "" && isObject {
			fn(ref, typ, payload)
		}
		for _, child := range x {
			walkEvidenceDocs(child, fn)
		}
	case []any:
		for _, child := range x {
			walkEvidenceDocs(child, fn)
		}
	}
}
