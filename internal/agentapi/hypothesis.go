package agentapi

import "time"

// reckon-hypothesis status values (01-domain-model.md §reckon-hypothesis). Status is a
// projection over interpretation events, never stored aggregate state: a
// hypothesis is created PROPOSED (AI-authored) or OPEN (human-authored), a
// human acknowledgment moves PROPOSED→OPEN, and the evidential outcomes
// (support / refutation / inconclusive interpretations) terminate it.
const (
	HypothesisProposed     = "PROPOSED"
	HypothesisOpen         = "OPEN"
	HypothesisSupported    = "SUPPORTED"
	HypothesisRefuted      = "REFUTED"
	HypothesisInconclusive = "INCONCLUSIVE"
	HypothesisAbandoned    = "ABANDONED"
)

// HypothesisStatus is a hypothesis's status as a served view carries it.
type HypothesisStatus string

// WireValues lists every hypothesis status, in the order declared above.
func (HypothesisStatus) WireValues() []string {
	return []string{
		HypothesisProposed, HypothesisOpen, HypothesisSupported,
		HypothesisRefuted, HypothesisInconclusive, HypothesisAbandoned,
	}
}

// reckon-prediction status values (01-domain-model.md §reckon-prediction). A
// prediction is created UNTESTED and terminates when its test outcome is
// recorded, or when a human WITHDRAWS it untested.
const (
	PredictionUntested     = "UNTESTED"
	PredictionConfirmed    = "CONFIRMED"
	PredictionDisconfirmed = "DISCONFIRMED"
	PredictionInconclusive = "INCONCLUSIVE"
	PredictionWithdrawn    = "WITHDRAWN"
)

// PredictionStatus is a prediction's status as a served view carries it.
type PredictionStatus string

// WireValues lists every prediction status, in the order declared above.
func (PredictionStatus) WireValues() []string {
	return []string{
		PredictionUntested, PredictionConfirmed, PredictionDisconfirmed,
		PredictionInconclusive, PredictionWithdrawn,
	}
}

// reckon-prediction test kinds (01 §reckon-prediction, adr/0054). A tool test
// is a query the agent or the analyst runs. A manual test is a person's
// answer, which no verb can fetch, so its outcome is a human act.
const (
	TestKindTool   = "tool"
	TestKindManual = "manual"
)

// TestKind is who decides a prediction, as a served view carries it.
type TestKind string

// WireValues lists every test kind, in the order declared above.
func (TestKind) WireValues() []string {
	return []string{TestKindTool, TestKindManual}
}

// HypothesisView is one hypothesis with its predictions nested: the shape the
// reasoning panel and the agent's reasoning-state block render.
type HypothesisView struct {
	ID          string           `json:"id"`
	Statement   string           `json:"statement"`
	Handle      string           `json:"handle,omitempty"`
	Status      HypothesisStatus `json:"status"`
	ParentRef   string           `json:"parent_ref,omitempty"`
	RootedAtRef string           `json:"rooted_at_ref,omitempty"`
	Labels      []string         `json:"labels,omitempty"`
	Predictions []PredictionView `json:"predictions,omitempty"`
}

// PredictionView is one prediction row in a HypothesisView. TestQuery is the
// declared falsification test (01 §reckon-prediction): the tracker's "Test this"
// stages it into the composer, never fires it. Discriminates names the rivals
// this prediction decides against.
type PredictionView struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
	// Title is the short name the record holds (adr/0090); TitleDerived says
	// the aggregate derived it from the statement. Surfaces show it as is.
	Title        string           `json:"title"`
	TitleDerived bool             `json:"title_derived,omitempty"`
	Status       PredictionStatus `json:"status"`
	// Ordinal is the prediction's short name, P1, P2, … in the order
	// predictions were recorded across the investigation (adr/0047): what
	// the tracker, the composer, the agent's reasoning-state block and an
	// analyst's sentence all use to point at one. Display vocabulary, not
	// identity: the id stays the reference.
	Ordinal        int        `json:"ordinal,omitempty"`
	TestQuery      *QuerySpec `json:"test_query,omitempty"`
	Discriminates  []string   `json:"discriminates,omitempty"`
	TestResultRefs []string   `json:"test_result_refs,omitempty"`
	// TestKind is tool or manual (adr/0054): manual means a person's answer
	// decides it and the analyst records the result; the agent cannot.
	TestKind TestKind `json:"test_kind,omitempty"`
	// Attestation is the human observation the outcome cites, when it has
	// one, rendered as "attested by … by phone: …", distinct from telemetry.
	Attestation *AttestationView `json:"attestation,omitempty"`
}

// AttestationView is the read shape of a recorded attestation (adr/0054).
type AttestationView struct {
	NoteRef    string        `json:"note_ref"`
	Author     string        `json:"author"`
	AuthorName string        `json:"author_name,omitempty"` // the principal's display name at the time, if the record has it
	Content    string        `json:"content"`
	Source     string        `json:"source,omitempty"`
	Channel    string        `json:"channel,omitempty"`
	ObservedAt *time.Time    `json:"observed_at,omitempty"`
	Reference  string        `json:"reference,omitempty"`
	Artifact   *ArtifactBody `json:"artifact,omitempty"`
}

// HypothesesResponse is GET /api/investigations/{id}/hypotheses.
type HypothesesResponse struct {
	Hypotheses []HypothesisView `json:"hypotheses"`
}
