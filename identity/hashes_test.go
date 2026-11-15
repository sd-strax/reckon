package identity

import (
	"reflect"
	"strings"
	"testing"
)

func TestHashFits(t *testing.T) {
	for _, c := range []struct {
		algo, digest string
		want         bool
	}{
		{"MD5", strings.Repeat("a", 32), true},
		{"MD5", strings.Repeat("a", 31), false},
		{"SHA-1", strings.Repeat("b", 40), true},
		{"SHA-1", strings.Repeat("b", 64), false},
		{"SHA-256", strings.Repeat("C", 64), true},
		{"sha256", "deadbeef", false},
		{"SHA256", strings.Repeat("g", 64), false},
		{"SHA-512", strings.Repeat("d", 128), true},
		{"SHA-512", strings.Repeat("d", 64), false},
		{"SHA3-256", strings.Repeat("e", 64), true},
		{"SHA3-512", strings.Repeat("e", 127), false},
		{"SSDEEP", "3:AXGBicFlgVNhBGcL6wCrFQEv:AXGHsNhxLsr2C", true},
		{"TLSH", strings.Repeat("A", 70), true},
		{"TLSH", strings.Repeat("A", 72), false},
		// STIX gives these no format, so any digest fits.
		{"CTPH", "x", true},
		{"Imphash", "anything", true},
	} {
		if got := HashFits(c.algo, c.digest); got != c.want {
			t.Errorf("HashFits(%q, %q) = %v; want %v", c.algo, c.digest, got, c.want)
		}
	}
}

// FittingHashes keeps the hashes that fit, and the file id is minted from
// the same set: a broken hash beside a good one gives the good one's id, and
// a broken hash alone leaves the file to its name (adr/0357).
func TestFittingHashesAndTheFileID(t *testing.T) {
	md5 := strings.Repeat("a", 32)
	got := FittingHashes(map[string]string{"SHA-256": "deadbeef", "MD5": md5})
	if want := map[string]string{"MD5": md5}; !reflect.DeepEqual(got, want) {
		t.Errorf("FittingHashes = %v; want %v", got, want)
	}
	if FittingHashes(map[string]string{"SHA-256": "deadbeef"}) != nil {
		t.Error("no hash fits, and the result is not nil")
	}

	r := NewResolver(nsA)
	if r.File(FileIdentity{Hashes: map[string]string{"SHA-256": "deadbeef", "MD5": md5}}) != r.File(FileIdentity{Hashes: map[string]string{"MD5": md5}}) {
		t.Error("a broken SHA-256 beside a good MD5 changed the file id")
	}
	if r.File(FileIdentity{Hashes: map[string]string{"SHA-256": "deadbeef"}, Name: "a.exe"}) != r.File(FileIdentity{Name: "a.exe"}) {
		t.Error("a broken hash alone did not leave the file to its name")
	}
	if id := r.File(FileIdentity{Hashes: map[string]string{"SHA-256": "deadbeef"}}); id != "" {
		t.Errorf("a broken hash and nothing else minted %s", id)
	}
}

// A TLSH digest with its "T1" version prefix keeps the 70 characters STIX
// holds, and an algorithm name STIX does not allow as a key gets "-" for each
// disallowed character, in the stored hashes and so in the id inputs too.
func TestFittingHashesShapesTLSHAndKeys(t *testing.T) {
	tlsh := strings.Repeat("A1", 35)
	sha := strings.Repeat("f", 64)
	got := FittingHashes(map[string]string{
		"TLSH":             "T1" + tlsh,
		"SHA-512/256":      sha,
		"xxHash H3 64-bit": "0123456789abcdef",
	})
	want := map[string]string{"TLSH": tlsh, "SHA-512-256": sha, "xxHash-H3-64-bit": "0123456789abcdef"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FittingHashes = %v; want %v", got, want)
	}
	if got := FittingHashes(map[string]string{"TLSH": "T2" + tlsh}); got != nil {
		t.Errorf("a TLSH digest with another prefix was kept: %v", got)
	}
	for _, k := range []string{"MD5", "SHA-256", "quickXorHash", "SHA3-512"} {
		if HashKey(k) != k {
			t.Errorf("HashKey(%q) = %q; a key STIX allows is unchanged", k, HashKey(k))
		}
	}
}

func TestFittingHashesKeyLength(t *testing.T) {
	got := FittingHashes(map[string]string{"H1": "x", strings.Repeat("h", 251): "x", "H12": "x", strings.Repeat("h", 250): "x"})
	if len(got) != 2 || got["H12"] != "x" || got[strings.Repeat("h", 250)] != "x" {
		t.Errorf("FittingHashes = %v; want the 3- and 250-character keys alone", got)
	}
}

// Class rule R4 (adr/0357): cleanup comes before the format check, and a key
// collapse never depends on input order.
func TestFittingHashesCleanupFirstAndDeterministic(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	if got := FittingHashes(map[string]string{"SHA-256": " " + sha + "\n"}); got["SHA-256"] != sha {
		t.Errorf("a padded digest: %v; want it trimmed and kept", got)
	}
	if got := FittingHashes(map[string]string{"TLSH": " T1" + strings.Repeat("A1", 35) + " "}); got["TLSH"] != strings.Repeat("A1", 35) {
		t.Errorf("a padded versioned TLSH: %v", got)
	}
	d1, d2 := strings.Repeat("1", 64), strings.Repeat("2", 64)
	for name, c := range map[string]struct {
		in   map[string]string
		want map[string]string
	}{
		"equal digests keep the key":     {map[string]string{"SHA-512/256": d1, "SHA-512 256": d1}, map[string]string{"SHA-512-256": d1}},
		"equal up to case keep the key":  {map[string]string{"SHA-512/256": strings.ToUpper(sha), "SHA-512 256": sha}, map[string]string{"SHA-512-256": strings.ToUpper(sha)}},
		"different digests drop the key": {map[string]string{"SHA-512/256": d1, "SHA-512 256": d2, "MD5": strings.Repeat("3", 32)}, map[string]string{"MD5": strings.Repeat("3", 32)}},
	} {
		first := FittingHashes(c.in)
		if !reflect.DeepEqual(first, c.want) {
			t.Errorf("%s: %v; want %v", name, first, c.want)
		}
		// Go randomizes map order, so many runs cover every input order.
		for i := 0; i < 200; i++ {
			if got := FittingHashes(c.in); !reflect.DeepEqual(got, first) {
				t.Fatalf("%s: run %d gave %v, run 0 gave %v", name, i, got, first)
			}
		}
	}
}

// Keys collapse without case after cleanup, a name STIX knows takes its
// spelling, and the format is checked against the final key (adr/0357).
// pickHash reads the same cleaned set, so the file id is deterministic.
func TestHashKeysCollapseWithoutCase(t *testing.T) {
	d1, d2 := strings.Repeat("ab", 32), strings.Repeat("cd", 32)
	for name, c := range map[string]struct {
		in   map[string]string
		want map[string]string
	}{
		"one SHA-256 three ways": {map[string]string{"SHA-256": d1, "sha256": strings.ToUpper(d1), "SHA 256": d1}, map[string]string{"SHA-256": strings.ToUpper(d1)}},
		"two digests, ambiguous": {map[string]string{"SHA-256": d1, "sha256": d2, "MD5": strings.Repeat("e", 32)}, map[string]string{"MD5": strings.Repeat("e", 32)}},
		"checked as SHA-256":     {map[string]string{"SHA 256": "deadbeef"}, nil},
		"a vendor name kept":     {map[string]string{"quickXorHash": "abc"}, map[string]string{"quickXorHash": "abc"}},
	} {
		for i := 0; i < 100; i++ {
			if got := FittingHashes(c.in); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("%s (run %d): %v; want %v", name, i, got, c.want)
			}
		}
	}
	r := NewResolver(nsA)
	want := r.File(FileIdentity{Hashes: map[string]string{"SHA-256": d1}})
	for i := 0; i < 100; i++ {
		if got := r.File(FileIdentity{Hashes: map[string]string{"sha256": d1, "SHA-256": strings.ToUpper(d1), "SHA-1": strings.Repeat("1", 40)}}); got != want {
			t.Fatalf("run %d: file id %s; want %s, from the one SHA-256", i, got, want)
		}
		// An ambiguous SHA-256 is dropped, so the id falls to the SHA-1.
		if got, sha1 := r.File(FileIdentity{Hashes: map[string]string{"sha256": d1, "SHA-256": d2, "SHA-1": strings.Repeat("1", 40)}}),
			r.File(FileIdentity{Hashes: map[string]string{"SHA-1": strings.Repeat("1", 40)}}); got != sha1 {
			t.Fatalf("run %d: an ambiguous SHA-256 gave %s; want the SHA-1's id %s", i, got, sha1)
		}
	}
}
