package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// DefaultMaxToolResultBytes bounds one tool result fed back to the model when
// Config.MaxToolResultBytes is unset. A larger answer is shown a page at a time
// (adr/0209).
const DefaultMaxToolResultBytes = 128 << 10

const (
	// maxHeldResults and maxHeldBytes bound the large answers a session keeps
	// for read_result; the oldest goes first.
	maxHeldResults = 8
	maxHeldBytes   = 64 << 20
	// digestTop bounds each digest facet to its most frequent values.
	digestTop = 20
	// boundedKey is the field a paged result carries its framing under.
	boundedKey = "result_bounded"
)

const pageNote = "This result is larger than the limit for one tool result, so it is shown a page at a time: " +
	"each list holds items offset..offset+returned of total. Read the next page with read_result " +
	"(result_id, offset = next_offset), or narrow the query. Items not shown are not absent: " +
	"conclude nothing from what this page leaves out."

// resultStore holds the full bytes of large tool results for the session, so
// read_result can page through the same snapshot the first page came from.
type resultStore struct {
	seq   int
	held  []heldResult // oldest first
	bytes int
}

type heldResult struct {
	id  string
	raw []byte
}

func (r *resultStore) nextID() string {
	r.seq++
	return fmt.Sprintf("res-%d", r.seq)
}

func (r *resultStore) put(id string, raw []byte) {
	r.held = append(r.held, heldResult{id: id, raw: raw})
	r.bytes += len(raw)
	for len(r.held) > maxHeldResults || (r.bytes > maxHeldBytes && len(r.held) > 1) {
		r.bytes -= len(r.held[0].raw)
		r.held = r.held[1:]
	}
}

func (r *resultStore) get(id string) ([]byte, bool) {
	for _, h := range r.held {
		if h.id == id {
			return h.raw, true
		}
	}
	return nil, false
}

// clear drops every held result; ids are never reused.
func (r *resultStore) clear() {
	r.held, r.bytes = nil, 0
}

// listedResult is a JSON result split into its top-level lists, which are
// paged, and everything else, which rides every page. A top-level array is
// presented as the list "items".
type listedResult struct {
	scalars map[string]json.RawMessage
	lists   map[string][]json.RawMessage
}

func splitResult(raw []byte) (listedResult, bool) {
	t := bytes.TrimSpace(raw)
	lr := listedResult{scalars: map[string]json.RawMessage{}, lists: map[string][]json.RawMessage{}}
	if len(t) == 0 {
		return lr, false
	}
	switch t[0] {
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(t, &items) != nil {
			return lr, false
		}
		lr.lists["items"] = items
	case '{':
		var obj map[string]json.RawMessage
		if json.Unmarshal(t, &obj) != nil {
			return lr, false
		}
		for k, v := range obj {
			if vt := bytes.TrimSpace(v); len(vt) > 0 && vt[0] == '[' {
				var items []json.RawMessage
				if json.Unmarshal(vt, &items) == nil {
					lr.lists[k] = items
					continue
				}
			}
			lr.scalars[k] = v
		}
	default:
		return lr, false
	}
	return lr, len(lr.lists) > 0
}

// longest is the length of the longest list: the offset range read_result
// accepts.
func (lr listedResult) longest() int {
	n := 0
	for _, items := range lr.lists {
		n = max(n, len(items))
	}
	return n
}

// boundedNote is the framing a paged result carries under boundedKey.
type boundedNote struct {
	ResultID   string               `json:"result_id,omitempty"`
	LimitBytes int                  `json:"limit_bytes"`
	TotalBytes int                  `json:"total_bytes"`
	Offset     int                  `json:"offset"`
	NextOffset *int                 `json:"next_offset,omitempty"`
	Lists      map[string]listCount `json:"lists,omitempty"`
	// Skipped is set when the item at Offset is alone larger than a page.
	Skipped *int          `json:"oversized_item_skipped,omitempty"`
	Digest  *resultDigest `json:"digest,omitempty"`
	Note    string        `json:"note"`
}

type listCount struct {
	Returned int `json:"returned"`
	Total    int `json:"total"`
}

// render builds the page holding k items of every list from offset, with the
// next page starting at offset+advance.
func (lr listedResult) render(note boundedNote, offset, k, advance int) []byte {
	out := make(map[string]any, len(lr.scalars)+len(lr.lists)+1)
	for name, v := range lr.scalars {
		out[name] = v
	}
	note.Offset = offset
	note.Lists = make(map[string]listCount, len(lr.lists))
	for name, items := range lr.lists {
		lo, hi := min(offset, len(items)), min(offset+k, len(items))
		page := items[lo:hi]
		if page == nil {
			page = []json.RawMessage{}
		}
		out[name] = page
		note.Lists[name] = listCount{Returned: hi - lo, Total: len(items)}
	}
	if next := offset + advance; next < lr.longest() {
		note.NextOffset = &next
	}
	out[boundedKey] = note
	b, _ := json.Marshal(out)
	return b
}

// page renders the largest page from offset that fits in limit bytes. It
// fails only when the non-list fields alone do not fit.
func (lr listedResult) page(note boundedNote, offset, limit int) ([]byte, bool) {
	empty := lr.render(note, offset, 0, 0)
	if len(empty) > limit {
		return nil, false
	}
	remaining := lr.longest() - offset
	// Largest k in [0, remaining] whose page fits: fits(k) is monotone in k.
	k := sort.Search(remaining, func(k int) bool {
		return len(lr.render(note, offset, k+1, k+1)) > limit
	})
	if k == 0 && remaining > 0 {
		// The next item alone is larger than a page: say so and step past it,
		// or paging could never advance.
		skipped := offset
		note.Skipped = &skipped
		return lr.render(note, offset, 0, 1), true
	}
	return lr.render(note, offset, k, k), true
}

// oversized answers a result that cannot be paged (not a JSON object or array
// with a list, or its other fields alone exceed the limit).
func oversized(total, limit int) []byte {
	b, _ := json.Marshal(map[string]any{boundedKey: boundedNote{
		LimitBytes: limit,
		TotalBytes: total,
		Note: fmt.Sprintf("This result is %d bytes, over the %d-byte limit for one tool result, "+
			"and cannot be shown in pages. Narrow the query.", total, limit),
	}})
	return b
}

// boundResult returns raw unchanged when it fits in limit. Otherwise it holds
// raw for read_result and returns its first page, carrying a digest of the
// whole result.
func (s *Session) boundResult(raw []byte) []byte {
	if len(raw) <= s.maxResultBytes {
		return raw
	}
	lr, ok := splitResult(raw)
	if !ok {
		return oversized(len(raw), s.maxResultBytes)
	}
	id := s.results.nextID()
	note := boundedNote{
		ResultID: id, LimitBytes: s.maxResultBytes, TotalBytes: len(raw),
		Digest: digestEvents(lr.lists["events"]), Note: pageNote,
	}
	page, ok := lr.page(note, 0, s.maxResultBytes)
	if !ok {
		return oversized(len(raw), s.maxResultBytes)
	}
	s.results.put(id, raw)
	return page
}

// readResult serves one page of a held result.
func (s *Session) readResult(input json.RawMessage) ([]byte, error) {
	var in struct {
		ResultID string `json:"result_id"`
		Offset   int    `json:"offset"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("bad read_result input: %w", err)
	}
	raw, ok := s.results.get(in.ResultID)
	if !ok {
		return nil, fmt.Errorf("result %q is no longer held (a session keeps its last %d large results); run the query again", in.ResultID, maxHeldResults)
	}
	lr, _ := splitResult(raw)
	if in.Offset < 0 || in.Offset >= lr.longest() {
		return nil, fmt.Errorf("offset %d is out of range: the longest list in %s has %d items", in.Offset, in.ResultID, lr.longest())
	}
	note := boundedNote{ResultID: in.ResultID, LimitBytes: s.maxResultBytes, TotalBytes: len(raw), Note: pageNote}
	page, ok := lr.page(note, in.Offset, s.maxResultBytes)
	if !ok {
		return oversized(len(raw), s.maxResultBytes), nil
	}
	return page, nil
}

// resultDigest is an exact, deterministic overview of every event in a result,
// shown or not, so the model can see the shape of what it has not read.
type resultDigest struct {
	Events  int    `json:"events"`
	First   string `json:"first,omitempty"`
	Last    string `json:"last,omitempty"`
	ByClass facet  `json:"by_class"`
	ByHost  facet  `json:"by_host"`
	ByUser  facet  `json:"by_user"`
}

// facet counts the events carrying each value: the digestTop most frequent,
// and how many distinct values there are in all.
type facet struct {
	Distinct int          `json:"distinct"`
	Top      []valueCount `json:"top,omitempty"`
}

type valueCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// OCSF fields the digest reads hosts and users from.
var (
	hostPaths = [][]string{{"device", "hostname"}, {"dst_endpoint", "hostname"}, {"src_endpoint", "hostname"}}
	userPaths = [][]string{{"actor", "user", "name"}, {"user", "name"}}
)

// digestEvents summarizes a capability result's events list; nil when there
// is none.
func digestEvents(events []json.RawMessage) *resultDigest {
	if len(events) == 0 {
		return nil
	}
	classes, hosts, users := map[string]int{}, map[string]int{}, map[string]int{}
	var first, last time.Time
	for _, raw := range events {
		var ev struct {
			ClassName string         `json:"class_name"`
			Time      time.Time      `json:"time"`
			Payload   map[string]any `json:"payload"`
		}
		if json.Unmarshal(raw, &ev) != nil {
			continue
		}
		if ev.ClassName != "" {
			classes[ev.ClassName]++
		}
		if !ev.Time.IsZero() {
			if first.IsZero() || ev.Time.Before(first) {
				first = ev.Time
			}
			if ev.Time.After(last) {
				last = ev.Time
			}
		}
		countDistinct(hosts, ev.Payload, hostPaths)
		countDistinct(users, ev.Payload, userPaths)
	}
	d := &resultDigest{
		Events:  len(events),
		ByClass: toFacet(classes),
		ByHost:  toFacet(hosts),
		ByUser:  toFacet(users),
	}
	if !first.IsZero() {
		d.First, d.Last = first.UTC().Format(time.RFC3339), last.UTC().Format(time.RFC3339)
	}
	return d
}

// countDistinct counts each distinct string found at paths in payload once.
func countDistinct(counts map[string]int, payload map[string]any, paths [][]string) {
	seen := map[string]bool{}
	for _, p := range paths {
		if v, ok := lookupString(payload, p); ok && !seen[v] {
			seen[v] = true
			counts[v]++
		}
	}
}

func lookupString(m map[string]any, path []string) (string, bool) {
	var cur any = m
	for _, k := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur = obj[k]
	}
	s, ok := cur.(string)
	return s, ok && s != ""
}

func toFacet(counts map[string]int) facet {
	f := facet{Distinct: len(counts)}
	for v, n := range counts {
		f.Top = append(f.Top, valueCount{Value: v, Count: n})
	}
	sort.Slice(f.Top, func(i, j int) bool {
		if f.Top[i].Count != f.Top[j].Count {
			return f.Top[i].Count > f.Top[j].Count
		}
		return f.Top[i].Value < f.Top[j].Value
	})
	if len(f.Top) > digestTop {
		f.Top = f.Top[:digestTop]
	}
	return f
}
