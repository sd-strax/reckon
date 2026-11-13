package ocsfview

import (
	"fmt"
	"os"
	"testing"

	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// This file is the event view's side of the OCSF schema check (adr/0356, "A
// test fails on any class or field that OCSF doesn't define"). It works the
// way the normalizers' check in package capability does: TestMain records
// every OCSF field the templates read while the package's tests run, and
// checks the record afterwards.

// lookupGuardEnabled turns on the check that every recorded field exists in
// OCSF. It is off until the templates read OCSF's own fields, the second
// stage of #356.
const lookupGuardEnabled = false

// lookups is the record for the whole test run.
var lookups = ocsfschema.NewRecorder()

func TestMain(m *testing.M) {
	record(lookups)
	code := m.Run()
	if code == 0 {
		code = checkLookups(lookups)
	}
	os.Exit(code)
}

// record points the package's lookup hooks at rec.
func record(rec *ocsfschema.Recorder) {
	lookupHook = rec.Lookup
	renderHook = func(e Event) func() { return rec.Begin(e.ClassUID, e.Payload) }
}

// checkLookups reports what is wrong with the recorded reads and returns the
// process exit code.
//
// A read the recorder could not place fails the run. It means a template read
// a field from an object it reached without pathAny or fieldOf, so the
// field's path is unknown and cannot be checked.
//
// With the guard on, a read of a field the ceiling schema does not define for
// the event's class fails the run too. unmapped.* always passes: the schema
// types unmapped as an object with no fields of its own, which accepts any
// path below it. A class with no template is skipped: its view shows whatever
// top-level scalars the event has, and names no field.
func checkLookups(rec *ocsfschema.Recorder) int {
	var failures []string
	for _, l := range rec.Unrouted() {
		failures = append(failures, fmt.Sprintf(
			"class %d: %q was read from an object the recorder did not see; reach the object with pathAny or fieldOf", l.ClassUID, l.Path))
	}
	if lookupGuardEnabled {
		templated := map[int]bool{}
		for _, uid := range Classes() {
			templated[uid] = true
		}
		schema, err := ocsfschema.Load(ocsfschema.Ceiling)
		if err != nil {
			failures = append(failures, err.Error())
		} else {
			for _, l := range rec.Lookups() {
				if !templated[l.ClassUID] {
					continue
				}
				if err := schema.Resolve(l.ClassUID, l.Path); err != nil {
					failures = append(failures, "a template reads "+l.Path+": "+err.Error())
				}
			}
		}
	}
	if len(failures) == 0 {
		return 0
	}
	fmt.Fprintln(os.Stderr, "--- FAIL: OCSF field lookups (adr/0356)")
	for _, f := range failures {
		fmt.Fprintln(os.Stderr, "    "+f)
	}
	fmt.Fprintln(os.Stderr, "FAIL")
	return 1
}

// The hooks reach the recorder: a read from the top of the event, a read from
// an entry of a list, and a read from an object inside that entry are each
// recorded under the event's class with the full path.
func TestLookupsAreRecorded(t *testing.T) {
	rec := ocsfschema.NewRecorder()
	record(rec)
	t.Cleanup(func() { record(lookups) })

	Render(Event{ClassUID: classCloudInventory, Payload: map[string]any{
		"class_uid": 5023,
		"cloud":     map[string]any{"provider": "AWS", "region": "us-east-1", "account": map[string]any{"uid": "111122223333"}},
		"resources": []any{map[string]any{
			"uid": "arn:aws:s3:::bucket", "name": "bucket", "type": "AWS::S3::Bucket",
			"owner": map[string]any{"name": "alice"},
			"group": map[string]any{"name": "rg"},
		}},
	}}, nil)

	got := map[ocsfschema.Lookup]bool{}
	for _, l := range rec.Lookups() {
		got[l] = true
	}
	for _, want := range []ocsfschema.Lookup{
		{ClassUID: 5023, Path: "category_uid"},
		{ClassUID: 5023, Path: "cloud.region"},
		{ClassUID: 5023, Path: "resources"},
		{ClassUID: 5023, Path: "resources[].uid"},
		{ClassUID: 5023, Path: "resources[].zone"},
		{ClassUID: 5023, Path: "resources[].group.name"},
		{ClassUID: 5023, Path: "resources[].owner.name"},
		{ClassUID: 5023, Path: "resources[].data"},
	} {
		if !got[want] {
			t.Errorf("not recorded: class %d %s", want.ClassUID, want.Path)
		}
	}
	if un := rec.Unrouted(); len(un) != 0 {
		t.Errorf("reads the recorder could not place: %v", un)
	}

	// A read outside Render is not an event read.
	before := len(rec.Lookups())
	Category(4001, map[string]any{"category_uid": 4})
	if after := len(rec.Lookups()); after != before {
		t.Errorf("a read outside Render was recorded")
	}
}
