package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
)

// maxInterpretationBodyBytes bounds the /api/interpretations request body
// (transcript + tool calls for ONE turn). 10MB is far beyond any real turn;
// the cap exists so a runaway client can't exhaust memory or the side store.
const maxInterpretationBodyBytes = 10 << 20

// RecordInterpretationBody is the /api/interpretations request: one reasoning
// act the agent loop (or analyst) is committing to the thread (05 §3.4 — the
// extension posts the final Interpretation command plus the transcript bytes;
// the backend hashes the bytes, writes the side store, and appends the event in
// one transaction).
type RecordInterpretationBody struct {
	InvestigationRef   string           `json:"investigation_ref"`
	InterpretationType string           `json:"interpretation_type"`
	InputRefs          []string         `json:"input_refs,omitempty"`
	OutputRefs         []string         `json:"output_refs,omitempty"`
	Rationale          string           `json:"rationale"`
	Confidence         string           `json:"confidence,omitempty"`
	Transcript         *TranscriptInput `json:"transcript,omitempty"`
	ToolCalls          []ToolCallInput  `json:"tool_calls,omitempty"`

	// Reasoning-node payloads (D.2) — hypotheses/predictions are outputs of
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
	TestResultRefs   []string        `json:"test_result_refs,omitempty"`
	// Attestation is the analyst's own observation as the outcome's evidence
	// (adr/0054): what a person said, or what was checked by hand. The note
	// id is minted server-side; the aggregate refuses it from an AI delegate.
	Attestation *AttestationBody `json:"attestation,omitempty"`

	// Verdict payload for type=verdict (01 §Verdict): the disposition of
	// record. The AI-verdict dial is applied SERVER-side from tenant config —
	// there is deliberately no client field for it.
	Verdict *VerdictBody `json:"verdict,omitempty"`

	// ConsultedSOPs is the turn's knowledge-retrieval provenance (01 schema):
	// what retrieval surfaced, and what the act actually built on.
	ConsultedSOPs []ConsultedSOPBody `json:"consulted_sops,omitempty"`

	// ConsultedSimilar is the case-knowledge counterpart (06 §6).
	ConsultedSimilar []ConsultedSimilarBody `json:"consulted_similar_investigations,omitempty"`
}

// ConsultedSOPBody mirrors aggregate.ConsultedSOP on the wire.
type ConsultedSOPBody struct {
	SOPID          string  `json:"sop_id"`
	Title          string  `json:"title,omitempty"`
	RetrievalScore float64 `json:"retrieval_score,omitempty"`
	Used           bool    `json:"used"`
}

// ConsultedSimilarBody mirrors aggregate.ConsultedSimilarInvestigation.
type ConsultedSimilarBody struct {
	InvestigationRef string  `json:"investigation_ref"`
	Title            string  `json:"title,omitempty"`
	RetrievalScore   float64 `json:"retrieval_score,omitempty"`
	Band             string  `json:"band,omitempty"`
	Used             bool    `json:"used"`
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
// (01 §x-prediction).
type PredictionBody struct {
	HypothesisRef string               `json:"hypothesis_ref"`
	Statement     string               `json:"statement"`
	TestQuery     *aggregate.QuerySpec `json:"test_query,omitempty"`
	Discriminates []string             `json:"discriminates,omitempty"`
	TestKind      string               `json:"test_kind,omitempty"` // tool (default) | manual — a person tests it (adr/0054)
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

func (a *ArtifactBody) location() *aggregate.ArtifactLocation {
	if a == nil {
		return nil
	}
	return &aggregate.ArtifactLocation{System: a.System, Location: a.Location, SHA256: a.SHA256, Access: a.Access}
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
}

// RecordInterpretationResponse reports the appended interpretation. NodeID is
// the STIX id of the hypothesis/prediction this act created, when it created
// one — the agent references it in later transitions.
type RecordInterpretationResponse struct {
	InterpretationID string `json:"interpretation_id"`
	SequenceNo       int64  `json:"sequence_no"`
	NodeID           string `json:"node_id,omitempty"`
}

// interpretationsCollection routes /api/interpretations. POST records a
// reasoning act; requires the analyst role — the AI authors interpretations on
// an analyst's behalf, never as a principal (the delegate is stamped from the
// JWT, the principal is the analyst).
func (b *Backend) interpretationsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, b.recordInterpretation)
	default:
		methodNotAllowed(w, "POST")
	}
}

// recordInterpretation handles POST /api/interpretations: the agent loop's write
// into the reasoning thread (the legal-author seam, 03 §1). It mints the
// interpretation id, derives Actor.Kind from the JWT delegate_kind claim (never
// the body), and appends the InterpretationRecorded event together with its
// transcript/tool-call side store in one transaction.
func (b *Backend) recordInterpretation(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate not configured")
		return
	}
	claims, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth context missing")
		return
	}

	// This route carries transcript bytes by design, so its bound is the
	// larger one.
	var body RecordInterpretationBody
	if !decodeBody(w, r, &body, maxInterpretationBodyBytes) {
		return
	}
	investigationID, err := uuid.Parse(body.InvestigationRef)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "investigation_ref is not a valid id")
		return
	}

	// Actor.Kind comes from the JWT delegate_kind claim — NEVER the request body
	// (04 §5.6 seam obligation): otherwise an AI-authored interpretation could
	// masquerade as human-authored, or vice versa.
	actor := actorFromClaims(claims)

	cmd := aggregate.RecordInterpretation{
		InterpretationID:   uuid.New(),
		InterpretationType: body.InterpretationType,
		InputRefs:          body.InputRefs,
		OutputRefs:         body.OutputRefs,
		Rationale:          body.Rationale,
		Confidence:         body.Confidence,
		HypothesisRef:      body.HypothesisRef,
		Restatement:        body.Restatement,
		Handle:             body.Handle,
		Abandoned:          body.Abandoned,
		PredictionRef:      body.PredictionRef,
		PredictionStatus:   body.PredictionStatus,
		TestResultRefs:     body.TestResultRefs,
	}
	if body.Verdict != nil {
		cmd.Verdict = &aggregate.VerdictNode{Disposition: body.Verdict.Disposition}
	}
	for _, cs := range body.ConsultedSOPs {
		cmd.ConsultedSOPs = append(cmd.ConsultedSOPs, aggregate.ConsultedSOP{
			SOPID: cs.SOPID, Title: cs.Title, RetrievalScore: cs.RetrievalScore, Used: cs.Used,
		})
	}
	for _, cs := range body.ConsultedSimilar {
		cmd.ConsultedSimilar = append(cmd.ConsultedSimilar, aggregate.ConsultedSimilarInvestigation{
			InvestigationRef: cs.InvestigationRef, Title: cs.Title, RetrievalScore: cs.RetrievalScore, Band: cs.Band, Used: cs.Used,
		})
	}
	// The AI-verdict dial (01 §Verdict): an AI-delegated verdict is refused
	// unless tenant config enables it; when enabled, the enabling config ref is
	// stamped onto the command — the aggregate rejects an AI verdict without
	// the stamp (structural default-deny), and the ref rides the audit event.
	if body.InterpretationType == aggregate.InterpretationVerdict && actor.IsAIDelegated() {
		if !b.cfg.AllowAIVerdict {
			writeJSONError(w, http.StatusForbidden,
				"AI-delegated verdicts are denied by default: the tenant trust dial (trust.ai_verdict) is not enabled — record the verdict as the analyst, or enable the dial")
			return
		}
		cmd.AIVerdictConfigRef = "trust.ai_verdict"
	}
	// The autonomous-reasoning dial (01 §Interpretation types): when the tenant
	// enables it, stamp the enabling ref so the aggregate permits an AI delegate
	// to record outcomes on a still-PROPOSED hypothesis. Absent the stamp the
	// aggregate blocks that specific case only (an OPEN hypothesis is unaffected),
	// so no 403 here — the state-dependent gate lives at the aggregate boundary.
	if actor.IsAIDelegated() && b.cfg.AllowAIReasoning {
		cmd.AIReasoningConfigRef = "trust.ai_reasoning"
	}

	// Node creations: mint the id server-side and report it back as node_id.
	var nodeID string
	if body.Hypothesis != nil {
		id := uuid.New()
		nodeID = aggregate.HypothesisSTIXID(id)
		cmd.Hypothesis = &aggregate.HypothesisNode{
			ID:          id,
			Statement:   body.Hypothesis.Statement,
			Handle:      body.Hypothesis.Handle,
			ParentRef:   body.Hypothesis.ParentRef,
			RootedAtRef: body.Hypothesis.RootedAtRef,
			Labels:      body.Hypothesis.Labels,
		}
	}
	if body.Prediction != nil {
		id := uuid.New()
		nodeID = aggregate.PredictionSTIXID(id)
		cmd.Prediction = &aggregate.PredictionNode{
			ID:            id,
			HypothesisRef: body.Prediction.HypothesisRef,
			Statement:     body.Prediction.Statement,
			TestQuery:     body.Prediction.TestQuery,
			Discriminates: body.Prediction.Discriminates,
			TestKind:      body.Prediction.TestKind,
		}
	}
	if body.Attestation != nil {
		// The note id is minted here like every node id; the aggregate
		// materializes the note and cites it (adr/0054).
		cmd.Attestation = &aggregate.Attestation{
			ID:         uuid.New(),
			Content:    body.Attestation.Content,
			Source:     body.Attestation.Source,
			Channel:    body.Attestation.Channel,
			ObservedAt: body.Attestation.ObservedAt,
			Reference:  attestationReference(body.Attestation),
			Artifact:   body.Attestation.Artifact.location(),
		}
	}
	if body.Transcript != nil {
		// transcript_id groups the turns of one conversation in the audit
		// record. A malformed id is REJECTED, never silently replaced — a
		// coerced fresh UUID would sever the turn from its conversation while
		// telling the client everything is fine.
		tID := uuid.New()
		if body.Transcript.TranscriptID != "" {
			parsed, perr := uuid.Parse(body.Transcript.TranscriptID)
			if perr != nil {
				writeJSONError(w, http.StatusBadRequest, "transcript.transcript_id is not a valid id")
				return
			}
			tID = parsed
		}
		cmd.Transcript = &aggregate.TranscriptContent{
			TranscriptID: tID,
			TurnID:       body.Transcript.TurnID,
			Body:         []byte(body.Transcript.Body),
		}
	}
	for _, tc := range body.ToolCalls {
		cmd.ToolCalls = append(cmd.ToolCalls, aggregate.ToolCallContent{
			CallID:     tc.CallID,
			ToolName:   tc.ToolName,
			Args:       tc.Args,
			ResultHash: tc.ResultHash,
		})
	}

	env := newEnvelope(investigationID, actor, commandNow())
	res, err := b.cfg.Handler.Handle(r.Context(), env, cmd)
	if err != nil {
		writeCommandError(w, "record interpretation", err)
		return
	}
	b.publishDeltas(res)

	if res.RecordedInterpretationID != (uuid.UUID{}) {
		// A retried turn commit: the turn is already recorded, under the id
		// its first attempt was given (adr/0231).
		writeJSON(w, http.StatusOK, RecordInterpretationResponse{
			InterpretationID: res.RecordedInterpretationID.String(),
			SequenceNo:       res.NewSequenceNo,
		})
		return
	}
	writeJSON(w, http.StatusCreated, RecordInterpretationResponse{
		InterpretationID: cmd.InterpretationID.String(),
		SequenceNo:       res.NewSequenceNo,
		NodeID:           nodeID,
	})
}

// HypothesisView is one hypothesis with its predictions nested — the shape the
// reasoning panel renders.
type HypothesisView struct {
	ID          string           `json:"id"`
	Statement   string           `json:"statement"`
	Handle      string           `json:"handle,omitempty"`
	Status      string           `json:"status"`
	ParentRef   string           `json:"parent_ref,omitempty"`
	RootedAtRef string           `json:"rooted_at_ref,omitempty"`
	Labels      []string         `json:"labels,omitempty"`
	Predictions []PredictionView `json:"predictions,omitempty"`
}

// PredictionView is one prediction row in a HypothesisView. TestQuery is the
// declared falsification test (01 §x-prediction) — the tracker's "Test this"
// stages it into the composer, never fires it. Discriminates names the rivals
// this prediction decides against.
type PredictionView struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
	Status    string `json:"status"`
	// Ordinal is the prediction's short name, P1, P2, … in the order
	// predictions were recorded across the investigation (adr/0047): what
	// the tracker, the composer, the agent's reasoning-state block and an
	// analyst's sentence all use to point at one. Display vocabulary, not
	// identity — the id stays the reference.
	Ordinal        int                  `json:"ordinal,omitempty"`
	TestQuery      *aggregate.QuerySpec `json:"test_query,omitempty"`
	Discriminates  []string             `json:"discriminates,omitempty"`
	TestResultRefs []string             `json:"test_result_refs,omitempty"`
	// TestKind is tool or manual (adr/0054): manual means a person's answer
	// decides it and the analyst records the result; the agent cannot.
	TestKind string `json:"test_kind,omitempty"`
	// Attestation is the human observation the outcome cites, when it has
	// one — rendered as "attested by … by phone: …", distinct from telemetry.
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

// attestationReference is the reference as recorded: as written, or only its
// SHA-256 when the analyst asked for that (#139).
func attestationReference(a *AttestationBody) string {
	ref := strings.TrimSpace(a.Reference)
	if ref == "" || !a.HashReference {
		return ref
	}
	sum := sha256.Sum256([]byte(ref))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func attestationView(a *aggregate.AttestationRecorded) *AttestationView {
	if a == nil {
		return nil
	}
	v := &AttestationView{NoteRef: a.ID, Author: a.Author, AuthorName: a.AuthorName, Content: a.Content, Source: a.Source, Channel: a.Channel, ObservedAt: a.ObservedAt, Reference: a.Reference}
	if x := a.Artifact; x != nil {
		v.Artifact = &ArtifactBody{System: x.System, Location: x.Location, SHA256: x.SHA256, Access: x.Access}
	}
	return v
}

// listInvestigationHypotheses serves GET /api/investigations/{id}/hypotheses:
// the investigation's reasoning nodes, predictions nested under the hypothesis
// they test.
func (b *Backend) listInvestigationHypotheses(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	invID, ok := investigationSubresourceID(r.URL.Path, "hypotheses")
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid investigation id in path")
		return
	}

	hs, err := aggregate.ListHypotheses(r.Context(), b.cfg.Handler.DB(), invID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list hypotheses: "+err.Error())
		return
	}
	ps, err := aggregate.ListPredictions(r.Context(), b.cfg.Handler.DB(), invID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list predictions: "+err.Error())
		return
	}

	byHypothesis := make(map[string][]PredictionView, len(ps))
	for i, p := range ps {
		byHypothesis[p.HypothesisRef] = append(byHypothesis[p.HypothesisRef], PredictionView{
			ID:             p.ID,
			Statement:      p.Statement,
			Status:         p.Status,
			Ordinal:        i + 1, // ListPredictions is in record order
			TestQuery:      p.TestQuery,
			Discriminates:  p.Discriminates,
			TestResultRefs: p.TestResultRefs,
			TestKind:       p.TestKind,
			Attestation:    attestationView(p.Attestation),
		})
	}
	out := make([]HypothesisView, 0, len(hs))
	for _, h := range hs {
		out = append(out, HypothesisView{
			ID:          h.ID,
			Statement:   h.Statement,
			Handle:      h.Handle,
			Status:      h.Status,
			ParentRef:   h.ParentRef,
			RootedAtRef: h.RootedAtRef,
			Labels:      h.Labels,
			Predictions: byHypothesis[h.ID],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"hypotheses": out})
}
