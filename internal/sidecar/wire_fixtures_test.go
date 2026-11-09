package sidecar

import (
	"testing"

	"github.com/sd-strax/reckon/internal/apiversion"
	"github.com/sd-strax/reckon/internal/wirefixture"
)

const sidecarFixtures = "../../workbench/src/wire/fixtures/sidecar"

// TestWireFixtures keeps the sample of each result and notification the
// workbench decodes, and the two versions it checks (adr/0244).
func TestWireFixtures(t *testing.T) {
	dir := sidecarFixtures
	wirefixture.Check[initializeResult](t, dir, "initialize")
	wirefixture.Check[createSessionResult](t, dir, "createSession")
	wirefixture.Check[turnResult](t, dir, "turn")
	wirefixture.Check[proposeSOPResult](t, dir, "proposeSOP")
	wirefixture.Check[draftNarrativeResult](t, dir, "draftNarrative")
	wirefixture.Check[turnTextNote](t, dir, "text")
	wirefixture.Check[turnStepNote](t, dir, "step")
	wirefixture.Check[turnToolCallNote](t, dir, "toolCall")
	wirefixture.Check[turnToolResultNote](t, dir, "toolResult")
	wirefixture.CheckValue(t, dir, "versions", struct {
		Protocol int `json:"protocol"`
		API      int `json:"api"`
	}{ProtocolVersion, apiversion.Current})
}
