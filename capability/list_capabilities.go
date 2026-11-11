package capability

import (
	"slices"
	"sort"
)

// CapabilityStatus is a verb's current resolvability in the tenant (§6.3).
type CapabilityStatus string

const (
	// StatusAvailable: at least one configured binding's adapter is present and
	// healthy — the agent may advertise this verb to the LLM.
	StatusAvailable CapabilityStatus = "available"
	// StatusDegraded: bindings are configured but every adapter is absent or
	// unhealthy. Recoverable; the agent may re-list later in the session.
	StatusDegraded CapabilityStatus = "degraded"
	// StatusUnavailable: no binding is configured for this verb in the tenant.
	// Structural — will not change without a config edit.
	StatusUnavailable CapabilityStatus = "unavailable"
)

// CapabilitySummary pairs a verb descriptor with its current availability and
// the number of configured bindings. Limitations lists what a present, healthy
// bound adapter reports it cannot express for the bound operation (§2.8,
// 11 §4.4); it never changes Status — the verb still answers the calls it can
// express.
type CapabilitySummary struct {
	Descriptor  CapabilityDescriptor `json:"descriptor"`
	Status      CapabilityStatus     `json:"status"`
	Bindings    int                  `json:"bindings"`
	Limitations []Limitation         `json:"limitations,omitempty"`
}

// Limitation is one capability a verb's bound adapter cannot express, and why
// (11 §4.4), attributed to the adapter instance that reports it.
type Limitation struct {
	Adapter    string `json:"adapter"`
	Capability string `json:"capability"`
	Reason     string `json:"reason"`
}

// ListCapabilities implements the list_capabilities verb (§2.8): for every
// catalog descriptor it reports whether the tenant can currently resolve it,
// so the agent loop can trim the advertised tool set to healthy verbs (§6.3),
// and the limitations of the bindings that answer it. Each adapter's health is
// probed at most once per listing. Results are sorted by verb for stable
// output.
func (r *Resolver) ListCapabilities(catalog *Catalog) []CapabilitySummary {
	return r.listCapabilities(catalog, true)
}

func (r *Resolver) listCapabilities(catalog *Catalog, withLimitations bool) []CapabilitySummary {
	verbs := catalog.Verbs()
	p := healthProbe{r: r, seen: map[string]HealthStatus{}}
	out := make([]CapabilitySummary, 0, len(verbs))
	for _, verb := range verbs {
		d, _ := catalog.Descriptor(verb)
		bindings := sortedByPriority(r.bindings[verb])
		status, first := p.verbStatus(bindings)
		s := CapabilitySummary{Descriptor: d, Status: status, Bindings: len(bindings)}
		if withLimitations && first >= 0 {
			s.Limitations = p.limitations(bindings, first)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Descriptor.Verb < out[j].Descriptor.Verb })
	return out
}

// healthProbe memoizes Health() per adapter across one listing.
type healthProbe struct {
	r    *Resolver
	seen map[string]HealthStatus
}

// health reports a bound adapter's health; an adapter not in the resolver is
// absent (ok false) and never probed.
func (p healthProbe) health(adapter string) (HealthStatus, bool) {
	if h, ok := p.seen[adapter]; ok {
		return h, true
	}
	a, ok := p.r.adapters[adapter]
	if !ok {
		return HealthStatus{}, false
	}
	h := a.Health()
	p.seen[adapter] = h
	return h, true
}

// verbStatus classifies a verb from its bindings (in priority order) and
// their adapters' health, stopping at the first present, healthy binding so a
// fallback adapter is not probed (spawned) for a listing. first is that
// binding's index, -1 when there is none.
func (p healthProbe) verbStatus(bindings []Binding) (CapabilityStatus, int) {
	if len(bindings) == 0 {
		return StatusUnavailable, -1
	}
	for i, b := range bindings {
		if h, ok := p.health(b.Adapter); ok && h.Healthy {
			return StatusAvailable, i
		}
	}
	return StatusDegraded, -1
}

// limitations is what the bindings that answer a call report degraded for
// their bound operations, each attributed to its adapter. bindings are in
// priority order and first is the first healthy one. A verb that is not fan-out
// is answered by that binding, so its degradations are the verb's and no
// fallback is probed (spawned) for a listing. A fan-out verb is answered by
// every healthy binding at once, so it carries the union of theirs, deduped by
// adapter and capability (the first reason kept).
func (p healthProbe) limitations(bindings []Binding, first int) []Limitation {
	var out []Limitation
	add := func(b Binding, h HealthStatus) {
		for _, d := range h.Degraded {
			if d.Operation != b.Operation || slices.ContainsFunc(out, func(l Limitation) bool {
				return l.Adapter == b.Adapter && l.Capability == d.Capability
			}) {
				continue
			}
			out = append(out, Limitation{Adapter: b.Adapter, Capability: d.Capability, Reason: d.Reason})
		}
	}
	if !anyFanOut(bindings) {
		h, _ := p.health(bindings[first].Adapter)
		add(bindings[first], h)
		return out
	}
	for _, b := range bindings[first:] {
		if h, ok := p.health(b.Adapter); ok && h.Healthy {
			add(b, h)
		}
	}
	return out
}

// AvailableVerbs returns just the verbs the agent should advertise (§6.3): those
// whose status is available.
func (r *Resolver) AvailableVerbs(catalog *Catalog) []string {
	var out []string
	for _, s := range r.listCapabilities(catalog, false) {
		if s.Status == StatusAvailable {
			out = append(out, s.Descriptor.Verb)
		}
	}
	return out
}
