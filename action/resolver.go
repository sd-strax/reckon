package action

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
)

// Reserved params keys the resolver injects so the (fixture) adapter can match
// and downstream ops can see the action context.
const (
	ParamActionType         = "action_type"
	ParamResolvedIdentifier = "resolved_identifier" // targets[0] convenience for single-target matching
)

// ActionBinding maps an action_type to a concrete write-adapter operation
// (08 §4). Symmetric to the read binding, with one write-specific field.
type ActionBinding struct {
	ActionType                        string         `yaml:"action_type" json:"action_type"`
	Adapter                           string         `yaml:"adapter" json:"adapter"`
	Operation                         string         `yaml:"operation" json:"operation"`
	Priority                          int            `yaml:"priority" json:"priority"`
	Params                            map[string]any `yaml:"params" json:"params"`
	ExternalApprovalSubstitutesReckon bool           `yaml:"external_approval_substitutes_reckon" json:"external_approval_substitutes_reckon"`
	// Identifier is the binding's identifier rule (adr/0406): for each STIX
	// type the action acts on, a template over the stored target entity,
	// under the root target.entity. The engine renders it at request time
	// and freezes the result as the target's resolved_identifier. Required
	// for an action type with target types; absent for one with none
	// (ticket, notify), whose target is a destination the requester names.
	Identifier map[string]string `yaml:"identifier" json:"identifier,omitempty"`
	// Scope is the source scope inherited from the adapter instance (03 §3.5),
	// set from the ActionAdapterSpec at build time — never read from binding YAML
	// (scope lives on the instance). A scope mismatch makes the binding
	// inapplicable, and because the write side never falls through, an action
	// whose every binding is scope-filtered reports no binding: unavailable for
	// that investigation, nothing dispatched (08 §4).
	Scope string `yaml:"-" json:"scope"`
}

// DispatchRequest is what the resolver dispatches: the frozen reckon-action content.
// Resolution of the targets happened at request time (04 §8.1); the adapter
// consumes resolved_identifier and never re-resolves.
type DispatchRequest struct {
	ActionID   uuid.UUID
	ActionType string
	Targets    []aggregate.TargetSpec
	Parameters map[string]any
	// SourceScope is the investigation's source scope (03 §3.5), carried from the
	// Seed so the resolver dispatches only to the matching organization's
	// instance. Empty for single-organization investigations.
	SourceScope string
	// Binding is the binding frozen at request time (adr/0406). Dispatch
	// uses it and no other.
	Binding aggregate.FrozenBinding
}

// ActionResolver selects the write binding for an action and dispatches it
// (08 §4). Unlike the read resolver it does NOT fall through to a lower-priority
// binding on a failed call — a partially-executed state change must never be
// silently re-attempted against a different tool. Binding selection is resolved
// once; retries (08 §6) reuse the same binding.
type ActionResolver struct {
	bindings map[string][]ActionBinding
	adapters map[string]WriteAdapter
}

// NewActionResolver builds a resolver over a tenant's action bindings and its
// enabled write-adapter instances.
func NewActionResolver(bindings map[string][]ActionBinding, adapters map[string]WriteAdapter) *ActionResolver {
	return &ActionResolver{bindings: bindings, adapters: adapters}
}

// ValidateActionBindings compiles every binding template up front (08 §4,
// mirrors 03 §3.3.4) and checks each binding's identifier rule against the
// catalog (adr/0406). A binding for an action type with target types must
// declare an identifier, keyed only by those types. A binding for a type with
// none (ticket, notify) must not. The root target.entity is valid only inside
// identifier, and an identifier reads nothing else, so dispatch never reads
// the entity. Each binding names an adapter and an operation, and no two
// bindings of one action type name the same pair: that triple is the
// identity a request freezes and dispatch finds the binding by. Everything
// dispatch would refuse for a static reason is refused here, never after
// approval. A binding for a type the catalog does not hold has its templates
// compiled and its identity checked, and no more.
func ValidateActionBindings(bindings map[string][]ActionBinding, catalog *ActionCatalog) error {
	for at, bs := range bindings {
		var (
			d     ActionDescriptor
			known bool
		)
		if catalog != nil {
			d, known = catalog.Descriptor(at)
		}
		// The binding a request freezes is found again by its identity, the
		// action type, adapter and operation (frozenSelection), so that
		// identity is a unique key of the binding table, enforced here.
		seen := map[aggregate.FrozenBinding]int{}
		for i, b := range bs {
			if b.Adapter == "" || b.Operation == "" {
				return fmt.Errorf("action_type %s, binding %d: adapter and operation are required; a binding is identified by its action type, adapter and operation", at, i)
			}
			key := aggregate.FrozenBinding{Adapter: b.Adapter, Operation: b.Operation}
			if j, dup := seen[key]; dup {
				return fmt.Errorf("action_type %s: bindings %d (priority %d) and %d (priority %d) both name adapter %s operation %s; a binding is identified by its action type, adapter and operation, so no two may share them",
					at, j, bs[j].Priority, i, b.Priority, b.Adapter, b.Operation)
			}
			seen[key] = i
			if err := validateBinding(at, b, d, known); err != nil {
				return fmt.Errorf("action_type %s, adapter %s: %w", at, b.Adapter, err)
			}
		}
	}
	return nil
}

// validateBinding is ValidateActionBindings for one binding.
func validateBinding(actionType string, b ActionBinding, d ActionDescriptor, known bool) error {
	if err := capability.ValidateParamTemplates(b.Params); err != nil {
		return err
	}
	if p, ok := paramsReadTargetEntity(b.Params); ok {
		return fmt.Errorf("params reference %s; %s is valid only inside identifier, so dispatch never reads the entity", p, targetEntityRoot)
	}
	for typ, tmpl := range b.Identifier {
		if err := capability.ValidateParamTemplates(map[string]any{typ: tmpl}); err != nil {
			return fmt.Errorf("identifier for %s: %w", typ, err)
		}
		if err := checkIdentifierRule(tmpl); err != nil {
			return fmt.Errorf("identifier for %s %w", typ, err)
		}
	}
	if !known {
		return nil
	}
	if len(d.TargetTypes) == 0 {
		if len(b.Identifier) > 0 {
			return fmt.Errorf("%s targets a destination the requester names, so its binding takes no identifier", actionType)
		}
		return nil
	}
	if len(b.Identifier) == 0 {
		return fmt.Errorf("identifier required: %s acts on %s, and the binding must say how each target's identifier is derived from the stored entity",
			actionType, strings.Join(d.TargetTypes, ", "))
	}
	for typ := range b.Identifier {
		if !slices.Contains(d.TargetTypes, typ) {
			return fmt.Errorf("identifier key %s is not a type %s acts on (acts on: %s)", typ, actionType, strings.Join(d.TargetTypes, ", "))
		}
	}
	return nil
}

// ErrNoBinding is returned when no applicable, enabled binding exists for an
// action type — dispatch cannot proceed.
var ErrNoBinding = errors.New("no applicable write binding")

// candidate is a binding that could serve a request in a source scope, with
// its configured adapter.
type candidate struct {
	binding ActionBinding
	adapter WriteAdapter
}

// candidates walks the action type's bindings by priority and returns the
// usable ones: scope match and adapter configured and enabled (03 §3.5,
// adr/0380). It is the walk the request-time planner selects from; health is
// not checked.
func (r *ActionResolver) candidates(actionType, sourceScope string) []candidate {
	var out []candidate
	for _, b := range sortedActionBindings(r.bindings[actionType]) {
		// §3.5: a wrong organization's instance is not applicable, and an
		// adapter not configured or enabled cannot carry it.
		if adapter, ok := r.usableFor(b, sourceScope); ok {
			out = append(out, candidate{binding: b, adapter: adapter})
		}
	}
	return out
}

// configuredAdapter returns the binding's adapter when that adapter is
// configured and enabled: the resolver holds only enabled instances.
func (r *ActionResolver) configuredAdapter(b ActionBinding) (WriteAdapter, bool) {
	a, ok := r.adapters[b.Adapter]
	return a, ok
}

// usableFor returns the binding's adapter when the binding could serve a
// request in sourceScope: its scope matches (03 §3.5) and its adapter is
// configured and enabled. Health is not checked.
func (r *ActionResolver) usableFor(b ActionBinding, sourceScope string) (WriteAdapter, bool) {
	if !capability.ScopeApplicable(b.Scope, sourceScope) {
		return nil, false
	}
	return r.configuredAdapter(b)
}

// HasUsableBinding reports whether actionType has a usable binding for a
// request in sourceScope: one whose adapter is configured and enabled and
// whose scope matches, the rule dispatch's selection skips bindings by
// (adr/0380). Health is not checked and no adapter is probed. A type with
// none cannot be carried out in this install, and the engine refuses its
// requests.
func (r *ActionResolver) HasUsableBinding(actionType, sourceScope string) bool {
	for _, b := range r.bindings[actionType] {
		if _, ok := r.usableFor(b, sourceScope); ok {
			return true
		}
	}
	return false
}

// selection is the frozen binding with its configured adapter and its params
// rendered against the frozen targets and parameters.
type selection struct {
	binding ActionBinding
	adapter WriteAdapter
	params  map[string]any
}

// frozenSelection finds the binding frozen on the request (adr/0406) and
// renders its params. It never selects another binding. It fails, wrapping
// ErrNoBinding, when the action has no frozen binding, when the binding is no
// longer configured, enabled or in scope, or when its params no longer
// render; nothing is sent then.
func (r *ActionResolver) frozenSelection(req DispatchRequest) (selection, error) {
	fb := req.Binding
	if fb.Adapter == "" || fb.Operation == "" {
		return selection{}, fmt.Errorf("%w for action_type %q: the action has no binding frozen at request", ErrNoBinding, req.ActionType)
	}
	var (
		found ActionBinding
		ok    bool
	)
	for _, b := range sortedActionBindings(r.bindings[req.ActionType]) {
		if b.Adapter == fb.Adapter && b.Operation == fb.Operation {
			found, ok = b, true
			break
		}
	}
	if !ok {
		return selection{}, fmt.Errorf("%w for action_type %q: the binding chosen at request (%s %s) is no longer configured",
			ErrNoBinding, req.ActionType, fb.Adapter, fb.Operation)
	}
	adapter, usable := r.usableFor(found, req.SourceScope)
	if !usable {
		return selection{binding: found}, fmt.Errorf("%w for action_type %q: the binding chosen at request (%s %s) is no longer enabled or in scope",
			ErrNoBinding, req.ActionType, fb.Adapter, fb.Operation)
	}
	tctx := dispatchContext(req)
	params, applicable, err := capability.RenderParams(found.Params, tctx)
	if err != nil {
		return selection{binding: found}, fmt.Errorf("action_type %s, adapter %s: template render: %w", req.ActionType, found.Adapter, err)
	}
	if !applicable {
		return selection{binding: found}, fmt.Errorf("%w for action_type %q: the params of the binding chosen at request (%s %s) no longer render: missing %s",
			ErrNoBinding, req.ActionType, fb.Adapter, fb.Operation, strings.Join(missingRefs(found.Params, tctx), ", "))
	}
	return selection{binding: found, adapter: adapter, params: params}, nil
}

// CheckFrozenParams reports whether the frozen binding of req still renders
// its params (adr/0406). An edit of a request's parameters calls it with the
// new parameters, and is refused on an error: the edited action could not be
// carried out by the binding it was approved for.
func (r *ActionResolver) CheckFrozenParams(req DispatchRequest) error {
	_, err := r.frozenSelection(req)
	return err
}

// Resolve dispatches the request through its frozen binding (adr/0406): it
// renders the binding's params against the frozen targets and parameters,
// injects the action context and idempotency key, and dispatches exactly
// once. It never selects another binding; when the frozen one cannot be used
// it fails with nothing sent. It returns the binding used alongside the
// result.
func (r *ActionResolver) Resolve(ctx context.Context, req DispatchRequest) (WriteResult, ActionBinding, error) {
	sel, err := r.frozenSelection(req)
	if err != nil {
		return WriteResult{}, sel.binding, err
	}

	// From here there is NO fall-through: this is the single outbound path
	// for the action.
	sel.params[ParamActionType] = req.ActionType
	if len(req.Targets) > 0 {
		sel.params[ParamResolvedIdentifier] = req.Targets[0].ResolvedIdentifier
	}
	key := IdempotencyKey(req.ActionID, "")

	result, err := sel.adapter.Dispatch(ctx, sel.binding.Operation, sel.params, key)
	if err != nil {
		return WriteResult{}, sel.binding, err // classified WriteError; caller applies the retry budget
	}
	return result, sel.binding, nil
}

// dispatchContext assembles the template roots for action bindings: the target
// (targets[0]), the full target list, the request parameters, and the action
// type.
func dispatchContext(req DispatchRequest) map[string]any {
	targets := make([]any, 0, len(req.Targets))
	for _, t := range req.Targets {
		targets = append(targets, map[string]any{
			"entity_ref":          t.EntityRef,
			"resolved_identifier": t.ResolvedIdentifier,
			"asset_criticality":   t.AssetCriticality,
		})
	}
	ctx := map[string]any{
		"targets":    targets,
		"parameters": req.Parameters,
		"action":     req.ActionType,
	}
	if len(targets) > 0 {
		ctx["target"] = targets[0]
	}
	return ctx
}

func sortedActionBindings(bindings []ActionBinding) []ActionBinding {
	out := append([]ActionBinding(nil), bindings...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority > out[j].Priority })
	return out
}
