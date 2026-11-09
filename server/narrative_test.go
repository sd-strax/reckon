package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestNarrative_EndToEnd: no narrative is 404; a delegate's draft and a
// person's edit are two revisions with their actors from the token; the
// detail carries the current one; a blank body is 400 and a model on a
// human token is refused by the aggregate.
func TestNarrative_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	b := newTestBackend(t)
	human := mintToken(t, nil)
	delegate := mintToken(t, map[string]any{"delegate_kind": "claude"})
	id := activeInvestigation(t).String()
	base := "/api/investigations/" + id + "/narrative"

	if code, _ := send(t, b, "GET", base, human, ""); code != http.StatusNotFound {
		t.Fatalf("no narrative = %d; want 404", code)
	}
	code, body := send(t, b, "POST", base, delegate, `{"body":"The record so far: one host, one logon.","model":"m-1"}`)
	if code != http.StatusOK {
		t.Fatalf("draft = %d %s", code, body)
	}
	var v NarrativeView
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	if v.Revision != 1 || v.Author.Kind != "AI_DELEGATED" || v.Author.Name != "alice" || v.Author.Model != "m-1" || v.DraftedBy.Model != "m-1" {
		t.Errorf("draft view = %+v", v)
	}
	code, body = send(t, b, "POST", base, human, `{"body":"Edited by hand."}`)
	if code != http.StatusOK {
		t.Fatalf("edit = %d %s", code, body)
	}
	v = NarrativeView{} // a fresh decode: omitted keys must not keep the draft's values
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	if v.Revision != 2 || v.Body != "Edited by hand." || v.Author.Kind != "HUMAN" || v.Author.Model != "" || v.DraftedBy.Kind != "AI_DELEGATED" || len(v.Revisions) != 2 || v.Revisions[1].Body == "" {
		t.Errorf("edit view = %+v", v)
	}
	code, body = send(t, b, "GET", "/api/investigations/"+id, human, "")
	if code != http.StatusOK || !strings.Contains(body, `"narrative":{`) || !strings.Contains(body, `"body":"Edited by hand."`) {
		t.Errorf("detail = %d %s; want the current narrative on it", code, body)
	}
	if code, _ := send(t, b, "POST", base, human, `{"body":"   "}`); code != http.StatusBadRequest {
		t.Errorf("blank body = %d; want 400", code)
	}
	if code, body := send(t, b, "POST", base, human, `{"body":"x","model":"m"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("model on a human token = %d %s; want a refusal", code, body)
	}
	if code, _ := send(t, b, "GET", "/api/investigations/"+uuid.New().String()+"/narrative", human, ""); code != http.StatusNotFound {
		t.Errorf("unknown investigation = %d; want 404", code)
	}
}
