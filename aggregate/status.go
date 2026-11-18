package aggregate

import "github.com/sd-strax/reckon/internal/agentapi"

// Investigation lifecycle states — the "Lifecycle" extension state machine
// (design/01-domain-model.md §Extension 2). Stored lowercase in the
// investigation_current projection; the spec writes them uppercase in prose.
//
// The machine:
//
//	draft ──activate──▶ active ──conclude──▶ concluded ──archive──▶ archived
//	                     ▲   │                   │
//	               resume│   │pause         reopen│ (clears conclusion_ref)
//	                     │   ▼                   ▼
//	                    paused                 active
//
// draft is the entry state: an investigation that can be reasoned over (the
// interpretation layer — hypotheses, notes, evidence) and can even accept an
// external-action REQUEST, but is not yet cleared to *act* on the world.
// Activation is implicit at the action boundary (01-domain-model.md §Extension
// 2, "invisible activate"): the first APPROVAL of a draft's action transitions
// draft→active in the same transaction, and auto-approve is suppressed until
// then so that first action always gets a human. archived is terminal — the
// aggregate accepts no further events. A draft started by mistake can be
// discarded (draft ──discard──▶ discarded, adr/0161): terminal too, with the
// reason on the record — never a deletion.
//
// The words are defined once in internal/agentapi, which the agent shares
// (adr/0278).
const (
	StatusDraft     = agentapi.InvestigationDraft
	StatusActive    = agentapi.InvestigationActive
	StatusPaused    = agentapi.InvestigationPaused
	StatusConcluded = agentapi.InvestigationConcluded
	StatusArchived  = agentapi.InvestigationArchived
	StatusDiscarded = agentapi.InvestigationDiscarded
)

// InvestigationStatus is an investigation's lifecycle status as a served view
// carries it. The words are the untyped constants above, so code that holds a
// status as a string is unchanged.
type InvestigationStatus = agentapi.InvestigationStatus
