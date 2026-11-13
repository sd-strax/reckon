package capability

import "github.com/sd-strax/reckon/identity"

// authNormalizer handles OCSF authentication (class_uid 3002) → STIX (§4.2):
// the account being authenticated, the destination host, and the source IP,
// grouped under one ObservedData that records logon_type/status in its
// extensions, with an x-authenticated-to relationship from the account to the
// host.
//
// The account being authenticated is the event's user, as OCSF defines it.
// actor.user is whoever started the logon. When present it is a separate
// account, the initiator, and nothing says it authenticated to the host
// (adr/0356).
type authNormalizer struct{ r *identity.Resolver }

func (*authNormalizer) ClassUID() int { return 3002 }

// 2: host identity, adr/0211; 3: account identity, adr/0324
// 4: OCSF classes and fields, adr/0356
// 5: the authenticated account is user, not actor.user, adr/0356
func (*authNormalizer) Version() int { return 5 }

func (n *authNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "authentication", n.Version())

	b.deviceObjects("device", "src_endpoint", "dst_endpoint")

	// The host first: a local account is scoped by the machine it logged on to.
	host := b.endpointHost("dst_endpoint")
	user := b.userAccount("user", "dst_endpoint", host)
	b.userAccount("actor.user", "dst_endpoint", host) // initiator
	b.ipAddr("src_endpoint.ip")

	b.addRel(RelAuthenticatedTo, user, host)

	// logon_type and status are authentication-specific signal that has no STIX
	// SCO home; they ride in the ObservedData extension (§4.2).
	ext := map[string]any{}
	if lt := pathStr(p, "logon_type"); lt != "" {
		ext["logon_type"] = lt
	}
	if st := pathStr(p, "status"); st != "" {
		ext["status"] = st
	}
	if len(ext) == 0 {
		ext = nil
	}

	return b.finish(ext), nil
}
