package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
)

func newSurface() (*capability.Resolver, *capability.Catalog) {
	reg := capability.NewRegistry(identity.NewResolver(uuid.New()))
	r := capability.NewResolver(capability.TenantContext{Name: "t"}, nil, nil, reg)
	return r, capability.NewCatalog()
}

func newActionResolver() *action.ActionResolver {
	return action.NewActionResolver(map[string][]action.ActionBinding{
		"host.isolate": {{ActionType: "host.isolate", Adapter: "a", Operation: "op", Priority: 1}},
	}, nil)
}

// TestReload_SwapsBothSides: Reload runs the injected rebuild once and swaps
// the capability surface, the /action-types readout and the worker's
// dispatch holder to what it returns (adr/0223).
func TestReload_SwapsBothSides(t *testing.T) {
	boot, bootCat := newSurface()
	next, nextCat := newSurface()
	bootAR, nextAR := action.NewActionResolver(nil, nil), newActionResolver()
	dispatch := action.NewAtomicResolver(bootAR)

	calls := 0
	b := &Backend{cfg: BackendConfig{
		CapabilityResolver: boot,
		CapabilityCatalog:  bootCat,
		ActionResolver:     bootAR,
		ActionDispatch:     dispatch,
		Rebuild: func() (ReloadedSurface, error) {
			calls++
			return ReloadedSurface{CapabilityResolver: next, CapabilityCatalog: nextCat, ActionResolver: nextAR}, nil
		},
	}}

	if err := b.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if calls != 1 {
		t.Errorf("rebuild called %d times, want 1", calls)
	}
	if r, c := b.capabilitySurface(); r != next || c != nextCat {
		t.Error("capability surface was not swapped")
	}
	if b.actionResolver() != nextAR {
		t.Error("/action-types readout was not swapped")
	}
	if dispatch.Load() != nextAR {
		t.Error("worker dispatch holder was not swapped")
	}
}

// TestReload_FailureSwapsNothing: a rebuild error returns the error and
// leaves both sides as they were.
func TestReload_FailureSwapsNothing(t *testing.T) {
	boot, bootCat := newSurface()
	bootAR := action.NewActionResolver(nil, nil)
	dispatch := action.NewAtomicResolver(bootAR)
	b := &Backend{cfg: BackendConfig{
		CapabilityResolver: boot,
		CapabilityCatalog:  bootCat,
		ActionResolver:     bootAR,
		ActionDispatch:     dispatch,
		Rebuild: func() (ReloadedSurface, error) {
			return ReloadedSurface{}, errors.New("bad action_adapters stanza")
		},
	}}
	if err := b.Reload(); err == nil {
		t.Fatal("want error from a failing rebuild")
	}
	if r, c := b.capabilitySurface(); r != boot || c != bootCat {
		t.Error("a failed reload must leave the capability surface unchanged")
	}
	if b.actionResolver() != bootAR || dispatch.Load() != bootAR {
		t.Error("a failed reload must leave the action side unchanged")
	}
}

// TestReload_NothingWired: with no rebuild and no config path, Reload is a
// no-op.
func TestReload_NothingWired(t *testing.T) {
	b := &Backend{cfg: BackendConfig{}}
	if err := b.Reload(); err != nil {
		t.Errorf("reload with nothing wired should be a nil no-op, got %v", err)
	}
}

// TestReload_OneAtATime: concurrent reloads (a SIGHUP during an enablement
// apply) never run their builds at the same time.
func TestReload_OneAtATime(t *testing.T) {
	var inFlight, maxInFlight atomic.Int32
	b := &Backend{cfg: BackendConfig{
		Rebuild: func() (ReloadedSurface, error) {
			n := inFlight.Add(1)
			defer inFlight.Add(-1)
			for {
				m := maxInFlight.Load()
				if n <= m || maxInFlight.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			r, c := newSurface()
			return ReloadedSurface{CapabilityResolver: r, CapabilityCatalog: c}, nil
		},
	}}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := b.Reload(); err != nil {
				t.Errorf("reload: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := maxInFlight.Load(); got != 1 {
		t.Errorf("%d reloads ran at once, want 1", got)
	}
}

// TestEnablement_ApplyRunsTheReload: an enablement apply runs the same
// reload SIGHUP does, so the write side is swapped as well as the read side.
func TestEnablement_ApplyRunsTheReload(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	b, _ := enablementBackend(t)
	next, nextCat := newSurface()
	nextAR := newActionResolver()
	dispatch := action.NewAtomicResolver(action.NewActionResolver(nil, nil))
	calls := 0
	b.cfg.ActionDispatch = dispatch
	b.cfg.Rebuild = func() (ReloadedSurface, error) {
		calls++
		return ReloadedSurface{CapabilityResolver: next, CapabilityCatalog: nextCat, ActionResolver: nextAR}, nil
	}
	srv := httptest.NewServer(b.buildRouter(b.verifier))
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(ApplyEnablementBody{Enabled: true, Config: map[string]string{"scenario": "lateral-movement-via-rdp"}})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/enablement/adapters/fixture_context", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+mintToken(t, nil))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply = %d: %s", resp.StatusCode, respBody)
	}
	if calls != 1 {
		t.Errorf("rebuild called %d times, want 1", calls)
	}
	if r, _ := b.capabilitySurface(); r != next {
		t.Error("capability surface was not swapped")
	}
	if dispatch.Load() != nextAR || b.actionResolver() != nextAR {
		t.Error("write side was not swapped by the enablement apply")
	}
}
