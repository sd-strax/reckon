package action

import (
	"sort"

	"github.com/sd-strax/reckon/internal/agentapi"
	"github.com/sd-strax/reckon/internal/catalog"
)

// ActionAvailability is an action type's current dispatchability in the tenant,
// analogous to capability.CapabilityStatus (08 §3, mirroring 03 §6.3). It is
// defined once in internal/catalog, which the agent shares (adr/0385).
type ActionAvailability = catalog.ActionAvailability

const (
	ActionAvailable   = catalog.ActionAvailable   // ≥1 healthy binding
	ActionDegraded    = catalog.ActionDegraded    // usable bindings exist, all unhealthy
	ActionUnavailable = catalog.ActionUnavailable // no binding with a configured, enabled adapter
)

// ActionSummary pairs an action descriptor with its current availability.
// It is defined in internal/agentapi, which the agent shares (adr/0278).
type ActionSummary = agentapi.ActionSummary

// ListActionTypes implements the list_action_types verb (08 §3): for every
// cataloged action it reports whether the tenant can currently dispatch it, so
// the agent loop advertises only usable actions. An action appears once its
// descriptor is registered and at least one binding exists.
func (r *ActionResolver) ListActionTypes(cat *ActionCatalog) []ActionSummary {
	types := cat.ActionTypes()
	out := make([]ActionSummary, 0, len(types))
	for _, at := range types {
		d, _ := cat.Descriptor(at)
		bindings := r.bindings[at]
		out = append(out, ActionSummary{
			Descriptor: d,
			Status:     r.actionStatus(bindings),
			Bindings:   len(bindings),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Descriptor.ActionType < out[j].Descriptor.ActionType })
	return out
}

// actionStatus classifies an action from its bindings and their adapters: no
// binding with a configured, enabled adapter is unavailable; one whose adapter
// is healthy is available; otherwise degraded. The listing has no request
// scope, so scope is not read here (adr/0380).
func (r *ActionResolver) actionStatus(bindings []ActionBinding) ActionAvailability {
	status := ActionUnavailable
	for _, b := range bindings {
		a, ok := r.configuredAdapter(b)
		if !ok {
			continue
		}
		if a.Health().Healthy {
			return ActionAvailable
		}
		status = ActionDegraded
	}
	return status
}
