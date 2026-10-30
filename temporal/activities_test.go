package temporal

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	sdktemporal "go.temporal.io/sdk/temporal"

	"github.com/sd-strax/reckon/aggregate"
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
