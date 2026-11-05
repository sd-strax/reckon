package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
)

// RequestActionBody is the request_action tool call over HTTP (08 §2).
type RequestActionBody struct {
	ActionType       string                 `json:"action_type"`
	Targets          []aggregate.TargetSpec `json:"targets"`
	Parameters       json.RawMessage        `json:"parameters,omitempty"`
	EvidenceRefs     []string               `json:"evidence_refs,omitempty"`
	Rationale        string                 `json:"rationale"`
	InvestigationRef string                 `json:"investigation_ref"`
	// ReversalOfRef, when set, makes this request a reversal of that original
	// action (04 §7): action_type should be the inverse, and auto-approval
	// triggers the ReversalSaga instead of a plain dispatch.
	ReversalOfRef string `json:"reversal_of_ref,omitempty"`
	// RetryOf records lineage to the prior FAILED/EXPIRED action this request
	// replaces (design/ui binding §2.3).
	RetryOf string `json:"retry_of,omitempty"`
}

// RequestActionResponse reports the created x-action and how authorization
// resolved.
type RequestActionResponse struct {
	ActionID         string `json:"action_id"`
	Tier             string `json:"tier"`
	Status           string `json:"status"` // PENDING_MANUAL | PENDING_TWO_PARTY | APPROVED
	Mode             string `json:"mode"`
	MatchedPolicyRef string `json:"matched_policy_ref,omitempty"`
	WorkflowID       string `json:"workflow_id,omitempty"`
}

// ActionView is one row of GET /api/investigations/{id}/actions — the action
// review queue + audit list a surface renders to offer approvals. Status is the
// projection's raw lifecycle status (REQUESTED/PENDING_SECONDARY/APPROVED/…);
// required_mode says what approval the request still needs (MANUAL/TWO_PARTY).
type ActionView struct {
	ActionID     string `json:"action_id"`
	ActionType   string `json:"action_type"`
	Tier         string `json:"tier"`
	Status       string `json:"status"`
	RequiredMode string `json:"required_mode,omitempty"`
	// SecondaryApproverPool is who may complete a two-party approval, frozen
	// at request time by the policy that required it (04 §4.1); empty for a
	// manual action, and for a two-party one whose policy named no pool (the
	// on-call rotation then). The card tells the first approver who finishes
	// it (adr/0099).
	SecondaryApproverPool []string `json:"secondary_approver_pool,omitempty"`
	// PrimaryApproverRef is who gave the first approval, once one is in; a
	// two-party card awaiting its second approval leaves them out of the
	// candidates, since the aggregate requires two distinct humans.
	PrimaryApproverRef string                 `json:"primary_approver_ref,omitempty"`
	Mode               string                 `json:"mode,omitempty"`
	IsReversal         bool                   `json:"is_reversal,omitempty"`
	Targets            []aggregate.TargetSpec `json:"targets,omitempty"`
	EvidenceRefs       []string               `json:"evidence_refs,omitempty"`
	ExpiresAt          *time.Time             `json:"expires_at,omitempty"`
	// Parameters are the request's frozen parameters — the pre-send preview
	// for notify.* actions renders the message body from here (binding §4:
	// the preview IS the approval surface, so the approver must see exactly
	// what will be sent).
	Parameters json.RawMessage `json:"parameters,omitempty"`

	// Reversibility is the classification frozen at request time (04 §7):
	// REVERSIBLE | BEST_EFFORT | IRREVERSIBLE — the decision-grade card states
	// it honestly BEFORE approval (design/ui 03 §3.3).
	Reversibility string `json:"reversibility,omitempty"`
	// TierEscalated: the blast-radius escalator raised this above the type's
	// default tier (04 §1) — the card says why it is T3.
	TierEscalated bool `json:"tier_escalated,omitempty"`
	// EditedAt is when the request was last edited before approval (adr/0127).
	EditedAt *time.Time `json:"edited_at,omitempty"`
	// Inputs are the type's declared non-entity parameters — what an edit may
	// set — so a surface can offer the fields the request did not fill.
	Inputs []EditableInput `json:"inputs,omitempty"`
	// Name and Category come from the action catalog (#123): what a surface
	// titles and groups the action by, so no surface guesses from the id.
	Name     string `json:"name,omitempty"`
	Category string `json:"category,omitempty"`
	// RetryOf: lineage to the action this one replaces (zero when not a retry).
	RetryOf string `json:"retry_of,omitempty"`
	// RequestingInterpretationID: the act that requested it, so the thread's
	// request line can name the action (#153).
	RequestingInterpretationID string `json:"requesting_interpretation_id,omitempty"`
	// ReversalOfRef: the action this one reverses (zero when not a reversal),
	// so a surface can show an original's reversal on the original.
	ReversalOfRef string `json:"reversal_of_ref,omitempty"`
	// Adapter is the tool that dispatched the action (write-side provenance):
	// which of several possible tools actually acted. Empty until dispatched.
	Adapter string `json:"adapter,omitempty"`
	// PlannedAdapter is the tool the resolver WOULD dispatch to if approved now —
	// the pre-approval preview so the analyst sees which system of record an
	// external action will hit before committing. Set only while pending
	// (superseded by Adapter once dispatched).
	PlannedAdapter string `json:"planned_adapter,omitempty"`
	// ErrorDetail is the reason a FAILED action failed (08 §6c), so the ledger
	// explains the outcome instead of a bare FAILED. Empty on success.
	ErrorDetail string `json:"error_detail,omitempty"`
	// ResultRef is the operational reference the dispatch returned (e.g. the
	// created ServiceNow incident number) — the analyst's handle into the
	// external system of record.
	ResultRef string `json:"result_ref,omitempty"`
}

// listInvestigationActions serves GET /api/investigations/{id}/actions: every
// x-action of the investigation, oldest first. This is how a surface recovers
// the pending-approval queue after the turn that proposed an action is gone —
// the request-time response is a one-shot; this list is the durable view.
func (b *Backend) listInvestigationActions(w http.ResponseWriter, r *http.Request, invID uuid.UUID) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}

	acts, err := b.cfg.Reads.ListActionCurrents(r.Context(), invID)
	if err != nil {
		writeInternalError(w, "list actions", err)
		return
	}
	out := make([]ActionView, 0, len(acts))
	for _, a := range acts {
		out = append(out, b.actionView(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": out})
}

// EditableInput is one parameter an edit may set (adr/0127).
type EditableInput struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
}

// actionView maps one projected x-action to the list/record view: the frozen
// request facts plus the two derived facts (tier escalation against the
// catalog's default, the pre-approval planned adapter).
func (b *Backend) actionView(a aggregate.ActionCurrent) ActionView {
	v := ActionView{
		ActionID:              a.ActionID.String(),
		ActionType:            a.ActionType,
		Tier:                  a.Tier,
		Status:                a.Status,
		RequiredMode:          a.RequiredMode,
		SecondaryApproverPool: a.SecondaryApproverPool,
		PrimaryApproverRef:    a.PrimaryApprover,
		Mode:                  a.Mode,
		IsReversal:            a.IsReversal,
		Targets:               a.Targets,
		EvidenceRefs:          a.EvidenceRefs,
		Reversibility:         a.Reversibility,
		Parameters:            a.Parameters,
		Adapter:               a.Adapter,
		ErrorDetail:           a.ErrorDetail,
		ResultRef:             a.RawResponseRef,
	}
	if a.RetryOf != (uuid.UUID{}) {
		v.RetryOf = a.RetryOf.String()
	}
	if a.ReversalOfRef != (uuid.UUID{}) {
		v.ReversalOfRef = a.ReversalOfRef.String()
	}
	if a.RequestingInterpretationID != (uuid.UUID{}) {
		v.RequestingInterpretationID = a.RequestingInterpretationID.String()
	}
	if !a.EditedAt.IsZero() {
		t := a.EditedAt
		v.EditedAt = &t
	}
	if !a.ExpiresAt.IsZero() {
		t := a.ExpiresAt
		v.ExpiresAt = &t
	}
	// The escalation is derivable, not stored: the catalog's default tier
	// for the type vs the frozen tier on the action.
	if b.cfg.ActionCatalog != nil {
		if d, ok := b.cfg.ActionCatalog.Descriptor(a.ActionType); ok {
			v.TierEscalated = d.DefaultTier != a.Tier && a.Tier == aggregate.TierT3
			// The human name and category the surfaces show (#123).
			v.Name, v.Category = d.Name, d.Category
			for _, in := range d.Inputs {
				if in.Type != "entity" {
					v.Inputs = append(v.Inputs, EditableInput{Name: in.Name, Required: in.Required})
				}
			}
		}
	}
	// Pre-approval preview: which adapter WOULD dispatch this if approved now
	// (superseded by the actual Adapter once dispatched). SourceScope is v0
	// unscoped; a scoped preview reads the Seed scope (03 §3.5) in v1.
	if a.Adapter == "" {
		if resolver := b.actionResolver(); resolver != nil {
			var params map[string]any
			if len(a.Parameters) > 0 {
				_ = json.Unmarshal(a.Parameters, &params)
			}
			if binding, ok := resolver.PlannedBinding(action.DispatchRequest{
				ActionID:   a.ActionID,
				ActionType: a.ActionType,
				Targets:    a.Targets,
				Parameters: params,
			}); ok {
				v.PlannedAdapter = binding.Adapter
			}
		}
	}
	return v
}

// listActionTypes serves GET /api/action-types: the write-side catalog with
// per-type dispatchability (08 §3), the symmetric twin of GET /api/capabilities.
// This is how the agent learns the real, frozen action vocabulary (host.isolate,
// ioc.block, …) with intents and tiers, instead of guessing action_type strings.
// Any authenticated reader.
func (b *Backend) listActionTypes(w http.ResponseWriter, _ *http.Request) {
	resolver := b.actionResolver()
	if b.cfg.ActionCatalog == nil || resolver == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "action layer not configured")
		return
	}
	summaries := resolver.ListActionTypes(b.cfg.ActionCatalog)
	writeJSON(w, http.StatusOK, map[string]any{"action_types": summaries})
}

// requestAction handles POST /api/actions: the agent-facing request_action tool
// (08 §2). It builds the x-action, runs Gate 2 (04 §4), records the request +
// policy evaluation in one transaction, and — on auto-approval — approves and
// triggers the ActionLifecycle workflow. Requires the analyst role (the AI
// proposes on an analyst's behalf; the AI is never a principal).
func (b *Backend) requestAction(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Handler == nil || b.cfg.Gate2 == nil || b.cfg.ActionCatalog == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "action layer not configured")
		return
	}
	claims, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth context missing")
		return
	}

	var body RequestActionBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	b.dispatchActionRequest(w, r, actorFromClaims(claims), claims.Subject, body)
}

// dispatchActionRequest is the shared request path (fresh request_action and
// re-request of an expired action both flow through it): build the x-action,
// run Gate 2, record, and on auto-approval dispatch. body carries the frozen
// request fields; the caller has already resolved the actor.
func (b *Backend) dispatchActionRequest(w http.ResponseWriter, r *http.Request, actor aggregate.Actor, subject string, body RequestActionBody) {
	investigationID, err := uuid.Parse(body.InvestigationRef)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "investigation_ref is not a valid id")
		return
	}

	now := commandNow()

	// A reversal targets an original action id. Validate it BEFORE anything
	// else (04 §7): the original must exist and be SUCCEEDED (you can only
	// reverse an action that took effect), and the requested action_type must
	// be the original descriptor's declared inverse — otherwise the eventual
	// action.reversed would claim an undo that never happened.
	var (
		reversalOf   uuid.UUID
		originalTier string
	)
	if body.ReversalOfRef != "" {
		reversalOf, err = uuid.Parse(body.ReversalOfRef)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "reversal_of_ref is not a valid id")
			return
		}
		orig, err := b.cfg.Reads.LoadActionCurrent(r.Context(), reversalOf)
		if err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, "reversal_of_ref: original action not found")
			return
		}
		if orig.Status != aggregate.ActionStatusSucceeded {
			writeJSONError(w, http.StatusUnprocessableEntity,
				"reversal_of_ref: original action is "+orig.Status+", only a SUCCEEDED action can be reversed")
			return
		}
		origDesc, ok := b.cfg.ActionCatalog.Descriptor(orig.ActionType)
		if !ok || origDesc.ReversibleBy == "" {
			writeJSONError(w, http.StatusUnprocessableEntity,
				"reversal_of_ref: action type "+orig.ActionType+" is irreversible")
			return
		}
		if origDesc.ReversibleBy != body.ActionType {
			writeJSONError(w, http.StatusUnprocessableEntity,
				"reversal of "+orig.ActionType+" must use action_type "+origDesc.ReversibleBy)
			return
		}
		originalTier = orig.Tier
	}

	// Build the x-action command (validates against the catalog, applies the
	// blast-radius escalator).
	var retryOf uuid.UUID
	if body.RetryOf != "" {
		if retryOf, err = uuid.Parse(body.RetryOf); err != nil {
			writeJSONError(w, http.StatusBadRequest, "retry_of is not a valid id")
			return
		}
	}
	cmd, err := action.BuildRequestCommand(b.cfg.ActionCatalog, action.ActionRequest{
		ActionType:       body.ActionType,
		Targets:          body.Targets,
		Parameters:       body.Parameters,
		EvidenceRefs:     body.EvidenceRefs,
		Rationale:        body.Rationale,
		InvestigationRef: investigationID,
		ReversalOfRef:    reversalOf,
		RetryOf:          retryOf,
	}, now)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 04 §7: reversing is the same tier as the original, NOT lower — enforce
	// tier parity before Gate 2 evaluates, so policies see the true tier.
	if originalTier != "" {
		cmd.Tier = action.MaxTier(cmd.Tier, originalTier)
	}

	// Gate 2: evaluate policies over the (post-escalator) request.
	decision, err := b.cfg.Gate2.Evaluate(action.EvalInputForCommand(cmd, actor.Kind, subject, now))
	if err != nil {
		writeInternalError(w, "policy evaluation", err)
		return
	}
	cmd = action.ApplyDecision(cmd, decision)

	// Record the request + its policy evaluation in one transaction.
	env := newEnvelope(investigationID, actor, now)
	_, err = b.cfg.Handler.Handle(r.Context(), env, cmd)
	if err != nil {
		writeCommandError(w, "request action", err)
		return
	}
	resp := RequestActionResponse{
		ActionID:         cmd.ActionID.String(),
		Tier:             cmd.Tier,
		Mode:             decision.Mode,
		MatchedPolicyRef: decision.MatchedPolicyRef,
		Status:           "PENDING_MANUAL",
	}
	// Invisible activate at the action boundary (01 §Extension 2): the first
	// action of a DRAFT investigation always gets a human — that approval is the
	// activation moment (applyApproveAction transitions draft→active) — so
	// auto-approve is suppressed until the investigation is active. The policy
	// still evaluated and was recorded (would-have-fired); it just doesn't drive
	// this first decision.
	draft := false
	if ic, ierr := b.cfg.Reads.LoadInvestigationCurrent(r.Context(), investigationID); ierr == nil {
		draft = ic.Status == aggregate.StatusDraft
	}
	switch decision.Mode {
	case action.ModeAutoPolicy:
		if draft {
			resp.Status = "PENDING_MANUAL" // held for the activating human approval
		} else {
			resp.Status, resp.WorkflowID = b.autoApproveAndDispatch(r.Context(), env, cmd, decision, now)
		}
	case action.ModeTwoParty:
		// Gate 2's REQUIRE_TWO_PARTY demand is frozen on the action: the approve
		// endpoint honors it (action_approval.go) and the aggregate rejects a solo
		// approval, so this path only reports the pending status.
		resp.Status = "PENDING_TWO_PARTY"
	}

	// A pending action gets its durable expiry timer (02 §3: ActionExpired is
	// system-emitted on expires_at). Best-effort: the deadline stays the
	// invariant (the approve path refuses past it regardless), so a missed
	// start only delays the stored-status transition — the startup sweep
	// backstops it.
	if resp.Status == "PENDING_MANUAL" || resp.Status == "PENDING_TWO_PARTY" {
		b.startExpiryTimer(r.Context(), cmd.ActionID, env.AggregateID, env.TenantID, cmd.ExpiresAt)
	}

	writeJSON(w, http.StatusCreated, resp)
}

// RerequestBody is the POST /api/actions/{id}/rerequest request — the
// analyst's re-affirmation note (why the action is still warranted).
type RerequestBody struct {
	Rationale string `json:"rationale"`
}

// rerequestAction re-requests an EXPIRED action (design/ui: the re-request
// affordance). Re-request is NOT a bypass of expiry — it creates a NEW action
// (the dispatch ledger forbids re-using an id, 08 §6b) with the ORIGINAL's
// frozen fields (same descriptor, targets, parameters, evidence), fresh Gate 2,
// a fresh approval window, and retry_of lineage — which the analyst must then
// separately approve. Expiry's freshness guarantee is honored: re-requesting is
// the conscious re-affirmation it exists to elicit, and the new rationale is
// the analyst's statement of why it still holds. Analyst role; the human is the
// requester (a re-request is a human act even if the original was AI-proposed).
func (b *Backend) rerequestAction(w http.ResponseWriter, r *http.Request, priorID uuid.UUID) {
	if b.cfg.Handler == nil || b.cfg.Gate2 == nil || b.cfg.ActionCatalog == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "action layer not configured")
		return
	}
	claims, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth context missing")
		return
	}
	prior, err := b.cfg.Reads.LoadActionCurrent(r.Context(), priorID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "action not found")
		return
	}
	now := commandNow()

	// Only a spent-but-unexecuted action is re-requestable: EXPIRED or REJECTED
	// by stored status, or a pending action whose frozen deadline has elapsed.
	// The deadline is the INVARIANT (the approve path refuses past it
	// regardless); the stored EXPIRED status is scheduling, owned by the
	// durable expiry timer — which may lag by moments, so the gate checks the
	// invariant directly rather than waiting on the schedule. No status
	// transition happens here: the timer owns that, and it fires on its own.
	deadlineElapsed := (prior.Status == aggregate.ActionStatusRequested ||
		prior.Status == aggregate.ActionStatusPendingSecondary) &&
		!prior.ExpiresAt.IsZero() && prior.ExpiresAt.Before(now)
	switch {
	case prior.Status == aggregate.ActionStatusExpired,
		prior.Status == aggregate.ActionStatusRejected,
		deadlineElapsed:
		// re-requestable
	default:
		writeJSONError(w, http.StatusUnprocessableEntity,
			"only an EXPIRED or REJECTED action can be re-requested; this one is "+prior.Status+" and its approval window has not elapsed")
		return
	}

	var body RerequestBody
	if !decodeOptionalBody(w, r, &body, maxBodyBytes) {
		return
	}
	rationale := body.Rationale
	if rationale == "" {
		rationale = "Re-requested after expiry — re-affirmed as still warranted."
	}

	// Reconstruct the request from the original's FROZEN fields — a faithful
	// "same action, fresh clock", not a client reconstruction that could drift.
	b.dispatchActionRequest(w, r,
		aggregate.Actor{PrincipalID: claims.Subject, Kind: aggregate.ActorHuman}, claims.Subject,
		RequestActionBody{
			ActionType:       prior.ActionType,
			Targets:          prior.Targets,
			Parameters:       prior.Parameters,
			EvidenceRefs:     prior.EvidenceRefs,
			Rationale:        rationale,
			InvestigationRef: prior.AggregateID.String(),
			RetryOf:          priorID.String(),
		})
}
