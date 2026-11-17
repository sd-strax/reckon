package eval

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sd-strax/reckon/agent"
)

// TestServedCatalog_FetchErrorFailsTheRun: the run fetches the action catalog
// before any trial, and any error, a 503 included, fails it at once with an
// error naming the fetch. There is no retry and no run on an empty catalog
// (adr/0380).
func TestServedCatalog_FetchErrorFailsTheRun(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusInternalServerError, http.StatusForbidden} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/action-types" {
				calls++
			}
			http.Error(w, "no", status)
		}))
		client := agent.NewClient(srv.URL, agent.StaticToken("A"), agent.StaticToken("H"))
		_, _, _, err := servedCatalog(context.Background(), client)
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "fetch the action catalog") {
			t.Errorf("status %d: err = %v; want the run to fail naming the fetch", status, err)
		}
		if calls != 1 {
			t.Errorf("status %d: %d fetches; want one, no retry", status, calls)
		}
	}
}

// TestTrialEndActions_FetchErrorStopsTheRun: a failed fetch of the trial-end
// action list is an error that stops the whole run, not one trial, and it
// names the fetch (adr/0380).
func TestTrialEndActions_FetchErrorStopsTheRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := agent.NewClient(srv.URL, agent.StaticToken("A"), agent.StaticToken("H"))
	_, err := trialEndActions(context.Background(), client, "inv-1")
	if err == nil || !strings.Contains(err.Error(), "fetch the trial-end action list") {
		t.Fatalf("err = %v; want an error naming the fetch", err)
	}
	runCtx, stop := context.WithCancelCause(context.Background())
	defer stop(nil)
	stopRunOn(stop, errors.New("turn 2: model call failed"))
	if runCtx.Err() != nil || runStopped(runCtx) != nil {
		t.Error("a trial's own failure stopped the run; it should stop only that trial")
	}
	stopRunOn(stop, err)
	select {
	case <-runCtx.Done():
	default:
		t.Fatal("the failed fetch did not cancel the run's context, so the other trials would keep spending tokens")
	}
	if got := runStopped(runCtx); !errors.Is(got, errTrialEndActions) {
		t.Errorf("run error = %v; want the failed fetch", got)
	}
}
