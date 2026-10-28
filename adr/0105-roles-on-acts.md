# 0105 — The roles a token carried ride the act for display; authorization still reads the live token and reckon mirrors no roles

Issue: #105 · PR: #106 · Status: accepted

## Context

The Person inspector (`ui/08 §8.6.1`) shows what a principal did in this investigation and
should say what authorized each act. The record keeps the principal id and the display name
the identity provider presented at the time (`adr/0077`) but not the roles the token carried,
so the inspector said roles live in the identity provider and showed none. Gap 19 of
`design/ui/00-backend-binding.md §6`.

Constraints: roles live in the IdP and travel in JWTs; reckon never caches or mirrors them, and
Gate 1 reads the live token on every request. The one place every handler derives the actor
(`actorFromClaims`) is where `principal_name` is captured.

## Decision

- **`Actor.Roles`** is captured at command time from the token's roles by `actorFromClaims`,
  beside `PrincipalName`. It is what authorized the act *as the record saw it*: display only,
  never read for authorization, never a mirror. SYSTEM actors and events recorded before the
  field existed carry none.
- **Projected, not re-derived.** `ThreadActorView.roles` and the approver on
  `GET /api/actions/{id}` (`ActionApproverView.roles`, from the approving act's actor) carry it.
  Nothing joins back to the IdP.
- **The Person inspector derives everything client-side from the snapshot**: the acts whose
  actor is the principal, the union of the roles those acts carried with the caption that
  current roles may differ, per-act "as <roles>", and how many acts predate the field. The
  issuer is the `/api/auth-config` issuer, carried with the signed-in principal message.

## Alternatives rejected

- **Looking roles up live** for the inspector: would show today's roles against yesterday's
  acts and make the record depend on the IdP being reachable.
- **A role on the approval only**: every act is authorized by a role, not only approvals; the
  thread and the owner-item, attestation and verdict acts all carry an actor.

## Consequences

- No migration: the actor is JSON in the event payload. Older events show no roles, and the
  inspector says so rather than inferring.
- The retry path now records the principal's name and roles like every other command.
