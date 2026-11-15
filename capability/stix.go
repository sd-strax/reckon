package capability

import (
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/stix"
)

// The methods in this file hand each normalizer output to the stix
// serializer (adr/0357). They fill the object's properties and its
// property-extensions. The store that writes the object sets created and
// modified on an SDO or an SRO, because it decides what time the row
// carries.

// extension is the provenance as the reckon-provenance property-extension.
func (p Provenance) extension() map[string]map[string]any {
	return map[string]map[string]any{stix.ExtProvenance: {
		"derivation_mode":    string(p.DerivationMode),
		"tool":               p.Tool,
		"normalizer":         p.Normalizer,
		"normalizer_version": p.NormalizerVersion,
		"observed_at":        p.ObservedAt,
	}}
}

// STIXObject is the SCO as the serializer takes it. A person identity is an
// SDO that travels the SCO path, so it is no different here.
func (s SCO) STIXObject() stix.Object {
	return stix.Object{Type: s.Type, ID: string(s.ID), Properties: s.Properties, Extensions: s.Provenance.extension()}
}

// STIXObject is the observation as the serializer takes it. Its extensions
// map is the reckon-observation property-extension.
func (o ObservedData) STIXObject() stix.Object {
	ext := o.Provenance.extension()
	if len(o.Extensions) > 0 {
		ext[stix.ExtObservation] = o.Extensions
	}
	return stix.Object{
		Type: identity.TypeObservedData,
		ID:   string(o.ID),
		Properties: map[string]any{
			"first_observed":  o.FirstObserved,
			"last_observed":   o.LastObserved,
			"number_observed": o.NumberObserved,
			"object_refs":     o.ObjectRefs,
		},
		Extensions: ext,
	}
}

// STIXObject is the relationship as the serializer takes it.
func (r Relationship) STIXObject() stix.Object {
	return stix.Object{
		Type: "relationship",
		ID:   string(r.ID),
		Properties: map[string]any{
			"relationship_type": r.Type,
			"source_ref":        r.SourceRef,
			"target_ref":        r.TargetRef,
		},
		Extensions: r.Provenance.extension(),
	}
}

// STIXObject is the indicator as the serializer takes it.
func (i Indicator) STIXObject() stix.Object {
	return stix.Object{
		Type: "indicator",
		ID:   string(i.ID),
		Properties: map[string]any{
			"name":                i.Name,
			"pattern":             i.Pattern,
			"pattern_type":        i.PatternType,
			"indicator_types":     i.IndicatorTypes,
			"external_references": i.ExternalReferences,
			"labels":              i.Labels,
			"valid_from":          i.ValidFrom,
			"confidence":          i.Confidence,
			"created_by_ref":      i.CreatedByRef,
		},
		Extensions: i.Provenance.extension(),
	}
}

// STIXObject is the sighting as the serializer takes it.
func (s Sighting) STIXObject() stix.Object {
	return stix.Object{
		Type: "sighting",
		ID:   string(s.ID),
		Properties: map[string]any{
			"sighting_of_ref":    s.SightingOfRef,
			"observed_data_refs": s.ObservedDataRefs,
			"where_sighted_refs": s.WhereSighted,
			"confidence":         s.Confidence,
			"description":        s.Description,
			"created_by_ref":     s.CreatedByRef,
			"first_seen":         s.SeenAt,
			"last_seen":          s.SeenAt,
		},
		Extensions: s.Provenance.extension(),
	}
}

// STIXObject is the vendor identity as the serializer takes it: an
// organization.
func (v VendorIdentity) STIXObject() stix.Object {
	return stix.Object{
		Type:       identity.TypeIdentity,
		ID:         string(v.ID),
		Properties: map[string]any{"name": v.Name, "identity_class": "organization"},
	}
}

// STIXObject is the null observation as the serializer takes it.
func (n NullObservation) STIXObject() stix.Object {
	return stix.Object{
		Type: identity.TypeNullObservation,
		ID:   string(n.ID),
		Properties: map[string]any{
			"verb":        n.Verb,
			"subject":     n.Subject,
			"parameters":  n.Parameters,
			"window_from": n.WindowFrom,
			"window_to":   n.WindowTo,
			"sources":     n.Sources,
			"coverage":    n.Coverage,
			"object_refs": n.ObjectRefs,
			"observed_at": n.ObservedAt,
		},
		Extensions: n.Provenance.extension(),
	}
}
