package aggregate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/internal/stixschema"
	"github.com/sd-strax/reckon/stix"
)

// TestReasoningPayloadsAreValidSTIX builds the projector's STIX objects
// through the functions it uses (the hypothesis, the prediction and the
// attestation note) and checks each against the STIX 2.1 and reckon schemas
// (adr/0357 rule 8). Each object is built bare and with every optional field.
// The status and result edits the projector makes in SQL are applied the same
// way here, so the object after a test is checked too.
func TestReasoningPayloadsAreValidSTIX(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 678_000_000, time.UTC)
	evt := Event{OccurredAt: at}
	hyp := HypothesisSTIXID(uuid.New())
	parent := HypothesisSTIXID(uuid.New())
	pred := "reckon-prediction--" + uuid.NewString()
	observed := at.Add(-time.Hour)
	var report stixschema.Report
	add := func(source string, raw []byte, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		report.Add(source, raw)
	}

	raw, err := hypothesisPayload(evt, InterpretationRecorded{}, HypothesisRecorded{ID: hyp, Statement: "s", Status: "PROPOSED"})
	add("hypothesis, bare", raw, err)
	raw, err = hypothesisPayload(evt, InterpretationRecorded{Summary: "why"}, HypothesisRecorded{
		ID: hyp, Statement: "s", Handle: "h", Status: "OPEN", ParentRef: parent,
		RootedAtRef: "reckon-host--" + uuid.NewString(), Labels: []string{"T1021.001"},
	})
	add("hypothesis, full", raw, err)
	add("hypothesis, after a decision", projectEdit(t, raw, map[string]any{"status": "SUPPORTED"}, at.Add(time.Minute)), nil)

	raw, err = predictionPayload(evt, PredictionRecorded{ID: pred, HypothesisRef: hyp, Statement: "s", Title: "t", Status: "UNTESTED"})
	add("prediction, bare", raw, err)
	raw, err = predictionPayload(evt, PredictionRecorded{
		ID: pred, HypothesisRef: hyp, Statement: "s", Title: "t", TitleDerived: true, Status: "UNTESTED",
		TestQuery:     &QuerySpec{Tool: "query_logs", QueryText: "q", Parameters: map[string]any{"host": "ws-1"}},
		Discriminates: []string{parent}, TestKind: TestKindManual,
	})
	add("prediction, full", raw, err)
	add("prediction, after its test", projectEdit(t, raw, map[string]any{
		"status": PredictionConfirmed, "test_result_refs": []string{"observed-data--" + uuid.NewString(), NoteSTIXID(uuid.New())},
	}, at.Add(time.Minute)), nil)

	raw, err = attestationNotePayload(evt, AttestationRecorded{ID: NoteSTIXID(uuid.New()), PredictionRef: pred, Author: "u-1", Content: "yes"})
	add("attestation note, bare", raw, err)
	raw, err = attestationNotePayload(evt, AttestationRecorded{
		ID: NoteSTIXID(uuid.New()), PredictionRef: pred, HypothesisRef: hyp, Author: "u-1", AuthorName: "Ana Analyst",
		Content: "the owner confirmed the session", Source: "Jane Doe", Channel: "phone", ObservedAt: &observed,
		Reference: "https://cases.example/123",
		Artifact:  &ArtifactLocation{System: "store", Location: "s3://bucket/key", SHA256: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", Access: "ir-team"},
	})
	add("attestation note, full", raw, err)

	if report.Failed() {
		t.Errorf("reasoning objects that are not valid STIX 2.1:\n%s", report.String())
	}
	if got := report.Counts(); got[stixTypeHypothesis] != 3 || got[stixTypePrediction] != 3 || got[stixTypeNote] != 2 {
		t.Errorf("checked %v", got)
	}
}

// projectEdit sets top-level properties and modified on a stored payload, as
// the projector's jsonb_set updates do.
func projectEdit(t *testing.T, raw []byte, set map[string]any, at time.Time) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for k, v := range set {
		m[k] = v
	}
	m["modified"] = stix.Timestamp(at)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A reasoning object missing a property its schema requires is refused, so
// the projector's write, and with it the event's transaction, fails
// (adr/0357).
func TestReasoningPayloadRefusesAnEmptyStatement(t *testing.T) {
	evt := Event{OccurredAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	if _, err := hypothesisPayload(evt, InterpretationRecorded{}, HypothesisRecorded{ID: HypothesisSTIXID(uuid.New()), Status: "OPEN"}); err == nil {
		t.Error("a hypothesis with no statement was written")
	}
	if _, err := predictionPayload(evt, PredictionRecorded{ID: "reckon-prediction--" + uuid.NewString(), HypothesisRef: HypothesisSTIXID(uuid.New()), Status: "UNTESTED"}); err == nil {
		t.Error("a prediction with no statement was written")
	}
}

// A node's change is written whole through its payload function (adr/0357):
// an empty refs list is left out, never stored as [], and the change keeps
// what the node already held.
func TestReasoningNodeRewrite(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	n := predictionNode{PredictionRecorded: PredictionRecorded{ID: "reckon-prediction--" + uuid.NewString(), HypothesisRef: HypothesisSTIXID(uuid.New()), Statement: "s", Status: "UNTESTED"}, TestResultRefs: []string{}}
	raw, err := n.payload(at, at)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if _, ok := m["test_result_refs"]; ok {
		t.Errorf("an empty test_result_refs was stored: %s", raw)
	}
	// Decoding the stored payload and writing it again gives the same bytes.
	var back predictionNode
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	again, err := back.payload(back.Created, at)
	if err != nil || string(again) != string(raw) {
		t.Errorf("rewrite changed the payload:\n%s\n%s (%v)", raw, again, err)
	}
	var stixHyp hypothesisNode
	h, _ := hypothesisPayload(Event{OccurredAt: at}, InterpretationRecorded{Summary: "why"}, HypothesisRecorded{ID: HypothesisSTIXID(uuid.New()), Statement: "s", Status: "OPEN", Labels: []string{"T1021"}})
	if err := json.Unmarshal(h, &stixHyp); err != nil || stixHyp.Rationale != "why" || !stixHyp.Created.Equal(at) {
		t.Fatalf("decoded hypothesis = %+v, %v", stixHyp, err)
	}
	stixHyp.Status = "SUPPORTED"
	if _, err := stixHyp.payload(stixHyp.Created, at.Add(time.Hour)); err != nil {
		t.Errorf("a status change was refused: %v", err)
	}
	stixHyp.Statement = ""
	if _, err := stixHyp.payload(stixHyp.Created, at.Add(time.Hour)); err == nil {
		t.Error("a change that empties the statement was written")
	}
}

// A ref field holds a STIX identifier, checked at the command boundary
// (adr/0357).
func TestRefFieldsAreSTIXIdentifiers(t *testing.T) {
	node := func(rooted string) HypothesisNode {
		return HypothesisNode{ID: uuid.New(), Statement: "s", RootedAtRef: rooted}
	}
	if err := validateHypothesisNode(node("reckon-host--" + uuid.NewString())); err != nil {
		t.Errorf("a STIX rooted_at_ref was refused: %v", err)
	}
	for _, bad := range []string{"WS1", "reckon-host--1", uuid.NewString(), "Reckon-Host--" + uuid.NewString()} {
		if err := validateHypothesisNode(node(bad)); err == nil {
			t.Errorf("rooted_at_ref %q was accepted", bad)
		}
	}
	env := newTestEnvelope("analyst-1")
	if err := (AddMember{StixObjectRef: "10.0.0.1"}).Validate(env); err == nil {
		t.Error("a member ref that is not a STIX identifier was accepted")
	}
}

// A citation is stored split by form: STIX ids in test_result_refs, raw OCSF
// event ids in test_result_event_ids, each left out when empty, and the
// stored prediction is valid STIX. A ref of neither form is refused at the
// command (adr/0357).
func TestPredictionCitationsAreSplitByForm(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	od, note, event := "observed-data--"+uuid.NewString(), NoteSTIXID(uuid.New()), uuid.NewString()
	n := predictionNode{PredictionRecorded: PredictionRecorded{ID: "reckon-prediction--" + uuid.NewString(), HypothesisRef: HypothesisSTIXID(uuid.New()), Statement: "s", Status: PredictionConfirmed}}
	n.cite([]string{note, event, od})
	raw, err := n.payload(at, at)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if refs, _ := m["test_result_refs"].([]any); len(refs) != 2 || refs[0] != note || refs[1] != od {
		t.Errorf("test_result_refs = %v; want the note and the observation, in order", m["test_result_refs"])
	}
	if ids, _ := m["test_result_event_ids"].([]any); len(ids) != 1 || ids[0] != event {
		t.Errorf("test_result_event_ids = %v; want the raw event", m["test_result_event_ids"])
	}
	if p := stixschema.CheckJSON(raw); len(p) != 0 {
		t.Errorf("a mixed citation is not valid STIX: %v", p)
	}
	n.cite([]string{od})
	raw, _ = n.payload(at, at)
	m = map[string]any{}
	_ = json.Unmarshal(raw, &m)
	if _, ok := m["test_result_event_ids"]; ok {
		t.Errorf("an empty test_result_event_ids was stored: %s", raw)
	}

	if err := validateCitations("test_result_refs", []string{od, event, note}); err != nil {
		t.Errorf("valid citations were refused: %v", err)
	}
	for _, bad := range []string{"WS1", "observed-data--1", "10.0.0.1", uuid.NewString() + "x"} {
		if err := validateCitations("test_result_refs", []string{bad}); err == nil || !strings.Contains(err.Error(), "neither a STIX id") {
			t.Errorf("citation %q: err = %v; want it refused", bad, err)
		}
	}
}
