# 0227 — One route table, named role sets, one event-pair helper

Issue: #227 · Status: accepted

## Context

Most of the API was routed twice. `buildRouter` matched a prefix, then a handler
such as `investigationsItem` walked `strings.HasSuffix` blocks, checked the
method and checked the role by hand. Who may call what could only be learned by
reading every handler, and a handler that forgot its role check still served.
The read role set was written out at each check, with two named copies, and
fifteen commands built the same domain event and interpretation pair by hand.

## Decision

- **One route table** in `server/routes.go`. Each entry is a method and path
  pattern, the roles allowed and the handler. One loop wraps every entry in
  `RequireAuth` and the role check, and an entry with no roles panics at startup,
  so a route cannot be added without saying who may call it. `/healthz`,
  `/status`, `/metrics`, `/auth-config` and `/me` stay outside: they name no role.
- **Handlers read `r.PathValue`**, through one helper that answers 400 for an id
  that is not a UUID. They no longer check the method or the role. A check that
  depends on more than the route, such as refusing an AI-delegated token, stays
  in the handler, as does the 503 for a layer that is not configured.
- **`authz.ReadRoles` and `authz.AnalystRoles`** are the role sets.
- **`actEvents`** builds the pair: the domain event after a given sequence number
  and its interpretation next. A command appends what follows, such as a policy
  evaluation. It takes the sequence number rather than the state because an
  approval can follow the pair that activates a draft.
- **`TestRouteAuthorization`** lists every route independently of the router and
  sends it with no token and with each role. It landed before the table, passing
  against the hand-written routing, so the move is checked against what callers
  had.

## Consequences

The move changes a few answers, none on a path a client uses:

- A 404 or 405 from the router is plain text, not JSON, and comes before
  authentication.
- A trailing slash no longer matches.
- HEAD works on GET routes.
- `GET`, `PUT` and `DELETE` on `/sops/import` are a 400 (`import` is not an id),
  not a 405.
- A caller without the role gets a 403 from a route whose layer is not
  configured, where it got a 503.

## Alternatives considered

A router library: the standard `ServeMux` has method and wildcard patterns, so a
dependency buys nothing here. Keeping the role in each handler behind a helper:
it leaves the question of who may call what spread across the package.
