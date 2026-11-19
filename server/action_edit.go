package server

import (
	"encoding/json"
	"errors"
	"fmt"
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
// the type's declared inputs and against the binding frozen at request
// (adr/0406), the approval policies are evaluated again over the edited
// request, and the edit lands on the record beside what it replaced. Targets and tier never change; an edit never approves on its own
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
	ac, err := b.cfg.Reads.LoadActionCurrent(r.Context(), actionID)
	if errors.Is(err, aggregate.ErrNoRecord) {
		writeJSONError(w, http.StatusNotFound, "action not found")
		return
	}
	if err != nil {
		writeInternalError(w, "load action", err)
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
	// An edit keeps the binding frozen at request time (adr/0406), so the
	// new parameters must still render that binding's params. An edit that
	// would leave the approved action unable to dispatch is refused.
	resolver := b.actionResolver()
	if resolver == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "action layer not configured: no write bindings, so the edit cannot be checked against the action's binding")
		return
	}
	if ns := resolver.CheckFrozenParams(action.DispatchRequest{
		ActionID:    ac.ActionID,
		ActionType:  ac.ActionType,
		Targets:     ac.Targets,
		Parameters:  paramsMap(body.Parameters),
		SourceScope: b.requestScope(r.Context(), ac.AggregateID),
		Binding:     ac.Binding,
	}); ns != nil {
		// The check runs before any dispatch; an edit names only what broke.
		writeJSONError(w, http.StatusUnprocessableEntity, editBindingRefusal(ns))
		return
	}

	// Gate 2 again, over the request as it will now read. It evaluates the
	// action as its requester asked for it (adr/0374): the editor's identity
	// never stands in for the requester's, so a person editing an agent's
	// request neither makes a requester policy match nor lifts the AI baseline.
	requester, err := b.actionRequester(r, ac)
	if err != nil {
		writeInternalError(w, "load action requester", err)
		return
	}
	actor := actorFromRequest(r, claims)
	now := commandNow()
	probe := aggregate.RequestAction{
		ActionID: ac.ActionID, ActionType: ac.ActionType, Tier: ac.Tier,
		Targets: ac.Targets, Parameters: body.Parameters, EvidenceRefs: ac.EvidenceRefs,
	}
	decision, err := b.cfg.Gate2.Evaluate(action.EvalInputForCommand(probe, requester.Kind, requester.PrincipalID, now))
	if err != nil {
		writeInternalError(w, "policy evaluation", err)
		return
	}
	probe = action.ApplyDecision(probe, decision)
	mode := ac.RequiredMode
	if probe.RequiredMode == action.ModeTwoParty {
		mode = action.ModeTwoParty
	}

	env := newEnvelope(ac.AggregateID, actor, now)
	_, err = b.cfg.Handler.Handle(r.Context(), env, aggregate.EditAction{
		ActionID: actionID, Parameters: body.Parameters, PriorParameters: ac.Parameters, Reason: body.Reason,
		RequiredMode: mode, SecondaryApproverPool: probe.SecondaryApproverPool,
		PolicyEvaluations: probe.PolicyEvaluations, MatchedPolicyRef: probe.MatchedPolicyRef,
	})
	if err != nil {
		writeCommandError(w, "edit action", err)
		return
	}
	writeJSON(w, http.StatusOK, ActionDecisionResponse{ActionID: actionID.String(), Status: aggregate.ActionStatusRequested})
}

// actionRequester returns the actor of the action's ActionRequested event: who
// asked for it, and whether as a person or an AI delegate. An empty kind is a
// person's: events written before Kind existed carry none (aggregate.Actor).
func (b *Backend) actionRequester(r *http.Request, ac aggregate.ActionCurrent) (aggregate.Actor, error) {
	history, err := b.cfg.Reads.ActionHistory(r.Context(), ac.AggregateID, ac.ActionID)
	if err != nil {
		return aggregate.Actor{}, err
	}
	for _, e := range history {
		if req, ok := e.Event.(aggregate.ActionRequested); ok && req.ActionID == ac.ActionID {
			if e.Actor.Kind == "" {
				e.Actor.Kind = aggregate.ActorHuman
			}
			return e.Actor, nil
		}
	}
	return aggregate.Actor{}, fmt.Errorf("action %s has no request event", ac.ActionID)
}

// editBindingRefusal is the refusal of an edit whose new parameters the
// binding frozen at request can no longer carry (adr/0406, adr/0384).
func editBindingRefusal(ns *action.NotSentError) string {
	return "edit: the binding chosen at request can no longer carry the edited action: " + ns.Reason
}
