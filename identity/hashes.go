package identity

import (
	"regexp"
	"slices"
	"strings"
)

// hashFormats are the digest formats STIX 2.1 gives its hash algorithms, as
// the OASIS hashes-type schema checks them. Each is keyed by the folded
// algorithm name (foldAlgo), so "SHA-256", "sha256" and "SHA256" share one.
var hashFormats = map[string]*regexp.Regexp{
	"md5":     regexp.MustCompile(`^[a-fA-F0-9]{32}$`),
	"sha1":    regexp.MustCompile(`^[a-fA-F0-9]{40}$`),
	"sha256":  regexp.MustCompile(`^[a-fA-F0-9]{64}$`),
	"sha512":  regexp.MustCompile(`^[a-fA-F0-9]{128}$`),
	"sha3256": regexp.MustCompile(`^[a-fA-F0-9]{64}$`),
	"sha3512": regexp.MustCompile(`^[a-fA-F0-9]{128}$`),
	"ssdeep":  regexp.MustCompile(`^[a-zA-Z0-9/+:.]{1,128}$`),
	"tlsh":    regexp.MustCompile(`^[a-zA-Z0-9]{70}$`),
}

// tlshVersioned is a TLSH digest with its version prefix: "T1" and 70 hex
// characters. STIX's TLSH format is the 70 characters alone.
var tlshVersioned = regexp.MustCompile(`^T1([a-fA-F0-9]{70})$`)

// hashKeyDisallowed is a character STIX does not allow in a hashes key: the custom key
// pattern is ^[a-zA-Z0-9_-]{3,250}$.
var hashKeyDisallowed = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// HashFits reports whether a digest fits its algorithm's STIX format. An
// algorithm STIX gives no format, such as a vendor's own, always fits.
func HashFits(algo, digest string) bool {
	f, ok := hashFormats[foldAlgo(algo)]
	return !ok || f.MatchString(digest)
}

// HashKey is an algorithm name as a STIX hashes key: each character STIX does
// not allow in a key, such as the "/" of "SHA-512/256" or a space, becomes
// "-" (adr/0357).
func HashKey(algo string) string {
	return hashKeyDisallowed.ReplaceAllString(algo, "-")
}

// stixHashKeys are the hash-algorithm-ov names STIX 2.1 gives the
// algorithms it knows (§10.3), by folded name (foldAlgo). A key that folds to
// one is written that way, so "sha256" and "SHA-256" are one key.
var stixHashKeys = map[string]string{
	"md5": "MD5", "sha1": "SHA-1", "sha256": "SHA-256", "sha512": "SHA-512",
	"sha3256": "SHA3-256", "sha3512": "SHA3-512", "ssdeep": "SSDEEP", "tlsh": "TLSH",
}

// FittingHashes returns the hashes as STIX holds them, or nil when none is
// left (adr/0357). Cleanup comes first, and every check is made against the
// cleaned key and digest:
//
//   - each algorithm name becomes a key STIX allows (HashKey), and a name
//     STIX knows takes its hash-algorithm-ov spelling, so "SHA 256",
//     "sha256" and "SHA-256" are all SHA-256;
//   - a digest is trimmed, and a TLSH digest with the "T1" version prefix
//     loses the prefix;
//   - a digest that does not fit its algorithm, checked against the final
//     key, is left out, because a broken digest cannot say which file this
//     is;
//   - a hash whose key is shorter than 3 or longer than 250 characters is
//     left out, because STIX allows no such key (§2.7).
//
// Keys are collapsed without case: when two names give one key, equal
// digests keep it, and different digests make it ambiguous, so it is left
// out. The result never depends on the order the map is read in.
//
// A normalizer stores these hashes and mints the file's id from them: the
// same set in both places.
func FittingHashes(hashes map[string]string) map[string]string {
	type entry struct{ key, digest string }
	byFold := map[string][]entry{}
	for algo, digest := range hashes {
		key := HashKey(strings.TrimSpace(algo))
		fold := strings.ToLower(foldAlgo(key))
		if known, ok := stixHashKeys[fold]; ok {
			key = known
		}
		digest = strings.TrimSpace(digest)
		if fold == "tlsh" {
			if m := tlshVersioned.FindStringSubmatch(digest); m != nil {
				digest = m[1]
			}
		}
		if !HashFits(key, digest) || len(key) < 3 || len(key) > 250 {
			continue
		}
		byFold[fold] = append(byFold[fold], entry{key, digest})
	}
	var out map[string]string
	for _, entries := range byFold {
		slices.SortFunc(entries, func(a, b entry) int {
			if c := strings.Compare(a.key, b.key); c != 0 {
				return c
			}
			return strings.Compare(a.digest, b.digest)
		})
		same := true
		for _, e := range entries[1:] {
			same = same && strings.EqualFold(e.digest, entries[0].digest)
		}
		if !same {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(byFold))
		}
		out[entries[0].key] = entries[0].digest
	}
	return out
}
