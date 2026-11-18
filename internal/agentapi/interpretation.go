package agentapi

import (
	"encoding/json"
	"time"
)

// RecordInterpretationBody is POST /api/interpretations: the agent loop's
// write into the reasoning thread (the extension posts the final
// Interpretation command plus the transcript bytes; the backend hashes the
// bytes, writes the side store, and appends the event in one transaction).
//
// The fields the agent sends are declared in the order it has always sent
// them, so its request bytes do not change.
type RecordInterpretationBody struct {
	InvestigationRef   string           `json:"investigation_ref"`
	InterpretationType string           `json:"interpretation_type"`
	InputRefs          []string         `json:"input_refs,omitempty"`
	OutputRefs         []string         `json:"output_refs,omitempty"`
	Rationale          string           `json:"rationale"`
	Confidence         string           `json:"confidence,omitempty"`
	Transcript         *TranscriptInput `json:"transcript,omitempty"`
	ToolCalls          []ToolCallInput  `json:"tool_calls,omitempty"`

	// Reasoning-node payloads: hypotheses/predictions are outputs of
	// interpretations. The node ids are minted SERVER-side (returned in
	// node_id); transitions reference nodes by their STIX id.
	Hypothesis       *HypothesisBody `json:"hypothesis,omitempty"`     // type=hypothesis: create
	HypothesisRef    string          `json:"hypothesis_ref,omitempty"` // hypothesis ack|revise / support / refutation / inconclusive
	Restatement      string          `json:"restatement,omitempty"`    // ack|revise: the sharpened claim (human, untested-only; 01)
	Handle           string          `json:"handle,omitempty"`         // ack|revise: the (re)named mnemonic
	Abandoned        bool            `json:"abandoned,omitempty"`      // inconclusive → ABANDONED instead of INCONCLUSIVE
	Prediction       *PredictionBody `json:"prediction,omitempty"`     // type=prediction: create
	PredictionRef    string          `json:"prediction_ref,omitempty"` // prediction outcome
	PredictionStatus string          `json:"prediction_status,omitempty"`

	// ConsultedSOPs is the turn's knowledge-retrieval provenance (01 schema):
	// what retrieval surfaced, and what the act actually built on.
	ConsultedSOPs []ConsultedSOP `json:"consulted_sops,omitempty"`

	// ConsultedSimilar is the case-knowledge counterpart (06 §6).
	ConsultedSimilar []ConsultedSimilarInvestigation `json:"consulted_similar_investigations,omitempty"`

	TestResultRefs []string `json:"test_result_refs,omitempty"`
	// Attestation is the analyst's own observation as the outcome's evidence
	// (adr/0054): what a person said, or what was checked by hand. The note
	// id is minted server-side; the aggregate refuses it from an AI delegate.
	Attestation *AttestationBody `json:"attestation,omitempty"`

	// Verdict payload for type=verdict (01 §Verdict): the disposition of
	// record. The AI-verdict dial is applied SERVER-side from tenant config;
	// there is deliberately no client field for it.
	Verdict *VerdictBody `json:"verdict,omitempty"`
}

// VerdictBody is the client shape of a verdict act.
type VerdictBody struct {
	Disposition string `json:"disposition"` // BENIGN | SUSPICIOUS | MALICIOUS
}

// HypothesisBody is the client shape of a new hypothesis.
type HypothesisBody struct {
	Statement   string   `json:"statement"`
	Handle      string   `json:"handle,omitempty"`
	ParentRef   string   `json:"parent_ref,omitempty"`
	RootedAtRef string   `json:"rooted_at_ref,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

// PredictionBody is the client shape of a new prediction. Discriminates names
// the rival hypotheses whose expectation for it is the opposite of the owner's
// (01 §reckon-prediction).
type PredictionBody struct {
	HypothesisRef string     `json:"hypothesis_ref"`
	Statement     string     `json:"statement"`
	Title         string     `json:"title,omitempty"` // the short name; derived when absent (adr/0090)
	TestQuery     *QuerySpec `json:"test_query,omitempty"`
	Discriminates []string   `json:"discriminates,omitempty"`
	TestKind      string     `json:"test_kind,omitempty"` // tool (default) | manual: a person tests it (adr/0054)
}

// QuerySpec is a prediction's optional declared test (01 §reckon-prediction):
// what query against which tool would confirm or disconfirm it.
//
// It is also the stored event format: the prediction's event carries it as
// is, so a tag change changes how old events replay.
type QuerySpec struct {
	Tool       string         `json:"tool"`
	QueryText  string         `json:"query_text"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// AttestationBody is the client shape of a human attestation on a prediction
// outcome (adr/0054): the content is what was observed; source and channel
// say who was asked and how; observed_at defaults to the act's time.
type AttestationBody struct {
	Content    string     `json:"content"`
	Source     string     `json:"source,omitempty"`
	Channel    string     `json:"channel,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	// Reference says where it happened (#139): a message link, a ticket
	// number or a message id. HashReference stores only its SHA-256, for a
	// reference the analyst would not keep in the record as written.
	Reference     string `json:"reference,omitempty"`
	HashReference bool   `json:"hash_reference,omitempty"`
	// Artifact says where the evidence itself is kept (adr/0139): reckon
	// records the location, never the file.
	Artifact *ArtifactBody `json:"artifact,omitempty"`
}

// ArtifactBody is where an artifact backing an attestation is kept
// (aggregate.ArtifactLocation): the system, the location in it, its SHA-256,
// and how someone else gets access.
type ArtifactBody struct {
	System   string `json:"system,omitempty"`
	Location string `json:"location"`
	SHA256   string `json:"sha256,omitempty"`
	Access   string `json:"access,omitempty"`
}

// TranscriptInput carries the raw transcript for the turn. Body is the text as a
// string (LLM transcripts are text); it is hashed server-side into the content
// address stored on the event.
type TranscriptInput struct {
	TranscriptID string `json:"transcript_id,omitempty"`
	TurnID       string `json:"turn_id,omitempty"`
	Body         string `json:"body"`
}

// ToolCallInput is one tool dispatch the LLM made during the turn.
type ToolCallInput struct {
	CallID     string          `json:"call_id"`
	ToolName   string          `json:"tool_name"`
	Args       json.RawMessage `json:"args,omitempty"`
	ResultHash string          `json:"result_hash,omitempty"`
	// CapabilityCallID is the engine's id for the capability call the dispatch
	// made (adr/0092); call_id stays the model's tool_use id. Empty for every
	// other tool.
	CapabilityCallID string `json:"capability_call_id,omitempty"`
}

// RecordInterpretationResponse reports the appended interpretation. NodeID is
// the STIX id of the hypothesis/prediction this act created, when it created
// one: the agent references it in later transitions.
type RecordInterpretationResponse struct {
	InterpretationID string `json:"interpretation_id"`
	SequenceNo       int64  `json:"sequence_no"`
	NodeID           string `json:"node_id,omitempty"`
}

// TranscriptResponse is GET /api/interpretations/{id}/transcript: the
// committed turn record behind a thread step.
type TranscriptResponse struct {
	Body             string `json:"body"`
	ContentHash      string `json:"content_hash"`
	InterpretationID string `json:"interpretation_id"`
	TurnID           string `json:"turn_id"`
}
