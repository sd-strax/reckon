package server

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/authz"
)

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
		{"GET", "/investigations/" + id + "/events", authz.ReadRoles},
		{"GET", "/investigations/" + id + "/calls", authz.ReadRoles},
		{"GET", "/investigations/" + id + "/calls/" + id, authz.ReadRoles},
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
		{"POST", "/knowledge/reindex", authz.AnalystRoles},
		{"GET", "/knowledge/reindex", authz.ReadRoles},
		{"POST", "/knowledge/recall_similar_investigations", authz.ReadRoles},
		{"POST", "/knowledge/prior_investigations", authz.ReadRoles},
		{"POST", "/knowledge/summary_narrative", authz.AnalystRoles},
		{"GET", "/knowledge/case_summaries", authz.ReadRoles},
		{"GET", "/knowledge/case_summaries/" + id, authz.ReadRoles},
		{"GET", "/sops", authz.ReadRoles},
		{"POST", "/sops", authz.AnalystRoles},
		{"POST", "/sops/import", authz.AnalystRoles},
		{"GET", "/sops/" + id + "/sections", authz.ReadRoles},
		{"GET", "/sops/" + id + "/sections/2", authz.ReadRoles},
		{"GET", "/sops/" + id + "/revisions", authz.ReadRoles},
		{"GET", "/knowledge/case_summaries/facets", authz.ReadRoles},
		{"GET", "/sops/" + id + "/revisions/1/sections", authz.ReadRoles},
		{"GET", "/sops/" + id + "/revisions/1/sections/2", authz.ReadRoles},
		{"GET", "/sops/" + id + "/citations", authz.ReadRoles},
		{"GET", "/knowledge/case_summaries/" + id + "/citations", authz.ReadRoles},
		{"POST", "/sops/import_markdown", authz.AnalystRoles},
		{"GET", "/sops/" + id, authz.ReadRoles},
		{"PUT", "/sops/" + id, authz.AnalystRoles},
		{"DELETE", "/sops/" + id, authz.AnalystRoles},
		{"POST", "/admin/demo/seed", authz.AnalystRoles},
	}
	for _, sub := range []string{"hypotheses", "actions", "thread", "pins", "comms", "dependencies", "export.md"} {
		cases = append(cases, rc{"GET", "/investigations/" + id + "/" + sub, authz.ReadRoles})
	}
	for _, sub := range []string{"export", "lifecycle", "rename", "case_note", "dependencies"} {
		cases = append(cases, rc{"POST", "/investigations/" + id + "/" + sub, authz.AnalystRoles})
	}
	for _, verb := range []string{"approve", "reject", "edit", "rerequest", "resolve"} {
		cases = append(cases, rc{"POST", "/actions/" + id + "/" + verb, authz.AnalystRoles})
	}
	for _, verb := range []string{"ack", "done", "snooze"} {
		cases = append(cases, rc{"POST", "/comms/" + id + "/" + verb, authz.AnalystRoles})
	}
	for _, verb := range []string{"attach", "confirm", "waive"} {
		cases = append(cases, rc{"POST", "/dependencies/" + id + "/" + verb, authz.AnalystRoles})
	}
	return cases
}

// TestRouteAuthorization holds who reaches each route: no token is a 401, a
// role outside the route's set a 403, and a role inside it reaches the handler.
// A 404 only counts as reached with a JSON body, since the router's own is
// plain text. Every other method on the path is a 405 naming the allowed ones.
func TestRouteAuthorization(t *testing.T) {
	b := newFakeBackend(t)
	withEnablementConfig(t, b)
	b.cfg.CapabilityResolver, b.cfg.CapabilityCatalog = buildInvokeCapability(t)
	b.cfg.ActionCatalog = action.DefaultActionCatalog()
	b.cfg.ActionResolver = newActionResolver()
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

	// Every route in the table is one of the cases.
	table := http.NewServeMux()
	for _, rt := range b.routes() {
		table.Handle(rt.pattern, http.NotFoundHandler())
	}
	covered := map[string]bool{}

	declared := map[string][]string{}
	for _, c := range routeCases() {
		declared[c.path] = append(declared[c.path], c.method)
		_, pattern := table.Handler(httptest.NewRequest(c.method, c.path, nil))
		covered[pattern] = true
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

	for _, rt := range b.routes() {
		if !covered[rt.pattern] {
			t.Errorf("route %q is not in routeCases", rt.pattern)
		}
	}

	for path, methods := range declared {
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			if slices.Contains(methods, method) {
				continue
			}
			t.Run(method+" "+path, func(t *testing.T) {
				resp, body := do(t, method, path, tokens[authz.RoleAnalyst])
				// A method /sops/{id} takes reads "import" as an id (adr/0227).
				if strings.HasPrefix(path, "/sops/import") && slices.Contains([]string{"GET", "PUT", "DELETE"}, method) {
					if resp.StatusCode != http.StatusBadRequest {
						t.Errorf("status %d; want 400: %s", resp.StatusCode, body)
					}
					return
				}
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

// TestRouter_Unrouted: the mux answers what no route takes, before auth, and a
// trailing slash is a different path. HEAD works wherever GET does (adr/0227).
func TestRouter_Unrouted(t *testing.T) {
	b := newFakeBackend(t)
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	id := uuid.NewString()

	for _, tc := range []struct {
		method, path, token string
		want                int
	}{
		{"GET", "/api/nothing", "", http.StatusNotFound},
		{"GET", "/api/investigations/" + id + "/nothing", "", http.StatusNotFound},
		{"POST", "/api/comms/" + id + "/nothing", "", http.StatusNotFound},
		{"GET", "/api/investigations/", "", http.StatusNotFound},
		{"GET", "/api/investigations/" + id + "/thread/", mintToken(t, nil), http.StatusNotFound},
		{"DELETE", "/api/investigations", "", http.StatusMethodNotAllowed},
		{"HEAD", "/api/investigations", "", http.StatusUnauthorized},
		{"HEAD", "/api/investigations", mintToken(t, nil), http.StatusOK},
		{"GET", "/api/investigations/not-a-uuid", mintToken(t, nil), http.StatusBadRequest},
	} {
		req, _ := http.NewRequest(tc.method, srv.URL+tc.path, nil)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s %s: status %d; want %d", tc.method, tc.path, resp.StatusCode, tc.want)
		}
	}
}

// TestRouter_UnconfiguredLayer: a layer that is not configured answers 503 to a
// caller with the role, whatever the body, and 403 to one without (adr/0227).
func TestRouter_UnconfiguredLayer(t *testing.T) {
	b := newFakeBackend(t)
	b.cfg.Comms = nil
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	viewer := mintToken(t, map[string]any{"realm_access": map[string]any{"roles": []string{"viewer"}}})

	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/comms/inbound"},
		{"POST", "/api/comms/" + uuid.NewString() + "/ack"},
		{"POST", "/api/comms/" + uuid.NewString() + "/snooze"},
		{"GET", "/api/enablement"},
		{"POST", "/api/enablement/adapters/x"},
		{"POST", "/api/capability/x"},
	} {
		for token, want := range map[string]int{mintToken(t, nil): http.StatusServiceUnavailable, viewer: http.StatusForbidden} {
			req, _ := http.NewRequest(tc.method, srv.URL+tc.path, strings.NewReader("not json"))
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != want {
				t.Errorf("%s %s: status %d; want %d", tc.method, tc.path, resp.StatusCode, want)
			}
		}
	}
}

// TestMountRoutes_RefusesARouteWithNoRoles: a route cannot be added open.
func TestMountRoutes_RefusesARouteWithNoRoles(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a route with no roles should panic")
		}
	}()
	noop := func(next http.Handler) http.Handler { return next }
	mountRoutes(http.NewServeMux(), noop, []route{{pattern: "GET /x", handler: func(http.ResponseWriter, *http.Request) {}}})
}

// TestRouteAuthorization_Me: /api/me takes any signed-in person and no one else.
func TestRouteAuthorization_Me(t *testing.T) {
	b := newFakeBackend(t)
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
