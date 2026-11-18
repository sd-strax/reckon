package server

import (
	"testing"
	"time"

	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/internal/wirefixture"
	"github.com/sd-strax/reckon/knowledge"
)

const backendFixtures = "../workbench/src/wire/fixtures/backend"

// TestWireFixtures keeps the sample of each response the workbench decodes
// (adr/0244).
func TestWireFixtures(t *testing.T) {
	dir := backendFixtures
	wirefixture.Check[StatusResponse](t, dir, "status")
	wirefixture.Check[AuthConfigResponse](t, dir, "authConfig")
	wirefixture.Check[MeResponse](t, dir, "me")
	wirefixture.Check[CapabilitiesResponse](t, dir, "capabilities")
	wirefixture.Check[EnablementResponse](t, dir, "enablement", func(r *EnablementResponse) {
		r.Adapters[0].ConfigSchema = map[string]any{
			"properties": map[string]any{"x": map[string]any{"type": "string", "description": "x", "x-secret": true}},
			"required":   []string{"x"},
		}
	})
	wirefixture.Check[CapabilityInvokeResponse](t, dir, "capabilityInvoke")
	wirefixture.Check[RecordInterpretationResponse](t, dir, "interpretation")
	wirefixture.Check[InvestigationsResponse](t, dir, "investigations")
	wirefixture.Check[InvestigationView](t, dir, "investigation")
	wirefixture.Check[CreateInvestigationResponse](t, dir, "created")
	wirefixture.Check[HypothesesResponse](t, dir, "hypotheses")
	wirefixture.Check[ThreadResponse](t, dir, "thread")
	wirefixture.Check[ActionsResponse](t, dir, "actions")
	wirefixture.Check[CommsThreadsResponse](t, dir, "comms")
	wirefixture.Check[DependenciesResponse](t, dir, "dependencies")
	wirefixture.Check[DependencyView](t, dir, "dependency")
	wirefixture.Check[RequestActionResponse](t, dir, "actionRequested")
	wirefixture.Check[ActionDecisionResponse](t, dir, "actionDecision")
	wirefixture.Check[ActionTypesResponse](t, dir, "actionTypes")
	wirefixture.Check[PinsResponse](t, dir, "pins")
	wirefixture.Check[TranscriptResponse](t, dir, "transcript")
	wirefixture.Check[AppearancesResponse](t, dir, "appearances")
	// The host's identity view as a host carries it (adr/0211): an observed
	// alias, an asserted one with a note, a withdrawn assertion, a split, and
	// a host seen under two domains. Its every key is held by identityAct,
	// the identity acts' answer.
	wirefixture.Check[EvidenceView](t, dir, "evidence", func(v *EvidenceView) {
		at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		v.Identity = &EvidenceIdentity{
			Aliases: []IdentityEdgeView{
				{EdgeID: "relationship--1", OtherRef: "reckon-host--2", Kind: evidence.IdentityObserved, Source: "edr", At: at},
				{EdgeID: "relationship--3", OtherRef: "reckon-host--4", Kind: evidence.IdentityAsserted, By: "u-1", ByName: "Ana Analyst", At: at, Note: "same serial in the asset register"},
				{EdgeID: "relationship--5", OtherRef: "reckon-host--6", Kind: evidence.IdentityAsserted, By: "u-1", ByName: "Ana Analyst", At: at, Withdrawn: true},
			},
			Distinct: []IdentityEdgeView{
				{EdgeID: "relationship--7", OtherRef: "reckon-host--8", Kind: evidence.IdentityAsserted, By: "u-2", ByName: "Ben Analyst", At: at, Note: "two machines"},
			},
			SeenAs: SeenAsView{
				Domains: []string{"corp", "corp.example"},
				Agents:  []SourceIDView{{Tool: "edr", UID: "a-7"}},
				IPs:     []string{"10.0.0.7"},
			},
		}
		// An account's view as an account carries it (adr/0324): the login
		// aliased to it by the record, what it was seen as, an owner from a
		// directory's record and one a person asserted and withdrew; and a
		// person's, with the accounts attached to it. The wire carries one
		// of the three identity views per ref; the sample fills all three.
		v.AccountIdentity = &EvidenceAccountIdentity{
			Aliases:  []IdentityEdgeView{{EdgeID: "relationship--9", OtherRef: "user-account--10", Kind: evidence.IdentityObserved, Source: "dc", At: at}},
			Distinct: []IdentityEdgeView{},
			SeenAs: AccountSeenAsView{
				Logins:         []string{`CONTOSO\jdoe`},
				IDs:            []SourceIDView{{Tool: "dc", UID: "S-1-5-21-1001"}},
				PrincipalNames: []string{"jdoe@contoso.example"},
			},
			Owners: []OwnerView{
				{PersonRef: "identity--11", AccountRef: "user-account--10", EdgeID: "relationship--12", Kind: evidence.IdentityObserved, Source: "idp", At: at},
				{PersonRef: "identity--13", AccountRef: "user-account--10", EdgeID: "relationship--14", Kind: evidence.IdentityAsserted, By: "u-1", ByName: "Ana Analyst", At: at, Note: "the help desk ticket", Withdrawn: true},
			},
		}
		v.PersonIdentity = &EvidencePersonIdentity{
			Aliases:  []IdentityEdgeView{},
			Distinct: []IdentityEdgeView{{EdgeID: "relationship--15", OtherRef: "identity--16", Kind: evidence.IdentityAsserted, By: "u-2", ByName: "Ben Analyst", At: at, Note: "namesakes"}},
			Accounts: []PersonAccountView{
				{AccountRef: "user-account--10", PersonRef: "identity--11", AccountAliases: []string{"user-account--10", "user-account--9"}, EdgeID: "relationship--12", Kind: evidence.IdentityObserved, Source: "idp", At: at},
				{AccountRef: "user-account--17", PersonRef: "identity--19", AccountAliases: []string{"user-account--17"}, EdgeID: "relationship--18", Kind: evidence.IdentityAsserted, By: "u-1", ByName: "Ana Analyst", At: at, Note: "same desk"},
			},
			ReadAccounts: []string{"user-account--10", "user-account--9", "user-account--17"},
		}
	})
	// The identity acts' answer: the view after the act, null when it could
	// not be read.
	wirefixture.Check[IdentityActResponse](t, dir, "identityAct")
	wirefixture.Check[InvestigationEventsResponse](t, dir, "investigationEvents")
	wirefixture.Check[InvestigationCallsResponse](t, dir, "investigationCalls")
	wirefixture.Check[CapabilityCallView](t, dir, "capabilityCall")
	wirefixture.Check[ActionRecordView](t, dir, "actionRecord")
	wirefixture.Check[CaseSummaryDetail](t, dir, "caseSummary")
	wirefixture.Check[SOPListResponse](t, dir, "sops")
	wirefixture.Check[CaseSummaryListResponse](t, dir, "caseSummaries")
	wirefixture.Check[knowledge.SummaryFacets](t, dir, "caseFacets")
	wirefixture.Check[KnowledgeCitationsResponse](t, dir, "citations")
	wirefixture.Check[knowledge.RecallResult](t, dir, "recallSOPs")
	wirefixture.Check[knowledge.SimilarResult](t, dir, "recallSimilar")
	wirefixture.Check[knowledge.SOP](t, dir, "sop")
	wirefixture.Check[knowledge.SOPRevisions](t, dir, "sopRevisions")
	wirefixture.Check[knowledge.SOPOutline](t, dir, "sopOutline")
	wirefixture.Check[knowledge.SOPSectionRead](t, dir, "sopSection")
	wirefixture.Check[LifecycleResponse](t, dir, "lifecycle")
	wirefixture.Check[RenameResponse](t, dir, "renamed")
	wirefixture.Check[NarrativeView](t, dir, "narrative")
	wirefixture.Check[SOPMarkdownImportResponse](t, dir, "sopImported")
	wirefixture.Check[CaseListResponse](t, dir, "cases")
	wirefixture.Check[CaseView](t, dir, "case")
	checkWireValues(t)
	wirefixture.RequireWordsChecked(t)
}
