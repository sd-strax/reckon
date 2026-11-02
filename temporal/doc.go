// Package temporal holds the in-process Temporal worker and the OSS workflow
// inventory. The worker (worker.go) runs in the same OS process as the backend
// — there is no separate worker binary (design/05-component-architecture.md
// §3.3) — and satisfies supervisor.Component; the dispatch Client (client.go)
// is the command-path entry point for starting workflows.
//
// The worker runs on the `reckon` task queue with the full inventory —
// ActionLifecycle, ActionExpiryTimer, ReversalSaga, RenormalizePass,
// ArchiveInvestigation, PostConclusionPipeline, SummarizeForKnowledgeIndex —
// plus the trivial Ping workflow that proves the round trip. RenormalizePass is
// the one skeleton: it fails fast with a non-retryable "unimplemented" error.
// v1+ adds the top-level InvestigationLifecycleWorkflow.
package temporal
