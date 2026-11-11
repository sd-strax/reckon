package capability

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// TestListCapabilitiesStatus classifies verbs by binding presence and adapter
// health: available (healthy binding), degraded (binding but unhealthy),
// unavailable (no binding).
func TestListCapabilitiesStatus(t *testing.T) {
	catalog := DefaultCatalog()
	res := newResolver(
		map[string][]Binding{
			// enumerate_logons: healthy adapter → available.
			"enumerate_logons": {{Adapter: "fixture", Operation: "replay", Priority: 100}},
			// get_process_ancestry: adapter present but unhealthy → degraded.
			"get_process_ancestry": {{Adapter: "down", Operation: "op", Priority: 100}},
			// get_host_context: bound to an adapter not in the map → degraded.
			"get_host_context": {{Adapter: "missing", Operation: "op", Priority: 100}},
			// (get_indicator_context, search_alerts, resolve_entity have no
			// bindings → unavailable.)
		},
		map[string]Adapter{
			"fixture": healthyStub("fixture"),
			"down":    &stubAdapter{name: "down", healthy: false},
		},
	)

	byVerb := map[string]CapabilityStatus{}
	for _, s := range res.ListCapabilities(catalog) {
		byVerb[s.Descriptor.Verb] = s.Status
	}

	cases := map[string]CapabilityStatus{
		"enumerate_logons":      StatusAvailable,
		"get_process_ancestry":  StatusDegraded,
		"get_host_context":      StatusDegraded,
		"get_indicator_context": StatusUnavailable,
		"search_alerts":         StatusUnavailable,
	}
	for verb, want := range cases {
		if byVerb[verb] != want {
			t.Errorf("%s status = %q; want %q", verb, byVerb[verb], want)
		}
	}
}

// TestListCapabilitiesCoversCatalog: every catalog descriptor appears in the
// output exactly once, even with no bindings at all.
func TestListCapabilitiesCoversCatalog(t *testing.T) {
	catalog := DefaultCatalog()
	res := newResolver(map[string][]Binding{}, map[string]Adapter{})
	summaries := res.ListCapabilities(catalog)
	if len(summaries) != len(catalog.Verbs()) {
		t.Fatalf("got %d summaries; want %d (one per descriptor)", len(summaries), len(catalog.Verbs()))
	}
	for _, s := range summaries {
		if s.Status != StatusUnavailable {
			t.Errorf("%s: status %q with no bindings; want unavailable", s.Descriptor.Verb, s.Status)
		}
	}
}

// TestAvailableVerbs returns only the healthy-binding verbs.
func TestAvailableVerbs(t *testing.T) {
	catalog := DefaultCatalog()
	res := newResolver(
		map[string][]Binding{"enumerate_logons": {{Adapter: "fixture", Operation: "replay", Priority: 100}}},
		map[string]Adapter{"fixture": healthyStub("fixture")},
	)
	got := res.AvailableVerbs(catalog)
	if len(got) != 1 || got[0] != "enumerate_logons" {
		t.Errorf("available verbs = %v; want [enumerate_logons]", got)
	}
}

// degradedStub is a healthy adapter that reports degraded capabilities and
// counts its health probes.
type degradedStub struct {
	stubAdapter
	degraded []DegradedCapability
	probes   int
}

func (d *degradedStub) Health() HealthStatus {
	d.probes++
	return HealthStatus{Healthy: d.healthy, Degraded: d.degraded}
}

// listOne lists the catalog and returns enumerate_logons' summary.
func listOne(t *testing.T, bindings []Binding, adapters map[string]Adapter) CapabilitySummary {
	t.Helper()
	res := newResolver(map[string][]Binding{"enumerate_logons": bindings}, adapters)
	for _, s := range res.ListCapabilities(DefaultCatalog()) {
		if s.Descriptor.Verb == "enumerate_logons" {
			return s
		}
	}
	t.Fatal("enumerate_logons not listed")
	return CapabilitySummary{}
}

func degradedAdapter(name string, healthy bool, d ...DegradedCapability) *degradedStub {
	return &degradedStub{stubAdapter: stubAdapter{name: name, healthy: healthy}, degraded: d}
}

var credFilter = DegradedCapability{Operation: "activity", Capability: "credential filter", Reason: "actor.session.credential_uid absent"}

// TestListCapabilitiesLimitations_SingleBinding: a healthy binding's degraded
// capability for its bound operation is the verb's limitation, attributed to
// its adapter and listed once, with the status unchanged; another operation's
// degradation stays off it.
func TestListCapabilitiesLimitations_SingleBinding(t *testing.T) {
	lake := degradedAdapter("lake", true, credFilter, credFilter,
		DegradedCapability{Operation: "history", Capability: "resource region", Reason: "absent"})
	s := listOne(t, []Binding{{Adapter: "lake", Operation: "activity", Priority: 100}}, map[string]Adapter{"lake": lake})
	if s.Status != StatusAvailable {
		t.Errorf("status = %q; want available", s.Status)
	}
	want := Limitation{Adapter: "lake", Capability: credFilter.Capability, Reason: credFilter.Reason}
	if len(s.Limitations) != 1 || s.Limitations[0] != want {
		t.Errorf("limitations = %+v; want [%+v]", s.Limitations, want)
	}
}

// TestListCapabilitiesLimitations_PrimaryDegraded: a verb that is not fan-out
// carries the first healthy binding's degradations, attributed to it, and the
// fallback is never probed.
func TestListCapabilitiesLimitations_PrimaryDegraded(t *testing.T) {
	lake := degradedAdapter("lake", true, credFilter)
	full := degradedAdapter("full", true)
	s := listOne(t, []Binding{
		{Adapter: "full", Operation: "activity", Priority: 100},
		{Adapter: "lake", Operation: "activity", Priority: 200},
	}, map[string]Adapter{"lake": lake, "full": full})
	want := Limitation{Adapter: "lake", Capability: credFilter.Capability, Reason: credFilter.Reason}
	if len(s.Limitations) != 1 || s.Limitations[0] != want {
		t.Errorf("limitations = %+v; want [%+v]", s.Limitations, want)
	}
	if full.probes != 0 {
		t.Errorf("fallback probed %d time(s); want 0", full.probes)
	}
}

// TestListCapabilitiesLimitations_PrimaryClean: a primary that reports nothing
// degraded leaves the verb without limitations, whatever a fallback would
// report, and the fallback is never probed.
func TestListCapabilitiesLimitations_PrimaryClean(t *testing.T) {
	full := degradedAdapter("full", true)
	lake := degradedAdapter("lake", true, credFilter)
	s := listOne(t, []Binding{
		{Adapter: "full", Operation: "activity", Priority: 200},
		{Adapter: "lake", Operation: "activity", Priority: 100},
	}, map[string]Adapter{"lake": lake, "full": full})
	if s.Status != StatusAvailable || s.Limitations != nil {
		t.Errorf("summary = %q %+v; want available, no limitations", s.Status, s.Limitations)
	}
	if lake.probes != 0 {
		t.Errorf("fallback probed %d time(s); want 0", lake.probes)
	}
}

// TestListCapabilitiesLimitations_FanOutOneDegraded: a fan-out verb carries
// the degradation of the one binding that reports it.
func TestListCapabilitiesLimitations_FanOutOneDegraded(t *testing.T) {
	full := degradedAdapter("full", true)
	lake := degradedAdapter("lake", true, credFilter)
	s := listOne(t, []Binding{
		{Adapter: "full", Operation: "activity", Priority: 200, FanOut: true},
		{Adapter: "lake", Operation: "activity", Priority: 100, FanOut: true},
	}, map[string]Adapter{"lake": lake, "full": full})
	want := Limitation{Adapter: "lake", Capability: credFilter.Capability, Reason: credFilter.Reason}
	if len(s.Limitations) != 1 || s.Limitations[0] != want {
		t.Errorf("limitations = %+v; want [%+v]", s.Limitations, want)
	}
}

// TestListCapabilitiesLimitations_FanOutBothDegraded: two fan-out bindings
// reporting the same capability give one attributed entry each.
func TestListCapabilitiesLimitations_FanOutBothDegraded(t *testing.T) {
	a := degradedAdapter("a", true, credFilter, credFilter)
	b := degradedAdapter("b", true, DegradedCapability{Operation: "search", Capability: credFilter.Capability, Reason: "other reason"})
	s := listOne(t, []Binding{
		{Adapter: "a", Operation: "activity", Priority: 200, FanOut: true},
		{Adapter: "b", Operation: "search", Priority: 100, FanOut: true},
	}, map[string]Adapter{"a": a, "b": b})
	want := []Limitation{
		{Adapter: "a", Capability: credFilter.Capability, Reason: credFilter.Reason},
		{Adapter: "b", Capability: credFilter.Capability, Reason: "other reason"},
	}
	if !slices.Equal(s.Limitations, want) {
		t.Errorf("limitations = %+v; want %+v", s.Limitations, want)
	}
}

// TestListCapabilitiesLimitations_UnhealthyIgnored: an unhealthy binding
// contributes no limitation, in priority order or in a fan-out.
func TestListCapabilitiesLimitations_UnhealthyIgnored(t *testing.T) {
	down := degradedAdapter("down", false, credFilter)
	lake := degradedAdapter("lake", true)
	s := listOne(t, []Binding{
		{Adapter: "down", Operation: "activity", Priority: 200},
		{Adapter: "lake", Operation: "activity", Priority: 100},
	}, map[string]Adapter{"down": down, "lake": lake})
	if s.Status != StatusAvailable || s.Limitations != nil {
		t.Errorf("priority: summary = %q %+v; want available, no limitations", s.Status, s.Limitations)
	}
	s = listOne(t, []Binding{
		{Adapter: "lake", Operation: "activity", Priority: 200, FanOut: true},
		{Adapter: "down", Operation: "activity", Priority: 100, FanOut: true},
	}, map[string]Adapter{"down": down, "lake": lake})
	if s.Limitations != nil {
		t.Errorf("fan-out: limitations = %+v; want none", s.Limitations)
	}
	downOnly := degradedAdapter("down", false, credFilter)
	s = listOne(t, []Binding{{Adapter: "down", Operation: "activity", Priority: 100}}, map[string]Adapter{"down": downOnly})
	if s.Status != StatusDegraded || s.Limitations != nil {
		t.Errorf("unhealthy only: summary = %q %+v; want degraded, no limitations", s.Status, s.Limitations)
	}
}

// TestListCapabilities_FallbackNotProbed: when the primary binding is healthy,
// the fallback adapter is never probed, by the listing or by AvailableVerbs,
// degraded primary or not; each adapter is probed at most once per listing.
func TestListCapabilities_FallbackNotProbed(t *testing.T) {
	primary := degradedAdapter("primary", true)
	fallback := degradedAdapter("fallback", true)
	res := newResolver(map[string][]Binding{
		"enumerate_logons": {
			{Adapter: "fallback", Operation: "activity", Priority: 100},
			{Adapter: "primary", Operation: "activity", Priority: 200},
		},
		"get_host_context": {{Adapter: "primary", Operation: "context", Priority: 100}},
	}, map[string]Adapter{"primary": primary, "fallback": fallback})
	res.ListCapabilities(DefaultCatalog())
	if primary.probes != 1 || fallback.probes != 0 {
		t.Errorf("listing probes: primary %d, fallback %d; want 1, 0", primary.probes, fallback.probes)
	}
	res.AvailableVerbs(DefaultCatalog())
	if primary.probes != 2 || fallback.probes != 0 {
		t.Errorf("AvailableVerbs probes: primary %d, fallback %d; want 2, 0", primary.probes, fallback.probes)
	}
	primary.degraded = []DegradedCapability{credFilter}
	res.ListCapabilities(DefaultCatalog())
	if fallback.probes != 0 {
		t.Errorf("fallback probes with a degraded primary = %d; want 0", fallback.probes)
	}
}

// TestCapabilitySummaryLimitationsWire: limitations travel on the
// /api/capabilities wire as `limitations: [{adapter, capability, reason}]` and are
// absent when there are none.
func TestCapabilitySummaryLimitationsWire(t *testing.T) {
	with, err := json.Marshal(CapabilitySummary{Status: StatusAvailable, Limitations: []Limitation{{Adapter: "a", Capability: "c", Reason: "r"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(with), `"limitations":[{"adapter":"a","capability":"c","reason":"r"}]`) {
		t.Errorf("summary with a limitation = %s", with)
	}
	without, err := json.Marshal(CapabilitySummary{Status: StatusAvailable})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), "limitations") {
		t.Errorf("summary without limitations carries the key: %s", without)
	}
}
