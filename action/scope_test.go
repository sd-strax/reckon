package action

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
)

func scopedActionBinding(adapter, scope string) ActionBinding {
	return ActionBinding{Adapter: adapter, Operation: "op", Priority: 100, Scope: scope,
		Identifier: map[string]string{"user-account": "${target.entity.account_login}"}}
}

// jdoe is a stored account the scope tests plan against.
var jdoe = TargetEntity{Properties: map[string]any{"account_login": "jdoe"}}

// TestActionResolveRoutesByScope: the planner freezes the instance whose scope
// matches the investigation and never the other organization's, and dispatch
// goes there.
func TestActionResolveRoutesByScope(t *testing.T) {
	acme := &stubWriteAdapter{name: "acme", healthy: true, result: okResult()}
	meridian := &stubWriteAdapter{name: "meridian", healthy: true, result: okResult()}
	r := NewActionResolver(
		map[string][]ActionBinding{"account.disable": {
			scopedActionBinding("acme", "acme"),
			scopedActionBinding("meridian", "meridian"),
		}},
		map[string]WriteAdapter{"acme": acme, "meridian": meridian},
	)

	targets := []aggregate.TargetSpec{{EntityRef: "user-account--1"}}
	plan, err := r.Plan(PlanInput{ActionType: "account.disable", Targets: targets, Entities: []TargetEntity{jdoe}, SourceScope: "meridian"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	req := DispatchRequest{
		ActionID:    uuid.New(),
		ActionType:  "account.disable",
		Targets:     plan.Targets,
		SourceScope: "meridian",
		Binding:     plan.Binding,
	}
	_, binding, err := r.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if binding.Adapter != "meridian" {
		t.Errorf("dispatched to %q; want meridian", binding.Adapter)
	}
	if meridian.calls != 1 || acme.calls != 0 {
		t.Errorf("routing: meridian=%d acme=%d; want meridian once, acme never", meridian.calls, acme.calls)
	}
}

// TestActionResolveScopeMismatchNoDispatch: with no matching-scope instance the
// planner refuses the request, and a frozen binding of another scope is not
// used at dispatch: the no-fall-through write resolver reports ErrNoBinding
// and dispatches nothing, so the mis-routed-write failure mode (§3.5) can
// never occur.
func TestActionResolveScopeMismatchNoDispatch(t *testing.T) {
	acme := &stubWriteAdapter{name: "acme", healthy: true, result: okResult()}
	r := NewActionResolver(
		map[string][]ActionBinding{"account.disable": {scopedActionBinding("acme", "acme")}},
		map[string]WriteAdapter{"acme": acme},
	)
	for _, scope := range []string{"meridian", ""} {
		targets := []aggregate.TargetSpec{{EntityRef: "user-account--1", ResolvedIdentifier: "jdoe"}}
		if _, err := r.Plan(PlanInput{ActionType: "account.disable", Targets: targets, Entities: []TargetEntity{jdoe}, SourceScope: scope}); !IsPlanRefusal(err) {
			t.Errorf("scope %q: plan err = %v; want a refusal", scope, err)
		}
		req := DispatchRequest{
			ActionID:    uuid.New(),
			ActionType:  "account.disable",
			Targets:     targets,
			SourceScope: scope,
			Binding:     aggregate.FrozenBinding{Adapter: "acme", Operation: "op"},
		}
		if _, _, err := r.Resolve(context.Background(), req); !errors.Is(err, ErrNoBinding) {
			t.Errorf("scope %q: err = %v; want ErrNoBinding", scope, err)
		}
	}
	if acme.calls != 0 {
		t.Errorf("a mismatched-scope dispatch happened: calls=%d", acme.calls)
	}
}
