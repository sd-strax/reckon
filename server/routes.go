package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/authz"
)

// route is one API endpoint: its method and path (the /api prefix is the
// router's), who may call it, and what serves it (adr/0227). Everything here
// requires a verified token and one of roles; see mountRoutes.
type route struct {
	pattern string
	roles   []string
	handler http.HandlerFunc
}

// routes is every authorized endpoint. The endpoints that name no role
// (/healthz, /status, /metrics, /auth-config, /me) are in buildRouter.
func (b *Backend) routes() []route {
	read, analyst := authz.ReadRoles, authz.AnalystRoles
	rs := []route{
		{"GET /investigations", read, b.listInvestigations},
		{"POST /investigations", analyst, b.createInvestigation},
		{"GET /investigations/{id}", read, withUUID(b.getInvestigation)},
		{"GET /investigations/{id}/hypotheses", read, withUUID(b.listInvestigationHypotheses)},
		{"GET /investigations/{id}/actions", read, withUUID(b.listInvestigationActions)},
		{"GET /investigations/{id}/thread", read, withUUID(b.listInvestigationThread)},
		{"GET /investigations/{id}/pins", read, withUUID(b.listInvestigationPins)},
		{"GET /investigations/{id}/comms", read, withUUID(b.listInvestigationComms)},
		// The live markdown projection (binding §6 item 10), distinct from POST
		// .../export, which builds the signed post-conclusion bundle (D.5).
		{"GET /investigations/{id}/export.md", read, withUUID(b.getInvestigationMarkdown)},
		{"POST /investigations/{id}/export", analyst, withUUID(b.exportInvestigation)},
		{"POST /investigations/{id}/lifecycle", analyst, withUUID(b.investigationLifecycle)},
		{"POST /investigations/{id}/rename", analyst, withUUID(b.renameInvestigation)},
		// The "opened in reckon" note on the seed case (adr/0032).
		{"POST /investigations/{id}/case_note", analyst, withUUID(b.requestCaseNote)},

		// The catalogs (§2.8, 08 §3) and verb invocation (03 §3.4). Invoking is
		// analyst only: it causes outbound tool I/O, unlike the catalog.
		{"GET /capabilities", read, b.listCapabilities},
		{"POST /capability/{verb}", analyst, b.capabilityInvoke},
		{"GET /action-types", read, b.listActionTypes},

		// The case queue, read-only and non-persisting (adr/0030, 14 §4.2).
		{"GET /cases", read, b.listCases},
		{"GET /cases/{number}", read, func(w http.ResponseWriter, r *http.Request) {
			b.getCase(w, r, r.PathValue("number"))
		}},

		// The operator's enablement surface (11 §5.1). The tenant config file
		// stays the authority; the POST refuses AI-delegated tokens.
		{"GET /enablement", analyst, b.getEnablement},
		{"POST /enablement/adapters/{name}", analyst, func(w http.ResponseWriter, r *http.Request) {
			b.applyEnablement(w, r, r.PathValue("name"))
		}},

		// Actions (08 §2, 04 §5). Approving and rejecting are human acts: the
		// endpoint refuses an AI delegate, and so does the aggregate.
		{"POST /actions", analyst, b.requestAction},
		{"GET /actions/{id}", read, withUUID(b.getActionRecord)},
		{"POST /actions/{id}/approve", analyst, withUUID(b.approveAction)},
		{"POST /actions/{id}/reject", analyst, withUUID(b.rejectAction)},
		{"POST /actions/{id}/edit", analyst, withUUID(b.editAction)},
		{"POST /actions/{id}/rerequest", analyst, withUUID(b.rerequestAction)},
		{"POST /actions/{id}/resolve", analyst, withUUID(b.resolveActionOutcome)},

		// The reasoning thread (05 §3.4): the AI authors as a delegate, never a
		// principal. Retraction is annotate-tier, open to both actor kinds (01).
		{"POST /interpretations", analyst, b.recordInterpretation},
		{"POST /interpretations/{id}/supersede", analyst, withUUID(b.supersedeInterpretation)},
		{"GET /interpretations/{id}/transcript", read, withUUID(b.getInterpretationTranscript)},

		// Cross-investigation memory (binding §6.1) and citation-open (design/ui
		// 02 §2.8).
		{"GET /entities/{ref}/appearances", read, b.listRefAppearances},
		{"GET /evidence/{ref}", read, b.getEvidence},

		// Comms acts (binding §4): inbound replies, and the analyst's acts on a
		// thread.
		{"POST /comms/inbound", analyst, b.commsInbound},
		{"POST /comms/{id}/ack", analyst, withUUID(b.commsAck)},
		{"POST /comms/{id}/done", analyst, withUUID(b.commsDone)},
		{"POST /comms/{id}/snooze", analyst, withUUID(b.commsSnooze)},
	}
	if b.cfg.Knowledge == nil {
		return rs
	}
	return append(rs, []route{
		// SOP corpus and retrieval (06), and similarity recall over the
		// case-summaries corpus.
		{"POST /knowledge/recall_sops", read, b.recallSOPs},
		{"POST /knowledge/recall_similar_investigations", read, b.recallSimilar},
		{"POST /knowledge/prior_investigations", read, b.priorInvestigations},
		{"POST /knowledge/summary_narrative", analyst, b.summaryNarrative},
		{"GET /knowledge/case_summaries", read, b.listCaseSummaries},
		{"GET /knowledge/case_summaries/{ref}", read, b.getCaseSummary},
		{"GET /sops", read, b.listSOPs},
		{"POST /sops", analyst, b.createSOP},
		{"POST /sops/import", analyst, b.importSOP},
		{"POST /sops/import_markdown", analyst, b.importMarkdownSOP},
		{"GET /sops/{id}", read, withUUID(b.getSOP)},
		{"PUT /sops/{id}", analyst, withUUID(b.updateSOP)},
		{"DELETE /sops/{id}", analyst, withUUID(b.retireSOP)},
		// `reckon demo seed` loads the demo knowledge pack; the handler refuses
		// a working install.
		{"POST /admin/demo/seed", analyst, b.seedDemoKnowledge},
	}...)
}

// mountRoutes registers each route behind auth and its role check. A route with
// no roles is a mistake, not an open endpoint, so it stops the process at startup.
func mountRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, routes []route) {
	for _, rt := range routes {
		if len(rt.roles) == 0 {
			panic(fmt.Sprintf("route %q names no roles", rt.pattern))
		}
		mux.Handle(rt.pattern, auth(requireRoles(rt.roles, rt.handler)))
	}
}

// requireRoles admits a caller holding any of roles: otherwise a 403 naming
// them, or a 500 if no claims reached it.
func requireRoles(roles []string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authz.FromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "auth wiring error")
			return
		}
		if !claims.HasAnyRole(roles...) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":          "required role(s) missing",
				"required_roles": roles,
				"required_any":   true,
			})
			return
		}
		next(w, r)
	}
}

// withUUID hands next the path's {id}; anything but a UUID there is a 400.
func withUUID(next func(http.ResponseWriter, *http.Request, uuid.UUID)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "id in path is not a valid id")
			return
		}
		next(w, r, id)
	}
}
