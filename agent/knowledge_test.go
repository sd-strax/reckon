package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// knowledgeBackend serves canned implicit-retrieval results.
func knowledgeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	f := newFakeBackend(t)
	prev := f.override
	f.override = func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/api/knowledge/recall_sops":
			_, _ = w.Write([]byte(`{"results":[
			  {"sop_id":"sop-1","title":"Ransomware Containment","excerpt":"Isolate first.","score":0.4,"match_rationale":"keyword match"}
			],"coverage":"COMPLETE"}`))
			return true
		case "/api/knowledge/recall_similar_investigations":
			_, _ = w.Write([]byte(`{"results":[
			  {"investigation_ref":"grouping--near","title":"Prior RDP pivot","excerpt":"WIN-FILE01.","score":0.94,"band":"NEAR_DUPLICATE","match_rationale":"cosine 0.94"},
			  {"investigation_ref":"grouping--far","title":"Unrelated BEC","excerpt":"mailbox.","score":0.1,"band":"DISTINCT","match_rationale":"cosine 0.10"}
			],"coverage":"COMPLETE"}`))
			return true
		}
		if prev != nil {
			return prev(w, r)
		}
		return false
	}
	return f
}

// seedInvestigation makes the fake backend's investigation carry a seed +
// posture, so refreshContext's implicit retrieval has a query.
func (f *fakeBackend) withInvestigation(inv Investigation) {
	f.investigation = &inv
}

func newKnowledgeSession(t *testing.T, posture string) (*Session, *fakeBackend) {
	t.Helper()
	f := knowledgeBackend(t)
	f.withInvestigation(Investigation{
		AggregateID: "inv-1", Title: "RDP lateral movement", Status: "ACTIVE",
		SeedSummary: "entity WIN-FILE01", KnowledgeInjection: posture,
	})
	s, err := NewSession(context.Background(), Config{
		Backend: f.client(), LLM: &scriptedLLM{}, InvestigationID: "inv-1",
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return s, f
}

// TestImplicitRetrieval_OptIn: under the default posture, retrieval runs and is
// surfaced with relevance signals, but NOTHING enters the system prompt until
// the analyst includes it.
func TestImplicitRetrieval_OptIn(t *testing.T) {
	s, _ := newKnowledgeSession(t, injectionOptIn)

	items := s.RetrievedKnowledge()
	if len(items) != 3 {
		t.Fatalf("expected 3 surfaced items, got %d: %+v", len(items), items)
	}
	for _, k := range items {
		if k.Included {
			t.Errorf("opt_in must default every item OUT: %+v", k)
		}
		if k.Rationale == "" {
			t.Errorf("relevance rationale missing: %+v", k)
		}
	}
	if strings.Contains(s.System(), "relevant SOPs") || strings.Contains(s.System(), "Similar past investigations") {
		t.Error("knowledge must never enter the system prompt — it rides the turn as a [knowledge] block")
	}
	// Nothing included: the turn carries no knowledge block.
	if u := lastUserMessage(t, s, "first look"); strings.Contains(u, "[knowledge") {
		t.Errorf("opt_in delivered knowledge before the analyst included anything:\n%s", u)
	}

	// The analyst pulls two in; the next turn's user message delivers exactly
	// those, and the system prompt stays byte-identical (prompt-cache prefix).
	before := s.System()
	s.SetIncludedKnowledge([]string{"sop-1", "grouping--near"})
	u := lastUserMessage(t, s, "and now?")
	if !strings.Contains(u, "Ransomware Containment") || !strings.Contains(u, "Prior RDP pivot") {
		t.Errorf("included knowledge not delivered on the turn:\n%s", u)
	}
	if strings.Contains(u, "Unrelated BEC") {
		t.Error("un-included item leaked into the turn")
	}
	if s.System() != before {
		t.Error("including knowledge rewrote the system prompt")
	}
	// Delivered once: it now lives in the history, the following turn repeats nothing.
	if u := lastUserMessage(t, s, "go on"); strings.Contains(u, "[knowledge") {
		t.Errorf("knowledge re-delivered on an unchanged inclusion:\n%s", u)
	}
}

// lastUserMessage runs one turn and returns the text of the user message that
// carried it (the analyst's prompt plus any [engine] blocks the session put
// in front of it).
func lastUserMessage(t *testing.T, s *Session, prompt string) string {
	t.Helper()
	if _, err := s.Turn(context.Background(), prompt); err != nil {
		t.Fatalf("turn: %v", err)
	}
	reqs := s.llm.(*scriptedLLM).requests
	msgs := reqs[len(reqs)-1].Messages
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != RoleUser {
			continue
		}
		var b strings.Builder
		for _, c := range msgs[i].Content {
			b.WriteString(c.Text)
			b.WriteString("\n")
		}
		return b.String()
	}
	t.Fatal("no user message in the model request")
	return ""
}

// TestImplicitRetrieval_Auto: strong matches default in; DISTINCT stays out.
func TestImplicitRetrieval_Auto(t *testing.T) {
	s, _ := newKnowledgeSession(t, injectionAuto)

	byRef := map[string]KnowledgeItem{}
	for _, k := range s.RetrievedKnowledge() {
		byRef[k.Ref] = k
	}
	if !byRef["sop-1"].Included {
		t.Error("auto: a matched SOP should default in")
	}
	if !byRef["grouping--near"].Included {
		t.Error("auto: a NEAR_DUPLICATE case should default in")
	}
	if byRef["grouping--far"].Included {
		t.Error("auto: a DISTINCT case should stay surfaced-but-off")
	}
	u := lastUserMessage(t, s, "first look")
	if !strings.Contains(u, "Ransomware Containment") || !strings.Contains(u, "Prior RDP pivot") {
		t.Errorf("auto-included knowledge not delivered on the first turn:\n%s", u)
	}
	if strings.Contains(u, "Unrelated BEC") {
		t.Error("DISTINCT case leaked into the turn under auto")
	}

	// The analyst can veto a strong match — the next turn says so, and the
	// excerpt is not re-delivered.
	s.SetIncludedKnowledge([]string{"grouping--near"}) // drop sop-1
	u = lastUserMessage(t, s, "and now?")
	if !strings.Contains(u, "removed from context: Ransomware Containment") {
		t.Errorf("veto not surfaced to the model:\n%s", u)
	}
	if strings.Contains(u, "Isolate first.") {
		t.Error("vetoed SOP excerpt re-delivered")
	}
}

// TestImplicitRetrieval_IncludedIsConsulted: included knowledge is folded into
// the turn's consulted provenance (design/06 §6).
func TestImplicitRetrieval_IncludedIsConsulted(t *testing.T) {
	s, f := newKnowledgeSession(t, injectionAuto)
	s.SetIncludedKnowledge([]string{"sop-1", "grouping--near"})

	if _, err := s.Turn(context.Background(), "look into this"); err != nil {
		t.Fatalf("turn: %v", err)
	}
	calls := f.callsTo("/api/interpretations")
	if len(calls) == 0 {
		t.Fatal("no interpretation committed")
	}
	var body InterpretationRequest
	if err := json.Unmarshal(calls[len(calls)-1].Body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.ConsultedSOPs) != 1 || body.ConsultedSOPs[0].SOPID != "sop-1" {
		t.Errorf("included SOP not recorded as consulted: %+v", body.ConsultedSOPs)
	}
	if len(body.ConsultedSimilar) != 1 || body.ConsultedSimilar[0].InvestigationRef != "grouping--near" {
		t.Errorf("included case not recorded as consulted: %+v", body.ConsultedSimilar)
	}
}
