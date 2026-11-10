package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/idna"
)

// canonicalIP normalizes an IP address string: IPv4 without leading zeros,
// IPv6 lowercased and compressed to its canonical form. An unparseable value
// falls back to a trimmed lowercase string so identity is still deterministic.
func canonicalIP(v string) string {
	s := strings.TrimSpace(v)
	ip := net.ParseIP(s)
	if ip == nil {
		return strings.ToLower(s)
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String() // net.IP.String already yields lowercase compressed IPv6
}

// canonicalDomain normalizes a domain name: lowercased, trailing dots stripped,
// IDN punycoded (ASCII/A-label). Punycoding failures fall back to the
// lowercased form.
func canonicalDomain(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	s = strings.TrimRight(s, ".")
	if s == "" {
		return s
	}
	if ascii, err := idna.Lookup.ToASCII(s); err == nil {
		return ascii
	}
	return s
}

// canonicalURL normalizes a URL: scheme and host lowercased, default ports
// stripped, query-string keys sorted, fragment preserved. Unparseable values
// fall back to a trimmed string.
func canonicalURL(v string) string {
	s := strings.TrimSpace(v)
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	u.Scheme = strings.ToLower(u.Scheme)

	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && !isDefaultPort(u.Scheme, port) {
		u.Host = net.JoinHostPort(host, port) // re-brackets IPv6 literals
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]" // IPv6 literal without a port keeps its brackets
	} else {
		u.Host = host
	}

	if u.RawQuery != "" {
		q := u.Query()
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for i, k := range keys {
			vals := q[k]
			sort.Strings(vals)
			for j, val := range vals {
				if i > 0 || j > 0 {
					b.WriteByte('&')
				}
				b.WriteString(url.QueryEscape(k))
				b.WriteByte('=')
				b.WriteString(url.QueryEscape(val))
			}
		}
		u.RawQuery = b.String()
	}
	return u.String()
}

// FoldMAC folds a MAC address to its bare lowercase hex digits, or returns ""
// when v is not in an accepted form. The forms are a fixed rule of identity,
// not whatever the standard library parses, so a MAC mints the same host on
// every toolchain. After trimming, exactly these are accepted, in either case:
// six bytes as xx:xx:xx:xx:xx:xx or xx-xx-xx-xx-xx-xx, dotted
// xxxx.xxxx.xxxx, or 12 bare hex digits; eight bytes (EUI-64) as
// colon- or dash-separated pairs, or 16 bare hex digits. One separator
// throughout. The evidence store's seen-as query applies the same forms in
// SQL; the two must agree.
func FoldMAC(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	var parts []string
	switch {
	case len(s) == 12 || len(s) == 16:
		parts = []string{s}
	case len(s) == 14 && s[4] == '.':
		parts = strings.Split(s, ".")
		if len(parts) != 3 || !allLen(parts, 4) {
			return ""
		}
	case len(s) > 2 && (s[2] == ':' || s[2] == '-'):
		parts = strings.Split(s, s[2:3])
		if (len(parts) != 6 && len(parts) != 8) || !allLen(parts, 2) {
			return ""
		}
	default:
		return ""
	}
	digits := strings.Join(parts, "")
	if strings.Trim(digits, "0123456789abcdef") != "" {
		return ""
	}
	return digits
}

// allLen reports whether every part is n bytes long.
func allLen(parts []string, n int) bool {
	for _, p := range parts {
		if len(p) != n {
			return false
		}
	}
	return true
}

// canonicalMAC formats a MAC from its fold (FoldMAC) as lowercase
// colon-separated pairs, so the bare, dashed, dotted and colon forms of one
// address are one value. A value in no accepted form falls back to its
// lowercased input; Host never mints on one (PlaceholderMAC refuses it).
func canonicalMAC(v string) string {
	digits := FoldMAC(v)
	if digits == "" {
		return strings.ToLower(strings.TrimSpace(v))
	}
	pairs := make([]string, 0, len(digits)/2)
	for i := 0; i < len(digits); i += 2 {
		pairs = append(pairs, digits[i:i+2])
	}
	return strings.Join(pairs, ":")
}

// canonicalHostname folds a hostname to the short name that is an x-host's
// identity (adr/0211): trimmed, lowercased, trailing dots removed, cut at the
// first dot; an IP literal in the hostname field is kept whole. It returns ""
// when nothing remains.
func canonicalHostname(v string) string {
	host, _ := SplitHostname(v)
	return host
}

// SplitHostname splits a reported hostname into the lowercase short name and
// the lowercase remainder after the first dot, the DNS domain. Both are ""
// when the value is empty; the domain is "" for a short name. Callers that
// keep the domain as a property use it; identity uses the short name only.
// A source with no resolved name may put the address in the hostname field:
// a value that is not a hostname is returned whole with no domain, never split
// at its dots, so two addresses stay two hosts. That is a value that parses as
// an IP, consists only of digits and dots, or contains a colon: a legal
// hostname is never all digits and dots, and a colon never belongs in one.
// The evidence store's seen-as query applies the same rule in SQL; the two
// must agree.
func SplitHostname(v string) (host, domain string) {
	s := strings.TrimRight(strings.ToLower(strings.TrimSpace(v)), ".")
	if notHostname(s) {
		return s, ""
	}
	host, domain, _ = strings.Cut(s, ".")
	return host, domain
}

// notHostname reports whether a folded hostname-field value is an address or
// other literal kept whole: an IP, only digits and dots, or any colon.
func notHostname(s string) bool {
	if s == "" {
		return false
	}
	if net.ParseIP(s) != nil || strings.Contains(s, ":") {
		return true
	}
	return strings.Trim(s, "0123456789.") == ""
}

// isDefaultPort reports whether port is the scheme's default (stripped from the
// canonical URL).
func isDefaultPort(scheme, port string) bool {
	switch scheme {
	case "http", "ws":
		return port == "80"
	case "https", "wss":
		return port == "443"
	case "ftp":
		return port == "21"
	default:
		return false
	}
}

// canonicalJSON serializes v deterministically for hashing. json.Marshal sorts
// map keys and preserves struct field order, so the same logical value always
// produces the same bytes. The identity-contributing values passed here are
// always JSON-serializable (strings, ints, and nested maps of the same), so the
// error path is unreachable in practice; it falls back to fmt.Sprintf only to
// keep the function total and panic-free.
func canonicalJSON(v any) []byte {
	if b, err := json.Marshal(v); err == nil {
		return b
	}
	return []byte(fmt.Sprintf("%v", v))
}

// contentHash returns the lowercase hex SHA-256 of v's canonical JSON. Used for
// ObservedData identity (hashing the OCSF payload).
func contentHash(v any) string {
	sum := sha256.Sum256(canonicalJSON(v))
	return hex.EncodeToString(sum[:])
}
