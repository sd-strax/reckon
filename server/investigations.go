package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
)

// InvestigationView is the JSON shape returned to clients.
type InvestigationView struct {
	AggregateID       string `json:"aggregate_id"`
	Title             string `json:"title"`
	Status            string `json:"status"`
	LastEventSequence int64  `json:"last_event_sequence"`

	// Verdict is the disposition of record (01 §Verdict), absent when none —
	// the honest zero: no stored "pending".
	Verdict *VerdictView `json:"verdict,omitempty"`

	// Seed is the investigation's root (01 §Extension 1); absent on pre-seed
	// investigations. SeedSummary is the triage-queue display line.
	Seed        *SeedBody `json:"seed,omitempty"`
	SeedSummary string    `json:"seed_summary,omitempty"`

	// Triage cues (ui/06 §Tree view — "what needs me, now?"), list rows only:
	// actions still awaiting a human decision, and the soonest approval-window
	// deadline among them (the countdown outranks everything else).
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
	PendingActions int        `json:"pending_actions,omitempty"`
	NearestExpiry  *time.Time `json:"nearest_expiry,omitempty"`
	// Stage inputs for the work queue (design/ui 08 §8.3), list rows only, so
	// a row's stage step derives without fetching every investigation: whether
	// a verdict act exists, the non-notify actions not yet terminal, and the
	// notify.* actions not yet terminal.
	HasVerdict        bool `json:"has_verdict,omitempty"`
	OpenActions       int  `json:"open_actions,omitempty"`
	OpenNotifications int  `json:"open_notifications,omitempty"`

	// CreatedAt and OpenedByName are the investigation's start (the
	// investigation-time divider of the Story lens) and who opened it — the
	// creating act's actor by display name (adr/0077). Detail responses.
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	OpenedByName string     `json:"opened_by_name,omitempty"`

	// HypothesisFraming is the tenant's competing-hypotheses dial
	// (config.Reasoning.HypothesisFraming; 01 §x-hypothesis): "leading_null"
	// (default) or "leading_only". Detail responses only — the agent session
	// renders its framing contract from it.
	HypothesisFraming string `json:"hypothesis_framing,omitempty"`

	// CaseNoteOnSeed is the tenant's case write-back posture
	// (config.Cases.NoteOnSeed; adr/0032): "confirm" | "auto" | "off". CaseNote
	// is the offer itself — the default note text for the seed case — present
	// on the create AND detail responses of a case-rooted investigation when
	// the posture is not off. The client offers it on demand (confirm) or
	// posts it at creation (auto) via POST /investigations/{id}/case_note.
	CaseNoteOnSeed string         `json:"case_note_on_seed,omitempty"`
	CaseNote       *CaseNoteOffer `json:"case_note,omitempty"`
}

func seedBody(s *aggregate.Seed) *SeedBody {
	if s == nil {
		return nil
	}
	return &SeedBody{
		Type: s.Type, AlertID: s.AlertID, Source: s.Source,
		DetectionFindingRef: s.DetectionFindingRef, EntityRef: s.EntityRef,
		EntityIdentifier: s.EntityIdentifier, HypothesisStatement: s.HypothesisStatement,
		CaseID: s.CaseID, CaseRef: s.CaseRef,
	}
}

// VerdictView is the verdict fold as served.
type VerdictView struct {
	Disposition      string    `json:"disposition"`
	Rationale        string    `json:"rationale,omitempty"`
	VerdictAt        time.Time `json:"verdict_at"`
	InterpretationID string    `json:"interpretation_id,omitempty"`
}

// SeedBody is the client shape of the investigation's root (01 §Extension 1).
type SeedBody struct {
	Type                string `json:"type"` // alert | entity | question | case
	AlertID             string `json:"alert_id,omitempty"`
	Source              string `json:"source,omitempty"`
	DetectionFindingRef string `json:"detection_finding_ref,omitempty"`
	EntityRef           string `json:"entity_ref,omitempty"`
	EntityIdentifier    string `json:"entity_identifier,omitempty"`
	HypothesisStatement string `json:"hypothesis_statement,omitempty"`
	CaseID              string `json:"case_id,omitempty"`
	CaseRef             string `json:"case_ref,omitempty"`
}

// SeedInputBody is the raw analyst-typed seed (design/ui/02 §2.7): the value the
// analyst rooted on plus their confirmed kind. The server resolves it to a full
// seed — minting the STIX id for an entity, or reading the case for a case seed
// (14-case-seed.md §3) — so the workbench never handles ids. Kind is
// "entity" | "question" | "case" (empty = infer entity/question); alerts carry
// an explicit Seed. SourceScope applies to a case seed's read (14 §3).
type SeedInputBody struct {
	Value       string `json:"value"`
	Kind        string `json:"kind,omitempty"`
	SourceScope string `json:"source_scope,omitempty"`
	// AllowUnknown starts the investigation even though the tenant's host
	// inventory has never seen the entity — the analyst knows better (#159).
	AllowUnknown bool `json:"allow_unknown,omitempty"`
}

// CreateInvestigationRequest is the body of POST /api/investigations. Exactly
// one of Seed (a fully-formed root, e.g. an alert) or SeedInput (a raw typed
// value the server resolves) is supplied by the product; both absent is a
// pre-seed investigation. Title may be empty when a seed is present — it is
// derived from the seed's display line.
type CreateInvestigationRequest struct {
	Title     string         `json:"title"`
	Seed      *SeedBody      `json:"seed,omitempty"`
	SeedInput *SeedInputBody `json:"seed_input,omitempty"`
}

// CreateInvestigationResponse is the body of a successful POST.
type CreateInvestigationResponse struct {
	InvestigationView
	NewSequenceNo int64 `json:"new_sequence_no"`
}

// listInvestigations renders all rows of investigation_current.
// Requires viewer or analyst or auditor (enforced by caller).
func (b *Backend) listInvestigations(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	rows, err := b.cfg.Handler.Reads().ListInvestigations(r.Context())
	if err != nil {
		writeInternalError(w, "list investigations", err)
		return
	}
	var out []InvestigationView
	for i := range rows {
		row := &rows[i]
		// The seed rides list rows too (the queue shows the case reference).
		out = append(out, InvestigationView{
			AggregateID:       row.AggregateID.String(),
			Title:             row.Title,
			Status:            row.Status,
			LastEventSequence: row.LastEventSequence,
			SeedSummary:       row.SeedSummary,
			Seed:              seedBody(row.Seed),
			UpdatedAt:         &row.UpdatedAt,
			PendingActions:    row.PendingActions,
			NearestExpiry:     row.NearestExpiry,
			HasVerdict:        row.HasVerdict,
			OpenActions:       row.OpenActions,
			OpenNotifications: row.OpenNotifications,
		})
	}
	if out == nil {
		out = []InvestigationView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"investigations": out})
}

// getInvestigation loads a single investigation by aggregate_id.
func (b *Backend) getInvestigation(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	id, ok := investigationIDFromPath(r.URL.Path)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid investigation id in path")
		return
	}
	ic, err := b.cfg.Handler.Reads().LoadInvestigationCurrent(r.Context(), id)
	if errors.Is(err, aggregate.ErrNoRecord) {
		writeJSONError(w, http.StatusNotFound, "investigation not found")
		return
	}
	if err != nil {
		writeInternalError(w, "load investigation", err)
		return
	}
	view := InvestigationView{
		AggregateID:       ic.AggregateID.String(),
		Title:             ic.Title,
		Status:            ic.Status,
		LastEventSequence: ic.LastEventSequence,
		Seed:              seedBody(ic.Seed),
		SeedSummary:       ic.SeedSummary,
		HypothesisFraming: b.cfg.HypothesisFraming,
		CaseNoteOnSeed:    b.cfg.CaseNoteOnSeed,
	}
	// The case-note offer rides the detail view too (adr/0032 as amended):
	// the note is on demand from the document, not a creation-time choice, so
	// any load must be able to render the button with the default text.
	if claims, ok := authz.FromContext(r.Context()); ok {
		view.CaseNote = b.caseNoteOffer(r.Context(), view.AggregateID, ic.Title, ic.Seed, claims.PreferredUsername, commandNow())
	} else {
		view.CaseNote = b.caseNoteOffer(r.Context(), view.AggregateID, ic.Title, ic.Seed, "", commandNow())
	}
	if ic.VerdictDisposition != "" {
		view.Verdict = &VerdictView{
			Disposition:      ic.VerdictDisposition,
			Rationale:        ic.VerdictRationale,
			VerdictAt:        ic.VerdictAt.Time,
			InterpretationID: ic.VerdictInterpretationID.String(),
		}
	}
	// The investigation's start and who opened it: the creating act (sequence
	// 1) and its actor's display name (adr/0077).
	if at, actor, err := b.cfg.Handler.Reads().InvestigationOpening(r.Context(), id); err == nil {
		view.CreatedAt = &at
		view.OpenedByName = actor.PrincipalName
		if view.OpenedByName == "" {
			view.OpenedByName = actor.PrincipalID
		}
	}
	writeJSON(w, http.StatusOK, view)
}

// createInvestigation dispatches a CreateInvestigation command via the
// aggregate Handler. Returns 201 on success with the projection's view of
// the new aggregate.
func (b *Backend) createInvestigation(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	claims, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth context missing")
		return
	}

	var req CreateInvestigationRequest
	if !decodeBody(w, r, &req, maxBodyBytes) {
		return
	}

	cmd := aggregate.CreateInvestigation{Title: strings.TrimSpace(req.Title)}
	// The workbench's seed surface (design/ui/02 §2.7) sends a raw typed value;
	// the server resolves it to a full seed here, minting the STIX id for an
	// entity so the analyst never handles ids. A fully-formed Seed (alerts)
	// bypasses resolution.
	switch {
	case req.SeedInput != nil && req.SeedInput.Kind == aggregate.SeedCase && req.Seed == nil:
		// A case seed reads the SoR through the capability layer and fails closed
		// on anything short of exactly one case (14 §2/§3) — not the pure
		// identity resolution the entity/question path does.
		resolved, status, err := b.resolveCaseSeed(r, req.SeedInput.Value, req.SeedInput.SourceScope)
		if err != nil {
			writeJSONError(w, status, "seed from case: "+err.Error())
			return
		}
		seed := resolved.Seed
		// One investigation per case (#158): a case already investigated —
		// open or concluded — opens that investigation; a concluded one is
		// reopened, not started again.
		if existing, err := b.investigationForCase(r, seed.CaseID); err != nil {
			writeInternalError(w, "look up investigations", err)
			return
		} else if existing != "" {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":            "case " + seed.CaseID + " already has an investigation",
				"investigation_id": existing,
			})
			return
		}
		cmd.Seed = &seed
		if cmd.Title == "" {
			cmd.Title = deriveSeedTitle(resolved.Display)
		}
	case req.SeedInput != nil && req.Seed == nil:
		resolved, err := resolveSeedInput(b.identityResolver(), req.SeedInput.Value, req.SeedInput.Kind)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "resolve seed: "+err.Error())
			return
		}
		// Report an entity nothing knows about rather than root on it (#159).
		if resolved.Seed.Type == aggregate.SeedEntity && !req.SeedInput.AllowUnknown {
			if unknown, msg := b.unknownHost(r, resolved); unknown {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": msg, "unknown_entity": true})
				return
			}
		}
		seed := resolved.Seed
		cmd.Seed = &seed
		if cmd.Title == "" {
			cmd.Title = deriveSeedTitle(resolved.Display)
		}
	case req.Seed != nil:
		cmd.Seed = &aggregate.Seed{
			Type: req.Seed.Type, AlertID: req.Seed.AlertID, Source: req.Seed.Source,
			DetectionFindingRef: req.Seed.DetectionFindingRef, EntityRef: req.Seed.EntityRef,
			EntityIdentifier: req.Seed.EntityIdentifier, HypothesisStatement: req.Seed.HypothesisStatement,
			CaseID: req.Seed.CaseID, CaseRef: req.Seed.CaseRef,
		}
		if cmd.Title == "" {
			cmd.Title = deriveSeedTitle(cmd.Seed.Summary())
		}
	}
	if cmd.Title == "" {
		writeJSONError(w, http.StatusBadRequest, "title is required (or supply a seed to derive it from)")
		return
	}

	// Actor derived from the claims like every other write path — a delegate
	// token creating an investigation must be recorded AI_DELEGATED, never
	// silently attributed as a bare human act.
	env := newEnvelope(uuid.New(), actorFromClaims(claims), commandNow())
	res, err := b.cfg.Handler.Handle(r.Context(), env, cmd)
	if err != nil {
		writeCommandError(w, "create investigation", err)
		return
	}
	writeJSON(w, http.StatusCreated, CreateInvestigationResponse{
		InvestigationView: InvestigationView{
			AggregateID:       res.AggregateID.String(),
			Title:             cmd.Title,
			Status:            aggregate.StatusDraft,
			LastEventSequence: res.NewSequenceNo,
			Seed:              seedBody(cmd.Seed),
			SeedSummary:       seedSummaryOf(cmd.Seed),
			CaseNoteOnSeed:    b.cfg.CaseNoteOnSeed,
			// The offer rides the create response (adr/0032): the client either
			// asks the analyst (confirm) or posts it (auto); nil when the seed
			// is not a case or the posture is off.
			CaseNote: b.caseNoteOffer(r.Context(), res.AggregateID.String(), cmd.Title, cmd.Seed, claims.PreferredUsername, env.OccurredAt),
		},
		NewSequenceNo: res.NewSequenceNo,
	})
}

// investigationIDFromPath parses the UUID out of `/investigations/{id}`.
// The /api prefix has already been stripped by the parent router.
func investigationIDFromPath(p string) (uuid.UUID, bool) {
	const prefix = "/investigations/"
	if !strings.HasPrefix(p, prefix) {
		return uuid.UUID{}, false
	}
	tail := strings.TrimPrefix(p, prefix)
	tail = strings.TrimSuffix(tail, "/")
	if strings.Contains(tail, "/") {
		// nested paths not supported yet
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(tail)
	if err != nil {
		return uuid.UUID{}, false
	}
	return id, true
}
