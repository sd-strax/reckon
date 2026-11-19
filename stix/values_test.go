package stix

import (
	"strings"
	"testing"
	"time"
)

func TestValueChecks(t *testing.T) {
	for _, c := range []struct {
		name  string
		check func(string) bool
		in    string
		want  bool
	}{
		{"uri", IsURI, "https://example.com/login?a=1#f", true},
		{"uri, other scheme", IsURI, "reckon://ocsf-record/sha256/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", true},
		{"uri, no scheme", IsURI, "www.example.com/login", false},
		{"uri, a space", IsURI, "http://example.com/a b", false},
		{"uri, a broken percent-encoding", IsURI, "http://example.com/%zz", false},
		{"uri, nothing after the scheme", IsURI, "http:", false},
		{"addr-spec", IsAddrSpec, "jdoe@example.com", true},
		{"addr-spec, quoted local part", IsAddrSpec, `"j doe"@example.com`, true},
		{"addr-spec, display name", IsAddrSpec, "Bob <bob@example.com>", false},
		{"addr-spec, angle brackets", IsAddrSpec, "<bob@example.com>", false},
		{"addr-spec, unquoted comma", IsAddrSpec, "a,b@example.com", false},
		{"addr-spec, white space", IsAddrSpec, " bob@example.com", false},
		// RFC 5322 §3.2.4: "@", "<" and ">" are ordinary inside a quoted
		// local part (the review's inputs on #365).
		{"addr-spec, quoted @", IsAddrSpec, `"alerts@corp"@attacker.example`, true},
		{"addr-spec, quoted <", IsAddrSpec, `"alerts<corp"@attacker.example`, true},
		{"addr-spec, quoted >", IsAddrSpec, `"alerts>corp"@attacker.example`, true},
		{"addr-spec, trailing space", IsAddrSpec, "bob@example.com ", false},
		{"addr-spec, quoted display name", IsAddrSpec, `"Bob" <bob@example.com>`, false},
		{"addr-spec, brackets around a quoted local part", IsAddrSpec, `<"a@b"@example.com>`, false},
		{"addr-spec, no domain", IsAddrSpec, "bob@", false},
		{"domain", IsDomainName, "www.example.com", true},
		{"domain, trailing dot", IsDomainName, "www.example.com.", true},
		{"domain, U-label", IsDomainName, "bücher.example", true},
		{"domain, underscore labels", IsDomainName, "_ldap._tcp.dc._msdcs.example.com", true},
		{"domain, a backslash", IsDomainName, `CONTOSO\`, false},
		{"domain, a space", IsDomainName, "not a name", false},
		{"domain, an empty label", IsDomainName, "a..example", false},
		{"domain, a leading hyphen", IsDomainName, "-a.example", false},
		{"domain, a 64-character label", IsDomainName, strings.Repeat("a", 64) + ".example", false},
		{"domain, longer than 253", IsDomainName, strings.Repeat("abcdefghi.", 26) + "example", false},
		{"timestamp", IsTimestamp, "2026-01-02T03:04:05.678Z", true},
		{"timestamp, an offset", IsTimestamp, "2026-01-02T03:04:05+02:00", true},
		{"timestamp, a five-digit year", IsTimestamp, "10000-01-02T03:04:05Z", false},
		{"timestamp, a space", IsTimestamp, "2026-01-02 03:04:05Z", false},
	} {
		if got := c.check(c.in); got != c.want {
			t.Errorf("%s: %q = %v; want %v", c.name, c.in, got, c.want)
		}
	}
	for y, want := range map[int]bool{0: true, 9999: true, -1: false, 10000: false} {
		if got := InTimestampRange(time.Date(y, 6, 1, 0, 0, 0, 0, time.UTC)); got != want {
			t.Errorf("year %d: %v; want %v", y, got, want)
		}
	}
}

// TestEmailAddress: an address is trimmed, and a mailbox with a display name
// keeps its address. Every address is written back in its shortest RFC 5322
// form, so one mailbox quoted two ways is one value, and a local part holding
// quotes keeps them (adr/0371). A local part that would start or end with
// white space is quoted. A value that is still not an addr-spec is not one.
func TestEmailAddress(t *testing.T) {
	for in, want := range map[string]string{
		"alice@corp.example":                "alice@corp.example",
		"  alice@corp.example\t":            "alice@corp.example",
		"Alice <alice@corp.example>":        "alice@corp.example",
		" \"Doe, Jo\" <jo@corp.example>":    "jo@corp.example",
		"<bob@corp.example>":                "bob@corp.example",
		`"bob@x.example" <alice@x.example>`: "alice@x.example",
		// Quoted where it need not be: written unquoted.
		`"alice"@x.example`:         "alice@x.example",
		`Alice <"alice"@x.example>`: "alice@x.example",
		`"alerts"@example.com`:      "alerts@example.com",
		// A local part holding quotes keeps them, escaped.
		`Alice <"\"alerts\""@example.com>`: `"\"alerts\""@example.com`,
		`"\"alerts\""@example.com`:         `"\"alerts\""@example.com`,
		// Quoted because it cannot be a dot-atom.
		`"a b"@x.example`:  `"a b"@x.example`,
		`"a@b"@x.example`:  `"a@b"@x.example`,
		`"a..b"@x.example`: `"a..b"@x.example`,
		// A quoted pair stands for its character.
		`"a\b"@x.example`: "ab@x.example",
		// A local part that would start or end with white space stays
		// quoted, so the id's trim can't make it another mailbox.
		"\"\u3000alerts\"@example.com":         "\"\u3000alerts\"@example.com",
		"\"alerts\u3000\"@example.com":         "\"alerts\u3000\"@example.com",
		"alerts\u3000@example.com":             "\"alerts\u3000\"@example.com",
		"\"\u2028a\"@x.example":                "\"\u2028a\"@x.example",
		"\"\u0085a\"@x.example":                "\"\u0085a\"@x.example",
		"\"\\\u3000alerts\"@example.com":       "\"\u3000alerts\"@example.com",
		"Alice <\"\u3000alerts\"@example.com>": "\"\u3000alerts\"@example.com",
		// A domain that ends in white space parses, but the address it is
		// written back as is refused.
		"<a@x.example\u3000>":       "",
		"Alice <a@x.example\u3000>": "",
		// The domain and a non-ASCII local part are written as parsed.
		"user@[192.0.2.1]":         "user@[192.0.2.1]",
		"josé@example.com":         "josé@example.com",
		"":                         "",
		"   ":                      "",
		"Undisclosed recipients:;": "",
		"a,b@corp.example":         "",
		"accounts payable":         "",
	} {
		got, ok := EmailAddress(in)
		if got != want || ok != (want != "") {
			t.Errorf("EmailAddress(%q) = %q, %v; want %q", in, got, ok, want)
		}
		// A stored value cleans up to itself.
		if again, ok := EmailAddress(got); want != "" && (again != got || !ok) {
			t.Errorf("EmailAddress(%q) = %q, %v; want it unchanged", got, again, ok)
		}
	}
}
