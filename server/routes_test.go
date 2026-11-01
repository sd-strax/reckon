package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/comms"
	"github.com/sd-strax/reckon/knowledge"
	"github.com/sd-strax/reckon/knowledge/substrate"
)

// errNoCorpus answers every substrate call. A route only has to reach its
// handler here; what the corpus holds is the knowledge tests' concern.
var errNoCorpus = errors.New("no corpus in routing tests")

type noCorpus struct{}

func (noCorpus) Put(context.Context, string, string, substrate.Entry) (substrate.Entry, error) {
	return substrate.Entry{}, errNoCorpus
}
func (noCorpus) Revise(context.Context, string, string, uuid.UUID, substrate.Revision) (substrate.Entry, error) {
	return substrate.Entry{}, errNoCorpus
}
func (noCorpus) Transition(context.Context, string, string, uuid.UUID, substrate.Transition) error {
	return errNoCorpus
}
func (noCorpus) Purge(context.Context, string, string, uuid.UUID) error { return errNoCorpus }
func (noCorpus) Get(context.Context, string, string, uuid.UUID) (substrate.Entry, error) {
	return substrate.Entry{}, errNoCorpus
}
func (noCorpus) List(context.Context, string, string, substrate.ListFilter) ([]substrate.Entry, error) {
	return nil, errNoCorpus
}
func (noCorpus) Recall(context.Context, string, substrate.RecallQuery) (substrate.RecallResult, error) {
	return substrate.RecallResult{}, errNoCorpus
}
func (noCorpus) Snapshot(context.Context, string, substrate.ContentHash) (substrate.Snapshot, error) {
	return substrate.Snapshot{}, errNoCorpus
}

// routeCases is every authorized route, written out independently of the
// router so a route that drops its role check, or drops out, fails here.
func routeCases() []struct {
	method, path string
	roles        []string
} {
	id := uuid.NewString()
	type rc = struct {
		method, path string
		roles        []string
	}
	cases := []rc{
		{"GET", "/investigations", authz.ReadRoles},
		{"POST", "/investigations", authz.AnalystRoles},
		{"GET", "/investigations/" + id, authz.ReadRoles},
		{"GET", "/capabilities", authz.ReadRoles},
		{"GET", "/cases", authz.ReadRoles},
		{"GET", "/cases/42", authz.ReadRoles},
		{"POST", "/capability/enumerate_logons", authz.AnalystRoles},
		{"GET", "/enablement", authz.AnalystRoles},
		{"POST", "/enablement/adapters/fixture_context", authz.AnalystRoles},
		{"GET", "/action-types", authz.ReadRoles},
		{"POST", "/actions", authz.AnalystRoles},
		{"GET", "/actions/" + id, authz.ReadRoles},
		{"POST", "/interpretations", authz.AnalystRoles},
		{"POST", "/interpretations/" + id + "/supersede", authz.AnalystRoles},
		{"GET", "/interpretations/" + id + "/transcript", authz.ReadRoles},
		{"GET", "/entities/indicator--" + id + "/appearances", authz.ReadRoles},
		{"GET", "/evidence/indicator--" + id, authz.ReadRoles},
		{"POST", "/comms/inbound", authz.AnalystRoles},
		{"POST", "/knowledge/recall_sops", authz.ReadRoles},
		{"POST", "/knowledge/recall_similar_investigations", authz.ReadRoles},
		{"POST", "/knowledge/prior_investigations", authz.ReadRoles},
		{"POST", "/knowledge/summary_narrative", authz.AnalystRoles},
		{"GET", "/knowledge/case_summaries", authz.ReadRoles},
		{"GET", "/knowledge/case_summaries/" + id, authz.ReadRoles},
		{"GET", "/sops", authz.ReadRoles},
		{"POST", "/sops", authz.AnalystRoles},
		{"POST", "/sops/import", authz.AnalystRoles},
		{"POST", "/sops/import_markdown", authz.AnalystRoles},
		{"GET", "/sops/" + id, authz.ReadRoles},
		{"PUT", "/sops/" + id, authz.AnalystRoles},
		{"DELETE", "/sops/" + id, authz.AnalystRoles},
		{"POST", "/admin/demo/seed", authz.AnalystRoles},
	}
	for _, sub := range []string{"hypotheses", "actions", "thread", "pins", "comms", "export.md"} {
		cases = append(cases, rc{"GET", "/investigations/" + id + "/" + sub, authz.ReadRoles})
	}
	for _, sub := range []string{"export", "lifecycle", "rename", "case_note"} {
		cases = append(cases, rc{"POST", "/investigations/" + id + "/" + sub, authz.AnalystRoles})
	}
	for _, verb := range []string{"approve", "reject", "edit", "rerequest", "resolve"} {
		cases = append(cases, rc{"POST", "/actions/" + id + "/" + verb, authz.AnalystRoles})
	}
	for _, verb := range []string{"ack", "done", "snooze"} {
		cases = append(cases, rc{"POST", "/comms/" + id + "/" + verb, authz.AnalystRoles})
	}
	return cases
}

// TestRouteAuthorization holds who reaches each route: no token is a 401, a
// role outside the route's set a 403, and a role inside it reaches the handler.
// A 404 only counts as reached with a JSON body, since the router's own is
// plain text. Every other method on the path is a 405 naming the allowed ones.
func TestRouteAuthorization(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	b, _ := enablementBackend(t)
	b.cfg.CapabilityResolver, b.cfg.CapabilityCatalog = buildInvokeCapability(t)
	b.cfg.ActionCatalog = action.DefaultActionCatalog()
	b.cfg.ActionResolver = newActionResolver()
	b.cfg.Comms = comms.NewStore(testDB)
	b.cfg.Knowledge = knowledge.NewStore(noCorpus{})
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)

	tokens := map[string]string{}
	for _, role := range authz.AllRoles {
		tokens[role] = mintToken(t, map[string]any{"realm_access": map[string]any{"roles": []string{role}}})
	}
	do := func(t *testing.T, method, path, token string) (*http.Response, string) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+"/api"+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp, readBody(resp)
	}

	declared := map[string][]string{}
	for _, c := range routeCases() {
		declared[c.path] = append(declared[c.path], c.method)
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			if resp, _ := do(t, c.method, c.path, ""); resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("no token: status %d; want 401", resp.StatusCode)
			}
			for _, role := range authz.AllRoles {
				resp, body := do(t, c.method, c.path, tokens[role])
				if !slices.Contains(c.roles, role) {
					if resp.StatusCode != http.StatusForbidden {
						t.Errorf("%s: status %d; want 403: %s", role, resp.StatusCode, body)
					}
					continue
				}
				switch resp.StatusCode {
				case http.StatusUnauthorized, http.StatusForbidden, http.StatusMethodNotAllowed:
					t.Errorf("%s: status %d did not reach the handler: %s", role, resp.StatusCode, body)
				case http.StatusNotFound:
					if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
						t.Errorf("%s: unrouted: %s", role, body)
					}
				}
			}
		})
	}

	for path, methods := range declared {
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			if slices.Contains(methods, method) {
				continue
			}
			t.Run(method+" "+path, func(t *testing.T) {
				resp, body := do(t, method, path, tokens[authz.RoleAnalyst])
				if resp.StatusCode != http.StatusMethodNotAllowed {
					t.Fatalf("status %d; want 405: %s", resp.StatusCode, body)
				}
				allow := resp.Header.Get("Allow")
				for _, m := range methods {
					if !strings.Contains(allow, m) {
						t.Errorf("Allow %q is missing %s", allow, m)
					}
				}
			})
		}
	}
}

// TestRouteAuthorization_Me: /api/me takes any signed-in person and no one else.
func TestRouteAuthorization_Me(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	b := newTestBackend(t)
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)

	for name, want := range map[string]int{"": http.StatusUnauthorized, "no roles": http.StatusOK} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/me", nil)
		if name != "" {
			req.Header.Set("Authorization", "Bearer "+mintToken(t, map[string]any{"realm_access": map[string]any{"roles": []string{}}}))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%q: status %d; want %d", name, resp.StatusCode, want)
		}
	}
}
