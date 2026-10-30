package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// bigEnvelope builds a capability envelope of n events, each with a padded
// payload, an aligned normalized entry and an event ref. Host H<i%3>, user
// u<i%2>; event i is at base + i minutes.
func bigEnvelope(t *testing.T, n int) []byte {
	t.Helper()
	base := time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC)
	var events, normalized []map[string]any
	var refs []string
	for i := range n {
		id := fmt.Sprintf("ocsf--%04d", i)
		events = append(events, map[string]any{
			"id": id, "class_uid": 3002, "class_name": "Authentication",
			"time": base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
			"payload": map[string]any{
				"actor":        map[string]any{"user": map[string]any{"name": fmt.Sprintf("u%d", i%2)}},
				"dst_endpoint": map[string]any{"hostname": fmt.Sprintf("H%d", i%3)},
				"pad":          strings.Repeat("x", 600),
			},
		})
		normalized = append(normalized, map[string]any{"observed_data": []map[string]any{{"id": "observed-data--" + id}}})
		refs = append(refs, id)
	}
	raw, err := json.Marshal(map[string]any{
		"verb": "enumerate_logons", "coverage": "COMPLETE",
		"degradation_notes": []string{}, "events": events, "normalized": normalized, "ocsf_event_refs": refs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func pageNoteOf(t *testing.T, page []byte) (map[string]json.RawMessage, boundedNote) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(page, &obj); err != nil {
		t.Fatalf("page is not a JSON object: %v\n%s", err, page)
	}
	var note boundedNote
	if err := json.Unmarshal(obj[boundedKey], &note); err != nil {
		t.Fatalf("page has no %s: %v", boundedKey, err)
	}
	return obj, note
}

func eventIDs(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var evs []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &evs); err != nil {
		t.Fatalf("events: %v", err)
	}
	ids := make([]string, len(evs))
	for i, e := range evs {
		ids[i] = e.ID
	}
	return ids
}

func TestBoundResult_UnderLimitUnchanged(t *testing.T) {
	s := &Session{maxResultBytes: 1 << 10}
	raw := []byte(`{"verb":"x","events":[1,2,3]}`)
	if got := s.boundResult(raw); string(got) != string(raw) {
		t.Errorf("a result under the limit changed: %s", got)
	}
	if len(s.results.held) != 0 {
		t.Error("a result under the limit was held")
	}
}

// TestBoundResult_PagesWholeResult: the first page and every read_result page
// are valid JSON under the limit, lists stay aligned, scalars ride every page,
// and reading to the end yields every event exactly once, in order.
func TestBoundResult_PagesWholeResult(t *testing.T) {
	const n, limit = 200, 16 << 10
	raw := bigEnvelope(t, n)
	s := &Session{maxResultBytes: limit}

	page := s.boundResult(raw)
	var all []string
	for pages := 0; ; pages++ {
		if pages > n {
			t.Fatal("paging did not terminate")
		}
		if len(page) > limit {
			t.Fatalf("page %d is %d bytes, over the %d limit", pages, len(page), limit)
		}
		obj, note := pageNoteOf(t, page)
		if string(obj["coverage"]) != `"COMPLETE"` || string(obj["verb"]) != `"enumerate_logons"` {
			t.Fatalf("page %d lost its scalars: coverage=%s verb=%s", pages, obj["coverage"], obj["verb"])
		}
		ev, nz, refs := note.Lists["events"], note.Lists["normalized"], note.Lists["ocsf_event_refs"]
		if ev.Total != n || ev.Returned == 0 || nz != ev || refs != ev {
			t.Fatalf("page %d lists not aligned: %+v", pages, note.Lists)
		}
		if note.Offset != len(all) || note.TotalBytes != len(raw) || note.ResultID == "" {
			t.Fatalf("page %d framing wrong: %+v", pages, note)
		}
		if (pages == 0) != (note.Digest != nil) {
			t.Fatalf("page %d: the digest belongs on the first page only", pages)
		}
		all = append(all, eventIDs(t, obj["events"])...)
		if note.NextOffset == nil {
			break
		}
		var err error
		page, err = s.readResult(json.RawMessage(fmt.Sprintf(`{"result_id":%q,"offset":%d}`, note.ResultID, *note.NextOffset)))
		if err != nil {
			t.Fatalf("read_result: %v", err)
		}
	}
	if len(all) != n {
		t.Fatalf("read %d events across pages; want %d", len(all), n)
	}
	for i, id := range all {
		if id != fmt.Sprintf("ocsf--%04d", i) {
			t.Fatalf("event %d is %s: pages out of order or overlapping", i, id)
		}
	}
}

// TestBoundResult_DigestCoversWholeResult: the digest counts every event, not
// only those on the first page.
func TestBoundResult_DigestCoversWholeResult(t *testing.T) {
	s := &Session{maxResultBytes: 16 << 10}
	_, note := pageNoteOf(t, s.boundResult(bigEnvelope(t, 90)))
	d := note.Digest
	if d == nil || d.Events != 90 {
		t.Fatalf("digest = %+v; want 90 events", d)
	}
	if d.First != "2026-04-20T14:00:00Z" || d.Last != "2026-04-20T15:29:00Z" {
		t.Errorf("time range %s..%s", d.First, d.Last)
	}
	if d.ByHost.Distinct != 3 || d.ByHost.Top[0].Count != 30 {
		t.Errorf("by_host = %+v; want 3 hosts × 30", d.ByHost)
	}
	if d.ByUser.Distinct != 2 || d.ByUser.Top[0] != (valueCount{Value: "u0", Count: 45}) {
		t.Errorf("by_user = %+v", d.ByUser)
	}
	if d.ByClass.Top[0] != (valueCount{Value: "Authentication", Count: 90}) {
		t.Errorf("by_class = %+v", d.ByClass)
	}
}

func TestBoundResult_TopLevelArray(t *testing.T) {
	items := make([]string, 300)
	for i := range items {
		items[i] = strings.Repeat("y", 100)
	}
	raw, _ := json.Marshal(items)
	s := &Session{maxResultBytes: 4 << 10}
	obj, note := pageNoteOf(t, s.boundResult(raw))
	if c := note.Lists["items"]; c.Total != 300 || c.Returned == 0 {
		t.Errorf("items = %+v", c)
	}
	if _, ok := obj["items"]; !ok {
		t.Error("a top-level array is paged under items")
	}
}

// TestBoundResult_Unpageable: a result with no list, or whose other fields
// alone exceed the limit, is answered with a well-formed notice, never a
// byte prefix.
func TestBoundResult_Unpageable(t *testing.T) {
	s := &Session{maxResultBytes: 1 << 10}
	for name, raw := range map[string][]byte{
		"scalar":      []byte(`{"blob":"` + strings.Repeat("z", 4<<10) + `"}`),
		"not json":    []byte(strings.Repeat("z", 4<<10)),
		"big scalars": []byte(`{"blob":"` + strings.Repeat("z", 4<<10) + `","events":[1]}`),
	} {
		obj, note := pageNoteOf(t, s.boundResult(raw))
		if len(obj) != 1 || note.ResultID != "" || note.TotalBytes != len(raw) {
			t.Errorf("%s: got %v / %+v; want only the notice", name, obj, note)
		}
	}
	if len(s.results.held) != 0 {
		t.Error("an unpageable result was held")
	}
}

// TestBoundResult_OversizedItemIsSkipped: an item larger than a page is
// named and stepped past, so paging always advances.
func TestBoundResult_OversizedItemIsSkipped(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"items": []string{"a", strings.Repeat("b", 4<<10), "c"}, "pad": strings.Repeat("p", 100)})
	s := &Session{maxResultBytes: 2 << 10}
	_, first := pageNoteOf(t, s.boundResult(raw))
	if first.Lists["items"].Returned != 1 || first.NextOffset == nil || *first.NextOffset != 1 {
		t.Fatalf("first page = %+v", first)
	}
	page, err := s.readResult(json.RawMessage(fmt.Sprintf(`{"result_id":%q,"offset":1}`, first.ResultID)))
	if err != nil {
		t.Fatal(err)
	}
	_, second := pageNoteOf(t, page)
	if second.Skipped == nil || *second.Skipped != 1 || second.NextOffset == nil || *second.NextOffset != 2 {
		t.Errorf("oversized item not skipped: %+v", second)
	}
}

func TestReadResult_Errors(t *testing.T) {
	s := &Session{maxResultBytes: 16 << 10}
	_, note := pageNoteOf(t, s.boundResult(bigEnvelope(t, 50)))
	if _, err := s.readResult(json.RawMessage(`{"result_id":"res-99","offset":0}`)); err == nil || !strings.Contains(err.Error(), "no longer held") {
		t.Errorf("unknown id: %v", err)
	}
	if _, err := s.readResult(json.RawMessage(fmt.Sprintf(`{"result_id":%q,"offset":50}`, note.ResultID))); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("offset past the end: %v", err)
	}
}

func TestResultStore_EvictsOldest(t *testing.T) {
	var r resultStore
	var ids []string
	for range maxHeldResults + 1 {
		id := r.nextID()
		r.put(id, []byte("{}"))
		ids = append(ids, id)
	}
	if _, ok := r.get(ids[0]); ok {
		t.Error("the oldest result survived past the bound")
	}
	if _, ok := r.get(ids[len(ids)-1]); !ok {
		t.Error("the newest result was evicted")
	}
	r.clear()
	if id := r.nextID(); id == ids[0] {
		t.Error("an id was reused after clear")
	}
}

// TestSession_LargeCapabilityResultIsPaged (#209): through the turn loop, the
// model receives a well-formed first page and reads the next with
// read_result; the configured limit applies.
func TestSession_LargeCapabilityResultIsPaged(t *testing.T) {
	const limit = 16 << 10
	f := newFakeBackend(t)
	big := bigEnvelope(t, 120)
	f.override = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasPrefix(r.URL.Path, "/api/capability/") {
			return false
		}
		_, _ = w.Write(big)
		return true
	}
	llm := &scriptedLLM{script: []CompleteResponse{
		{StopReason: StopToolUse, Content: []ContentBlock{toolUse("t1", "enumerate_logons", `{"entity":{"host":{"hostname":"H0"}}}`)}},
		{StopReason: StopToolUse, Content: []ContentBlock{toolUse("t2", ToolReadResult, `{"result_id":"res-1","offset":1}`)}},
		{StopReason: StopEndTurn, Content: []ContentBlock{TextBlock("done")}},
	}}
	s, err := NewSession(context.Background(), Config{Backend: f.client(), LLM: llm, InvestigationID: "inv-1", MaxToolResultBytes: limit})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, err := s.Turn(context.Background(), "go"); err != nil {
		t.Fatalf("turn: %v", err)
	}
	var results []ContentBlock
	for _, m := range s.messages {
		for _, b := range m.Content {
			if b.Type == BlockToolResult {
				results = append(results, b)
			}
		}
	}
	if len(results) != 2 {
		t.Fatalf("got %d tool results; want 2", len(results))
	}
	for i, r := range results {
		if r.IsError || len(r.Content) > limit || !json.Valid([]byte(r.Content)) {
			t.Fatalf("result %d: error=%v, %d bytes, valid=%v", i, r.IsError, len(r.Content), json.Valid([]byte(r.Content)))
		}
	}
	_, first := pageNoteOf(t, []byte(results[0].Content))
	_, second := pageNoteOf(t, []byte(results[1].Content))
	if first.ResultID != "res-1" || first.Lists["events"].Total != 120 || second.Offset != 1 {
		t.Errorf("first=%+v second=%+v", first, second)
	}
}
