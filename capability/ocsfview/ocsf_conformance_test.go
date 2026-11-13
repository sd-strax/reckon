package ocsfview

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// This file is the event view's side of the OCSF schema check (adr/0356, "A
// test fails on any class or field that OCSF doesn't define"). It works the
// way the normalizers' check in package capability does: TestMain records
// every OCSF field the templates read while the package's tests run, and
// checks the record afterwards.

// lookupGuardEnabled turns on the check that every recorded field exists in
// OCSF. It is on: the templates read OCSF's own fields (adr/0356).
const lookupGuardEnabled = true

// lookups is the record for the whole test run.
var lookups = ocsfschema.NewRecorder()

func TestMain(m *testing.M) {
	record(lookups)
	viewHook = checkNamedFields
	code := m.Run()
	if code == 0 {
		code = checkLookups(lookups)
	}
	if code == 0 {
		code = reportNamedFields()
	}
	os.Exit(code)
}

// namedFieldFailures collects, over the whole test run, each view that names
// a field of its event wrongly (see checkNamedFields).
var (
	namedFieldMu       sync.Mutex
	namedFieldFailures []string
)

// checkNamedFields is the rule for a field path a view names: the path must
// lead, in the view's own event, to the value the view says it read there.
// It runs on every view any test renders, so a template that names a field
// is checked by whichever test exercises it.
//
// Today the one such path is Decoded.Field. It is resolved the way a reader
// of the view would, with capability.LookupPath (through ResolveField for an
// entry of a list), and must give Decoded.Stored.
func checkNamedFields(e Event, v View) {
	if msg := namedFieldProblem(e, v); msg != "" {
		namedFieldMu.Lock()
		namedFieldFailures = append(namedFieldFailures, msg)
		namedFieldMu.Unlock()
	}
}

func namedFieldProblem(e Event, v View) string {
	if v.Decoded == nil {
		return ""
	}
	got, ok := ResolveField(e.Payload, v.Decoded.Field, capability.LookupPath)
	switch {
	case !ok:
		return fmt.Sprintf("class %d: the view names %q as the decoded field, and the event has no such field", e.ClassUID, v.Decoded.Field)
	case got != any(v.Decoded.Stored) || v.Decoded.Stored == "":
		return fmt.Sprintf("class %d: the view names %q as the decoded field and %q as its stored value, and the event holds %v there", e.ClassUID, v.Decoded.Field, v.Decoded.Stored, got)
	}
	return ""
}

func reportNamedFields() int {
	namedFieldMu.Lock()
	defer namedFieldMu.Unlock()
	if len(namedFieldFailures) == 0 {
		return 0
	}
	fmt.Fprintln(os.Stderr, "--- FAIL: fields a view names (adr/0356)")
	for _, f := range namedFieldFailures {
		fmt.Fprintln(os.Stderr, "    "+f)
	}
	fmt.Fprintln(os.Stderr, "FAIL")
	return 1
}

// The rule catches a view that names a field the event does not have, or a
// stored value the field does not hold.
func TestNamedFieldRule(t *testing.T) {
	e := Event{ClassUID: classScheduledJob, Payload: payload(`{"job":{"cmd_line":"old.exe","job_actions":[{"desc":"no command"},{"cmd_line":"pwsh -enc ZQBjAGgAbwA="}]}}`)}
	good := View{Decoded: &Decoded{Field: "job.job_actions[1].cmd_line", Stored: "pwsh -enc ZQBjAGgAbwA="}}
	if msg := namedFieldProblem(e, good); msg != "" {
		t.Errorf("a correct view: %s", msg)
	}
	for name, d := range map[string]Decoded{
		"the first action, which has no command": {Field: "job.job_actions[0].cmd_line", Stored: "pwsh -enc ZQBjAGgAbwA="},
		"the fallback field, not the one read":   {Field: "job.cmd_line", Stored: "pwsh -enc ZQBjAGgAbwA="},
		"past the end of the list":               {Field: "job.job_actions[2].cmd_line", Stored: "pwsh -enc ZQBjAGgAbwA="},
		"a field the event lacks":                {Field: "process.cmd_line", Stored: "pwsh -enc ZQBjAGgAbwA="},
		"no stored value":                        {Field: "job.job_actions[1].cmd_line"},
	} {
		if msg := namedFieldProblem(e, View{Decoded: &d}); msg == "" {
			t.Errorf("%s: the rule passed %+v", name, d)
		}
	}
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
