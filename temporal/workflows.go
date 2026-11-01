package temporal

import (
	sdktemporal "go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// Workflow type names — the stable identifiers registered on the task queue.
// These names are the contract: renaming a Go function must not change the registered name, so registration
// pins each one explicitly (workflow.RegisterOptions.Name).
const (
	WorkflowActionLifecycle            = "ActionLifecycle"
	WorkflowActionExpiryTimer          = "ActionExpiryTimer"
	WorkflowReversalSaga               = "ReversalSaga"
	WorkflowRenormalizePass            = "RenormalizePass"
	WorkflowArchiveInvestigation       = "ArchiveInvestigation"
	WorkflowPostConclusionPipeline     = "PostConclusionPipeline"
	WorkflowSummarizeForKnowledgeIndex = "SummarizeForKnowledgeIndex"

	// WorkflowPing is the wireup prover — not a domain workflow. It exists so
	// the worker registration round trip is testable with no business logic.
	// See Client.Ping.
	WorkflowPing = "Ping"

	// unimplementedErrType is the ApplicationError type the skeleton domain
	// workflows return. Non-retryable: a premature call fails fast and visibly
	// rather than spinning on retries.
	unimplementedErrType = "Unimplemented"
)

// registerWorkflows registers the full OSS workflow inventory (05 §3.3) on the
// worker under stable type names. Called by Worker.Start.
func registerWorkflows(w worker.Worker) {
	w.RegisterWorkflowWithOptions(Ping, workflow.RegisterOptions{Name: WorkflowPing})
	w.RegisterWorkflowWithOptions(ActionLifecycle, workflow.RegisterOptions{Name: WorkflowActionLifecycle})
	w.RegisterWorkflowWithOptions(ActionExpiryTimer, workflow.RegisterOptions{Name: WorkflowActionExpiryTimer})
	w.RegisterWorkflowWithOptions(ReversalSaga, workflow.RegisterOptions{Name: WorkflowReversalSaga})
	w.RegisterWorkflowWithOptions(RenormalizePass, workflow.RegisterOptions{Name: WorkflowRenormalizePass})
	w.RegisterWorkflowWithOptions(ArchiveInvestigation, workflow.RegisterOptions{Name: WorkflowArchiveInvestigation})
	w.RegisterWorkflowWithOptions(PostConclusionPipeline, workflow.RegisterOptions{Name: WorkflowPostConclusionPipeline})
	w.RegisterWorkflowWithOptions(SummarizeForKnowledgeIndex, workflow.RegisterOptions{Name: WorkflowSummarizeForKnowledgeIndex})
}

// Ping echoes its input, prefixed, proving the command → Temporal → worker →
// result round trip. See WorkflowPing.
func Ping(ctx workflow.Context, msg string) (string, error) {
	workflow.GetLogger(ctx).Info("Ping workflow invoked", "msg", msg)
	return "pong:" + msg, nil
}

// unimplemented is the body of a skeleton domain workflow: it logs and returns
// a non-retryable ApplicationError. A real body replaces the call site, with the
// input/output types the workflow's owning spec defines.
func unimplemented(ctx workflow.Context, name string) error {
	workflow.GetLogger(ctx).Warn("skeleton workflow invoked", "workflow", name)
	return sdktemporal.NewApplicationError(name+" is not implemented", unimplementedErrType)
}

// The OSS domain workflow inventory (05 §3.3). ActionLifecycle (dispatch) and
// ReversalSaga have their real bodies in action_lifecycle.go / reversal_saga.go.

// RenormalizePass re-runs normalizers over stored telemetry when a normalizer
// version changes (05 §3.3). It is a skeleton: it registers, and fails fast with
// Unimplemented.
func RenormalizePass(ctx workflow.Context) error { return unimplemented(ctx, WorkflowRenormalizePass) }

// ArchiveInvestigation and PostConclusionPipeline have their real bodies in
// archive.go; SummarizeForKnowledgeIndex in summary.go.
