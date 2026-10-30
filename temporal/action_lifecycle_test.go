package temporal

import (
	"testing"

	"github.com/stretchr/testify/mock"
	sdktemporal "go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/sd-strax/reckon/aggregate"
)

func lifecycleInput() ActionLifecycleInput {
	return ActionLifecycleInput{
		ActionID:    "11111111-1111-4111-8111-111111111111",
		AggregateID: "22222222-2222-4222-8222-222222222222",
		TenantID:    "33333333-3333-4333-8333-333333333333",
		ApproverID:  "alice",
		ActionType:  "host.isolate",
		Targets:     []aggregate.TargetSpec{{EntityRef: "x-host--1", ResolvedIdentifier: "WIN-A"}},
	}
}

// TestActionLifecycle_HappyPath: guard clear → dispatched → SUCCEEDED result.
func TestActionLifecycle_HappyPath(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()
	var a *Activities

	env.OnActivity(a.ActionDispatchState, mock.Anything, mock.Anything).Return(DispatchState{Status: aggregate.ActionStatusApproved}, nil)
	env.OnActivity(a.EmitDispatched, mock.Anything, mock.Anything).Return(nil)
	// The dispatch succeeded on its 2nd attempt: the audit record must carry the
	// REAL attempt number, never a fabricated 1.
	env.OnActivity(a.DoDispatch, mock.Anything, mock.Anything).Return(
		DispatchOutput{FinalOutcome: "SUCCEEDED", PerTargetResults: map[string]string{"0": "OK"}, Attempts: 2}, nil)
	env.OnActivity(a.EmitResulted, mock.Anything, mock.MatchedBy(func(in EmitResultedInput) bool {
		return in.FinalOutcome == "SUCCEEDED" && in.Attempts == 2
	})).Return(nil)

	env.ExecuteWorkflow(ActionLifecycle, lifecycleInput())

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow errored: %v", err)
	}
	env.AssertExpectations(t)
}

// TestActionLifecycle_DispatchWithoutResultIsUnknown is the R15 core: a run
// that finds the dispatch recorded and no result must NOT dispatch again. The
// run that made the call stopped, so the outcome is unknown, and it records
// that instead (adr/0216).
func TestActionLifecycle_DispatchWithoutResultIsUnknown(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()
	var a *Activities

	env.OnActivity(a.ActionDispatchState, mock.Anything, mock.Anything).Return(DispatchState{Status: aggregate.ActionStatusExecuting}, nil)
	env.OnActivity(a.EmitOutcomeUnknown, mock.Anything, mock.MatchedBy(func(in EmitOutcomeUnknownInput) bool {
		return in.ActionID == lifecycleInput().ActionID && in.ApproverID == "alice"
	})).Return(nil)
	// EmitDispatched / DoDispatch / EmitResulted intentionally NOT mocked — if
	// the workflow calls them, the test fails with an unexpected-activity error.

	env.ExecuteWorkflow(ActionLifecycle, lifecycleInput())

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("unknown-outcome path errored: %v", err)
	}
	var outcome string
	if err := env.GetWorkflowResult(&outcome); err != nil || outcome != aggregate.ActionStatusOutcomeUnknown {
		t.Errorf("outcome = %q, %v; want OUTCOME_UNKNOWN", outcome, err)
	}
	env.AssertExpectations(t)
	env.AssertNotCalled(t, "DoDispatch", mock.Anything, mock.Anything)
	env.AssertNotCalled(t, "EmitDispatched", mock.Anything, mock.Anything)
}

// TestActionLifecycle_SettledReturnsRecordedOutcome: a run started for an
// action that already has an outcome dispatches nothing and returns the
// outcome on record — PARTIAL stays PARTIAL even though the action reads
// SUCCEEDED, so a saga restarted after its child finished never over-claims an
// undo (adr/0216).
func TestActionLifecycle_SettledReturnsRecordedOutcome(t *testing.T) {
	cases := []struct{ status, outcome string }{
		{aggregate.ActionStatusSucceeded, "PARTIAL"},
		{aggregate.ActionStatusSucceeded, "SUCCEEDED"},
		{aggregate.ActionStatusFailed, "TIMEOUT"},
		{aggregate.ActionStatusOutcomeUnknown, ""},
	}
	for _, tc := range cases {
		ts := &testsuite.WorkflowTestSuite{}
		env := ts.NewTestWorkflowEnvironment()
		var a *Activities
		env.OnActivity(a.ActionDispatchState, mock.Anything, mock.Anything).Return(DispatchState{Status: tc.status, Outcome: tc.outcome}, nil)

		env.ExecuteWorkflow(ActionLifecycle, lifecycleInput())

		if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
			t.Fatalf("%s/%s: not clean: %v", tc.status, tc.outcome, env.GetWorkflowError())
		}
		want := tc.outcome
		if tc.status == aggregate.ActionStatusOutcomeUnknown {
			want = aggregate.ActionStatusOutcomeUnknown
		}
		var outcome string
		if err := env.GetWorkflowResult(&outcome); err != nil || outcome != want {
			t.Errorf("%s/%s: outcome = %q, %v; want %q", tc.status, tc.outcome, outcome, err, want)
		}
		env.AssertNotCalled(t, "DoDispatch", mock.Anything, mock.Anything)
		env.AssertNotCalled(t, "EmitOutcomeUnknown", mock.Anything, mock.Anything)
	}
}

// TestActionLifecycle_PreChangeHistoryKeepsBooleanGuard: a history recorded
// before adr/0216 replays the boolean CheckDispatched guard and short-circuits
// as it did.
func TestActionLifecycle_PreChangeHistoryKeepsBooleanGuard(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()
	var a *Activities

	env.OnGetVersion(dispatchStateChange, workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	env.OnActivity(a.CheckDispatched, mock.Anything, mock.Anything).Return(true, nil)

	env.ExecuteWorkflow(ActionLifecycle, lifecycleInput())

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("old-history path not clean: %v", env.GetWorkflowError())
	}
	env.AssertNotCalled(t, "DoDispatch", mock.Anything, mock.Anything)
	env.AssertNotCalled(t, "ActionDispatchState", mock.Anything, mock.Anything)
}

// TestActionLifecycle_FatalDispatch: a FATAL dispatch is not retried and closes
// the action FAILED with per-target UNKNOWN (08 §6c — no success inference).
func TestActionLifecycle_FatalDispatch(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()
	var a *Activities

	env.OnActivity(a.ActionDispatchState, mock.Anything, mock.Anything).Return(DispatchState{Status: aggregate.ActionStatusApproved}, nil)
	env.OnActivity(a.EmitDispatched, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(a.DoDispatch, mock.Anything, mock.Anything).Return(
		DispatchOutput{}, sdktemporal.NewApplicationError("no fixture matches", fatalErrorType))

	var gotFailed, gotUnknown, gotOneAttempt bool
	env.OnActivity(a.EmitResulted, mock.Anything, mock.MatchedBy(func(in EmitResultedInput) bool {
		gotFailed = in.FinalOutcome == "FAILED"
		gotUnknown = in.PerTargetResults["0"] == "UNKNOWN"
		gotOneAttempt = in.Attempts == 1 // FATAL is non-retryable: exactly one attempt ran
		return true
	})).Return(nil)

	env.ExecuteWorkflow(ActionLifecycle, lifecycleInput())

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow not clean: completed=%v err=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if !gotFailed || !gotUnknown || !gotOneAttempt {
		t.Errorf("FATAL dispatch → failed=%v unknown=%v oneAttempt=%v; want FAILED / UNKNOWN / attempts=1",
			gotFailed, gotUnknown, gotOneAttempt)
	}
	// DoDispatch is FATAL → called exactly once (no retry).
	env.AssertNumberOfCalls(t, "DoDispatch", 1)
}

// TestActionLifecycle_RetryExhausted: a RETRYABLE dispatch is retried up to the
// budget, then closes FAILED/UNKNOWN.
func TestActionLifecycle_RetryExhausted(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()
	var a *Activities

	env.OnActivity(a.ActionDispatchState, mock.Anything, mock.Anything).Return(DispatchState{Status: aggregate.ActionStatusApproved}, nil)
	env.OnActivity(a.EmitDispatched, mock.Anything, mock.Anything).Return(nil)
	// Plain (retryable) error every attempt.
	env.OnActivity(a.DoDispatch, mock.Anything, mock.Anything).Return(
		DispatchOutput{}, sdktemporal.NewApplicationError("rate limited", "RETRYABLE_ERROR"))
	env.OnActivity(a.EmitResulted, mock.Anything, mock.MatchedBy(func(in EmitResultedInput) bool {
		return in.FinalOutcome == "FAILED"
	})).Return(nil)

	env.ExecuteWorkflow(ActionLifecycle, lifecycleInput())

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow not clean: err=%v", env.GetWorkflowError())
	}
	// Retried up to the budget (3 attempts).
	env.AssertNumberOfCalls(t, "DoDispatch", 3)
}
