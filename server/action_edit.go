package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
)

// EditActionBody is the analyst's edit of a requested action (adr/0127): the
// complete new parameters and, optionally, why.
type EditActionBody struct {
	Parameters json.RawMessage `json:"parameters"`
	Reason     string          `json:"reason,omitempty"`
}

// editAction handles POST /api/actions/{id}/edit: change a REQUESTED action's
// parameters before anyone approved it. The new parameters are checked against
// the type's declared inputs, the approval policies are evaluated again over
// the edited request, and the edit lands on the record beside what it
// replaced. Targets and tier never change; an edit never approves on its own
// (a policy that would auto-approve leaves it for the analyst's approval).
// Human-only: an AI delegate is refused here and at the aggregate.
func (b *Backend) editAction(w http.ResponseWriter, r *http.Request, actionID uuid.UUID) {
	if b.cfg.Handler == nil || b.cfg.Gate2 == nil || b.cfg.ActionCatalog == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "action layer not configured")
		return
	}
	claims, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth context missing")
		return
	}
	if claims.DelegateKind != "" {
		writeJSONError(w, http.StatusForbidden, "an AI delegate cannot edit a request; the analyst decides what goes out")
		return
	}
	var body EditActionBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	if len(body.Parameters) == 0 {
		writeJSONError(w, http.StatusBadRequest, "edit: parameters required")
		return
	}
	ac, err := aggregate.LoadActionCurrent(r.Context(), b.cfg.Handler.DB(), actionID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "action not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "load action: "+err.Error())
		return
	}
	d, ok := b.cfg.ActionCatalog.Descriptor(ac.ActionType)
	if !ok {
		writeJSONError(w, http.StatusUnprocessableEntity, "edit: action type "+ac.ActionType+" is not in the catalog")
		return
	}
	if err := action.ValidateParameters(d, body.Parameters); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Gate 2 again, over the request as it will now read.
	actor := actorFromClaims(claims)
	now := commandNow()
	probe := aggregate.RequestAction{
		ActionID: ac.ActionID, ActionType: ac.ActionType, Tier: ac.Tier,
		Targets: ac.Targets, Parameters: body.Parameters, EvidenceRefs: ac.EvidenceRefs,
	}
	decision, err := b.cfg.Gate2.Evaluate(action.EvalInputForCommand(probe, actor.Kind, claims.Subject, now))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "policy evaluation: "+err.Error())
		return
	}
	probe = action.ApplyDecision(probe, decision)
	mode := ac.RequiredMode
	if probe.RequiredMode == action.ModeTwoParty {
		mode = action.ModeTwoParty
	}

	env := newEnvelope(ac.AggregateID, actor, now)
	res, err := b.cfg.Handler.Handle(r.Context(), env, aggregate.EditAction{
		ActionID: actionID, Parameters: body.Parameters, PriorParameters: ac.Parameters, Reason: body.Reason,
		RequiredMode: mode, SecondaryApproverPool: probe.SecondaryApproverPool,
		PolicyEvaluations: probe.PolicyEvaluations, MatchedPolicyRef: probe.MatchedPolicyRef,
	})
	if err != nil {
		writeCommandError(w, "edit action", err)
		return
	}
	b.publishDeltas(res)
	writeJSON(w, http.StatusOK, ActionDecisionResponse{ActionID: actionID.String(), Status: aggregate.ActionStatusRequested})
}
