package server

import (
	"testing"

	"github.com/sd-strax/reckon/aggregate"
)

// TestLifecycleCommand_Discard: the endpoint knows the discard verb (adr/0161).
func TestLifecycleCommand_Discard(t *testing.T) {
	cmd, err := lifecycleCommand(LifecycleRequestBody{Transition: "discard", Reason: "mistyped host"})
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := cmd.(aggregate.DiscardInvestigation); !ok || d.Reason != "mistyped host" {
		t.Errorf("discard maps to %#v", cmd)
	}
}
