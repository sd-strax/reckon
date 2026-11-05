package server

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/comms"
	"github.com/sd-strax/reckon/dependency"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/knowledge"
)

// The stores that reach a database, each narrowed to what handlers call, so a
// handler can be tested without Postgres (adr/0226). The concrete types satisfy
// them. A store that is off is left unset: a nil pointer in one of these does
// not compare equal to nil, so it would pass the "not configured" checks.

// commandHandler applies a command; *aggregate.Handler satisfies it.
type commandHandler interface {
	Handle(ctx context.Context, env aggregate.Envelope, cmd aggregate.Command) (aggregate.Result, error)
}

// readModel reads the aggregate's tables; *aggregate.ReadModel satisfies it.
type readModel interface {
	ActionHistory(ctx context.Context, aggID aggregate.AggregateID, actionID uuid.UUID) ([]aggregate.ActionHistoryEntry, error)
	CountInvestigations(ctx context.Context) (int, error)
	InvestigationForCase(ctx context.Context, caseID string) (string, error)
	InvestigationOpening(ctx context.Context, aggID aggregate.AggregateID) (time.Time, aggregate.Actor, error)
	InvestigationsForCases(ctx context.Context, caseIDs []string) (map[string]aggregate.CaseInvestigation, error)
	ListActionCurrents(ctx context.Context, aggregateID uuid.UUID) ([]aggregate.ActionCurrent, error)
	ListEvidencePins(ctx context.Context, aggregateID uuid.UUID) ([]aggregate.EvidencePin, error)
	ListHypotheses(ctx context.Context, aggregateID uuid.UUID) ([]aggregate.HypothesisCurrent, error)
	ListInvestigations(ctx context.Context) ([]aggregate.InvestigationRow, error)
	ListPendingActionCurrents(ctx context.Context) ([]aggregate.ActionCurrent, error)
	ListPredictions(ctx context.Context, aggregateID uuid.UUID) ([]aggregate.PredictionCurrent, error)
	ListRefAppearances(ctx context.Context, ref string) ([]aggregate.RefAppearance, error)
	ListStrandedActionCurrents(ctx context.Context) ([]aggregate.ActionCurrent, error)
	LoadActionCurrent(ctx context.Context, actionID uuid.UUID) (aggregate.ActionCurrent, error)
	LoadInterpretation(ctx context.Context, id uuid.UUID) (aggregate.InterpretationRecorded, error)
	LoadInvestigationCurrent(ctx context.Context, aggID aggregate.AggregateID) (aggregate.InvestigationCurrent, error)
	Thread(ctx context.Context, aggID aggregate.AggregateID) ([]aggregate.ThreadAct, error)
	TranscriptBody(ctx context.Context, tenantID uuid.UUID, hash string) ([]byte, error)
}

// evidenceStore holds telemetry and STIX objects; *evidence.Store satisfies it.
type evidenceStore interface {
	Persist(ctx context.Context, call evidence.Call, res capability.CapabilityResult) error
	Object(ctx context.Context, ref string) (evidence.Object, error)
	Event(ctx context.Context, id uuid.UUID) (evidence.Event, error)
	EventEntities(ctx context.Context, eventID uuid.UUID) ([]evidence.Object, error)
	IndicatorNeighbourhood(ctx context.Context, indicator evidence.Object) (evidence.Neighbourhood, error)
	InvestigationEvents(ctx context.Context, investigationID uuid.UUID, f evidence.EventFilter) (evidence.Events, error)
	Calls(ctx context.Context, investigationID uuid.UUID) ([]evidence.CallRecord, error)
	Call(ctx context.Context, investigationID, callID uuid.UUID) (evidence.CallRecord, error)
}

// commsStore holds comms threads; *comms.Store satisfies it.
type commsStore interface {
	List(ctx context.Context, aggregateID uuid.UUID, now time.Time) ([]comms.Thread, error)
	Inbound(ctx context.Context, threadID uuid.UUID, author, body string, at time.Time) error
	Ack(ctx context.Context, threadID uuid.UUID, principal string, at time.Time) error
	Done(ctx context.Context, threadID uuid.UUID, principal string, at time.Time) error
	Snooze(ctx context.Context, threadID uuid.UUID, principal string, hours int, at time.Time) error
}

// dependencyStore holds the dependencies (adr/0101); *dependency.Store
// satisfies it. SENT is the engine's transition on a SUCCEEDED notify.* send:
// the result activity's usually, a route's only to catch up a success the
// activity could not see (a person resolving an unknown outcome, an attachment
// racing the send).
type dependencyStore interface {
	List(ctx context.Context, aggregateID uuid.UUID) ([]dependency.Dependency, error)
	Get(ctx context.Context, id uuid.UUID) (dependency.Dependency, error)
	Create(ctx context.Context, c dependency.Create) (dependency.Dependency, error)
	Attach(ctx context.Context, id, actionID uuid.UUID, by dependency.Actor, at time.Time) error
	MarkSent(ctx context.Context, actionID uuid.UUID, at time.Time) (int, error)
	OpenRides(ctx context.Context) ([]uuid.UUID, error)
	Confirm(ctx context.Context, id uuid.UUID, by dependency.Actor, note, externalRef string, at time.Time) error
	Waive(ctx context.Context, id uuid.UUID, by dependency.Actor, note, externalRef string, at time.Time) error
}

// knowledgeStore holds SOPs and case summaries; *knowledge.Store satisfies it.
type knowledgeStore interface {
	Create(ctx context.Context, sop knowledge.SOP) (uuid.UUID, error)
	Get(ctx context.Context, tenantID, id uuid.UUID) (knowledge.SOP, error)
	Import(ctx context.Context, sop knowledge.SOP) (uuid.UUID, bool, error)
	List(ctx context.Context, tenantID uuid.UUID, includeRetired bool) ([]knowledge.SOP, error)
	Retire(ctx context.Context, tenantID, id uuid.UUID) error
	Update(ctx context.Context, tenantID, id uuid.UUID, title, body string, tags []string, recommendation string) error
	RecallSOPs(ctx context.Context, tenantID uuid.UUID, req knowledge.RecallRequest) (knowledge.RecallResult, error)
	RecallSimilar(ctx context.Context, tenantID uuid.UUID, req knowledge.SimilarRequest) (knowledge.SimilarResult, error)
	PriorByEntity(ctx context.Context, tenantID uuid.UUID, req knowledge.PriorRequest) (knowledge.PriorResult, error)
	WriteSummary(ctx context.Context, tenantID uuid.UUID, sum knowledge.Summary) (uuid.UUID, error)
	EnrichSummary(ctx context.Context, tenantID uuid.UUID, investigationRef, narrative, generatorModel string) (uuid.UUID, error)
	ListSummaries(ctx context.Context, tenantID uuid.UUID, limit int) ([]knowledge.CaseSummaryView, error)
	SummaryByInvestigation(ctx context.Context, tenantID uuid.UUID, investigationRef string) (knowledge.CaseSummaryView, error)
}
