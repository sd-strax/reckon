package capability

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// This file is the normalizers' side of the OCSF schema check (adr/0356, "A
// test fails on any class or field that OCSF doesn't define").
//
// TestMain records every OCSF field the normalizers read while the package's
// tests run. After the tests it checks the record. A read is only recorded
// when a test runs the code that makes it, so each normalizer's tests must
// cover its fallbacks.

// lookupGuardEnabled turns on the check that every recorded field exists in
// OCSF. It is on: the normalizers read OCSF's own fields (adr/0356).
const lookupGuardEnabled = true

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
	normalizeHook = func(evt OcsfEvent) func() { return rec.Begin(evt.ClassUID, evt.Payload) }
}

// checkLookups reports what is wrong with the recorded reads and returns the
// process exit code.
//
// A read the recorder could not place fails the run. It means a normalizer
// read a field from an object it reached without lookupPath or fieldOf, so
// the field's path is unknown and cannot be checked.
//
// With the guard on, a read of a field the ceiling schema does not define for
// the event's class fails the run too. unmapped.* always passes: the schema
// types unmapped as an object with no fields of its own, which accepts any
// path below it.
func checkLookups(rec *ocsfschema.Recorder) int {
	var failures []string
	for _, l := range rec.Unrouted() {
		failures = append(failures, fmt.Sprintf(
			"class %d: %q was read from an object the recorder did not see; reach the object with lookupPath or fieldOf", l.ClassUID, l.Path))
	}
	if lookupGuardEnabled {
		schema, err := ocsfschema.Load(ocsfschema.Ceiling)
		if err != nil {
			failures = append(failures, err.Error())
		} else {
			for _, l := range rec.Lookups() {
				if err := schema.Resolve(l.ClassUID, l.Path); err != nil {
					failures = append(failures, "a normalizer reads "+l.Path+": "+err.Error())
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

// useRecorder points the hooks at a fresh recorder for one test and puts the
// run's recorder back afterwards.
func useRecorder(t *testing.T) *ocsfschema.Recorder {
	t.Helper()
	rec := ocsfschema.NewRecorder()
	record(rec)
	t.Cleanup(func() { record(lookups) })
	return rec
}

// The hooks reach the recorder: a read from the top of the event, a read from
// an entry of a list, and a read from an object inside that entry are each
// recorded under the event's class with the full path.
func TestLookupsAreRecorded(t *testing.T) {
	rec := useRecorder(t)
	reg := testRegistry()

	if _, err := reg.Normalize(makeEvent(map[string]any{
		"class_uid": 1007, "time": "2026-05-06T03:14:00Z",
		"device": map[string]any{"hostname": "WIN-FILE01", "agent_list": []any{map[string]any{"uid": "a1"}}},
		"process": map[string]any{
			"pid":  4242,
			"file": map[string]any{"name": "x.exe", "hashes": []any{map[string]any{"algorithm": "SHA-256", "value": "ab"}}},
		},
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Normalize(makeEvent(map[string]any{
		"class_uid": 6003, "time": "2026-05-06T03:14:00Z",
		"cloud": map[string]any{"provider": "AWS", "account": map[string]any{"uid": "111122223333"}},
		"actor": map[string]any{"user": map[string]any{
			"uid":                      "AIDAEXAMPLE",
			"programmatic_credentials": []any{map[string]any{"uid": "AKIAEXAMPLE"}},
		}},
		"resources": []any{map[string]any{"name": "bucket", "type": "AWS::S3::Bucket", "group": map[string]any{"name": "rg"}}},
	})); err != nil {
		t.Fatal(err)
	}

	got := map[ocsfschema.Lookup]bool{}
	for _, l := range rec.Lookups() {
		got[l] = true
	}
	for _, want := range []ocsfschema.Lookup{
		{ClassUID: 1007, Path: "process.pid"},
		{ClassUID: 1007, Path: "device.hostname"},
		{ClassUID: 1007, Path: "device.agent_list"},
		{ClassUID: 1007, Path: "device.agent_list[].uid"},
		{ClassUID: 1007, Path: "process.file.hashes"},
		{ClassUID: 1007, Path: "process.file.hashes[].algorithm"},
		{ClassUID: 1007, Path: "actor.user"},
		{ClassUID: 6003, Path: "cloud.provider"},
		{ClassUID: 6003, Path: "actor.user.type_id"},
		{ClassUID: 6003, Path: "actor.user.programmatic_credentials[].uid"},
		{ClassUID: 6003, Path: "resources"},
		{ClassUID: 6003, Path: "resources[].zone"},
		{ClassUID: 6003, Path: "resources[].group.name"},
	} {
		if !got[want] {
			t.Errorf("not recorded: class %d %s", want.ClassUID, want.Path)
		}
	}
	for l := range got {
		if l.ClassUID != 1007 && l.ClassUID != 6003 {
			t.Errorf("recorded under class %d: %s", l.ClassUID, l.Path)
		}
	}
	if un := rec.Unrouted(); len(un) != 0 {
		t.Errorf("reads the recorder could not place: %v", un)
	}
}

// A read outside Registry.Normalize is not an event read: the fixture matcher
// and the parameter templates use lookupPath on their own maps.
func TestLookupsOutsideNormalizeAreNotRecorded(t *testing.T) {
	rec := useRecorder(t)
	if _, ok := LookupPath(map[string]any{"target": map[string]any{"hostname": "h"}}, "target.hostname"); !ok {
		t.Fatal("LookupPath did not find the value")
	}
	if got := rec.Lookups(); len(got) != 0 {
		t.Errorf("recorded %v, want nothing", got)
	}
}

// fieldOf is obj[key], on a nil object too.
func TestFieldOf(t *testing.T) {
	obj := map[string]any{"a": 1, "b.c": 2}
	for key, want := range map[string]any{"a": 1, "b.c": 2, "missing": nil} {
		if got := fieldOf(obj, key); !reflect.DeepEqual(got, want) {
			t.Errorf("fieldOf(%q) = %v, want %v", key, got, want)
		}
	}
	if got := fieldOf(nil, "a"); got != nil {
		t.Errorf("fieldOf on a nil object = %v, want nil", got)
	}
}

// Check 1 of adr/0356: every registered class exists in the ceiling schema,
// under the OCSF name its normalizer records in provenance.
func TestRegisteredClassesAreOCSF(t *testing.T) {
	schema, err := ocsfschema.Load(ocsfschema.Ceiling)
	if err != nil {
		t.Fatal(err)
	}
	reg := testRegistry()
	for _, uid := range reg.HandledClasses() {
		class, ok := schema.Class(uid)
		if !ok {
			t.Errorf("class %d is registered, and OCSF %s has no such class", uid, schema.Version())
			continue
		}
		res, err := reg.Normalize(makeEvent(map[string]any{"class_uid": uid, "time": "2026-05-06T03:14:00Z"}))
		if err != nil || len(res.ObservedData) == 0 {
			t.Errorf("class %d: an empty event did not normalize: %v", uid, err)
			continue
		}
		if got := res.ObservedData[0].Provenance.Normalizer; got != class.Name {
			t.Errorf("class %d is OCSF's %s, and its normalizer calls itself %s", uid, class.Name, got)
		}
	}
}
