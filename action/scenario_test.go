package action

import (
	"context"
	"testing"
	"time"

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
	c2 := map[string]any{"value": "185.220.101.5"}
	blockCmd, blockRes, _, err := planAndResolve(t, resolver, catalog, ActionRequest{
		ActionType:   "ioc.block",
		Targets:      []aggregate.TargetSpec{{EntityRef: "ipv4-addr--1"}},
		EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-0000000000c2"},
		Rationale:    "block the C2 channel",
	}, c2)
	if blockCmd.Targets[0].ResolvedIdentifier != "185.220.101.5" {
		t.Errorf("ioc.block identifier = %q; want the address's value", blockCmd.Targets[0].ResolvedIdentifier)
	}
	if err != nil {
		t.Fatalf("resolve/dispatch ioc.block: %v", err)
	}
	if blockRes.FinalOutcome != OutcomeSucceeded {
		t.Errorf("ioc.block outcome = %q; want SUCCEEDED", blockRes.FinalOutcome)
	}

	// ioc.unblock dispatches as a standalone action — not a tracked reversal of
	// the block (04 §7): no reversal_of_ref, and the block record stays SUCCEEDED.
	_, unblockRes, _, err := planAndResolve(t, resolver, catalog, ActionRequest{
		ActionType:   "ioc.unblock",
		Targets:      []aggregate.TargetSpec{{EntityRef: "ipv4-addr--1"}},
		EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-0000000000c2"},
		Rationale:    "shared CDN edge — legit traffic breaking; analyst judged removal safe",
	}, c2)
	if err != nil {
		t.Fatalf("resolve/dispatch ioc.unblock: %v", err)
	}
	if unblockRes.FinalOutcome != OutcomeSucceeded {
		t.Errorf("ioc.unblock outcome = %q; want SUCCEEDED", unblockRes.FinalOutcome)
	}

	// ticket.create dispatches end-to-end — the operational handoff (04 §2.2):
	// target = the DESTINATION queue (not the hosts being handed off, which ride
	// in evidence_refs); irreversible-additive, stays T2.
	ticketCmd, ticketRes, _, err := planAndResolve(t, resolver, catalog, ActionRequest{
		ActionType:   "ticket.create",
		Targets:      []aggregate.TargetSpec{{ResolvedIdentifier: "IT-OPS"}},
		EvidenceRefs: []string{"reckon-host--0d000000-0000-4000-8000-000000000001"},
		Parameters:   []byte(`{"summary":"Reimage WIN-FILE01 after RDP lateral movement","issue_type":"remediation"}`),
		Rationale:    "hand off host rebuild to IT after containment",
	}, nil)
	if ticketCmd.Tier != aggregate.TierT2 || ticketCmd.Reversibility != ReversibilityIrreversible {
		t.Errorf("ticket.create tier/reversibility = %s/%s; want T2/irreversible", ticketCmd.Tier, ticketCmd.Reversibility)
	}
	// A destination type keeps the requester's identifier (adr/0406).
	if ticketCmd.Targets[0].ResolvedIdentifier != "IT-OPS" {
		t.Errorf("ticket.create destination = %q; want IT-OPS as requested", ticketCmd.Targets[0].ResolvedIdentifier)
	}
	if err != nil {
		t.Fatalf("resolve/dispatch ticket.create: %v", err)
	}
	if ticketRes.FinalOutcome != OutcomeSucceeded {
		t.Errorf("ticket.create outcome = %q; want SUCCEEDED", ticketRes.FinalOutcome)
	}

	// Build a request_action for host.isolate (T2, single target), plan it
	// against the stored host, then dispatch it through the frozen binding
	// into the fixture write adapter. The host stores its hostname with the
	// reported case.
	cmd, res, binding, err := planAndResolve(t, resolver, catalog, ActionRequest{
		ActionType:   "host.isolate",
		Targets:      []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}},
		EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000ad9"},
		Rationale:    "contain the RDP lateral movement",
	}, map[string]any{"hostname": "WIN-FILE01"})
	if cmd.Tier != aggregate.TierT2 {
		t.Errorf("host.isolate tier = %q; want T2", cmd.Tier)
	}
	if cmd.Targets[0].ResolvedIdentifier != "WIN-FILE01" || cmd.Binding.Adapter != "fixture_write" {
		t.Errorf("planned identifier %q via %+v; want WIN-FILE01 via fixture_write", cmd.Targets[0].ResolvedIdentifier, cmd.Binding)
	}
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

	// The domain account stores its login domain-qualified, as the
	// normalizer composes it (capability/normalizer_user.go).
	_, res, _, err := planAndResolve(t, resolver, catalog, ActionRequest{
		ActionType:   "account.disable",
		Targets:      []aggregate.TargetSpec{{EntityRef: "user-account--1"}},
		EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-00000000f051"},
		Rationale:    "contain the account behind the foreign legacy-protocol sign-in",
	}, map[string]any{"account_login": `CONTOSO\m.reyes`, "domain": "CONTOSO", "account_type": "windows-domain"})
	if err != nil {
		t.Fatalf("resolve/dispatch account.disable: %v", err)
	}
	if res.FinalOutcome != OutcomeSucceeded {
		t.Errorf("account.disable outcome = %q; want SUCCEEDED", res.FinalOutcome)
	}
}

// TestShippedExposedBucketActionScenario: the cloud-rooted scenario's write
// side. The principal and the source address dispatch end to end; there is no
// host, so host isolation is unbound and advertises as unavailable, and a
// principal the scenario never mentions fails honestly.
func TestShippedExposedBucketActionScenario(t *testing.T) {
	cfg, err := LoadActionConfig("../examples/action/exposed-bucket.yaml")
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

	// A cloud principal stores no login: the identifier rule reads its
	// display name, the IAM user name (capability/normalizer_cloud.go).
	principal := func(name string) map[string]any {
		return map[string]any{"user_id": "arn:aws:iam::111122223333:user/" + name, "account_type": "aws", "display_name": name}
	}
	dispatch := func(actionType, ref string, entity map[string]any) (WriteResult, error) {
		t.Helper()
		_, res, _, err := planAndResolve(t, resolver, catalog, ActionRequest{
			ActionType:   actionType,
			Targets:      []aggregate.TargetSpec{{EntityRef: ref}},
			EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-0000000000b9"},
			Rationale:    "contain the principal behind the public bucket policy",
		}, entity)
		return res, err
	}
	for _, c := range []struct {
		actionType, ref string
		entity          map[string]any
	}{
		{"account.disable", "user-account--1", principal("deploy-ci")}, {"account.enable", "user-account--1", principal("deploy-ci")},
		{"ioc.block", "ipv4-addr--1", map[string]any{"value": "203.0.113.77"}}, {"ioc.unblock", "ipv4-addr--1", map[string]any{"value": "203.0.113.77"}},
	} {
		if res, err := dispatch(c.actionType, c.ref, c.entity); err != nil || res.FinalOutcome != OutcomeSucceeded {
			t.Errorf("%s(%v): outcome=%q err=%v; want SUCCEEDED", c.actionType, c.entity, res.FinalOutcome, err)
		}
	}
	if _, err := dispatch("account.disable", "user-account--1", principal("someone-else")); err == nil {
		t.Error("account.disable on a principal the scenario never mentions should fail, not succeed")
	}
}

// TestShippedScenariosBindEveryInverse guards the Reverse path in the demo: a
// bound action whose catalog entry names an inverse must have that inverse bound
// too, or the reversal is approved and then fails at dispatch with no binding.
func TestShippedScenariosBindEveryInverse(t *testing.T) {
	for _, path := range []string{
		"../examples/action/lateral-movement.yaml",
		"../examples/action/impossible-travel.yaml",
		"../examples/action/exposed-bucket.yaml",
	} {
		cfg, err := LoadActionConfig(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		catalog := defaultActionCatalog(t)
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
	resolver, catalog, err := BuildActionResolver(cfg, "../fixtures", true)
	if err != nil {
		t.Fatal(err)
	}
	// Each account stores its login domain-qualified, as the normalizer
	// composes a domain account's (capability/normalizer_user.go).
	dispatch := func(actionType, account string) (WriteResult, error) {
		_, res, _, err := planAndResolve(t, resolver, catalog, ActionRequest{
			ActionType:   actionType,
			Targets:      []aggregate.TargetSpec{{EntityRef: "user-account--1"}},
			EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-0000000000a1"},
			Rationale:    "contain the account",
		}, map[string]any{"account_login": account, "domain": "CONTOSO", "account_type": "windows-domain"})
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

// planAndResolve is the request path a shipped scenario exercises (adr/0406):
// build the request, plan it against the target's stored entity (nil for a
// destination type), and dispatch through the frozen binding. entity holds
// the properties the normalizer stores for the scenario's entity.
func planAndResolve(t *testing.T, resolver *ActionResolver, catalog *ActionCatalog, req ActionRequest, entity map[string]any) (aggregate.RequestAction, WriteResult, ActionBinding, error) {
	t.Helper()
	cmd, err := BuildRequestCommand(catalog, req, time.Now())
	if err != nil {
		t.Fatalf("build %s request: %v", req.ActionType, err)
	}
	in := PlanInput{ActionType: cmd.ActionType, Targets: cmd.Targets}
	if d, _ := catalog.Descriptor(cmd.ActionType); len(d.TargetTypes) > 0 {
		for range cmd.Targets {
			in.Entities = append(in.Entities, TargetEntity{Properties: entity})
		}
	}
	plan, err := resolver.Plan(in)
	if err != nil {
		return cmd, WriteResult{}, ActionBinding{}, err
	}
	cmd.Targets, cmd.Binding = plan.Targets, plan.Binding
	res, binding, err := resolver.Resolve(context.Background(), DispatchRequest{
		ActionID: cmd.ActionID, ActionType: cmd.ActionType, Targets: cmd.Targets, Binding: cmd.Binding,
	})
	return cmd, res, binding, err
}
