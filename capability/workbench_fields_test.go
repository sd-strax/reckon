package capability

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The workbench reads the objects this package writes by field name, in
// TypeScript, where the compiler cannot see a Go field go away. These tests
// are the join (adr/0356).

// workbenchDir is the workbench, from this package's directory.
const workbenchDir = "../workbench"

// retiredFields are JSON field names this package used to fill and no longer
// does. The workbench must not read any of them. A field that is removed, or
// kept in a struct and no longer filled, is added here in the same change.
var retiredFields = map[string]string{
	"where_sighted_refs": "a Sighting names its entities through observed_data_refs, the observation's object_refs (adr/0356)",
	"privilege":          "user access and group management write the list privileges (adr/0356)",
}

// unfilledFields are fields a struct still declares and nothing fills, by
// the object's STIX type. They are not fields the workbench can be sent.
var unfilledFields = map[string][]string{
	"sighting": {"where_sighted_refs"},
}

// TestWorkbenchReadsNoRetiredField: no TypeScript file of the workbench,
// tests and recorded snapshots included, names a field this package no
// longer fills.
func TestWorkbenchReadsNoRetiredField(t *testing.T) {
	patterns := map[string]*regexp.Regexp{}
	for name := range retiredFields {
		patterns[name] = regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	}
	scanned := 0
	for _, root := range []string{"src", "webview/src", "harness"} {
		err := filepath.WalkDir(filepath.Join(workbenchDir, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// The wire fixtures are written by the engine's own tests
				// from its structs, every field filled to show the shape, so
				// they are not a reader.
				if d.Name() == "node_modules" || d.Name() == "dist" || filepath.ToSlash(path) == workbenchDir+"/src/wire/fixtures" {
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".ts", ".tsx", ".json":
			default:
				return nil
			}
			data, err := os.ReadFile(path) // #nosec G304 G122 -- a walk of the repository's own workbench sources
			if err != nil {
				return err
			}
			scanned++
			for i, line := range strings.Split(string(data), "\n") {
				for name, re := range patterns {
					if !re.MatchString(line) {
						continue
					}
					// A test that proves the field is ignored may name it.
					if isTest(path) && ignoredOnPurpose(line, name) {
						continue
					}
					t.Errorf("%s:%d reads %q, which the engine no longer fills: %s", path, i+1, name, retiredFields[name])
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if scanned < 50 {
		t.Fatalf("scanned only %d workbench files; is %s the workbench?", scanned, workbenchDir)
	}
}

func isTest(path string) bool {
	return strings.HasSuffix(path, ".test.ts") || strings.HasSuffix(path, ".test.tsx")
}

// ignoredOnPurpose reports whether a test line names a retired field to show
// it is ignored: a comment, or a line marked "retired: <name>".
func ignoredOnPurpose(line, name string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "//") || strings.Contains(line, "retired: "+name)
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
