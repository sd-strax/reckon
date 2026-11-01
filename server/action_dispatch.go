package server

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/temporal"
)

// autoApproveAndDispatch records the AUTO_POLICY approval (attributed to the
// policy's accountable human, 04 §3.3) and triggers the ActionLifecycle
// workflow. Returns the resulting status + workflow id. A missing accountable or
// a trigger failure degrades to PENDING_MANUAL rather than dropping the action.
func (b *Backend) autoApproveAndDispatch(ctx context.Context, reqEnv aggregate.Envelope, cmd aggregate.RequestAction, decision action.Decision, now time.Time) (status, workflowID string) {
	if decision.PolicyAccountable == "" {
		log.Printf("action %s: AUTO_POLICY matched %s but no accountable human; leaving for manual approval",
			cmd.ActionID, decision.MatchedPolicyRef)
		return "PENDING_MANUAL", ""
	}

	// The approval is attributed to the policy's accountable human, who is
	// the recorded principal — satisfying the actor/approver invariant.
	approveEnv := newEnvelope(reqEnv.AggregateID,
		aggregate.Actor{PrincipalID: decision.PolicyAccountable, Kind: aggregate.ActorHuman}, now)
	_, err := b.cfg.Handler.Handle(ctx, approveEnv, aggregate.ApproveAction{
		ActionID: cmd.ActionID,
		Authorization: aggregate.Authorization{
			Mode:               aggregate.AuthModeAutoPolicy,
			Stage:              aggregate.AuthStageSolo,
			PrimaryApproverRef: decision.PolicyAccountable,
			PrimaryApprovedAt:  now,
			PolicyRef:          decision.MatchedPolicyRef,
		},
	})
	if err != nil {
		log.Printf("action %s: auto-approve failed: %v", cmd.ActionID, err)
		return "PENDING_MANUAL", ""
	}
	// Trigger the durable dispatch workflow. If the trigger fails, the
	// action is APPROVED and durable in the event log, and the stranded-action
	// sweep starts it (adr/0216). With no dispatch client wired it stays
	// APPROVED: nothing can run it.
	wfID := b.startActionWorkflow(ctx, dispatchSpec{
		ActionID:      cmd.ActionID,
		AggregateID:   reqEnv.AggregateID,
		TenantID:      reqEnv.TenantID,
		ApproverID:    decision.PolicyAccountable,
		ActionType:    cmd.ActionType,
		Targets:       cmd.Targets,
		Parameters:    cmd.Parameters,
		ReversalOfRef: cmd.ReversalOfRef,
	})
	return "APPROVED", wfID
}

// dispatchSpec is the frozen action content needed to start the durable
// dispatch workflow — shared by the auto-approval path and the manual approval
// endpoint so both trigger dispatch identically.
type dispatchSpec struct {
	ActionID      uuid.UUID
	AggregateID   uuid.UUID
	TenantID      uuid.UUID
	ApproverID    string
	ActionType    string
	Targets       []aggregate.TargetSpec
	Parameters    json.RawMessage
	ReversalOfRef uuid.UUID
}

// startActionWorkflow triggers the ActionLifecycle (or ReversalSaga for a
// reversal, 04 §7) for an action. Returns the workflow id, or "" when the
// dispatch client is not wired or the trigger fails; the stranded-action sweep
// tries a failed trigger again (adr/0216). A run already live counts as
// started.
func (b *Backend) startActionWorkflow(ctx context.Context, s dispatchSpec) string {
	client := b.getActionStarter()
	if client == nil {
		return ""
	}
	lifecycle := temporal.ActionLifecycleInput{
		ActionID:    s.ActionID.String(),
		AggregateID: s.AggregateID.String(),
		TenantID:    s.TenantID.String(),
		ApproverID:  s.ApproverID,
		ActionType:  s.ActionType,
		Targets:     s.Targets,
		Parameters:  paramsMap(s.Parameters),
	}

	// Route the dispatch to the investigation's organization (03 §3.5). The
	// source scope lives on the immutable Seed, so every action in an
	// investigation inherits one value — reading it here covers the auto-approve,
	// manual-approve, and reversal paths uniformly (all flow through this func).
	// A lookup miss leaves it empty: fail-closed for a scoped tenant (a scoped
	// write finds no binding rather than mis-routing) and a no-op for a
	// single-organization one.
	if b.cfg.Handler != nil {
		if inv, err := b.cfg.Reads.LoadInvestigationCurrent(ctx, s.AggregateID); err != nil {
			log.Printf("action %s: source-scope lookup failed, dispatch will be unscoped: %v", s.ActionID, err)
		} else if inv.Seed != nil {
			lifecycle.SourceScope = inv.Seed.SourceScope
		}
	}
	var (
		wfID string
		err  error
	)
	if s.ReversalOfRef != uuid.Nil {
		// 04 §7.1 Position C: the REVERSED status claim on the original is gated
		// on its reversibility classification — only a RELIABLE original may be
		// marked REVERSED on a successful undo; a best_effort original (e.g.
		// ioc.block) gets action.reversal_attempted instead and stays SUCCEEDED.
		// The classification is read from the value FROZEN on the original at
		// request time (so a catalog edit cannot re-classify it in flight),
		// falling back to the live catalog for actions requested before the
		// field existed. A lookup failure defaults to NOT reliable — the
		// conservative, honest choice.
		reliable := false
		if orig, lerr := b.cfg.Reads.LoadActionCurrent(ctx, s.ReversalOfRef); lerr != nil {
			log.Printf("action %s: reversal reliability lookup for original %s failed (defaulting to best-effort): %v",
				s.ActionID, s.ReversalOfRef, lerr)
		} else if orig.Reversibility != "" {
			reliable = orig.Reversibility == action.ReversibilityReversible
		} else if d, ok := b.cfg.ActionCatalog.Descriptor(orig.ActionType); ok {
			reliable = d.ReliablyReversible()
		}
		wfID, err = client.StartReversalSaga(ctx, temporal.ReversalSagaInput{
			OriginalActionID: s.ReversalOfRef.String(),
			Reversing:        lifecycle,
			OriginalReliable: reliable,
		})
	} else {
		wfID, err = client.StartActionLifecycle(ctx, lifecycle)
	}
	if err != nil {
		log.Printf("action %s: workflow trigger failed (the stranded-action sweep will retry): %v", s.ActionID, err)
		return ""
	}
	return wfID
}

// paramsMap decodes frozen action parameters (JSON object by contract) into the
// map the workflow input carries. A malformed/absent value yields nil.
func paramsMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}
