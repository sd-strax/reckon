package agentapi

import (
	"time"

	"github.com/sd-strax/reckon/internal/catalog"
)

// CapabilityDescriptor is the LLM-facing declaration of a verb (03 §5.3): its
// name, an intent description the agent renders into a tool definition, its
// typed inputs, and its output type. Descriptors are data, not code: a new
// verb is a catalog entry plus a binding, never a change to dispatch (§5.1).
type CapabilityDescriptor struct {
	Verb   string       `json:"verb"`
	Intent string       `json:"intent"` // one-line, LLM-facing
	Inputs []InputParam `json:"inputs"`
	Output string       `json:"output"` // output type description
}

// InputParam is one typed input of a verb or an action type.
type InputParam struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "entity", "time_window", "string", "int", "enum", …
	Required bool   `json:"required"`
	Desc     string `json:"desc,omitempty"`
}

// CapabilitySummary pairs a verb descriptor with its current availability and
// the number of configured bindings. Limitations lists what a present, healthy
// bound adapter reports it cannot express for the bound operation (03 §2.8,
// 11 §4.4); it never changes Status: the verb still answers the calls it can
// express.
type CapabilitySummary struct {
	Descriptor  CapabilityDescriptor     `json:"descriptor"`
	Status      catalog.CapabilityStatus `json:"status"`
	Bindings    int                      `json:"bindings"`
	Limitations []Limitation             `json:"limitations,omitempty"`
}

// Limitation is one capability a verb's bound adapter cannot express, and why
// (11 §4.4), attributed to the adapter instance that reports it.
type Limitation struct {
	Adapter    string `json:"adapter"`
	Capability string `json:"capability"`
	Reason     string `json:"reason"`
}

// CapabilitiesResponse is GET /api/capabilities. CaseQueue is the tenant's
// case queue, sent only when one is configured.
type CapabilitiesResponse struct {
	Capabilities []CapabilitySummary `json:"capabilities"`
	CaseQueue    string              `json:"case_queue,omitempty"`
}

// CapabilityInvokeBody is a verb call's input (03 §2): the entity the binding
// templates resolve against, an optional time window, and extra template roots.
type CapabilityInvokeBody struct {
	Entity map[string]any `json:"entity,omitempty"`
	Window *WindowInput   `json:"window,omitempty"`
	Extra  map[string]any `json:"extra,omitempty"`
	// SourceScope routes the call to the matching organization's adapter instance
	// and scopes identity (03 §3.5). Empty in single-organization tenants
	// reaches every unscoped instance. The agent loop sends none today.
	SourceScope string `json:"source_scope,omitempty"`
	// InvestigationRef names the investigation the call is made for, so the
	// events it returns are members of it (adr/0092). Empty for a call made
	// outside any investigation: it is recorded, but listed under none.
	InvestigationRef string `json:"investigation_ref,omitempty"`
}

// WindowInput is the JSON shape of a verb call's time bound.
type WindowInput struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}
