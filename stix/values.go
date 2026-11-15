package stix

import (
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

// The checks in this file say whether a value is one STIX 2.1 can hold
// (adr/0357). The normalizers use them to leave out a value STIX cannot hold,
// and the conformance test uses them for the formats its schema validator
// does not check.

// uriScheme is RFC 3986 §3.1: a letter, then letters, digits, "+", "-" and
// ".".
var uriScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*$`)

// uriChars are the characters RFC 3986 allows in a URI: the unreserved and
// reserved sets (§2.2, §2.3), with "%" for a percent-encoding (§2.1).
const uriChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~:/?#[]@!$&'()*+,;=%"

// IsURI reports whether s is an absolute URI as RFC 3986 defines it: a scheme,
// a colon and the rest, every character one RFC 3986 allows, and every "%"
// the start of a percent-encoding. STIX 2.1 §6.15.1 requires a url's value to
// be a URL in this sense, and its other URI properties use the same format.
func IsURI(s string) bool {
	scheme, rest, ok := strings.Cut(s, ":")
	if !ok || !uriScheme.MatchString(scheme) || rest == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !strings.ContainsRune(uriChars, rune(c)) {
			return false
		}
		if c == '%' && (i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2])) {
			return false
		}
	}
	_, err := url.Parse(s)
	return err == nil
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// IsAddrSpec reports whether s is an RFC 5322 addr-spec (§3.4.1): an address
// alone, with no display name, no enclosing angle brackets, no comment and no
// surrounding white space. STIX 2.1 §6.5.1 requires an email-addr's value to
// be one. net/mail parses the grammar. The checks here only keep a mailbox
// that is not an addr-spec from passing:
//
//   - the address is split at its last "@", since a domain cannot hold one,
//     while a quoted local part can (§3.2.4);
//   - "<" and ">" are allowed only inside a quoted local part, where they are
//     ordinary characters; anywhere else they enclose a mailbox.
func IsAddrSpec(s string) bool {
	if s == "" || strings.TrimSpace(s) != s || strings.ContainsAny(s, "\r\n") {
		return false
	}
	at := strings.LastIndex(s, "@")
	if at <= 0 || at == len(s)-1 {
		return false
	}
	local, domain := s[:at], s[at+1:]
	quoted := len(local) >= 2 && strings.HasPrefix(local, `"`) && strings.HasSuffix(local, `"`)
	if strings.ContainsAny(domain, "<> ") || (!quoted && strings.ContainsAny(local, "<>")) {
		return false
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Name != "" {
		return false
	}
	if quoted {
		// The parser returns a quoted local part unquoted; the domain must
		// still be the whole tail.
		return strings.HasSuffix(a.Address, "@"+domain)
	}
	return a.Address == s
}

// EmailAddress cleans up an email address read from telemetry and reports
// whether the result is an addr-spec (IsAddrSpec). Surrounding white space is
// trimmed. A mailbox with a display name, such as "Alice
// <alice@corp.example>", keeps its address, alice@corp.example. Both are the
// same address, so the cleanup invents nothing (adr/0357). Every reader that
// writes an email-addr, or matches a raw address against one, cleans it up
// here, so they agree on the value.
func EmailAddress(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	if IsAddrSpec(s) {
		return s, true
	}
	a, err := mail.ParseAddress(s)
	if err != nil || !IsAddrSpec(a.Address) {
		return "", false
	}
	return a.Address, true
}

// ldhLabel is a host name label (RFC 1034 §3.5, with RFC 1123 §2.1 allowing a
// digit first): letters, digits and inner hyphens, 1 to 63 characters.
var ldhLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// underscoreLabel is a label that holds an underscore, such as "_ldap" or
// "_domainkey", which DNS service and policy records use. Other than the
// underscore it follows the host name rule. reckon keeps a name with one, a
// deviation design/03 §7 lists (adr/0357).
var underscoreLabel = regexp.MustCompile(`^[A-Za-z0-9_]([A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?$`)

// IsDomainName reports whether s is a domain name STIX 2.1 §6.4.1 accepts: an
// RFC 1034 name whose labels conform to RFC 5890. A label may be an IDNA
// U-label, checked in its A-label form. A label may also hold an
// underscore, a deviation (design/03 §7). One trailing dot, RFC 1034's form
// of an absolute name, is allowed. The whole name is at most 253 characters
// in its A-label form.
func IsDomainName(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return false
	}
	labels := strings.Split(s, ".")
	total := len(labels) - 1
	for _, label := range labels {
		if strings.Contains(label, "_") && underscoreLabel.MatchString(label) {
			total += len(label)
			continue
		}
		ascii, err := idna.Lookup.ToASCII(label)
		if err != nil || !ldhLabel.MatchString(ascii) {
			return false
		}
		total += len(ascii)
	}
	return total <= 253
}

// IsTimestamp reports whether s is an RFC 3339 date-time (§5.6): a date, a
// time with optional fractions of a second, and a "Z" or a numeric offset.
// RFC 3339 writes the year in four digits.
func IsTimestamp(s string) bool {
	_, err := time.Parse(time.RFC3339Nano, s)
	return err == nil && len(s) >= len("2006-01-02T15:04:05Z") && s[4] == '-'
}

// InTimestampRange reports whether t's year is one a STIX timestamp can
// write: 0000 to 9999, four digits (STIX 2.1 §2.16, RFC 3339 §5.6).
func InTimestampRange(t time.Time) bool {
	y := t.UTC().Year()
	return y >= 0 && y <= 9999
}
