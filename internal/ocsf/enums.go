package ocsf

import "strings"

// The tables below are OCSF enum captions by value, for an event that
// carries the value without its caption. A test checks each against the
// schema checked in, in every release (adr/0356). The values 0 (Unknown)
// and 99 (Other) are left out of each on purpose: for those OCSF puts the
// meaning in the caption field, so the caller reads that instead.
var (
	// HashAlgorithms is fingerprint.algorithm_id.
	HashAlgorithms = map[int]string{
		1: "MD5", 2: "SHA-1", 3: "SHA-256", 4: "SHA-512", 5: "CTPH", 6: "TLSH", 7: "quickXorHash",
		8: "SHA-224", 9: "SHA-384", 10: "SHA-512/224", 11: "SHA-512/256",
		12: "SHA3-224", 13: "SHA3-256", 14: "SHA3-384", 15: "SHA3-512",
		16: "xxHash H3 64-bit", 17: "xxHash H3 128-bit", 18: "Imphash", 19: "NPF", 20: "HASSH",
	}
	// Severities is severity_id.
	Severities = map[int]string{1: "Informational", 2: "Low", 3: "Medium", 4: "High", 5: "Critical", 6: "Fatal"}
	// Confidences is a finding's confidence_id.
	Confidences = map[int]string{1: "Low", 2: "Medium", 3: "High"}
	// Statuses is status_id.
	Statuses = map[int]string{1: "Success", 2: "Failure"}
	// IncidentStatuses is an incident finding's status_id.
	IncidentStatuses = map[int]string{1: "New", 2: "In Progress", 3: "On Hold", 4: "Resolved", 5: "Closed"}
)

// Other is the value every OCSF enum has for a source's word that is none of
// its own. The caption field beside it holds that word.
const Other = 99

// Unknown is the value every OCSF enum has for a source that does not know.
const Unknown = 0

// CaptionID returns the enum value a source's word stands for: the value in
// the table whose caption is the word, letter case aside; Unknown when the
// word is the source's own "Unknown"; and Other when it is any other word
// the table does not have. The caller writes the word itself to the caption
// field.
func CaptionID(table map[int]string, word string) int {
	for id, caption := range table {
		if strings.EqualFold(caption, word) {
			return id
		}
	}
	if strings.EqualFold(word, "Unknown") {
		return Unknown
	}
	return Other
}

// Hashes reads an OCSF hashes value, a list of fingerprints, as algorithm →
// digest. OCSF has no other form, so nothing else is read (adr/0356).
//
// A fingerprint's value and algorithm_id are required and its algorithm
// caption is optional. So the algorithm is the caption when the fingerprint
// carries one, else the caption OCSF gives the algorithm_id. A fingerprint
// with no value, or whose algorithm cannot be named (no caption, and an id
// that is absent, 0 Unknown, 99 Other or not OCSF's), is left out.
//
// field reads one key of a fingerprint. The caller passes its own recorded
// reader, so the schema check sees which fields were read.
func Hashes(v any, field func(fingerprint map[string]any, key string) any) map[string]string {
	list, _ := v.([]any)
	var out map[string]string
	for _, item := range list {
		fp, ok := item.(map[string]any)
		if !ok {
			continue
		}
		value, _ := field(fp, "value").(string)
		algorithm, _ := field(fp, "algorithm").(string)
		if algorithm == "" {
			if id, ok := WholeNumber(field(fp, "algorithm_id")); ok {
				algorithm = HashAlgorithms[id]
			}
		}
		if algorithm == "" || value == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[algorithm] = value
	}
	return out
}
