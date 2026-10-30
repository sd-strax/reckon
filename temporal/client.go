package temporal

import (
	"context"
	"errors"
	"fmt"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/sd-strax/reckon/internal/branding"
)

// Client is a thin dispatch wrapper over the Temporal SDK client, scoped to
// reckon's task queue and namespace. It is the command-path entry point for
// starting workflows; Phase C extends it with real Start* methods
// (StartActionLifecycle, …). At A.7 it carries only Ping — the wireup prover.
type Client struct {
	c         client.Client
	taskQueue string
}

// ClientConfig configures a dispatch Client.
type ClientConfig struct {
	// HostPort is the Temporal frontend gRPC address, e.g. "127.0.0.1:7233"
	// (supervisor.TemporalHostPort — an IP literal, see there for why).
	HostPort string

	// Namespace is the Temporal namespace. Default "default".
	Namespace string

	// TaskQueue is the queue workflows are dispatched onto. Default
	// branding.CLI — must match the Worker's task queue for work to be picked
	// up.
	TaskQueue string
}

// NewClient dials the Temporal frontend and returns a dispatch Client. The
// caller owns the returned Client and must Close it. Bounded by Dial's own
// ready window; use NewClientContext to bound it with a caller deadline.
func NewClient(cfg ClientConfig) (*Client, error) {
	return NewClientContext(context.Background(), cfg)
}

// NewClientContext is NewClient with the dial bounded by ctx (see Dial).
func NewClientContext(ctx context.Context, cfg ClientConfig) (*Client, error) {
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if cfg.TaskQueue == "" {
		cfg.TaskQueue = branding.CLI
	}
	c, err := Dial(ctx, cfg.HostPort, cfg.Namespace)
	if err != nil {
		return nil, err
	}
	return &Client{c: c, taskQueue: cfg.TaskQueue}, nil
}

// Close releases the underlying Temporal client.
func (c *Client) Close() { c.c.Close() }

// StartActionLifecycle starts (fire-and-forget) the ActionLifecycle workflow
// for an approved action and returns the workflow id (which becomes the
// adapter_request_id, 05 §6.2). The workflow id is derived from the action id,
// so a trigger while a run is live is refused by Temporal — a second
// idempotency layer above the dispatch-ledger guard (08 §6b) — and that refusal
// is success here: the live run owns the action, so starting it from the
// approval path and from the stranded-action sweep is safe (adr/0216). Does not
// block on the workflow's completion; the lifecycle runs durably.
func (c *Client) StartActionLifecycle(ctx context.Context, in ActionLifecycleInput) (string, error) {
	id := "action-lifecycle-" + in.ActionID
	if err := c.startOnce(ctx, id, WorkflowActionLifecycle, in); err != nil {
		return "", fmt.Errorf("start ActionLifecycle for %s: %w", in.ActionID, err)
	}
	return id, nil
}

// startOnce starts a workflow under a fixed id, treating a run already live
// under that id as started.
func (c *Client) startOnce(ctx context.Context, id string, workflowType string, in any) error {
	_, err := c.c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        id,
		TaskQueue: c.taskQueue,
	}, workflowType, in)
	var already *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &already) {
		return nil
	}
	return err
}

// StartActionExpiryTimer starts (fire-and-forget) the durable expiry timer for
// a pending action. The workflow id is derived from the action id, so starting
// it twice (request path + the startup sweep) is idempotent — the duplicate is
// rejected by Temporal and swallowed here. An already-elapsed deadline fires
// the timer immediately.
func (c *Client) StartActionExpiryTimer(ctx context.Context, in ActionExpiryTimerInput) error {
	if err := c.startOnce(ctx, "action-expiry-"+in.ActionID, WorkflowActionExpiryTimer, in); err != nil {
		return fmt.Errorf("start ActionExpiryTimer for %s: %w", in.ActionID, err)
	}
	return nil
}

// StartReversalSaga starts (fire-and-forget) the ReversalSaga for a reversal
// action, returning the workflow id. Like StartActionLifecycle, the id is
// derived from the reversing action id, and a run already live under it counts
// as started.
func (c *Client) StartReversalSaga(ctx context.Context, in ReversalSagaInput) (string, error) {
	id := "reversal-saga-" + in.Reversing.ActionID
	if err := c.startOnce(ctx, id, WorkflowReversalSaga, in); err != nil {
		return "", fmt.Errorf("start ReversalSaga for %s: %w", in.Reversing.ActionID, err)
	}
	return id, nil
}

// StartPostConclusionPipeline starts (fire-and-forget) the post-conclusion
// pipeline for a concluded investigation, returning the workflow id. The id is
// derived from the grouping id, so a duplicate trigger (a re-export while one is
// already running) is rejected by Temporal rather than producing two concurrent
// builds. The bundle is a pure function of the immutable concluded
// investigation, so a re-export after completion simply rebuilds the same
// artifact.
func (c *Client) StartPostConclusionPipeline(ctx context.Context, in ArchiveInvestigationInput) (string, error) {
	id := "post-conclusion-" + in.GroupingID
	run, err := c.c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        id,
		TaskQueue: c.taskQueue,
	}, WorkflowPostConclusionPipeline, in)
	if err != nil {
		return "", fmt.Errorf("start PostConclusionPipeline for %s: %w", in.GroupingID, err)
	}
	return run.GetID(), nil
}

// Ping executes the Ping workflow and returns its result, proving the round
// trip command → Temporal → worker → result. Blocks until the workflow
// completes.
func (c *Client) Ping(ctx context.Context, msg string) (string, error) {
	run, err := c.c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		TaskQueue: c.taskQueue,
	}, WorkflowPing, msg)
	if err != nil {
		return "", fmt.Errorf("start Ping workflow: %w", err)
	}
	var out string
	if err := run.Get(ctx, &out); err != nil {
		return "", fmt.Errorf("Ping workflow result: %w", err)
	}
	return out, nil
}
