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
	"regexp"
	"slices"
	"strconv"
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

// --- accounts and persons (adr/0324) ----------------------------------------

// sidForm is a Windows security identifier as text: "S-1-", the identifier
// authority and one or more sub-authorities, decimal; case-insensitive.
var sidForm = regexp.MustCompile(`^[sS]-1-[0-9]+(-[0-9]+)+$`)

// IsSID reports whether v, trimmed, has the textual form of a Windows SID
// (S-1-<authority>-<sub>...). It says nothing about whether the SID names a
// principal; PlaceholderSID does.
func IsSID(v string) bool { return sidForm.MatchString(strings.TrimSpace(v)) }

// placeholderSIDs are the SIDs, uppercased, that name no principal: S-1-0-0
// (Nobody, the null SID) and S-1-5-7 (Anonymous Logon). Every record that
// carries one would otherwise make one account of every caller it describes.
var placeholderSIDs = []string{"S-1-0-0", "S-1-5-7"}

// PlaceholderSID reports whether v is no SID that names a principal: a value
// that does not have the SID form (empty, a dash, "unknown"), the null SID
// S-1-0-0 or the anonymous SID S-1-5-7. A well-known principal's SID (the
// system, local and network service, a built-in group) is not a placeholder:
// it names a principal per host (WellKnownLocalSID).
func PlaceholderSID(v string) bool {
	v = strings.TrimSpace(v)
	if !IsSID(v) {
		return true
	}
	return slices.Contains(placeholderSIDs, strings.ToUpper(v))
}

// wellKnownLocalPrefixes are the authorities and NT-authority sub-authorities
// whose every SID names a principal of the machine itself: the local (2) and
// creator (3) authorities, mandatory integrity labels (16), app containers
// and capabilities (15-2), built-in groups (5-32), authentication packages
// (5-64), service SIDs (5-80), IIS application pool identities (5-82),
// virtual machine SIDs (5-83), user-mode drivers (5-84), Window Manager
// (5-90) and font driver host (5-96).
var wellKnownLocalPrefixes = []string{
	"S-1-2-", "S-1-3-", "S-1-16-", "S-1-15-2-",
	"S-1-5-32-", "S-1-5-64-", "S-1-5-80-", "S-1-5-82-", "S-1-5-83-", "S-1-5-84-", "S-1-5-90-", "S-1-5-96-",
}

// wellKnownLocalSIDs are single SIDs of the machine's own principals outside
// those families: Everyone, and the local account groups (S-1-5-113 Local
// account, S-1-5-114 Local account and member of Administrators).
var wellKnownLocalSIDs = []string{"S-1-1-0", "S-1-5-113", "S-1-5-114"}

// WellKnownLocalSID reports whether v is the SID of a principal every Windows
// machine has for itself: Everyone (S-1-1-0), the local account groups
// (S-1-5-113, S-1-5-114), the local, creator, integrity-label and
// app-container families (S-1-2-*, S-1-3-*, S-1-16-*, S-1-15-2-*); an
// NT-authority principal with a single sub-authority below 21 (S-1-5-18
// Local System, S-1-5-19 Local Service, S-1-5-20 Network Service, S-1-5-4
// Interactive, ...); a built-in group (S-1-5-32-*); an authentication
// package, service, IIS application pool, virtual machine, user-mode driver,
// Window Manager or font driver host identity (S-1-5-64-*, -80-*, -82-*,
// -83-*, -84-*, -90-*, -96-*). Such an account is scoped by the host it runs
// on (windows-local), never a person (adr/0324). A placeholder SID (S-1-5-7,
// anonymous) is none: PlaceholderSID refuses it first.
func WellKnownLocalSID(v string) bool {
	s := strings.ToUpper(strings.TrimSpace(v))
	if !IsSID(s) || PlaceholderSID(s) {
		return false
	}
	if slices.Contains(wellKnownLocalSIDs, s) {
		return true
	}
	if rest, ok := strings.CutPrefix(s, "S-1-5-"); ok && !strings.Contains(rest, "-") {
		n, err := strconv.Atoi(rest)
		return err == nil && n < 21
	}
	for _, p := range wellKnownLocalPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// placeholderLogins are the folded logins, beside the hostname placeholders,
// that a source writes when it names no one.
var placeholderLogins = []string{"anonymous logon"}

// PlaceholderLogin reports whether v, trimmed and lowercased, is a login a
// source writes when it has none: empty, one of the hostname placeholders
// ("-", "?", "unknown", "n/a", "none", "null", "localhost") or "anonymous
// logon". A real but common login ("administrator", "root") is not one.
func PlaceholderLogin(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	return slices.Contains(placeholderHostnames, s) || slices.Contains(placeholderLogins, s)
}

// PlaceholderUserID reports whether v names no user or person: the device id
// placeholders (empty, the nil GUID, "0", "-1", a dash, "unknown", ...), or a
// value of the SID form that PlaceholderSID refuses. A value that is neither
// a SID nor a placeholder (a GUID, a provider id, an ARN) is an identifier.
func PlaceholderUserID(v string) bool {
	if PlaceholderDeviceUID(v) {
		return true
	}
	return IsSID(v) && PlaceholderSID(v)
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
