package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/knowledge"
)

// TestKnowledgeCitations_OnFakes: both citation routes answer the collapsed
// rows with the two counts; no aggregate handler is a 503; a failed read is a
// 500, never an empty list.
func TestKnowledgeCitations_OnFakes(t *testing.T) {
	b := newFakeBackend(t)
	invA, invB := uuid.New(), uuid.New()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	reads := &fakeReads{citations: []aggregate.KnowledgeCitation{
		{AggregateID: invB, Title: "second", Status: "ACTIVE", Cited: false, LastAt: at.Add(time.Hour), Acts: 1, Revisions: []int{2}},
		{AggregateID: invA, Title: "first", Status: "CONCLUDED", Cited: true, LastAt: at, Acts: 2, Revisions: []int{1, 2}},
	}}
	b.cfg.Reads = reads
	token := mintToken(t, nil)
	sopID := uuid.New()

	for _, path := range []string{"/api/sops/" + sopID.String() + "/citations", "/api/knowledge/case_summaries/grouping--prior-1/citations"} {
		code, body := send(t, b, "GET", path, token, "")
		if code != http.StatusOK {
			t.Fatalf("%s = %d %s", path, code, body)
		}
		var out KnowledgeCitationsResponse
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatal(err)
		}
		if out.CitedIn != 1 || out.RecalledIn != 1 || len(out.Investigations) != 2 {
			t.Errorf("%s: cited_in=%d recalled_in=%d rows=%d; want 1, 1, 2", path, out.CitedIn, out.RecalledIn, len(out.Investigations))
		}
		if out.Investigations[0].InvestigationID != invB.String() || out.Investigations[0].Cited || out.Investigations[0].Status != "ACTIVE" {
			t.Errorf("%s: first row = %+v; want B, recalled, ACTIVE", path, out.Investigations[0])
		}
		if r := out.Investigations[1]; r.InvestigationID != invA.String() || !r.Cited || r.Acts != 2 || len(r.Revisions) != 2 {
			t.Errorf("%s: second row = %+v; want A, cited, 2 acts, revisions [1 2]", path, r)
		}
		if !strings.Contains(body, `"ref":`) {
			t.Errorf("%s: body names no ref: %s", path, body)
		}
	}
	// A ref in the path that is not an id is refused on the SOP route only.
	if code, _ := send(t, b, "GET", "/api/sops/not-an-id/citations", token, ""); code != http.StatusBadRequest {
		t.Errorf("non-uuid sop id = %d; want 400", code)
	}
	// Nothing consulted: an empty list, not an error.
	b.cfg.Reads = &fakeReads{}
	code, body := send(t, b, "GET", "/api/sops/"+sopID.String()+"/citations", token, "")
	if code != http.StatusOK || !strings.Contains(body, `"investigations":[]`) {
		t.Errorf("no citations = %d %s; want 200 with an empty list", code, body)
	}
	// A failed read is a 500.
	b.cfg.Reads = &fakeReads{citationsErr: errors.New("db down")}
	if code, _ := send(t, b, "GET", "/api/sops/"+sopID.String()+"/citations", token, ""); code != http.StatusInternalServerError {
		t.Errorf("failed read = %d; want 500", code)
	}
	// No aggregate handler: 503, as for appearances.
	b.cfg.Handler = nil
	if code, _ := send(t, b, "GET", "/api/sops/"+sopID.String()+"/citations", token, ""); code != http.StatusServiceUnavailable {
		t.Errorf("no handler = %d; want 503", code)
	}
}

// TestKnowledgeLists_CitedIn_OnFakes: the library lists carry cited_in per row
// from the projection's counts, the case detail carries the procedures learned
// from it, and a deployment without the aggregate lists with every count zero.
func TestKnowledgeLists_CitedIn_OnFakes(t *testing.T) {
	b := newFakeBackend(t)
	token := mintToken(t, nil)
	citedSOP, quietSOP, learnedSOP := uuid.New(), uuid.New(), uuid.New()
	b.cfg.Knowledge = &fakeKnowledge{
		sops: []knowledge.SOP{{ID: citedSOP, Title: "Cited"}, {ID: quietSOP, Title: "Quiet"}},
		summaries: []knowledge.CaseSummaryView{
			{InvestigationRef: "grouping--prior-1", Title: "Prior one"},
			{InvestigationRef: "grouping--prior-2", Title: "Prior two"},
		},
		summary: knowledge.CaseSummaryView{InvestigationRef: "grouping--prior-1", Title: "Prior one", Techniques: []string{}, PrimaryEntities: []string{}, ActionsTaken: []knowledge.SummaryAction{}, Tags: []string{}},
		learned: []knowledge.SOP{{ID: learnedSOP, Title: "Learned from prior one", Source: &knowledge.SOPSource{URL: knowledge.InvestigationSourcePrefix + "prior-1"}}},
	}
	b.cfg.Reads = &fakeReads{counts: map[string]aggregate.CitationCount{
		citedSOP.String():   {CitedIn: 3, RecalledIn: 1},
		"grouping--prior-2": {CitedIn: 1},
	}}

	code, body := send(t, b, "GET", "/api/sops", token, "")
	if code != http.StatusOK {
		t.Fatalf("sops = %d %s", code, body)
	}
	var sops struct {
		SOPs []SOPRow `json:"sops"`
	}
	if err := json.Unmarshal([]byte(body), &sops); err != nil {
		t.Fatal(err)
	}
	if len(sops.SOPs) != 2 || sops.SOPs[0].CitedIn != 3 || sops.SOPs[1].CitedIn != 0 || sops.SOPs[0].Title != "Cited" {
		t.Errorf("sops rows = %+v; want Cited with cited_in 3 and Quiet with 0", sops.SOPs)
	}

	code, body = send(t, b, "GET", "/api/knowledge/case_summaries", token, "")
	if code != http.StatusOK {
		t.Fatalf("case_summaries = %d %s", code, body)
	}
	var cases struct {
		Rows []CaseSummaryRow `json:"case_summaries"`
	}
	if err := json.Unmarshal([]byte(body), &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Rows) != 2 || cases.Rows[0].CitedIn != 0 || cases.Rows[1].CitedIn != 1 {
		t.Errorf("case rows = %+v; want prior-1 with 0 and prior-2 with 1", cases.Rows)
	}

	code, body = send(t, b, "GET", "/api/knowledge/case_summaries/grouping--prior-1", token, "")
	if code != http.StatusOK {
		t.Fatalf("case detail = %d %s", code, body)
	}
	var detail CaseSummaryDetail
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Title != "Prior one" || len(detail.LearnedProcedures) != 1 || detail.LearnedProcedures[0].ID != learnedSOP.String() || detail.LearnedProcedures[0].Title != "Learned from prior one" {
		t.Errorf("case detail = %+v; want the summary with one learned procedure", detail)
	}

	// A failed count fails the list rather than showing zeros as facts.
	b.cfg.Reads = &fakeReads{citationsErr: errors.New("db down")}
	if code, _ := send(t, b, "GET", "/api/sops", token, ""); code != http.StatusInternalServerError {
		t.Errorf("sops with a failed count = %d; want 500", code)
	}
	// Without the aggregate handler the corpus still lists, every count zero.
	b.cfg.Handler = nil
	code, body = send(t, b, "GET", "/api/sops", token, "")
	if code != http.StatusOK || !strings.Contains(body, `"cited_in":0`) {
		t.Errorf("sops without a handler = %d %s; want 200 with zero counts", code, body)
	}
}

// TestKnowledgeCitations_EndToEnd: consulted knowledge recorded through
// POST /api/interpretations in two investigations is read back per procedure
// and per past case, collapsed to one row per investigation.
func TestKnowledgeCitations_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	b := newTestBackend(t)
	// The citation routes sit with the knowledge routes, which register only
	// when a knowledge store is configured; they read the projection, not it.
	b.cfg.Knowledge = &fakeKnowledge{}
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	token := mintToken(t, nil)
	call := func(method, path string, body any) (int, map[string]any) {
		t.Helper()
		var rd *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rd = bytes.NewReader(raw)
		} else {
			rd = bytes.NewReader(nil)
		}
		req, _ := http.NewRequest(method, srv.URL+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	sopID := uuid.New().String()
	const prior = "grouping--prior-1"

	var invs []string
	for _, title := range []string{"first case", "second case"} {
		code, out := call(http.MethodPost, "/api/investigations", map[string]any{"title": title})
		if code != http.StatusCreated {
			t.Fatalf("create = %d %v", code, out)
		}
		invs = append(invs, out["aggregate_id"].(string))
	}
	record := func(inv string, sops []map[string]any, cases []map[string]any) {
		t.Helper()
		body := map[string]any{"investigation_ref": inv, "interpretation_type": "pivot", "rationale": "consulted"}
		if sops != nil {
			body["consulted_sops"] = sops
		}
		if cases != nil {
			body["consulted_similar_investigations"] = cases
		}
		if code, out := call(http.MethodPost, "/api/interpretations", body); code != http.StatusCreated {
			t.Fatalf("interpretation = %d %v", code, out)
		}
	}
	record(invs[0],
		[]map[string]any{{"sop_id": sopID, "title": "Triage", "revision": 1, "cut": "1/16000", "sections": []int{1}, "cited": true}},
		[]map[string]any{{"investigation_ref": prior, "title": "Prior", "band": "RELATED", "cited": false}})
	record(invs[1],
		[]map[string]any{{"sop_id": sopID, "title": "Triage", "revision": 1, "cut": "1/16000"}},
		[]map[string]any{{"investigation_ref": prior, "title": "Prior", "band": "RELATED", "cited": false}})
	// A second act in the second investigation recalls the procedure again:
	// still one row for it.
	record(invs[1], []map[string]any{{"sop_id": sopID, "title": "Triage", "revision": 1, "cut": "1/16000"}}, nil)

	code, out := call(http.MethodGet, "/api/sops/"+sopID+"/citations", nil)
	if code != http.StatusOK {
		t.Fatalf("sop citations = %d %v", code, out)
	}
	if out["cited_in"] != float64(1) || out["recalled_in"] != float64(1) {
		t.Errorf("sop counts = %v / %v; want 1 cited, 1 recalled", out["cited_in"], out["recalled_in"])
	}
	rows, _ := out["investigations"].([]any)
	if len(rows) != 2 {
		t.Fatalf("sop rows = %v; want 2", rows)
	}
	first, _ := rows[0].(map[string]any)
	if first["investigation_id"] != invs[1] || first["cited"] != false || first["acts"] != float64(2) || first["title"] != "second case" {
		t.Errorf("first sop row = %v; want the second case, recalled, two acts", first)
	}

	code, out = call(http.MethodGet, "/api/knowledge/case_summaries/"+prior+"/citations", nil)
	if code != http.StatusOK || out["cited_in"] != float64(0) || out["recalled_in"] != float64(2) {
		t.Errorf("case citations = %d %v; want 0 cited, 2 recalled", code, out)
	}
}
