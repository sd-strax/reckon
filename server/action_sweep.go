package server

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

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
	b.reconcileDependencies(ctx)
	t := time.NewTicker(strandedSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.sweepStrandedActions(ctx)
			b.reconcileDependencies(ctx)
		}
	}
}

// reconcileDependencies moves to SENT every OPEN dependency riding on a
// notify.* action that has SUCCEEDED (adr/0101). The result activity and the
// routes mark on their own paths; this is the durable net under them, for a
// MarkSent that failed after the fact it follows had committed. Idempotent, and
// a closed investigation's rows are left where its conclusion found them.
func (b *Backend) reconcileDependencies(ctx context.Context) {
	if b.cfg.Dependencies == nil || b.cfg.Reads == nil {
		return
	}
	rides, err := b.cfg.Dependencies.OpenRides(ctx)
	if err != nil {
		log.Printf("dependency reconcile: list open rides: %v", err)
		return
	}
	moved := 0
	for _, actionID := range rides {
		if ctx.Err() != nil {
			return
		}
		ac, err := b.cfg.Reads.LoadActionCurrent(ctx, actionID)
		if err != nil {
			continue
		}
		moved += b.markDependenciesSent(ctx, ac)
	}
	if moved > 0 {
		log.Printf("dependency reconcile: marked %d dependenc(y|ies) sent under succeeded notifications", moved)
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
	stranded, err := b.cfg.Reads.ListStrandedActionCurrents(ctx)
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

// startExpiryTimer starts the durable expiry timer for one pending action.
// Best-effort and idempotent (the workflow id derives from the action id).
func (b *Backend) startExpiryTimer(ctx context.Context, actionID, aggregateID, tenantID uuid.UUID, expiresAt time.Time) {
	if expiresAt.IsZero() {
		return
	}
	client := b.getDispatchClient()
	if client == nil {
		return
	}
	if err := client.StartActionExpiryTimer(ctx, temporal.ActionExpiryTimerInput{
		ActionID:    actionID.String(),
		AggregateID: aggregateID.String(),
		TenantID:    tenantID.String(),
		ExpiresAt:   expiresAt,
	}); err != nil {
		log.Printf("action %s: could not start expiry timer (the deadline still binds at approve; the startup sweep will retry): %v", actionID, err)
	}
}

// sweepExpiryTimers ensures every pending action has its durable expiry timer —
// the startup reconciliation covering actions requested before the timer
// existed, or whose start was missed. Idempotent (per-action workflow ids);
// already-elapsed deadlines fire immediately, converging stored statuses that
// lazy expiry left behind.
func (b *Backend) sweepExpiryTimers(ctx context.Context) {
	pending, err := b.cfg.Reads.ListPendingActionCurrents(ctx)
	if err != nil {
		log.Printf("expiry sweep: list pending actions: %v", err)
		return
	}
	started := 0
	for _, a := range pending {
		if a.ExpiresAt.IsZero() {
			continue
		}
		b.startExpiryTimer(ctx, a.ActionID, a.AggregateID, module.SingleTenantUUID, a.ExpiresAt)
		started++
	}
	if started > 0 {
		log.Printf("expiry sweep: ensured timers for %d pending action(s)", started)
	}
}
