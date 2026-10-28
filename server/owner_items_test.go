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
	"github.com/sd-strax/reckon/owneritems"
	"github.com/sd-strax/reckon/temporal"
)

// adr/0101: an owner action item is created, attached to the notification
// that asks for it, moves to SENT only when that notification succeeds, and
// is confirmed or waived by a human; the agent's creation is a proposal and
// it may not confirm; the open count feeds the preflight.
func TestOwnerItems_Lifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetInvestigations(t)
	if _, err := testDB.Exec(`TRUNCATE owner_items, comms_threads`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	invID := activeInvestigation(t)
	store := owneritems.NewStore(testDB)
	b := newTestBackend(t)
	b.cfg.OwnerItems = store
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)
	human := mintToken(t, nil)
	ai := mintToken(t, map[string]any{"delegate_kind": "claude"})
	call := func(method, path, token string, body any) (int, map[string]any) {
		t.Helper()
		var rd *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rd = bytes.NewReader(raw)
		} else {
			rd = bytes.NewReader(nil)
		}
		req, _ := http.NewRequest(method, srv.URL+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(r.Body).Decode(&out)
		return r.StatusCode, out
	}

	// The agent proposes; the analyst adds one of their own.
	code, proposed := call(http.MethodPost, "/api/investigations/"+invID.String()+"/owner_items", ai, OwnerItemBody{Title: "Rotate the service account's credential", Owner: "Identity team", Detail: "the account was used from a workstation it never logs on from"})
	if code != http.StatusCreated || proposed["status"] != owneritems.StatusOpen || proposed["created_by_kind"] != "AI_DELEGATED" {
		t.Fatalf("agent proposal = %d %+v", code, proposed)
	}
	code, own := call(http.MethodPost, "/api/investigations/"+invID.String()+"/owner_items", human, OwnerItemBody{Title: "Confirm the maintenance window", Owner: "Change owner"})
	if code != http.StatusCreated || own["created_by_kind"] != "HUMAN" {
		t.Fatalf("analyst item = %d %+v", code, own)
	}
	if code, _ := call(http.MethodPost, "/api/investigations/"+invID.String()+"/owner_items", human, OwnerItemBody{Title: "no owner"}); code != http.StatusBadRequest {
		t.Errorf("missing owner = %d; want 400", code)
	}
	proposedID := proposed["id"].(string)

	// The agent may not confirm or waive.
	if code, _ := call(http.MethodPost, "/api/owner_items/"+proposedID+"/confirm", ai, map[string]any{"note": "done"}); code != http.StatusForbidden {
		t.Errorf("agent confirm = %d; want 403", code)
	}

	// Attach to a notification, then the notification succeeds → SENT.
	actionID := uuid.New()
	if code, out := call(http.MethodPost, "/api/owner_items/"+proposedID+"/attach", human, map[string]any{"action_id": actionID.String()}); code != http.StatusOK {
		t.Fatalf("attach = %d %v", code, out) // the body names the store error
	}
	env := newEnvelope(invID, aggregate.Actor{PrincipalID: "test-subject"}, commandNow())
	if _, err := testHandler.Handle(context.Background(), env, aggregate.RequestAction{
		ActionID: actionID, ActionType: "notify.email", Tier: aggregate.TierT2,
		Targets: []aggregate.TargetSpec{{ResolvedIdentifier: "identity@example.test"}}, Parameters: json.RawMessage(`{"subject":"please rotate","body":"…"}`),
		ExpiresAt: time.Now().Add(time.Hour), Rationale: "ask the owner",
	}); err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := testHandler.Handle(context.Background(), newEnvelope(invID, aggregate.Actor{PrincipalID: "test-subject"}, commandNow()), aggregate.ApproveAction{
		ActionID: actionID, Authorization: aggregate.Authorization{Mode: aggregate.AuthModeManual, Stage: aggregate.AuthStageSolo, PrimaryApproverRef: "test-subject", PrimaryApprovedAt: time.Now()},
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	acts := temporal.NewActivities(testHandler, nil, nil, store)
	if err := acts.EmitDispatched(context.Background(), temporal.EmitDispatchedInput{ActionID: actionID.String(), AggregateID: invID.String(), TenantID: module.SingleTenantUUID.String(), ApproverID: "test-subject", AdapterRequestID: "wf-" + actionID.String()}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// Still OPEN before the result lands: nothing was sent yet.
	if n, _ := store.OpenCount(context.Background(), invID); n != 2 {
		t.Errorf("open before result = %d; want 2", n)
	}
	if err := acts.EmitResulted(context.Background(), temporal.EmitResultedInput{ActionID: actionID.String(), AggregateID: invID.String(), TenantID: module.SingleTenantUUID.String(), ApproverID: "test-subject", FinalOutcome: "SUCCEEDED", PerTargetResults: map[string]string{"0": "OK"}, Attempts: 1}); err != nil {
		t.Fatalf("result: %v", err)
	}
	_, list := call(http.MethodGet, "/api/investigations/"+invID.String()+"/owner_items", human, nil)
	items := list["owner_items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	first := items[0].(map[string]any)
	if first["status"] != owneritems.StatusSent || first["notify_action_id"] != actionID.String() || first["sent_at"] == nil {
		t.Errorf("attached item after the send = %+v; want SENT", first)
	}
	// SENT still counts as open for the preflight; confirmation closes it.
	if n, _ := store.OpenCount(context.Background(), invID); n != 2 {
		t.Errorf("open after send = %d; want 2", n)
	}
	code, confirmed := call(http.MethodPost, "/api/owner_items/"+proposedID+"/confirm", human, map[string]any{"note": "rotated at 14:05, confirmed by the team lead"})
	if code != http.StatusOK || confirmed["status"] != owneritems.StatusConfirmed || confirmed["confirmation_note"] == "" {
		t.Errorf("confirm = %d %+v", code, confirmed)
	}
	hist := confirmed["history"].([]any)
	if len(hist) != 4 { // created, attached, sent, confirmed
		t.Errorf("history = %d entries; want 4: %+v", len(hist), hist)
	}
	// A waive needs a reason; a closed item takes no further act.
	ownID := own["id"].(string)
	if code, _ := call(http.MethodPost, "/api/owner_items/"+ownID+"/waive", human, map[string]any{}); code != http.StatusUnprocessableEntity {
		t.Errorf("waive without reason = %d; want 422", code)
	}
	if code, _ := call(http.MethodPost, "/api/owner_items/"+ownID+"/waive", human, map[string]any{"reason": "the window was never used"}); code != http.StatusOK {
		t.Errorf("waive = %d", code)
	}
	if code, _ := call(http.MethodPost, "/api/owner_items/"+ownID+"/confirm", human, map[string]any{"note": "late"}); code != http.StatusUnprocessableEntity {
		t.Errorf("confirm after waive = %d; want 422", code)
	}
	if n, _ := store.OpenCount(context.Background(), invID); n != 0 {
		t.Errorf("open at the end = %d; want 0", n)
	}
}
