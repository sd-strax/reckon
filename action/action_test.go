package action

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
)

// --- stub write adapter ------------------------------------------------------

type stubWriteAdapter struct {
	name    string
	healthy bool
	result  WriteResult
	err     error
	calls   int
}

func (s *stubWriteAdapter) Name() string                   { return s.name }
func (s *stubWriteAdapter) Class() capability.AdapterClass { return capability.ClassCustom }
func (s *stubWriteAdapter) SupportedActionOps() []string   { return []string{"op"} }
func (s *stubWriteAdapter) Health() capability.HealthStatus {
	return capability.HealthStatus{Healthy: s.healthy}
}
func (s *stubWriteAdapter) Dispatch(_ context.Context, _ string, _ map[string]any, _ string) (WriteResult, error) {
	s.calls++
	return s.result, s.err
}

func okResult() WriteResult {
	return WriteResult{FinalOutcome: OutcomeSucceeded, PerTargetResults: map[string]PerTargetResult{"0": TargetOK}, AuditDepth: AuditFull}
}

func target(id string) aggregate.TargetSpec {
	return aggregate.TargetSpec{EntityRef: "reckon-host--" + id, ResolvedIdentifier: id}
}

// --- descriptor / list_action_types -----------------------------------------

func TestListActionTypesStatus(t *testing.T) {
	catalog := defaultActionCatalog(t)
	r := NewActionResolver(
		map[string][]ActionBinding{
			"host.isolate":    {{ActionType: "host.isolate", Adapter: "edr", Operation: "op", Priority: 100}},
			"account.disable": {{ActionType: "account.disable", Adapter: "down", Operation: "op", Priority: 100}},
		},
		map[string]WriteAdapter{
			"edr":  &stubWriteAdapter{name: "edr", healthy: true},
			"down": &stubWriteAdapter{name: "down", healthy: false},
		},
	)
	byType := map[string]ActionAvailability{}
	for _, s := range r.ListActionTypes(catalog) {
		byType[s.Descriptor.ActionType] = s.Status
	}
	if byType["host.isolate"] != ActionAvailable {
		t.Errorf("host.isolate = %q; want available", byType["host.isolate"])
	}
	if byType["account.disable"] != ActionDegraded {
		t.Errorf("account.disable = %q; want degraded", byType["account.disable"])
	}
	if byType["email.purge"] != ActionUnavailable {
		t.Errorf("email.purge = %q; want unavailable (no binding)", byType["email.purge"])
	}
}

// TestUsableBinding: a type whose bindings all name an adapter that is not
// configured or not enabled is unavailable in the listing and has no usable
// binding; one whose adapter is configured but unhealthy is degraded and
// usable; a binding of another scope is not usable for that request. The
// usable-binding check never asks an adapter for its health (adr/0380).
func TestUsableBinding(t *testing.T) {
	catalog := defaultActionCatalog(t)
	healthCalls := 0
	down := &countingHealthAdapter{stubWriteAdapter: stubWriteAdapter{name: "down"}, calls: &healthCalls}
	r := NewActionResolver(
		map[string][]ActionBinding{
			// "gone" is not among the enabled adapters: disabled or missing.
			"ticket.comment":  {{ActionType: "ticket.comment", Adapter: "gone", Operation: "op"}},
			"account.disable": {{ActionType: "account.disable", Adapter: "down", Operation: "op"}},
			"host.isolate":    {{ActionType: "host.isolate", Adapter: "down", Operation: "op", Scope: "org-a"}},
		},
		map[string]WriteAdapter{"down": down},
	)
	byType := map[string]ActionAvailability{}
	for _, s := range r.ListActionTypes(catalog) {
		byType[s.Descriptor.ActionType] = s.Status
	}
	if byType["ticket.comment"] != ActionUnavailable {
		t.Errorf("listing: ticket.comment = %q; want unavailable (its adapter is not enabled)", byType["ticket.comment"])
	}
	if byType["account.disable"] != ActionDegraded {
		t.Errorf("listing: account.disable = %q; want degraded", byType["account.disable"])
	}

	healthCalls = 0
	for _, c := range []struct {
		actionType, scope string
		want              bool
	}{
		{"ticket.comment", "", false},
		{"account.disable", "", true},
		{"host.isolate", "org-a", true},
		{"host.isolate", "org-b", false},
		{"email.purge", "", false},
	} {
		if got := r.HasUsableBinding(c.actionType, c.scope); got != c.want {
			t.Errorf("HasUsableBinding(%s, %q) = %v; want %v", c.actionType, c.scope, got, c.want)
		}
	}
	if healthCalls != 0 {
		t.Errorf("HasUsableBinding asked for health %d times; want none", healthCalls)
	}
}

// countingHealthAdapter counts its health checks.
type countingHealthAdapter struct {
	stubWriteAdapter
	calls *int
}

func (a *countingHealthAdapter) Health() capability.HealthStatus {
	*a.calls++
	return capability.HealthStatus{Healthy: false}
}

// TestPlan_DestinationType: for a type with no target types the planner
// keeps the requester's destination and freezes the highest-priority usable
// binding; a binding whose adapter is not configured gives way to the next;
// a type with no binding is refused (adr/0406).
func TestPlan_DestinationType(t *testing.T) {
	bindings := map[string][]ActionBinding{
		// The real SoR outranks the demo fixture (mirrors servicenow@200 vs
		// fixture_write@100).
		"ticket.create": {
			{ActionType: "ticket.create", Adapter: "servicenow", Operation: "create_incident", Priority: 200},
			{ActionType: "ticket.create", Adapter: "fixture_write", Operation: "dispatch", Priority: 100},
		},
	}
	r := NewActionResolver(bindings, map[string]WriteAdapter{
		"servicenow":    &stubWriteAdapter{name: "servicenow", healthy: true},
		"fixture_write": &stubWriteAdapter{name: "fixture_write", healthy: true},
	})
	desk := aggregate.TargetSpec{ResolvedIdentifier: "Service Desk"}
	plan, err := r.Plan(PlanInput{ActionType: "ticket.create", Targets: []aggregate.TargetSpec{desk}})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Binding != (aggregate.FrozenBinding{Adapter: "servicenow", Operation: "create_incident"}) {
		t.Errorf("frozen binding = %+v; want servicenow create_incident (higher priority)", plan.Binding)
	}
	if len(plan.Targets) != 1 || plan.Targets[0].ResolvedIdentifier != "Service Desk" {
		t.Errorf("targets = %+v; want the requester's destination kept", plan.Targets)
	}

	r2 := NewActionResolver(bindings, map[string]WriteAdapter{"fixture_write": &stubWriteAdapter{name: "fixture_write", healthy: true}})
	if plan, err := r2.Plan(PlanInput{ActionType: "ticket.create", Targets: []aggregate.TargetSpec{desk}}); err != nil || plan.Binding.Adapter != "fixture_write" {
		t.Errorf("planned with servicenow disabled = %+v (err %v); want fixture_write", plan.Binding, err)
	}

	if _, err := r.Plan(PlanInput{ActionType: "email.purge", Targets: []aggregate.TargetSpec{desk}}); !IsPlanRefusal(err) {
		t.Errorf("email.purge has no binding: err = %v; want a refusal", err)
	}
}

// --- request construction + blast-radius escalator ---------------------------

// TestBuildRequestCommand_RefusesAnEvent: evidence names STIX objects. A bare
// event id in evidence_refs is refused with an EventCitedError, and a ref in
// neither form is refused too (adr/0364).
func TestBuildRequestCommand_RefusesAnEvent(t *testing.T) {
	catalog := defaultActionCatalog(t)
	now := time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC)
	event := uuid.NewString()
	req := ActionRequest{
		ActionType:   "host.isolate",
		Targets:      []aggregate.TargetSpec{target("WIN-A")},
		EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000001", event},
		Rationale:    "contain",
	}
	_, err := BuildRequestCommand(catalog, req, now)
	var cited *aggregate.EventCitedError
	if !errors.As(err, &cited) || cited.Field != "evidence_refs" || len(cited.Refs) != 1 || cited.Refs[0] != event {
		t.Fatalf("an event in evidence_refs: err = %v; want an EventCitedError naming evidence_refs and %s", err, event)
	}
	req.EvidenceRefs = []string{"WS1"}
	if _, err := BuildRequestCommand(catalog, req, now); err == nil || errors.As(err, &cited) {
		t.Fatalf("evidence_refs %v: err = %v; want it refused as not a STIX id", req.EvidenceRefs, err)
	}
}

func TestBuildRequestCommand(t *testing.T) {
	catalog := defaultActionCatalog(t)
	now := time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC)

	cmd, err := BuildRequestCommand(catalog, ActionRequest{
		ActionType:   "host.isolate",
		Targets:      []aggregate.TargetSpec{target("WIN-A")},
		EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000001"},
		Rationale:    "contain",
	}, now)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if cmd.Tier != aggregate.TierT2 {
		t.Errorf("tier = %q; want T2", cmd.Tier)
	}
	if cmd.ActionID == (uuid.UUID{}) {
		t.Error("ActionID not minted")
	}
	if !cmd.ExpiresAt.Equal(now.Add(DefaultRequestTTL)) {
		t.Errorf("expires_at = %v; want now+TTL", cmd.ExpiresAt)
	}
	// 04 §7.1: the descriptor's reversibility classification is frozen onto the
	// request, so the REVERSED-claim gate reads the value the analyst approved
	// under — a later catalog edit cannot re-classify an in-flight action.
	if cmd.Reversibility != ReversibilityReversible {
		t.Errorf("reversibility = %q; want frozen %q", cmd.Reversibility, ReversibilityReversible)
	}
}

// TestBuildRequestCommand_TypedTargetNamesTheEntity (adr/0406): a typed
// action's target needs only its entity_ref, and a resolved_identifier the
// requester sent is dropped, so only the planner's derived value can be
// recorded.
func TestBuildRequestCommand_TypedTargetNamesTheEntity(t *testing.T) {
	catalog := defaultActionCatalog(t)
	cmd, err := BuildRequestCommand(catalog, ActionRequest{
		ActionType:   "host.isolate",
		Targets:      []aggregate.TargetSpec{{EntityRef: "reckon-host--1", ResolvedIdentifier: "SOMEWHERE-ELSE"}},
		EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000001"},
		Rationale:    "contain",
	}, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if cmd.Targets[0].EntityRef != "reckon-host--1" || cmd.Targets[0].ResolvedIdentifier != "" {
		t.Errorf("targets = %+v; want the entity with no requester identifier", cmd.Targets)
	}
}

func TestBlastRadiusEscalator(t *testing.T) {
	catalog := defaultActionCatalog(t)
	now := time.Now()

	many := make([]aggregate.TargetSpec, DefaultBlastRadiusThreshold+1)
	for i := range many {
		many[i] = target("host-" + string(rune('a'+i)))
	}
	cmd, err := BuildRequestCommand(catalog, ActionRequest{
		ActionType:   "host.isolate", // T2 by default
		Targets:      many,
		EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000001"},
		Rationale:    "mass containment",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Tier != aggregate.TierT3 {
		t.Errorf("11 targets on a T2 action → tier %q; want T3 (blast-radius escalator)", cmd.Tier)
	}

	// An already-irreversible T3 action stays T3 regardless of count.
	if got := EscalateTier(aggregate.TierT3, 1, DefaultBlastRadiusThreshold); got != aggregate.TierT3 {
		t.Errorf("T3 single-target escalated to %q; want T3", got)
	}
	// A T2 under the threshold stays T2.
	if got := EscalateTier(aggregate.TierT2, DefaultBlastRadiusThreshold, DefaultBlastRadiusThreshold); got != aggregate.TierT2 {
		t.Errorf("T2 at threshold escalated to %q; want T2", got)
	}

	// The escalator counts DISTINCT entities (04 §1), not raw list length: the
	// same entity duplicated 11 times is one target, not eleven.
	dupes := make([]aggregate.TargetSpec, DefaultBlastRadiusThreshold+1)
	for i := range dupes {
		dupes[i] = target("same-host")
	}
	cmd, err = BuildRequestCommand(catalog, ActionRequest{
		ActionType: "host.isolate", Targets: dupes, EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000001"}, Rationale: "dup",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Tier != aggregate.TierT2 {
		t.Errorf("11 duplicates of one entity escalated to %q; want T2 (1 distinct)", cmd.Tier)
	}
}

func TestBuildRequestCommandRejections(t *testing.T) {
	catalog := defaultActionCatalog(t)
	now := time.Now()
	cases := map[string]ActionRequest{
		"unknown type": {ActionType: "nope", Targets: []aggregate.TargetSpec{target("x")}, Rationale: "r"},
		"no targets":   {ActionType: "host.isolate", Rationale: "r"},
		"no rationale": {ActionType: "host.isolate", Targets: []aggregate.TargetSpec{target("x")}},
		// A destination type needs the requester's identifier; a typed
		// one does not (adr/0406).
		"unresolved destination": {ActionType: "ticket.close", Targets: []aggregate.TargetSpec{{EntityRef: "x"}},
			EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000001"}, Rationale: "r"},
		// Grounding is enforced (09 §3): a forward action with no evidence_refs is
		// rejected — the ungrounded containment a live opus run produced 1-in-3.
		"no evidence_refs": {ActionType: "host.isolate", Targets: []aggregate.TargetSpec{target("x")}, Rationale: "r"},
		// Parameter-schema validation (08 §3: Inputs is the schema for
		// request_action.parameters). Both found by the eval harness: the model
		// invented {"title","body"} for ticket.create instead of the declared
		// {summary, description, ...} — undetected until a real binding would
		// template ${parameters.summary} post-approval.
		"undeclared parameter key": {ActionType: "ticket.create",
			Targets:    []aggregate.TargetSpec{target("IT-OPS")},
			Parameters: []byte(`{"title":"t","body":"b"}`), Rationale: "r"},
		"missing required parameter": {ActionType: "ticket.create",
			Targets:    []aggregate.TargetSpec{target("IT-OPS")},
			Parameters: []byte(`{"description":"d"}`), Rationale: "r"},
		"required parameter empty": {ActionType: "ticket.create",
			Targets:    []aggregate.TargetSpec{target("IT-OPS")},
			Parameters: []byte(`{"summary":""}`), Rationale: "r"},
		"parameters not an object": {ActionType: "ticket.create",
			Targets:    []aggregate.TargetSpec{target("IT-OPS")},
			Parameters: []byte(`["summary"]`), Rationale: "r"},
	}
	for name, req := range cases {
		if _, err := BuildRequestCommand(catalog, req, now); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}

	// The rejection message names the declared keys so a model caller can
	// self-correct in one round.
	_, err := BuildRequestCommand(catalog, ActionRequest{ActionType: "ticket.create",
		Targets:    []aggregate.TargetSpec{target("IT-OPS")},
		Parameters: []byte(`{"title":"t"}`), Rationale: "r"}, now)
	if err == nil || !strings.Contains(err.Error(), "summary") {
		t.Errorf("unknown-key error should name the declared inputs; got %v", err)
	}

	// Entity-typed inputs ride targets, not parameters: an action whose only
	// declared input is an entity accepts empty parameters, and an action with
	// optional string inputs accepts their absence.
	for _, ok := range []ActionRequest{
		{ActionType: "host.isolate", Targets: []aggregate.TargetSpec{target("WIN-A")}, EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000001"}, Rationale: "r"},
		{ActionType: "ticket.close", Targets: []aggregate.TargetSpec{target("IT-1")}, EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000001"}, Rationale: "r"}, // resolution is optional
		{ActionType: "ticket.create", Targets: []aggregate.TargetSpec{target("IT-OPS")},
			EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000001"}, Parameters: []byte(`{"summary":"handoff"}`), Rationale: "r"},
	} {
		if _, err := BuildRequestCommand(catalog, ok, now); err != nil {
			t.Errorf("%s: unexpected rejection: %v", ok.ActionType, err)
		}
	}

	// A reversal (reversal_of_ref set) is EXEMPT from the evidence requirement:
	// its grounding is the original action, the same carve-out G1 makes
	// (design/10). Retries are not exempt — they restate their grounding.
	reversal := ActionRequest{ActionType: "host.isolate", Targets: []aggregate.TargetSpec{target("WIN-A")},
		Rationale: "revert the isolation", ReversalOfRef: uuid.New()}
	if _, err := BuildRequestCommand(catalog, reversal, now); err != nil {
		t.Errorf("reversal with no evidence_refs should be exempt (grounded by reversal_of_ref); got %v", err)
	}
}

// TestMaxTier: reversal tier parity (04 §7) — never lower than the original.
func TestMaxTier(t *testing.T) {
	if MaxTier(aggregate.TierT2, aggregate.TierT3) != aggregate.TierT3 {
		t.Error("T2 inverse of a T3 original must be T3")
	}
	if MaxTier(aggregate.TierT3, aggregate.TierT2) != aggregate.TierT3 {
		t.Error("T3 inverse of a T2 original stays T3")
	}
	if MaxTier(aggregate.TierT2, aggregate.TierT2) != aggregate.TierT2 {
		t.Error("equal tiers stay equal")
	}
}

// --- resolver ----------------------------------------------------------------

func TestActionResolverDispatches(t *testing.T) {
	edr := &stubWriteAdapter{name: "edr", healthy: true, result: okResult()}
	r := NewActionResolver(
		map[string][]ActionBinding{"host.isolate": {{
			ActionType: "host.isolate", Adapter: "edr", Operation: "op", Priority: 100,
			Params: map[string]any{"device_id": "${target.resolved_identifier}"},
		}}},
		map[string]WriteAdapter{"edr": edr},
	)
	res, binding, err := r.Resolve(context.Background(), DispatchRequest{
		ActionID: uuid.New(), ActionType: "host.isolate", Targets: []aggregate.TargetSpec{target("WIN-A14")},
		Binding: aggregate.FrozenBinding{Adapter: "edr", Operation: "op"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.FinalOutcome != OutcomeSucceeded || binding.Adapter != "edr" {
		t.Errorf("unexpected result %+v / binding %+v", res, binding)
	}
}

// TestActionResolverNoFallThrough is the write-side invariant: once a binding is
// chosen, a failed dispatch is NOT retried against a lower-priority binding — a
// partially-executed state change must not be re-attempted against another tool.
func TestActionResolverNoFallThrough(t *testing.T) {
	primary := &stubWriteAdapter{name: "primary", healthy: true, err: writeErrorf(WriteFatal, "boom")}
	backup := &stubWriteAdapter{name: "backup", healthy: true, result: okResult()}
	r := NewActionResolver(
		map[string][]ActionBinding{"host.isolate": {
			{ActionType: "host.isolate", Adapter: "primary", Operation: "op", Priority: 100},
			{ActionType: "host.isolate", Adapter: "backup", Operation: "op", Priority: 50},
		}},
		map[string]WriteAdapter{"primary": primary, "backup": backup},
	)
	_, _, err := r.Resolve(context.Background(), DispatchRequest{
		ActionID: uuid.New(), ActionType: "host.isolate", Targets: []aggregate.TargetSpec{target("WIN-A")},
		Binding: aggregate.FrozenBinding{Adapter: "primary", Operation: "op"},
	})
	if err == nil {
		t.Fatal("expected the primary dispatch error to propagate")
	}
	if primary.calls != 1 {
		t.Errorf("primary calls = %d; want 1", primary.calls)
	}
	if backup.calls != 0 {
		t.Errorf("backup calls = %d; want 0 (no fall-through after a chosen binding fails)", backup.calls)
	}
}

// TestResolve_UsesTheFrozenBinding (adr/0406): dispatch uses the binding
// frozen at request time, even when a higher-priority one was added since,
// and fails with nothing sent when the frozen one was removed or its adapter
// is no longer enabled.
func TestResolve_UsesTheFrozenBinding(t *testing.T) {
	frozen := &stubWriteAdapter{name: "frozen", healthy: true, result: okResult()}
	newer := &stubWriteAdapter{name: "newer", healthy: true, result: okResult()}
	req := DispatchRequest{
		ActionID: uuid.New(), ActionType: "host.isolate", Targets: []aggregate.TargetSpec{target("WIN-A")},
		Binding: aggregate.FrozenBinding{Adapter: "frozen", Operation: "op"},
	}
	r := NewActionResolver(
		map[string][]ActionBinding{"host.isolate": {
			{ActionType: "host.isolate", Adapter: "newer", Operation: "op", Priority: 500},
			{ActionType: "host.isolate", Adapter: "frozen", Operation: "op", Priority: 100},
		}},
		map[string]WriteAdapter{"frozen": frozen, "newer": newer},
	)
	_, binding, err := r.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if binding.Adapter != "frozen" || frozen.calls != 1 || newer.calls != 0 {
		t.Errorf("dispatched via %s (frozen=%d newer=%d); want the frozen binding only", binding.Adapter, frozen.calls, newer.calls)
	}

	for name, r := range map[string]*ActionResolver{
		"binding removed": NewActionResolver(
			map[string][]ActionBinding{"host.isolate": {{ActionType: "host.isolate", Adapter: "newer", Operation: "op", Priority: 500}}},
			map[string]WriteAdapter{"frozen": frozen, "newer": newer}),
		"adapter disabled": NewActionResolver(
			map[string][]ActionBinding{"host.isolate": {
				{ActionType: "host.isolate", Adapter: "newer", Operation: "op", Priority: 500},
				{ActionType: "host.isolate", Adapter: "frozen", Operation: "op", Priority: 100},
			}},
			map[string]WriteAdapter{"newer": newer}),
	} {
		frozen.calls, newer.calls = 0, 0
		if _, _, err := r.Resolve(context.Background(), req); !errors.Is(err, ErrNoBinding) {
			t.Errorf("%s: err = %v; want ErrNoBinding", name, err)
		}
		if frozen.calls != 0 || newer.calls != 0 {
			t.Errorf("%s: something was sent (frozen=%d newer=%d); want nothing", name, frozen.calls, newer.calls)
		}
	}
}

func TestActionResolverNoBinding(t *testing.T) {
	r := NewActionResolver(map[string][]ActionBinding{}, map[string]WriteAdapter{})
	_, _, err := r.Resolve(context.Background(), DispatchRequest{ActionID: uuid.New(), ActionType: "host.isolate", Targets: []aggregate.TargetSpec{target("x")}})
	if !errors.Is(err, ErrNoBinding) {
		t.Errorf("err = %v; want ErrNoBinding", err)
	}
}

// --- fixture write adapter ---------------------------------------------------

func writeFixtureAdapter(t *testing.T, files map[string]string) *FixtureWriteAdapter {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := NewFixtureWriteAdapter(root, "s")
	a.SetApplyDelays(false)
	return a
}

func TestFixtureWriteAdapterMatch(t *testing.T) {
	a := writeFixtureAdapter(t, map[string]string{
		"isolate.action.json": `{
          "fixture_meta":{"scenario":"s","matches":{"action_type":"host.isolate","params":{"resolved_identifier":"WIN-A14"}},"delay_ms":0},
          "result":{"final_outcome":"SUCCEEDED","per_target_results":{"0":"OK"},"audit_depth":"FULL"}
        }`,
		"purge.action.json": `{
          "fixture_meta":{"scenario":"s","matches":{"action_type":"email.purge","params":{}},"delay_ms":0},
          "result":{"final_outcome":"FAILED","per_target_results":{"0":"FAIL"},"error_class":"FATAL_ERROR"}
        }`,
	})

	res, err := a.Dispatch(context.Background(), "dispatch", map[string]any{
		ParamActionType: "host.isolate", ParamResolvedIdentifier: "WIN-A14",
	}, "key-1")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.FinalOutcome != OutcomeSucceeded || res.AdapterRequestID == "" {
		t.Errorf("unexpected result: %+v", res)
	}

	// A FAILED fixture drives the failure path.
	res, err = a.Dispatch(context.Background(), "dispatch", map[string]any{ParamActionType: "email.purge"}, "key-2")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.FinalOutcome != OutcomeFailed || res.ErrorClass != WriteFatal {
		t.Errorf("failed fixture not surfaced: %+v", res)
	}
}

func TestFixtureWriteAdapterNoMatchIsFatal(t *testing.T) {
	a := writeFixtureAdapter(t, map[string]string{
		"only.action.json": `{"fixture_meta":{"scenario":"s","matches":{"action_type":"host.isolate","params":{}}},"result":{"final_outcome":"SUCCEEDED","audit_depth":"FULL"}}`,
	})
	_, err := a.Dispatch(context.Background(), "dispatch", map[string]any{ParamActionType: "account.disable"}, "k")
	var we *WriteError
	if !errors.As(err, &we) || we.Class != WriteFatal {
		t.Errorf("no-match should be FATAL_ERROR; got %v", err)
	}
}

// --- idempotency -------------------------------------------------------------

func TestIdempotencyKeyDeterministic(t *testing.T) {
	id := uuid.New()
	k1, k2 := IdempotencyKey(id, "WIN-A"), IdempotencyKey(id, "WIN-A")
	if k1 != k2 {
		t.Error("idempotency key not deterministic")
	}
	if IdempotencyKey(id, "WIN-A") == IdempotencyKey(id, "WIN-B") {
		t.Error("different targets should yield different keys")
	}
	if IdempotencyKey(id, "WIN-A") == IdempotencyKey(uuid.New(), "WIN-A") {
		t.Error("different actions should yield different keys")
	}
}

// TestResolve_NotSent (adr/0384): every way the frozen binding can fail before
// the outbound call is a NotSentError whose reason names what broke, and no
// adapter is called. An error the adapter itself returns is never one: the
// call was made.
func TestResolve_NotSent(t *testing.T) {
	frozen := aggregate.FrozenBinding{Adapter: "edr", Operation: "contain"}
	bound := ActionBinding{ActionType: "host.isolate", Adapter: "edr", Operation: "contain", Priority: 100,
		Params: map[string]any{"device_id": "${target.resolved_identifier}"}}
	for _, c := range []struct {
		name     string
		bindings []ActionBinding
		enabled  bool
		scope    string
		binding  aggregate.FrozenBinding
		want     string
	}{
		{"no frozen binding", []ActionBinding{bound}, true, "", aggregate.FrozenBinding{}, "action host.isolate has no binding frozen at request"},
		{"binding gone", nil, true, "", frozen, "operation contain on adapter edr is no longer bound to host.isolate"},
		{"operation gone", []ActionBinding{{ActionType: "host.isolate", Adapter: "edr", Operation: "other", Priority: 100}}, true, "", frozen,
			"operation contain on adapter edr is no longer bound to host.isolate"},
		{"adapter not enabled", []ActionBinding{bound}, false, "", frozen, "adapter edr is not configured or not enabled"},
		{"scope mismatch", []ActionBinding{func() ActionBinding { b := bound; b.Scope = "org-a"; return b }()}, true, "org-b", frozen,
			`the binding on adapter edr is scoped to "org-a", and this investigation's scope is "org-b"`},
		{"params no longer render", []ActionBinding{func() ActionBinding {
			b := bound
			b.Params = map[string]any{"device_id": "${target.resolved_identifier}", "note": "${parameters.note}"}
			return b
		}()}, true, "", frozen, "operation contain on adapter edr no longer renders: missing parameters.note"},
	} {
		edr := &stubWriteAdapter{name: "edr", healthy: true, result: okResult()}
		adapters := map[string]WriteAdapter{}
		if c.enabled {
			adapters["edr"] = edr
		}
		r := NewActionResolver(map[string][]ActionBinding{"host.isolate": c.bindings}, adapters)
		_, _, err := r.Resolve(context.Background(), DispatchRequest{
			ActionID: uuid.New(), ActionType: "host.isolate", Targets: []aggregate.TargetSpec{target("WIN-A")},
			SourceScope: c.scope, Binding: c.binding,
		})
		var ns *NotSentError
		if !errors.As(err, &ns) || !errors.Is(err, ErrNotSent) {
			t.Errorf("%s: err = %v; want a NotSentError", c.name, err)
			continue
		}
		if ns.Reason != c.want {
			t.Errorf("%s: reason = %q; want %q", c.name, ns.Reason, c.want)
		}
		if edr.calls != 0 {
			t.Errorf("%s: the adapter was called %d times; want none", c.name, edr.calls)
		}
	}

	// A template that fails to render (a transform refusing the value) is
	// not sent too, the one reason whose cause is not ErrNoBinding.
	tmplEdr := &stubWriteAdapter{name: "edr", healthy: true, result: okResult()}
	badTemplate := bound
	badTemplate.Params = map[string]any{"device_id": "${target.resolved_identifier | int}"}
	tr := NewActionResolver(map[string][]ActionBinding{"host.isolate": {badTemplate}}, map[string]WriteAdapter{"edr": tmplEdr})
	_, _, err := tr.Resolve(context.Background(), DispatchRequest{
		ActionID: uuid.New(), ActionType: "host.isolate", Targets: []aggregate.TargetSpec{target("WIN-A")}, Binding: frozen,
	})
	var ns *NotSentError
	if !errors.As(err, &ns) || !strings.HasPrefix(ns.Reason, "operation contain on adapter edr no longer renders: transform int") ||
		errors.Is(err, ErrNoBinding) || tmplEdr.calls != 0 {
		t.Errorf("template error: err = %v (calls %d); want a NotSentError naming the transform, not ErrNoBinding, nothing sent", err, tmplEdr.calls)
	}

	// The adapter's own fatal error is not "not sent": the call was made.
	edr := &stubWriteAdapter{name: "edr", healthy: true, err: writeErrorf(WriteFatal, "boom")}
	r := NewActionResolver(map[string][]ActionBinding{"host.isolate": {bound}}, map[string]WriteAdapter{"edr": edr})
	_, _, err = r.Resolve(context.Background(), DispatchRequest{
		ActionID: uuid.New(), ActionType: "host.isolate", Targets: []aggregate.TargetSpec{target("WIN-A")}, Binding: frozen,
	})
	if err == nil || errors.Is(err, ErrNotSent) || edr.calls != 1 {
		t.Errorf("adapter fatal error: err = %v (calls %d); want the adapter's error, not a NotSentError", err, edr.calls)
	}
}
