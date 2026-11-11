package adapterplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/capability"
)

// Method names, mapped 1:1 onto JSON-RPC methods (§2 "invoke/dispatch/health
// mapped 1:1"). The handshake trio (initialize/describe/configure) runs once
// per process lifetime; the serve trio (invoke/dispatch/health) runs per call.
const (
	methodInitialize = "initialize"
	methodDescribe   = "describe"
	methodConfigure  = "configure"
	methodInvoke     = "invoke"
	methodDispatch   = "dispatch"
	methodHealth     = "health"
)

// InitializeParams is the first call after spawn (§4.1). Version negotiation is
// the highest protocol version both sides support.
type InitializeParams struct {
	EngineProtocolVersions []int  `json:"engine_protocol_versions"`
	EngineVersion          string `json:"engine_version"`
}

// InitializeResult is the adapter's negotiation reply (§4.1). Concurrency is the
// adapter's declared parallelism; the default (and a valid answer for a
// single-threaded script) is serial.
type InitializeResult struct {
	ProtocolVersion int    `json:"protocol_version"`
	AdapterVersion  string `json:"adapter_version"`
	Concurrency     int    `json:"concurrency"`
}

// DescribeResult is the source of truth on capability (§4.2). The engine
// disposes of what it proposes: descriptors and default bindings enter the
// registries as CANDIDATES, filtered by enablement before an agent can see them
// (§5). Two hard rules from §4.2 apply when this is reconciled into the
// catalog: the adapter is the authority on capability FACTS (which ops exist,
// their schemas, which inverse op exists) but NEVER on tier or reversibility
// CLASSIFICATION — those are engine-side risk judgments (04 §2), and a
// conflicting adapter claim is a drift signal surfaced at enablement, never
// silently adopted.
//
// Read and write default bindings are carried under separate keys because their
// shapes differ (an ActionBinding declares external-approval substitution); a
// single adapter of class mcp may legitimately describe both (e.g. CRWD-falcon:
// read verbs plus ioc.block).
type DescribeResult struct {
	Verbs                []capability.CapabilityDescriptor `json:"verbs"`
	ActionTypes          []action.ActionDescriptor         `json:"action_types"`
	Operations           []OperationSchema                 `json:"operations"`
	DefaultReadBindings  []ReadBinding                     `json:"default_read_bindings"`
	DefaultWriteBindings []action.ActionBinding            `json:"default_write_bindings"`
	ConfigSchema         map[string]any                    `json:"config_schema"`
	// SchemaSource says where the adapter's fields come from (§4.4). It is a
	// pointer so an adapter that declares none is distinguishable from one that
	// declares an empty kind: conformance fails both.
	SchemaSource *SchemaSource `json:"schema_source,omitempty"`
}

// Schema source kinds (§4.4).
const (
	// SchemaSourceResponse: the adapter decodes a vendor API's response; its
	// obligation is tolerant decoding (an absent field is absent, never an
	// error).
	SchemaSourceResponse = "response"
	// SchemaSourceQuery: the adapter names fields in a query against a store
	// whose schema it does not own. It discovers the schema, carries a recorded
	// profile per source version, declines with FALLTHROUGH what the schema
	// cannot express and reports degraded capabilities in health.
	SchemaSourceQuery = "query"
)

// SchemaSource is describe's declaration of where the adapter's fields come
// from (§4.4). Profiles is required for kind query and unused for response.
type SchemaSource struct {
	Kind     string          `json:"kind"`
	Profiles []SchemaProfile `json:"profiles,omitempty"`
}

// SchemaProfile is one source version a query adapter supports (§4.4): the
// store it reads, the source version, and the fields its projection and
// predicates name. Its recording lives with the adapter's tests under
// testdata/profiles/<name>/.
type SchemaProfile struct {
	Name    string   `json:"name"`
	Store   string   `json:"store"`
	Version string   `json:"version"`
	Fields  []string `json:"fields"`
}

// ReadBinding is a default read-verb binding in describe output (§4.2), ready to
// adopt into tenant config. Unlike a bare capability.Binding it carries the Verb
// it binds — the tenant config keys bindings by verb (03 §3.2), and the write
// side already carries its ActionType on action.ActionBinding.
type ReadBinding struct {
	Verb      string         `json:"verb"`
	Adapter   string         `json:"adapter"`
	Operation string         `json:"operation"`
	Priority  int            `json:"priority,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
}

// OperationSchema names an operation invoke/dispatch will accept and its param
// schema (§4.2). The operation name is what a binding's `operation` field
// references (03 §3.2 / 08 §4).
type OperationSchema struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params"`
}

// ConfigureParams delivers the instance's validated config as the final
// handshake step (§4.3). x-secret fields carry UNRESOLVED secret references
// (keychain:// / env:// / vault://), never literals — resolution is
// per-invocation (05 §10.2) and a literal is refused at config load.
type ConfigureParams struct {
	Config map[string]any `json:"config"`
}

// InvokeParams runs one read operation with binding-templated params (§4,
// mapping capability.Adapter.Invoke).
type InvokeParams struct {
	Operation string         `json:"operation"`
	Params    map[string]any `json:"params"`
}

// invokeResult is the wire shape of one Invoke: OCSF events plus their
// source-tool label. It maps onto capability.AdapterResponse.
type invokeResult struct {
	SourceTool string            `json:"source_tool"`
	Events     []ocsfPayloadWire `json:"events"`
}

// ocsfPayloadWire is the wire shape of capability.OcsfPayload (which has no JSON
// tags of its own — it is an in-process type).
type ocsfPayloadWire struct {
	ClassUID  int            `json:"class_uid"`
	ClassName string         `json:"class_name"`
	Time      time.Time      `json:"time"`
	Raw       map[string]any `json:"raw"`
}

func (w invokeResult) toResponse() capability.AdapterResponse {
	events := make([]capability.OcsfPayload, 0, len(w.Events))
	for _, e := range w.Events {
		events = append(events, capability.OcsfPayload{
			ClassUID:  e.ClassUID,
			ClassName: e.ClassName,
			Time:      e.Time,
			Raw:       e.Raw,
		})
	}
	return capability.AdapterResponse{SourceTool: w.SourceTool, Events: events}
}

// DispatchParams runs one write operation (§4, mapping action.WriteAdapter.
// Dispatch). The idempotency key is mandatory (08 §6a); the adapter must dedup
// a re-issue on it.
type DispatchParams struct {
	Operation      string         `json:"operation"`
	Params         map[string]any `json:"params"`
	IdempotencyKey string         `json:"idempotency_key"`
}

// HealthResult is the wire shape of the health method's result (§4.4), mapped
// onto capability.HealthStatus. Healthy is the whole-instance signal; Degraded
// lists what the instance cannot express while healthy, and is absent when
// nothing is degraded. Adapters written in Go return this type.
type HealthResult struct {
	Healthy  bool                 `json:"healthy"`
	Message  string               `json:"message"`
	Degraded []DegradedCapability `json:"degraded,omitempty"`
}

// DegradedCapability is one capability of one operation the instance cannot
// express, and why (§4.4): "credential filter: actor.session.credential_uid
// absent".
type DegradedCapability struct {
	Operation  string `json:"operation"`
	Capability string `json:"capability"`
	Reason     string `json:"reason"`
}

// healthWire is how the host decodes a health result: degraded is kept raw so
// a malformed list is dropped on its own rather than failing the whole probe
// (§4.4).
type healthWire struct {
	Healthy  bool            `json:"healthy"`
	Message  string          `json:"message"`
	Degraded json.RawMessage `json:"degraded,omitempty"`
}

// toStatus maps the wire result onto the engine's health type. A degraded
// value that is not a list of entries is dropped and reported as err; absent
// or null is none.
func (h healthWire) toStatus() (capability.HealthStatus, error) {
	st := capability.HealthStatus{Healthy: h.Healthy, Message: h.Message}
	raw := bytes.TrimSpace(h.Degraded)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return st, nil
	}
	var entries []DegradedCapability
	if err := json.Unmarshal(raw, &entries); err != nil {
		return st, fmt.Errorf("degraded is not a list of {operation, capability, reason}: %w", err)
	}
	for _, d := range entries {
		st.Degraded = append(st.Degraded, capability.DegradedCapability{
			Operation:  d.Operation,
			Capability: d.Capability,
			Reason:     d.Reason,
		})
	}
	return st, nil
}

// errorData is the optional `data` on a JSON-RPC error from invoke/dispatch: the
// reckon error class the adapter reports (§4, mapping 03 §6.2 for reads and
// 08 §5 for writes). A read class is one of RETRY/FALLTHROUGH/UNHEALTHY/FATAL;
// a write class is RETRYABLE_ERROR/FATAL_ERROR. An absent class is treated as
// the safe default by the host (FALLTHROUGH for reads, FATAL_ERROR for writes).
type errorData struct {
	Class string `json:"class"`
}
