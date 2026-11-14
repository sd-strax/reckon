package capability

import "github.com/sd-strax/reckon/identity"

// processNormalizer handles OCSF process_activity (class_uid 1007) → STIX (§4.1):
// the process, its parent, the image file, the host, and the executing user,
// grouped under one ObservedData, with a parent-process-of relationship.
type processNormalizer struct{ r *identity.Resolver }

func (*processNormalizer) ClassUID() int { return 1007 }

// 2: host identity, adr/0211; 3: account identity, adr/0324
// 4: OCSF classes and fields, adr/0356
func (*processNormalizer) Version() int { return 4 }

func (n *processNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	b := newBuilder(n.r, evt, "process_activity", n.Version())
	b.deviceObjects("device")

	host := b.host("device")
	b.userAccount("actor.user", "device", host)
	// The subject process, its image and its parent.
	b.process(evt.Payload, "process", host)

	return b.finish(nil), nil
}
