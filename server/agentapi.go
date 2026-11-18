package server

import "github.com/sd-strax/reckon/internal/agentapi"

// The shapes the agent exchanges with the backend are defined once in
// internal/agentapi, which the agent shares (adr/0278). The server serves and
// decodes them under the names below, so its handlers are unchanged.

// What the agent reads from the capability and action catalogs, and sends to
// call a verb.
type (
	CapabilitiesResponse = agentapi.CapabilitiesResponse
	CapabilityInvokeBody = agentapi.CapabilityInvokeBody
	WindowInput          = agentapi.WindowInput
	ActionTypesResponse  = agentapi.ActionTypesResponse
)

// What the agent sends to request an action, decide one as the analyst, and
// read the investigation's actions.
type (
	RequestActionBody     = agentapi.RequestActionBody
	RequestActionResponse = agentapi.RequestActionResponse
	ActionRequestStatus   = agentapi.ActionRequestStatus
	ApproveActionBody     = agentapi.ApproveActionBody
	RejectActionBody      = agentapi.RejectActionBody
	ActionView            = agentapi.ActionView
	EditableInput         = agentapi.EditableInput
	ActionsResponse       = agentapi.ActionsResponse
)

// The statuses a request answers with.
const (
	RequestPendingManual   = agentapi.RequestPendingManual
	RequestPendingTwoParty = agentapi.RequestPendingTwoParty
	RequestApproved        = agentapi.RequestApproved
)

// What the agent sends to record a reasoning act, and reads back.
type (
	RecordInterpretationBody     = agentapi.RecordInterpretationBody
	VerdictBody                  = agentapi.VerdictBody
	HypothesisBody               = agentapi.HypothesisBody
	PredictionBody               = agentapi.PredictionBody
	AttestationBody              = agentapi.AttestationBody
	ArtifactBody                 = agentapi.ArtifactBody
	TranscriptInput              = agentapi.TranscriptInput
	ToolCallInput                = agentapi.ToolCallInput
	RecordInterpretationResponse = agentapi.RecordInterpretationResponse
)

// What the agent reads about the investigation: its header, its thread, a
// turn's transcript, its narrative and its hypotheses, and what it sends to
// write a narrative.
type (
	InvestigationView     = agentapi.InvestigationView
	VerdictView           = agentapi.VerdictView
	SeedBody              = agentapi.SeedBody
	CaseNoteOffer         = agentapi.CaseNoteOffer
	ThreadActorView       = agentapi.ThreadActorView
	ThreadEntryView       = agentapi.ThreadEntryView
	ThreadResponse        = agentapi.ThreadResponse
	TranscriptResponse    = agentapi.TranscriptResponse
	NarrativeAuthorView   = agentapi.NarrativeAuthorView
	NarrativeRevisionView = agentapi.NarrativeRevisionView
	NarrativeView         = agentapi.NarrativeView
	NarrativeRequestBody  = agentapi.NarrativeRequestBody
	HypothesesResponse    = agentapi.HypothesesResponse
	HypothesisView        = agentapi.HypothesisView
	PredictionView        = agentapi.PredictionView
	AttestationView       = agentapi.AttestationView
)
