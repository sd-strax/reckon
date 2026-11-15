package identity

import (
	"net/netip"
	"strings"
)

// IPValue is the address an ipv4-addr or ipv6-addr stores for a value the
// telemetry gives (adr/0357). It is the one cleanup the normalizers write
// with and the readers match with (class rule R2):
//
//   - surrounding white space is trimmed;
//   - the text is netip's canonical form: an IPv6 address lowercased and
//     compressed, so "2001:DB8:0:0::1" and "2001:db8::1" are one value;
//   - an IPv4 address written as IPv4-mapped IPv6 (::ffff:10.0.0.5) is the
//     IPv4 address, in dotted-quad form, so it is the same object as that
//     address seen anywhere else;
//   - a CIDR block, which STIX 2.1 §6.8 and §6.9 allow as a value, is kept
//     in canonical prefix text, masked to its network address
//     (10.0.0.5/24 is 10.0.0.0/24), with the type of its address family. A /32
//     or a /128 is a single address and is stored as the bare address, since
//     the spec says the suffix MAY be omitted.
//
// typ is TypeIPv4Addr or TypeIPv6Addr. ok is false for a value that names no
// address STIX holds: one that does not parse, one with a zone (§6.9), or a
// placeholder (PlaceholderIP).
func IPValue(v string) (value, typ string, ok bool) {
	v = strings.TrimSpace(v)
	if strings.Contains(v, "/") {
		p, err := netip.ParsePrefix(v)
		if err != nil {
			return "", "", false
		}
		addr, bits := p.Addr(), p.Bits()
		if addr.Is4In6() && bits >= 96 {
			addr, bits = addr.Unmap(), bits-96
		}
		if bits == addr.BitLen() {
			if PlaceholderIP(addr.String()) {
				return "", "", false
			}
			return addr.String(), familyType(addr), true
		}
		// The block is its network: 10.0.0.5/24 and 10.0.0.0/24 name one
		// block, so they are one value and one id.
		block := netip.PrefixFrom(addr, bits).Masked()
		if PlaceholderIP(block.Addr().String()) {
			return "", "", false
		}
		return block.String(), familyType(addr), true
	}
	addr, err := netip.ParseAddr(v)
	if err != nil || addr.Zone() != "" || PlaceholderIP(v) {
		return "", "", false
	}
	addr = addr.Unmap()
	return addr.String(), familyType(addr), true
}

func familyType(a netip.Addr) string {
	if a.Is4() {
		return TypeIPv4Addr
	}
	return TypeIPv6Addr
}
