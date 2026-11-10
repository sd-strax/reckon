package identity

// Placeholders (adr/0211). A source emits a value when it has none: a zero
// MAC, an unspecified address, "localhost", the nil GUID, the hash of the
// empty file. A placeholder names nothing, so it is refused as an identity
// (the field reads as absent) and ignored as a discriminator in the seen-as
// view. The list is built in and deliberately short: only values that cannot
// be an identifier, never merely common ones, and never configuration, since
// a per-install list would mint different ids for one record on two installs.

import (
	"net"
	"slices"
	"strings"
)

// PlaceholderMAC reports whether v names no network interface: a value in no
// form FoldMAC accepts, or one whose digits are all zero or all f. A zero MAC
// is what a source reports for an interface it cannot read, and all ones is
// the broadcast address; neither belongs to one machine.
func PlaceholderMAC(v string) bool {
	digits := FoldMAC(v)
	return digits == "" || strings.Trim(digits, "0") == "" || strings.Trim(digits, "f") == ""
}

// PlaceholderIP reports whether v names no reachable address: a value that
// does not parse, the unspecified address (0.0.0.0, ::), a loopback address
// (127/8, ::1) or the IPv4 broadcast 255.255.255.255. Each is the same value
// on every machine, so it identifies none; link-local and private addresses
// do identify a machine within a network and are not placeholders.
func PlaceholderIP(v string) bool {
	ip := net.ParseIP(strings.TrimSpace(v))
	if ip == nil {
		return true
	}
	return ip.IsUnspecified() || ip.IsLoopback() || ip.Equal(net.IPv4bcast)
}

// placeholderHostnames are the folded short names a source writes when it has
// no hostname; "" is the absent name itself.
var placeholderHostnames = []string{"", "localhost", "-", "?", "unknown", "n/a", "none", "null"}

// PlaceholderHostnames returns the folded short names PlaceholderHostname
// refuses, for a caller that applies the same rule outside Go (the evidence
// store's seen-as query). The slice is a copy.
func PlaceholderHostnames() []string { return slices.Clone(placeholderHostnames) }

// PlaceholderHostname reports whether v, folded to its short name as
// SplitHostname folds it, is one a source writes when it has no hostname:
// empty, "localhost" (every machine's name for itself, so
// "localhost.localdomain" too), or a dash, question mark, "unknown", "n/a",
// "none" or "null". An address in the hostname field, which SplitHostname
// keeps whole, is judged as an address: it is a placeholder exactly when
// PlaceholderIP says so, so "127.0.0.1" or "::1" there names no host.
func PlaceholderHostname(v string) bool {
	short, _ := SplitHostname(v)
	if net.ParseIP(short) != nil {
		return PlaceholderIP(short)
	}
	return slices.Contains(placeholderHostnames, short)
}

// PlaceholderDomain reports whether v, trimmed, lowercased and with trailing
// dots removed, is a domain a source writes when it has none: empty or one of
// the hostname placeholders ("-", "unknown", "n/a", "localhost", ...). The
// value is compared whole, so "unknown.example" is a real domain.
func PlaceholderDomain(v string) bool {
	d := strings.TrimRight(strings.ToLower(strings.TrimSpace(v)), ".")
	return slices.Contains(placeholderHostnames, d)
}

// nilGUID is the all-zero GUID, a device id a source writes when the device
// has none.
const nilGUID = "00000000-0000-0000-0000-000000000000"

// placeholderUIDs are the device ids, lowercased, a source writes when it has
// none.
var placeholderUIDs = []string{"", nilGUID, "0", "-1", "-", "?", "unknown", "n/a", "none", "null"}

// PlaceholderDeviceUID reports whether v, trimmed and lowercased, is a device
// id a source writes when it has none: empty, the nil GUID (with or without
// braces), "0", "-1", a dash, a question mark, "unknown", "n/a", "none" or
// "null". Every device lacking an id carries the same one, so it joins them
// all.
func PlaceholderDeviceUID(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return slices.Contains(placeholderUIDs, s)
}

// emptyDigests are the digests of the empty input, keyed by the algorithm
// name folded as pickHash folds it (lowercase, no dash).
var emptyDigests = map[string]string{
	"md5":    "d41d8cd98f00b204e9800998ecf8427e",
	"sha1":   "da39a3ee5e6b4b0d3255bfef95601890afd80709",
	"sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	"sha512": "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
}

// PlaceholderHash reports whether digest names no file content: an empty or
// all-zero digest, which a source writes when it did not hash the file, or the
// digest of the empty input for algo (MD5, SHA-1, SHA-256, SHA-512), which
// every empty file shares. algo is matched case-insensitively with or without
// a dash; for an algorithm not listed only the all-zero case applies.
func PlaceholderHash(algo, digest string) bool {
	d := strings.ToLower(strings.TrimSpace(digest))
	if strings.Trim(d, "0") == "" {
		return true
	}
	return emptyDigests[foldAlgo(algo)] == d
}

// foldAlgo folds a hash algorithm name to a bare lowercase name, so "SHA-256",
// "sha256" and "SHA256" are one.
func foldAlgo(algo string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(algo), "-", ""))
}
