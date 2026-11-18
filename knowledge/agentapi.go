package knowledge

import "github.com/sd-strax/reckon/internal/agentapi"

// The recall results and section reads the agent decodes are defined once in
// internal/agentapi, which the agent shares (adr/0278). The knowledge service
// builds and returns them under the names below, so its code is unchanged.
type (
	SectionRef      = agentapi.SectionRef
	SOPMatch        = agentapi.SOPMatch
	RecallResult    = agentapi.RecallResult
	SummaryAction   = agentapi.SummaryAction
	SimilarMatch    = agentapi.SimilarMatch
	SimilarResult   = agentapi.SimilarResult
	PriorResult     = agentapi.PriorResult
	CaseSummaryView = agentapi.CaseSummaryView
	SOPSection      = agentapi.SOPSection
	SOPOutline      = agentapi.SOPOutline
	SOPSectionRead  = agentapi.SOPSectionRead
)
