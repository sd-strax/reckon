package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
)

// listInvestigationActions serves GET /api/investigations/{id}/actions: every
// reckon-action of the investigation, oldest first. This is how a surface recovers
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
	outcomes, err := b.cfg.Reads.ListRecordedOutcomes(r.Context(), invID)
	if err != nil {
		writeInternalError(w, "list action outcomes", err)
		return
	}
	out := make([]ActionView, 0, len(acts))
	for _, a := range acts {
		v := b.actionView(a)
		v.Outcome = outcomes[a.ActionID]
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, ActionsResponse{Actions: out})
}

// actionView maps one projected reckon-action to the list/record view: the frozen
// request facts plus the two derived facts (tier escalation against the
// catalog's default, the pre-approval planned adapter).
func (b *Backend) actionView(a aggregate.ActionCurrent) ActionView {
	v := ActionView{
		ActionID:              a.ActionID.String(),
		ActionType:            a.ActionType,
		Tier:                  aggregate.Tier(a.Tier),
		Status:                aggregate.ActionStatus(a.Status),
		RequiredMode:          aggregate.AuthMode(a.RequiredMode),
		SecondaryApproverPool: a.SecondaryApproverPool,
		PrimaryApproverRef:    a.PrimaryApprover,
		Mode:                  aggregate.AuthMode(a.Mode),
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
	if !a.CreatedAt.IsZero() {
		t := a.CreatedAt
		v.RequestedAt = &t
	}
	if !a.PrimaryApprovedAt.IsZero() {
		t := a.PrimaryApprovedAt
		v.ApprovedAt = &t
	}
	// The escalation is derivable, not stored: the catalog's default tier
	// for the type vs the frozen tier on the action.
	if b.cfg.ActionCatalog != nil {
		if d, ok := b.cfg.ActionCatalog.Descriptor(a.ActionType); ok {
			v.TierEscalated = string(d.DefaultTier) != a.Tier && a.Tier == aggregate.TierT3
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
	writeJSON(w, http.StatusOK, ActionTypesResponse{ActionTypes: summaries})
}

// requestAction handles POST /api/actions: the agent-facing request_action tool
// (08 §2). It builds the reckon-action, runs Gate 2 (04 §4), records the request +
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
	b.dispatchActionRequest(w, r, actorFromRequest(r, claims), claims.Subject, body)
}

// dispatchActionRequest is the shared request path (fresh request_action and
// re-request of an expired action both flow through it): build the reckon-action,
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

	// An action type with no usable binding for this investigation cannot be
	// carried out in this install. It is refused after the reversal checks
	// and before anything is built or recorded: recorded, it would wait for
	// approval and fail only at dispatch (adr/0380). The check covers a fresh
	// request, a re-request and a reversal, whose action_type is the inverse
	// type. A degraded type is accepted.
	if b.actionTypeUnavailable(body.ActionType, b.requestScope(r.Context(), investigationID)) {
		writeJSONError(w, http.StatusUnprocessableEntity, unavailableActionMessage(body.ActionType))
		return
	}

	// Build the reckon-action command (validates against the catalog, applies the
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
		if b.writeEventCitedError(r.Context(), w, "request action", err) {
			return
		}
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
		Tier:             aggregate.Tier(cmd.Tier),
		Mode:             aggregate.AuthMode(decision.Mode),
		MatchedPolicyRef: decision.MatchedPolicyRef,
		Status:           RequestPendingManual,
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
			resp.Status = RequestPendingManual // held for the activating human approval
		} else {
			resp.Status, resp.WorkflowID = b.autoApproveAndDispatch(r.Context(), env, cmd, decision, now)
		}
	case action.ModeTwoParty:
		// Gate 2's REQUIRE_TWO_PARTY demand is frozen on the action: the approve
		// endpoint honors it (action_approval.go) and the aggregate rejects a solo
		// approval, so this path only reports the pending status.
		resp.Status = RequestPendingTwoParty
	}

	// A pending action gets its durable expiry timer (02 §3: ActionExpired is
	// system-emitted on expires_at). Best-effort: the deadline stays the
	// invariant (the approve path refuses past it regardless), so a missed
	// start only delays the stored-status transition — the startup sweep
	// backstops it.
	if resp.Status == RequestPendingManual || resp.Status == RequestPendingTwoParty {
		b.startExpiryTimer(r.Context(), cmd.ActionID, env.AggregateID, env.TenantID, cmd.ExpiresAt)
	}

	writeJSON(w, http.StatusCreated, resp)
}

// actionTypeUnavailable reports whether a catalog action type has no usable
// binding for a request in sourceScope: none whose adapter is configured and
// enabled and whose scope matches (action.ActionResolver.HasUsableBinding).
// Health is not checked. A type outside the catalog is left to the request's
// own validation, and with no resolver nothing is refused here.
func (b *Backend) actionTypeUnavailable(actionType, sourceScope string) bool {
	resolver := b.actionResolver()
	if resolver == nil || b.cfg.ActionCatalog == nil {
		return false
	}
	if _, ok := b.cfg.ActionCatalog.Descriptor(actionType); !ok {
		return false
	}
	return !resolver.HasUsableBinding(actionType, sourceScope)
}

// requestScope is the investigation's source scope (03 §3.5), read from its
// Seed as dispatch reads it. A lookup miss leaves it empty, which is
// fail-closed for a scoped binding, as at dispatch.
func (b *Backend) requestScope(ctx context.Context, investigationID uuid.UUID) string {
	if b.cfg.Reads == nil {
		return ""
	}
	inv, err := b.cfg.Reads.LoadInvestigationCurrent(ctx, investigationID)
	if err != nil || inv.Seed == nil {
		return ""
	}
	return inv.Seed.SourceScope
}

// unavailableActionMessage is the refusal of a request for an unavailable
// action type.
func unavailableActionMessage(actionType string) string {
	return "action type " + actionType + " is unavailable in this install and cannot be carried out"
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

	// A re-request restates the original's grounding. When the original
	// cited raw events only, nothing of it may be cited now (adr/0364).
	evidenceRefs := citableRefs(prior.EvidenceRefs)
	if len(prior.EvidenceRefs) > 0 && len(evidenceRefs) == 0 {
		writeJSONError(w, http.StatusUnprocessableEntity,
			"re-request action: the original cites raw events only ("+strings.Join(prior.EvidenceRefs, ", ")+"), and a new request must cite the observations made from them; request the action anew, citing them")
		return
	}

	// Reconstruct the request from the original's FROZEN fields — a faithful
	// "same action, fresh clock", not a client reconstruction that could drift.
	// The one change is the grounding: a raw event id the original cited is
	// left out, since a new citation names a STIX object (adr/0364).
	b.dispatchActionRequest(w, r, actorFromRequest(r, claims), claims.Subject,
		RequestActionBody{
			ActionType:       prior.ActionType,
			Targets:          prior.Targets,
			Parameters:       prior.Parameters,
			EvidenceRefs:     evidenceRefs,
			Rationale:        rationale,
			InvestigationRef: prior.AggregateID.String(),
			RetryOf:          priorID.String(),
		})
}
