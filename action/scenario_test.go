package action

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
)

// TestShippedActionScenario loads the committed write-side config + bundled
// action fixtures and drives request → resolve → dispatch end to end — a
// regression guard that the shipped fixtures/config stay valid and wired.
func TestShippedActionScenario(t *testing.T) {
	cfg, err := LoadActionConfig("../examples/action/lateral-movement.yaml")
	if err != nil {
		t.Fatalf("load shipped action config: %v", err)
	}
	resolver, catalog, err := BuildActionResolver(cfg, "../fixtures", true)
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}

	// host.isolate, account.disable/enable, ioc.block/unblock, and the ticketing
	// family advertise as available.
	avail := map[string]ActionAvailability{}
	for _, s := range resolver.ListActionTypes(catalog) {
		avail[s.Descriptor.ActionType] = s.Status
	}
	for _, at := range []string{
		"host.isolate", "host.unisolate", "account.disable", "account.enable", "ioc.block", "ioc.unblock",
		"ticket.create", "ticket.comment", "ticket.transition", "ticket.close",
	} {
		if avail[at] != ActionAvailable {
			t.Errorf("bound action %s = %q; want available", at, avail[at])
		}
	}

	// ioc.block dispatches end-to-end against the C2 indicator — the containment
	// the agent could not complete before the catalog was frozen + surfaced.
	blockCmd, err := BuildRequestCommand(catalog, ActionRequest{
		ActionType:   "ioc.block",
		Targets:      []aggregate.TargetSpec{{EntityRef: "ipv4-addr--1", ResolvedIdentifier: "185.220.101.5"}},
		EvidenceRefs: []string{"observed-data--c2"},
		Rationale:    "block the C2 channel",
	}, time.Now())
	if err != nil {
		t.Fatalf("build ioc.block request: %v", err)
	}
	blockRes, _, err := resolver.Resolve(context.Background(), DispatchRequest{
		ActionID:   blockCmd.ActionID,
		ActionType: blockCmd.ActionType,
		Targets:    blockCmd.Targets,
	})
	if err != nil {
		t.Fatalf("resolve/dispatch ioc.block: %v", err)
	}
	if blockRes.FinalOutcome != OutcomeSucceeded {
		t.Errorf("ioc.block outcome = %q; want SUCCEEDED", blockRes.FinalOutcome)
	}

	// ioc.unblock dispatches as a standalone action — not a tracked reversal of
	// the block (04 §7): no reversal_of_ref, and the block record stays SUCCEEDED.
	unblockCmd, err := BuildRequestCommand(catalog, ActionRequest{
		ActionType:   "ioc.unblock",
		Targets:      []aggregate.TargetSpec{{EntityRef: "ipv4-addr--1", ResolvedIdentifier: "185.220.101.5"}},
		EvidenceRefs: []string{"observed-data--c2"},
		Rationale:    "shared CDN edge — legit traffic breaking; analyst judged removal safe",
	}, time.Now())
	if err != nil {
		t.Fatalf("build ioc.unblock request: %v", err)
	}
	unblockRes, _, err := resolver.Resolve(context.Background(), DispatchRequest{
		ActionID:   unblockCmd.ActionID,
		ActionType: unblockCmd.ActionType,
		Targets:    unblockCmd.Targets,
	})
	if err != nil {
		t.Fatalf("resolve/dispatch ioc.unblock: %v", err)
	}
	if unblockRes.FinalOutcome != OutcomeSucceeded {
		t.Errorf("ioc.unblock outcome = %q; want SUCCEEDED", unblockRes.FinalOutcome)
	}

	// ticket.create dispatches end-to-end — the operational handoff (04 §2.2):
	// target = the DESTINATION queue (not the hosts being handed off, which ride
	// in evidence_refs); irreversible-additive, stays T2.
	ticketCmd, err := BuildRequestCommand(catalog, ActionRequest{
		ActionType:   "ticket.create",
		Targets:      []aggregate.TargetSpec{{ResolvedIdentifier: "IT-OPS"}},
		EvidenceRefs: []string{"x-host--1"},
		Parameters:   []byte(`{"summary":"Reimage WIN-FILE01 after RDP lateral movement","issue_type":"remediation"}`),
		Rationale:    "hand off host rebuild to IT after containment",
	}, time.Now())
	if err != nil {
		t.Fatalf("build ticket.create request: %v", err)
	}
	if ticketCmd.Tier != aggregate.TierT2 || ticketCmd.Reversibility != ReversibilityIrreversible {
		t.Errorf("ticket.create tier/reversibility = %s/%s; want T2/irreversible", ticketCmd.Tier, ticketCmd.Reversibility)
	}
	ticketRes, _, err := resolver.Resolve(context.Background(), DispatchRequest{
		ActionID:   ticketCmd.ActionID,
		ActionType: ticketCmd.ActionType,
		Targets:    ticketCmd.Targets,
	})
	if err != nil {
		t.Fatalf("resolve/dispatch ticket.create: %v", err)
	}
	if ticketRes.FinalOutcome != OutcomeSucceeded {
		t.Errorf("ticket.create outcome = %q; want SUCCEEDED", ticketRes.FinalOutcome)
	}

	// Build a request_action for host.isolate (T2, single target), then dispatch
	// it through the resolver into the fixture write adapter.
	cmd, err := BuildRequestCommand(catalog, ActionRequest{
		ActionType:   "host.isolate",
		Targets:      []aggregate.TargetSpec{{EntityRef: "x-host--1", ResolvedIdentifier: "WIN-FILE01"}},
		EvidenceRefs: []string{"observed-data--rdp"},
		Rationale:    "contain the RDP lateral movement",
	}, time.Now())
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if cmd.Tier != aggregate.TierT2 {
		t.Errorf("host.isolate tier = %q; want T2", cmd.Tier)
	}

	res, binding, err := resolver.Resolve(context.Background(), DispatchRequest{
		ActionID:   cmd.ActionID,
		ActionType: cmd.ActionType,
		Targets:    cmd.Targets,
	})
	if err != nil {
		t.Fatalf("resolve/dispatch: %v", err)
	}
	if res.FinalOutcome != OutcomeSucceeded {
		t.Errorf("dispatch outcome = %q; want SUCCEEDED", res.FinalOutcome)
	}
	if binding.Adapter != "fixture_write" || res.AuditDepth != AuditFull {
		t.Errorf("unexpected binding/audit: %s / %s", binding.Adapter, res.AuditDepth)
	}
	if res.AdapterRequestID == "" {
		t.Error("no adapter_request_id stamped")
	}
}

// TestShippedImpossibleTravelActionScenario: the second bundled scenario's
// write side (adr/0058). The account and the indicator dispatch end to end;
// host isolation is deliberately unbound — there is no host — and advertises
// as unavailable, so the agent is told rather than left to try.
func TestShippedImpossibleTravelActionScenario(t *testing.T) {
	cfg, err := LoadActionConfig("../examples/action/impossible-travel.yaml")
	if err != nil {
		t.Fatalf("load shipped action config: %v", err)
	}
	resolver, catalog, err := BuildActionResolver(cfg, "../fixtures", true)
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}
	avail := map[string]ActionAvailability{}
	for _, s := range resolver.ListActionTypes(catalog) {
		avail[s.Descriptor.ActionType] = s.Status
	}
	for _, at := range []string{"account.disable", "account.enable", "ioc.block", "ioc.unblock", "ticket.create", "ticket.close", "notify.slack", "notify.email"} {
		if avail[at] != ActionAvailable {
			t.Errorf("bound action %s = %q; want available", at, avail[at])
		}
	}
	for _, at := range []string{"host.isolate", "host.unisolate"} {
		if avail[at] == ActionAvailable {
			t.Errorf("%s is available but the scenario has no host — it must be unbound", at)
		}
	}

	cmd, err := BuildRequestCommand(catalog, ActionRequest{
		ActionType:   "account.disable",
		Targets:      []aggregate.TargetSpec{{EntityRef: "user-account--1", ResolvedIdentifier: `CONTOSO\m.reyes`}},
		EvidenceRefs: []string{"observed-data--foreign-signin"},
		Rationale:    "contain the account behind the foreign legacy-protocol sign-in",
	}, time.Now())
	if err != nil {
		t.Fatalf("build account.disable request: %v", err)
	}
	res, _, err := resolver.Resolve(context.Background(), DispatchRequest{ActionID: cmd.ActionID, ActionType: cmd.ActionType, Targets: cmd.Targets})
	if err != nil {
		t.Fatalf("resolve/dispatch account.disable: %v", err)
	}
	if res.FinalOutcome != OutcomeSucceeded {
		t.Errorf("account.disable outcome = %q; want SUCCEEDED", res.FinalOutcome)
	}
}

// TestShippedScenariosBindEveryInverse guards the Reverse path in the demo: a
// bound action whose catalog entry names an inverse must have that inverse bound
// too, or the reversal is approved and then fails at dispatch with no binding.
func TestShippedScenariosBindEveryInverse(t *testing.T) {
	for _, path := range []string{
		"../examples/action/lateral-movement.yaml",
		"../examples/action/impossible-travel.yaml",
	} {
		cfg, err := LoadActionConfig(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		catalog := DefaultActionCatalog()
		for at := range cfg.Bindings {
			d, ok := catalog.Descriptor(at)
			if !ok || d.ReversibleBy == "" {
				continue
			}
			if len(cfg.Bindings[d.ReversibleBy]) == 0 {
				t.Errorf("%s: %s is bound but its inverse %s is not", path, at, d.ReversibleBy)
			}
		}
	}
}

// TestShippedScenarioContainsEveryAccountItSurfaces: the RDP scenario surfaces
// two accounts (svc_backup and jdoe); disabling and re-enabling either one
// completes in the demo, and an account the scenario never mentions still
// fails honestly (#149).
func TestShippedScenarioContainsEveryAccountItSurfaces(t *testing.T) {
	cfg, err := LoadActionConfig("../examples/action/lateral-movement.yaml")
	if err != nil {
		t.Fatal(err)
	}
	resolver, _, err := BuildActionResolver(cfg, "../fixtures", true)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := func(actionType, account string) (WriteResult, error) {
		res, _, err := resolver.Resolve(context.Background(), DispatchRequest{
			ActionID: uuid.New(), ActionType: actionType,
			Targets: []aggregate.TargetSpec{{EntityRef: "user-account--1", ResolvedIdentifier: account}},
		})
		return res, err
	}
	for _, account := range []string{`CONTOSO\svc_backup`, `CONTOSO\jdoe`} {
		for _, at := range []string{"account.disable", "account.enable"} {
			res, err := dispatch(at, account)
			if err != nil || res.FinalOutcome != OutcomeSucceeded {
				t.Errorf("%s %s: outcome=%q err=%v; want SUCCEEDED", at, account, res.FinalOutcome, err)
			}
		}
	}
	if _, err := dispatch("account.disable", `CONTOSO\nobody`); err == nil {
		t.Error("an account the scenario never mentions should fail, not succeed")
	}
}
