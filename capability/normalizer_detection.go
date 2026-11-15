package capability

import (
	"regexp"
	"slices"
	"strings"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsf"
)

// detectionNormalizer handles OCSF detection_finding (class_uid 2004) → STIX
// (§4.12) — the single INFERRED path in the capability layer. A vendor detection
// is a claim, not a passive observation, so this normalizer:
//
//   - reads the finding's own device as the host, whether or not the finding
//     has evidences[] (adr/0356);
//   - reads each artifact in evidences[] with the extractors the activity
//     normalizers use, producing the DIRECT entities under one ObservedData.
//     An artifact is not an event and has no class, so none is sent through
//     the registry (adr/0356);
//   - reads the finding's own cloud and resources[] by the API-activity rules
//     (§4.15), so a cloud alert and the API calls about one resource meet on
//     the same reckon-cloud-account and reckon-cloud-resource (adr/0356);
//   - emits an Indicator and a Sighting with derivation_mode = INFERRED,
//     attributed to a per-tenant vendor Identity via created_by_ref. The
//     vendor is also where the indicator was sighted. The ATT&CK techniques
//     the finding names are the indicator's external_references, and its
//     other types are indicator_types or labels (adr/0357);
//   - does NOT wrap them in a reckon-interpretation (capability §1 / persistence
//     §2.1 forbid it — the agent loop does that on engagement).
type detectionNormalizer struct{ r *identity.Resolver }

func (*detectionNormalizer) ClassUID() int { return 2004 }

// 2: host identity, adr/0211; 3: account identity of the evidence, adr/0324
// 4: OCSF classes and fields, adr/0356
// 5: a cloud finding's account, resources and cloud principal, adr/0356
// 6: the finding's own host, and an unknown confidence left out, adr/0356
// 7: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; the vendor in
// where_sighted_refs, ATT&CK ids in external_references, other types as
// labels, a pattern always, the indicator id from its vendor, pattern, type
// and uid, the sighting id from its time and observations, adr/0357
func (*detectionNormalizer) Version() int { return 7 }

func (n *detectionNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload

	// OCSF has no field for a detection's pattern, so a vendor's pattern is
	// its own field, unmapped.pattern. STIX requires a pattern. Without the
	// vendor's, the pattern is the finding's rule reference: its uid, or
	// else its title. OCSF requires the uid at every version, and the title
	// from 1.1.0 to 1.3.0 (recommended from 1.4.0). So a finding with
	// neither, and no pattern, is a broken record (adr/0357).
	// Each is trimmed first, so a value of white space alone is absent
	// everywhere: in this check, in the pattern and in the id (adr/0357).
	uid := strings.TrimSpace(pathStr(p, "finding_info.uid"))
	title := strings.TrimSpace(pathStr(p, "finding_info.title"))
	vendorPattern := strings.TrimSpace(pathStr(p, "unmapped.pattern"))
	if vendorPattern == "" && uid == "" && title == "" {
		return NormalizationResult{}, ErrNoRuleReference
	}

	// 1. The evidence: DIRECT entities under the detection event's own
	// ObservedData. §4.12 requires an ObservedData preserving the raw "this
	// was seen" signal separate from the Sighting, so a finding with no
	// evidence still gets one. The finding's own device names the host the
	// detection fired on, with or without evidences[].
	b := newBuilder(n.r, evt, "detection_finding", n.Version())
	b.deviceObjects("device")
	b.host("device")
	// A cloud finding names its account and resources on the finding itself.
	// They are read as an API call's are (§4.15). With no provider there is
	// no namespace for a cloud identity, so none is minted.
	provider := providerSlug(pathStr(p, "cloud.provider"))
	var account identity.STIXID
	if provider != "" {
		account = b.cloudAccount(provider, at{})
		b.cloudResourcesOf(provider, account)
	}
	for _, artifact := range objectsAt(p, "evidences") {
		b.evidence(artifact, provider, account)
	}
	result := b.finish(nil)
	// The sighting names the observation. The entities the alert is about
	// are that observation's object_refs. They do not go in
	// where_sighted_refs, where STIX allows only identity and location
	// objects (adr/0356).
	var odRefs []identity.STIXID
	for _, od := range result.ObservedData {
		odRefs = append(odRefs, od.ID)
	}

	// 2. Vendor Identity (created_by_ref target), auto-created per tenant.
	vendor := detectionVendor(p, evt.SourceTool)
	vendorID := n.r.Identity(vendor)
	result.Identities = append(result.Identities, VendorIdentity{ID: vendorID, Name: vendor})

	prov := Provenance{
		DerivationMode:    DerivationInferred,
		Tool:              vendor,
		Normalizer:        "detection_finding",
		NormalizerVersion: n.Version(),
		ObservedAt:        evt.Time,
	}

	// 3. Indicator — the vendor's claim. When the pattern is the rule
	// reference, the pattern type is the tool's name. The finding uid is
	// stored as an external reference under the vendor's name, where STIX
	// puts a source's own id for an object. The id is minted from what the
	// indicator stores: the vendor, the pattern, the pattern_type and that
	// uid (adr/0357).
	conf := detectionConfidence(p)
	var types []string
	if v, ok := lookupPath(p, "finding_info.types"); ok {
		types = stringSlice(v)
	}

	pattern, patternType := vendorPattern, "vendor"
	if pattern == "" {
		pattern, patternType = firstNonEmpty(uid, title), vendor
	}
	refs := attackReferences(p, types)
	if uid != "" {
		refs = append(refs, ExternalReference{SourceName: vendor, ExternalID: uid})
	}
	indID := n.r.Indicator(vendorID, pattern, patternType, uid)
	result.Indicators = append(result.Indicators, Indicator{
		ID:                 indID,
		Name:               title,
		Pattern:            pattern,
		PatternType:        patternType,
		IndicatorTypes:     indicatorTypes(types),
		Labels:             findingLabels(types),
		ExternalReferences: refs,
		ValidFrom:          evt.Time,
		Confidence:         conf,
		CreatedByRef:       vendorID,
		Provenance:         prov,
	})

	// 4. Sighting — the indicator seen in the observation above, by the
	// vendor. STIX's where_sighted_refs names who saw it (adr/0357).
	result.Sightings = append(result.Sightings, Sighting{
		ID:               n.r.Sighting(indID, evt.Time, vendor, odRefs),
		SightingOfRef:    indID,
		ObservedDataRefs: odRefs,
		WhereSighted:     []identity.STIXID{vendorID},
		Confidence:       conf,
		Description:      title,
		CreatedByRef:     vendorID,
		SeenAt:           evt.Time,
		Provenance:       prov,
	})

	return result, nil
}

// evidence reads one artifact of a detection's evidences[] with the shared
// extractors (§4.12, adr/0356):
//
//   - the host from the artifact's device, else, when it has none or its
//     device names no host, from the finding's own device
//   - a process from process and from actor.process
//   - a file from file
//   - addresses and the destination's domain from the endpoints
//   - accounts from actor.user and from user
//   - a domain from query
//   - a URL from url
//
// At OCSF 1.1.0 and 1.2.0 an artifact has no device, user or url. They are
// read when present (03 §4.16).
//
// provider is the cloud provider the finding names, "" for none, and account
// the reckon-cloud-account the finding is about. With a provider, actor.user is
// that provider's cloud principal (§4.15), not a directory login: the alert
// then names the same user-account the provider's API calls do (adr/0356).
func (b *resultBuilder) evidence(artifact map[string]any, provider string, account identity.STIXID) {
	own := at{obj: artifact, path: "device"}
	device := own
	b.devices = []at{own, {obj: artifact, path: "src_endpoint"}, {obj: artifact, path: "dst_endpoint"}}
	var host identity.STIXID
	if objectAt(artifact, "device") != nil {
		host = b.hostAt(own, true)
	}
	if host == "" {
		// No device, or one that names no host (an address alone): the
		// finding's own device is the host. What the artifact's device does
		// say, its OS for one, still counts.
		device = b.top("device")
		b.devices = append([]at{device}, b.devices...)
		host = b.hostAt(device, true)
	}

	if objectAt(artifact, "process") != nil {
		b.process(artifact, "process", host)
	}
	b.actorProcess(artifact, host)
	b.fileAt(artifact, "file")
	b.endpoints(artifact)
	if provider != "" {
		actor := at{obj: artifact, path: "actor"}
		b.cloudHome(provider, b.cloudPrincipal(provider, actor), account, actor)
	} else {
		b.userAccountAt(at{obj: artifact, path: "actor.user"}, device, host)
	}
	b.userAccountAt(at{obj: artifact, path: "user"}, device, host)
	b.addDomain(pathStr(artifact, "query.hostname"))
	b.url(pathStr(artifact, "url.url_string"))
}

// indicatorTypeOV is STIX 2.1's indicator-type-ov (§10.13).
var indicatorTypeOV = []string{
	"anomalous-activity", "anonymization", "benign", "compromised",
	"malicious-activity", "attribution", "unknown",
}

// indicatorTypes keeps the finding types that are values of STIX's
// indicator-type-ov, in their vocabulary form. A technique id, or any other
// label, is not an indicator type and is dropped (adr/0357).
func indicatorTypes(types []string) []string {
	var out []string
	for _, t := range types {
		t = strings.ToLower(strings.TrimSpace(t))
		if slices.Contains(indicatorTypeOV, t) && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// findingLabels keeps the finding types that are neither technique ids nor
// indicator types, such as a vendor's tag or tactic name, as the indicator's
// STIX labels: terms that describe it (adr/0357). Each is kept once, in
// reading order.
func findingLabels(types []string) []string {
	var out []string
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t == "" || slices.Contains(out, t) || techniqueID.MatchString(strings.ToUpper(t)) ||
			slices.Contains(indicatorTypeOV, strings.ToLower(t)) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// techniqueID matches an ATT&CK technique or sub-technique id, such as T1059
// or T1059.001.
var techniqueID = regexp.MustCompile(`^T\d{4}(\.\d{3})?$`)

// attackReferences reads the ATT&CK techniques a finding names, as STIX
// external references under mitre-attack (adr/0357). They come from two
// places, in this order:
//
//   - the values of finding_info.types[] that are technique ids
//   - OCSF's ATT&CK objects, attacks[] on the finding and on finding_info,
//     each with its technique.uid and sub_technique.uid
//
// Both lists of ATT&CK objects exist from 1.1.0 to 1.9.0. An id seen twice is
// kept once, at its first place. Ids that are not ATT&CK technique ids, such
// as ATLAS ids, are left out.
func attackReferences(p map[string]any, types []string) []ExternalReference {
	var out []ExternalReference
	seen := map[string]bool{}
	add := func(v string) {
		v = strings.ToUpper(strings.TrimSpace(v))
		if !techniqueID.MatchString(v) || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, ExternalReference{SourceName: SourceMITREATTACK, ExternalID: v})
	}
	for _, t := range types {
		add(t)
	}
	for _, path := range []string{"attacks", "finding_info.attacks"} {
		for _, a := range objectsAt(p, path) {
			add(pathStr(a, "technique.uid"))
			add(pathStr(a, "sub_technique.uid"))
		}
	}
	return out
}

// detectionVendor picks the vendor name for attribution, preferring OCSF
// metadata.product over the source tool.
func detectionVendor(p map[string]any, sourceTool string) string {
	return firstNonEmpty(
		pathStr(p, "metadata.product.vendor_name"),
		pathStr(p, "metadata.product.name"),
		sourceTool,
	)
}

// detectionConfidence reads a finding's confidence as a 0–100 STIX
// confidence (adr/0356):
//
//   - confidence_score, capped to 0–100. A score of 0 is kept: the vendor
//     said it has no confidence.
//   - else confidence_id: 1 Low, 2 Medium and 3 High become 15, 50 and 85.
//   - else nil. The confidence is unknown and is left out, never written
//     as 0.
func detectionConfidence(p map[string]any) *int {
	if v, ok := lookupPath(p, "confidence_score"); ok {
		if score, ok := ocsf.WholeNumber(v); ok {
			score = min(max(score, 0), 100)
			return &score
		}
	}
	level, ok := confidenceLevels[ocsf.Confidences[pathInt(p, "confidence_id")]]
	if !ok {
		return nil
	}
	return &level
}

// confidenceLevels is the STIX confidence for each OCSF confidence level, by
// the level's caption.
var confidenceLevels = map[string]int{"Low": 15, "Medium": 50, "High": 85}

// stringSlice coerces a value to a []string, tolerating an []any of strings.
func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s := toString(item); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
