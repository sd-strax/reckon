package sidecar

import (
	"testing"

	"github.com/sd-strax/reckon/internal/agentapi"
	"github.com/sd-strax/reckon/internal/wirefixture"
)

// checkWireValues checks the status word lists the sidecar sends (adr/0278).
// The words are the backend's, from internal/agentapi, so no list is copied
// here. TestWireFixtures runs it after the samples, then fails on any word
// type a sample met that it does not check.
func checkWireValues(t *testing.T) {
	t.Helper()
	wirefixture.CheckWords(t, agentapi.Tier(""), "../agentapi", "TierT1")
	wirefixture.CheckWords(t, agentapi.PendingStatus(""), ".")
	wirefixture.CheckWords(t, resultCoverage(""), ".")
}
