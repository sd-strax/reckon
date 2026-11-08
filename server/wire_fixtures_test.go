package server

import (
	"testing"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/internal/wirefixture"
	"github.com/sd-strax/reckon/knowledge"
)

const backendFixtures = "../workbench/src/wire/fixtures/backend"

// TestWireFixtures keeps the sample of each response the workbench decodes
// (adr/0244). Inline map responses are stated here as structs: their envelope
// keys are the one part this test restates by hand rather than reads.
func TestWireFixtures(t *testing.T) {
	dir := backendFixtures
	wirefixture.Check[StatusResponse](t, dir, "status")
	wirefixture.Check[AuthConfigResponse](t, dir, "authConfig")
	wirefixture.Check[MeResponse](t, dir, "me")
	wirefixture.Check[struct {
		Capabilities []capability.CapabilitySummary `json:"capabilities"`
		CaseQueue    string                         `json:"case_queue,omitempty"`
	}](t, dir, "capabilities")
	wirefixture.Check[EnablementResponse](t, dir, "enablement", func(r *EnablementResponse) {
		r.Adapters[0].ConfigSchema = map[string]any{
			"properties": map[string]any{"x": map[string]any{"type": "string", "description": "x", "x-secret": true}},
			"required":   []string{"x"},
		}
	})
	wirefixture.Check[CapabilityInvokeResponse](t, dir, "capabilityInvoke")
	wirefixture.Check[RecordInterpretationResponse](t, dir, "interpretation")
	wirefixture.Check[struct {
		Investigations []InvestigationView `json:"investigations"`
	}](t, dir, "investigations")
	wirefixture.Check[InvestigationView](t, dir, "investigation")
	wirefixture.Check[CreateInvestigationResponse](t, dir, "created")
	wirefixture.Check[struct {
		Hypotheses []HypothesisView `json:"hypotheses"`
	}](t, dir, "hypotheses")
	wirefixture.Check[struct {
		Thread []ThreadEntryView `json:"thread"`
	}](t, dir, "thread")
	wirefixture.Check[struct {
		Actions []ActionView `json:"actions"`
	}](t, dir, "actions")
	wirefixture.Check[struct {
		Threads []CommsThreadView `json:"threads"`
	}](t, dir, "comms")
	wirefixture.Check[struct {
		Dependencies []DependencyView `json:"dependencies"`
	}](t, dir, "dependencies")
	wirefixture.Check[DependencyView](t, dir, "dependency")
	wirefixture.Check[RequestActionResponse](t, dir, "actionRequested")
	wirefixture.Check[ActionDecisionResponse](t, dir, "actionDecision")
	wirefixture.Check[struct {
		ActionTypes []action.ActionSummary `json:"action_types"`
	}](t, dir, "actionTypes")
	wirefixture.Check[struct {
		Pins []PinView `json:"pins"`
	}](t, dir, "pins")
	wirefixture.Check[struct {
		InterpretationID string `json:"interpretation_id"`
		TurnID           string `json:"turn_id"`
		ContentHash      string `json:"content_hash"`
		Body             string `json:"body"`
	}](t, dir, "transcript")
	wirefixture.Check[struct {
		Ref         string           `json:"ref"`
		Appearances []AppearanceView `json:"appearances"`
	}](t, dir, "appearances")
	wirefixture.Check[EvidenceView](t, dir, "evidence")
	wirefixture.Check[InvestigationEventsResponse](t, dir, "investigationEvents")
	wirefixture.Check[InvestigationCallsResponse](t, dir, "investigationCalls")
	wirefixture.Check[CapabilityCallView](t, dir, "capabilityCall")
	wirefixture.Check[ActionRecordView](t, dir, "actionRecord")
	wirefixture.Check[CaseSummaryDetail](t, dir, "caseSummary")
	wirefixture.Check[struct {
		SOPs []SOPRow `json:"sops"`
	}](t, dir, "sops")
	wirefixture.Check[struct {
		CaseSummaries []CaseSummaryRow `json:"case_summaries"`
	}](t, dir, "caseSummaries")
	wirefixture.Check[KnowledgeCitationsResponse](t, dir, "citations")
	wirefixture.Check[knowledge.RecallResult](t, dir, "recallSOPs")
	wirefixture.Check[knowledge.SimilarResult](t, dir, "recallSimilar")
	wirefixture.Check[knowledge.SOP](t, dir, "sop")
	wirefixture.Check[LifecycleResponse](t, dir, "lifecycle")
	wirefixture.Check[RenameResponse](t, dir, "renamed")
	wirefixture.Check[struct {
		ID      string `json:"id"`
		Outcome string `json:"outcome"`
		Title   string `json:"title"`
	}](t, dir, "sopImported")
	wirefixture.Check[CaseListResponse](t, dir, "cases")
	wirefixture.Check[CaseView](t, dir, "case")
}
