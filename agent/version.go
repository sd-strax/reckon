package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// PromptVersion identifies the text the agent's turn loop puts in front of
// the model, for the given hypothesis-framing dial (adr/0057, adr/0232): the
// base prompt, every intrinsic tool definition, and the engine text. It is a
// pure function of the code and the one tenant dial that changes the prompt's
// words (01 §x-hypothesis); it does not move with the investigation, the
// environment's verbs or its action catalog, which vary per case and per
// install and are not the prompt (the catalog's content is attributed apart, as
// the action-catalog hash). The eval harness stamps it on every run (10 §1.4),
// and "model X is supported" is a claim about a baseline at a prompt version
// (10 §4.4), so it must be stable across runs on the same code and differ
// whenever the model is read different instructions.
func PromptVersion(framing string) string {
	return versionOf(promptComponents(framing))
}

// SideInstructionVersion identifies the instructions of the side completions,
// the knowledge-base summary and the SOP draft. No eval assertion grades what
// they produce, so they are versioned apart from PromptVersion: a change here
// re-attributes the side products and asks for no baseline rerun.
func SideInstructionVersion() string {
	return versionOf(sideComponents())
}

// versionComponent is one named input of a version hash.
type versionComponent struct {
	Name  string
	Bytes []byte
}

// versionOf hashes each component's name and digest, in order. A golden over
// the per-component digests names what moved when a version does; the order
// is hashed too, so a reordering moves the version and no component.
func versionOf(comps []versionComponent) string {
	h := sha256.New()
	for _, c := range comps {
		h.Write([]byte(c.Name + " " + digest(c.Bytes) + "\n")) // a hash.Hash never fails a write
	}
	return hex.EncodeToString(h.Sum(nil))
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// promptComponents is what PromptVersion covers, in a fixed order: the base
// prompt; each intrinsic tool as rendered with no action catalog; the parts of
// the tool text that only render with a catalog or a verb, through the example
// ones below; and each engine-text template.
func promptComponents(framing string) []versionComponent {
	comps := []versionComponent{{"prompt", []byte(systemPrompt(Investigation{HypothesisFraming: framing}, nil))}}
	for _, t := range intrinsicTools(nil) {
		comps = append(comps, versionComponent{"tools/" + t.def.Name, toolBytes(t.def)})
	}
	for _, t := range intrinsicTools(exampleCatalog()) {
		if t.def.Name == ToolRequestAction {
			comps = append(comps, versionComponent{"tools/" + t.def.Name + "+catalog", toolBytes(t.def)})
		}
	}
	comps = append(comps, versionComponent{"tools/<verb>", toolBytes(verbToolDef(exampleVerb()))})
	for _, e := range engineTexts {
		comps = append(comps, versionComponent{"engine-text/" + e.ID, []byte(e.Text)})
	}
	return comps
}

func sideComponents() []versionComponent {
	comps := make([]versionComponent, 0, len(sideInstructions))
	for _, e := range sideInstructions {
		comps = append(comps, versionComponent{"side/" + e.ID, []byte(e.Text)})
	}
	return comps
}

// toolBytes is a tool definition as hashed. encoding/json writes map keys
// sorted, so the bytes are a function of the definition alone.
func toolBytes(d ToolDef) []byte {
	b, err := json.Marshal(d)
	if err != nil {
		panic(fmt.Sprintf("agent: tool %s does not marshal: %v", d.Name, err))
	}
	return b
}

// exampleCatalog exercises the request_action text a catalog brings: a type of
// each availability, entity and scalar inputs, and a parameter two types share.
// It stands in for an install's catalog so the hash covers the code's words
// around the catalog, never the catalog's own. Its literal strings are part of
// the version all the same: editing one moves tools/request_action+catalog,
// though no model is ever shown them.
func exampleCatalog() []ActionType {
	var isolate, ticket, block ActionType
	isolate.Status = "available"
	isolate.Descriptor.ActionType = "host.isolate"
	isolate.Descriptor.Intent = "Isolate a host."
	isolate.Descriptor.DefaultTier = "T2"
	isolate.Descriptor.Reversibility = "REVERSIBLE"
	isolate.Descriptor.Inputs = []ActionInput{{Name: "target", Type: "entity", Required: true}, {Name: "reason", Type: "string", Required: true, Desc: "why"}}
	ticket.Status = "degraded"
	ticket.Descriptor.ActionType = "ticket.create"
	ticket.Descriptor.Intent = "Open a ticket."
	ticket.Descriptor.DefaultTier = "T1"
	ticket.Descriptor.Reversibility = "IRREVERSIBLE"
	ticket.Descriptor.Inputs = []ActionInput{{Name: "summary", Type: "string", Required: true, Desc: "headline"}, {Name: "reason", Type: "string"}, {Name: "priority", Type: "integer"}}
	block.Status = "unavailable"
	block.Descriptor.ActionType = "ioc.block"
	block.Descriptor.Intent = "Block an indicator."
	block.Descriptor.DefaultTier = "T2"
	block.Descriptor.Reversibility = "BEST_EFFORT"
	return []ActionType{isolate, ticket, block}
}

// exampleVerb is a capability descriptor with every input kind the loop
// renders (entity, time window, scalar), for the same reason. Its literal
// strings are part of the version too, through tools/<verb>.
func exampleVerb() Capability {
	var c Capability
	c.Status = "available"
	c.Descriptor.Verb = "verb"
	c.Descriptor.Intent = "Intent."
	c.Descriptor.Output = "output"
	for _, in := range []struct {
		name, kind string
		required   bool
	}{{"target", "entity", true}, {"window", "time_window", false}, {"limit", "string", false}} {
		c.Descriptor.Inputs = append(c.Descriptor.Inputs, struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Required bool   `json:"required"`
			Desc     string `json:"desc,omitempty"`
		}{Name: in.name, Type: in.kind, Required: in.required, Desc: in.name})
	}
	return c
}
