package server

import (
	"strings"
	"testing"
)

// TestAttestationReference: kept as written, or only its SHA-256 when the
// analyst asked (#139).
func TestAttestationReference(t *testing.T) {
	if got := attestationReference(&AttestationBody{Reference: " INC0010042 "}); got != "INC0010042" {
		t.Errorf("plain = %q", got)
	}
	got := attestationReference(&AttestationBody{Reference: "<msg-1@mail.example>", HashReference: true})
	if !strings.HasPrefix(got, "sha256:") || len(got) != len("sha256:")+64 || strings.Contains(got, "msg-1") {
		t.Errorf("hashed = %q", got)
	}
	if attestationReference(&AttestationBody{HashReference: true}) != "" {
		t.Error("no reference, nothing to hash")
	}
}
