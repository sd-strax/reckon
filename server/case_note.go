package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/config"
)

// The case note (adr/0032-case-note.md): when an investigation is rooted on a
// system-of-record case, the case can carry a note that an investigation was
// opened in reckon. The note is an ordinary ticket.comment x-action INSIDE the
// new investigation — requested by the analyst, authorized by Gate 2, recorded
// against their name — never a side effect of creating the investigation.
// The tenant posture (config.Cases.NoteOnSeed) decides whether the client
// asks first (confirm, the default), requests without asking (auto), or never
// offers (off). "off" is enforced here: the endpoint refuses.

// CaseNoteOffer is what the create response hands the client when a note may
// be posted: the default text, which the analyst may edit before posting.
type CaseNoteOffer struct {
	CaseID string `json:"case_id"`
	Body   string `json:"body"`
	// SystemName is the case system's display name ("ServiceNow", "Jira"),
	// derived from the seed's source label or, failing that, the promoted
	// case read's own `sor` field; "" when neither says (the client then says
	// "the case system"). Display only — never used to route the write.
	SystemName string `json:"system_name,omitempty"`
}

// CaseNoteRequest is the body of POST /api/investigations/{id}/case_note. An
// empty Body posts the default text.
type CaseNoteRequest struct {
	Body string `json:"body,omitempty"`
}

// caseNoteBody renders the default note. It names the investigation and who
// opened it; the reckon:// URL is the same stable pointer the SOP import path
// uses for an investigation (knowledge import source), so a reader of the case
// can find the investigation even without a web surface.
func caseNoteBody(investigationID, title, principal string, now time.Time) string {
	who := principal
	if who == "" {
		who = "an analyst"
	}
	return fmt.Sprintf("Investigation opened in reckon by %s at %s: %q (reckon://investigation/%s). Findings and actions are recorded there; this case will be updated when the investigation concludes.",
		who, now.UTC().Format(time.RFC3339), title, investigationID)
}

// caseNoteOffer builds the offer for a freshly created case-rooted
// investigation under the tenant posture, or nil when the seed is not a case
// or the posture is off.
func (b *Backend) caseNoteOffer(ctx context.Context, investigationID, title string, seed *aggregate.Seed, principal string, now time.Time) *CaseNoteOffer {
	if seed == nil || seed.Type != aggregate.SeedCase || b.cfg.CaseNoteOnSeed == config.CaseNoteOff {
		return nil
	}
	return &CaseNoteOffer{
		CaseID:     seed.CaseID,
		Body:       caseNoteBody(investigationID, title, principal, now),
		SystemName: b.caseSystemName(ctx, seed),
	}
}

// caseSystemDisplayNames maps the labels adapters stamp (provenance.tool, or
// a case body's `sor`) to the name an analyst says.
var caseSystemDisplayNames = map[string]string{
	"servicenow": "ServiceNow",
	"jira":       "Jira",
	"thehive":    "TheHive",
}

// caseSystemName names the case system for display: the seed's source label
// when it is a known system; otherwise the `sor` the promoted case read
// carried (a fixture replaying a ServiceNow incident says "servicenow" there
// while its provenance honestly says "fixture:…"); otherwise "".
func (b *Backend) caseSystemName(ctx context.Context, seed *aggregate.Seed) string {
	if name, ok := caseSystemDisplayNames[strings.ToLower(seed.Source)]; ok {
		return name
	}
	if b.cfg.Evidence == nil || seed.CaseRef == "" {
		return ""
	}
	obj, err := b.cfg.Evidence.Object(ctx, seed.CaseRef)
	if err != nil {
		return ""
	}
	var od struct {
		Extensions map[string]any `json:"extensions"`
	}
	if err := json.Unmarshal(obj.Payload, &od); err != nil {
		return ""
	}
	sor, _ := od.Extensions["sor"].(string)
	if name, ok := caseSystemDisplayNames[strings.ToLower(sor)]; ok {
		return name
	}
	return sor
}

// buildCaseNoteRequest shapes the note as the ticket.comment request the
// ordinary action path takes (04/08): the case is the single target, the
// promoted case read (case_ref) is the grounding evidence, and the rationale
// says why this write exists.
func buildCaseNoteRequest(ic *aggregate.InvestigationCurrent, body string) (RequestActionBody, error) {
	if ic.Seed == nil || ic.Seed.Type != aggregate.SeedCase {
		return RequestActionBody{}, fmt.Errorf("investigation is not rooted on a case")
	}
	if ic.Seed.CaseRef == "" {
		// The seed flow always promotes the case read (14 §3); a case seed
		// without case_ref would be an unsourced write — refuse rather than
		// invent evidence.
		return RequestActionBody{}, fmt.Errorf("case seed carries no case_ref to ground the note on")
	}
	params, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return RequestActionBody{}, err
	}
	return RequestActionBody{
		ActionType:       "ticket.comment",
		Targets:          []aggregate.TargetSpec{{EntityRef: ic.Seed.CaseRef, ResolvedIdentifier: ic.Seed.CaseID}},
		Parameters:       params,
		EvidenceRefs:     []string{ic.Seed.CaseRef},
		Rationale:        fmt.Sprintf("Record on %s that this investigation was opened in reckon (cases.note_on_seed).", ic.Seed.CaseID),
		InvestigationRef: ic.AggregateID.String(),
	}, nil
}

// requestCaseNote serves POST /api/investigations/{id}/case_note: build the
// note request for this investigation's seed case and hand it to the shared
// action path (Gate 2, record, dispatch-or-wait). 409 when the posture is off
// or the investigation is not case-rooted.
func (b *Backend) requestCaseNote(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Handler == nil || b.cfg.Gate2 == nil || b.cfg.ActionCatalog == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "action layer not configured")
		return
	}
	if b.cfg.CaseNoteOnSeed == config.CaseNoteOff {
		writeJSONError(w, http.StatusConflict, "case notes are disabled by tenant posture (cases.note_on_seed: off)")
		return
	}
	id, ok := investigationSubresourceID(r.URL.Path, "case_note")
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "path must be /investigations/{id}/case_note")
		return
	}
	claims, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth context missing")
		return
	}
	var req CaseNoteRequest
	if !decodeOptionalBody(w, r, &req, maxBodyBytes) {
		return
	}
	ic, err := aggregate.LoadInvestigationCurrent(r.Context(), b.cfg.Handler.DB(), id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "investigation not found")
		return
	}
	text := strings.TrimSpace(req.Body)
	if text == "" {
		text = caseNoteBody(ic.AggregateID.String(), ic.Title, claims.PreferredUsername, commandNow())
	}
	body, err := buildCaseNoteRequest(&ic, text)
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	b.dispatchActionRequest(w, r, actorFromClaims(claims), claims.Subject, body)
}
