package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
)

// TestActorFromRequest: the actor carries what the token asserted (roles) and
// what the route required (the gate), both display only (adr/0105); kind and
// delegate still come from the claims alone; a request that passed no role
// gate records no gate; the claims' slice is copied, not aliased.
func TestActorFromRequest(t *testing.T) {
	claims := authz.Claims{Subject: "sub-1", PreferredUsername: "alice", Roles: []string{"analyst", "approver"}}
	gated := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	gated = gated.WithContext(withRequiredRoles(gated.Context(), authz.AnalystRoles))

	human := actorFromRequest(gated, claims)
	if human.Kind != aggregate.ActorHuman || human.PrincipalID != "sub-1" || human.PrincipalName != "alice" {
		t.Errorf("human = %+v; want HUMAN sub-1 alice", human)
	}
	if !slices.Equal(human.Roles, []string{"analyst", "approver"}) || !slices.Equal(human.RequiredRoles, []string{"analyst"}) {
		t.Errorf("human authorization = roles %v, required %v; want [analyst approver] and [analyst]", human.Roles, human.RequiredRoles)
	}
	claims.Roles[0] = "changed"
	if human.Roles[0] != "analyst" {
		t.Error("the actor's roles alias the claims' slice")
	}

	claims.Roles[0] = "analyst"
	claims.DelegateKind = "claude"
	delegate := actorFromRequest(gated, claims)
	if delegate.Kind != aggregate.ActorAIDelegated || delegate.Delegate == nil || delegate.Delegate.Vendor != "claude" {
		t.Errorf("delegate = %+v; want AI_DELEGATED by claude", delegate)
	}
	if !slices.Equal(delegate.Roles, []string{"analyst", "approver"}) || !slices.Equal(delegate.RequiredRoles, []string{"analyst"}) {
		t.Errorf("delegate authorization = roles %v, required %v; want the same two facts", delegate.Roles, delegate.RequiredRoles)
	}

	ungated := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	if a := actorFromRequest(ungated, claims); a.RequiredRoles != nil {
		t.Errorf("ungated request recorded a gate: %v", a.RequiredRoles)
	}
	if requiredRolesFrom(context.Background()) != nil {
		t.Error("a bare context reports a gate")
	}
}
