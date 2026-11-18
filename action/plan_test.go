package action

import (
	"strings"
	"testing"

	"github.com/sd-strax/reckon/aggregate"
)

// planResolver builds a resolver over bindings whose adapters are all
// configured and healthy.
func planResolver(bindings map[string][]ActionBinding) *ActionResolver {
	adapters := map[string]WriteAdapter{}
	for _, bs := range bindings {
		for _, b := range bs {
			adapters[b.Adapter] = &stubWriteAdapter{name: b.Adapter, healthy: true, result: okResult()}
		}
	}
	return NewActionResolver(bindings, adapters)
}

func hostEntity(props map[string]any, ids ...SourceID) TargetEntity {
	vendorIDs, conflicts := VendorIDs(ids)
	out := map[string]any{VendorIDsKey: vendorIDs}
	for k, v := range props {
		out[k] = v
	}
	return TargetEntity{Properties: out, VendorConflicts: conflicts}
}

func isolateBy(adapter, rule string, priority int) ActionBinding {
	return ActionBinding{ActionType: "host.isolate", Adapter: adapter, Operation: "op", Priority: priority,
		Identifier: map[string]string{"reckon-host": rule},
		Params:     map[string]any{"device_id": "${target.resolved_identifier}"}}
}

// TestPlan_DerivesTheHostname (adr/0406): the planner renders the binding's
// rule from the stored host, freezes the binding, and overwrites any
// identifier the requester sent.
func TestPlan_DerivesTheHostname(t *testing.T) {
	r := planResolver(map[string][]ActionBinding{"host.isolate": {isolateBy("fixture_write", "${target.entity.hostname}", 100)}})
	plan, err := r.Plan(PlanInput{
		ActionType: "host.isolate",
		Targets:    []aggregate.TargetSpec{{EntityRef: "reckon-host--1", ResolvedIdentifier: "SOMEWHERE-ELSE"}},
		Entities:   []TargetEntity{hostEntity(map[string]any{"hostname": "WIN-FILE01"})},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Targets[0].ResolvedIdentifier != "WIN-FILE01" {
		t.Errorf("identifier = %q; want WIN-FILE01 from the stored host", plan.Targets[0].ResolvedIdentifier)
	}
	if plan.Binding != (aggregate.FrozenBinding{Adapter: "fixture_write", Operation: "op"}) {
		t.Errorf("binding = %+v; want fixture_write op", plan.Binding)
	}
}

// TestPlan_MissingFieldGivesWay: a higher-priority binding whose rule names a
// field the entity lacks gives way to the next; nothing has been sent.
func TestPlan_MissingFieldGivesWay(t *testing.T) {
	r := planResolver(map[string][]ActionBinding{"host.isolate": {
		isolateBy("edr", "${target.entity.vendor_ids.falcon}", 200),
		isolateBy("fixture_write", "${target.entity.hostname}", 100),
	}})
	plan, err := r.Plan(PlanInput{
		ActionType: "host.isolate",
		Targets:    []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}},
		Entities:   []TargetEntity{hostEntity(map[string]any{"hostname": "WIN-FILE01"})},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Binding.Adapter != "fixture_write" || plan.Targets[0].ResolvedIdentifier != "WIN-FILE01" {
		t.Errorf("plan = %+v; want fixture_write deriving WIN-FILE01", plan)
	}
}

// TestPlan_NothingRendersIsRefused: when no binding's rule renders, the
// request is refused, and the refusal names the missing field.
func TestPlan_NothingRendersIsRefused(t *testing.T) {
	r := planResolver(map[string][]ActionBinding{"host.isolate": {isolateBy("edr", "${target.entity.vendor_ids.falcon}", 100)}})
	_, err := r.Plan(PlanInput{
		ActionType: "host.isolate",
		Targets:    []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}},
		Entities:   []TargetEntity{hostEntity(map[string]any{"hostname": "WIN-FILE01"})},
	})
	if !IsPlanRefusal(err) || !strings.Contains(err.Error(), "vendor_ids.falcon") {
		t.Fatalf("err = %v; want a refusal naming vendor_ids.falcon", err)
	}
}

// TestPlan_WeakHostWithoutTheField: a weak host has no hostname, so a rule
// that names one does not render for it (adr/0406: no alias following for
// base fields).
func TestPlan_WeakHostWithoutTheField(t *testing.T) {
	r := planResolver(map[string][]ActionBinding{"host.isolate": {isolateBy("fixture_write", "${target.entity.hostname}", 100)}})
	_, err := r.Plan(PlanInput{
		ActionType: "host.isolate",
		Targets:    []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}},
		Entities:   []TargetEntity{hostEntity(map[string]any{"mac": "00:11:22:33:44:55", "weak": true})},
	})
	if !IsPlanRefusal(err) || !strings.Contains(err.Error(), "hostname") {
		t.Fatalf("err = %v; want a refusal naming hostname", err)
	}
}

// TestPlan_Indicators: an indicator binding derives an address's value and a
// file's SHA-256, through the template engine's ordinary path lookup.
func TestPlan_Indicators(t *testing.T) {
	r := planResolver(map[string][]ActionBinding{"ioc.block": {{
		ActionType: "ioc.block", Adapter: "edr", Operation: "add_ioc", Priority: 100,
		Identifier: map[string]string{
			"ipv4-addr": "${target.entity.value}",
			"file":      "${target.entity.hashes.SHA-256}",
		},
		Params: map[string]any{"value": "${target.resolved_identifier}"},
	}}})
	const sha = "6ca13d52ca70c883e0f0bb101e425a89e8624de51db2d2392593af6a84118090"
	plan, err := r.Plan(PlanInput{
		ActionType: "ioc.block",
		Targets:    []aggregate.TargetSpec{{EntityRef: "ipv4-addr--1"}, {EntityRef: "file--1"}},
		Entities: []TargetEntity{
			{Properties: map[string]any{"value": "185.220.101.5"}},
			{Properties: map[string]any{"name": "evil.dll", "hashes": map[string]any{"SHA-256": sha, "MD5": "x"}}},
		},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Targets[0].ResolvedIdentifier != "185.220.101.5" || plan.Targets[1].ResolvedIdentifier != sha {
		t.Errorf("identifiers = %q, %q; want the address and the file's SHA-256", plan.Targets[0].ResolvedIdentifier, plan.Targets[1].ResolvedIdentifier)
	}

	// A type the binding has no rule for is refused: every target needs a
	// rule under the same binding.
	_, err = r.Plan(PlanInput{
		ActionType: "ioc.block",
		Targets:    []aggregate.TargetSpec{{EntityRef: "ipv4-addr--1"}, {EntityRef: "url--1"}},
		Entities:   []TargetEntity{{Properties: map[string]any{"value": "185.220.101.5"}}, {Properties: map[string]any{"value": "http://x"}}},
	})
	if !IsPlanRefusal(err) || !strings.Contains(err.Error(), "no identifier rule for url") {
		t.Errorf("err = %v; want a refusal naming the url target's missing rule", err)
	}
}

// TestPlan_FalconDeviceID: a containment binding reads the Falcon device id
// under vendor_ids, whichever Falcon tool reported it; a host no Falcon
// source reported is refused; two disagreeing Falcon ids refuse with the
// conflict named.
func TestPlan_FalconDeviceID(t *testing.T) {
	r := planResolver(map[string][]ActionBinding{"host.isolate": {isolateBy("crowdstrike-response", "${target.entity.vendor_ids.falcon}", 100)}})
	plan := func(ent TargetEntity) (PlanResult, error) {
		return r.Plan(PlanInput{
			ActionType: "host.isolate",
			Targets:    []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}},
			Entities:   []TargetEntity{ent},
		})
	}

	got, err := plan(hostEntity(map[string]any{"hostname": "WIN-FILE01"},
		SourceID{Label: "fixture:lateral-movement-via-rdp", ID: "edr-01"},
		SourceID{Label: "falcon:search_hosts", ID: "aid-1"},
		SourceID{Label: "falcon:get_host_details", ID: "aid-1"}))
	if err != nil || got.Targets[0].ResolvedIdentifier != "aid-1" {
		t.Errorf("plan = %+v (err %v); want the Falcon device id aid-1", got, err)
	}

	if _, err := plan(hostEntity(map[string]any{"hostname": "WIN-FILE01"}, SourceID{Label: "fixture:s", ID: "edr-01"})); !IsPlanRefusal(err) {
		t.Errorf("a host no Falcon source reported: err = %v; want a refusal", err)
	}

	_, err = plan(hostEntity(map[string]any{"hostname": "WIN-FILE01"},
		SourceID{Label: "falcon:search_hosts", ID: "aid-old"},
		SourceID{Label: "falcon:search_hosts", ID: "aid-new"}))
	if !IsPlanRefusal(err) || !strings.Contains(err.Error(), "aid-new") || !strings.Contains(err.Error(), "aid-old") {
		t.Errorf("two Falcon ids: err = %v; want a refusal naming both", err)
	}
}

// TestPlan_ParamsMustRender: a binding whose params do not render gives way,
// and the refusal names what they need.
func TestPlan_ParamsMustRender(t *testing.T) {
	b := isolateBy("edr", "${target.entity.hostname}", 100)
	b.Params = map[string]any{"device_id": "${target.resolved_identifier}", "note": "${parameters.note}"}
	r := planResolver(map[string][]ActionBinding{"host.isolate": {b}})
	_, err := r.Plan(PlanInput{
		ActionType: "host.isolate",
		Targets:    []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}},
		Entities:   []TargetEntity{hostEntity(map[string]any{"hostname": "WIN-FILE01"})},
	})
	if !IsPlanRefusal(err) || !strings.Contains(err.Error(), "parameters.note") {
		t.Errorf("err = %v; want a refusal naming parameters.note", err)
	}
}

// TestVendor: the vendor is the label up to the first colon.
func TestVendor(t *testing.T) {
	for label, want := range map[string]string{
		"falcon:search_hosts": "falcon",
		"servicenow":          "servicenow",
		"Okta:get_user":       "okta",
		"aws-security-lake":   "aws-security-lake",
	} {
		if got := Vendor(label); got != want {
			t.Errorf("Vendor(%q) = %q; want %q", label, got, want)
		}
	}
}

// TestValidateActionBindings_IdentifierRules (adr/0406): config load refuses
// a typed binding with no identifier, a key outside the type's target types,
// an identifier on a destination type's binding, target.entity in params,
// and an identifier rule reading anything but the entity.
func TestValidateActionBindings_IdentifierRules(t *testing.T) {
	cat := defaultActionCatalog(t)
	ok := map[string][]ActionBinding{
		"host.isolate": {isolateBy("edr", "${target.entity.hostname | lower}", 100)},
		"ticket.create": {{ActionType: "ticket.create", Adapter: "sor", Operation: "op",
			Params: map[string]any{"queue": "${target.resolved_identifier}"}}},
	}
	if err := ValidateActionBindings(ok, cat); err != nil {
		t.Fatalf("valid bindings refused: %v", err)
	}
	for name, c := range map[string]struct {
		bindings map[string][]ActionBinding
		want     string
	}{
		"typed with no identifier": {map[string][]ActionBinding{"host.isolate": {{ActionType: "host.isolate", Adapter: "edr", Operation: "op"}}}, "identifier required"},
		"key outside target types": {map[string][]ActionBinding{"host.isolate": {{ActionType: "host.isolate", Adapter: "edr", Operation: "op",
			Identifier: map[string]string{"reckon-host": "${target.entity.hostname}", "ipv4-addr": "${target.entity.value}"}}}}, "ipv4-addr"},
		"identifier on a ticket": {map[string][]ActionBinding{"ticket.create": {{ActionType: "ticket.create", Adapter: "sor", Operation: "op",
			Identifier: map[string]string{"reckon-host": "${target.entity.hostname}"}}}}, "takes no identifier"},
		"target.entity in params": {map[string][]ActionBinding{"host.isolate": {{ActionType: "host.isolate", Adapter: "edr", Operation: "op",
			Identifier: map[string]string{"reckon-host": "${target.entity.hostname}"},
			Params:     map[string]any{"host": "${target.entity.hostname}"}}}}, "valid only inside identifier"},
		"identifier reads another root": {map[string][]ActionBinding{"host.isolate": {{ActionType: "host.isolate", Adapter: "edr", Operation: "op",
			Identifier: map[string]string{"reckon-host": "${parameters.host}"}}}}, "reads only target.entity"},
		// An identifier rule renders every field it names, or the binding
		// gives way: no default, no optional marker, no transform that does
		// not give text.
		"default": {map[string][]ActionBinding{"host.isolate": {isolateBy("edr", "${target.entity.hostname ?? unknown}", 100)}}, "gives target.entity.hostname a default"},
		"optional joined field": {map[string][]ActionBinding{"account.disable": {{ActionType: "account.disable", Adapter: "idp", Operation: "op",
			Identifier: map[string]string{"user-account": `${target.entity.domain?}\${target.entity.account_login}`}}}}, "marks target.entity.domain optional"},
		"lone optional field":     {map[string][]ActionBinding{"host.isolate": {isolateBy("edr", "${target.entity.hostname?}", 100)}}, "marks target.entity.hostname optional"},
		"literal with no field":   {map[string][]ActionBinding{"host.isolate": {isolateBy("edr", "WIN-FILE01", 100)}}, "names no field"},
		"reference typo":          {map[string][]ActionBinding{"host.isolate": {isolateBy("edr", "{target.entity.hostname}", 100)}}, "names no field"},
		"transform giving an int": {map[string][]ActionBinding{"host.isolate": {isolateBy("edr", "${target.entity.hostname | int}", 100)}}, `transform "int"`},
	} {
		err := ValidateActionBindings(c.bindings, cat)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v; want one naming %q", name, err, c.want)
		}
	}
}

// TestValidateActionBindings_BindingIdentity: a binding is found again by its
// action type, adapter and operation, so config load refuses two bindings
// that share them, naming both, and a binding with no adapter or operation.
func TestValidateActionBindings_BindingIdentity(t *testing.T) {
	cat := defaultActionCatalog(t)
	dup := map[string][]ActionBinding{"host.isolate": {
		isolateBy("edr", "${target.entity.hostname}", 200),
		isolateBy("edr", "${target.entity.hostname}", 100),
	}}
	err := ValidateActionBindings(dup, cat)
	if err == nil || !strings.Contains(err.Error(), "bindings 0 (priority 200) and 1 (priority 100)") || !strings.Contains(err.Error(), "adapter edr operation op") {
		t.Errorf("two bindings on one identity: err = %v; want one naming both", err)
	}

	noOp := isolateBy("edr", "${target.entity.hostname}", 100)
	noOp.Operation = ""
	noAdapter := isolateBy("", "${target.entity.hostname}", 100)
	for name, b := range map[string]ActionBinding{"empty operation": noOp, "empty adapter": noAdapter} {
		err := ValidateActionBindings(map[string][]ActionBinding{"host.isolate": {b}}, cat)
		if err == nil || !strings.Contains(err.Error(), "adapter and operation are required") {
			t.Errorf("%s: err = %v; want a config-load refusal", name, err)
		}
	}
}

// TestPlan_IdentifierText: a field the rule reads is used as text: a JSON
// number in decimal, never in exponent form; a bool is no identifier, and the
// refusal names the field.
func TestPlan_IdentifierText(t *testing.T) {
	r := planResolver(map[string][]ActionBinding{"host.isolate": {isolateBy("edr", "${target.entity.device_number}", 100)}})
	plan := func(v any) (PlanResult, error) {
		return r.Plan(PlanInput{
			ActionType: "host.isolate",
			Targets:    []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}},
			Entities:   []TargetEntity{{Properties: map[string]any{"device_number": v}}},
		})
	}
	for _, c := range []struct {
		v    any
		want string
	}{{float64(1234567), "1234567"}, {1.5, "1.5"}, {int64(42), "42"}} {
		got, err := plan(c.v)
		if err != nil || got.Targets[0].ResolvedIdentifier != c.want {
			t.Errorf("%v: identifier %+v (err %v); want %q", c.v, got.Targets, err, c.want)
		}
	}
	if _, err := plan(true); !IsPlanRefusal(err) || !strings.Contains(err.Error(), "device_number is a bool") {
		t.Errorf("bool property: err = %v; want a refusal naming device_number", err)
	}
	if _, err := plan(map[string]any{"a": "b"}); !IsPlanRefusal(err) || !strings.Contains(err.Error(), "device_number is a structure") {
		t.Errorf("structure property: err = %v; want a refusal naming device_number", err)
	}
}

// TestReadsVendorIDs: the loader reads a host's device ids only when a
// binding that could be chosen reads vendor_ids for a host.
func TestReadsVendorIDs(t *testing.T) {
	byHostname := planResolver(map[string][]ActionBinding{"host.isolate": {isolateBy("edr", "${target.entity.hostname}", 100)}})
	if byHostname.ReadsVendorIDs("host.isolate", "", "reckon-host") {
		t.Error("a hostname rule reads no vendor_ids")
	}
	byFalcon := planResolver(map[string][]ActionBinding{"host.isolate": {
		isolateBy("edr", "${target.entity.hostname}", 200),
		isolateBy("falcon", "${target.entity.vendor_ids.falcon}", 100),
	}})
	if !byFalcon.ReadsVendorIDs("host.isolate", "", "reckon-host") {
		t.Error("a candidate's vendor_ids rule must make the loader read device ids")
	}
}

// TestPlan_MultiFieldRule: a rule that joins several fields renders each as
// text, a number in decimal, and gives way when any one is missing.
func TestPlan_MultiFieldRule(t *testing.T) {
	r := planResolver(map[string][]ActionBinding{"account.disable": {{
		ActionType: "account.disable", Adapter: "idp", Operation: "op", Priority: 100,
		Identifier: map[string]string{"user-account": `${target.entity.domain}\${target.entity.account_login}`},
		Params:     map[string]any{"account": "${target.resolved_identifier}"},
	}}, "host.isolate": {isolateBy("edr", "${target.entity.site}-${target.entity.rack}", 100)}})
	plan := func(actionType, ref string, props map[string]any) (PlanResult, error) {
		return r.Plan(PlanInput{ActionType: actionType, Targets: []aggregate.TargetSpec{{EntityRef: ref}}, Entities: []TargetEntity{{Properties: props}}})
	}
	got, err := plan("account.disable", "user-account--1", map[string]any{"domain": "CONTOSO", "account_login": "jdoe"})
	if err != nil || got.Targets[0].ResolvedIdentifier != `CONTOSO\jdoe` {
		t.Errorf("joined rule: %+v (err %v); want CONTOSO\\jdoe", got.Targets, err)
	}
	if _, err := plan("account.disable", "user-account--1", map[string]any{"account_login": "jdoe"}); !IsPlanRefusal(err) || !strings.Contains(err.Error(), "the entity has no domain") {
		t.Errorf("joined rule missing a field: err = %v; want a refusal naming domain", err)
	}
	got, err = plan("host.isolate", "reckon-host--1", map[string]any{"site": "LON", "rack": float64(1234567)})
	if err != nil || got.Targets[0].ResolvedIdentifier != "LON-1234567" {
		t.Errorf("joined rule with a number: %+v (err %v); want LON-1234567", got.Targets, err)
	}
}
