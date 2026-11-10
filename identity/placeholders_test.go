package identity

import "testing"

func TestPlaceholderMAC(t *testing.T) {
	cases := map[string]bool{
		"00:00:00:00:00:00": true,
		"00-00-00-00-00-00": true,
		"0000.0000.0000":    true,
		"FF:FF:FF:FF:FF:FF": true,
		"ff-ff-ff-ff-ff-ff": true,
		"unknown":           true,
		"":                  true,
		"  ":                true,
		"00:1a:2b:3c:4d:5e": false,
		"00-1A-2B-3C-4D-5E": false,
		" 001a.2b3c.4d5e ":  false,
		"ff:ff:ff:ff:ff:fe": false,
	}
	for in, want := range cases {
		if got := PlaceholderMAC(in); got != want {
			t.Errorf("PlaceholderMAC(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPlaceholderIP(t *testing.T) {
	cases := map[string]bool{
		"0.0.0.0":         true,
		"::":              true,
		"127.0.0.1":       true,
		"127.9.9.9":       true,
		"::1":             true,
		"255.255.255.255": true,
		"not-an-ip":       true,
		"":                true,
		"169.254.1.1":     false,
		"10.0.0.1":        false,
		"fe80::1":         false,
		"2001:db8::1":     false,
	}
	for in, want := range cases {
		if got := PlaceholderIP(in); got != want {
			t.Errorf("PlaceholderIP(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPlaceholderHostname(t *testing.T) {
	cases := map[string]bool{
		"":                      true,
		"LOCALHOST.":            true,
		"localhost.localdomain": true,
		"-":                     true,
		"?":                     true,
		"Unknown":               true,
		"N/A":                   true,
		"none":                  true,
		"NULL":                  true,
		"ws1":                   false,
		"WS1.corp.example":      false,
		"localhost-2":           false,
		"10.0.0.5":              false,
		"127.0.0.1":             true,
		"0.0.0.0":               true,
		"::1":                   true,
		"::":                    true,
		"255.255.255.255":       true,
		"::ffff:127.0.0.1":      true,
		"fe80::1":               false,
		"1.2.3":                 false, // kept whole, but no address
		"127.0.0.01":            false, // not an address to Go's parser
	}
	for in, want := range cases {
		if got := PlaceholderHostname(in); got != want {
			t.Errorf("PlaceholderHostname(%q) = %v, want %v", in, got, want)
		}
	}
	if got := PlaceholderHostnames(); len(got) == 0 || got[0] != "" {
		t.Errorf("PlaceholderHostnames() = %v", got)
	}
}

func TestPlaceholderDomain(t *testing.T) {
	cases := map[string]bool{
		"-":               true,
		"N/A":             true,
		"localhost":       true,
		"unknown.":        true,
		"":                true,
		"unknown.example": false,
		"corp.example":    false,
		"localhost.corp":  false,
		"CORP":            false,
	}
	for in, want := range cases {
		if got := PlaceholderDomain(in); got != want {
			t.Errorf("PlaceholderDomain(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPlaceholderDeviceUID(t *testing.T) {
	cases := map[string]bool{
		"":                                       true,
		"00000000-0000-0000-0000-000000000000":   true,
		"{00000000-0000-0000-0000-000000000000}": true,
		"0":                                      true,
		"-1":                                     true,
		"-":                                      true,
		"?":                                      true,
		"UNKNOWN":                                true,
		"n/a":                                    true,
		"None":                                   true,
		"null":                                   true,
		"C.4f2a":                                 false,
		"a-1":                                    false,
		"00000000-0000-0000-0000-000000000001":   false,
	}
	for in, want := range cases {
		if got := PlaceholderDeviceUID(in); got != want {
			t.Errorf("PlaceholderDeviceUID(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestPlaceholdersTrim: every predicate reads the value trimmed.
func TestPlaceholdersTrim(t *testing.T) {
	pad := func(s string) string { return "\t" + s + "  " }
	if PlaceholderIP(pad("192.168.1.5")) || !PlaceholderIP(pad("0.0.0.0")) {
		t.Error("PlaceholderIP does not trim")
	}
	if PlaceholderHostname(pad("ws1")) || !PlaceholderHostname(pad("-")) {
		t.Error("PlaceholderHostname does not trim")
	}
	if PlaceholderDeviceUID(pad("a-1")) || !PlaceholderDeviceUID(pad("0")) {
		t.Error("PlaceholderDeviceUID does not trim")
	}
	if PlaceholderMAC(pad("00:1a:2b:3c:4d:5e")) || !PlaceholderMAC(pad("00:00:00:00:00:00")) {
		t.Error("PlaceholderMAC does not trim")
	}
}

func TestPlaceholderHash(t *testing.T) {
	cases := []struct {
		algo, digest string
		want         bool
	}{
		{"SHA-256", "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855", true},
		{"sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true},
		{"MD5", "d41d8cd98f00b204e9800998ecf8427e", true},
		{"sha1", "da39a3ee5e6b4b0d3255bfef95601890afd80709", true},
		{"SHA-512", "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e", true},
		{"sha256", "0000000000000000000000000000000000000000000000000000000000000000", true},
		{"ssdeep", "000000", true},
		{"sha256", "", true},
		{"sha256", "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", false},
		// The empty digest of one algorithm is not a placeholder under another.
		{"sha1", "d41d8cd98f00b204e9800998ecf8427e", false},
		{"ssdeep", "d41d8cd98f00b204e9800998ecf8427e", false},
	}
	for _, c := range cases {
		if got := PlaceholderHash(c.algo, c.digest); got != c.want {
			t.Errorf("PlaceholderHash(%q, %q) = %v, want %v", c.algo, c.digest, got, c.want)
		}
	}
}
