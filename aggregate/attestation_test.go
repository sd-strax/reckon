package aggregate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// adr/0054: a prediction may declare that a person tests it; a human records
// its result with an attestation the outcome cites as a STIX note. An AI
// delegate can propose such a prediction and never record its result, and can
// never carry an attestation on any prediction.
func TestManualPrediction_Apply(t *testing.T) {
	human := newTestEnvelope("analyst-1")
	ai := newTestEnvelope("analyst-1")
	ai.Actor.Kind = ActorAIDelegated
	ai.Actor.Delegate = &AIDelegate{Vendor: "claude"}

	hRef := HypothesisSTIXID(uuid.New())
	live := map[string]hypothesisState{hRef: {Status: HypothesisOpen}}

	// The AI proposes a manual prediction: legal, and the kind is recorded.
	predID := uuid.New()
	create := RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction, Rationale: "ask the owner",
		Prediction: &PredictionNode{ID: predID, HypothesisRef: hRef, Statement: "the account owner confirms starting the 02:14 session", TestKind: TestKindManual,
			TestQuery: &QuerySpec{Tool: "person", QueryText: "call the owner; ask whether they started the RDP session"}},
	}
	events, err := applyCommand(ai, create, reasoningState(live, nil), testIDs())
	if err != nil {
		t.Fatalf("AI proposing a manual prediction: %v", err)
	}
	rec := recFromEvents(t, events)
	if rec.Prediction == nil || rec.Prediction.TestKind != TestKindManual {
		t.Fatalf("recorded prediction = %+v; want test_kind manual", rec.Prediction)
	}
	// The fold carries the kind, through a transition too.
	var st aggregateState
	st.Predictions = map[string]predictionState{}
	st.Hypotheses = map[string]hypothesisState{}
	foldReasoningEvent(&st, rec)
	pRef := PredictionSTIXID(predID)
	if !st.Predictions[pRef].Manual {
		t.Error("fold lost the manual kind")
	}
	foldReasoningEvent(&st, InterpretationRecorded{PredictionTransition: &PredictionTransitionRecorded{Ref: pRef, From: PredictionUntested, To: PredictionInconclusive}})
	if !st.Predictions[pRef].Manual || st.Predictions[pRef].Status != PredictionInconclusive {
		t.Errorf("fold through transition = %+v; want manual INCONCLUSIVE", st.Predictions[pRef])
	}

	// A bad kind is a shape error.
	bad := create
	bad.Prediction = &PredictionNode{ID: uuid.New(), HypothesisRef: hRef, Statement: "s", TestKind: "oracle"}
	if err := bad.Validate(human); err == nil {
		t.Error("test_kind oracle accepted")
	}

	state := reasoningState(live, map[string]predictionState{pRef: {Status: PredictionUntested, HypothesisRef: hRef, Manual: true}})
	outcome := func(status string, att *Attestation, refs ...string) RecordInterpretation {
		return RecordInterpretation{
			InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction, Rationale: "r",
			PredictionRef: pRef, PredictionStatus: status, TestResultRefs: refs, Attestation: att,
		}
	}
	att := func() *Attestation {
		return &Attestation{ID: uuid.New(), Content: "Owner says they started it from home over VPN at 02:10.", Source: "J. Doe (account owner)", Channel: "phone"}
	}

	// The AI cannot record any outcome on a manual prediction — even with refs.
	for _, status := range []string{PredictionConfirmed, PredictionDisconfirmed, PredictionInconclusive} {
		if _, err := applyCommand(ai, outcome(status, nil, "observed-data--1"), state, testIDs()); err == nil || !strings.Contains(err.Error(), "tested by a person") {
			t.Errorf("AI %s on a manual prediction: err = %v; want the manual guard", status, err)
		}
	}
	// Nor carry an attestation on any prediction, manual or not.
	toolState := reasoningState(live, map[string]predictionState{pRef: {Status: PredictionUntested, HypothesisRef: hRef}})
	if _, err := applyCommand(ai, outcome(PredictionConfirmed, att()), toolState, testIDs()); err == nil || !strings.Contains(err.Error(), "human act") {
		t.Errorf("AI attestation on a tool prediction: err = %v; want the human-act guard", err)
	}

	// The human records the result with an attestation and no telemetry refs:
	// the note is minted, cited first, and listed among the outputs.
	a := att()
	events, err = applyCommand(human, outcome(PredictionConfirmed, a), state, testIDs())
	if err != nil {
		t.Fatalf("human attested outcome: %v", err)
	}
	rec = recFromEvents(t, events)
	noteID := NoteSTIXID(a.ID)
	if rec.Attestation == nil || rec.Attestation.ID != noteID || rec.Attestation.Author != "analyst-1" ||
		rec.Attestation.PredictionRef != pRef || rec.Attestation.HypothesisRef != hRef || rec.Attestation.Channel != "phone" {
		t.Errorf("recorded attestation = %+v; want note %s by analyst-1 on %s/%s", rec.Attestation, noteID, pRef, hRef)
	}
	if rec.PredictionTransition == nil || len(rec.PredictionTransition.TestResultRefs) != 1 || rec.PredictionTransition.TestResultRefs[0] != noteID {
		t.Errorf("transition refs = %+v; want [%s]", rec.PredictionTransition, noteID)
	}
	if !containsString(rec.OutputRefs, noteID) {
		t.Errorf("output refs %v lack the note", rec.OutputRefs)
	}
	// An attestation alongside telemetry refs: the note comes first.
	events, err = applyCommand(human, outcome(PredictionDisconfirmed, att(), "observed-data--9"), toolState, testIDs())
	if err != nil {
		t.Fatalf("human attested outcome with refs on a tool prediction: %v", err)
	}
	if refs := recFromEvents(t, events).PredictionTransition.TestResultRefs; len(refs) != 2 || !strings.HasPrefix(refs[0], "note--") || refs[1] != "observed-data--9" {
		t.Errorf("refs = %v; want the note first, then the telemetry", refs)
	}

	// Shape guards: no attestation on a withdraw, on a create, on another
	// type, or without content / id; a decisive outcome still needs evidence.
	for name, cmd := range map[string]RecordInterpretation{
		"withdraw":        outcome(PredictionWithdrawn, att()),
		"no content":      outcome(PredictionConfirmed, &Attestation{ID: uuid.New(), Content: "  "}),
		"zero id":         outcome(PredictionConfirmed, &Attestation{Content: "x"}),
		"confirmed bare":  outcome(PredictionConfirmed, nil),
		"on a create":     {InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction, Rationale: "r", Prediction: &PredictionNode{ID: uuid.New(), HypothesisRef: hRef, Statement: "s"}, Attestation: att()},
		"on a support":    {InterpretationID: uuid.New(), InterpretationType: InterpretationSupport, Rationale: "r", HypothesisRef: hRef, Attestation: att()},
		"on a free-stand": {InterpretationID: uuid.New(), InterpretationType: InterpretationOther, Rationale: "r", Attestation: att()},
	} {
		if err := cmd.Validate(human); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Withdraw stays legal (and human) on a manual prediction.
	if _, err := applyCommand(human, outcome(PredictionWithdrawn, nil), state, testIDs()); err != nil {
		t.Errorf("human withdraw of a manual prediction: %v", err)
	}
	// The decide gate reads a manual discriminator like any other (adr/0051).
	rival := HypothesisSTIXID(uuid.New())
	gated := reasoningState(map[string]hypothesisState{hRef: {Status: HypothesisOpen}, rival: {Status: HypothesisOpen}},
		map[string]predictionState{pRef: {Status: PredictionUntested, HypothesisRef: hRef, Manual: true, Discriminates: []string{rival}}})
	if _, err := applyCommand(ai, RecordInterpretation{InterpretationID: uuid.New(), InterpretationType: InterpretationSupport, Rationale: "r", HypothesisRef: rival}, gated, testIDs()); err == nil {
		t.Error("AI decided the rival over an untested manual discriminator")
	}
}

// The note is materialized into stix_objects, mirrored onto the prediction
// row, carried by the bundle payload list, and rebuilt by replay.
func TestAttestation_ProjectionAndReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	tenantID := uuid.New()
	h := newTestHandler()
	invID := activeInvestigation(t, h, tenantID)
	env := Envelope{
		AggregateID: invID, TenantID: tenantID, CorrelationID: uuid.New(),
		Actor: Actor{PrincipalID: "analyst-1"}, OccurredAt: newTestEnvelope("").OccurredAt,
	}
	mustHandle := func(cmd Command) {
		t.Helper()
		if _, err := h.Handle(ctx, env, cmd); err != nil {
			t.Fatalf("%s: %v", cmd.Kind(), err)
		}
	}
	hypID, predID, noteID := uuid.New(), uuid.New(), uuid.New()
	mustHandle(RecordInterpretation{InterpretationID: uuid.New(), InterpretationType: InterpretationHypothesis, Rationale: "r",
		Hypothesis: &HypothesisNode{ID: hypID, Statement: "legit admin", Handle: "H2 legit-admin"}})
	hRef := HypothesisSTIXID(hypID)
	mustHandle(RecordInterpretation{InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction, Rationale: "r",
		Prediction: &PredictionNode{ID: predID, HypothesisRef: hRef, Statement: "the owner confirms the session", TestKind: TestKindManual}})
	pRef := PredictionSTIXID(predID)
	mustHandle(RecordInterpretation{InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction, Rationale: "owner confirmed",
		PredictionRef: pRef, PredictionStatus: PredictionConfirmed,
		Attestation: &Attestation{ID: noteID, Content: "Confirmed by phone: started it from home.", Source: "J. Doe", Channel: "phone", Reference: "INC0010042",
			Artifact: &ArtifactLocation{System: "S3", Location: "s3://ir-evidence/2026/call-note.pdf", SHA256: "9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08", Access: "ir-evidence-readers group"}}})

	verify := func(phase string) {
		t.Helper()
		ps, err := ListPredictions(ctx, testDB, invID)
		if err != nil || len(ps) != 1 {
			t.Fatalf("[%s] ListPredictions = %v, %v; want 1", phase, ps, err)
		}
		p := ps[0]
		if p.TestKind != TestKindManual || p.Status != PredictionConfirmed || p.Attestation == nil ||
			p.Attestation.Author != "analyst-1" || p.Attestation.Source != "J. Doe" ||
			len(p.TestResultRefs) != 1 || p.TestResultRefs[0] != NoteSTIXID(noteID) {
			t.Errorf("[%s] prediction = %+v (attestation %+v); want manual CONFIRMED citing the note", phase, p, p.Attestation)
		}
		var typ string
		var payload []byte
		if err := testDB.QueryRow(`SELECT type, payload FROM stix_objects WHERE id = $1`, noteID).Scan(&typ, &payload); err != nil {
			t.Fatalf("[%s] note row: %v", phase, err)
		}
		var node map[string]any
		_ = json.Unmarshal(payload, &node)
		if typ != "note" || node["type"] != "note" || node["content"] != "Confirmed by phone: started it from home." ||
			!strings.Contains(node["abstract"].(string), "attested by analyst-1 by phone: J. Doe") {
			t.Errorf("[%s] note = %s %v", phase, typ, node)
		}
		// Where it happened rides the note as an external reference (#139).
		// So does where the artifact is kept (adr/0139): location, digest, access.
		ext, _ := node["external_references"].([]any)
		if len(ext) != 2 || ext[0].(map[string]any)["external_id"] != "INC0010042" || p.Attestation.Reference != "INC0010042" {
			t.Fatalf("[%s] external_references = %v, reference = %q", phase, ext, p.Attestation.Reference)
		}
		art := ext[1].(map[string]any)
		hashes, _ := art["hashes"].(map[string]any)
		if art["source_name"] != "S3" || art["url"] != "s3://ir-evidence/2026/call-note.pdf" || art["description"] != "ir-evidence-readers group" ||
			hashes["SHA-256"] != "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" {
			t.Errorf("[%s] artifact reference = %v", phase, art)
		}
		if a := p.Attestation.Artifact; a == nil || a.Location != "s3://ir-evidence/2026/call-note.pdf" || a.SHA256 != "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" {
			t.Errorf("[%s] attestation artifact = %+v", phase, p.Attestation.Artifact)
		}
		refs, _ := node["object_refs"].([]any)
		if len(refs) != 2 || refs[0] != pRef || refs[1] != hRef {
			t.Errorf("[%s] note object_refs = %v; want [%s %s]", phase, refs, pRef, hRef)
		}
		payloads, err := ListReasoningStixPayloads(ctx, testDB, invID)
		if err != nil {
			t.Fatalf("[%s] ListReasoningStixPayloads: %v", phase, err)
		}
		found := false
		for _, raw := range payloads {
			var obj struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			}
			if json.Unmarshal(raw, &obj) == nil && obj.Type == "note" && obj.ID == NoteSTIXID(noteID) {
				found = true
			}
		}
		if !found || len(payloads) != 3 {
			t.Errorf("[%s] bundle payloads = %d, note present %v; want 3 with the note", phase, len(payloads), found)
		}
	}
	verify("live")
	if err := h.Replay(ctx); err != nil {
		t.Fatalf("replay: %v", err)
	}
	verify("replayed")
}

// TestValidateArtifact: a location is required once any field is given, the
// digest is a SHA-256, and a link carrying a credential is refused (adr/0139).
func TestValidateArtifact(t *testing.T) {
	att := func(a *ArtifactLocation) *Attestation {
		return &Attestation{ID: uuid.New(), Content: "c", Artifact: a}
	}
	ok := map[string]*ArtifactLocation{
		"none":        nil,
		"all blank":   {System: " ", Location: " "},
		"s3":          {System: "S3", Location: "s3://bucket/key.eml", SHA256: "sha256:" + strings.Repeat("AB", 32), Access: "ir group"},
		"path":        {Location: `\\files\\ir\\note.txt`},
		"plain query": {Location: "https://docs.example/d/42?version=3"},
	}
	for name, a := range ok {
		if err := validateAttestation(att(a)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := map[string]*ArtifactLocation{
		"no location":   {System: "S3", Access: "ir group"},
		"short digest":  {Location: "s3://b/k", SHA256: "abc"},
		"presigned s3":  {Location: "https://b.s3.amazonaws.com/k?X-Amz-Credential=AKIA&X-Amz-Signature=deadbeef"},
		"sas link":      {Location: "https://acct.blob.core.windows.net/c/k?sv=2024&sig=abc"},
		"long location": {Location: "s3://b/" + strings.Repeat("k", artifactLocationMaxRunes)},
	}
	for name, a := range bad {
		if err := validateAttestation(att(a)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if a := trimArtifact(&ArtifactLocation{Location: " s3://b/k ", SHA256: " SHA256:AB "}); a.Location != "s3://b/k" || a.SHA256 != "ab" {
		t.Errorf("trim = %+v", a)
	}
	if got := artifactRefs(&ArtifactLocation{Location: "case INC1 attachment 3"}); len(got) != 1 || got[0].SourceName != "artifact" || got[0].ExternalID != "case INC1 attachment 3" || got[0].URL != "" {
		t.Errorf("no scheme: %+v", got)
	}
}

// TestAttestationRefs: a reference becomes a URL, a hash or an external id (#139).
func TestAttestationRefs(t *testing.T) {
	if got := attestationRefs("https://chat.example/m/42"); len(got) != 1 || got[0].URL != "https://chat.example/m/42" {
		t.Errorf("url: %+v", got)
	}
	if got := attestationRefs("sha256:ab12"); len(got) != 1 || got[0].Hashes["SHA-256"] != "ab12" {
		t.Errorf("hash: %+v", got)
	}
	if got := attestationRefs("<msg-1@mail>"); len(got) != 1 || got[0].ExternalID != "<msg-1@mail>" {
		t.Errorf("id: %+v", got)
	}
	if attestationRefs("") != nil {
		t.Error("an empty reference should add none")
	}
}
