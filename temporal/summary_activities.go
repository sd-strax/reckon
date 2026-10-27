package temporal

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/knowledge"
	"github.com/sd-strax/reckon/summary"
)

// summaryExtractorVersion stamps every summary's provenance (design/06 §3.1
// extractor_version); the extraction itself lives in summary/ (adr/0103).
const summaryExtractorVersion = summary.ExtractorVersion

// summaryWriter is the narrow consumer seam the producer needs — the knowledge
// facade maps the domain summary onto the substrate's DERIVED corpus. Kept an
// interface so the activity is testable without a substrate.
type summaryWriter interface {
	WriteSummary(ctx context.Context, tenantID uuid.UUID, sum knowledge.Summary) (uuid.UUID, error)
}

// SummaryActivities are the side-effecting steps of SummarizeForKnowledgeIndex.
// They hold the aggregate handler (event store + projections, the read side of
// extraction) and the knowledge writer (the substrate-backed DERIVED corpus).
// Extraction reads investigation/STIX domain state here; the mapping onto the
// substrate lives entirely behind the writer, keeping this producer unaware of
// the storage mechanism.
type SummaryActivities struct {
	handler *aggregate.Handler
	writer  summaryWriter
}

// NewSummaryActivities constructs the summary activity set.
func NewSummaryActivities(handler *aggregate.Handler, writer summaryWriter) *SummaryActivities {
	return &SummaryActivities{handler: handler, writer: writer}
}

// SummarizeInput is the frozen identity of the concluded investigation to
// summarize plus the tenant context the write needs.
type SummarizeInput struct {
	GroupingID string `json:"grouping_id"`
	TenantID   string `json:"tenant_id"`
}

// SummarizeOutput is the small descriptor the write returns — the summary
// itself lives in the substrate.
type SummarizeOutput struct {
	SummaryID  string `json:"summary_id"`
	SeedKind   string `json:"seed_kind"`
	Outcome    string `json:"outcome"`
	ActionsLen int    `json:"actions_len"`
}

// Summarize loads a CONCLUDED investigation, extracts the structured summary
// deterministically (design/06 §3.2), and writes it to the DERIVED
// case-summaries corpus. v0 renders the recallable body from the structured
// fields — no LLM; the narrative + conclusion-outcome disambiguation are a
// server-side-LLM enhancement (06 §3.2), and the seam is the summary body /
// GeneratorModel. Idempotent: a re-run revises the prior summary of the same
// source, so a retry never duplicates.
func (a *SummaryActivities) Summarize(ctx context.Context, in SummarizeInput) (SummarizeOutput, error) {
	tenantID, err := uuid.Parse(in.TenantID)
	if err != nil {
		return SummarizeOutput{}, fmt.Errorf("bad tenant id %q: %w", in.TenantID, err)
	}
	aggID, err := uuid.Parse(in.GroupingID)
	if err != nil {
		return SummarizeOutput{}, fmt.Errorf("bad grouping id %q: %w", in.GroupingID, err)
	}
	db := a.handler.DB()

	ic, err := aggregate.LoadInvestigationCurrent(ctx, db, aggID)
	if err != nil {
		return SummarizeOutput{}, fmt.Errorf("load investigation: %w", err)
	}
	if ic.Status != aggregate.StatusConcluded {
		return SummarizeOutput{}, fmt.Errorf("investigation %s is %s; only a CONCLUDED investigation is summarized", in.GroupingID, ic.Status)
	}
	hyps, err := aggregate.ListHypotheses(ctx, db, aggID)
	if err != nil {
		return SummarizeOutput{}, fmt.Errorf("load hypotheses: %w", err)
	}
	actions, err := aggregate.ListActionCurrents(ctx, db, aggID)
	if err != nil {
		return SummarizeOutput{}, fmt.Errorf("load actions: %w", err)
	}
	createdAt, concludedAt, err := investigationSpan(ctx, db, aggID)
	if err != nil {
		return SummarizeOutput{}, err
	}

	sum := buildSummary(in.GroupingID, ic, hyps, actions, createdAt, concludedAt)
	id, err := a.writer.WriteSummary(ctx, tenantID, sum)
	if err != nil {
		return SummarizeOutput{}, fmt.Errorf("write summary: %w", err)
	}
	return SummarizeOutput{
		SummaryID:  id.String(),
		SeedKind:   sum.SeedKind,
		Outcome:    sum.ConclusionOutcome,
		ActionsLen: len(sum.ActionsTaken),
	}, nil
}

// investigationSpan reads the create → conclusion span from the event stream
// (created = first event's occurred-at, concluded = the InvestigationConcluded
// event's). Both feed duration_hours (design/06 §3.2).
func investigationSpan(ctx context.Context, db *sql.DB, aggID uuid.UUID) (created, concluded time.Time, err error) {
	stream, err := aggregate.NewStore(db).LoadStream(ctx, aggID)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("load event stream: %w", err)
	}
	for i := range stream {
		if i == 0 {
			created = stream[i].OccurredAt
		}
		if stream[i].Type == aggregate.EventTypeConcluded {
			concluded = stream[i].OccurredAt
		}
	}
	return created, concluded, nil
}

// buildSummary is the knowledge-summary extraction, now summary.Extract
// (adr/0103): the one fold the Summary lens, the export and this pipeline share.
func buildSummary(groupingID string, ic aggregate.InvestigationCurrent, hyps []aggregate.HypothesisCurrent, actions []aggregate.ActionCurrent, createdAt, concludedAt time.Time) knowledge.Summary {
	return summary.Extract(groupingID, ic, hyps, actions, createdAt, concludedAt)
}
