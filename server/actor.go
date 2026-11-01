package server

import (
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
)

// actorFromClaims derives the command actor from the JWT claims — Kind from the
// delegate_kind claim, NEVER a request body (04 §5.6): otherwise the AI
// write-protection at the aggregate boundary is caller-spoofable. Every handler
// that issues commands uses this ONE derivation, so the aggregate's allowlist
// is a real second layer behind any endpoint-level delegate check.
func actorFromClaims(c authz.Claims) aggregate.Actor {
	if c.DelegateKind != "" {
		return aggregate.Actor{
			PrincipalID:   c.Subject,
			PrincipalName: c.PreferredUsername,
			Kind:          aggregate.ActorAIDelegated,
			Delegate:      &aggregate.AIDelegate{Vendor: c.DelegateKind},
		}
	}
	return aggregate.Actor{PrincipalID: c.Subject, PrincipalName: c.PreferredUsername, Kind: aggregate.ActorHuman}
}
