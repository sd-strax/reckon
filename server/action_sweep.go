package server

import (
	"context"
	"log"
	"time"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/module"
	"github.com/sd-strax/reckon/temporal"
)

// strandedSweepInterval is how often the stranded-action sweep runs after the
// one at startup (adr/0216).
const strandedSweepInterval = 5 * time.Minute

// actionStarter is the narrow seam the action paths need from the Temporal
// dispatch client. *temporal.Client satisfies it; tests inject a fake to see
// what is started.
type actionStarter interface {
	StartActionLifecycle(ctx context.Context, in temporal.ActionLifecycleInput) (string, error)
	StartReversalSaga(ctx context.Context, in temporal.ReversalSagaInput) (string, error)
}

// getActionStarter returns the test override when set, else the live dispatch
// client (nil when it isn't started).
func (b *Backend) getActionStarter() actionStarter {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.actionStarterOverride != nil {
		return b.actionStarterOverride
	}
	if b.dispatchClient == nil {
		return nil
	}
	return b.dispatchClient
}

// runActionSweeps reconciles actions with the workflows that drive them: the
// expiry timers once at startup, and stranded actions at startup and every
// strandedSweepInterval until ctx ends.
func (b *Backend) runActionSweeps(ctx context.Context) {
	b.sweepExpiryTimers(ctx)
	b.sweepStrandedActions(ctx)
	t := time.NewTicker(strandedSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.sweepStrandedActions(ctx)
		}
	}
}

// sweepStrandedActions starts the workflow of every action that should have
// one running (adr/0216): APPROVED actions whose start failed or never
// happened, EXECUTING ones whose run stopped before recording a result (the
// restarted run records OUTCOME_UNKNOWN), and SUCCEEDED reversing actions
// whose saga stopped before updating the original. A start refused because a
// run is live counts as started, so the sweep is safe to run at any time.
func (b *Backend) sweepStrandedActions(ctx context.Context) {
	if b.getActionStarter() == nil {
		return // no dispatch client: approvals cannot run, and stay APPROVED
	}
	stranded, err := b.cfg.Handler.Reads().ListStrandedActionCurrents(ctx)
	if err != nil {
		log.Printf("stranded-action sweep: list actions: %v", err)
		return
	}
	started := 0
	for _, ac := range stranded {
		if ctx.Err() != nil {
			return
		}
		if b.startActionWorkflow(ctx, specFromCurrent(ac)) != "" {
			started++
		}
	}
	if started > 0 {
		log.Printf("stranded-action sweep: started or confirmed workflows for %d action(s)", started)
	}
}

// specFromCurrent is the dispatch content of an action as projected. The
// workflow carries the human who approved it, as on the approval path.
func specFromCurrent(ac aggregate.ActionCurrent) dispatchSpec {
	return dispatchSpec{
		ActionID:      ac.ActionID,
		AggregateID:   ac.AggregateID,
		TenantID:      module.SingleTenantUUID,
		ApproverID:    ac.PrimaryApprover,
		ActionType:    ac.ActionType,
		Targets:       ac.Targets,
		Parameters:    ac.Parameters,
		ReversalOfRef: ac.ReversalOfRef,
	}
}
