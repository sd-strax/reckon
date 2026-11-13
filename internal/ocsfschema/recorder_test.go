package ocsfschema

import (
	"reflect"
	"testing"
)

// get reads path from obj the way a package's lookup helper would: one key,
// with the read reported to the recorder.
func get(r *Recorder, obj map[string]any, key string) any {
	v := obj[key]
	r.Lookup(obj, key, v)
	return v
}

func TestRecorderBuildsFullPaths(t *testing.T) {
	payload := map[string]any{
		"device": map[string]any{"hostname": "h1", "os": map[string]any{"name": "Windows"}},
		"resources": []any{
			map[string]any{"uid": "r1", "group": map[string]any{"name": "g"}},
			map[string]any{"uid": "r2"},
		},
		"email": map[string]any{"to": []any{"a@example.com"}},
	}
	r := NewRecorder()
	end := r.Begin(6003, payload)

	r.Lookup(payload, "device.hostname", "h1") // a dotted read from the top
	device := get(r, payload, "device").(map[string]any)
	os := get(r, device, "os").(map[string]any)
	get(r, os, "name")
	for _, item := range get(r, payload, "resources").([]any) {
		res := item.(map[string]any)
		get(r, res, "uid")
		get(r, res, "zone") // absent, still a read
		if g, ok := get(r, res, "group").(map[string]any); ok {
			get(r, g, "name")
		}
	}
	r.Lookup(payload, "email.to", payload["email"].(map[string]any)["to"])
	end()

	want := []Lookup{
		{6003, "device"},
		{6003, "device.hostname"},
		{6003, "device.os"},
		{6003, "device.os.name"},
		{6003, "email.to"},
		{6003, "resources"},
		{6003, "resources[].group"},
		{6003, "resources[].group.name"},
		{6003, "resources[].uid"},
		{6003, "resources[].zone"},
	}
	if got := r.Lookups(); !reflect.DeepEqual(got, want) {
		t.Errorf("Lookups() =\n %v\nwant\n %v", got, want)
	}
	if got := r.Unrouted(); len(got) != 0 {
		t.Errorf("Unrouted() = %v, want none", got)
	}
}

// An event handled inside another (a detection's evidence, before adr/0356)
// records under its own class, and the outer event resumes after it.
func TestRecorderNestsEvents(t *testing.T) {
	inner := map[string]any{"class_uid": 1007, "process": map[string]any{"pid": 4}}
	outer := map[string]any{"evidence": inner, "severity": "High"}
	r := NewRecorder()

	endOuter := r.Begin(2004, outer)
	ev := get(r, outer, "evidence").(map[string]any)
	get(r, ev, "class_uid")
	endInner := r.Begin(1007, ev)
	r.Lookup(ev, "process.pid", 4)
	endInner()
	get(r, outer, "severity")
	endOuter()

	want := []Lookup{
		{1007, "process.pid"},
		{2004, "evidence"},
		{2004, "evidence.class_uid"},
		{2004, "severity"},
	}
	if got := r.Lookups(); !reflect.DeepEqual(got, want) {
		t.Errorf("Lookups() = %v, want %v", got, want)
	}
}

func TestRecorderUnroutedAndIgnored(t *testing.T) {
	sub := map[string]any{"name": "x"}
	payload := map[string]any{"user": sub}
	r := NewRecorder()

	// Outside any event: ignored.
	r.Lookup(payload, "user.name", "x")

	end := r.Begin(3001, payload)
	// sub was reached by indexing the payload directly, not through a lookup.
	r.Lookup(sub, "name", "x")
	// An empty or nil object has no field to check.
	r.Lookup(map[string]any{}, "name", nil)
	r.Lookup(nil, "name", nil)
	end()

	// After the event ended: ignored again.
	r.Lookup(payload, "user", sub)

	if got := r.Lookups(); len(got) != 0 {
		t.Errorf("Lookups() = %v, want none", got)
	}
	if got, want := r.Unrouted(), []Lookup{{3001, "name"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Unrouted() = %v, want %v", got, want)
	}
}

// Every path the recorder builds is in the syntax Resolve reads.
func TestRecorderPathsResolve(t *testing.T) {
	payload := map[string]any{"resources": []any{map[string]any{"owner": map[string]any{"uid": "u"}}}}
	r := NewRecorder()
	end := r.Begin(5023, payload)
	res := get(r, payload, "resources").([]any)[0].(map[string]any)
	owner := get(r, res, "owner").(map[string]any)
	get(r, owner, "uid")
	end()

	s := mustLoad(t, "1.7.0")
	for _, l := range r.Lookups() {
		if err := s.Resolve(l.ClassUID, l.Path); err != nil {
			t.Errorf("%v: %v", l, err)
		}
	}
}
