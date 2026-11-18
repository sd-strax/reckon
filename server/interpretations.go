package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
)

// maxInterpretationBodyBytes bounds the /api/interpretations request body
// (transcript + tool calls for ONE turn). 10MB is far beyond any real turn;
// the cap exists so a runaway client can't exhaust memory or the side store.
const maxInterpretationBodyBytes = 10 << 20

// artifactLocation is the aggregate's record of where an attestation's
// artifact is kept; nil when the body names none.
func artifactLocation(a *ArtifactBody) *aggregate.ArtifactLocation {
	if a == nil {
		return nil
	}
	return &aggregate.ArtifactLocation{System: a.System, Location: a.Location, SHA256: a.SHA256, Access: a.Access}
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
	// (04 §5.6): otherwise an AI-authored interpretation could
	// masquerade as human-authored, or vice versa.
	actor := actorFromRequest(r, claims)

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
	cmd.ConsultedSOPs = body.ConsultedSOPs
	cmd.ConsultedSimilar = body.ConsultedSimilar
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
			Title:         body.Prediction.Title,
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
			Artifact:   artifactLocation(body.Attestation.Artifact),
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
			CallID:           tc.CallID,
			ToolName:         tc.ToolName,
			Args:             tc.Args,
			ResultHash:       tc.ResultHash,
			CapabilityCallID: tc.CapabilityCallID,
		})
	}

	env := newEnvelope(investigationID, actor, commandNow())
	res, err := b.cfg.Handler.Handle(r.Context(), env, cmd)
	if err != nil {
		if b.writeEventCitedError(r.Context(), w, "record interpretation", err) {
			return
		}
		writeCommandError(w, "record interpretation", err)
		return
	}
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
func (b *Backend) listInvestigationHypotheses(w http.ResponseWriter, r *http.Request, invID uuid.UUID) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}

	hs, err := b.cfg.Reads.ListHypotheses(r.Context(), invID)
	if err != nil {
		writeInternalError(w, "list hypotheses", err)
		return
	}
	ps, err := b.cfg.Reads.ListPredictions(r.Context(), invID)
	if err != nil {
		writeInternalError(w, "list predictions", err)
		return
	}

	byHypothesis := make(map[string][]PredictionView, len(ps))
	for i, p := range ps {
		byHypothesis[p.HypothesisRef] = append(byHypothesis[p.HypothesisRef], PredictionView{
			ID:             p.ID,
			Statement:      p.Statement,
			Title:          p.Title,
			TitleDerived:   p.TitleDerived,
			Status:         aggregate.PredictionStatus(p.Status),
			Ordinal:        i + 1, // ListPredictions is in record order
			TestQuery:      p.TestQuery,
			Discriminates:  p.Discriminates,
			TestResultRefs: p.TestResultRefs,
			TestKind:       aggregate.TestKind(p.TestKind),
			Attestation:    attestationView(p.Attestation),
		})
	}
	out := make([]HypothesisView, 0, len(hs))
	for _, h := range hs {
		out = append(out, HypothesisView{
			ID:          h.ID,
			Statement:   h.Statement,
			Handle:      h.Handle,
			Status:      aggregate.HypothesisStatus(h.Status),
			ParentRef:   h.ParentRef,
			RootedAtRef: h.RootedAtRef,
			Labels:      h.Labels,
			Predictions: byHypothesis[h.ID],
		})
	}
	writeJSON(w, http.StatusOK, HypothesesResponse{Hypotheses: out})
}
