package temporal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	sdktemporal "go.temporal.io/sdk/temporal"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
)

// TestCorrelationIDFor: every retry of one activity derives the same id, so the
// aggregate recognises it; a new run or another activity derives a different
// one (#215).
func TestCorrelationIDFor(t *testing.T) {
	base := correlationIDFor("action-lifecycle-a", "run-1", "5")
	if got := correlationIDFor("action-lifecycle-a", "run-1", "5"); got != base {
		t.Errorf("same activity identity gave %v then %v; want equal", base, got)
	}
	for name, got := range map[string]uuid.UUID{
		"new run":        correlationIDFor("action-lifecycle-a", "run-2", "5"),
		"other activity": correlationIDFor("action-lifecycle-a", "run-1", "6"),
		"other workflow": correlationIDFor("action-lifecycle-b", "run-1", "5"),
	} {
		if got == base {
			t.Errorf("%s: correlation id equals the original; want different", name)
		}
	}
}

// TestEmitErr: a domain rejection fails fast; a lost optimistic-concurrency
// race and infrastructure errors stay retryable (#215).
func TestEmitErr(t *testing.T) {
	if emitErr(nil) != nil {
		t.Error("emitErr(nil) should be nil")
	}

	rejected := &aggregate.RejectedError{Err: errors.New("not APPROVED")}
	var appErr *sdktemporal.ApplicationError
	if err := emitErr(rejected); !errors.As(err, &appErr) || !appErr.NonRetryable() || appErr.Type() != rejectedErrorType {
		t.Errorf("rejection = %v; want a non-retryable %s ApplicationError", err, rejectedErrorType)
	}

	for _, err := range []error{aggregate.ErrConcurrent, fmt.Errorf("commit: %w", errors.New("connection reset"))} {
		if got := emitErr(err); !errors.Is(got, err) || errors.As(got, &appErr) {
			t.Errorf("emitErr(%v) = %v; want it returned unchanged (retryable)", err, got)
		}
	}
}

// TestEmitActivities_BadInputNonRetryable: a malformed id can never parse, so
// it fails without spending the retry budget (#215).
func TestEmitActivities_BadInputNonRetryable(t *testing.T) {
	a := &Activities{}
	ctx := context.Background()
	const agg, tenant = "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"

	cases := map[string]error{
		"dispatched bad action id": a.EmitDispatched(ctx, EmitDispatchedInput{ActionID: "nope", AggregateID: agg, TenantID: tenant}),
		"resulted bad aggregate":   a.EmitResulted(ctx, EmitResultedInput{ActionID: agg, AggregateID: "nope", TenantID: tenant}),
		"reversed bad original":    a.EmitReversed(ctx, EmitReversedInput{OriginalActionID: "nope", AggregateID: agg, TenantID: tenant}),
		"attempted bad reversing":  a.EmitReversalAttempted(ctx, EmitReversedInput{OriginalActionID: agg, ReversingActionID: "nope", AggregateID: agg, TenantID: tenant}),
		"expired bad tenant":       a.EmitExpired(ctx, EmitExpiredInput{ActionID: agg, AggregateID: agg, TenantID: "nope"}),
	}
	for name, err := range cases {
		var appErr *sdktemporal.ApplicationError
		if !errors.As(err, &appErr) || !appErr.NonRetryable() || appErr.Type() != badInputErrorType {
			t.Errorf("%s: %v; want a non-retryable %s ApplicationError", name, err, badInputErrorType)
		}
	}
}

// fatalWriteAdapter answers every dispatch with an adapter-reported fatal
// error: the call was made.
type fatalWriteAdapter struct{ calls int }

func (*fatalWriteAdapter) Name() string                   { return "edr" }
func (*fatalWriteAdapter) Class() capability.AdapterClass { return capability.ClassCustom }
func (*fatalWriteAdapter) SupportedActionOps() []string   { return []string{"contain"} }
func (*fatalWriteAdapter) Health() capability.HealthStatus {
	return capability.HealthStatus{Healthy: true}
}
func (f *fatalWriteAdapter) Dispatch(context.Context, string, map[string]any, string) (action.WriteResult, error) {
	f.calls++
	return action.WriteResult{}, &action.WriteError{Class: action.WriteFatal, Err: errors.New("the tool refused")}
}

// TestDoDispatch_NotSent (adr/0384): a failure before the outbound call, no
// write resolver or a frozen binding that can no longer carry the action, is a
// non-retryable NOT_SENT error with the reason; an error the adapter returns
// keeps its own class, since the call was made.
func TestDoDispatch_NotSent(t *testing.T) {
	ctx := context.Background()
	in := DispatchInput{
		ActionID: "11111111-1111-4111-8111-111111111111", ActionType: "host.isolate",
		Targets: []aggregate.TargetSpec{{EntityRef: "reckon-host--1", ResolvedIdentifier: "WIN-A"}},
		Binding: aggregate.FrozenBinding{Adapter: "edr", Operation: "contain"},
	}
	binding := action.ActionBinding{ActionType: "host.isolate", Adapter: "edr", Operation: "contain", Priority: 100,
		Params: map[string]any{"device_id": "${target.resolved_identifier}"}}
	notSent := func(name string, err error, reason string) {
		t.Helper()
		var appErr *sdktemporal.ApplicationError
		if !errors.As(err, &appErr) || appErr.Type() != notSentErrorType || !appErr.NonRetryable() || appErr.Message() != reason {
			t.Errorf("%s: err = %v; want a non-retryable %s error %q", name, err, notSentErrorType, reason)
		}
	}

	_, err := (&Activities{}).DoDispatch(ctx, in)
	notSent("no resolver", err, "not sent: no write resolver configured")

	disabled := action.NewAtomicResolver(action.NewActionResolver(map[string][]action.ActionBinding{"host.isolate": {binding}}, nil))
	_, err = NewActivities(nil, disabled, nil, nil).DoDispatch(ctx, in)
	notSent("adapter not enabled", err, "not sent: adapter edr is not configured or not enabled")

	// On a later attempt the same failure is not "not sent": the first
	// attempt may have reached the tool before a reload took the binding
	// away. It is retryable, so the record ends UNKNOWN when the budget runs
	// out, and its message makes no claim about sending.
	second := context.WithValue(ctx, attemptKey{}, 2)
	_, err = NewActivities(nil, disabled, nil, nil).DoDispatch(second, in)
	var later *sdktemporal.ApplicationError
	if !errors.As(err, &later) || later.Type() != bindingUnusableErrorType || later.NonRetryable() ||
		strings.Contains(later.Message(), "not sent") {
		t.Errorf("pre-call failure on attempt 2: err = %v; want a retryable %s error that does not say not sent", err, bindingUnusableErrorType)
	}
	_, err = (&Activities{}).DoDispatch(second, in)
	if !errors.As(err, &later) || later.Type() != bindingUnusableErrorType || later.NonRetryable() ||
		strings.Contains(later.Message(), "not sent") {
		t.Errorf("no resolver on attempt 2: err = %v; want a retryable %s error that does not say not sent", err, bindingUnusableErrorType)
	}

	edr := &fatalWriteAdapter{}
	live := action.NewAtomicResolver(action.NewActionResolver(map[string][]action.ActionBinding{"host.isolate": {binding}},
		map[string]action.WriteAdapter{"edr": edr}))
	_, err = NewActivities(nil, live, nil, nil).DoDispatch(ctx, in)
	var appErr *sdktemporal.ApplicationError
	if !errors.As(err, &appErr) || appErr.Type() != fatalErrorType || edr.calls != 1 {
		t.Errorf("adapter fatal error: err = %v (calls %d); want %s, the call made", err, edr.calls, fatalErrorType)
	}
}
