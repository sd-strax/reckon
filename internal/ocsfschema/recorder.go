package ocsfschema

import (
	"reflect"
	"sort"
	"sync"
)

// Lookup is one field read: the class of the event being read, and the full
// path of the field from the top of the event, in Resolve's syntax.
type Lookup struct {
	ClassUID int
	Path     string
}

// Recorder records which OCSF fields code reads while it handles events
// (adr/0356). A package that reads events calls Begin when it starts on an
// event and Lookup on every field read, from hooks that only its tests set.
//
// Code often reads a field from an object inside the event, such as the
// "name" of one entry of "resources". The Recorder works out the full path
// ("resources[].name") by itself. It remembers every object and list a lookup
// returned and the path it was found at, so a later read from that object is
// recorded under that path. This only works when the object was itself
// reached through a recorded lookup. A read from any other object is kept in
// Unrouted, so a test can fail on it.
//
// A Recorder is safe for concurrent use, but events must not be handled
// concurrently: a lookup belongs to the event whose Begin was called last.
type Recorder struct {
	mu       sync.Mutex
	frames   []*frame
	seen     map[Lookup]bool
	unrouted map[Lookup]bool
}

// frame is one event being handled.
type frame struct {
	classUID int
	paths    map[uintptr]string // an object in the event -> the path it sits at
}

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder {
	return &Recorder{seen: map[Lookup]bool{}, unrouted: map[Lookup]bool{}}
}

// Begin starts recording lookups against one event: its class and its payload.
// The returned function ends it. Events nest: after the inner event ends,
// lookups belong to the outer one again.
func (r *Recorder) Begin(classUID int, payload map[string]any) (end func()) {
	f := &frame{classUID: classUID, paths: map[uintptr]string{}}
	if payload != nil {
		f.paths[mapID(payload)] = ""
	}
	r.mu.Lock()
	r.frames = append(r.frames, f)
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		for i := len(r.frames) - 1; i >= 0; i-- {
			if r.frames[i] == f {
				r.frames = append(r.frames[:i], r.frames[i+1:]...)
				return
			}
		}
	}
}

// Lookup records one field read: obj is the object read from (the event
// payload or an object inside it), path is the dotted path read within obj,
// and found is the value read (nil when absent). A lookup outside any event
// is ignored.
func (r *Recorder) Lookup(obj map[string]any, path string, found any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.frames) == 0 {
		return
	}
	f := r.frames[len(r.frames)-1]
	prefix, known := "", false
	if obj != nil {
		prefix, known = f.paths[mapID(obj)]
	}
	if !known {
		// An empty object can hold no field, so there is nothing to check. A
		// non-empty one was reached some way the Recorder did not see.
		if len(obj) > 0 {
			r.unrouted[Lookup{ClassUID: f.classUID, Path: path}] = true
		}
		return
	}
	full := path
	if prefix != "" {
		full = prefix + "." + path
	}
	r.seen[Lookup{ClassUID: f.classUID, Path: full}] = true

	switch v := found.(type) {
	case map[string]any:
		if v != nil {
			f.paths[mapID(v)] = full
		}
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok && m != nil {
				f.paths[mapID(m)] = full + "[]"
			}
		}
	}
}

// Lookups returns every field read recorded so far, sorted by class and path.
func (r *Recorder) Lookups() []Lookup {
	r.mu.Lock()
	defer r.mu.Unlock()
	return sorted(r.seen)
}

// Unrouted returns the reads made from an object the Recorder could not place
// in its event, sorted. Path is the path within that object, not from the top
// of the event.
func (r *Recorder) Unrouted() []Lookup {
	r.mu.Lock()
	defer r.mu.Unlock()
	return sorted(r.unrouted)
}

func sorted(set map[Lookup]bool) []Lookup {
	out := make([]Lookup, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ClassUID != out[j].ClassUID {
			return out[i].ClassUID < out[j].ClassUID
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// mapID identifies a map value: two map values with the same id are the same
// map. It holds while the map is reachable, which an event's objects are for
// as long as the event is being handled.
func mapID(m map[string]any) uintptr { return reflect.ValueOf(m).Pointer() }
