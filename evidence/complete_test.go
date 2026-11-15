package evidence

import (
	"time"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
)

// testProvenance is the provenance a hand-built test object gets when it has
// none. The serializer refuses an object missing a property its schema
// requires (adr/0357), and a test object built for a store read is not about
// its provenance.
var testProvenance = capability.Provenance{
	DerivationMode: capability.DerivationDirect, Tool: "fixture:test", Normalizer: "test", NormalizerVersion: 1,
	ObservedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
}

// complete fills what a hand-built test result leaves out but STIX requires,
// as the normalizers always give it: provenance, an observation's times,
// count and one object, a person's name, an indicator's pattern. A value a
// test sets is kept, so no test's subject changes.
func complete(res capability.CapabilityResult) capability.CapabilityResult {
	prov := func(p capability.Provenance) capability.Provenance {
		if p.Tool == "" && p.DerivationMode == "" {
			return testProvenance
		}
		if p.Normalizer == "" {
			p.Normalizer, p.NormalizerVersion = testProvenance.Normalizer, testProvenance.NormalizerVersion
		}
		if p.DerivationMode == "" {
			p.DerivationMode = testProvenance.DerivationMode
		}
		if p.ObservedAt.IsZero() {
			p.ObservedAt = testProvenance.ObservedAt
		}
		return p
	}
	for i := range res.Normalized {
		n := &res.Normalized[i]
		for j := range n.ObservedData {
			od := &n.ObservedData[j]
			od.Provenance = prov(od.Provenance)
			if od.FirstObserved.IsZero() {
				od.FirstObserved = testProvenance.ObservedAt
			}
			if od.LastObserved.IsZero() {
				od.LastObserved = od.FirstObserved
			}
			if od.NumberObserved == 0 {
				od.NumberObserved = 1
			}
			if len(od.ObjectRefs) == 0 {
				od.ObjectRefs = []identity.STIXID{"artifact--00000000-0000-4000-8000-000000000000"}
			}
		}
		for j := range n.SCOs {
			s := &n.SCOs[j]
			s.Provenance = prov(s.Provenance)
			if s.Type == identity.TypeIdentity && s.Properties["name"] == nil {
				props := map[string]any{"name": "Test Person"}
				for k, v := range s.Properties {
					props[k] = v
				}
				s.Properties = props
			}
		}
		for j := range n.Relationships {
			n.Relationships[j].Provenance = prov(n.Relationships[j].Provenance)
		}
		for j := range n.Indicators {
			ind := &n.Indicators[j]
			ind.Provenance = prov(ind.Provenance)
			if ind.Pattern == "" {
				ind.Pattern, ind.PatternType = "test-rule", "vendor"
			}
			if ind.ValidFrom.IsZero() {
				ind.ValidFrom = testProvenance.ObservedAt
			}
		}
		for j := range n.Sightings {
			n.Sightings[j].Provenance = prov(n.Sightings[j].Provenance)
		}
		for j := range n.Identities {
			if n.Identities[j].Name == "" {
				n.Identities[j].Name = "test vendor"
			}
		}
	}
	return res
}
