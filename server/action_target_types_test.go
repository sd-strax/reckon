package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/module"
)

// TestRequestAction_WrongTargetTypeRefused: an entity action requested on a
// target of the wrong type is refused with a 400, and nothing is recorded
// (adr/0354).
func TestRequestAction_WrongTargetTypeRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t)

	for _, target := range []aggregate.TargetSpec{
		{EntityRef: "user-account--1", ResolvedIdentifier: "svc_backup"},
		{ResolvedIdentifier: "WIN-A"},
	} {
		resp, _ := postAction(t, b, mintToken(t, nil), RequestActionBody{
			ActionType:       "host.isolate",
			Targets:          []aggregate.TargetSpec{target},
			Rationale:        "contain",
			EvidenceRefs:     []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"},
			InvestigationRef: invID.String(),
		})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("host.isolate on %+v: status %d; want 400", target, resp.StatusCode)
		}
	}
	acts, err := testHandler.Reads().ListActionCurrents(context.Background(), invID)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 0 {
		t.Errorf("a refused request recorded %d actions; want none", len(acts))
	}
}

// TestRequestAction_ReversalTakesOriginalTargets: a reversal acts on the
// original action's targets as recorded. Body targets that carry names only,
// as the workbench's Reverse sends them, are not used (adr/0354).
func TestRequestAction_ReversalTakesOriginalTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	origID := succeededAction(t, invID) // host.isolate on reckon-host--1 / WIN-A
	b := actionBackend(t)

	resp, out := postAction(t, b, mintToken(t, nil), RequestActionBody{
		ActionType:       "host.unisolate",
		Targets:          []aggregate.TargetSpec{{ResolvedIdentifier: "something-else"}},
		Rationale:        "undo",
		InvestigationRef: invID.String(),
		ReversalOfRef:    origID.String(),
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reversal with name-only body targets: status %d; want 201", resp.StatusCode)
	}
	rev, err := testHandler.Reads().LoadActionCurrent(context.Background(), uuid.MustParse(out.ActionID))
	if err != nil {
		t.Fatal(err)
	}
	want := aggregate.TargetSpec{EntityRef: "reckon-host--1", ResolvedIdentifier: "WIN-A"}
	if len(rev.Targets) != 1 || rev.Targets[0] != want {
		t.Errorf("reversal targets = %+v; want the original's [%+v]", rev.Targets, want)
	}
}

// TestRerequestAction_UntypedTargetRefused: a re-request copies the stored
// targets, so re-requesting an action stored with no entity_ref is refused.
// The analyst makes a new request (adr/0354).
func TestRerequestAction_UntypedTargetRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	invID := activeInvestigation(t)
	b := actionBackend(t)

	// Record the action directly, bypassing the request check, then expire it.
	ctx := context.Background()
	origID := uuid.New()
	env := func(kind string) aggregate.Envelope {
		return aggregate.Envelope{
			AggregateID: invID, TenantID: module.SingleTenantUUID, CorrelationID: uuid.New(),
			Actor: aggregate.Actor{PrincipalID: "test-subject", Kind: kind}, OccurredAt: time.Now().UTC(),
		}
	}
	if _, err := testHandler.Handle(ctx, env(""), aggregate.RequestAction{
		ActionID: origID, ActionType: "host.isolate", Tier: aggregate.TierT2,
		Targets:      []aggregate.TargetSpec{{ResolvedIdentifier: "WIN-A"}},
		EvidenceRefs: []string{"observed-data--18ef927c-93bc-5470-b729-1b40841d3947"},
		ExpiresAt:    time.Now().Add(time.Hour), Rationale: "contain",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.Handle(ctx, env(aggregate.ActorSystem), aggregate.ExpireAction{ActionID: origID}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	raw, _ := json.Marshal(RerequestBody{Rationale: "still warranted"})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/actions/"+origID.String()+"/rerequest", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+mintToken(t, nil))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("re-request of an action with no entity_ref: status %d; want 400", resp.StatusCode)
	}
}
