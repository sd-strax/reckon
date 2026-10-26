package aggregate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// x-hypothesis status values (01-domain-model.md §x-hypothesis). Status is a
// projection over interpretation events, never stored aggregate state: a
// hypothesis is created PROPOSED (AI-authored) or OPEN (human-authored), a
// human acknowledgment moves PROPOSED→OPEN, and the evidential outcomes
// (support / refutation / inconclusive interpretations) terminate it. Terminal
// statuses accept no further transitions in v0 — a changed judgment is a new
// hypothesis (parent_ref chains the refinement).
const (
	HypothesisProposed     = "PROPOSED"
	HypothesisOpen         = "OPEN"
	HypothesisSupported    = "SUPPORTED"
	HypothesisRefuted      = "REFUTED"
	HypothesisInconclusive = "INCONCLUSIVE"
	HypothesisAbandoned    = "ABANDONED"
)

// x-prediction status values (01-domain-model.md §x-prediction). A prediction
// is created UNTESTED against a PROPOSED/OPEN hypothesis and terminates when
// its test outcome is recorded — or when a human WITHDRAWS it untested (a
// discarded test, not an evidential outcome: it carries no evidence and never
// feeds a hypothesis evaluation).
const (
	PredictionUntested     = "UNTESTED"
	PredictionConfirmed    = "CONFIRMED"
	PredictionDisconfirmed = "DISCONFIRMED"
	PredictionInconclusive = "INCONCLUSIVE"
	PredictionWithdrawn    = "WITHDRAWN"
)

// STIX type prefixes for the reasoning nodes (custom objects per the x- STIX
// convention, 01-domain-model.md §Adopted vs invented).
const (
	stixTypeHypothesis = "x-hypothesis"
	stixTypePrediction = "x-prediction"
	// stixTypeNote is STIX 2.1's Note, adopted unchanged (01 "Adopted vs
	// invented"): the object a human's attestation of a prediction result is
	// recorded as (adr/0054).
	stixTypeNote = "note"
)

// x-prediction test kinds (01 §x-prediction, adr/0054). A TOOL test is a query
// the agent or the analyst runs; a MANUAL test is a person's answer — the
// account owner confirming a session, the change owner confirming a window —
// which no verb can fetch. The kind decides who may record the result: a
// manual prediction's outcome is a human act, carrying an Attestation.
const (
	TestKindTool   = "tool"
	TestKindManual = "manual"
)

// Bounds on an attestation (adr/0054): the content is what a person said or
// what was checked by hand, a paragraph at most; source and channel are labels.
const (
	attestationContentMaxRunes = 2000
	attestationLabelMaxRunes   = 200
)

// NoteSTIXID renders the canonical STIX id for an attestation note uuid.
func NoteSTIXID(id uuid.UUID) string { return stixTypeNote + "--" + id.String() }

// HypothesisSTIXID renders the canonical STIX id for a hypothesis uuid.
func HypothesisSTIXID(id uuid.UUID) string { return stixTypeHypothesis + "--" + id.String() }

// PredictionSTIXID renders the canonical STIX id for a prediction uuid.
func PredictionSTIXID(id uuid.UUID) string { return stixTypePrediction + "--" + id.String() }

// stixIDUUID extracts the uuid part of a `<type>--<uuid>` STIX id, checking the
// type prefix. Returns an error on any malformation.
func stixIDUUID(stixID, wantType string) (uuid.UUID, error) {
	rest, ok := strings.CutPrefix(stixID, wantType+"--")
	if !ok {
		return uuid.UUID{}, fmt.Errorf("%q is not a %s id", stixID, wantType)
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("%q is not a %s id: %w", stixID, wantType, err)
	}
	return id, nil
}

// statementMaxRunes bounds the hypothesis/prediction statement — a claim, not
// an essay; supporting detail belongs in the rationale and transcript.
const statementMaxRunes = 1000

// maxLabels bounds the label list (ATT&CK technique ids by convention).
const maxLabels = 20

// handleMaxRunes bounds a hypothesis handle — a mnemonic ("H1 rdp-lateral"),
// not a second statement.
const handleMaxRunes = 40

// maxDiscriminates bounds a prediction's discriminates list: a competing set is
// a handful of rivals (01 §x-hypothesis caps the agent at three), never a crowd.
const maxDiscriminates = 8

// Bounds on a prediction's declared test query — it lands in the event payload
// (and the STIX node), so it must stay a query spec, not a bulk carrier. The
// transcript side store is where large content belongs.
const (
	maxQueryTextRunes  = 4000
	maxQueryParamBytes = 4096
)

// HypothesisNode is the creation payload for a new x-hypothesis, carried on a
// RecordInterpretation of type "hypothesis" (01: hypotheses are OUTPUTS of
// interpretations, not separate primitives — there is no CreateHypothesis
// command). ID is minted by the caller (the endpoint), like RequestAction.
type HypothesisNode struct {
	ID          uuid.UUID `json:"id"`
	Statement   string    `json:"statement"`
	Handle      string    `json:"handle,omitempty"`        // short mnemonic (01 §x-hypothesis)
	ParentRef   string    `json:"parent_ref,omitempty"`    // x-hypothesis-- id (refinement chain)
	RootedAtRef string    `json:"rooted_at_ref,omitempty"` // SCO id (anchor entity)
	Labels      []string  `json:"labels,omitempty"`        // ATT&CK technique ids by convention
}

// QuerySpec is a prediction's optional declared test (01 §x-prediction): what
// query against which tool would confirm or disconfirm it.
type QuerySpec struct {
	Tool       string         `json:"tool"`
	QueryText  string         `json:"query_text"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// PredictionNode is the creation payload for a new x-prediction, carried on a
// RecordInterpretation of type "prediction".
type PredictionNode struct {
	ID            uuid.UUID  `json:"id"`
	HypothesisRef string     `json:"hypothesis_ref"`
	Statement     string     `json:"statement"`
	TestQuery     *QuerySpec `json:"test_query,omitempty"`
	// Discriminates lists the rival x-hypothesis ids whose expectation for this
	// prediction is the OPPOSITE of the owner's (01 §x-prediction): CONFIRMED
	// supports the owner and counts against these; DISCONFIRMED the reverse.
	// Empty means the prediction is merely consistent with its owner.
	Discriminates []string `json:"discriminates,omitempty"`
	// TestKind is TestKindTool (the default; empty reads as tool) or
	// TestKindManual — a person's answer decides it (adr/0054). For a manual
	// test, TestQuery says whom to ask and what.
	TestKind string `json:"test_kind,omitempty"`
}

// Attestation is what a human observed outside any tool — the answer to a
// question put to a person, a check made by hand — carried on a prediction
// outcome as its evidence (adr/0054). The server mints ID; the aggregate
// materializes a STIX Note whose id is prepended to the outcome's
// test_result_refs. The evidence discipline is unchanged — a decisive outcome
// still cites what was observed — what changed is that a human observation is
// now citeable, and reads as attested rather than as telemetry. Human-only.
type Attestation struct {
	ID         uuid.UUID  `json:"id"`
	Content    string     `json:"content"`               // what was observed: who said what
	Source     string     `json:"source,omitempty"`      // who was asked, or what was checked
	Channel    string     `json:"channel,omitempty"`     // phone, chat, email, in person…
	ObservedAt *time.Time `json:"observed_at,omitempty"` // when; the act's time if omitted
}

// AttestationRecorded is the note content recorded on the event when a human
// attests a prediction's result; the projector materializes it as a `note`.
type AttestationRecorded struct {
	ID            string     `json:"id"` // note--<uuid>
	PredictionRef string     `json:"prediction_ref"`
	HypothesisRef string     `json:"hypothesis_ref"`
	Author        string     `json:"author"`                // the human principal (id)
	AuthorName    string     `json:"author_name,omitempty"` // the principal's display name at the time, if known
	Content       string     `json:"content"`
	Source        string     `json:"source,omitempty"`
	Channel       string     `json:"channel,omitempty"`
	ObservedAt    *time.Time `json:"observed_at,omitempty"`
}

// --- event payload records ----------------------------------------------------

// HypothesisRecorded is the node content recorded on the interpretation event
// when a hypothesis is created. The projector materializes it into stix_objects
// and hypothesis_current; foldState folds its status for transition legality.
type HypothesisRecorded struct {
	ID          string   `json:"id"` // full STIX id
	Statement   string   `json:"statement"`
	Handle      string   `json:"handle,omitempty"`
	Status      string   `json:"status"` // PROPOSED (AI-authored) | OPEN (human-authored)
	ParentRef   string   `json:"parent_ref,omitempty"`
	RootedAtRef string   `json:"rooted_at_ref,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

// HypothesisTransitionRecorded records a status move on an existing hypothesis
// (acknowledgment or evidential outcome), or — From == To == OPEN — a human
// field update. Statement/Handle are set when the act restated the hypothesis
// (acknowledge-with-restatement, 01 §x-hypothesis): the projector rewrites the
// node's fields; the prior wording stays on the earlier event.
type HypothesisTransitionRecorded struct {
	Ref       string `json:"ref"` // full STIX id
	From      string `json:"from"`
	To        string `json:"to"`
	Statement string `json:"statement,omitempty"`
	Handle    string `json:"handle,omitempty"`
}

// PredictionRecorded is the node content recorded when a prediction is created.
type PredictionRecorded struct {
	ID            string     `json:"id"` // full STIX id
	HypothesisRef string     `json:"hypothesis_ref"`
	Statement     string     `json:"statement"`
	Status        string     `json:"status"` // always UNTESTED at creation
	TestQuery     *QuerySpec `json:"test_query,omitempty"`
	Discriminates []string   `json:"discriminates,omitempty"`
	TestKind      string     `json:"test_kind,omitempty"` // "manual" when a person tests it; empty = tool
}

// PredictionTransitionRecorded records a prediction's test outcome.
type PredictionTransitionRecorded struct {
	Ref            string   `json:"ref"` // full STIX id
	From           string   `json:"from"`
	To             string   `json:"to"`
	TestResultRefs []string `json:"test_result_refs,omitempty"` // ObservedData / Sighting ids
}

// --- folded state ---------------------------------------------------------------

// hypothesisState / predictionState are the per-node folded state, keyed by full
// STIX id in aggregateState. Only what transition legality needs.
type hypothesisState struct {
	Status string
}

type predictionState struct {
	Status        string
	HypothesisRef string   // parent — so an outcome can check the hypothesis is acknowledged
	Discriminates []string // rivals this prediction decides against — so an outcome can check nothing is outstanding (adr/0051)
	Manual        bool     // a person tests it — so an outcome can check the actor is human (adr/0054)
}

// foldReasoningEvent applies one interpretation.recorded event's node content to
// the folded maps. Interpretation events without node content (lifecycle,
// action-*, free-standing reasoning) are a no-op here.
func foldReasoningEvent(s *aggregateState, rec InterpretationRecorded) {
	if rec.Hypothesis != nil {
		s.Hypotheses[rec.Hypothesis.ID] = hypothesisState{Status: rec.Hypothesis.Status}
	}
	if rec.HypothesisTransition != nil {
		s.Hypotheses[rec.HypothesisTransition.Ref] = hypothesisState{Status: rec.HypothesisTransition.To}
	}
	if rec.Prediction != nil {
		s.Predictions[rec.Prediction.ID] = predictionState{
			Status: rec.Prediction.Status, HypothesisRef: rec.Prediction.HypothesisRef,
			Discriminates: rec.Prediction.Discriminates, Manual: rec.Prediction.TestKind == TestKindManual,
		}
	}
	if rec.PredictionTransition != nil {
		prev := s.Predictions[rec.PredictionTransition.Ref]
		prev.Status = rec.PredictionTransition.To
		s.Predictions[rec.PredictionTransition.Ref] = prev
	}
}

// --- command validation (shape) -------------------------------------------------

// validateReasoningNodeShape runs the pure shape checks for the node payloads a
// RecordInterpretation may carry, per interpretation type. State-dependent
// legality (existence, current status) lives in applyReasoningNodes.
func validateReasoningNodeShape(c RecordInterpretation) error {
	// An attestation is evidence for a prediction OUTCOME and nothing else
	// (adr/0054): never on a create, never on another type.
	if c.Attestation != nil && (c.InterpretationType != InterpretationPrediction || c.PredictionRef == "") {
		return errors.New("RecordInterpretation: an attestation rides only a prediction outcome (type prediction with prediction_ref)")
	}
	// No smuggling: node payloads are only legal on the types that produce them.
	nodeBearing := map[string]bool{
		InterpretationHypothesis:   true,
		InterpretationSupport:      true,
		InterpretationRefutation:   true,
		InterpretationInconclusive: true,
		InterpretationPrediction:   true,
	}
	if !nodeBearing[c.InterpretationType] {
		if c.Hypothesis != nil || c.HypothesisRef != "" || c.Prediction != nil || c.PredictionRef != "" ||
			c.Abandoned || c.PredictionStatus != "" || len(c.TestResultRefs) > 0 || c.Attestation != nil ||
			c.Restatement != "" || c.Handle != "" {
			return fmt.Errorf("RecordInterpretation: type %q must not carry hypothesis/prediction payloads", c.InterpretationType)
		}
		return nil
	}

	// A restatement/handle rides only on the acknowledge-or-revise form of a
	// hypothesis act (hypothesis_ref set) — a create carries them on the node.
	if (c.Restatement != "" || c.Handle != "") && (c.InterpretationType != InterpretationHypothesis || c.HypothesisRef == "") {
		return errors.New("RecordInterpretation: restatement/handle are only valid on a hypothesis acknowledge/revise (type hypothesis with hypothesis_ref)")
	}

	switch c.InterpretationType {
	case InterpretationHypothesis:
		if c.Prediction != nil || c.PredictionRef != "" {
			return errors.New("RecordInterpretation: type hypothesis must not carry prediction payloads")
		}
		// Exactly one of: create (Hypothesis) or acknowledge/revise (HypothesisRef).
		if (c.Hypothesis == nil) == (c.HypothesisRef == "") {
			return errors.New("RecordInterpretation: type hypothesis requires exactly one of hypothesis (create) or hypothesis_ref (acknowledge)")
		}
		if c.Hypothesis != nil {
			return validateHypothesisNode(*c.Hypothesis)
		}
		if _, err := stixIDUUID(c.HypothesisRef, stixTypeHypothesis); err != nil {
			return fmt.Errorf("RecordInterpretation: %w", err)
		}
		if c.Restatement != "" && utf8.RuneCountInString(c.Restatement) > statementMaxRunes {
			return fmt.Errorf("RecordInterpretation: restatement exceeds %d chars", statementMaxRunes)
		}
		if err := validateHandle(c.Handle); err != nil {
			return err
		}
	case InterpretationSupport, InterpretationRefutation, InterpretationInconclusive:
		if c.HypothesisRef == "" {
			return fmt.Errorf("RecordInterpretation: type %s requires hypothesis_ref", c.InterpretationType)
		}
		if _, err := stixIDUUID(c.HypothesisRef, stixTypeHypothesis); err != nil {
			return fmt.Errorf("RecordInterpretation: %w", err)
		}
		if c.Hypothesis != nil || c.Prediction != nil || c.PredictionRef != "" {
			return fmt.Errorf("RecordInterpretation: type %s carries only hypothesis_ref", c.InterpretationType)
		}
		if c.Abandoned && c.InterpretationType != InterpretationInconclusive {
			return errors.New("RecordInterpretation: abandoned is only valid on type inconclusive")
		}
	case InterpretationPrediction:
		if c.Hypothesis != nil || c.HypothesisRef != "" {
			return errors.New("RecordInterpretation: type prediction must not carry hypothesis payloads")
		}
		// Exactly one of: create (Prediction) or record outcome (PredictionRef).
		if (c.Prediction == nil) == (c.PredictionRef == "") {
			return errors.New("RecordInterpretation: type prediction requires exactly one of prediction (create) or prediction_ref (outcome)")
		}
		if c.Prediction != nil {
			if c.PredictionStatus != "" || len(c.TestResultRefs) > 0 || c.Attestation != nil {
				return errors.New("RecordInterpretation: a prediction create carries no outcome fields")
			}
			return validatePredictionNode(*c.Prediction)
		}
		if err := validateAttestation(c.Attestation); err != nil {
			return err
		}
		if _, err := stixIDUUID(c.PredictionRef, stixTypePrediction); err != nil {
			return fmt.Errorf("RecordInterpretation: %w", err)
		}
		switch c.PredictionStatus {
		case PredictionConfirmed, PredictionDisconfirmed:
			// The decisive outcomes must cite what was observed (04 §1's
			// evidence discipline): a confirmation with nothing observed is a
			// claim, not a test result.
			// An attestation IS what was observed — a person's answer, a check
			// made by hand — materialized as a citeable note (adr/0054).
			if len(c.TestResultRefs) == 0 && c.Attestation == nil {
				return fmt.Errorf("RecordInterpretation: prediction outcome %s requires test_result_refs (or an attestation)", c.PredictionStatus)
			}
		case PredictionInconclusive:
		case PredictionWithdrawn:
			// A withdrawal is not an evidential outcome — it carries no evidence.
			if len(c.TestResultRefs) > 0 || c.Attestation != nil {
				return errors.New("RecordInterpretation: a WITHDRAWN prediction carries no test_result_refs or attestation")
			}
		default:
			return fmt.Errorf("RecordInterpretation: invalid prediction outcome %q", c.PredictionStatus)
		}
		if len(c.TestResultRefs) > maxRefsPerInterpretation {
			return fmt.Errorf("RecordInterpretation: test_result_refs exceed %d", maxRefsPerInterpretation)
		}
	}

	// Abandoned only rides on inconclusive.
	if c.Abandoned && c.InterpretationType != InterpretationInconclusive {
		return errors.New("RecordInterpretation: abandoned is only valid on type inconclusive")
	}
	return nil
}

func validateHypothesisNode(h HypothesisNode) error {
	if h.ID == (uuid.UUID{}) {
		return errors.New("RecordInterpretation: hypothesis.id is zero")
	}
	if strings.TrimSpace(h.Statement) == "" {
		return errors.New("RecordInterpretation: hypothesis.statement is required")
	}
	if utf8.RuneCountInString(h.Statement) > statementMaxRunes {
		return fmt.Errorf("RecordInterpretation: hypothesis.statement exceeds %d chars", statementMaxRunes)
	}
	if h.ParentRef != "" {
		if _, err := stixIDUUID(h.ParentRef, stixTypeHypothesis); err != nil {
			return fmt.Errorf("RecordInterpretation: parent_ref: %w", err)
		}
	}
	if len(h.Labels) > maxLabels {
		return fmt.Errorf("RecordInterpretation: hypothesis labels exceed %d", maxLabels)
	}
	if err := validateRefList("labels", h.Labels); err != nil {
		return err
	}
	if h.RootedAtRef != "" {
		if err := validateRefList("rooted_at_ref", []string{h.RootedAtRef}); err != nil {
			return err
		}
	}
	return validateHandle(h.Handle)
}

// validateHandle bounds a hypothesis handle: a single short line.
func validateHandle(handle string) error {
	if handle == "" {
		return nil
	}
	if utf8.RuneCountInString(handle) > handleMaxRunes {
		return fmt.Errorf("RecordInterpretation: hypothesis handle exceeds %d chars", handleMaxRunes)
	}
	if strings.ContainsAny(handle, "\r\n") {
		return errors.New("RecordInterpretation: hypothesis handle must be a single line")
	}
	return nil
}

// validateAttestation runs the shape checks on an outcome's attestation
// (adr/0054): a minted id and a bounded content; source and channel are
// optional labels. nil is legal — most outcomes cite telemetry.
func validateAttestation(a *Attestation) error {
	if a == nil {
		return nil
	}
	if a.ID == (uuid.UUID{}) {
		return errors.New("RecordInterpretation: attestation.id is zero")
	}
	if strings.TrimSpace(a.Content) == "" {
		return errors.New("RecordInterpretation: attestation.content is required — what the person said, or what was checked")
	}
	if utf8.RuneCountInString(a.Content) > attestationContentMaxRunes {
		return fmt.Errorf("RecordInterpretation: attestation.content exceeds %d chars", attestationContentMaxRunes)
	}
	if utf8.RuneCountInString(a.Source) > attestationLabelMaxRunes || utf8.RuneCountInString(a.Channel) > attestationLabelMaxRunes {
		return fmt.Errorf("RecordInterpretation: attestation.source/channel exceed %d chars", attestationLabelMaxRunes)
	}
	return nil
}

func validatePredictionNode(p PredictionNode) error {
	switch p.TestKind {
	case "", TestKindTool, TestKindManual:
	default:
		return fmt.Errorf("RecordInterpretation: prediction.test_kind %q is not tool|manual", p.TestKind)
	}
	if p.ID == (uuid.UUID{}) {
		return errors.New("RecordInterpretation: prediction.id is zero")
	}
	if _, err := stixIDUUID(p.HypothesisRef, stixTypeHypothesis); err != nil {
		return fmt.Errorf("RecordInterpretation: prediction.hypothesis_ref: %w", err)
	}
	if strings.TrimSpace(p.Statement) == "" {
		return errors.New("RecordInterpretation: prediction.statement is required")
	}
	if utf8.RuneCountInString(p.Statement) > statementMaxRunes {
		return fmt.Errorf("RecordInterpretation: prediction.statement exceeds %d chars", statementMaxRunes)
	}
	if len(p.Discriminates) > maxDiscriminates {
		return fmt.Errorf("RecordInterpretation: prediction.discriminates exceeds %d", maxDiscriminates)
	}
	seen := make(map[string]bool, len(p.Discriminates))
	for _, ref := range p.Discriminates {
		if _, err := stixIDUUID(ref, stixTypeHypothesis); err != nil {
			return fmt.Errorf("RecordInterpretation: prediction.discriminates: %w", err)
		}
		if ref == p.HypothesisRef {
			return errors.New("RecordInterpretation: prediction.discriminates must not include its own hypothesis")
		}
		if seen[ref] {
			return fmt.Errorf("RecordInterpretation: prediction.discriminates repeats %s", ref)
		}
		seen[ref] = true
	}
	// The test query lands in the event payload — bound it (the transcript
	// side store is where bulk belongs).
	if q := p.TestQuery; q != nil {
		if utf8.RuneCountInString(q.QueryText) > maxQueryTextRunes {
			return fmt.Errorf("RecordInterpretation: test_query.query_text exceeds %d chars", maxQueryTextRunes)
		}
		if len(q.Parameters) > 0 {
			raw, err := json.Marshal(q.Parameters)
			if err != nil {
				return fmt.Errorf("RecordInterpretation: test_query.parameters: %w", err)
			}
			if len(raw) > maxQueryParamBytes {
				return fmt.Errorf("RecordInterpretation: test_query.parameters exceed %d bytes", maxQueryParamBytes)
			}
		}
	}
	return nil
}

// --- apply (state-dependent legality + event enrichment) -------------------------

// applyReasoningNodes validates a node-bearing interpretation against folded
// state and enriches the InterpretationRecorded payload with the node content
// the projector materializes. Returns nil for types that carry no nodes.
func applyReasoningNodes(env Envelope, state aggregateState, c RecordInterpretation, rec *InterpretationRecorded) error {
	switch c.InterpretationType {
	case InterpretationHypothesis:
		if c.Hypothesis != nil {
			return applyHypothesisCreate(env, state, *c.Hypothesis, rec)
		}
		return applyHypothesisAckOrRevise(env, state, c, rec)
	case InterpretationSupport:
		return applyHypothesisOutcome(env, state, c.HypothesisRef, HypothesisSupported, c, rec)
	case InterpretationRefutation:
		return applyHypothesisOutcome(env, state, c.HypothesisRef, HypothesisRefuted, c, rec)
	case InterpretationInconclusive:
		to := HypothesisInconclusive
		if c.Abandoned {
			to = HypothesisAbandoned
		}
		return applyHypothesisOutcome(env, state, c.HypothesisRef, to, c, rec)
	case InterpretationPrediction:
		if c.Prediction != nil {
			return applyPredictionCreate(state, *c.Prediction, rec)
		}
		return applyPredictionOutcome(env, state, c, rec)
	default:
		return nil
	}
}

func applyHypothesisCreate(env Envelope, state aggregateState, h HypothesisNode, rec *InterpretationRecorded) error {
	stixID := HypothesisSTIXID(h.ID)
	if _, exists := state.Hypotheses[stixID]; exists {
		return fmt.Errorf("RecordInterpretation rejected: hypothesis %s already exists", stixID)
	}
	// A refinement must chain to a real parent in this investigation.
	if h.ParentRef != "" {
		if _, ok := state.Hypotheses[h.ParentRef]; !ok {
			return fmt.Errorf("RecordInterpretation rejected: parent hypothesis %s does not exist", h.ParentRef)
		}
	}
	// AI-authored hypotheses arrive PROPOSED and await a human acknowledgment;
	// human-authored ones are OPEN immediately (01 §Interpretation types:
	// "AI-PROPOSED → OPEN acknowledgment").
	status := HypothesisOpen
	if env.Actor.IsAIDelegated() {
		status = HypothesisProposed
	}
	rec.Hypothesis = &HypothesisRecorded{
		ID:          stixID,
		Statement:   h.Statement,
		Handle:      h.Handle,
		Status:      status,
		ParentRef:   h.ParentRef,
		RootedAtRef: h.RootedAtRef,
		Labels:      h.Labels,
	}
	rec.OutputRefs = append(rec.OutputRefs, stixID)
	return nil
}

// hypothesisHasResults reports whether any prediction under the hypothesis
// carries a test result (anything but UNTESTED / WITHDRAWN). Tested wording is
// frozen (01 §x-hypothesis): a result was recorded against the statement as it
// stood, so a restatement after that point is a refinement via parent_ref, not
// an edit.
func hypothesisHasResults(state aggregateState, ref string) bool {
	for _, p := range state.Predictions {
		if p.HypothesisRef == ref && p.Status != PredictionUntested && p.Status != PredictionWithdrawn {
			return true
		}
	}
	return false
}

// applyHypothesisAckOrRevise handles a type=hypothesis act carrying a
// hypothesis_ref: on a PROPOSED hypothesis it is the acknowledgment (→ OPEN),
// optionally restating the claim in the same act; on an OPEN hypothesis it is a
// field update (statement/handle, no status change). Both are human acts — an
// AI delegate can neither adopt a proposal nor rewrite an analyst-owned claim —
// and a restatement is legal only while nothing under the hypothesis has been
// tested.
func applyHypothesisAckOrRevise(env Envelope, state aggregateState, c RecordInterpretation, rec *InterpretationRecorded) error {
	ref := c.HypothesisRef
	hs, ok := state.Hypotheses[ref]
	if !ok {
		return fmt.Errorf("RecordInterpretation rejected: hypothesis %s does not exist", ref)
	}
	// Acknowledging an AI-proposed hypothesis is the human taking ownership of
	// the line of inquiry — an AI delegate cannot acknowledge (its own, or any)
	// proposal into OPEN, nor restate a hypothesis.
	if env.Actor.IsAIDelegated() {
		return errors.New("RecordInterpretation rejected: acknowledging or restating a hypothesis is a human act")
	}
	revises := c.Restatement != "" || c.Handle != ""
	switch hs.Status {
	case HypothesisProposed:
		// acknowledge (+ optional restatement)
	case HypothesisOpen:
		if !revises {
			return fmt.Errorf("RecordInterpretation rejected: hypothesis %s is already OPEN (a revise needs a restatement or handle)", ref)
		}
	default:
		return fmt.Errorf("RecordInterpretation rejected: hypothesis %s is %s, only PROPOSED can be acknowledged and only OPEN revised", ref, hs.Status)
	}
	if c.Restatement != "" && hypothesisHasResults(state, ref) {
		return fmt.Errorf("RecordInterpretation rejected: hypothesis %s already has tested predictions — a changed theory is a new hypothesis refining it (parent_ref), not a restatement", ref)
	}
	rec.HypothesisTransition = &HypothesisTransitionRecorded{
		Ref: ref, From: hs.Status, To: HypothesisOpen,
		Statement: c.Restatement, Handle: c.Handle,
	}
	rec.OutputRefs = append(rec.OutputRefs, ref)
	return nil
}

// aiReasoningGate enforces the human-in-the-loop rung (01 §Interpretation types,
// "AI-PROPOSED → OPEN acknowledgment"): an AI delegate may not record an
// evidential OUTCOME — a prediction result or a hypothesis evaluation — on a
// hypothesis that is still PROPOSED (never human-acknowledged). A human passes
// freely (engaging with an AI proposal is itself the human taking the loop), and
// an OPEN hypothesis passes freely. The tenant dial trust.ai_reasoning reopens
// full automation: the server stamps AIReasoningConfigRef only when it is on, so
// absent stamp + AI actor + PROPOSED = denied on every path (structural
// default-deny, mirroring the AI-verdict dial).
// outstandingDiscriminatorGate enforces decide-against-rivals (01 §x-hypothesis;
// adr/0051) for an AI delegate: a hypothesis may not be recorded SUPPORTED or
// REFUTED while an UNTESTED prediction discriminates between it and a live
// rival — whether the rival holds that prediction against it, or it holds one
// against the rival. Such a set is undecided; confirmations on one side do
// not substitute. The rule was a prompt contract; it became a gate when the
// agent broke it inside a single turn, writing the discriminator and then
// deciding over it, where no per-turn state block could have shown it. A
// human is not gated: the analyst clears the way by testing or withdrawing
// the prediction, or records the outcome themselves — an override that is a
// visible human act, never a flag an AI could set.
func outstandingDiscriminatorGate(env Envelope, state aggregateState, ref, to string) error {
	if !env.Actor.IsAIDelegated() || (to != HypothesisSupported && to != HypothesisRefuted) {
		return nil
	}
	isLive := func(h string) bool {
		hs, ok := state.Hypotheses[h]
		return ok && (hs.Status == HypothesisProposed || hs.Status == HypothesisOpen)
	}
	for id, p := range state.Predictions {
		if p.Status != PredictionUntested {
			continue
		}
		switch {
		case p.HypothesisRef != ref && isLive(p.HypothesisRef) && containsString(p.Discriminates, ref):
			return fmt.Errorf("RecordInterpretation rejected: hypothesis %s cannot be recorded %s — rival %s still holds untested prediction %s that discriminates against it; test it, or the analyst withdraws it", ref, to, p.HypothesisRef, id)
		case p.HypothesisRef == ref:
			for _, rival := range p.Discriminates {
				if isLive(rival) {
					return fmt.Errorf("RecordInterpretation rejected: hypothesis %s cannot be recorded %s — its untested prediction %s discriminates against live rival %s; test it, or the analyst withdraws it", ref, to, id, rival)
				}
			}
		}
	}
	return nil
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func aiReasoningGate(env Envelope, c RecordInterpretation, hypStatus string) error {
	if hypStatus != HypothesisProposed || !env.Actor.IsAIDelegated() {
		return nil
	}
	if c.AIReasoningConfigRef == "" {
		return errors.New("RecordInterpretation rejected: an AI delegate cannot record an outcome on an unacknowledged (PROPOSED) hypothesis — a human must Acknowledge it first, or enable the tenant dial trust.ai_reasoning")
	}
	return nil
}

func applyHypothesisOutcome(env Envelope, state aggregateState, ref, to string, c RecordInterpretation, rec *InterpretationRecorded) error {
	hs, ok := state.Hypotheses[ref]
	if !ok {
		return fmt.Errorf("RecordInterpretation rejected: hypothesis %s does not exist", ref)
	}
	// Only a live hypothesis (PROPOSED or OPEN) takes an evidential outcome;
	// terminal statuses are terminal in v0 — a changed judgment is a new
	// hypothesis chained via parent_ref, keeping every reversal auditable.
	if hs.Status != HypothesisProposed && hs.Status != HypothesisOpen {
		return fmt.Errorf("RecordInterpretation rejected: hypothesis %s is %s (terminal), cannot move to %s", ref, hs.Status, to)
	}
	if err := aiReasoningGate(env, c, hs.Status); err != nil {
		return err
	}
	if err := outstandingDiscriminatorGate(env, state, ref, to); err != nil {
		return err
	}
	rec.HypothesisTransition = &HypothesisTransitionRecorded{Ref: ref, From: hs.Status, To: to}
	rec.OutputRefs = append(rec.OutputRefs, ref)
	return nil
}

func applyPredictionCreate(state aggregateState, p PredictionNode, rec *InterpretationRecorded) error {
	stixID := PredictionSTIXID(p.ID)
	if _, exists := state.Predictions[stixID]; exists {
		return fmt.Errorf("RecordInterpretation rejected: prediction %s already exists", stixID)
	}
	hs, ok := state.Hypotheses[p.HypothesisRef]
	if !ok {
		return fmt.Errorf("RecordInterpretation rejected: hypothesis %s does not exist", p.HypothesisRef)
	}
	// Predictions exist to test undecided hypotheses.
	if hs.Status != HypothesisProposed && hs.Status != HypothesisOpen {
		return fmt.Errorf("RecordInterpretation rejected: hypothesis %s is %s, predictions test PROPOSED/OPEN hypotheses", p.HypothesisRef, hs.Status)
	}
	// A discriminated rival must be a real hypothesis in this investigation.
	for _, rival := range p.Discriminates {
		if _, ok := state.Hypotheses[rival]; !ok {
			return fmt.Errorf("RecordInterpretation rejected: discriminated hypothesis %s does not exist", rival)
		}
	}
	rec.Prediction = &PredictionRecorded{
		ID:            stixID,
		HypothesisRef: p.HypothesisRef,
		Statement:     p.Statement,
		Status:        PredictionUntested,
		TestQuery:     p.TestQuery,
		Discriminates: p.Discriminates,
	}
	if p.TestKind == TestKindManual {
		rec.Prediction.TestKind = TestKindManual
	}
	rec.OutputRefs = append(rec.OutputRefs, stixID)
	return nil
}

func applyPredictionOutcome(env Envelope, state aggregateState, c RecordInterpretation, rec *InterpretationRecorded) error {
	ps, ok := state.Predictions[c.PredictionRef]
	if !ok {
		return fmt.Errorf("RecordInterpretation rejected: prediction %s does not exist", c.PredictionRef)
	}
	if ps.Status != PredictionUntested {
		return fmt.Errorf("RecordInterpretation rejected: prediction %s is %s, only UNTESTED takes an outcome", c.PredictionRef, ps.Status)
	}
	if c.PredictionStatus == PredictionWithdrawn {
		// Withdrawing is the analyst discarding a test, not an evidential act:
		// human-only, and outside the reasoning gate (nothing was decided).
		if env.Actor.IsAIDelegated() {
			return errors.New("RecordInterpretation rejected: withdrawing a prediction is a human act")
		}
	} else {
		// Two human-only guards beside withdraw (adr/0054). What a person was
		// told is recorded by the analyst who was told it, never by the
		// delegate — the one statement the model could most easily invent is
		// kept out of its reach entirely. And a prediction a person tests has
		// no outcome the delegate can observe: it may ask for the result, not
		// supply it.
		if c.Attestation != nil && env.Actor.IsAIDelegated() {
			return errors.New("RecordInterpretation rejected: an attestation is a human act — the analyst records what a person said")
		}
		if ps.Manual && env.Actor.IsAIDelegated() {
			return fmt.Errorf("RecordInterpretation rejected: prediction %s is tested by a person, not a tool — the analyst records its result (or withdraws it)", c.PredictionRef)
		}
		// Running a test is an evidential outcome — gated on the parent
		// hypothesis being human-acknowledged when the actor is an AI delegate.
		if err := aiReasoningGate(env, c, state.Hypotheses[ps.HypothesisRef].Status); err != nil {
			return err
		}
	}
	refs := c.TestResultRefs
	if a := c.Attestation; a != nil {
		// The attestation becomes a note the outcome cites, first among its
		// evidence, so the evidence rule reads the same for every outcome.
		noteID := NoteSTIXID(a.ID)
		rec.Attestation = &AttestationRecorded{
			ID: noteID, PredictionRef: c.PredictionRef, HypothesisRef: ps.HypothesisRef,
			Author: env.Actor.PrincipalID, AuthorName: env.Actor.PrincipalName, Content: strings.TrimSpace(a.Content),
			Source: strings.TrimSpace(a.Source), Channel: strings.TrimSpace(a.Channel), ObservedAt: a.ObservedAt,
		}
		refs = append([]string{noteID}, refs...)
		rec.OutputRefs = append(rec.OutputRefs, noteID)
	}
	rec.PredictionTransition = &PredictionTransitionRecorded{
		Ref:            c.PredictionRef,
		From:           ps.Status,
		To:             c.PredictionStatus,
		TestResultRefs: refs,
	}
	rec.OutputRefs = append(rec.OutputRefs, c.PredictionRef)
	return nil
}
