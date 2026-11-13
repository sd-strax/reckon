package capability

import "github.com/sd-strax/reckon/identity"

// incidentNormalizer handles OCSF incident_finding (class_uid 2005) → an
// ObservedData wrapping an external case reference (§2.9). DIRECT derivation:
// unlike the detection normalizer (2004 → INFERRED Indicator), a case is a
// plain observation. It extracts no SCOs, since a ticket system's incidents
// carry no structured entity fields in v0. The case metadata
// (number/title/status/summary/link) rides in the ObservedData Extensions.
type incidentNormalizer struct{ r *identity.Resolver }

func (*incidentNormalizer) ClassUID() int { return 2005 }

// 2: OCSF classes and fields, adr/0356
func (*incidentNormalizer) Version() int { return 2 }

func (n *incidentNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	ext := map[string]any{}
	put := func(k, v string) {
		if v != "" {
			ext[k] = v
		}
	}
	// The findings the case is about. The first entry names the case, and
	// every entry's uid is kept.
	var uids []string
	for i, f := range objectsAt(p, "finding_info_list") {
		uid := toString(fieldOf(f, "uid"))
		if uid != "" {
			uids = append(uids, uid)
		}
		if i == 0 {
			put("case_number", uid)
			put("title", toString(fieldOf(f, "title")))
			put("summary", toString(fieldOf(f, "desc")))
		}
	}
	if len(uids) > 1 {
		ext["finding_uids"] = uids
	}
	put("status", pathStr(p, "status"))
	// priority is a number at OCSF 1.1.0 and a caption later.
	if v, ok := lookupPath(p, "priority"); ok {
		put("priority", scalarString(v))
	}
	put("assignment_group", firstNonEmpty(pathStr(p, "assignee_group.name"), pathStr(p, "assignee_group.uid")))
	put("link", pathStr(p, "src_url"))
	// OCSF has no field for the ticket system's name.
	put("sor", pathStr(p, "unmapped.sor"))
	if len(ext) == 0 {
		ext = nil
	}
	return newBuilder(n.r, evt, "incident_finding", n.Version()).finish(ext), nil
}
