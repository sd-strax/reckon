package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/testsuite"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/dependency"
	"github.com/sd-strax/reckon/module"
	"github.com/sd-strax/reckon/temporal"
)

// TestDependencies_Lifecycle walks a dependency through the engine (adr/0101):
// created OPEN over HTTP by a person (an AI delegate is refused, so is an
// unknown investigation), attached to a pending notify.* action, moved to SENT
// when that action SUCCEEDS — by the result activity, on a retry as well as
// on the attempt that committed the result, and by the routes for an
// attachment that landed under a completing send — then confirmed by the
// analyst with a note, after which it is final. A waive needs a note; a
// dependency rides only on a notify.* action of its own investigation.
func TestDependencies_Lifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	if _, err := testDB.Exec(`TRUNCATE dependencies`); err != nil {
		t.Fatalf("truncate dependencies: %v", err)
	}
	invID := activeInvestigation(t)
	store := dependency.NewStore(testDB)
	acts := temporal.NewActivities(testHandler, nil, nil, store)

	b := newTestBackend(t)
	b.cfg.Dependencies = store
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	token := mintToken(t, nil)
	doAs := func(token, method, path string, body any) (int, []byte) {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out bytes.Buffer
		_, _ = out.ReadFrom(resp.Body)
		return resp.StatusCode, out.Bytes()
	}
	do := func(method, path string, body any) (int, []byte) {
		t.Helper()
		return doAs(token, method, path, body)
	}
	env := func() aggregate.Envelope {
		return newEnvelope(invID, aggregate.Actor{PrincipalID: "test-subject"}, commandNow())
	}

	// A notify action, requested but not yet sent.
	notifyID := uuid.New()
	if _, err := testHandler.Handle(context.Background(), env(), aggregate.RequestAction{
		ActionID: notifyID, ActionType: "notify.slack", Tier: aggregate.TierT2,
		Targets:    []aggregate.TargetSpec{{ResolvedIdentifier: "#platform"}},
		Parameters: json.RawMessage(`{"message":"Please rotate svc_backup and re-image WIN-FILE01"}`),
		ExpiresAt:  time.Now().Add(time.Hour), Rationale: "coordinate",
	}); err != nil {
		t.Fatalf("request notify: %v", err)
	}
	// An action that is not a notification.
	isolateID := uuid.New()
	if _, err := testHandler.Handle(context.Background(), env(), aggregate.RequestAction{
		ActionID: isolateID, ActionType: "host.isolate", Tier: aggregate.TierT2,
		Targets:   []aggregate.TargetSpec{{EntityRef: "x-host--1", ResolvedIdentifier: "WIN-FILE01"}},
		ExpiresAt: time.Now().Add(time.Hour), Rationale: "contain",
	}); err != nil {
		t.Fatalf("request isolate: %v", err)
	}

	// 1. Create: a person's act on a known investigation; title and owner
	// required; created OPEN, unattached.
	delegate := mintToken(t, map[string]any{"delegate_kind": "claude"})
	if code, _ := doAs(delegate, "POST", "/api/investigations/"+invID.String()+"/dependencies", CreateDependencyBody{Title: "rotate svc_backup", Owner: "platform team"}); code != http.StatusForbidden {
		t.Errorf("create as a delegate = %d; want 403", code)
	}
	if code, _ := do("POST", "/api/investigations/"+uuid.NewString()+"/dependencies", CreateDependencyBody{Title: "rotate svc_backup", Owner: "platform team"}); code != http.StatusNotFound {
		t.Errorf("create on an unknown investigation = %d; want 404", code)
	}
	if code, _ := do("POST", "/api/investigations/"+invID.String()+"/dependencies", CreateDependencyBody{Title: "rotate svc_backup"}); code != http.StatusBadRequest {
		t.Errorf("create without owner = %d; want 400", code)
	}
	code, raw := do("POST", "/api/investigations/"+invID.String()+"/dependencies", CreateDependencyBody{
		Title: "rotate svc_backup", Owner: "platform team", Detail: "the secret was exposed",
	})
	if code != http.StatusCreated {
		t.Fatalf("create = %d: %s", code, raw)
	}
	var rotate DependencyView
	_ = json.Unmarshal(raw, &rotate)
	if rotate.Status != dependency.StatusOpen || rotate.CreatedBy != "test-subject" || rotate.NotifyActionID != "" || len(rotate.History) != 1 {
		t.Fatalf("created = %+v; want OPEN by test-subject, unattached, one history entry", rotate)
	}
	// A second one, created already riding on the notify action.
	code, raw = do("POST", "/api/investigations/"+invID.String()+"/dependencies", CreateDependencyBody{
		Title: "re-image WIN-FILE01", Owner: "desktop team", NotifyActionID: notifyID.String(),
	})
	if code != http.StatusCreated {
		t.Fatalf("create attached = %d: %s", code, raw)
	}
	var reimage DependencyView
	_ = json.Unmarshal(raw, &reimage)
	if reimage.NotifyActionID != notifyID.String() {
		t.Fatalf("created attached = %+v; want riding on %s", reimage, notifyID)
	}

	// 2. Attach: a notify.* action of this investigation, pending; nothing else.
	if code, body := do("POST", "/api/dependencies/"+rotate.DependencyID+"/attach", AttachDependencyBody{NotifyActionID: isolateID.String()}); code != http.StatusUnprocessableEntity {
		t.Errorf("attach to host.isolate = %d: %s; want 422", code, body)
	}
	if code, body := do("POST", "/api/dependencies/"+rotate.DependencyID+"/attach", AttachDependencyBody{NotifyActionID: uuid.New().String()}); code != http.StatusUnprocessableEntity {
		t.Errorf("attach to an unknown action = %d: %s; want 422", code, body)
	}
	code, raw = do("POST", "/api/dependencies/"+rotate.DependencyID+"/attach", AttachDependencyBody{NotifyActionID: notifyID.String()})
	if code != http.StatusOK {
		t.Fatalf("attach = %d: %s", code, raw)
	}
	_ = json.Unmarshal(raw, &rotate)
	if rotate.NotifyActionID != notifyID.String() || rotate.Status != dependency.StatusOpen {
		t.Fatalf("attached = %+v; want OPEN riding on %s", rotate, notifyID)
	}

	// 3. The send: approved, dispatched, SUCCEEDED. The first result attempt
	// is one whose worker stopped before the dependencies moved (no store);
	// the retry finds the result on the stream, appends nothing, and still
	// moves both to SENT. A further repeat moves nothing more.
	if _, err := testHandler.Handle(context.Background(), env(), aggregate.ApproveAction{
		ActionID: notifyID, Authorization: aggregate.Authorization{Mode: aggregate.AuthModeManual, Stage: aggregate.AuthStageSolo, PrimaryApproverRef: "test-subject", PrimaryApprovedAt: time.Now()},
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := acts.EmitDispatched(context.Background(), temporal.EmitDispatchedInput{
		ActionID: notifyID.String(), AggregateID: invID.String(), TenantID: module.SingleTenantUUID.String(),
		ApproverID: "test-subject", AdapterRequestID: "wf-" + notifyID.String(),
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	resulted := temporal.EmitResultedInput{
		ActionID: notifyID.String(), AggregateID: invID.String(), TenantID: module.SingleTenantUUID.String(),
		ApproverID: "test-subject", FinalOutcome: "SUCCEEDED", PerTargetResults: map[string]string{"0": "OK"}, Attempts: 1,
	}
	// Both attempts run under one activity identity, as a retry does, so the
	// aggregate recognises the second as already applied and appends nothing.
	ts := &testsuite.WorkflowTestSuite{}
	stoppedShort := temporal.NewActivities(testHandler, nil, nil, nil)
	first := ts.NewTestActivityEnvironment()
	first.RegisterActivity(stoppedShort)
	if _, err := first.ExecuteActivity(stoppedShort.EmitResulted, resulted); err != nil {
		t.Fatalf("result: %v", err)
	}
	deps, err := store.List(context.Background(), invID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 || deps[0].Status != dependency.StatusOpen || deps[1].Status != dependency.StatusOpen {
		t.Fatalf("after a result attempt that stopped short: %+v; want both still OPEN", deps)
	}
	retry := ts.NewTestActivityEnvironment()
	retry.RegisterActivity(acts)
	if _, err := retry.ExecuteActivity(acts.EmitResulted, resulted); err != nil {
		t.Fatalf("result retry: %v", err)
	}
	deps, err = store.List(context.Background(), invID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 || deps[0].Status != dependency.StatusSent || deps[1].Status != dependency.StatusSent {
		t.Fatalf("after the retry: %+v; want both SENT", deps)
	}
	if n, err := store.MarkSent(context.Background(), notifyID, time.Now()); err != nil || n != 0 {
		t.Errorf("second MarkSent = %d, %v; want 0 rows (idempotent)", n, err)
	}
	// An attachment that landed as the send completed: the route's pending
	// check passed, the result activity ran before the row existed. The
	// post-write settle moves it.
	raced, err := store.Create(context.Background(), dependency.Create{
		AggregateID: invID, TenantID: module.SingleTenantUUID, Title: "revoke the session", Owner: "identity team",
		NotifyActionID: notifyID, By: dependency.Actor{ID: "test-subject"}, At: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !b.settleSentUnder(context.Background(), notifyID) {
		t.Error("settle under a SUCCEEDED send moved nothing")
	}
	if got, _ := store.Get(context.Background(), raced.ID); got.Status != dependency.StatusSent {
		t.Errorf("raced attachment = %s; want SENT", got.Status)
	}
	if err := store.Waive(context.Background(), raced.ID, dependency.Actor{ID: "test-subject"}, "out of scope", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if code, _ := do("POST", "/api/dependencies/"+rotate.DependencyID+"/attach", AttachDependencyBody{NotifyActionID: notifyID.String()}); code != http.StatusUnprocessableEntity {
		t.Errorf("attach to a SUCCEEDED send = %d; want 422", code)
	}

	// 4. Resolve: a person's act; a waive needs a note; confirm carries the
	// note and the reference; resolved is final.
	if code, body := doAs(delegate, "POST", "/api/dependencies/"+rotate.DependencyID+"/confirm", ResolveDependencyBody{Note: "done"}); code != http.StatusForbidden {
		t.Errorf("confirm as a delegate = %d: %s; want 403", code, body)
	}
	if code, body := do("POST", "/api/dependencies/"+reimage.DependencyID+"/waive", ResolveDependencyBody{}); code != http.StatusUnprocessableEntity {
		t.Errorf("waive without a note = %d: %s; want 422", code, body)
	}
	code, raw = do("POST", "/api/dependencies/"+rotate.DependencyID+"/confirm", ResolveDependencyBody{Note: "confirmed by the platform lead", ExternalRef: "OPS-4411"})
	if code != http.StatusOK {
		t.Fatalf("confirm = %d: %s", code, raw)
	}
	_ = json.Unmarshal(raw, &rotate)
	if rotate.Status != dependency.StatusConfirmed || rotate.ResolvedBy != "test-subject" || rotate.ResolutionNote != "confirmed by the platform lead" || rotate.ExternalRef != "OPS-4411" || rotate.ResolvedAt == nil {
		t.Fatalf("confirmed = %+v; want CONFIRMED by test-subject with note and reference", rotate)
	}
	if code, body := do("POST", "/api/dependencies/"+rotate.DependencyID+"/waive", ResolveDependencyBody{Note: "retiring"}); code != http.StatusConflict {
		t.Errorf("waive after confirm = %d: %s; want 409", code, body)
	}
	if code, body := do("POST", "/api/dependencies/"+rotate.DependencyID+"/attach", AttachDependencyBody{}); code != http.StatusConflict {
		t.Errorf("detach after confirm = %d: %s; want 409", code, body)
	}
	code, raw = do("POST", "/api/dependencies/"+reimage.DependencyID+"/waive", ResolveDependencyBody{Note: "the machine is being retired"})
	if code != http.StatusOK {
		t.Fatalf("waive = %d: %s", code, raw)
	}
	_ = json.Unmarshal(raw, &reimage)
	if reimage.Status != dependency.StatusWaived || len(reimage.History) != 4 {
		t.Fatalf("waived = %+v; want WAIVED with created, attached, sent, waived on the history", reimage)
	}

	// 5. The list serves all three, oldest first; nothing is open.
	code, raw = do("GET", "/api/investigations/"+invID.String()+"/dependencies", nil)
	if code != http.StatusOK {
		t.Fatalf("list = %d", code)
	}
	var out struct {
		Dependencies []DependencyView `json:"dependencies"`
	}
	_ = json.Unmarshal(raw, &out)
	if len(out.Dependencies) != 3 || out.Dependencies[0].DependencyID != rotate.DependencyID {
		t.Fatalf("listed = %+v; want rotate, re-image, then the raced one", out.Dependencies)
	}
	if n, _ := store.OpenCount(context.Background(), invID); n != 0 {
		t.Errorf("open count = %d; want 0", n)
	}
	if _, err := store.Get(context.Background(), uuid.New()); !errors.Is(err, dependency.ErrNotFound) {
		t.Errorf("Get unknown = %v; want ErrNotFound", err)
	}
}

// TestDependencies_SentOnResolvedOutcome: a notify.* send whose run stopped
// (OUTCOME_UNKNOWN) and that a person then establishes as SUCCEEDED never
// passes through the result activity; the resolve route moves the dependencies
// riding on it to SENT. Resolving it FAILED moves nothing.
func TestDependencies_SentOnResolvedOutcome(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	if _, err := testDB.Exec(`TRUNCATE dependencies`); err != nil {
		t.Fatalf("truncate dependencies: %v", err)
	}
	invID := activeInvestigation(t)
	store := dependency.NewStore(testDB)
	b := newTestBackend(t)
	b.cfg.Dependencies = store
	token := mintToken(t, nil)

	stopped := func(title string, outcome string) dependency.Dependency {
		t.Helper()
		notifyID := uuid.New()
		humanHandle(t, invID, aggregate.RequestAction{
			ActionID: notifyID, ActionType: "notify.email", Tier: aggregate.TierT2,
			Targets:    []aggregate.TargetSpec{{ResolvedIdentifier: "platform@example.test"}},
			Parameters: json.RawMessage(`{"subject":"please rotate","body":"svc_backup"}`),
			ExpiresAt:  time.Now().Add(time.Hour), Rationale: "coordinate",
		})
		d, err := store.Create(context.Background(), dependency.Create{
			AggregateID: invID, TenantID: module.SingleTenantUUID, Title: title, Owner: "platform team",
			NotifyActionID: notifyID, By: dependency.Actor{ID: "test-subject"}, At: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		humanHandle(t, invID, aggregate.ApproveAction{
			ActionID: notifyID, Authorization: aggregate.Authorization{Mode: aggregate.AuthModeManual, Stage: aggregate.AuthStageSolo, PrimaryApproverRef: "test-subject", PrimaryApprovedAt: time.Now()},
		})
		dispatched(t, invID, notifyID)
		systemHandle(t, invID, aggregate.RecordOutcomeUnknown{ActionID: notifyID, Detail: "the run stopped"})
		resp, _ := postActionDecision(t, b, token, notifyID.String(), "resolve", ResolveOutcomeBody{Outcome: outcome, Note: "checked the mailbox"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("resolve %s = %d", outcome, resp.StatusCode)
		}
		got, err := store.Get(context.Background(), d.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if d := stopped("rotate svc_backup", aggregate.ActionStatusSucceeded); d.Status != dependency.StatusSent {
		t.Errorf("after resolving the send SUCCEEDED: %s; want SENT", d.Status)
	}
	if d := stopped("re-image WIN-FILE01", aggregate.ActionStatusFailed); d.Status != dependency.StatusOpen {
		t.Errorf("after resolving the send FAILED: %s; want OPEN", d.Status)
	}
}
