package agentapi

import (
	"time"

	"github.com/google/uuid"
)

// SOP statuses (design/06 §2.1). Stored lowercase in the SOP shape.
// Lightweight governance goes straight to published; gated governance walks
// draft → in_review → published.
const (
	SOPDraft     = "draft"
	SOPInReview  = "in_review"
	SOPPublished = "published"
	SOPRetired   = "retired"
)

// SOPStatus is a SOP's status as a served shape carries it. The words are the
// untyped constants above.
type SOPStatus string

// WireValues lists every SOP status, in the order declared above.
func (SOPStatus) WireValues() []string {
	return []string{SOPDraft, SOPInReview, SOPPublished, SOPRetired}
}

// SectionRef locates the section a hit carries within its document (design/06
// §4.2): where in the source the excerpt came from.
type SectionRef struct {
	Idx         int      `json:"idx"`
	Of          int      `json:"of"`
	HeadingPath []string `json:"heading_path"`
	Part        int      `json:"part,omitempty"`
	Parts       int      `json:"parts,omitempty"`
	Location    string   `json:"location"`
}

// SOPMatch is one ranked retrieval result (design/06 §4). Excerpt is the text
// of Section — the piece that matched — up to the budget; Outline is one
// location per section of the document, so the reader knows what else it
// holds and can read it by section.
type SOPMatch struct {
	SOPID uuid.UUID `json:"sop_id"`
	Title string    `json:"title"`
	// Revision is the SOP revision the hit's section indexes belong to; a
	// section read passes it back so a re-import between recall and read is
	// detected (ErrRevisionChanged) rather than answered with another section.
	Revision int `json:"revision"`
	// Cut names the cut the section indexes belong to (substrate
	// Section.Cut); a section read passes it back with the revision, since a
	// changed section budget re-cuts a revision without changing it.
	Cut            string     `json:"cut"`
	Excerpt        string     `json:"excerpt"`
	Truncated      bool       `json:"truncated,omitempty"`
	Section        SectionRef `json:"section"`
	Outline        []string   `json:"outline"`
	Score          float64    `json:"score"`
	MatchRationale string     `json:"match_rationale"`
	Tags           []string   `json:"tags,omitempty"`
	Recommendation string     `json:"recommendation,omitempty"`
	Status         SOPStatus  `json:"status"`
	// Backend attributes the ranker that produced this result (substrate §5.2)
	// — "vector-cosine/<model>" or the degraded "pg-fts", so a consumer can tell
	// semantic recall from the keyword fallback.
	Backend string `json:"backend,omitempty"`
}

// RecallResult is the recall_sops response envelope.
type RecallResult struct {
	Results     []SOPMatch     `json:"results"`
	Coverage    RecallCoverage `json:"coverage"`
	RetrievalAt time.Time      `json:"retrieval_at"`
}

// SummaryAction is one state-changing action a summary records (design/06
// §3.2 actions_taken).
type SummaryAction struct {
	ActionType  string `json:"action_type"`
	TargetCount int    `json:"target_count"`
	Outcome     string `json:"outcome"`
}

// SimilarMatch is one ranked similar-investigation result. InvestigationRef is
// the source investigation's grouping ref (recovered from the reserved source
// tag), so the agent can cite the prior case it learned from.
type SimilarMatch struct {
	InvestigationRef string     `json:"investigation_ref"`
	Title            string     `json:"title"`
	Excerpt          string     `json:"excerpt"`
	Truncated        bool       `json:"truncated,omitempty"`
	Section          SectionRef `json:"section"`
	Outline          []string   `json:"outline"`
	Score            float64    `json:"score"`
	Band             string     `json:"band"`
	MatchRationale   string     `json:"match_rationale"`
	Tags             []string   `json:"tags,omitempty"`
	ContentHash      string     `json:"content_hash,omitempty"`
	Backend          string     `json:"backend,omitempty"`
}

// SimilarResult is the recall_similar_investigations response envelope.
type SimilarResult struct {
	Results     []SimilarMatch `json:"results"`
	Coverage    RecallCoverage `json:"coverage"`
	RetrievalAt time.Time      `json:"retrieval_at"`
	// UnmatchedTags names the filter tags no summary carries, when they are
	// why the answer is empty (Coverage EMPTY_UNDER_FILTER).
	UnmatchedTags []string `json:"unmatched_tags,omitempty"`
}

// PriorResult answers PriorRequest exactly: every concluded investigation whose
// summary records the entity, most recently concluded first. Coverage is
// COMPLETE with results, EMPTY with none — over the summaries that record
// entities (those written since adr/0163, plus the seeded prior cases).
type PriorResult struct {
	Entity         string            `json:"entity"`
	Coverage       RecallCoverage    `json:"coverage"`
	Investigations []CaseSummaryView `json:"investigations"`
}

// CaseSummaryView is one concluded investigation's summary read back by its
// source investigation (design/ui 08 §8.6.1 "Similar past case"): the
// structured fields the producer recorded plus the recallable body — the LLM
// narrative once the client tier wrote one, the deterministic baseline until
// then (06 §3.2). Reserved tags are stripped.
type CaseSummaryView struct {
	InvestigationRef   string          `json:"investigation_ref"`
	Title              string          `json:"title"`
	Body               string          `json:"body"`
	GeneratorModel     string          `json:"generator_model,omitempty"`
	ConclusionAt       string          `json:"conclusion_at,omitempty"`
	SeedKind           string          `json:"seed_kind,omitempty"`
	SeedValue          string          `json:"seed_value,omitempty"`
	Techniques         []string        `json:"techniques"`
	PrimaryEntities    []string        `json:"primary_entities"`
	ActionsTaken       []SummaryAction `json:"actions_taken"`
	ConclusionOutcome  string          `json:"conclusion_outcome,omitempty"`
	DurationHours      float64         `json:"duration_hours,omitempty"`
	VerdictDisposition string          `json:"verdict_disposition,omitempty"`
	VerdictRationale   string          `json:"verdict_rationale,omitempty"`
	Tags               []string        `json:"tags"`
	Revision           int             `json:"revision"`
	ContentHash        string          `json:"content_hash,omitempty"`
}

// SOPSection is one section of a procedure (design/06 §4.2): its place in
// the document and, when read singly, its text. Runes is the text's length
// whether or not the text is carried, so a reader can budget before reading.
type SOPSection struct {
	Idx         int      `json:"idx"`
	Of          int      `json:"of"`
	HeadingPath []string `json:"heading_path"`
	Part        int      `json:"part,omitempty"`
	Parts       int      `json:"parts,omitempty"`
	Location    string   `json:"location"`
	Runes       int      `json:"runes"`
	Text        string   `json:"text,omitempty"`
}

// SOPOutline is a procedure's table of contents: every section without its
// text (GET /api/sops/{id}/sections).
type SOPOutline struct {
	SOPID    uuid.UUID    `json:"sop_id"`
	Title    string       `json:"title"`
	Revision int          `json:"revision"`
	Cut      string       `json:"cut"`
	Status   SOPStatus    `json:"status"`
	Sections []SOPSection `json:"sections"`
}

// SOPSectionRead is one section read whole, with the outline around it
// (GET /api/sops/{id}/sections/{n}), so one call answers "what does this part
// say and what else is there".
type SOPSectionRead struct {
	SOPID    uuid.UUID  `json:"sop_id"`
	Title    string     `json:"title"`
	Revision int        `json:"revision"`
	Cut      string     `json:"cut"`
	Status   SOPStatus  `json:"status"`
	Section  SOPSection `json:"section"`
	Outline  []string   `json:"outline"`
}
