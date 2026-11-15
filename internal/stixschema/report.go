package stixschema

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Report collects the checks of many objects and prints the failures grouped
// by what produced them, so a contributor sees which normalizer or writer
// made the bad object. It is safe for concurrent use.
type Report struct {
	mu     sync.Mutex
	counts map[string]int // checked objects by type
	// failures by source, then by the object type and its problems, so one
	// mistake that many inputs repeat is printed once.
	failures map[string]map[string]*failure
}

type failure struct {
	typ, id  string
	problems []Problem
	times    int
}

// Add checks one object's STIX JSON and records the result under source: a
// label such as "normalizer authentication v5" or "aggregate attestation
// note".
func (r *Report) Add(source string, raw []byte) {
	var obj map[string]any
	problems := []Problem{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		problems = append(problems, Problem{Message: "not a JSON object: " + err.Error()})
	} else {
		problems = Check(obj)
	}
	typ, _ := obj["type"].(string)
	id, _ := obj["id"].(string)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counts == nil {
		r.counts = map[string]int{}
		r.failures = map[string]map[string]*failure{}
	}
	r.counts[typ]++
	if len(problems) == 0 {
		return
	}
	key := typ
	for _, p := range problems {
		key += "\n" + p.String()
	}
	if r.failures[source] == nil {
		r.failures[source] = map[string]*failure{}
	}
	if f, ok := r.failures[source][key]; ok {
		f.times++
		return
	}
	r.failures[source][key] = &failure{typ: typ, id: id, problems: problems, times: 1}
}

// Counts returns how many objects of each type were checked.
func (r *Report) Counts() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int, len(r.counts))
	for k, v := range r.counts {
		out[k] = v
	}
	return out
}

// Total is the number of objects checked.
func (r *Report) Total() int {
	n := 0
	for _, v := range r.Counts() {
		n += v
	}
	return n
}

// Failed reports whether any object failed.
func (r *Report) Failed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.failures) > 0
}

// String prints every failure, grouped by source. Each failure names the
// object's type and id, then one line per problem: the schema and the
// validator's message, or the hand-written rule.
func (r *Report) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	sources := make([]string, 0, len(r.failures))
	for s := range r.failures {
		sources = append(sources, s)
	}
	sort.Strings(sources)
	for _, s := range sources {
		fmt.Fprintf(&b, "%s:\n", s)
		keys := make([]string, 0, len(r.failures[s]))
		for k := range r.failures[s] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			f := r.failures[s][k]
			fmt.Fprintf(&b, "  %s %s", f.typ, f.id)
			if f.times > 1 {
				fmt.Fprintf(&b, " (and %d more objects with the same problems)", f.times-1)
			}
			b.WriteString("\n")
			for _, p := range f.problems {
				fmt.Fprintf(&b, "    - %s\n", p)
			}
		}
	}
	return b.String()
}
