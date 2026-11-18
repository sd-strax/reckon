package server

import (
	"testing"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/comms"
	"github.com/sd-strax/reckon/dependency"
	"github.com/sd-strax/reckon/internal/catalog"
	"github.com/sd-strax/reckon/internal/wirefixture"
	"github.com/sd-strax/reckon/knowledge"
)

// checkWireValues holds every status word list the backend serves to its
// constants (adr/0278): each row names the first constant of each const block
// that declares its words. TestWireFixtures runs it after the samples, then
// fails on any word type a sample met that no row checks.
func checkWireValues(t *testing.T) {
	t.Helper()
	for _, c := range []struct {
		words  wirefixture.Words
		dir    string
		consts []string
	}{
		{aggregate.InvestigationStatus(""), "../internal/agentapi", []string{"InvestigationDraft"}},
		{aggregate.ActionStatus(""), "../internal/agentapi", []string{"ActionStatusRequested"}},
		{aggregate.Tier(""), "../internal/agentapi", []string{"TierT1"}},
		{aggregate.AuthMode(""), "../internal/agentapi", []string{"AuthModeManual"}},
		{aggregate.AuthStage(""), "../aggregate", []string{"AuthStageSolo"}},
		{aggregate.HypothesisStatus(""), "../internal/agentapi", []string{"HypothesisProposed"}},
		{aggregate.PredictionStatus(""), "../internal/agentapi", []string{"PredictionUntested"}},
		{aggregate.TestKind(""), "../internal/agentapi", []string{"TestKindTool"}},
		{aggregate.Confidence(""), "../internal/agentapi", []string{"ConfidenceHigh"}},
		{aggregate.VerdictDisposition(""), "../internal/agentapi", []string{"VerdictBenign"}},
		{catalog.CapabilityStatus(""), "../internal/catalog", []string{"StatusAvailable"}},
		{catalog.ActionAvailability(""), "../internal/catalog", []string{"ActionAvailable"}},
		{capability.Coverage(""), "../internal/agentapi", []string{"CoverageComplete"}},
		{capability.DerivationMode(""), "../capability", []string{"DerivationDirect"}},
		{knowledge.Coverage(""), "../internal/agentapi", []string{"RecallComplete"}},
		{knowledge.ImportOutcome(""), "../knowledge", []string{"ImportCreated"}},
		{knowledge.SOPStatus(""), "../internal/agentapi", []string{"SOPDraft"}},
		{comms.Status(""), "../comms", []string{"StatusAwaitingReply"}},
		{dependency.Status(""), "../dependency", []string{"StatusOpen"}},
		{ActionRequestStatus(""), "../internal/agentapi", []string{"RequestPendingManual"}},
		{CommsActStatus(""), ".", []string{"CommsRecorded"}},
		{EnablementApplied(""), ".", []string{"EnablementAppliedStatus"}},
		{ExportStatus(""), ".", []string{"ExportStarted"}},
		{ReindexState(""), ".", []string{"ReindexIdle"}},
	} {
		wirefixture.CheckWords(t, c.words, c.dir, c.consts...)
	}
}
