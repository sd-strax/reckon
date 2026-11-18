package server

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/sd-strax/reckon/internal/wirets"
	"github.com/sd-strax/reckon/knowledge"
)

// TestWireTS_BackendTypes runs the generator over every backend response the
// workbench decodes, and discards the output (adr/0278). The backend's
// TypeScript is generated in a later step. Until then the generator's
// refusals of these types are listed here, so a new one fails this test, and
// so does one that is fixed and left on the list.
func TestWireTS_BackendTypes(t *testing.T) {
	g := wirets.New("server")
	g.Decoder("status", reflect.TypeFor[StatusResponse]())
	g.Decoder("authConfig", reflect.TypeFor[AuthConfigResponse]())
	g.Decoder("me", reflect.TypeFor[MeResponse]())
	g.Decoder("capabilities", reflect.TypeFor[CapabilitiesResponse]())
	g.Decoder("enablement", reflect.TypeFor[EnablementResponse]())
	g.Decoder("capabilityInvoke", reflect.TypeFor[CapabilityInvokeResponse]())
	g.Decoder("interpretation", reflect.TypeFor[RecordInterpretationResponse]())
	g.Decoder("investigations", reflect.TypeFor[InvestigationsResponse]())
	g.Decoder("investigation", reflect.TypeFor[InvestigationView]())
	g.Decoder("created", reflect.TypeFor[CreateInvestigationResponse]())
	g.Decoder("hypotheses", reflect.TypeFor[HypothesesResponse]())
	g.Decoder("thread", reflect.TypeFor[ThreadResponse]())
	g.Decoder("actions", reflect.TypeFor[ActionsResponse]())
	g.Decoder("comms", reflect.TypeFor[CommsThreadsResponse]())
	g.Decoder("dependencies", reflect.TypeFor[DependenciesResponse]())
	g.Decoder("dependency", reflect.TypeFor[DependencyView]())
	g.Decoder("actionRequested", reflect.TypeFor[RequestActionResponse]())
	g.Decoder("actionDecision", reflect.TypeFor[ActionDecisionResponse]())
	g.Decoder("actionTypes", reflect.TypeFor[ActionTypesResponse]())
	g.Decoder("pins", reflect.TypeFor[PinsResponse]())
	g.Decoder("transcript", reflect.TypeFor[TranscriptResponse]())
	g.Decoder("appearances", reflect.TypeFor[AppearancesResponse]())
	g.Decoder("evidence", reflect.TypeFor[EvidenceView]())
	g.Decoder("identityAct", reflect.TypeFor[IdentityActResponse]())
	g.Decoder("investigationEvents", reflect.TypeFor[InvestigationEventsResponse]())
	g.Decoder("investigationCalls", reflect.TypeFor[InvestigationCallsResponse]())
	g.Decoder("capabilityCall", reflect.TypeFor[CapabilityCallView]())
	g.Decoder("actionRecord", reflect.TypeFor[ActionRecordView]())
	g.Decoder("caseSummary", reflect.TypeFor[CaseSummaryDetail]())
	g.Decoder("sops", reflect.TypeFor[SOPListResponse]())
	g.Decoder("caseSummaries", reflect.TypeFor[CaseSummaryListResponse]())
	g.Decoder("caseFacets", reflect.TypeFor[knowledge.SummaryFacets]())
	g.Decoder("citations", reflect.TypeFor[KnowledgeCitationsResponse]())
	g.Decoder("recallSOPs", reflect.TypeFor[knowledge.RecallResult]())
	g.Decoder("recallSimilar", reflect.TypeFor[knowledge.SimilarResult]())
	g.Decoder("sop", reflect.TypeFor[knowledge.SOP]())
	g.Decoder("sopRevisions", reflect.TypeFor[knowledge.SOPRevisions]())
	g.Decoder("sopOutline", reflect.TypeFor[knowledge.SOPOutline]())
	g.Decoder("sopSection", reflect.TypeFor[knowledge.SOPSectionRead]())
	g.Decoder("lifecycle", reflect.TypeFor[LifecycleResponse]())
	g.Decoder("renamed", reflect.TypeFor[RenameResponse]())
	g.Decoder("narrative", reflect.TypeFor[NarrativeView]())
	g.Decoder("sopImported", reflect.TypeFor[SOPMarkdownImportResponse]())
	g.Decoder("cases", reflect.TypeFor[CaseListResponse]())
	g.Decoder("case", reflect.TypeFor[CaseView]())
	_, err := g.Generate()
	var got []string
	if err != nil {
		got = strings.Split(err.Error(), "\n")
	}
	want := []string{
		"server.CommsThreadsResponse.threads[].status: server.StatusResponse and comms.Status both give the decoder status (adr/0278)",
		"server.DependenciesResponse.dependencies[].status: comms.Status and dependency.Status both give the TypeScript type Status (adr/0278)",
		"server.SOPListResponse.sops[].id: uuid.UUID has its own MarshalText, so its JSON is not what its fields say; list it in internal/wirets or drop the method (adr/0278)",
		"server.SOPListResponse.sops[].tenant_id: uuid.UUID has its own MarshalText, so its JSON is not what its fields say; list it in internal/wirets or drop the method (adr/0278)",
		"knowledge.RecallResult.results[].sop_id: uuid.UUID has its own MarshalText, so its JSON is not what its fields say; list it in internal/wirets or drop the method (adr/0278)",
		"knowledge.RecallResult.coverage: capability.Coverage and knowledge.Coverage both give the TypeScript type Coverage (adr/0278)",
		"knowledge.SimilarResult.coverage: capability.Coverage and knowledge.Coverage both give the TypeScript type Coverage (adr/0278)",
		"knowledge.SOP.id: uuid.UUID has its own MarshalText, so its JSON is not what its fields say; list it in internal/wirets or drop the method (adr/0278)",
		"knowledge.SOP.tenant_id: uuid.UUID has its own MarshalText, so its JSON is not what its fields say; list it in internal/wirets or drop the method (adr/0278)",
		"knowledge.SOPRevisions.sop_id: uuid.UUID has its own MarshalText, so its JSON is not what its fields say; list it in internal/wirets or drop the method (adr/0278)",
		"knowledge.SOPOutline.sop_id: uuid.UUID has its own MarshalText, so its JSON is not what its fields say; list it in internal/wirets or drop the method (adr/0278)",
		"knowledge.SOPSectionRead: knowledge.SOPSection and knowledge.SOPSectionRead both give the decoder sopSection (adr/0278)",
		"server.CaseListResponse.cases[]: the decoder name case is a reserved word in TypeScript (adr/0278)",
		"server.CaseView: the decoder name case is a reserved word in TypeScript (adr/0278)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the generator's refusals of the backend types changed:\n got: %q\nwant: %q", got, want)
	}
}
