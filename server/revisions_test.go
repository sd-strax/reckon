package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/knowledge"
)

// TestSOPRevisions_OnFakes: the three revision routes (adr/0097) answer the
// facade's shapes, 404 by kind, 400 for a cut this build cannot make, and
// never 409: a historical read is not a pinned read.
func TestSOPRevisions_OnFakes(t *testing.T) {
	b := newFakeBackend(t)
	token := mintToken(t, nil)
	sopID := uuid.New()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	fake := &fakeKnowledge{
		revisions: knowledge.SOPRevisions{SOPID: sopID, Current: 2, Revisions: []knowledge.SOPRevision{
			{Revision: 2, ContentHash: "b", CreatedAt: at.Add(time.Hour), Current: true},
			{Revision: 1, ContentHash: "a", CreatedAt: at, SupersededAt: &at},
		}},
		outline: knowledge.SOPOutline{SOPID: sopID, Title: "Triage", Revision: 1, Cut: "1/16000", Status: "published", Sections: []knowledge.SOPSection{{Idx: 1, Of: 1, HeadingPath: []string{"Triage"}, Location: "Triage", Runes: 8}}},
		section: knowledge.SOPSectionRead{SOPID: sopID, Title: "Triage", Revision: 1, Cut: "1/16000", Status: "published", Section: knowledge.SOPSection{Idx: 1, Of: 1, HeadingPath: []string{"Triage"}, Location: "Triage", Runes: 8, Text: "# Triage"}, Outline: []string{"Triage"}},
	}
	b.cfg.Knowledge = fake
	base := "/api/sops/" + sopID.String() + "/revisions"

	code, body := send(t, b, "GET", base, token, "")
	if code != http.StatusOK {
		t.Fatalf("revisions = %d %s", code, body)
	}
	var revs knowledge.SOPRevisions
	if err := json.Unmarshal([]byte(body), &revs); err != nil {
		t.Fatal(err)
	}
	if revs.Current != 2 || len(revs.Revisions) != 2 || !revs.Revisions[0].Current || revs.Revisions[1].SupersededAt == nil {
		t.Errorf("revisions %+v", revs)
	}
	if code, body := send(t, b, "GET", base+"/1/sections?cut=1/16000", token, ""); code != http.StatusOK || !strings.Contains(body, `"cut":"1/16000"`) || !strings.Contains(body, `"location":"Triage"`) {
		t.Errorf("revision sections = %d %s", code, body)
	}
	if code, body := send(t, b, "GET", base+"/1/sections/1?cut=1/16000", token, ""); code != http.StatusOK || !strings.Contains(body, `"text":"# Triage"`) || !strings.Contains(body, `"outline":["Triage"]`) {
		t.Errorf("revision section = %d %s", code, body)
	}

	// Path values that are not positive integers are refused before the store.
	for _, p := range []string{base + "/0/sections", base + "/x/sections", base + "/1/sections/0", base + "/1/sections/x"} {
		if code, _ := send(t, b, "GET", p, token, ""); code != http.StatusBadRequest {
			t.Errorf("%s = %d; want 400", p, code)
		}
	}
	if code, _ := send(t, b, "GET", "/api/sops/not-an-id/revisions", token, ""); code != http.StatusBadRequest {
		t.Errorf("non-uuid sop id = %d; want 400", code)
	}

	// The facade's refusals map by kind: not found, bad cut, otherwise 500.
	// A moved pin is not among them; these reads never answer 409.
	fake.err = knowledge.ErrNotFound
	for _, p := range []string{base, base + "/1/sections", base + "/1/sections/1"} {
		if code, _ := send(t, b, "GET", p, token, ""); code != http.StatusNotFound {
			t.Errorf("%s not found = %d; want 404", p, code)
		}
	}
	fake.err = knowledge.ErrBadCut
	for _, p := range []string{base + "/1/sections?cut=99/16000", base + "/1/sections/1?cut=99/16000"} {
		if code, _ := send(t, b, "GET", p, token, ""); code != http.StatusBadRequest {
			t.Errorf("%s bad cut = %d; want 400", p, code)
		}
	}
	fake.err = knowledge.ErrRevisionChanged
	for _, p := range []string{base + "/1/sections", base + "/1/sections/1"} {
		if code, _ := send(t, b, "GET", p, token, ""); code == http.StatusConflict {
			t.Errorf("%s answered 409: a revision read is never a moved pin", p)
		}
	}
	fake.err = errors.New("db down")
	for _, p := range []string{base, base + "/1/sections", base + "/1/sections/1"} {
		if code, _ := send(t, b, "GET", p, token, ""); code != http.StatusInternalServerError {
			t.Errorf("%s failed = %d; want 500", p, code)
		}
	}
}
