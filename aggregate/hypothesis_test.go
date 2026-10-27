package aggregate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// reasoningState builds an ACTIVE aggregateState carrying the given node maps.
func reasoningState(hyps map[string]hypothesisState, preds map[string]predictionState) aggregateState {
	if hyps == nil {
		hyps = map[string]hypothesisState{}
	}
	if preds == nil {
		preds = map[string]predictionState{}
	}
	return aggregateState{
		Seq: 4, Exists: true, TenantID: testTenantID, Status: StatusActive,
		Actions: map[uuid.UUID]actionState{}, Hypotheses: hyps, Predictions: preds,
	}
}

// recFromEvents unmarshals the single emitted interpretation event's payload.
func recFromEvents(t *testing.T, events []Event) InterpretationRecorded {
	t.Helper()
	if len(events) != 1 || events[0].Type != EventTypeInterpretationRecorded {
		t.Fatalf("expected one interpretation.recorded event, got %+v", events)
	}
	var rec InterpretationRecorded
	if err := json.Unmarshal(events[0].Payload, &rec); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return rec
}

// TestReasoningNodeShape_Validate covers the pure shape guards: payload
// smuggling on non-node types, the create-XOR-ref rules, outcome evidence
// requirements, and the abandoned flag's home.
func TestReasoningNodeShape_Validate(t *testing.T) {
	env := newTestEnvelope("analyst-1")
	base := RecordInterpretation{
		InterpretationID: uuid.New(),
		Rationale:        "because evidence",
	}

	// A non-node type must not carry node payloads.
	pivot := base
	pivot.InterpretationType = InterpretationPivot
	pivot.Hypothesis = &HypothesisNode{ID: uuid.New(), Statement: "x"}
	if err := pivot.Validate(env); err == nil {
		t.Error("pivot carrying a hypothesis payload accepted")
	}
	pivot.Hypothesis = nil
	pivot.PredictionStatus = PredictionConfirmed
	if err := pivot.Validate(env); err == nil {
		t.Error("pivot carrying a prediction outcome accepted")
	}

	// hypothesis: create XOR acknowledge.
	hyp := base
	hyp.InterpretationType = InterpretationHypothesis
	if err := hyp.Validate(env); err == nil {
		t.Error("type hypothesis with neither create nor ref accepted")
	}
	hyp.Hypothesis = &HypothesisNode{ID: uuid.New(), Statement: "claim"}
	hyp.HypothesisRef = HypothesisSTIXID(uuid.New())
	if err := hyp.Validate(env); err == nil {
		t.Error("type hypothesis with both create and ref accepted")
	}

	// Create requires a statement.
	noStmt := base
	noStmt.InterpretationType = InterpretationHypothesis
	noStmt.Hypothesis = &HypothesisNode{ID: uuid.New()}
	if err := noStmt.Validate(env); err == nil {
		t.Error("hypothesis without statement accepted")
	}

	// support requires a well-formed hypothesis ref.
	sup := base
	sup.InterpretationType = InterpretationSupport
	sup.HypothesisRef = "x-action--" + uuid.NewString() // wrong type prefix
	if err := sup.Validate(env); err == nil {
		t.Error("support with a non-hypothesis ref accepted")
	}

	// abandoned only rides on inconclusive.
	sup.HypothesisRef = HypothesisSTIXID(uuid.New())
	sup.Abandoned = true
	if err := sup.Validate(env); err == nil {
		t.Error("abandoned on type support accepted")
	}

	// A prediction's declared test query is bounded — it lands in the event
	// payload, and bulk belongs in the transcript side store.
	bigQuery := base
	bigQuery.InterpretationType = InterpretationPrediction
	bigQuery.Prediction = &PredictionNode{
		ID: uuid.New(), HypothesisRef: HypothesisSTIXID(uuid.New()), Statement: "s",
		TestQuery: &QuerySpec{Tool: "siem", QueryText: strings.Repeat("q", maxQueryTextRunes+1)},
	}
	if err := bigQuery.Validate(env); err == nil {
		t.Error("over-long test_query.query_text accepted")
	}
	bigQuery.Prediction.TestQuery = &QuerySpec{
		Tool: "siem", QueryText: "q",
		Parameters: map[string]any{"blob": strings.Repeat("p", maxQueryParamBytes+1)},
	}
	if err := bigQuery.Validate(env); err == nil {
		t.Error("oversized test_query.parameters accepted")
	}

	// A decisive prediction outcome must cite test results.
	out := base
	out.InterpretationType = InterpretationPrediction
	out.PredictionRef = PredictionSTIXID(uuid.New())
	out.PredictionStatus = PredictionConfirmed
	if err := out.Validate(env); err == nil {
		t.Error("CONFIRMED outcome without test_result_refs accepted")
	}
	out.TestResultRefs = []string{"observed-data--1"}
	if err := out.Validate(env); err != nil {
		t.Errorf("valid CONFIRMED outcome rejected: %v", err)
	}
	out.PredictionStatus = "MAYBE"
	if err := out.Validate(env); err == nil {
		t.Error("invalid prediction outcome vocabulary accepted")
	}
}

// TestHypothesisLifecycle_Apply covers the state machine at the pure layer:
// authorship-derived initial status, the human-only acknowledgment, evidential
// outcomes from live statuses only, and refinement chaining.
func TestHypothesisLifecycle_Apply(t *testing.T) {
	human := newTestEnvelope("analyst-1")
	ai := newTestEnvelope("analyst-1")
	ai.Actor.Kind = ActorAIDelegated
	ai.Actor.Delegate = &AIDelegate{Vendor: "claude"}

	// AI-authored → PROPOSED; human-authored → OPEN.
	mk := func(id uuid.UUID) RecordInterpretation {
		return RecordInterpretation{
			InterpretationID:   uuid.New(),
			InterpretationType: InterpretationHypothesis,
			Rationale:          "beaconing",
			Hypothesis:         &HypothesisNode{ID: id, Statement: "C2 via DNS"},
		}
	}
	aiEvents, err := applyCommand(ai, mk(uuid.New()), reasoningState(nil, nil))
	if err != nil {
		t.Fatalf("AI create: %v", err)
	}
	if rec := recFromEvents(t, aiEvents); rec.Hypothesis.Status != HypothesisProposed {
		t.Errorf("AI-authored status = %q; want PROPOSED", rec.Hypothesis.Status)
	}
	humanEvents, err := applyCommand(human, mk(uuid.New()), reasoningState(nil, nil))
	if err != nil {
		t.Fatalf("human create: %v", err)
	}
	rec := recFromEvents(t, humanEvents)
	if rec.Hypothesis.Status != HypothesisOpen {
		t.Errorf("human-authored status = %q; want OPEN", rec.Hypothesis.Status)
	}
	// The created node id is recorded as an output ref.
	if len(rec.OutputRefs) != 1 || rec.OutputRefs[0] != rec.Hypothesis.ID {
		t.Errorf("output_refs = %v; want [%s]", rec.OutputRefs, rec.Hypothesis.ID)
	}

	// A refinement's parent must exist.
	orphan := mk(uuid.New())
	orphan.Hypothesis.ParentRef = HypothesisSTIXID(uuid.New())
	if _, err := applyCommand(human, orphan, reasoningState(nil, nil)); err == nil {
		t.Error("refinement with nonexistent parent accepted")
	}

	// Acknowledgment: PROPOSED only, human only.
	ref := HypothesisSTIXID(uuid.New())
	proposed := func() aggregateState {
		return reasoningState(map[string]hypothesisState{ref: {Status: HypothesisProposed}}, nil)
	}
	ack := RecordInterpretation{
		InterpretationID:   uuid.New(),
		InterpretationType: InterpretationHypothesis,
		Rationale:          "worth pursuing",
		HypothesisRef:      ref,
	}
	if _, err := applyCommand(ai, ack, proposed()); err == nil {
		t.Error("AI acknowledgment accepted (human act)")
	}
	ackEvents, err := applyCommand(human, ack, proposed())
	if err != nil {
		t.Fatalf("human ack: %v", err)
	}
	if tr := recFromEvents(t, ackEvents).HypothesisTransition; tr == nil || tr.From != HypothesisProposed || tr.To != HypothesisOpen {
		t.Errorf("ack transition = %+v; want PROPOSED→OPEN", tr)
	}

	// Evidential outcome from OPEN; terminal statuses take no further moves.
	open := reasoningState(map[string]hypothesisState{ref: {Status: HypothesisOpen}}, nil)
	sup := RecordInterpretation{
		InterpretationID:   uuid.New(),
		InterpretationType: InterpretationSupport,
		Rationale:          "three confirmed predictions",
		HypothesisRef:      ref,
	}
	supEvents, err := applyCommand(ai, sup, open)
	if err != nil {
		t.Fatalf("support: %v", err)
	}
	if tr := recFromEvents(t, supEvents).HypothesisTransition; tr.To != HypothesisSupported {
		t.Errorf("support transition to %q; want SUPPORTED", tr.To)
	}
	terminal := reasoningState(map[string]hypothesisState{ref: {Status: HypothesisSupported}}, nil)
	if _, err := applyCommand(human, sup, terminal); err == nil {
		t.Error("outcome on a terminal hypothesis accepted")
	}

	// Human-in-the-loop by default (01 §Interpretation types): an AI delegate may
	// NOT record a decisive outcome on its own un-acknowledged (PROPOSED)
	// hypothesis — a human must Acknowledge it into OPEN first.
	if _, err := applyCommand(ai, sup, proposed()); err == nil {
		t.Error("AI decisive outcome on an un-acknowledged (PROPOSED) hypothesis should be denied by default")
	}
	// A human may, though — engaging with the AI's proposal is the human taking
	// the loop.
	if _, err := applyCommand(human, sup, proposed()); err != nil {
		t.Errorf("human outcome on a PROPOSED hypothesis should be allowed: %v", err)
	}
	// The trust.ai_reasoning dial reopens full automation: with the enabling ref
	// stamped (server sets it only when the tenant enables the dial), the AI
	// drives PROPOSED→SUPPORTED autonomously.
	autoSup := sup
	autoSup.AIReasoningConfigRef = "trust.ai_reasoning"
	aiSup, err := applyCommand(ai, autoSup, proposed())
	if err != nil {
		t.Fatalf("AI outcome on PROPOSED with trust.ai_reasoning enabled rejected: %v", err)
	}
	if tr := recFromEvents(t, aiSup).HypothesisTransition; tr == nil || tr.From != HypothesisProposed || tr.To != HypothesisSupported {
		t.Errorf("AI adjudication (dial on) transition = %+v; want PROPOSED→SUPPORTED", tr)
	}
	// And once decided, the hypothesis can never be acked — analyst disagreement
	// chains a new hypothesis via parent_ref instead.
	if _, err := applyCommand(human, ack, terminal); err == nil {
		t.Error("acknowledgment of a terminal hypothesis accepted")
	}

	// inconclusive routes to INCONCLUSIVE, or ABANDONED with the flag.
	inc := sup
	inc.InterpretationType = InterpretationInconclusive
	inc.Abandoned = true
	incEvents, err := applyCommand(human, inc, reasoningState(map[string]hypothesisState{ref: {Status: HypothesisOpen}}, nil))
	if err != nil {
		t.Fatalf("inconclusive/abandoned: %v", err)
	}
	if tr := recFromEvents(t, incEvents).HypothesisTransition; tr.To != HypothesisAbandoned {
		t.Errorf("abandoned transition to %q; want ABANDONED", tr.To)
	}
}

// TestPredictionLifecycle_Apply covers prediction creation (live hypotheses
// only) and the once-only test outcome.
// TestPredictionOutcome_AIReasoningGate: recording a prediction OUTCOME (the
// "run tests" rung) is gated the same way as hypothesis evaluation — an AI
// delegate can't record it while the parent hypothesis is still PROPOSED, unless
// trust.ai_reasoning is enabled.
func TestPredictionOutcome_AIReasoningGate(t *testing.T) {
	human := newTestEnvelope("analyst-1")
	ai := newTestEnvelope("analyst-1")
	ai.Actor.Kind = ActorAIDelegated
	ai.Actor.Delegate = &AIDelegate{Vendor: "claude"}

	hRef := HypothesisSTIXID(uuid.New())
	pRef := PredictionSTIXID(uuid.New())
	// Parent hypothesis is PROPOSED (AI-authored, not yet acknowledged); its
	// prediction is UNTESTED and linked to it.
	state := reasoningState(
		map[string]hypothesisState{hRef: {Status: HypothesisProposed}},
		map[string]predictionState{pRef: {Status: PredictionUntested, HypothesisRef: hRef}},
	)
	outcome := func() RecordInterpretation {
		return RecordInterpretation{
			InterpretationID:   uuid.New(),
			InterpretationType: InterpretationPrediction,
			Rationale:          "query returned matches",
			PredictionRef:      pRef,
			PredictionStatus:   PredictionConfirmed,
			TestResultRefs:     []string{"observed-data--1"},
		}
	}

	// AI, dial off → denied.
	if _, err := applyCommand(ai, outcome(), state); err == nil {
		t.Error("AI prediction outcome on a PROPOSED-parent hypothesis should be denied by default")
	}
	// Human → allowed.
	if _, err := applyCommand(human, outcome(), state); err != nil {
		t.Errorf("human prediction outcome should be allowed: %v", err)
	}
	// AI with the dial enabled → allowed.
	auto := outcome()
	auto.AIReasoningConfigRef = "trust.ai_reasoning"
	if _, err := applyCommand(ai, auto, state); err != nil {
		t.Errorf("AI prediction outcome with trust.ai_reasoning enabled rejected: %v", err)
	}
}

func TestPredictionLifecycle_Apply(t *testing.T) {
	env := newTestEnvelope("analyst-1")
	hRef := HypothesisSTIXID(uuid.New())

	mk := func(hyp string) RecordInterpretation {
		return RecordInterpretation{
			InterpretationID:   uuid.New(),
			InterpretationType: InterpretationPrediction,
			Rationale:          "if true, we'd see this",
			Prediction:         &PredictionNode{ID: uuid.New(), HypothesisRef: hyp, Statement: "outbound 53/udp spikes"},
		}
	}

	// Hypothesis must exist and be live.
	if _, err := applyCommand(env, mk(hRef), reasoningState(nil, nil)); err == nil {
		t.Error("prediction against nonexistent hypothesis accepted")
	}
	refuted := reasoningState(map[string]hypothesisState{hRef: {Status: HypothesisRefuted}}, nil)
	if _, err := applyCommand(env, mk(hRef), refuted); err == nil {
		t.Error("prediction against a terminal hypothesis accepted")
	}
	open := reasoningState(map[string]hypothesisState{hRef: {Status: HypothesisOpen}}, nil)
	events, err := applyCommand(env, mk(hRef), open)
	if err != nil {
		t.Fatalf("prediction create: %v", err)
	}
	rec := recFromEvents(t, events)
	if rec.Prediction == nil || rec.Prediction.Status != PredictionUntested {
		t.Fatalf("prediction = %+v; want UNTESTED", rec.Prediction)
	}

	// Outcome: UNTESTED only, once.
	pRef := rec.Prediction.ID
	outcome := RecordInterpretation{
		InterpretationID:   uuid.New(),
		InterpretationType: InterpretationPrediction,
		Rationale:          "query returned matches",
		PredictionRef:      pRef,
		PredictionStatus:   PredictionConfirmed,
		TestResultRefs:     []string{"observed-data--1"},
	}
	untested := reasoningState(
		map[string]hypothesisState{hRef: {Status: HypothesisOpen}},
		map[string]predictionState{pRef: {Status: PredictionUntested}},
	)
	outEvents, err := applyCommand(env, outcome, untested)
	if err != nil {
		t.Fatalf("outcome: %v", err)
	}
	if tr := recFromEvents(t, outEvents).PredictionTransition; tr.To != PredictionConfirmed || len(tr.TestResultRefs) != 1 {
		t.Errorf("outcome transition = %+v; want CONFIRMED with refs", tr)
	}
	tested := reasoningState(
		map[string]hypothesisState{hRef: {Status: HypothesisOpen}},
		map[string]predictionState{pRef: {Status: PredictionConfirmed}},
	)
	if _, err := applyCommand(env, outcome, tested); err == nil {
		t.Error("second outcome on a tested prediction accepted")
	}
}

// TestReasoningNodes_ProjectionAndReplay drives the full flow through the
// handler and verifies both stores (stix_objects + *_current) materialize in
// the same commits — then rebuilds everything via Replay and checks the nodes
// survive identically (the projector is a pure function of the event log).
func TestReasoningNodes_ProjectionAndReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	tenantID := uuid.New()
	h := newTestHandler()
	invID := activeInvestigation(t, h, tenantID)

	env := func(kind string) Envelope {
		e := Envelope{
			AggregateID: invID, TenantID: tenantID, CorrelationID: uuid.New(),
			Actor: Actor{PrincipalID: "analyst-1", Kind: kind}, OccurredAt: newTestEnvelope("").OccurredAt,
		}
		if kind == ActorAIDelegated {
			e.Actor.Delegate = &AIDelegate{Vendor: "claude"}
		}
		return e
	}
	mustHandle := func(e Envelope, cmd Command) Result {
		t.Helper()
		res, err := h.Handle(ctx, e, cmd)
		if err != nil {
			t.Fatalf("%s: %v", cmd.Kind(), err)
		}
		return res
	}

	// AI proposes → human acknowledges → prediction → CONFIRMED → SUPPORTED.
	hypID := uuid.New()
	mustHandle(env(ActorAIDelegated), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationHypothesis,
		Rationale:  "beaconing pattern",
		Hypothesis: &HypothesisNode{ID: hypID, Statement: "C2 via DNS tunneling", Labels: []string{"T1071.004"}},
	})
	hRef := HypothesisSTIXID(hypID)
	mustHandle(env(""), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationHypothesis,
		Rationale: "worth pursuing", HypothesisRef: hRef,
	})
	predID := uuid.New()
	mustHandle(env(ActorAIDelegated), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction,
		Rationale:  "if tunneling, TXT volume spikes",
		Prediction: &PredictionNode{ID: predID, HypothesisRef: hRef, Statement: "anomalous TXT query volume from WIN-A"},
	})
	pRef := PredictionSTIXID(predID)
	mustHandle(env(ActorAIDelegated), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction,
		Rationale: "query confirmed the spike", PredictionRef: pRef,
		PredictionStatus: PredictionConfirmed, TestResultRefs: []string{"observed-data--1"},
	})
	mustHandle(env(ActorAIDelegated), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationSupport,
		Rationale: "prediction confirmed", HypothesisRef: hRef,
	})

	verify := func(phase string) {
		t.Helper()
		hs, err := ListHypotheses(ctx, testDB, invID)
		if err != nil || len(hs) != 1 {
			t.Fatalf("[%s] ListHypotheses = %v, %v; want 1", phase, hs, err)
		}
		if hs[0].ID != hRef || hs[0].Status != HypothesisSupported || len(hs[0].Labels) != 1 {
			t.Errorf("[%s] hypothesis = %+v; want %s SUPPORTED with 1 label", phase, hs[0], hRef)
		}
		ps, err := ListPredictions(ctx, testDB, invID)
		if err != nil || len(ps) != 1 {
			t.Fatalf("[%s] ListPredictions = %v, %v; want 1", phase, ps, err)
		}
		if ps[0].Status != PredictionConfirmed || len(ps[0].TestResultRefs) != 1 {
			t.Errorf("[%s] prediction = %+v; want CONFIRMED with refs", phase, ps[0])
		}

		// The canonical STIX node: status transitioned in-place, id intact.
		var payload []byte
		if err := testDB.QueryRow(`SELECT payload FROM stix_objects WHERE id = $1`, hypID).Scan(&payload); err != nil {
			t.Fatalf("[%s] stix x-hypothesis row: %v", phase, err)
		}
		var node map[string]any
		_ = json.Unmarshal(payload, &node)
		if node["type"] != "x-hypothesis" || node["id"] != hRef || node["status"] != HypothesisSupported {
			t.Errorf("[%s] stix node = %v; want x-hypothesis %s SUPPORTED", phase, node, hRef)
		}
		if err := testDB.QueryRow(`SELECT payload FROM stix_objects WHERE id = $1`, predID).Scan(&payload); err != nil {
			t.Fatalf("[%s] stix x-prediction row: %v", phase, err)
		}
		_ = json.Unmarshal(payload, &node)
		if node["status"] != PredictionConfirmed {
			t.Errorf("[%s] stix prediction status = %v; want CONFIRMED", phase, node["status"])
		}
	}
	verify("live")

	// Replay rebuilds the nodes from the event log alone.
	if err := h.Replay(ctx); err != nil {
		t.Fatalf("replay: %v", err)
	}
	verify("replayed")
}

// TestCompetingHypotheses_Apply covers the competing-set additions (01
// §x-hypothesis "Competing hypotheses"): handle + discriminates on creation,
// acknowledge-with-restatement and its tested-wording freeze, the OPEN revise
// path, and the human-only WITHDRAWN prediction status.
// adr/0051: an AI delegate cannot record SUPPORTED/REFUTED over an untested
// discriminator — a rival's against this hypothesis, or this hypothesis's own
// against a live rival. A human can; so can the AI once the prediction is
// tested or withdrawn, or the rival is no longer live.
func TestOutstandingDiscriminatorGate(t *testing.T) {
	human := newTestEnvelope("analyst-1")
	ai := newTestEnvelope("analyst-1")
	ai.Actor.Kind = ActorAIDelegated
	ai.Actor.Delegate = &AIDelegate{Vendor: "claude"}

	h1 := HypothesisSTIXID(uuid.New())
	h2 := HypothesisSTIXID(uuid.New())
	p2 := PredictionSTIXID(uuid.New())
	support := func(ref string) RecordInterpretation {
		return RecordInterpretation{InterpretationID: uuid.New(), InterpretationType: InterpretationSupport, Rationale: "r", HypothesisRef: ref}
	}
	refute := func(ref string) RecordInterpretation {
		return RecordInterpretation{InterpretationID: uuid.New(), InterpretationType: InterpretationRefutation, Rationale: "r", HypothesisRef: ref}
	}
	live := map[string]hypothesisState{h1: {Status: HypothesisOpen}, h2: {Status: HypothesisOpen}}

	// H2 holds an untested prediction that discriminates against H1.
	st := reasoningState(live, map[string]predictionState{p2: {Status: PredictionUntested, HypothesisRef: h2, Discriminates: []string{h1}}})
	if _, err := applyCommand(ai, support(h1), st); err == nil {
		t.Error("AI recorded H1 SUPPORTED over H2's untested discriminator")
	}
	if _, err := applyCommand(ai, refute(h2), st); err == nil {
		t.Error("AI recorded H2 REFUTED while its own discriminator against H1 is untested")
	}
	if _, err := applyCommand(human, support(h1), st); err != nil {
		t.Errorf("human override refused: %v", err)
	}

	// Tested (or withdrawn): the way is clear.
	for _, status := range []string{PredictionConfirmed, PredictionWithdrawn} {
		st := reasoningState(live, map[string]predictionState{p2: {Status: status, HypothesisRef: h2, Discriminates: []string{h1}}})
		if _, err := applyCommand(ai, support(h1), st); err != nil {
			t.Errorf("prediction %s: AI support refused: %v", status, err)
		}
	}
	// A merely-consistent untested prediction (no discriminates) blocks nothing.
	st = reasoningState(live, map[string]predictionState{p2: {Status: PredictionUntested, HypothesisRef: h2}})
	if _, err := applyCommand(ai, support(h1), st); err != nil {
		t.Errorf("non-discriminating prediction blocked a decision: %v", err)
	}
	// A rival that is no longer live cannot hold anything outstanding.
	st = reasoningState(map[string]hypothesisState{h1: {Status: HypothesisOpen}, h2: {Status: HypothesisAbandoned}},
		map[string]predictionState{p2: {Status: PredictionUntested, HypothesisRef: h2, Discriminates: []string{h1}}})
	if _, err := applyCommand(ai, support(h1), st); err != nil {
		t.Errorf("abandoned rival's prediction blocked a decision: %v", err)
	}
}

func TestCompetingHypotheses_Apply(t *testing.T) {
	human := newTestEnvelope("analyst-1")
	ai := newTestEnvelope("analyst-1")
	ai.Actor.Kind = ActorAIDelegated
	ai.Actor.Delegate = &AIDelegate{Vendor: "claude"}

	h1 := HypothesisSTIXID(uuid.New())
	h2 := HypothesisSTIXID(uuid.New())

	// A handle rides creation and lands on the recorded node.
	create := RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationHypothesis, Rationale: "r",
		Hypothesis: &HypothesisNode{ID: uuid.New(), Statement: "stolen creds", Handle: "H1 rdp-lateral"},
	}
	ev, err := applyCommand(ai, create, reasoningState(nil, nil))
	if err != nil {
		t.Fatalf("create with handle: %v", err)
	}
	if rec := recFromEvents(t, ev); rec.Hypothesis.Handle != "H1 rdp-lateral" {
		t.Errorf("handle = %q; want H1 rdp-lateral", rec.Hypothesis.Handle)
	}
	long := create
	long.Hypothesis = &HypothesisNode{ID: uuid.New(), Statement: "x", Handle: strings.Repeat("h", handleMaxRunes+1)}
	if err := long.Validate(human); err == nil {
		t.Error("over-long handle accepted")
	}

	// discriminates: must name a real rival, never itself.
	both := reasoningState(map[string]hypothesisState{h1: {Status: HypothesisOpen}, h2: {Status: HypothesisOpen}}, nil)
	pred := func(d ...string) RecordInterpretation {
		return RecordInterpretation{
			InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction, Rationale: "r",
			Prediction: &PredictionNode{ID: uuid.New(), HypothesisRef: h1, Statement: "baseline shows no RDP", Discriminates: d},
		}
	}
	ev, err = applyCommand(ai, pred(h2), both)
	if err != nil {
		t.Fatalf("discriminating prediction: %v", err)
	}
	if rec := recFromEvents(t, ev); len(rec.Prediction.Discriminates) != 1 || rec.Prediction.Discriminates[0] != h2 {
		t.Errorf("discriminates = %v; want [%s]", rec.Prediction.Discriminates, h2)
	}
	if _, err := applyCommand(ai, pred(HypothesisSTIXID(uuid.New())), both); err == nil {
		t.Error("discriminating a nonexistent hypothesis accepted")
	}
	if err := pred(h1).Validate(human); err == nil {
		t.Error("self-discrimination accepted")
	}
	if err := pred(h2, h2).Validate(human); err == nil {
		t.Error("duplicate discriminates accepted")
	}

	// Acknowledge with restatement: human-only; the transition carries the new
	// wording; frozen once anything under it is tested.
	ack := RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationHypothesis, Rationale: "adopting",
		HypothesisRef: h1, Restatement: "stolen svc_backup creds used over RDP from 10.0.4.20", Handle: "H1 rdp-lateral",
	}
	proposed := reasoningState(map[string]hypothesisState{h1: {Status: HypothesisProposed}}, nil)
	if _, err := applyCommand(ai, ack, proposed); err == nil {
		t.Error("AI restatement accepted (human act)")
	}
	ev, err = applyCommand(human, ack, proposed)
	if err != nil {
		t.Fatalf("ack with restatement: %v", err)
	}
	tr := recFromEvents(t, ev).HypothesisTransition
	if tr == nil || tr.From != HypothesisProposed || tr.To != HypothesisOpen || tr.Statement != ack.Restatement || tr.Handle != "H1 rdp-lateral" {
		t.Errorf("ack transition = %+v; want PROPOSED→OPEN with restatement+handle", tr)
	}
	p1 := PredictionSTIXID(uuid.New())
	tested := reasoningState(
		map[string]hypothesisState{h1: {Status: HypothesisProposed}},
		map[string]predictionState{p1: {Status: PredictionConfirmed, HypothesisRef: h1}})
	if _, err := applyCommand(human, ack, tested); err == nil {
		t.Error("restatement over a tested prediction accepted (wording is frozen)")
	}
	// A withdrawn prediction does not freeze the wording.
	withdrawn := reasoningState(
		map[string]hypothesisState{h1: {Status: HypothesisProposed}},
		map[string]predictionState{p1: {Status: PredictionWithdrawn, HypothesisRef: h1}})
	if _, err := applyCommand(human, ack, withdrawn); err != nil {
		t.Errorf("restatement over a withdrawn prediction rejected: %v", err)
	}
	// Plain ack (no restatement) is still fine over tested predictions.
	plain := ack
	plain.Restatement, plain.Handle = "", ""
	if _, err := applyCommand(human, plain, tested); err != nil {
		t.Errorf("plain ack over tested predictions rejected: %v", err)
	}

	// Revise an OPEN hypothesis: needs a field; keeps status.
	open := reasoningState(map[string]hypothesisState{h1: {Status: HypothesisOpen}}, nil)
	if _, err := applyCommand(human, plain, open); err == nil {
		t.Error("bare ack on an OPEN hypothesis accepted")
	}
	rename := plain
	rename.Handle = "H1 stolen-creds"
	ev, err = applyCommand(human, rename, open)
	if err != nil {
		t.Fatalf("rename on OPEN: %v", err)
	}
	if tr := recFromEvents(t, ev).HypothesisTransition; tr.From != HypothesisOpen || tr.To != HypothesisOpen || tr.Handle != "H1 stolen-creds" {
		t.Errorf("revise transition = %+v; want OPEN→OPEN with handle", tr)
	}
	// Restatement/handle never ride a non-hypothesis act or a create.
	bad := RecordInterpretation{InterpretationID: uuid.New(), InterpretationType: InterpretationSupport, Rationale: "r", HypothesisRef: h1, Handle: "x"}
	if err := bad.Validate(human); err == nil {
		t.Error("handle on a support act accepted")
	}

	// WITHDRAWN: human-only, UNTESTED-only, no evidence, outside the AI gate.
	untested := reasoningState(
		map[string]hypothesisState{h1: {Status: HypothesisProposed}},
		map[string]predictionState{p1: {Status: PredictionUntested, HypothesisRef: h1}})
	withdraw := RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction, Rationale: "wrong test",
		PredictionRef: p1, PredictionStatus: PredictionWithdrawn,
	}
	if _, err := applyCommand(ai, withdraw, untested); err == nil {
		t.Error("AI withdrawal accepted (human act)")
	}
	ev, err = applyCommand(human, withdraw, untested)
	if err != nil {
		t.Fatalf("human withdraw: %v", err)
	}
	if tr := recFromEvents(t, ev).PredictionTransition; tr == nil || tr.To != PredictionWithdrawn {
		t.Errorf("withdraw transition = %+v; want → WITHDRAWN", tr)
	}
	withRefs := withdraw
	withRefs.TestResultRefs = []string{"observed-data--1"}
	if err := withRefs.Validate(human); err == nil {
		t.Error("withdrawal carrying evidence accepted")
	}
	if _, err := applyCommand(human, withdraw, tested); err == nil {
		t.Error("withdrawing a CONFIRMED prediction accepted")
	}
}

// TestCompetingHypotheses_Projection drives the competing-set fields through the
// real handler: handle/discriminates land on the read models and the STIX
// nodes, a restating acknowledgment rewrites the statement in place, a
// withdrawal is a projected status, and replay rebuilds all of it.
func TestCompetingHypotheses_Projection(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	tenantID := uuid.New()
	h := newTestHandler()
	invID := activeInvestigation(t, h, tenantID)

	env := func(kind string) Envelope {
		e := Envelope{
			AggregateID: invID, TenantID: tenantID, CorrelationID: uuid.New(),
			Actor: Actor{PrincipalID: "analyst-1", Kind: kind}, OccurredAt: newTestEnvelope("").OccurredAt,
		}
		if kind == ActorAIDelegated {
			e.Actor.Delegate = &AIDelegate{Vendor: "claude"}
		}
		return e
	}
	mustHandle := func(e Envelope, cmd Command) {
		t.Helper()
		if _, err := h.Handle(ctx, e, cmd); err != nil {
			t.Fatalf("%s: %v", cmd.Kind(), err)
		}
	}

	h1, h2 := uuid.New(), uuid.New()
	h1Ref, h2Ref := HypothesisSTIXID(h1), HypothesisSTIXID(h2)
	mustHandle(env(ActorAIDelegated), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationHypothesis, Rationale: "leader",
		Hypothesis: &HypothesisNode{ID: h1, Statement: "stolen creds", Handle: "H1 rdp-lateral"},
	})
	mustHandle(env(ActorAIDelegated), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationHypothesis, Rationale: "null",
		Hypothesis: &HypothesisNode{ID: h2, Statement: "legit admin session", Handle: "H2 legit-admin"},
	})
	p1, p2 := uuid.New(), uuid.New()
	mustHandle(env(ActorAIDelegated), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction, Rationale: "discriminator",
		Prediction: &PredictionNode{ID: p1, HypothesisRef: h1Ref, Statement: "baseline shows no RDP", Discriminates: []string{h2Ref}},
	})
	mustHandle(env(ActorAIDelegated), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction, Rationale: "bad test",
		Prediction: &PredictionNode{ID: p2, HypothesisRef: h1Ref, Statement: "something irrelevant"},
	})
	// Human adopts H1 with a sharpened statement, withdraws the bad test.
	mustHandle(env(""), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationHypothesis, Rationale: "adopt",
		HypothesisRef: h1Ref, Restatement: "stolen svc_backup creds used over RDP from 10.0.4.20",
	})
	mustHandle(env(""), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction, Rationale: "not a real test",
		PredictionRef: PredictionSTIXID(p2), PredictionStatus: PredictionWithdrawn,
	})
	// Rename H2 while it is still PROPOSED is an ack; rename H1 (OPEN) is a revise.
	mustHandle(env(""), RecordInterpretation{
		InterpretationID: uuid.New(), InterpretationType: InterpretationHypothesis, Rationale: "rename",
		HypothesisRef: h1Ref, Handle: "H1 stolen-creds",
	})

	verify := func(phase string) {
		t.Helper()
		hs, err := ListHypotheses(ctx, testDB, invID)
		if err != nil || len(hs) != 2 {
			t.Fatalf("[%s] ListHypotheses = %v, %v; want 2", phase, hs, err)
		}
		if hs[0].ID != h1Ref || hs[0].Status != HypothesisOpen || hs[0].Handle != "H1 stolen-creds" ||
			hs[0].Statement != "stolen svc_backup creds used over RDP from 10.0.4.20" {
			t.Errorf("[%s] H1 = %+v; want OPEN, renamed, restated", phase, hs[0])
		}
		if hs[1].Handle != "H2 legit-admin" || hs[1].Status != HypothesisProposed {
			t.Errorf("[%s] H2 = %+v; want PROPOSED with handle", phase, hs[1])
		}
		ps, err := ListPredictions(ctx, testDB, invID)
		if err != nil || len(ps) != 2 {
			t.Fatalf("[%s] ListPredictions = %v, %v; want 2", phase, ps, err)
		}
		if len(ps[0].Discriminates) != 1 || ps[0].Discriminates[0] != h2Ref || ps[0].Status != PredictionUntested {
			t.Errorf("[%s] p1 = %+v; want UNTESTED discriminating H2", phase, ps[0])
		}
		if ps[1].Status != PredictionWithdrawn {
			t.Errorf("[%s] p2 status = %s; want WITHDRAWN", phase, ps[1].Status)
		}
		var payload []byte
		if err := testDB.QueryRow(`SELECT payload FROM stix_objects WHERE id = $1`, h1).Scan(&payload); err != nil {
			t.Fatalf("[%s] stix x-hypothesis row: %v", phase, err)
		}
		var node map[string]any
		_ = json.Unmarshal(payload, &node)
		if node["handle"] != "H1 stolen-creds" || node["statement"] != "stolen svc_backup creds used over RDP from 10.0.4.20" {
			t.Errorf("[%s] stix H1 = %v; want renamed + restated", phase, node)
		}
		if err := testDB.QueryRow(`SELECT payload FROM stix_objects WHERE id = $1`, p1).Scan(&payload); err != nil {
			t.Fatalf("[%s] stix x-prediction row: %v", phase, err)
		}
		_ = json.Unmarshal(payload, &node)
		if d, _ := node["discriminates"].([]any); len(d) != 1 || d[0] != h2Ref {
			t.Errorf("[%s] stix p1 discriminates = %v; want [%s]", phase, node["discriminates"], h2Ref)
		}
	}
	verify("live")
	if err := h.Replay(ctx); err != nil {
		t.Fatalf("replay: %v", err)
	}
	verify("replayed")
}

// adr/0090: every recorded prediction holds a short title — authored with it,
// else derived deterministically from the statement at record time — and the
// derivation is the one function every reader uses.
func TestPredictionTitle(t *testing.T) {
	cases := []struct{ statement, want string }{
		{"the account's 30-day logon history shows only network logons from the backup host", "the account's 30-day logon history shows only network logons…"},
		{"outbound 53/udp volume from the host spikes, with TXT records over 200 bytes", "outbound 53/udp volume from the host spikes"},
		{"baseline shows no RDP.", "baseline shows no RDP"},
		{"short, with a comma", "short, with a comma"},
		{"the owner confirms the session — by phone, within the hour", "the owner confirms the session"},
	}
	for _, c := range cases {
		if got := DeriveTitle(c.statement); got != c.want {
			t.Errorf("DeriveTitle(%q) = %q; want %q", c.statement, got, c.want)
		}
	}
	if got := NormalizeTitle("  A title with a trailing period.  "); got != "A title with a trailing period" {
		t.Errorf("NormalizeTitle = %q", got)
	}

	hRef := HypothesisSTIXID(uuid.New())
	live := map[string]hypothesisState{hRef: {Status: HypothesisOpen}}
	env := newTestEnvelope("analyst-1")
	record := func(title string) *PredictionRecorded {
		t.Helper()
		events, err := applyCommand(env, RecordInterpretation{
			InterpretationID: uuid.New(), InterpretationType: InterpretationPrediction, Rationale: "r",
			Prediction: &PredictionNode{ID: uuid.New(), HypothesisRef: hRef, Statement: "outbound 53/udp volume from the host spikes, with TXT records over 200 bytes", Title: title},
		}, reasoningState(live, nil))
		if err != nil {
			t.Fatal(err)
		}
		return recFromEvents(t, events).Prediction
	}
	if p := record(""); p.Title != "outbound 53/udp volume from the host spikes" || !p.TitleDerived {
		t.Errorf("derived title = %+v", p)
	}
	if p := record("DNS volume spike."); p.Title != "DNS volume spike" || p.TitleDerived {
		t.Errorf("authored title = %+v", p)
	}
}
