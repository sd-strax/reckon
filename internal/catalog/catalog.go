// Package catalog holds what the backend's catalogs and the agent's tool list
// share (adr/0385): the words the backend uses to say whether a verb or an
// action type can be used. It imports no reckon package, so the agent can use
// it without depending on the engine's packages.
package catalog

// CapabilityStatus is a verb's current resolvability in the tenant
// (design/03-capability-layer.md §6.3).
type CapabilityStatus string

const (
	// StatusAvailable: at least one configured binding's adapter is present and
	// healthy, so the agent may advertise the verb to the model.
	StatusAvailable CapabilityStatus = "available"
	// StatusDegraded: bindings are configured but every adapter is absent or
	// unhealthy. Recoverable; the agent may re-list later in the session.
	StatusDegraded CapabilityStatus = "degraded"
	// StatusUnavailable: no binding is configured for the verb in the tenant.
	// Structural: it will not change without a config edit.
	StatusUnavailable CapabilityStatus = "unavailable"
)

// WireValues lists every capability status, in the order declared above.
func (CapabilityStatus) WireValues() []string {
	return []string{string(StatusAvailable), string(StatusDegraded), string(StatusUnavailable)}
}

// ActionAvailability is an action type's current dispatchability in the
// tenant (design/08-write-side-actions.md §3). It is a type of its own, not
// CapabilityStatus, because the two can grow apart.
type ActionAvailability string

const (
	// ActionAvailable: at least one healthy binding.
	ActionAvailable ActionAvailability = "available"
	// ActionDegraded: usable bindings exist, all unhealthy.
	ActionDegraded ActionAvailability = "degraded"
	// ActionUnavailable: no binding with a configured, enabled adapter.
	ActionUnavailable ActionAvailability = "unavailable"
)

// WireValues lists every action availability, in the order declared above.
func (ActionAvailability) WireValues() []string {
	return []string{string(ActionAvailable), string(ActionDegraded), string(ActionUnavailable)}
}
