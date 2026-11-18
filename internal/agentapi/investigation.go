package agentapi

import "time"

// InvestigationView is the JSON shape of an investigation returned to clients.
type InvestigationView struct {
	AggregateID       string              `json:"aggregate_id"`
	Title             string              `json:"title"`
	Status            InvestigationStatus `json:"status"`
	LastEventSequence int64               `json:"last_event_sequence"`

	// Verdict is the disposition of record (01 §Verdict), absent when none:
	// the honest zero, no stored "pending".
	Verdict *VerdictView `json:"verdict,omitempty"`

	// Seed is the investigation's root (01 §Extension 1); absent on pre-seed
	// investigations. SeedSummary is the triage-queue display line.
	Seed        *SeedBody `json:"seed,omitempty"`
	SeedSummary string    `json:"seed_summary,omitempty"`

	// Triage cues (ui/06 §Tree view: "what needs me, now?"), list rows only:
	// actions still awaiting a human decision, and the soonest approval-window
	// deadline among them (the countdown outranks everything else).
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
	PendingActions int        `json:"pending_actions,omitempty"`
	NearestExpiry  *time.Time `json:"nearest_expiry,omitempty"`
	// Stage inputs for the work queue (design/ui 08 §8.3), list rows only, so
	// a row's stage step derives without fetching every investigation: whether
	// a verdict act exists, the non-notify actions not yet terminal, and the
	// notify.* actions not yet terminal.
	HasVerdict        bool `json:"has_verdict,omitempty"`
	OpenActions       int  `json:"open_actions,omitempty"`
	OpenNotifications int  `json:"open_notifications,omitempty"`

	// CreatedAt and OpenedByName are the investigation's start (the
	// investigation-time divider of the Story lens) and who opened it: the
	// creating act's actor by display name (adr/0077). Detail responses.
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	OpenedByName string     `json:"opened_by_name,omitempty"`

	// Narrative is the investigation's written account with its authorship
	// (adr/0103); absent until one is recorded. Detail responses only.
	Narrative *NarrativeView `json:"narrative,omitempty"`

	// HypothesisFraming is the tenant's competing-hypotheses dial
	// (config.Reasoning.HypothesisFraming; 01 §reckon-hypothesis): "leading_null"
	// (default) or "leading_only". Detail responses only: the agent session
	// renders its framing contract from it.
	HypothesisFraming string `json:"hypothesis_framing,omitempty"`

	// AIReasoning is the tenant's autonomous-reasoning dial (config
	// trust.ai_reasoning; 01 §Interpretation types). False (the default) means
	// the engine refuses an AI delegate's outcome on a PROPOSED hypothesis.
	// Detail responses only: the agent's reasoning-state block says so only
	// when it is true for this tenant.
	AIReasoning bool `json:"ai_reasoning,omitempty"`

	// CaseNoteOnSeed is the tenant's case write-back posture
	// (config.Cases.NoteOnSeed; adr/0032): "confirm" | "auto" | "off". CaseNote
	// is the offer itself, the default note text for the seed case, present
	// on the create AND detail responses of a case-rooted investigation when
	// the posture is not off. The client offers it on demand (confirm) or
	// posts it at creation (auto) via POST /investigations/{id}/case_note.
	CaseNoteOnSeed string         `json:"case_note_on_seed,omitempty"`
	CaseNote       *CaseNoteOffer `json:"case_note,omitempty"`
}

// VerdictView is the verdict fold as served.
type VerdictView struct {
	Disposition      VerdictDisposition `json:"disposition"`
	Rationale        string             `json:"rationale,omitempty"`
	VerdictAt        time.Time          `json:"verdict_at"`
	InterpretationID string             `json:"interpretation_id,omitempty"`
}

// SeedBody is the client shape of the investigation's root (01 §Extension 1).
type SeedBody struct {
	Type                string `json:"type"` // alert | entity | question | case
	AlertID             string `json:"alert_id,omitempty"`
	Source              string `json:"source,omitempty"`
	DetectionFindingRef string `json:"detection_finding_ref,omitempty"`
	EntityRef           string `json:"entity_ref,omitempty"`
	EntityIdentifier    string `json:"entity_identifier,omitempty"`
	HypothesisStatement string `json:"hypothesis_statement,omitempty"`
	CaseID              string `json:"case_id,omitempty"`
	CaseRef             string `json:"case_ref,omitempty"`
}

// CaseNoteOffer is what the create response hands the client when a note may
// be posted: the default text, which the analyst may edit before posting.
type CaseNoteOffer struct {
	CaseID string `json:"case_id"`
	Body   string `json:"body"`
	// SystemName is the case system's display name ("ServiceNow", "Jira"),
	// derived from the seed's source label or, failing that, the promoted
	// case read's own `sor` field; "" when neither says (the client then says
	// "the case system"). Display only: never used to route the write.
	SystemName string `json:"system_name,omitempty"`
}

// NarrativeAuthorView is who recorded a narrative revision.
type NarrativeAuthorView struct {
	Kind string `json:"kind"` // HUMAN | AI_DELEGATED
	Name string `json:"name,omitempty"`
	// Model is the drafting model, on an AI_DELEGATED revision.
	Model string `json:"model,omitempty"`
}

// NarrativeRevisionView is one revision of the history, newest first.
type NarrativeRevisionView struct {
	Revision   int                 `json:"revision"`
	SequenceNo int64               `json:"sequence_no"`
	OccurredAt time.Time           `json:"occurred_at"`
	Author     NarrativeAuthorView `json:"author"`
	Body       string              `json:"body"`
}

// NarrativeView is the current narrative: its body, who recorded this
// revision, who drafted the first one, the event it was written at (so a
// reader can say how far the record has moved since), and the history.
type NarrativeView struct {
	InvestigationRef string                  `json:"investigation_ref"`
	Revision         int                     `json:"revision"`
	Body             string                  `json:"body"`
	SequenceNo       int64                   `json:"sequence_no"`
	OccurredAt       time.Time               `json:"occurred_at"`
	Author           NarrativeAuthorView     `json:"author"`
	DraftedBy        NarrativeAuthorView     `json:"drafted_by"`
	Revisions        []NarrativeRevisionView `json:"revisions"`
}

// NarrativeRequestBody appends a revision. Model is set by a delegate's
// draft only; the aggregate refuses it on a human actor.
type NarrativeRequestBody struct {
	Body  string `json:"body"`
	Model string `json:"model,omitempty"`
}

// ThreadActorView is who authored one reasoning act: the accountability
// surface of "AI is a delegate, never a principal". Principal is always the
// human; Kind says whether the AI (or the system) acted on their behalf.
type ThreadActorView struct {
	Principal string `json:"principal"`
	// PrincipalName is the display name the identity provider presented at the
	// time of the act (adr/0077); empty on events recorded before it existed.
	PrincipalName string `json:"principal_name,omitempty"`
	// Roles is what the identity provider asserted in the token at the time of
	// the act; RequiredRoles the role set reckon required of it (adr/0105).
	// Both empty on events recorded before they existed and on SYSTEM acts.
	Roles         []string `json:"roles,omitempty"`
	RequiredRoles []string `json:"required_roles,omitempty"`
	Kind          string   `json:"kind"` // HUMAN | AI_DELEGATED | SYSTEM
	// Model names the LLM for AI-delegated acts (actor.delegate).
	Model string `json:"model,omitempty"`
}

// ThreadEntryView is one reasoning act in the thread.
type ThreadEntryView struct {
	SequenceNo         int64           `json:"sequence_no"`
	OccurredAt         time.Time       `json:"occurred_at"`
	Actor              ThreadActorView `json:"actor"`
	InterpretationID   string          `json:"interpretation_id"`
	InterpretationType string          `json:"interpretation_type"`
	Summary            string          `json:"summary"`
	Confidence         Confidence      `json:"confidence,omitempty"`
	InputRefs          []string        `json:"input_refs,omitempty"`
	OutputRefs         []string        `json:"output_refs,omitempty"`
	// ToolCalls counts the act's recorded tool dispatches; HasTranscript
	// reports a committed transcript in the side store. Presence indicators:
	// the bodies stay in their stores (GET .../transcript serves the body).
	ToolCalls     int  `json:"tool_calls,omitempty"`
	HasTranscript bool `json:"has_transcript,omitempty"`

	// Superseded reports that a later interpretation.superseded retracted this
	// act (e.g. a revised verdict, or an un-pinned finding). The act stays in the
	// thread (no deletion), but a surface renders it struck, not current.
	Superseded bool `json:"superseded,omitempty"`

	// Disposition is a verdict act's disposition (BENIGN | SUSPICIOUS |
	// MALICIOUS), so the thread can say what a verdict was, and what a
	// revision changed, without the rationale standing in for it.
	Disposition VerdictDisposition `json:"disposition,omitempty"`

	// ConsultedSOPs is the act's knowledge-retrieval provenance
	// (design/ui 02 §2.11 renders the chips).
	ConsultedSOPs []ConsultedSOP `json:"consulted_sops,omitempty"`

	// ConsultedSimilar is the act's similar-investigation provenance (06 §6).
	ConsultedSimilar []ConsultedSimilarInvestigation `json:"consulted_similar_investigations,omitempty"`
}

// ThreadResponse is GET /api/investigations/{id}/thread.
type ThreadResponse struct {
	Thread []ThreadEntryView `json:"thread"`
}

// ConsultedSOP records one SOP surfaced by knowledge-service retrieval during
// a reasoning act (01 INTERPRETATION schema; 06 §6): retrieval provenance.
// Cited distinguishes an SOP the act named by ref from one merely retrieved
// (adr/0236). It records that the actor cited it, not that it applied it.
//
// It is also the stored event format: InterpretationRecorded carries it as
// is, so a tag change changes how old events replay.
type ConsultedSOP struct {
	SOPID          string  `json:"sop_id"`
	Title          string  `json:"title,omitempty"`
	RetrievalScore float64 `json:"retrieval_score,omitempty"`
	// Revision and Cut are the SOP revision and section cut the sections
	// belong to, as recall returned them: section indexes are stable only
	// within one revision and one cut, so the record names both. One act
	// records a procedure once per revision and cut shown.
	Revision int    `json:"revision,omitempty"`
	Cut      string `json:"cut,omitempty"`
	// Sections are the 1-based indexes of the procedure's sections the act
	// had in front of it: what was read, which the inspector shows as
	// "excerpt k of n" (adr/0303). Cited requires at least one.
	Sections []int `json:"sections,omitempty"`
	Cited    bool  `json:"cited"`
}

// ConsultedSimilarInvestigation records one concluded-investigation summary
// surfaced by similarity retrieval during a reasoning act (06 §3, §6): the
// case-knowledge counterpart of ConsultedSOP. InvestigationRef is the source
// investigation's grouping ref (from the summary's provenance), Band the
// substrate similarity band. Cited is as ConsultedSOP.Cited: the act named
// the case by ref.
//
// It is also the stored event format: InterpretationRecorded carries it as
// is, so a tag change changes how old events replay.
type ConsultedSimilarInvestigation struct {
	InvestigationRef string  `json:"investigation_ref"`
	Title            string  `json:"title,omitempty"`
	RetrievalScore   float64 `json:"retrieval_score,omitempty"`
	Band             string  `json:"band,omitempty"`
	Cited            bool    `json:"cited"`
}
