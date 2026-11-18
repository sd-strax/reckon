package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	sdktemporal "go.temporal.io/sdk/temporal"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/module"
	"github.com/sd-strax/reckon/temporal"
)

// fakeActionStarter records the workflows the backend starts.
type fakeActionStarter struct {
	mu        sync.Mutex
	lifecycle []string // action ids
	sagas     []string // reversing action ids
	// bindings is the frozen binding each started workflow carried, by
	// action id (adr/0406).
	bindings map[string]aggregate.FrozenBinding
}

func (f *fakeActionStarter) carried(actionID string, b aggregate.FrozenBinding) {
	if f.bindings == nil {
		f.bindings = map[string]aggregate.FrozenBinding{}
	}
	f.bindings[actionID] = b
}

func (f *fakeActionStarter) StartActionLifecycle(_ context.Context, in temporal.ActionLifecycleInput) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lifecycle = append(f.lifecycle, in.ActionID)
	f.carried(in.ActionID, in.Binding)
	return "action-lifecycle-" + in.ActionID, nil
}

func (f *fakeActionStarter) StartReversalSaga(_ context.Context, in temporal.ReversalSagaInput) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sagas = append(f.sagas, in.Reversing.ActionID)
	f.carried(in.Reversing.ActionID, in.Reversing.Binding)
	return "reversal-saga-" + in.Reversing.ActionID, nil
}

// testFrozenBinding is the binding approvedAction freezes, and the one
// testActionResolver's bindings are identified by.
var testFrozenBinding = aggregate.FrozenBinding{Adapter: "test_write", Operation: "op"}

// systemHandle runs a SYSTEM command, as the workflows do.
func systemHandle(t *testing.T, aggID uuid.UUID, cmd aggregate.Command) {
	t.Helper()
	env := newEnvelope(aggID, aggregate.Actor{PrincipalID: "test-subject", Kind: aggregate.ActorSystem}, commandNow())
	if _, err := testHandler.Handle(context.Background(), env, cmd); err != nil {
		t.Fatalf("%s: %v", cmd.Kind(), err)
	}
}

// humanHandle runs a command as the analyst.
func humanHandle(t *testing.T, aggID uuid.UUID, cmd aggregate.Command) {
	t.Helper()
	env := newEnvelope(aggID, aggregate.Actor{PrincipalID: "test-subject", Kind: aggregate.ActorHuman}, commandNow())
	if _, err := testHandler.Handle(context.Background(), env, cmd); err != nil {
		t.Fatalf("%s: %v", cmd.Kind(), err)
	}
}

// approvedAction records a requested and approved action without starting
// its workflow, the state a failed start leaves behind.
func approvedAction(t *testing.T, aggID uuid.UUID, req aggregate.RequestAction) uuid.UUID {
	t.Helper()
	if req.ActionID == uuid.Nil {
		req.ActionID = uuid.New()
	}
	if req.ActionType == "" {
		req.ActionType = "host.isolate"
	}
	req.Tier = aggregate.TierT2
	req.Targets = []aggregate.TargetSpec{{EntityRef: "reckon-host--1", ResolvedIdentifier: "WIN-A"}}
	req.ExpiresAt = time.Now().Add(time.Hour)
	req.Rationale = "contain"
	if req.Binding == (aggregate.FrozenBinding{}) {
		req.Binding = testFrozenBinding
	}
	humanHandle(t, aggID, req)
	humanHandle(t, aggID, aggregate.ApproveAction{ActionID: req.ActionID, Authorization: aggregate.Authorization{
		Mode: aggregate.AuthModeManual, Stage: aggregate.AuthStageSolo,
		PrimaryApproverRef: "test-subject", PrimaryApprovedAt: time.Now(),
	}})
	return req.ActionID
}

func dispatched(t *testing.T, aggID, id uuid.UUID) {
	t.Helper()
	systemHandle(t, aggID, aggregate.DispatchAction{ActionID: id, Adapter: "fx", AdapterRequestID: "action-lifecycle-" + id.String()})
}

// TestSweepStrandedActions: the sweep starts the workflow of every APPROVED
// and EXECUTING action, carrying the approver, and leaves one waiting for a
// person alone; without a dispatch client it does nothing (adr/0216).
func TestSweepStrandedActions(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t)

	waiting := approvedAction(t, invID, aggregate.RequestAction{})
	executing := approvedAction(t, invID, aggregate.RequestAction{})
	dispatched(t, invID, executing)
	unknown := approvedAction(t, invID, aggregate.RequestAction{})
	dispatched(t, invID, unknown)
	systemHandle(t, invID, aggregate.RecordOutcomeUnknown{ActionID: unknown, Detail: "the run stopped"})

	b.sweepStrandedActions(context.Background()) // no dispatch client: nothing to start with, no panic

	fake := &fakeActionStarter{}
	b.actionStarterOverride = fake
	b.sweepStrandedActions(context.Background())

	got := map[string]bool{}
	for _, id := range fake.lifecycle {
		got[id] = true
	}
	if !got[waiting.String()] || !got[executing.String()] || got[unknown.String()] || len(fake.lifecycle) != 2 {
		t.Errorf("started %v; want the APPROVED and the EXECUTING action", fake.lifecycle)
	}
	// The sweep reads the projection, and the workflow it starts carries the
	// binding frozen at request (adr/0406).
	for _, id := range []uuid.UUID{waiting, executing} {
		if fake.bindings[id.String()] != testFrozenBinding {
			t.Errorf("sweep started %s with binding %+v; want the frozen %+v", id, fake.bindings[id.String()], testFrozenBinding)
		}
	}
}

// TestApprovalPathsCarryTheFrozenBinding (adr/0406): every path that starts
// a workflow from an approved action, auto-approval and a person's approval,
// hands the workflow the binding frozen at request.
func TestApprovalPathsCarryTheFrozenBinding(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	request := func(b *Backend) string {
		t.Helper()
		resp, out := postAction(t, b, mintToken(t, nil), RequestActionBody{
			ActionType: "host.isolate", Targets: []aggregate.TargetSpec{{EntityRef: "reckon-host--1"}}, Rationale: "contain",
			EvidenceRefs: []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"}, InvestigationRef: invID.String(),
		})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("request: status %d", resp.StatusCode)
		}
		return out.ActionID
	}

	auto := actionBackend(t, action.Policy{
		ID: "policy/auto-isolate/1.0.0", ActionMatch: []string{"host.isolate"},
		Effect: action.EffectAutoApprove, Predicate: "true", SignedOffBy: []string{"secops-lead"},
	})
	autoFake := &fakeActionStarter{}
	auto.actionStarterOverride = autoFake
	autoID := request(auto)
	if got := autoFake.bindings[autoID]; got != testFrozenBinding {
		t.Errorf("auto-approval started the workflow with binding %+v; want %+v", got, testFrozenBinding)
	}

	manual := actionBackend(t)
	manualFake := &fakeActionStarter{}
	manual.actionStarterOverride = manualFake
	manualID := request(manual)
	if resp, _ := postActionDecision(t, manual, mintToken(t, nil), manualID, "approve", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("approve: status %d", resp.StatusCode)
	}
	if got := manualFake.bindings[manualID]; got != testFrozenBinding {
		t.Errorf("manual approval started the workflow with binding %+v; want %+v", got, testFrozenBinding)
	}
}

// TestResolveActionOutcome_Endpoint: a person resolves an unknown outcome; an
// AI delegate cannot; an action that is not unknown cannot be resolved; and a
// reversing action resolved SUCCEEDED restarts its saga so the original is
// marked (adr/0216).
func TestResolveActionOutcome_Endpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t)
	fake := &fakeActionStarter{}
	b.actionStarterOverride = fake

	toUnknown := func(req aggregate.RequestAction) uuid.UUID {
		id := approvedAction(t, invID, req)
		dispatched(t, invID, id)
		systemHandle(t, invID, aggregate.RecordOutcomeUnknown{ActionID: id, Detail: "the run stopped"})
		return id
	}
	plain := toUnknown(aggregate.RequestAction{})
	body := ResolveOutcomeBody{Outcome: aggregate.ActionStatusFailed, Note: "the console shows no isolation"}

	resp, _ := postActionDecision(t, b, mintToken(t, map[string]any{"delegate_kind": "claude"}), plain.String(), "resolve", body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("AI-delegated resolve: status %d; want 403", resp.StatusCode)
	}

	resp, out := postActionDecision(t, b, mintToken(t, nil), plain.String(), "resolve", body)
	if resp.StatusCode != http.StatusOK || out.Status != aggregate.ActionStatusFailed {
		t.Fatalf("resolve: resp %d out %+v; want 200/FAILED", resp.StatusCode, out)
	}
	ac, _ := testHandler.Reads().LoadActionCurrent(context.Background(), plain)
	if ac.Status != aggregate.ActionStatusFailed {
		t.Errorf("projected %s; want FAILED", ac.Status)
	}

	resp, _ = postActionDecision(t, b, mintToken(t, nil), plain.String(), "resolve", body)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("resolving an action that is no longer unknown: status %d; want 422", resp.StatusCode)
	}
	if len(fake.sagas) != 0 {
		t.Errorf("a plain action's resolution started sagas %v", fake.sagas)
	}

	// A reversal whose own outcome was unknown, resolved SUCCEEDED.
	orig := approvedAction(t, invID, aggregate.RequestAction{})
	dispatched(t, invID, orig)
	systemHandle(t, invID, aggregate.ResultAction{ActionID: orig, FinalOutcome: "SUCCEEDED", Attempts: 1})
	reversing := toUnknown(aggregate.RequestAction{ActionType: "host.unisolate", IsReversal: true, ReversalOfRef: orig})

	resp, out = postActionDecision(t, b, mintToken(t, nil), reversing.String(), "resolve",
		ResolveOutcomeBody{Outcome: aggregate.ActionStatusSucceeded, Note: "the host is back on the network"})
	if resp.StatusCode != http.StatusOK || out.Status != aggregate.ActionStatusSucceeded {
		t.Fatalf("resolve reversal: resp %d out %+v; want 200/SUCCEEDED", resp.StatusCode, out)
	}
	if len(fake.sagas) != 1 || fake.sagas[0] != reversing.String() {
		t.Errorf("sagas started %v; want the reversal's saga", fake.sagas)
	}
}

// TestEmitExpired_UnknownActionIsBadInput: a timer for an action the stream
// does not hold can never succeed, so it fails without spending the retry
// budget (the activity needs the real handler, which this package's harness has).
func TestEmitExpired_UnknownActionIsBadInput(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	a := temporal.NewActivities(testHandler, nil, nil, nil)
	err := a.EmitExpired(context.Background(), temporal.EmitExpiredInput{
		ActionID: uuid.NewString(), AggregateID: uuid.NewString(), TenantID: module.SingleTenantUUID.String(),
	})
	var appErr *sdktemporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Errorf("err = %v; want a non-retryable ApplicationError", err)
	}
}

// TestEmitDispatched_RecordsTheFrozenAdapter (adr/0406): the dispatched
// record names the adapter of the binding the workflow carries, the one
// frozen at request.
func TestEmitDispatched_RecordsTheFrozenAdapter(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	id := approvedAction(t, invID, aggregate.RequestAction{})
	acts := temporal.NewActivities(testHandler, nil, nil, nil)
	if err := acts.EmitDispatched(context.Background(), temporal.EmitDispatchedInput{
		ActionID: id.String(), AggregateID: invID.String(), TenantID: module.SingleTenantUUID.String(),
		ApproverID: "test-subject", Binding: testFrozenBinding, AdapterRequestID: "action-lifecycle-" + id.String(),
	}); err != nil {
		t.Fatalf("EmitDispatched: %v", err)
	}
	ac, err := testHandler.Reads().LoadActionCurrent(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if ac.Status != aggregate.ActionStatusExecuting || ac.Adapter != testFrozenBinding.Adapter || ac.Binding != testFrozenBinding {
		t.Errorf("after dispatch: status %s adapter %q binding %+v; want EXECUTING via %+v", ac.Status, ac.Adapter, ac.Binding, testFrozenBinding)
	}
}
