package server

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/capability"
)

// The case queue, read-only (adr/0030-cases-pane.md). GET /api/cases lists the
// tenant's queue (policies.case_queue, 14 §4.2) or an ad-hoc filter; GET
// /api/cases/{number} reads one case. Both go through the capability layer
// like any read, but — unlike POST /api/capability/{verb} — they do NOT
// persist what they read: a queue browsed every refresh is not evidence, and
// eager promotion (03 §4.13) exists so a ref the agent CITES stays openable.
// The case is promoted once, when it seeds an investigation (14 §3).
//
// Each row also says whether an investigation already roots on the case
// (seed_type = case, same case_id, not concluded/archived), so the surface
// leads to the existing investigation instead of offering a second one. That
// is a view rule, not an aggregate guard: 14 §6 deliberately keeps two
// investigations on one case legal (a second analyst, a post-reimage
// verification).

// CaseView is one case as the queue and the detail read render it: the
// class-2005 ObservedData's extensions (03 §2.9) flattened, plus the in-flight
// investigation when one exists.
type CaseView struct {
	Number          string     `json:"number"`
	Title           string     `json:"title,omitempty"`
	Status          string     `json:"status,omitempty"`
	Priority        string     `json:"priority,omitempty"`
	AssignmentGroup string     `json:"assignment_group,omitempty"`
	Summary         string     `json:"summary,omitempty"`
	Link            string     `json:"link,omitempty"`
	Source          string     `json:"source,omitempty"` // provenance.tool — the SoR as the binding labels it
	OpenedAt        *time.Time `json:"opened_at,omitempty"`

	// Investigation is the open investigation rooted on this case, if any.
	Investigation *CaseInvestigationRef `json:"investigation,omitempty"`
}

// CaseInvestigationRef points the queue row at the investigation already
// rooted on the case.
type CaseInvestigationRef struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// CaseListResponse is the body of GET /api/cases.
type CaseListResponse struct {
	// Queue is the queue the rows came from: the `queue` query parameter when
	// given, else the tenant's configured one, else "" (an ad-hoc or recent
	// search).
	Queue string     `json:"queue,omitempty"`
	Cases []CaseView `json:"cases"`
}

var caseReadRoles = []string{authz.RoleViewer, authz.RoleAnalyst, authz.RoleAuditor}

// casesRoute serves GET /api/cases and GET /api/cases/{number}.
func (b *Backend) casesRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/cases"), "/")
	if rest == "" {
		b.requireRolesOrDeny(w, r, caseReadRoles, b.listCases)
		return
	}
	if strings.Contains(rest, "/") {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	b.requireRolesOrDeny(w, r, caseReadRoles, func(w http.ResponseWriter, r *http.Request) {
		b.getCase(w, r, rest)
	})
}

// listCases reads the queue: `queue` (default: the tenant policy) and `filter`
// (free text, narrows) as query parameters.
func (b *Backend) listCases(w http.ResponseWriter, r *http.Request) {
	resolver, catalog := b.capabilitySurface()
	if resolver == nil || catalog == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "capability layer not configured")
		return
	}
	if _, known := catalog.Descriptor("query_external_cases"); !known {
		writeJSONError(w, http.StatusServiceUnavailable, "query_external_cases is not available in this tenant")
		return
	}
	q := r.URL.Query()
	queue := strings.TrimSpace(q.Get("queue"))
	if queue == "" {
		queue = resolver.PolicyString(capability.PolicyCaseQueue)
	}
	extra := map[string]any{}
	if f := strings.TrimSpace(q.Get("filter")); f != "" {
		extra["filter"] = f
	}
	if queue != "" {
		extra["queue"] = queue
	}

	res, status, err := b.readCases(r, resolver, "query_external_cases", extra)
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	cases := casesFromResult(res)
	if b.cfg.Handler != nil {
		if err := b.annotateInFlight(r, cases); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "look up investigations: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, CaseListResponse{Queue: queue, Cases: cases})
}

// getCase reads one case by number. 404 when the SoR answers "no such case"
// (COMPLETE and empty), the same distinction the seed flow draws (14 §2).
func (b *Backend) getCase(w http.ResponseWriter, r *http.Request, number string) {
	resolver, catalog := b.capabilitySurface()
	if resolver == nil || catalog == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "capability layer not configured")
		return
	}
	if _, known := catalog.Descriptor("get_external_case_details"); !known {
		writeJSONError(w, http.StatusServiceUnavailable, "get_external_case_details is not available in this tenant")
		return
	}
	res, status, err := b.readCases(r, resolver, "get_external_case_details", map[string]any{"case_id": number})
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	cases := casesFromResult(res)
	if len(cases) == 0 {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("no case matches %q in the system of record", number))
		return
	}
	if b.cfg.Handler != nil {
		if err := b.annotateInFlight(r, cases[:1]); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "look up investigations: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, cases[0])
}

// readCases runs one case verb WITHOUT persisting and maps the coverage to an
// HTTP status for the not-executed cases: 503 when no SoR is configured, 502
// when it could not be fully read or the adapter failed.
func (b *Backend) readCases(r *http.Request, resolver *capability.Resolver, verb string, extra map[string]any) (capability.CapabilityResult, int, error) {
	res, err := resolver.Resolve(r.Context(), verb, capability.CallInput{Extra: extra})
	if err != nil {
		var ae *capability.AdapterError
		if errors.As(err, &ae) {
			return res, http.StatusBadGateway, fmt.Errorf("%s: %w", verb, err)
		}
		return res, http.StatusInternalServerError, fmt.Errorf("%s: %w", verb, err)
	}
	switch res.Coverage {
	case capability.CoverageComplete:
		return res, http.StatusOK, nil
	case capability.CoverageUnavailableTenant:
		return res, http.StatusServiceUnavailable, errors.New("no case system of record is configured for this tenant")
	default:
		return res, http.StatusBadGateway, fmt.Errorf("case system of record unavailable (%s)", res.Coverage)
	}
}

// casesFromResult flattens the class-2005 ObservedData the caseNormalizer
// produced (03 §2.9) into CaseViews, in result order.
func casesFromResult(res capability.CapabilityResult) []CaseView {
	out := []CaseView{}
	str := func(ext map[string]any, k string) string {
		s, _ := ext[k].(string)
		return s
	}
	for _, n := range res.Normalized {
		for _, od := range n.ObservedData {
			num := str(od.Extensions, "case_number")
			if num == "" {
				continue
			}
			cv := CaseView{
				Number:          num,
				Title:           str(od.Extensions, "title"),
				Status:          str(od.Extensions, "status"),
				Priority:        str(od.Extensions, "priority"),
				AssignmentGroup: str(od.Extensions, "assignment_group"),
				Summary:         str(od.Extensions, "summary"),
				Link:            str(od.Extensions, "link"),
				Source:          od.Provenance.Tool,
			}
			if !od.FirstObserved.IsZero() {
				t := od.FirstObserved
				cv.OpenedAt = &t
			}
			out = append(out, cv)
		}
	}
	return out
}

// annotateInFlight attaches, to each case, the most recently updated
// investigation rooted on it that is neither concluded nor archived.
func (b *Backend) annotateInFlight(r *http.Request, cases []CaseView) error {
	if len(cases) == 0 {
		return nil
	}
	numbers := make([]string, 0, len(cases))
	for _, c := range cases {
		numbers = append(numbers, c.Number)
	}
	// Every investigation of the case counts (#158): a concluded one still
	// answers it — reopening it is the way back, not a second investigation.
	// An open one wins over a concluded one; newest first within each.
	rows, err := b.cfg.Handler.DB().QueryContext(r.Context(),
		`SELECT aggregate_id, title, status, seed->>'case_id'
		   FROM investigation_current
		  WHERE seed_type = $1
		    AND seed->>'case_id' = ANY($2)
		  ORDER BY (status IN ($3, $4)), updated_at DESC`,
		aggregate.SeedCase, pq.Array(numbers), aggregate.StatusConcluded, aggregate.StatusArchived)
	if err != nil {
		return err
	}
	defer rows.Close()
	byCase := map[string]*CaseInvestigationRef{}
	for rows.Next() {
		var ref CaseInvestigationRef
		var caseID string
		if err := rows.Scan(&ref.ID, &ref.Title, &ref.Status, &caseID); err != nil {
			return err
		}
		if _, seen := byCase[caseID]; !seen { // newest wins (ORDER BY updated_at DESC)
			r := ref
			byCase[caseID] = &r
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range cases {
		cases[i].Investigation = byCase[cases[i].Number]
	}
	return nil
}

// investigationForCase returns the investigation already rooted on a case —
// open first, else the most recent concluded or archived — or "" (#158).
func (b *Backend) investigationForCase(r *http.Request, caseID string) (string, error) {
	if caseID == "" || b.cfg.Handler == nil {
		return "", nil
	}
	var id string
	err := b.cfg.Handler.DB().QueryRowContext(r.Context(),
		`SELECT aggregate_id FROM investigation_current
		  WHERE seed_type = $1 AND seed->>'case_id' = $2
		  ORDER BY (status IN ($3, $4)), updated_at DESC
		  LIMIT 1`,
		aggregate.SeedCase, caseID, aggregate.StatusConcluded, aggregate.StatusArchived).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
