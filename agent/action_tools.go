package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/sd-strax/reckon/internal/agentapi"
	"github.com/sd-strax/reckon/internal/catalog"
)

// The agent requests each action type through its own tool (adr/0380). A
// tool's inputs are its type's non-entity inputs at the top level, beside the
// fields every request shares. Every action tool runs one handler, which
// builds the same request_action body (08 §2) the engine has always received.

// actionToolName is the tool name of an action type (catalog.ActionToolName,
// adr/0382). The name is never read back into a type: each action tool
// carries its type, which Session.OfferedTools reports and the eval's trial
// record keeps in its tool list.
func actionToolName(actionType string) string {
	return catalog.ActionToolName(actionType)
}

// actionShared is the fields every action tool carries beside its type's
// inputs (catalog.ActionRequestFields). It is the schema of those fields and
// their decoder.
type actionShared struct {
	Targets      []targetInput `json:"targets" required:"true" desc:"the distinct targets; blast radius drives the trust tier"`
	Rationale    string        `json:"rationale" required:"true" desc:"why this action, now"`
	EvidenceRefs []string      `json:"evidence_refs" required:"true" nonempty:"true" desc:"REQUIRED: the STIX refs that ground this action — the observed_data_refs / entity_refs / reckon-hypothesis ids you observed. A containment or remediation with no cited evidence is invalid and the engine rejects it."`
	RetryOf      string        `json:"retry_of" desc:"when re-requesting a FAILED or EXPIRED action: that action's id (records the retry lineage); omit it otherwise"`
}

// unavailable reports whether an action type has nothing to dispatch to: it
// cannot be carried out in this install. The engine refuses its requests.
func unavailable(a agentapi.ActionSummary) bool {
	return a.Status == catalog.ActionUnavailable
}

// actionTools builds one tool per action type of the catalog, unavailable
// ones included, sorted by action type (adr/0380). An unavailable type stays
// visible because other descriptions name it; the engine refuses its
// requests. The backend refused any descriptor the agent could not offer when
// it was registered (adr/0382); two tools of one name still fail in
// buildTools, as a last guard.
func actionTools(actionTypes []agentapi.ActionSummary) []tool {
	types := slices.Clone(actionTypes)
	slices.SortStableFunc(types, func(a, b agentapi.ActionSummary) int {
		return strings.Compare(a.Descriptor.ActionType, b.Descriptor.ActionType)
	})
	tools := make([]tool, 0, len(types))
	for _, a := range types {
		tools = append(tools, tool{
			def:        actionToolDef(a),
			kind:       ToolKindAction,
			actionType: a.Descriptor.ActionType,
			origin:     "#380",
			revised:    []string{"#386"},
			run: func(ctx context.Context, s *Session, input json.RawMessage) (toolResult, error) {
				return s.requestAction(ctx, a, input)
			},
		})
	}
	return tools
}

// ParameterInputs is an action type's non-entity inputs: the keys of its
// request's parameters (08 §3). Entity inputs ride targets.
func ParameterInputs(a agentapi.ActionSummary) []agentapi.InputParam {
	var out []agentapi.InputParam
	for _, in := range a.Descriptor.Inputs {
		if in.Type != "entity" {
			out = append(out, in)
		}
	}
	return out
}

// actionToolDef renders one action type as a tool: the shared fields, then
// the type's inputs as top-level properties, required as the descriptor
// requires them.
func actionToolDef(a agentapi.ActionSummary) ToolDef {
	schema := inputSchema[actionShared](nil)
	props := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]string)
	for _, in := range ParameterInputs(a) {
		p := verbScalarSchema(in.Type, strings.TrimSpace(in.Desc))
		if p["description"] == "" {
			delete(p, "description")
		}
		props[in.Name] = p
		if in.Required {
			required = append(required, in.Name)
		}
	}
	schema["required"] = required
	return ToolDef{
		Name:        actionToolName(a.Descriptor.ActionType),
		Description: actionToolDescription(a),
		InputSchema: schema,
	}
}

// actionToolDescription renders a tool's description from its descriptor and
// the ledger's text: the action type (adr/0386), the intent, the tier and
// reversibility, then for a type that can be carried out whether it is
// degraded and the guidance every request shares, and for an unavailable type
// that it cannot be carried out.
func actionToolDescription(a agentapi.ActionSummary) string {
	d := a.Descriptor
	var b strings.Builder
	fmt.Fprintf(&b, textActionToolType.Text, d.ActionType)
	fmt.Fprintf(&b, textActionTool.Text, d.Intent, d.DefaultTier, d.Reversibility)
	if unavailable(a) {
		// Nothing can carry it out, so the guidance on how a request goes
		// through authorization does not apply.
		b.WriteString(textActionToolUnavailable.Text)
		return b.String()
	}
	if a.Status == catalog.ActionDegraded {
		b.WriteString(textActionToolDegraded.Text)
	}
	b.WriteString(textActionToolGuidance.Text)
	return b.String()
}

// requestAction is the handler of every action tool. It builds the request
// the engine receives: the action type from the tool, the shared fields, and
// parameters from every other key the model sent. A key the type does not
// declare goes into parameters too, so the engine refuses it as undeclared
// rather than the loop dropping it unseen. A call to an unavailable type goes
// to the engine like any other, which refuses it before recording anything;
// the refusal reaches the model as the tool's error (adr/0380).
func (s *Session) requestAction(ctx context.Context, a agentapi.ActionSummary, input json.RawMessage) (toolResult, error) {
	name := actionToolName(a.Descriptor.ActionType)
	var all map[string]json.RawMessage
	if err := json.Unmarshal(input, &all); err != nil {
		return toolResult{}, fmt.Errorf("bad %s input: %w", name, err)
	}
	shared, err := sharedFields(all)
	if err != nil {
		return toolResult{}, fmt.Errorf("bad %s input: %w", name, err)
	}
	parameters := actionParameters(a, all)
	targets := make([]agentapi.TargetSpec, 0, len(shared.Targets))
	for _, t := range shared.Targets {
		targets = append(targets, agentapi.TargetSpec{EntityRef: t.EntityRef, ResolvedIdentifier: t.ResolvedIdentifier})
	}
	resp, err := s.backend.RequestAction(ctx, agentapi.RequestActionBody{
		ActionType:       a.Descriptor.ActionType,
		Targets:          targets,
		Parameters:       parameters,
		EvidenceRefs:     shared.EvidenceRefs,
		Rationale:        shared.Rationale,
		InvestigationRef: s.investigationID,
		RetryOf:          shared.RetryOf,
	})
	if err != nil {
		return toolResult{}, err
	}
	s.pendingActions = append(s.pendingActions, resp)
	return contentOf(json.Marshal(actionResult{
		ActionID: resp.ActionID, Tier: resp.Tier, Status: resp.Status, Mode: resp.Mode, WorkflowID: resp.WorkflowID,
	}))
}

// actionResult is the request_action result the model reads. It names its
// fields, so a field the backend's answer gains reaches the model only when
// it is added here.
type actionResult struct {
	ActionID   string                       `json:"action_id"`
	Tier       agentapi.Tier                `json:"tier"`
	Status     agentapi.ActionRequestStatus `json:"status"`
	Mode       agentapi.AuthMode            `json:"mode"`
	WorkflowID string                       `json:"workflow_id,omitempty"`
}

// actionParameters gathers a call's parameters: every key that is not a
// shared field. A null for a declared optional input means the model has no
// value, so the key is left out. A null for any other key stays, so the
// engine refuses a required input as missing and an undeclared key as
// unknown. With no keys, parameters is left out of the request.
func actionParameters(a agentapi.ActionSummary, all map[string]json.RawMessage) json.RawMessage {
	optional := map[string]bool{}
	for _, in := range ParameterInputs(a) {
		if !in.Required {
			optional[in.Name] = true
		}
	}
	params := map[string]json.RawMessage{}
	for k, v := range all {
		if IsActionSharedField(k) {
			continue
		}
		if optional[k] && string(v) == "null" {
			continue
		}
		params[k] = v
	}
	if len(params) == 0 {
		return nil
	}
	// A map of raw JSON values always marshals.
	raw, _ := json.Marshal(params)
	return raw
}

// sharedFields takes the shared fields out of a call's decoded input and
// decodes them into actionShared, so the shapes the schema declares are the
// ones read.
func sharedFields(all map[string]json.RawMessage) (actionShared, error) {
	subset := make(map[string]json.RawMessage, len(actionSharedFields))
	for k, v := range all {
		if IsActionSharedField(k) {
			subset[k] = v
		}
	}
	var shared actionShared
	raw, _ := json.Marshal(subset) // a map of raw JSON values always marshals
	err := json.Unmarshal(raw, &shared)
	return shared, err
}

// IsActionSharedField reports whether key is one of the fields every action
// tool carries beside its type's inputs: targets, rationale, evidence_refs
// and retry_of. Every other key of a call is a parameter of the request.
func IsActionSharedField(key string) bool {
	return actionSharedFields[key]
}

// actionSharedFields is the names of the fields every action tool carries,
// read from actionShared so the two cannot drift apart.
var actionSharedFields = func() map[string]bool {
	props := inputSchema[actionShared](nil)["properties"].(map[string]any)
	names := make(map[string]bool, len(props))
	for name := range props {
		names[name] = true
	}
	return names
}()
