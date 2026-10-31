package temporal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
	sdktemporal "go.temporal.io/sdk/temporal"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/comms"
)

// fatalErrorType is the non-retryable ApplicationError type a FATAL dispatch
// maps to, so the workflow's retry budget skips it (08 §5, §6).
const fatalErrorType = "FATAL_ERROR"

// Non-retryable ApplicationError types for the emit activities. A rejection
// is the aggregate deciding against the stream, and a malformed id never
// parses: retrying either only spends the budget before the same failure.
const (
	rejectedErrorType = "AGGREGATE_REJECTED"
	badInputErrorType = "BAD_INPUT"
)

// correlationNamespace scopes the UUIDv5 correlation ids derived for
// system-emitted commands (systemCorrelationID).
var correlationNamespace = uuid.MustParse("e194f0f3-748e-4c70-8a5d-77121aa7dbe5")

// Activities are the side-effecting steps the ActionLifecycle workflow calls.
// They hold the aggregate handler (to emit lifecycle events) and the action
// resolver (to dispatch). Activities run outside the workflow's determinism
// constraint, so they may touch the DB, the clock, and the network.
type Activities struct {
	handler *aggregate.Handler
	// resolver is a swappable holder, not a bare pointer, so a no-restart reload
	// (11 §5.1) can replace the dispatch resolver under a running worker — the
	// worker registers this struct wholesale and cannot re-register, so the
	// swap has to happen behind a stable field.
	resolver *action.AtomicResolver
	comms    *comms.Store
}

// NewActivities constructs the activity set. resolver is an AtomicResolver so
// the dispatch path can be hot-swapped on reload (11 §5.1). comms (Phase F) may
// be nil — with it, a SUCCEEDED notify.* result opens (or, via thread_ref,
// extends) its comms thread; without it, results record exactly as before. It
// is a constructor parameter rather than a builder method because the worker
// registers this struct wholesale (RegisterActivity), and Temporal panics on
// any exported method that isn't a valid activity signature.
func NewActivities(handler *aggregate.Handler, resolver *action.AtomicResolver, comms *comms.Store) *Activities {
	return &Activities{handler: handler, resolver: resolver, comms: comms}
}

// CheckDispatched is the dispatch-ledger guard (08 §6b) as workflows recorded it
// before ActionDispatchState replaced it (adr/0216). It stays registered so a
// workflow history that scheduled it still replays; new runs never call it.
func (a *Activities) CheckDispatched(ctx context.Context, actionID string) (bool, error) {
	id, err := uuid.Parse(actionID)
	if err != nil {
		return false, badInput(fmt.Errorf("CheckDispatched: bad action id: %w", err))
	}
	ac, err := a.handler.Reads().LoadActionCurrent(ctx, id)
	if errors.Is(err, aggregate.ErrNoRecord) {
		return false, nil // no projection yet → not dispatched
	}
	if err != nil {
		return false, err
	}
	switch ac.Status {
	case aggregate.ActionStatusExecuting, aggregate.ActionStatusSucceeded,
		aggregate.ActionStatusFailed, aggregate.ActionStatusReversed:
		return true, nil
	default:
		return false, nil
	}
}

// DispatchState is where an action stands when a workflow run starts: its
// status, and the outcome on record once there is one (SUCCEEDED | FAILED |
// PARTIAL | TIMEOUT from the result, or a person's resolution).
type DispatchState struct {
	Status  string
	Outcome string
}

// ActionDispatchState is the dispatch-ledger guard (08 §6b): it reads where
// the action stands so a run never dispatches twice. Nothing projected yet or
// APPROVED means dispatch; EXECUTING means a dispatch is recorded with no
// result, which a new run can only see after the run that dispatched stopped
// (one execution per workflow id); anything later carries its recorded
// outcome, so a restarted run returns what happened instead of nothing.
func (a *Activities) ActionDispatchState(ctx context.Context, actionID string) (DispatchState, error) {
	id, err := uuid.Parse(actionID)
	if err != nil {
		return DispatchState{}, badInput(fmt.Errorf("ActionDispatchState: bad action id: %w", err))
	}
	ac, err := a.handler.Reads().LoadActionCurrent(ctx, id)
	if errors.Is(err, aggregate.ErrNoRecord) {
		return DispatchState{}, nil
	}
	if err != nil {
		return DispatchState{}, err
	}
	st := DispatchState{Status: ac.Status}
	switch ac.Status {
	case aggregate.ActionStatusSucceeded, aggregate.ActionStatusFailed, aggregate.ActionStatusReversed:
		if st.Outcome, err = a.handler.Reads().LoadRecordedOutcome(ctx, id); err != nil {
			return DispatchState{}, err
		}
	}
	return st, nil
}

// EmitOutcomeUnknownInput identifies the action whose outcome is unknown.
type EmitOutcomeUnknownInput struct {
	ActionID    string
	AggregateID string
	TenantID    string
	ApproverID  string
}

// outcomeUnknownDetail is the reason recorded on an OUTCOME_UNKNOWN action,
// read by the analyst on the card.
const outcomeUnknownDetail = "the workflow that dispatched it stopped before recording a result, " +
	"so whether the tool acted is unknown; check the tool, then resolve the action to what it shows"

// EmitOutcomeUnknown records OUTCOME_UNKNOWN on an action dispatched by a run
// that stopped before recording its result (adr/0216). System-emitted.
func (a *Activities) EmitOutcomeUnknown(ctx context.Context, in EmitOutcomeUnknownInput) error {
	env, err := systemEnvelope(ctx, in.AggregateID, in.TenantID, in.ApproverID)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(in.ActionID)
	if err != nil {
		return badInput(err)
	}
	_, err = a.handler.Handle(ctx, env, aggregate.RecordOutcomeUnknown{ActionID: id, Detail: outcomeUnknownDetail})
	return emitErr(err)
}

// EmitDispatchedInput carries what EmitDispatched needs to record the ledger
// entry. ApproverID is the human principal the workflow carries (05 §6.2). The
// resolution fields (ActionType/Targets/Parameters/SourceScope) let EmitDispatched
// compute WHICH adapter will handle this action, so the dispatched event — and
// thus the ledger's "via <adapter>" and the chronicle's "dispatched via …" —
// records the real tool instead of an empty string (the binding is only
// resolved inside DoDispatch, which runs AFTER this).
type EmitDispatchedInput struct {
	ActionID         string
	AggregateID      string
	TenantID         string
	ApproverID       string
	ActionType       string
	Targets          []aggregate.TargetSpec
	Parameters       map[string]any
	SourceScope      string
	AdapterRequestID string
}

// EmitDispatched records ActionDispatched (the ledger entry, 08 §6b) BEFORE the
// outbound call, so a crash-and-retrigger sees it via CheckDispatched. It stamps
// the resolved adapter (via PlannedBinding — the same selection DoDispatch will
// make) so the write-side provenance is captured at dispatch time. An empty
// adapter (no applicable binding) is not fatal here: DoDispatch surfaces the
// no-binding error authoritatively.
func (a *Activities) EmitDispatched(ctx context.Context, in EmitDispatchedInput) error {
	env, err := systemEnvelope(ctx, in.AggregateID, in.TenantID, in.ApproverID)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(in.ActionID)
	if err != nil {
		return badInput(err)
	}
	adapter := ""
	if a.resolver != nil {
		if r := a.resolver.Load(); r != nil {
			if b, ok := r.PlannedBinding(action.DispatchRequest{
				ActionID:    id,
				ActionType:  in.ActionType,
				Targets:     in.Targets,
				Parameters:  in.Parameters,
				SourceScope: in.SourceScope,
			}); ok {
				adapter = b.Adapter
			}
		}
	}
	_, err = a.handler.Handle(ctx, env, aggregate.DispatchAction{
		ActionID:         id,
		Adapter:          adapter,
		AdapterRequestID: in.AdapterRequestID,
	})
	return emitErr(err)
}

// DispatchInput is the frozen action content the resolver dispatches.
type DispatchInput struct {
	ActionID    string
	ActionType  string
	Targets     []aggregate.TargetSpec
	Parameters  map[string]any
	SourceScope string
}

// DispatchOutput is the classified outcome the workflow records. Attempts is
// the REAL attempt number this dispatch succeeded on (from activity info), so
// the Execution audit record (04 §6.1) never carries a fabricated count.
type DispatchOutput struct {
	FinalOutcome     string
	PerTargetResults map[string]string
	Adapter          string
	AdapterRequestID string
	Attempts         int
	// ErrorDetail is the adapter's human-readable reason on a completed-but-
	// FAILED/PARTIAL dispatch (e.g. "HTTP 401: User Not Authenticated"), so a
	// FAILED action can tell the analyst WHY. Empty on success.
	ErrorDetail string
	// RawResponseRef is the operational reference the adapter returned (e.g.
	// the created ServiceNow incident number) — the analyst's handle into the
	// external system of record.
	RawResponseRef string
}

// DoDispatch runs the actual outbound write via the action resolver. A FATAL
// WriteError maps to a non-retryable ApplicationError (the retry budget skips
// it); a RETRYABLE one is returned as a plain error so Temporal retries within
// the budget; a completed dispatch (any final_outcome) returns normally.
func (a *Activities) DoDispatch(ctx context.Context, in DispatchInput) (DispatchOutput, error) {
	id, err := uuid.Parse(in.ActionID)
	if err != nil {
		return DispatchOutput{}, sdktemporal.NewApplicationError(err.Error(), fatalErrorType)
	}
	var resolver *action.ActionResolver
	if a.resolver != nil {
		resolver = a.resolver.Load()
	}
	if resolver == nil {
		// No write resolver wired (bare worker, or the action layer is off).
		// Honest: nothing to dispatch to.
		return DispatchOutput{}, sdktemporal.NewApplicationError("no write resolver configured", fatalErrorType)
	}
	res, binding, err := resolver.Resolve(ctx, action.DispatchRequest{
		ActionID:    id,
		ActionType:  in.ActionType,
		Targets:     in.Targets,
		Parameters:  in.Parameters,
		SourceScope: in.SourceScope,
	})
	if err != nil {
		var we *action.WriteError
		if errors.As(err, &we) && we.Class == action.WriteFatal {
			return DispatchOutput{}, sdktemporal.NewApplicationError(err.Error(), fatalErrorType)
		}
		return DispatchOutput{}, err // RETRYABLE (or unclassified) → Temporal retries
	}
	perTarget := make(map[string]string, len(res.PerTargetResults))
	for k, v := range res.PerTargetResults {
		perTarget[k] = string(v)
	}
	return DispatchOutput{
		FinalOutcome:     string(res.FinalOutcome),
		PerTargetResults: perTarget,
		Adapter:          binding.Adapter,
		AdapterRequestID: res.AdapterRequestID,
		Attempts:         activityAttempt(ctx),
		ErrorDetail:      res.ErrorDetail,
		RawResponseRef:   res.RawResponseRef,
	}, nil
}

// activityAttempt reads the current attempt number from activity info,
// defaulting to 1 outside an activity context (unit tests), where
// activity.GetInfo panics.
func activityAttempt(ctx context.Context) (n int) {
	n = 1
	defer func() { _ = recover() }()
	if a := int(activity.GetInfo(ctx).Attempt); a > 0 {
		n = a
	}
	return n
}

// EmitResultedInput carries the terminal outcome to record.
type EmitResultedInput struct {
	ActionID         string
	AggregateID      string
	TenantID         string
	ApproverID       string
	FinalOutcome     string
	PerTargetResults map[string]string
	Attempts         int
	ErrorDetail      string
	RawResponseRef   string
	// Adapter is the tool the dispatch ACTUALLY used (from the resolver's chosen
	// binding). The dispatched event recorded the PLANNED adapter before the
	// outbound call; this corrects the record if the live selection differed
	// (e.g. a hot reload between the two activities). Empty when the dispatch
	// never chose a binding (resolve failure) — the planned value then stands.
	Adapter string
}

// EmitResulted records ActionResulted, closing the action.
func (a *Activities) EmitResulted(ctx context.Context, in EmitResultedInput) error {
	env, err := systemEnvelope(ctx, in.AggregateID, in.TenantID, in.ApproverID)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(in.ActionID)
	if err != nil {
		return badInput(err)
	}
	res, err := a.handler.Handle(ctx, env, aggregate.ResultAction{
		ActionID:         id,
		FinalOutcome:     in.FinalOutcome,
		PerTargetResults: in.PerTargetResults,
		Attempts:         in.Attempts,
		ErrorDetail:      in.ErrorDetail,
		Adapter:          in.Adapter,
		RawResponseRef:   in.RawResponseRef,
	})
	if err != nil {
		return emitErr(err)
	}
	// Phase F: a SUCCEEDED notify.* dispatch opens/extends its comms thread.
	// Best-effort by design: the result event is committed, so a miss logs and
	// the analyst still has the action record. Only the attempt that recorded
	// the result does it: a retry finding its result already on the stream
	// applies nothing, and comms.Outbound is not idempotent (it would open a
	// second thread).
	if len(res.AppliedEvents) > 0 {
		a.recordCommsThread(ctx, id, in)
	}
	return nil
}

// recordCommsThread translates one successful notify.* result into comms
// thread state (binding §4: the outbound message is the action; the thread is
// the conversation that follows).
func (a *Activities) recordCommsThread(ctx context.Context, actionID uuid.UUID, in EmitResultedInput) {
	if a.comms == nil || in.FinalOutcome != aggregate.ActionStatusSucceeded {
		return
	}
	ac, err := a.handler.Reads().LoadActionCurrent(ctx, actionID)
	if err != nil || !strings.HasPrefix(ac.ActionType, "notify.") {
		return
	}
	var params struct {
		Message       string  `json:"message"`
		Subject       string  `json:"subject"`
		Body          string  `json:"body"`
		FollowUpHours float64 `json:"follow_up_hours"`
		ThreadRef     string  `json:"thread_ref"`
	}
	_ = json.Unmarshal(ac.Parameters, &params)
	body := params.Message
	if body == "" {
		body = params.Body
	}
	subject := params.Subject
	if subject == "" {
		subject = clip(body, 80)
	}
	target := ""
	if len(ac.Targets) > 0 {
		target = ac.Targets[0].ResolvedIdentifier
		if target == "" {
			target = ac.Targets[0].EntityRef
		}
	}
	threadRef := uuid.Nil
	if params.ThreadRef != "" {
		threadRef, _ = uuid.Parse(params.ThreadRef)
	}
	tenantID, _ := uuid.Parse(in.TenantID)
	aggID, _ := uuid.Parse(in.AggregateID)
	if err := a.comms.Outbound(ctx, comms.OutboundMessage{
		AggregateID:   aggID,
		TenantID:      tenantID,
		ActionID:      actionID,
		ActionType:    ac.ActionType,
		Target:        target,
		Subject:       subject,
		Body:          body,
		Author:        in.ApproverID,
		FollowUpHours: int(params.FollowUpHours),
		At:            time.Now().UTC(),
		ThreadRef:     threadRef,
	}); err != nil {
		activityLogger(ctx).Warn("comms thread not recorded", "action_id", actionID, "error", err)
	}
}

// clip shortens a string for use as a derived subject line.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// EmitReversedInput carries what EmitReversed needs to mark the original
// action REVERSED.
type EmitReversedInput struct {
	OriginalActionID  string
	ReversingActionID string
	AggregateID       string
	TenantID          string
	ApproverID        string
}

// EmitReversed records ActionReversed on the original action once its reversing
// action succeeded (04 §7). System-emitted by the ReversalSaga.
func (a *Activities) EmitReversed(ctx context.Context, in EmitReversedInput) error {
	env, err := systemEnvelope(ctx, in.AggregateID, in.TenantID, in.ApproverID)
	if err != nil {
		return err
	}
	origID, err := uuid.Parse(in.OriginalActionID)
	if err != nil {
		return badInput(err)
	}
	revID, err := uuid.Parse(in.ReversingActionID)
	if err != nil {
		return badInput(err)
	}
	_, err = a.handler.Handle(ctx, env, aggregate.ReverseAction{
		OriginalActionID:  origID,
		ReversingActionID: revID,
	})
	return emitErr(err)
}

// EmitReversalAttempted records ActionReversalAttempted on the original action:
// the reversing dispatch fully succeeded but the original is BEST_EFFORT, so
// the effect cannot be verified — the original stays SUCCEEDED and only the
// back-reference is recorded (04 §7.1 Position C). System-emitted by the
// ReversalSaga. Shares EmitReversedInput: same identifiers, different claim.
func (a *Activities) EmitReversalAttempted(ctx context.Context, in EmitReversedInput) error {
	env, err := systemEnvelope(ctx, in.AggregateID, in.TenantID, in.ApproverID)
	if err != nil {
		return err
	}
	origID, err := uuid.Parse(in.OriginalActionID)
	if err != nil {
		return badInput(err)
	}
	revID, err := uuid.Parse(in.ReversingActionID)
	if err != nil {
		return badInput(err)
	}
	_, err = a.handler.Handle(ctx, env, aggregate.RecordReversalAttempt{
		OriginalActionID:  origID,
		ReversingActionID: revID,
	})
	return emitErr(err)
}

// EmitExpiredInput identifies the action the expiry timer is closing out.
type EmitExpiredInput struct {
	ActionID    string
	AggregateID string
	TenantID    string
}

// EmitExpired records ActionExpired (the timer firing at the frozen deadline,
// 02 §3). The race with a human decision resolves in the human's favor: the
// aggregate rejects ExpireAction on anything no longer pending, and that
// rejection is a benign no-op here — never an error to retry. Only
// infrastructure failures (DB unreachable) propagate for retry. Nobody
// approved an expiry, so its principal is the requester (adr/0217).
func (a *Activities) EmitExpired(ctx context.Context, in EmitExpiredInput) error {
	env, err := systemEnvelope(ctx, in.AggregateID, in.TenantID, "")
	if err != nil {
		return err
	}
	id, err := uuid.Parse(in.ActionID)
	if err != nil {
		return badInput(err)
	}
	requester, err := a.handler.ActionRequester(ctx, env.AggregateID, id)
	if errors.Is(err, aggregate.ErrNoRecord) {
		return badInput(err)
	}
	if err != nil {
		return err
	}
	env.Actor.PrincipalID = requester
	_, err = a.handler.Handle(ctx, env, aggregate.ExpireAction{ActionID: id})
	var rejected *aggregate.RejectedError
	if errors.As(err, &rejected) {
		// Approved, rejected, or already expired before the timer fired — the
		// deadline race resolved; nothing to record.
		return nil
	}
	return err
}

// systemEnvelope builds the SYSTEM-actor envelope the workflow uses to emit
// lifecycle events. The principal is the named human the system acts for (the
// approver; the requester for an expiry), per the actor invariant; Kind is
// SYSTEM. The correlation id is the activity's
// own (systemCorrelationID), so the aggregate recognises a retry as the same
// command. time here is fine — activities are not under the workflow
// determinism constraint.
func systemEnvelope(ctx context.Context, aggregateID, tenantID, principalID string) (aggregate.Envelope, error) {
	aggID, err := uuid.Parse(aggregateID)
	if err != nil {
		return aggregate.Envelope{}, badInput(fmt.Errorf("bad aggregate id: %w", err))
	}
	tid, err := uuid.Parse(tenantID)
	if err != nil {
		return aggregate.Envelope{}, badInput(fmt.Errorf("bad tenant id: %w", err))
	}
	return aggregate.Envelope{
		AggregateID:   aggID,
		TenantID:      tid,
		CorrelationID: systemCorrelationID(ctx),
		Actor:         aggregate.Actor{PrincipalID: principalID, Kind: aggregate.ActorSystem},
		OccurredAt:    time.Now().UTC(),
	}, nil
}

// activityLogger is the activity's logger inside an activity and the process
// logger outside one: the result-recording helpers are also driven directly
// (tests, a replay tool), and the SDK's logger panics without an activity
// context.
// systemCorrelationID derives the correlation id of a system-emitted command
// from the activity that issues it: workflow id, run id and activity id. Every
// retry of one activity gets the same id, so the aggregate recognises a retry
// after a commit whose completion was lost (aggregate.alreadyApplied). A new
// run or another activity gets a different id. The run id is required:
// activity ids restart per run, and an action's workflow id is fixed. Outside
// an activity (unit tests) it falls back to a random id.
func systemCorrelationID(ctx context.Context) uuid.UUID {
	if !activity.IsActivity(ctx) {
		return uuid.New()
	}
	info := activity.GetInfo(ctx)
	return correlationIDFor(info.WorkflowExecution.ID, info.WorkflowExecution.RunID, info.ActivityID)
}

// correlationIDFor is the UUIDv5 of one activity's identity.
func correlationIDFor(workflowID, runID, activityID string) uuid.UUID {
	return uuid.NewSHA1(correlationNamespace, []byte(workflowID+"/"+runID+"/"+activityID))
}

// emitErr marks an aggregate rejection non-retryable. ErrConcurrent and
// infrastructure errors pass through unchanged and stay retryable.
func emitErr(err error) error {
	var rejected *aggregate.RejectedError
	if errors.As(err, &rejected) {
		return sdktemporal.NewNonRetryableApplicationError(err.Error(), rejectedErrorType, err)
	}
	return err
}

// badInput marks a malformed input non-retryable.
func badInput(err error) error {
	return sdktemporal.NewNonRetryableApplicationError(err.Error(), badInputErrorType, err)
}

func activityLogger(ctx context.Context) log.Logger {
	if activity.IsActivity(ctx) {
		return activity.GetLogger(ctx)
	}
	return log.NewStructuredLogger(slog.Default())
}
