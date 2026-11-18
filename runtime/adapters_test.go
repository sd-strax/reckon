package runtime

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/internal/catalog"
)

func TestRegisterNewVerbsAddsUnknownKeepsKnown(t *testing.T) {
	cat := capability.NewCatalog()
	// A verb the engine already defines (its descriptor is authoritative).
	if err := cat.Register(capability.CapabilityDescriptor{Verb: "enumerate_logons", Intent: "engine intent"}); err != nil {
		t.Fatal(err)
	}

	registerNewVerbs(cat, "edr", []capability.CapabilityDescriptor{
		// Adapter re-describes a known verb with a different intent — must NOT override.
		{Verb: "enumerate_logons", Intent: "adapter intent"},
		// A genuinely new adapter verb — must be added (this is what makes an
		// adapter's own verb visible to the agent).
		{Verb: "get_entity_context", Intent: "Fetch identity context."},
		{Verb: ""}, // refused: not a valid tool name
	}, nil)

	if d, _ := cat.Descriptor("enumerate_logons"); d.Intent != "engine intent" {
		t.Errorf("known verb overridden: intent = %q", d.Intent)
	}
	if d, ok := cat.Descriptor("get_entity_context"); !ok || d.Intent != "Fetch identity context." {
		t.Errorf("new adapter verb not registered: %+v ok=%v", d, ok)
	}
	if len(cat.Verbs()) != 2 {
		t.Errorf("catalog verbs = %v, want 2", cat.Verbs())
	}
}

// TestRegisterNewVerbs_RefusesAVerbTheAgentCannotOffer (adr/0382): an adapter
// verb named like an action tool, like a built-in tool, or with a name no
// model API accepts is left out with a log line naming the adapter and the
// verb, and the adapter's other verbs are registered.
func TestRegisterNewVerbs_RefusesAVerbTheAgentCannotOffer(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	actionTools, err := actionToolNames()
	if err != nil {
		t.Fatal(err)
	}
	clash := catalog.ActionToolName("host.isolate")
	if !actionTools[clash] {
		t.Fatalf("%s is not an action tool name", clash)
	}
	cat := capability.NewCatalog()
	registerNewVerbs(cat, "edr", []capability.CapabilityDescriptor{
		{Verb: clash},
		{Verb: catalog.ToolRecallSOPs},
		{Verb: "get.entity.context"},
		{Verb: ""},
		{Verb: "get_entity_context"},
	}, actionTools)

	if got := cat.Verbs(); len(got) != 1 || got[0] != "get_entity_context" {
		t.Errorf("catalog verbs = %v, want only get_entity_context", got)
	}
	for _, verb := range []string{clash, catalog.ToolRecallSOPs, "get.entity.context", ""} {
		if want := `adapter "edr" verb "` + verb + `" refused`; !strings.Contains(buf.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, buf.String())
		}
	}
}

// TestCheckBuiltinVerbs (adr/0382): a verb of the engine's own catalog that
// takes an action type's tool name fails startup; the shipped catalogs pass.
func TestCheckBuiltinVerbs(t *testing.T) {
	actionTools, err := actionToolNames()
	if err != nil {
		t.Fatal(err)
	}
	builtin, err := capability.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkBuiltinVerbs(builtin, actionTools); err != nil {
		t.Errorf("shipped catalogs clash: %v", err)
	}
	clash := catalog.ActionToolName("host.isolate")
	bad := capability.NewCatalog()
	if err := bad.Register(capability.CapabilityDescriptor{Verb: clash}); err != nil {
		t.Fatal(err)
	}
	if err := checkBuiltinVerbs(bad, actionTools); err == nil || !strings.Contains(err.Error(), clash) {
		t.Errorf("checkBuiltinVerbs = %v, want an error naming %s", err, clash)
	}
}
