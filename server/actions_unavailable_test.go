package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/module"
)

// healthWriteAdapter is a write adapter whose health is fixed; it never
// dispatches in these tests. healthCalls, when set, counts its health checks.
type healthWriteAdapter struct {
	name        string
	healthy     bool
	healthCalls *int
}

func (a healthWriteAdapter) Name() string                   { return a.name }
func (a healthWriteAdapter) Class() capability.AdapterClass { return capability.ClassCustom }
func (a healthWriteAdapter) SupportedActionOps() []string   { return []string{"op"} }
func (a healthWriteAdapter) Health() capability.HealthStatus {
	if a.healthCalls != nil {
		*a.healthCalls++
	}
	return capability.HealthStatus{Healthy: a.healthy}
}
func (a healthWriteAdapter) Dispatch(context.Context, string, map[string]any, string) (action.WriteResult, error) {
	return action.WriteResult{}, nil
}

// bindingResolver binds host.isolate to a healthy adapter (available),
// account.disable to an unhealthy one (degraded), ticket.comment to an
// adapter that is not enabled (unavailable), and email.quarantine only for
// the org-b scope. Every other type, the reversal inverse host.unisolate
// among them, has no binding (unavailable). healthCalls, when set, counts
// the adapters' health checks.
func bindingResolver(healthCalls *int) *action.ActionResolver {
	return action.NewActionResolver(map[string][]action.ActionBinding{
		"host.isolate":     {{ActionType: "host.isolate", Adapter: "up", Operation: "op"}},
		"account.disable":  {{ActionType: "account.disable", Adapter: "down", Operation: "op"}},
		"ticket.comment":   {{ActionType: "ticket.comment", Adapter: "disabled", Operation: "op"}},
		"email.quarantine": {{ActionType: "email.quarantine", Adapter: "up", Operation: "op", Scope: "org-b"}},
	}, map[string]action.WriteAdapter{
		"up":   healthWriteAdapter{name: "up", healthy: true, healthCalls: healthCalls},
		"down": healthWriteAdapter{name: "down", healthy: false, healthCalls: healthCalls},
	})
}

// postRaw posts a request body to path and returns the status and body.
func postRaw(t *testing.T, b *Backend, path string, body any) (int, string) {
	t.Helper()
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	defer srv.Close()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+mintToken(t, nil))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

// TestRequestAction_UnavailableRefused: a request for an action type with no
// usable binding (none at all, or only one whose adapter is not enabled) is
// refused with 422, naming the type, before anything is built or recorded
// (adr/0380). The check asks no adapter for its health. The backend here has
// a handler that would fail on any use, so a 422 proves nothing reached it.
func TestRequestAction_UnavailableRefused(t *testing.T) {
	g, err := action.NewGate2(nil)
	if err != nil {
		t.Fatal(err)
	}
	healthCalls := 0
	b := &Backend{
		cfg: BackendConfig{
			Handler:        new(aggregate.Handler),
			Gate2:          g,
			ActionCatalog:  defaultActionCatalog(t),
			ActionResolver: bindingResolver(&healthCalls),
		},
		verifier: testVerifier(t),
	}
	targets := []aggregate.TargetSpec{{EntityRef: "reckon-host--1", ResolvedIdentifier: "WIN-A"}}
	for _, actionType := range []string{"ioc.block", "ticket.comment"} {
		status, out := postRaw(t, b, "/api/actions", RequestActionBody{ActionType: actionType, Targets: targets, Rationale: "r",
			EvidenceRefs: []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"}, InvestigationRef: uuid.NewString()})
		if status != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d (%s); want 422", actionType, status, out)
		}
		if want := unavailableActionMessage(actionType); !strings.Contains(out, want) {
			t.Errorf("%s: body %s; want %q", actionType, out, want)
		}
	}
	if healthCalls != 0 {
		t.Errorf("the check asked for adapter health %d times; want none", healthCalls)
	}
}

// TestRequestAction_AvailableAndDegradedAccepted: an available and a degraded
// type are recorded as before; an unavailable one is refused and nothing is
// recorded for it.
func TestRequestAction_AvailableAndDegradedAccepted(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t)
	b.cfg.ActionResolver = bindingResolver(nil)
	for _, c := range []struct {
		actionType string
		target     aggregate.TargetSpec
		want       int
	}{
		{"host.isolate", aggregate.TargetSpec{EntityRef: "reckon-host--1", ResolvedIdentifier: "WIN-A"}, http.StatusCreated},
		{"account.disable", aggregate.TargetSpec{EntityRef: "user-account--1", ResolvedIdentifier: "CONTOSO\\a"}, http.StatusCreated},
		{"ioc.block", aggregate.TargetSpec{EntityRef: "ipv4-addr--1", ResolvedIdentifier: "203.0.113.7"}, http.StatusUnprocessableEntity},
	} {
		resp, _ := postAction(t, b, mintToken(t, nil), RequestActionBody{
			ActionType: c.actionType, Targets: []aggregate.TargetSpec{c.target}, Rationale: "r",
			EvidenceRefs: []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"}, InvestigationRef: invID.String(),
		})
		if resp.StatusCode != c.want {
			t.Errorf("%s: status %d; want %d", c.actionType, resp.StatusCode, c.want)
		}
	}
	acts, err := testHandler.Reads().ListActionCurrents(context.Background(), invID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range acts {
		if a.ActionType == "ioc.block" {
			t.Errorf("the refused ioc.block was recorded: %+v", a)
		}
	}
	if len(acts) != 2 {
		t.Errorf("%d actions recorded; want the available and the degraded one", len(acts))
	}
}

// TestRerequestAction_UnavailableRefused: re-requesting an expired action
// whose type has since lost its binding is refused with 422.
func TestRerequestAction_UnavailableRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t)
	_, orig := postAction(t, b, mintToken(t, nil), RequestActionBody{
		ActionType:       "ioc.block",
		Targets:          []aggregate.TargetSpec{{EntityRef: "ipv4-addr--1", ResolvedIdentifier: "203.0.113.7"}},
		EvidenceRefs:     []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"},
		Rationale:        "block",
		InvestigationRef: invID.String(),
	})
	origID := uuid.MustParse(orig.ActionID)
	expEnv := aggregate.Envelope{
		AggregateID: invID, TenantID: module.SingleTenantUUID, CorrelationID: uuid.New(),
		Actor: aggregate.Actor{PrincipalID: "test-subject", Kind: aggregate.ActorSystem}, OccurredAt: time.Now().UTC(),
	}
	if _, err := testHandler.Handle(context.Background(), expEnv, aggregate.ExpireAction{ActionID: origID}); err != nil {
		t.Fatalf("expire: %v", err)
	}
	b.cfg.ActionResolver = bindingResolver(nil) // ioc.block now has no binding
	status, out := postRaw(t, b, "/api/actions/"+origID.String()+"/rerequest", RerequestBody{Rationale: "again"})
	if status != http.StatusUnprocessableEntity || !strings.Contains(out, unavailableActionMessage("ioc.block")) {
		t.Errorf("rerequest: status %d (%s); want 422 naming ioc.block", status, out)
	}
}

// TestRequestAction_UnavailableAfterReversalChecks: the unavailable check runs
// after the reversal checks, so a reversal of a FAILED original whose inverse
// type is unbound is refused for the original's status, not as unavailable.
func TestRequestAction_UnavailableAfterReversalChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	origID := resultedAction(t, invID, "FAILED")
	b := actionBackend(t)
	b.cfg.ActionResolver = bindingResolver(nil) // host.unisolate has no binding
	status, out := postRaw(t, b, "/api/actions", RequestActionBody{
		ActionType: "host.unisolate", Targets: []aggregate.TargetSpec{{EntityRef: "reckon-host--1", ResolvedIdentifier: "WIN-A"}},
		Rationale: "undo", InvestigationRef: invID.String(), ReversalOfRef: origID.String(),
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(out, "only a SUCCEEDED action can be reversed") {
		t.Errorf("status %d (%s); want 422 for the original's status", status, out)
	}
	if strings.Contains(out, "unavailable") {
		t.Errorf("refused as unavailable before the reversal checks: %s", out)
	}
}

// TestRequestAction_ScopeMismatchRefused: a binding of another organization's
// scope is not usable for this investigation's request, so the type is
// refused for it.
func TestRequestAction_ScopeMismatchRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t) // its seed has no source scope
	b := actionBackend(t)
	b.cfg.ActionResolver = bindingResolver(nil) // email.quarantine is bound for org-b only
	status, out := postRaw(t, b, "/api/actions", RequestActionBody{
		ActionType: "email.quarantine", Targets: []aggregate.TargetSpec{{EntityRef: "email-message--1", ResolvedIdentifier: "<m@x>"}},
		Rationale: "r", EvidenceRefs: []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"}, InvestigationRef: invID.String(),
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(out, unavailableActionMessage("email.quarantine")) {
		t.Errorf("status %d (%s); want 422 as unavailable for this scope", status, out)
	}
}
