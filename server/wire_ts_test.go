package server

import (
	"reflect"

	"github.com/sd-strax/reckon/comms"
	"github.com/sd-strax/reckon/dependency"
	"github.com/sd-strax/reckon/internal/apiversion"
	"github.com/sd-strax/reckon/internal/wirets"
	"github.com/sd-strax/reckon/knowledge"
)

const backendShapes = "../workbench/src/wire/backendShapes.generated.ts"

// backendGenerator registers every shape on the workbench ↔ backend
// connection (adr/0278). A decoder's name is its sample's name, which the
// workbench's fixture test holds one to one. TestWireFixtures writes the file.
func backendGenerator() *wirets.Generator {
	g := wirets.New("server")
	g.Const("API_VERSION", apiversion.Current)

	// Two packages each call their status word Status, and the server's
	// status response already takes the decoder name status.
	g.Name(reflect.TypeFor[comms.Status](), "CommsStatus")
	g.Name(reflect.TypeFor[dependency.Status](), "DependencyStatus")
	// The section read is registered as sopSection; a section alone is an item.
	g.Name(reflect.TypeFor[knowledge.SOPSection](), "SOPSectionItem")

	// What the backend sends: every response the workbench reads.
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

	// The error bodies, which the workbench reads by key, and the
	// acknowledgements of the writes it makes, whose answer it reads only as
	// success.
	g.Decoder("error", reflect.TypeFor[ErrorResponse]())
	g.Decoder("internalError", reflect.TypeFor[InternalErrorResponse]())
	g.Decoder("forbidden", reflect.TypeFor[ForbiddenResponse]())
	g.Decoder("unknownEntity", reflect.TypeFor[UnknownEntityResponse]())
	g.Decoder("unknownAccount", reflect.TypeFor[UnknownAccountResponse]())
	g.Decoder("investigationExists", reflect.TypeFor[InvestigationExistsResponse]())
	g.Decoder("personCandidates", reflect.TypeFor[PersonCandidatesResponse]())
	g.Decoder("accountCandidates", reflect.TypeFor[AccountCandidatesResponse]())
	g.Decoder("superseded", reflect.TypeFor[SupersedeResponse]())
	g.Decoder("commsAct", reflect.TypeFor[CommsActResponse]())
	g.Decoder("enablementChanged", reflect.TypeFor[ApplyEnablementResponse]())

	// What the workbench sends: the body of every call it makes.
	g.Type(reflect.TypeFor[RequestActionBody]())
	g.Type(reflect.TypeFor[ApproveActionBody]())
	g.Type(reflect.TypeFor[RejectActionBody]())
	g.Type(reflect.TypeFor[EditActionBody]())
	g.Type(reflect.TypeFor[RerequestBody]())
	g.Type(reflect.TypeFor[ResolveOutcomeBody]())
	g.Type(reflect.TypeFor[CommsSnoozeBody]())
	g.Type(reflect.TypeFor[CreateDependencyBody]())
	g.Type(reflect.TypeFor[AttachDependencyBody]())
	g.Type(reflect.TypeFor[ResolveDependencyBody]())
	g.Type(reflect.TypeFor[ApplyEnablementBody]())
	g.Type(reflect.TypeFor[CapabilityInvokeBody]())
	g.Type(reflect.TypeFor[CreateInvestigationRequest]())
	g.Type(reflect.TypeFor[RenameRequestBody]())
	g.Type(reflect.TypeFor[LifecycleRequestBody]())
	g.Type(reflect.TypeFor[CaseNoteRequest]())
	g.Type(reflect.TypeFor[NarrativeRequestBody]())
	g.Type(reflect.TypeFor[RecordInterpretationBody]())
	g.Type(reflect.TypeFor[SupersedeBody]())
	g.Type(reflect.TypeFor[IdentityAssertBody]())
	g.Type(reflect.TypeFor[IdentityOwnerBody]())
	g.Type(reflect.TypeFor[IdentityWithdrawBody]())
	g.Type(reflect.TypeFor[knowledge.RecallRequest]())
	g.Type(reflect.TypeFor[knowledge.SimilarRequest]())
	g.Type(reflect.TypeFor[SOPMarkdownBody]())
	return g
}
