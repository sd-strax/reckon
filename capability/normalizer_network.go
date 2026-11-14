package capability

import "github.com/sd-strax/reckon/identity"

// networkNormalizer handles OCSF network_activity (class_uid 4001) → STIX
// (§4.3): source and destination IPs, an optional destination domain (with a
// resolves-to edge to the dst IP), a network-traffic SCO wrapping the
// connection, and the actor process and host if reported.
type networkNormalizer struct{ r *identity.Resolver }

func (*networkNormalizer) ClassUID() int { return 4001 }

// 2: host identity, adr/0211
// 3: OCSF classes and fields, adr/0356
func (*networkNormalizer) Version() int { return 3 }

func (n *networkNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "network_activity", n.Version())

	src, dst := b.endpoints(p)

	if src != "" || dst != "" {
		srcPort := pathInt(p, "src_endpoint.port")
		dstPort := pathInt(p, "dst_endpoint.port")
		proto := pathStr(p, "connection_info.protocol_name")
		b.addSCO(n.r.NetworkTraffic(src, dst, srcPort, dstPort, proto), identity.TypeNetworkTraffic, map[string]any{
			"src_ref":   string(src),
			"dst_ref":   string(dst),
			"src_port":  srcPort,
			"dst_port":  dstPort,
			"protocols": proto,
		})
	}

	// The process that made the connection is the actor's (adr/0356).
	b.actorProcess(p, b.host("device"))

	return b.finish(nil), nil
}
