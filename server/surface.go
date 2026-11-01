package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/capability"
)

// listCapabilities serves GET /api/capabilities. Returns 503 when the capability
// layer is not configured (no tenant capability YAML); otherwise the verb
// summaries with their availability. Read requires viewer/analyst/auditor.
func (b *Backend) listCapabilities(w http.ResponseWriter, _ *http.Request) {
	resolver, catalog := b.capabilitySurface()
	if resolver == nil || catalog == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "capability layer not configured")
		return
	}
	summaries := resolver.ListCapabilities(catalog)
	body := map[string]any{"capabilities": summaries}
	// The tenant's case queue (14 §4.2) rides the catalog response: it is
	// the SoR surface's one piece of standing config the picker needs.
	if q := resolver.PolicyString(capability.PolicyCaseQueue); q != "" {
		body["case_queue"] = q
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// capabilitySurface returns the live resolver + catalog: the hot-swapped pair
// when the enablement endpoint has rebuilt them, else the boot-time pair.
func (b *Backend) capabilitySurface() (*capability.Resolver, *capability.Catalog) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.capOverride != nil {
		return b.capOverride.resolver, b.capOverride.catalog
	}
	return b.cfg.CapabilityResolver, b.cfg.CapabilityCatalog
}

// capabilitySurface bundles the pair so the override is one pointer swap.
type capabilitySurface struct {
	resolver *capability.Resolver
	catalog  *capability.Catalog
}

// actionResolver returns the live write-side resolver for the /action-types
// availability readout: the hot-swapped one when a reload installed it, else
// the boot-time one. (The dispatch path itself lives in the Temporal worker,
// swapped in lockstep — this override only governs the availability display.)
func (b *Backend) actionResolver() *action.ActionResolver {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.actionOverride != nil {
		return b.actionOverride
	}
	return b.cfg.ActionResolver
}

// ReloadedSurface is what one reload builds from the tenant config: both
// sides, applied together or not at all.
type ReloadedSurface struct {
	CapabilityResolver *capability.Resolver
	CapabilityCatalog  *capability.Catalog
	// ActionResolver is nil when the action layer is off.
	ActionResolver *action.ActionResolver
}

// Reload rebuilds both sides from the tenant config on disk and swaps them in:
// the capability surface, the /action-types readout and the worker's dispatch
// holder. It is the one reload routine, called by SIGHUP and the enablement
// endpoint (adr/0223). A build failure on either side swaps nothing, so a
// broken edit degrades to "the change didn't take" and the process never runs
// half a file. In-flight requests keep the resolvers they already read (a
// resolver is immutable once built).
func (b *Backend) Reload() error {
	b.reloadMu.Lock()
	defer b.reloadMu.Unlock()
	return b.reloadLocked()
}

// reloadLocked is Reload for a caller already holding reloadMu.
func (b *Backend) reloadLocked() error {
	rebuild := b.cfg.Rebuild
	if rebuild == nil {
		if b.cfg.CapabilityConfigPath == "" {
			return nil // nothing to reload from
		}
		rebuild = b.buildInPackage
	}
	s, err := rebuild()
	if err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	b.mu.Lock()
	b.capOverride = &capabilitySurface{resolver: s.CapabilityResolver, catalog: s.CapabilityCatalog}
	if s.ActionResolver != nil {
		// Dispatch first, then the readout: /action-types never advertises a
		// binding the worker cannot yet dispatch through.
		if b.cfg.ActionDispatch != nil {
			b.cfg.ActionDispatch.Store(s.ActionResolver)
		}
		b.actionOverride = s.ActionResolver
	}
	b.mu.Unlock()
	return nil
}
