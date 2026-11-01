// Package action is the write-side of the capability layer plus the action
// authorization machinery (design/08-write-side-actions.md, design/04-action-
// authorization.md).
//
// The write surface, symmetric to capability's read surface: the
// ActionDescriptor catalog + list_action_types, the ActionBinding model and the
// no-fall-through action resolver, the WriteAdapter contract (Dispatch +
// idempotency key → WriteResult), the fixture write adapter, and request_action
// construction with the blast-radius tier escalator. It reuses capability's
// templating engine (capability.RenderParams) and Adapter primitives.
//
// Gate 2: the CEL auto-approval policy engine (Gate2) evaluates
// versioned policies over a flattened context (EvalInput → ctx.*) and resolves
// an authorization Decision by priority (DENY > REQUIRE_TWO_PARTY > AUTO_APPROVE
// > manual), with the non-deletable baseline DENY (AI-no-T3) always prepended.
// ApplyDecision threads the evaluation onto
// the RequestAction command so the PolicyEvaluated audit event (02 §3) is
// written in the same transaction as ActionRequested.
//
// The x-action lifecycle events themselves live in the aggregate (04 §3.1).
// The request_action HTTP endpoint applies Gate2, deriving Actor.Kind from the
// JWT delegate_kind claim; the ActionLifecycle and ReversalSaga workflows live
// in package temporal.
package action
