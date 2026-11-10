package capability

import (
	"fmt"
	"sort"
)

// CapabilityDescriptor is the LLM-facing declaration of a verb (§5.3): its name,
// an intent description the agent renders into a tool definition, its typed
// inputs, and its output type. Descriptors are data, not code — a new verb is a
// catalog entry plus a binding, never a change to dispatch (§5.1).
type CapabilityDescriptor struct {
	Verb   string       `json:"verb"`
	Intent string       `json:"intent"` // one-line, LLM-facing
	Inputs []InputParam `json:"inputs"`
	Output string       `json:"output"` // output type description
}

// InputParam is one typed verb input.
type InputParam struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "entity", "time_window", "string", "int", "enum", …
	Required bool   `json:"required"`
	Desc     string `json:"desc,omitempty"`
}

// Catalog holds the registered verb descriptors, keyed by verb name.
type Catalog struct {
	byVerb map[string]CapabilityDescriptor
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog { return &Catalog{byVerb: make(map[string]CapabilityDescriptor)} }

// Register adds or replaces a descriptor.
func (c *Catalog) Register(d CapabilityDescriptor) { c.byVerb[d.Verb] = d }

// Descriptor returns the descriptor for a verb, if registered.
func (c *Catalog) Descriptor(verb string) (CapabilityDescriptor, bool) {
	d, ok := c.byVerb[verb]
	return d, ok
}

// Verbs returns the registered verb names, sorted.
func (c *Catalog) Verbs() []string {
	out := make([]string, 0, len(c.byVerb))
	for v := range c.byVerb {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// entity is a shorthand for a required entity input.
func entityInput(name, desc string) InputParam {
	return InputParam{Name: name, Type: "entity", Required: true, Desc: desc}
}

// windowInput is the standard optional time-window input.
func windowInput() InputParam {
	return InputParam{Name: "window", Type: "time_window", Required: false, Desc: "time bound; defaults to the tenant investigation window"}
}

// limitInput is the optional result cap a bulk verb takes; the default is the
// verb's own (§2). What the model is shown of a large answer is bounded
// separately, by the session (adr/0209), so this narrows the query, not the page.
func limitInput(def int) InputParam {
	return InputParam{Name: "limit", Type: "int", Required: false, Desc: fmt.Sprintf("max observations to return (default %d)", def)}
}

// optionalEntityInput is an entity input a verb can be asked without: the
// fleet-wide question ("where did this run", "which hosts gained persistence")
// has no entity to start from, and the entity, when given, narrows the answer.
func optionalEntityInput(name, desc string) InputParam {
	return InputParam{Name: name, Type: "entity", Required: false, Desc: desc}
}

// DefaultCatalog registers the engine's verb descriptors (§2). A verb here is
// resolvable the moment a tenant binds it (§5.1); unbound it is listed as
// unavailable and trimmed from the agent's tool set (§6.3). The verbs still
// missing from §2 land as their bindings do: get_user_context,
// get_process_descendants, get_module_loads, get_failed_auth_burst,
// get_lateral_movement_signals, get_email_messages, get_email_clicks.
//
// Composite spec inputs (ProcessFilter, QuerySpec, scopes) are flattened to
// scalar inputs on the wire, the convention query_external_cases set for
// CaseFilter: every verb tool shares one input shape — an entity, a window and
// scalar extras — and each scalar is a template root a binding can address.
func DefaultCatalog() *Catalog {
	c := NewCatalog()
	for _, d := range []CapabilityDescriptor{
		{
			Verb:   "resolve_entity",
			Intent: "Canonicalize a free-form identifier (IP, hostname, user) into a typed entity with a stable id.",
			Inputs: []InputParam{{Name: "identifier", Type: "string", Required: true}, {Name: "hint_type", Type: "string", Required: false}},
			Output: "entity",
		},
		{
			Verb:   "get_host_context",
			Intent: "Fetch asset/CMDB context for a host: owner, OS, last-seen, sensors.",
			Inputs: []InputParam{entityInput("host", "the host to look up")},
			Output: "host context",
		},
		{
			Verb:   "get_indicator_context",
			Intent: "Enrich an indicator (IP, domain, hash, URL) with threat-intel reputation and sightings.",
			Inputs: []InputParam{entityInput("indicator", "the indicator to enrich")},
			Output: "indicator context",
		},
		{
			Verb:   "get_process_ancestry",
			Intent: "Walk a process's parent chain to root, returning ordered parent-child observations.",
			Inputs: []InputParam{entityInput("process", "the process to walk from"), windowInput()},
			Output: "list<observed_data>",
		},
		{
			Verb:   "get_process_executions",
			Intent: "Search process executions matching an image hash, image path or command-line fragment in a window, across every host unless narrowed to one host or user — the fleet-wide 'where did this run?'.",
			Inputs: []InputParam{
				optionalEntityInput("subject", "optional host or user-account to narrow to; omit to search the whole fleet"),
				windowInput(),
				{Name: "image_hash", Type: "string", Required: false, Desc: "hash of the executable image (SHA-256 preferred)"},
				{Name: "image_path", Type: "string", Required: false, Desc: "path of the executable image"},
				{Name: "command_line_contains", Type: "string", Required: false, Desc: "substring the command line must contain"},
				limitInput(200),
			},
			Output: "list<observed_data>",
		},
		{
			Verb:   "get_persistence_artifacts",
			Intent: "List persistence artifacts created or changed in a window — autostart registry values, scheduled tasks and jobs, services, loaded modules — on one host, or across every host when no host is given.",
			Inputs: []InputParam{
				optionalEntityInput("host", "optional host to narrow to; omit to sweep the fleet"),
				windowInput(),
				limitInput(200),
			},
			Output: "list<observed_data>",
		},
		{
			Verb:   "get_file_activity",
			Intent: "List reads, writes, renames and deletes touching a file, identified by hash or path, in a window; optionally narrowed to one host.",
			Inputs: []InputParam{
				entityInput("file", `the file, by hash or path, e.g. {"file":{"hash":"<sha256>"}} or {"file":{"path":"<path>"}}; a host may be given beside it`),
				windowInput(),
				limitInput(200),
			},
			Output: "list<observed_data>",
		},
		{
			Verb:   "resolve_dns",
			Intent: "List DNS resolutions of a domain name in a window — which hosts asked, and what it resolved to — or every resolution a host made.",
			Inputs: []InputParam{
				entityInput("name", `the domain name, e.g. {"domain":"<name>"}, or a host whose resolutions to list`),
				windowInput(),
				limitInput(200),
			},
			Output: "list<observed_data>",
		},
		{
			Verb:   "get_privilege_changes",
			Intent: "List group-membership changes and privilege grants or revocations affecting a user-account, or performed on a host, in a window.",
			Inputs: []InputParam{
				entityInput("target", "the user-account changed, or the host the change happened on"),
				windowInput(),
				limitInput(200),
			},
			Output: "list<observed_data>",
		},
		{
			Verb:   "query_logs",
			Intent: "Run a raw query against a bound log source and normalize the rows best-effort — the escape hatch when no typed verb asks the question. Prefer a typed verb when one fits.",
			Inputs: []InputParam{
				{Name: "query_text", Type: "string", Required: true, Desc: "the query, in the bound source's own language"},
				{Name: "tool", Type: "string", Required: false, Desc: "name of the bound log source to run it on; forwarded to the binding, which decides what it means"},
				windowInput(),
				limitInput(1000),
			},
			Output: "list<observed_data>",
		},
		{
			Verb:   "pivot_on_indicator",
			Intent: "Search every bound telemetry source for sightings of an indicator — an IP, domain, URL or file hash — in a window; `scopes` names source scopes for the bindings to apply.",
			Inputs: []InputParam{
				entityInput("indicator", `the indicator, e.g. {"ip":"<address>"}, {"domain":"<name>"}, {"url":"<url>"} or {"file":{"hash":"<sha256>"}}`),
				windowInput(),
				{Name: "scopes", Type: "list<string>", Required: false, Desc: "source scopes to search, e.g. edr, proxy, dns, email; forwarded to every fanned-out binding, not applied by the engine; omit for every source"},
				limitInput(500),
			},
			Output: "list<observed_data>",
		},
		{
			Verb:   "enumerate_logons",
			Intent: "List authentication events against a user or host in a window, optionally filtered by outcome.",
			Inputs: []InputParam{entityInput("target", "user-account or host"), windowInput(), {Name: "outcome", Type: "enum", Required: false, Desc: "SUCCESS|FAILURE|ALL"}},
			Output: "list<observed_data>",
		},
		{
			Verb:   "get_network_connections",
			Intent: "Return network connections for a host, IP, or process in a window.",
			Inputs: []InputParam{entityInput("endpoint", "host, IP, or process"), windowInput(), {Name: "direction", Type: "enum", Required: false, Desc: "INBOUND|OUTBOUND|ALL"}},
			Output: "list<observed_data>",
		},
		{
			Verb:   "search_alerts",
			Intent: "Search detection findings / alerts matching a filter in a window.",
			Inputs: []InputParam{{Name: "filter", Type: "alert_filter", Required: true}, windowInput()},
			Output: "list<observed_data>",
		},
		{
			Verb:   "query_external_cases",
			Intent: "Search the case management system of record (ServiceNow, Jira SOC, TheHive) for cases matching a filter — the read side of 'have we seen this before?'.",
			Inputs: []InputParam{{Name: "filter", Type: "string", Required: false, Desc: "free-text query over case title/summary"}, {Name: "queue", Type: "string", Required: false, Desc: "the work queue the case system assigns by (a ServiceNow assignment group, a Jira project/board, a TheHive team) — the tenant's configured queue (policies.case_queue) or one the analyst names"}, {Name: "status", Type: "string", Required: false, Desc: "case status filter, e.g. 'In Progress'"}, windowInput(), {Name: "limit", Type: "int", Required: false, Desc: "max cases to return (default 50)"}},
			Output: "list<observed_data>",
		},
		{
			Verb:   "get_external_case_details",
			Intent: "Fetch full content for one case in the SoR (title, status, summary, link) — a follow-up after query_external_cases returns hits worth deep-diving.",
			Inputs: []InputParam{{Name: "case_id", Type: "string", Required: true, Desc: "the case id/number, e.g. INC0010042 or a sys_id"}},
			Output: "observed_data",
		},
		{
			Verb:   "list_capabilities",
			Intent: "List the verbs resolvable in this tenant and their current availability.",
			Inputs: nil,
			Output: "list<capability_summary>",
		},
	} {
		c.Register(d)
	}
	return c
}
