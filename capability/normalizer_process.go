package capability

import "github.com/sd-strax/reckon/identity"

// processNormalizer handles OCSF process_activity (class_uid 1007) → STIX (§4.1):
// the process, its parent, the image file, the host, and the executing user,
// grouped under one ObservedData, with a parent-process-of relationship.
type processNormalizer struct{ r *identity.Resolver }

func (*processNormalizer) ClassUID() int { return 1007 }

// 2: host identity, adr/0211; 3: account identity, adr/0324
// 4: OCSF classes and fields, adr/0356
// 5: OCSF read gaps, adr/0359
// 6: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; the start time
// stored, pids only when given, file hashes, names and sizes STIX can hold,
// adr/0357
// 7: an account typed Windows only on a Windows signal, account type 19
// read as one, adr/0366
// 8: the account type read by the one rule, a caption for every type and
// the id over it, adr/0367
// 9: the account minted for each user object recorded in user_accounts,
// adr/0398
func (*processNormalizer) Version() int { return 9 }

func (n *processNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	b := newBuilder(n.r, evt, "process_activity", n.Version())
	b.deviceObjects("device")

	host := b.host("device")
	b.userAccount("actor.user", "device", host)
	// The subject process, its image and its parent.
	b.process(evt.Payload, "process", host)

	return b.finish(nil), nil
}
