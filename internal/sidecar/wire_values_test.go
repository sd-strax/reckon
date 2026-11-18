package sidecar

import (
	"slices"
	"testing"

	"github.com/sd-strax/reckon/agent"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/internal/wirefixture"
	"github.com/sd-strax/reckon/knowledge"
)

// checkWireValues holds the sidecar's status word lists to their sources
// (adr/0278). The sidecar may not import the backend's packages; its tests
// may. TestWireFixtures runs it after the samples, then fails on any word type
// a sample met that it does not check.
func checkWireValues(t *testing.T) {
	t.Helper()
	for _, w := range []wirefixture.Words{actionTier(""), pendingStatus(""), resultCoverage("")} {
		wirefixture.CheckWords(t, w, ".")
	}

	if got, want := actionTier("").WireValues(), aggregate.Tier("").WireValues(); !slices.Equal(got, want) {
		t.Errorf("actionTier lists %q; the backend's tiers are %q", got, want)
	}

	// The labels the agent gives every action status under every mode a
	// request can require, in the order they first appear.
	modes := append([]string{""}, aggregate.AuthMode("").WireValues()...)
	var want []string
	for _, s := range aggregate.ActionStatus("").WireValues() {
		for _, m := range modes {
			label := agent.ActionStatus{Status: s, RequiredMode: m}.PendingLabel()
			if !slices.Contains(want, label) {
				want = append(want, label)
			}
		}
	}
	if got := pendingStatus("").WireValues(); !slices.Equal(got, want) {
		t.Errorf("pendingStatus lists %q; the agent's labels are %q", got, want)
	}

	want = capability.Coverage("").WireValues()
	for _, s := range knowledge.Coverage("").WireValues() {
		if !slices.Contains(want, s) {
			want = append(want, s)
		}
	}
	if got := resultCoverage("").WireValues(); !slices.Equal(got, want) {
		t.Errorf("resultCoverage lists %q; want %q", got, want)
	}
}
