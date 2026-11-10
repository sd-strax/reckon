package server

import (
	"context"
	"net/http"
	"slices"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
)

// actorFromRequest derives the command actor from the request's JWT claims —
// Kind from the delegate_kind claim, NEVER a request body (04 §5.6): otherwise
// the AI write-protection at the aggregate boundary is caller-spoofable. Every
// handler that issues a command on a person's request uses this ONE
// derivation, so the aggregate's allowlist is a real second layer behind any
// endpoint-level delegate check. Two display-only facts ride along (adr/0105):
// the roles the token asserted, and the gate the route applied (the role set
// it required, recorded on the request by requireRoles). Authorization
// happened already, on the live token. The one actor built elsewhere is the
// policy's accountable human on an AUTO_POLICY approval, where no token was
// presented and no gate applied.
func actorFromRequest(r *http.Request, c authz.Claims) aggregate.Actor {
	a := aggregate.Actor{
		PrincipalID:   c.Subject,
		PrincipalName: c.PreferredUsername,
		Roles:         slices.Clone(c.Roles),
		RequiredRoles: requiredRolesFrom(r.Context()),
		Kind:          aggregate.ActorHuman,
	}
	if c.DelegateKind != "" {
		a.Kind = aggregate.ActorAIDelegated
		a.Delegate = &aggregate.AIDelegate{Vendor: c.DelegateKind}
	}
	return a
}

// humanClaims reads the caller's claims for a human act and refuses an AI
// delegate: the analyst route role admits a delegate token carrying the
// human's subject, so a handler whose act is a person's has to look. Missing
// claims are a wiring error (500); a delegate is 403.
func humanClaims(w http.ResponseWriter, r *http.Request) (authz.Claims, bool) {
	claims, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth context missing")
		return authz.Claims{}, false
	}
	if claims.DelegateKind != "" {
		writeJSONError(w, http.StatusForbidden, "an AI delegate cannot take this act: it is a person's")
		return authz.Claims{}, false
	}
	return claims, true
}

type requiredRolesKey struct{}

// withRequiredRoles records on the context the role set a route required of
// the request; requireRoles sets it as it admits the caller.
func withRequiredRoles(ctx context.Context, roles []string) context.Context {
	return context.WithValue(ctx, requiredRolesKey{}, slices.Clone(roles))
}

// requiredRolesFrom reads the role set the route required; nil when the
// request reached the handler through no role gate.
func requiredRolesFrom(ctx context.Context) []string {
	roles, _ := ctx.Value(requiredRolesKey{}).([]string)
	return roles
}
