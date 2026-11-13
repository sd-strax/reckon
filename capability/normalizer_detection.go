package capability

import (
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
//     the same x-cloud-account and x-cloud-resource (adr/0356);
//   - emits an Indicator and a Sighting with derivation_mode = INFERRED,
//     attributed to a per-tenant vendor Identity via created_by_ref;
//   - does NOT wrap them in an x-interpretation (capability §1 / persistence
//     §2.1 forbid it — the agent loop does that on engagement).
type detectionNormalizer struct{ r *identity.Resolver }

func (*detectionNormalizer) ClassUID() int { return 2004 }

// 2: host identity, adr/0211; 3: account identity of the evidence, adr/0324
// 4: OCSF classes and fields, adr/0356
// 5: a cloud finding's account, resources and cloud principal, adr/0356
// 6: the finding's own host, and an unknown confidence left out, adr/0356
func (*detectionNormalizer) Version() int { return 6 }

func (n *detectionNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload

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

	// 3. Indicator — the vendor's claim. OCSF has no field for a detection's
	// pattern, so a vendor's pattern is its own field, unmapped.pattern.
	// Without one the Indicator carries no pattern.
	uid := pathStr(p, "finding_info.uid")
	title := pathStr(p, "finding_info.title")
	pattern := pathStr(p, "unmapped.pattern")
	conf := detectionConfidence(p)
	var types []string
	if v, ok := lookupPath(p, "finding_info.types"); ok {
		types = stringSlice(v)
	}

	indID := n.r.Indicator(uid, pattern)
	patternType := "vendor"
	if pattern == "" {
		patternType = ""
	}
	result.Indicators = append(result.Indicators, Indicator{
		ID:             indID,
		Name:           title,
		Pattern:        pattern,
		PatternType:    patternType,
		IndicatorTypes: types,
		ValidFrom:      evt.Time,
		Confidence:     conf,
		CreatedByRef:   vendorID,
		Provenance:     prov,
	})

	// 4. Sighting — the indicator seen in the observation above.
	result.Sightings = append(result.Sightings, Sighting{
		ID:               n.r.Sighting(indID, evt.Time, vendor),
		SightingOfRef:    indID,
		ObservedDataRefs: odRefs,
		Confidence:       conf,
		Description:      title,
		CreatedByRef:     vendorID,
		Provenance:       prov,
	})

	return result, nil
}

// evidence reads one artifact of a detection's evidences[] with the shared
// extractors (§4.12, adr/0356):
//
//   - the host from the artifact's device, else from the finding's own device
//   - a process from process and from actor.process
//   - a file from file
//   - addresses and the destination's domain from the endpoints
//   - accounts from actor.user and from user
//   - a domain from query
//   - a URL from url
//
// At OCSF 1.1.0 an artifact has no device, user or url. They are read when
// present (03 §4.16).
//
// provider is the cloud provider the finding names, "" for none, and account
// the x-cloud-account the finding is about. With a provider, actor.user is
// that provider's cloud principal (§4.15), not a directory login: the alert
// then names the same user-account the provider's API calls do (adr/0356).
func (b *resultBuilder) evidence(artifact map[string]any, provider string, account identity.STIXID) {
	device := at{obj: artifact, path: "device"}
	if objectAt(artifact, "device") == nil {
		device = b.top("device")
	}
	b.devices = []at{device, {obj: artifact, path: "src_endpoint"}, {obj: artifact, path: "dst_endpoint"}}
	host := b.hostAt(device, true)

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
