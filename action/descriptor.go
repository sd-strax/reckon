package action

import (
	"fmt"
	"sort"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/agentapi"
	"github.com/sd-strax/reckon/internal/catalog"
)

// Reversibility classifications (04 §7). The ordered scale is
// IRREVERSIBLE < BEST_EFFORT < REVERSIBLE; per-binding overrides (deferred)
// may move along it under the §7.3 rules. The words are defined once in
// internal/agentapi (adr/0278).
const (
	// ReversibilityReversible: RELIABLE — the tool cleanly undoes the effect;
	// a successful reversal marks the original REVERSED.
	ReversibilityReversible = agentapi.ReversibilityReversible
	// ReversibilityBestEffort: an inverse action exists but its effect is
	// tooling-dependent; the affordance + forward linkage stay live but the
	// original is never marked REVERSED (04 §7.1 Position C).
	ReversibilityBestEffort = agentapi.ReversibilityBestEffort
	// ReversibilityIrreversible: no inverse exists; reversal is structurally
	// impossible.
	ReversibilityIrreversible = agentapi.ReversibilityIrreversible
)

// ActionDescriptor is the write analog of capability.CapabilityDescriptor
// (08 §3): the action surface the LLM sees plus the auth/taxonomy fields it
// declares for discoverability. The agent reads it from GET /api/action-types,
// so it is defined in internal/agentapi (adr/0278), with ReliablyReversible.
type ActionDescriptor = agentapi.ActionDescriptor

// The action categories (#123).
const (
	CategoryContainment  = "containment"
	CategoryEradication  = "eradication"
	CategoryRecovery     = "recovery"
	CategoryNotification = "notification"
	CategoryCaseRecord   = "case_record"
)

// ActionCatalog holds the registered action descriptors, keyed by action_type.
type ActionCatalog struct {
	byType map[string]ActionDescriptor
}

// NewActionCatalog returns an empty catalog.
func NewActionCatalog() *ActionCatalog {
	return &ActionCatalog{byType: make(map[string]ActionDescriptor)}
}

// Register adds or replaces a descriptor. It refuses a descriptor that breaks
// the tool-name rules (adr/0382): its action type gives an invalid tool name
// or the tool name of another registered type, or an input takes the name of
// a field every request carries or is declared twice.
func (c *ActionCatalog) Register(d ActionDescriptor) error {
	inputs := make([]string, 0, len(d.Inputs))
	for _, in := range d.Inputs {
		inputs = append(inputs, in.Name)
	}
	if err := catalog.ValidateActionDescriptor(d.ActionType, inputs); err != nil {
		return fmt.Errorf("register action descriptor: %w", err)
	}
	name := catalog.ActionToolName(d.ActionType)
	for t := range c.byType {
		if t != d.ActionType && catalog.ActionToolName(t) == name {
			return fmt.Errorf("register action descriptor: action types %q and %q both give the tool name %q", t, d.ActionType, name)
		}
	}
	c.byType[d.ActionType] = d
	return nil
}

// ToolNames returns the tool name of every registered action type
// (catalog.ActionToolName), so a verb can be checked against them.
func (c *ActionCatalog) ToolNames() []string {
	out := make([]string, 0, len(c.byType))
	for _, t := range c.ActionTypes() {
		out = append(out, catalog.ActionToolName(t))
	}
	return out
}

// Descriptor returns the descriptor for an action type, if registered.
func (c *ActionCatalog) Descriptor(actionType string) (ActionDescriptor, bool) {
	d, ok := c.byType[actionType]
	return d, ok
}

// ActionTypes returns the registered action types, sorted.
func (c *ActionCatalog) ActionTypes() []string {
	out := make([]string, 0, len(c.byType))
	for t := range c.byType {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// DefaultActionCatalog registers the v0 action descriptors the fixture write
// path can service (a representative subset; the rest land with their bindings,
// 08 §8). Tiers follow 04 §2.
//
// FROZEN v0 CATALOG. This function is the single authoritative source of truth
// for the dispatchable action vocabulary — the `action_type` strings, tiers,
// reversibility, and D3FEND annotations. `design/04-action-authorization.md §2.1`
// is the broader roadmap taxonomy and MUST match these rows where they overlap;
// TestDefaultActionCatalog_Frozen pins the set so neither can drift silently.
// D3FEND ids are MITRE technique ids (d3fend.mitre.org), illustrative metadata
// only (04 §2.1) — not load-bearing for control flow. Reversal actions carry a
// D3FEND id only where the ontology's Restore tactic names the restoration as a
// first-class technique (D3-RNA, D3-RE, D3-ULA); other reversals carry none.
//
// It fails when Register refuses a descriptor, and backend startup fails with
// it (adr/0382).
func DefaultActionCatalog() (*ActionCatalog, error) {
	// The entity types each family acts on (adr/0354). An IP address is not a
	// host: a host is isolated by the host the tool knows, and one address can
	// belong to different machines over time. A file stands for its hash.
	var (
		hostTargets      = []string{identity.TypeHost}
		accountTargets   = []string{identity.TypeUserAccount}
		emailTargets     = []string{identity.TypeEmailMessage}
		indicatorTargets = []string{identity.TypeIPv4Addr, identity.TypeIPv6Addr, identity.TypeDomainName, identity.TypeURL, identity.TypeFile}
	)
	c := NewActionCatalog()
	for _, d := range []ActionDescriptor{
		{
			ActionType:    "host.isolate",
			Name:          "Isolate host",
			Category:      CategoryContainment,
			Intent:        "Network-isolate a host, cutting it off from everything but reckon's control channel. Reversible.",
			TargetTypes:   hostTargets,
			Inputs:        []capability.InputParam{{Name: "host", Type: "entity", Required: true}},
			DefaultTier:   aggregate.TierT2,
			Reversibility: ReversibilityReversible,
			ReversibleBy:  "host.unisolate",
			D3FEND:        "D3-NI",
		},
		{
			ActionType:    "host.unisolate",
			Name:          "Restore host network access",
			Category:      CategoryRecovery,
			Intent:        "Restore a host's network connectivity — the inverse of host.isolate.",
			TargetTypes:   hostTargets,
			Inputs:        []capability.InputParam{{Name: "host", Type: "entity", Required: true}},
			DefaultTier:   aggregate.TierT2, // reversing is the same tier as the original (04 §7)
			Reversibility: ReversibilityReversible,
			ReversibleBy:  "host.isolate",
			D3FEND:        "D3-RNA", // Restore Network Access (Restore tactic)
		},
		{
			ActionType:    "account.disable",
			Name:          "Disable account",
			Category:      CategoryContainment,
			Intent:        "Disable a user account, blocking new sessions until re-enabled. Reversible (account.enable); already-established sessions need session revocation separately.",
			TargetTypes:   accountTargets,
			Inputs:        []capability.InputParam{{Name: "account", Type: "entity", Required: true}},
			DefaultTier:   aggregate.TierT2,
			Reversibility: ReversibilityReversible,
			ReversibleBy:  "account.enable",
			D3FEND:        "D3-AL", // Account Locking
		},
		{
			ActionType:    "account.enable",
			Name:          "Re-enable account",
			Category:      CategoryRecovery,
			Intent:        "Re-enable a disabled user account — the inverse of account.disable. Use to retract over-containment once the account is cleared.",
			TargetTypes:   accountTargets,
			Inputs:        []capability.InputParam{{Name: "account", Type: "entity", Required: true}},
			DefaultTier:   aggregate.TierT2, // reversing is the same tier as the original (04 §7)
			Reversibility: ReversibilityReversible,
			ReversibleBy:  "account.disable",
			D3FEND:        "D3-ULA", // Unlock Account (Restore tactic)
		},
		{
			ActionType:    "email.quarantine",
			Name:          "Quarantine email",
			Category:      CategoryContainment,
			Intent:        "Quarantine a single message from a mailbox. Reversible (release).",
			TargetTypes:   emailTargets,
			Inputs:        []capability.InputParam{{Name: "message", Type: "entity", Required: true}},
			DefaultTier:   aggregate.TierT2,
			Reversibility: ReversibilityReversible,
			ReversibleBy:  "email.release",
			D3FEND:        "D3-ER", // Email Removal (reversible application)
		},
		{
			ActionType:    "email.release",
			Name:          "Release email from quarantine",
			Category:      CategoryRecovery,
			Intent:        "Release a quarantined message back to its mailbox — the inverse of email.quarantine.",
			TargetTypes:   emailTargets,
			Inputs:        []capability.InputParam{{Name: "message", Type: "entity", Required: true}},
			DefaultTier:   aggregate.TierT2,
			Reversibility: ReversibilityReversible,
			ReversibleBy:  "email.quarantine",
			D3FEND:        "D3-RE", // Restore Email (Restore tactic)
		},
		{
			ActionType:    "email.purge",
			Name:          "Purge email from mailboxes",
			Category:      CategoryEradication,
			Intent:        "Permanently delete a message from mailboxes. IRREVERSIBLE.",
			TargetTypes:   emailTargets,
			Inputs:        []capability.InputParam{{Name: "message", Type: "entity", Required: true}},
			DefaultTier:   aggregate.TierT3,
			Reversibility: ReversibilityIrreversible,
			D3FEND:        "D3-ER", // Email Removal (terminal application)
		},
		{
			ActionType:  "ioc.block",
			Name:        "Block indicator",
			Category:    CategoryContainment,
			Intent:      "Block an IOC (hash/IP/domain) at the perimeter — firewall/proxy/DNS denylist. Use this to block a malicious IP, domain, or file hash. Reversible by ioc.unblock, but best-effort: whether removal fully undoes the block depends on the tenant's tooling (TTL lists, propagated RPZ, partner feeds).",
			TargetTypes: indicatorTargets,
			Inputs:      []capability.InputParam{{Name: "indicator", Type: "entity", Required: true}},
			DefaultTier: aggregate.TierT2,
			// best_effort + linked (04 §7 Position C): the reverse-this-action
			// affordance and the forward reversal_of_ref link are live, but a
			// successful ioc.unblock does NOT flip the block to REVERSED unless a
			// per-binding override upgrades this to "reversible" (deferred). The
			// block honestly stays SUCCEEDED; the analyst judges residual effect.
			Reversibility: ReversibilityBestEffort,
			ReversibleBy:  "ioc.unblock",
			D3FEND:        "D3-NTF", // Network Traffic Filtering
		},
		{
			ActionType:    "ioc.unblock",
			Name:          "Unblock indicator",
			Category:      CategoryRecovery,
			Intent:        "Remove an IOC (hash/IP/domain) from the perimeter denylist — the inverse of ioc.block. Best-effort: whether removal fully undoes propagation depends on the tenant's tooling.",
			TargetTypes:   indicatorTargets,
			Inputs:        []capability.InputParam{{Name: "indicator", Type: "entity", Required: true}},
			DefaultTier:   aggregate.TierT2, // reversing is the same tier as the original (04 §7)
			Reversibility: ReversibilityBestEffort,
			ReversibleBy:  "ioc.block",
			D3FEND:        "D3-RNA", // Restore Network Access (Restore tactic)
		},

		// The ticketing family (04 §2, 07 §7.1): operational handoff + collaboration
		// with the org's system of record. All T2 (external dispatch is always
		// T2+, 04 §1) and IRREVERSIBLE-ADDITIVE: the SoR is append-only — records,
		// notifications, and history are permanent; ticket.close is a forward
		// transition, NEVER a reversal of ticket.create. No D3FEND ids
		// (operational handoff, not a defensive technique).
		{
			ActionType: "ticket.create",
			Name:       "Open a ticket",
			Category:   CategoryCaseRecord,
			Intent:     "Open a ticket/incident in the org's system of record (Jira, ServiceNow, Linear) — the operational-handoff vehicle. Target the DESTINATION project/queue (resolved_identifier = project key or assignment group); put the entities the ticket concerns in evidence_refs, NOT targets. Irreversible: the record is permanent; closing later is a forward transition (ticket.close), not an undo.",
			Inputs: []capability.InputParam{
				{Name: "summary", Type: "string", Required: true},
				{Name: "description", Type: "string"},
				{Name: "issue_type", Type: "string"},
				{Name: "assignee", Type: "string"},
				// Vendor-neutral urgency (adr/0127): low | medium | high | critical;
				// each ticketing binding maps it onto its own field.
				{Name: "priority", Type: "string"},
			},
			DefaultTier:   aggregate.TierT2,
			Reversibility: ReversibilityIrreversible,
		},
		{
			ActionType:    "ticket.comment",
			Name:          "Comment on a ticket",
			Category:      CategoryCaseRecord,
			Intent:        "Add a comment to an existing ticket. Target the ticket (resolved_identifier = ticket id). Irreversible: comments are part of the permanent record.",
			Inputs:        []capability.InputParam{{Name: "body", Type: "string", Required: true}},
			DefaultTier:   aggregate.TierT2,
			Reversibility: ReversibilityIrreversible,
		},
		{
			ActionType:    "ticket.transition",
			Name:          "Move a ticket to another state",
			Category:      CategoryCaseRecord,
			Intent:        "Move an existing ticket to another workflow state (e.g. In Progress, Blocked). Target the ticket (resolved_identifier = ticket id). Irreversible: the state history is permanent; moving back is another forward transition.",
			Inputs:        []capability.InputParam{{Name: "to_status", Type: "string", Required: true}},
			DefaultTier:   aggregate.TierT2,
			Reversibility: ReversibilityIrreversible,
		},
		{
			ActionType:    "ticket.close",
			Name:          "Close a ticket",
			Category:      CategoryCaseRecord,
			Intent:        "Close an existing ticket — a forward transition, NOT a reversal of ticket.create (the record stays). Target the ticket (resolved_identifier = ticket id).",
			Inputs:        []capability.InputParam{{Name: "resolution", Type: "string"}},
			DefaultTier:   aggregate.TierT2,
			Reversibility: ReversibilityIrreversible,
		},

		// The comms family (binding §4): an outbound message is an
		// reckon-action like any external dispatch — T2, because there is no T1
		// external action (04 §1: chat is named explicitly; low friction comes
		// from the approval surface, never a lower tier). IRREVERSIBLE: a sent
		// message is permanent. The mandatory pre-send preview IS the approval
		// surface. A `thread_ref` parameter marks a follow-up on an existing
		// comms thread; the thread state itself lives in the comms package.
		{
			ActionType: "notify.slack",
			Name:       "Post to a Slack channel",
			Category:   CategoryNotification,
			Intent:     "Send a Slack message from the investigation — coordination, hand-offs, notifications. Target the DESTINATION (resolved_identifier = #channel or @user). The analyst previews and approves before anything is sent; the message becomes a tracked comms thread with follow-up. Irreversible: a sent message is permanent.",
			Inputs: []capability.InputParam{
				{Name: "message", Type: "string", Required: true},
				{Name: "subject", Type: "string"},
				{Name: "follow_up_hours", Type: "int"},
				{Name: "thread_ref", Type: "string"},
			},
			DefaultTier:   aggregate.TierT2,
			Reversibility: ReversibilityIrreversible,
		},
		{
			ActionType: "notify.email",
			Name:       "Send an email",
			Category:   CategoryNotification,
			Intent:     "Send an email from the investigation — notifications with a formal record (compliance, management, external parties). Target the recipient (resolved_identifier = address). The analyst previews and approves before anything is sent; the message becomes a tracked comms thread with follow-up. Irreversible: a sent email is permanent.",
			Inputs: []capability.InputParam{
				{Name: "subject", Type: "string", Required: true},
				{Name: "body", Type: "string", Required: true},
				{Name: "follow_up_hours", Type: "int"},
				{Name: "thread_ref", Type: "string"},
			},
			DefaultTier:   aggregate.TierT2,
			Reversibility: ReversibilityIrreversible,
		},
	} {
		if err := c.Register(d); err != nil {
			return nil, err
		}
	}
	return c, nil
}
