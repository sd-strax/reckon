package capability

import (
	"strings"

	"github.com/sd-strax/reckon/identity"
)

// networkNormalizer handles OCSF network_activity (class_uid 4001) → STIX
// (§4.3): source and destination IPs, an optional destination domain (with a
// resolves-to edge to the dst IP), a network-traffic SCO wrapping the
// connection, and the actor process and host if reported.
type networkNormalizer struct{ r *identity.Resolver }

func (*networkNormalizer) ClassUID() int { return 4001 }

// 2: host identity, adr/0211
// 3: OCSF classes and fields, adr/0356
// 4: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; protocols as a
// list from the telemetry, ports only when given, the id from the stored
// values, addresses in canonical text, adr/0357
func (*networkNormalizer) Version() int { return 4 }

func (n *networkNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "network_activity", n.Version())

	src, dst := b.endpoints(p)

	if src != "" || dst != "" {
		srcPort, hasSrcPort := pathNumber(p, "src_endpoint.port", maxPort)
		dstPort, hasDstPort := pathNumber(p, "dst_endpoint.port", maxPort)
		proto := pathStr(p, "connection_info.protocol_name")
		props := map[string]any{
			"src_ref": string(src),
			"dst_ref": string(dst),
		}
		// A port the telemetry does not give is unknown, and left out. Port
		// 0 is a real value (adr/0357).
		var srcPortRef, dstPortRef *int
		if hasSrcPort {
			props["src_port"] = srcPort
			srcPortRef = &srcPort
		}
		if hasDstPort {
			props["dst_port"] = dstPort
			dstPortRef = &dstPort
		}
		var protocols []string
		protoNum, _ := pathNumber(p, "connection_info.protocol_num", maxProtocol)
		if name := protocolName(proto, protoNum, src, dst); name != "" {
			protocols = []string{name}
			props["protocols"] = protocols
		}
		// The id is minted from what the object stores: the ends, the ports
		// and the protocols, not the raw protocol_name (adr/0357).
		b.addSCO(n.r.NetworkTraffic(src, dst, srcPortRef, dstPortRef, protocols), identity.TypeNetworkTraffic, props)
	}

	// The process that made the connection is the actor's (adr/0356).
	b.actorProcess(p, b.host("device"))

	return b.finish(nil), nil
}

// ipProtocols names the IANA protocol numbers OCSF's protocol_num carries
// that reckon maps to a STIX protocol name.
var ipProtocols = map[int]string{1: "icmp", 6: "tcp", 17: "udp"}

// protocolName is the protocol for STIX protocols, which STIX requires: a
// lowercase name, such as "tcp" (adr/0357). It is OCSF's protocol_name when
// present. Else it is protocol_num's name. Else it is the address family of
// the endpoints, "ipv4" or "ipv6". It is "" only when the telemetry gives
// none of these.
func protocolName(name string, num int, src, dst identity.STIXID) string {
	if n := strings.ToLower(strings.TrimSpace(name)); n != "" {
		return n
	}
	if n, ok := ipProtocols[num]; ok {
		return n
	}
	for _, ref := range []identity.STIXID{src, dst} {
		switch {
		case strings.HasPrefix(string(ref), identity.TypeIPv4Addr+"--"):
			return "ipv4"
		case strings.HasPrefix(string(ref), identity.TypeIPv6Addr+"--"):
			return "ipv6"
		}
	}
	return ""
}
