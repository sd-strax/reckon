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
