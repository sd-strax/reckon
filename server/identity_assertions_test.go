package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/identity"
)

const testTenantNamespace = "6f1c0c55-7d2e-4f1e-9a51-0b6f0e5d3a10"

// identityBackend is a fake backend with a tenant namespace and two persisted
// hosts: the seed's "win-file01" and another.
func identityBackend(t *testing.T) (*Backend, *fakeEvidence, string, string) {
	t.Helper()
	b := newFakeBackend(t)
	b.cfg.TenantNamespace = testTenantNamespace
	ids := identity.NewResolver(uuid.MustParse(testTenantNamespace))
	host := string(ids.Host(identity.HostIdentity{Hostname: "win-file01"}))
	other := "x-host--" + uuid.NewString()
	store := &fakeEvidence{
		objects: map[string]evidence.Object{
			host:  {Ref: host, Type: identity.TypeHost},
			other: {Ref: other, Type: identity.TypeHost},
		},
		identity: evidence.HostIdentityView{
			Aliases: []evidence.IdentityEdge{{EdgeID: "relationship--a", OtherRef: other, Kind: evidence.IdentityAsserted, By: "test-subject", ByName: "alice", Note: "same"}},
		},
	}
	b.cfg.Evidence = store
	return b, store, host, other
}

// TestAssertIdentity_Answers: a join and a split each record the act by the
// token's person with the note, and answer the identity view of the asked-for
// host.
func TestAssertIdentity_Answers(t *testing.T) {
	for _, tc := range []struct{ path, kind string }{{"aliases", "aliases"}, {"distinct", "distinct-from"}} {
		b, store, host, other := identityBackend(t)
		code, body := send(t, b, "POST", "/api/entities/"+other+"/"+tc.path, mintToken(t, nil), `{"other_ref":"`+host+`","note":"asset register"}`)
		if code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", tc.path, code, body)
		}
		if len(store.acts) != 1 {
			t.Fatalf("%s: acts = %+v", tc.path, store.acts)
		}
		a := store.acts[0]
		if a.kind != tc.kind || a.ref != other || a.other != host || a.by.ID != "test-subject" || a.by.Name != "alice" || a.note != "asset register" {
			t.Errorf("%s: act = %+v", tc.path, a)
		}
		var got IdentityActResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil || got.Identity == nil || len(got.Identity.Aliases) != 1 ||
			got.Identity.Aliases[0].EdgeID != "relationship--a" || got.Identity.Distinct == nil {
			t.Errorf("%s: view = %s (%v)", tc.path, body, err)
		}
		if store.identityRef != other {
			t.Errorf("%s: answered the view of %s; want %s", tc.path, store.identityRef, other)
		}
	}
}

// TestAssertIdentity_HostnameFolds: a hostname is minted as the seed mints
// it, so an FQDN in another case names the host the short name does.
func TestAssertIdentity_HostnameFolds(t *testing.T) {
	b, store, host, other := identityBackend(t)
	for _, name := range []string{"WIN-FILE01.corp.example", "win-file01", "WIN-FILE01"} {
		store.acts = nil
		code, body := send(t, b, "POST", "/api/entities/"+other+"/aliases", mintToken(t, nil), `{"other_hostname":"`+name+`"}`)
		if code != http.StatusOK || len(store.acts) != 1 || store.acts[0].other != host {
			t.Errorf("%s: status %d, acts %+v; want the act against %s: %s", name, code, store.acts, host, body)
		}
	}
}

// TestAssertIdentity_Refusals: a delegate is refused; a non-host ref, both or
// neither of the other's names, the host itself, a placeholder, an address or
// a malformed hostname are 400; an unknown other host is 404.
func TestAssertIdentity_Refusals(t *testing.T) {
	b, store, host, other := identityBackend(t)
	human, delegate := mintToken(t, nil), mintToken(t, map[string]any{"delegate_kind": "claude"})
	for _, tc := range []struct {
		name, path, token, body string
		want                    int
	}{
		{"delegate", "/api/entities/" + other + "/aliases", delegate, `{"other_ref":"` + host + `"}`, http.StatusForbidden},
		{"delegate split", "/api/entities/" + other + "/distinct", delegate, `{"other_ref":"` + host + `"}`, http.StatusForbidden},
		{"non-host ref", "/api/entities/ipv4-addr--" + uuid.NewString() + "/aliases", human, `{"other_ref":"` + host + `"}`, http.StatusBadRequest},
		{"non-host other", "/api/entities/" + other + "/distinct", human, `{"other_ref":"user-account--` + uuid.NewString() + `"}`, http.StatusBadRequest},
		{"malformed ref", "/api/entities/x-host--nope/aliases", human, `{"other_ref":"` + host + `"}`, http.StatusBadRequest},
		{"both", "/api/entities/" + other + "/aliases", human, `{"other_ref":"` + host + `","other_hostname":"win-file01"}`, http.StatusBadRequest},
		{"neither", "/api/entities/" + other + "/aliases", human, `{"note":"x"}`, http.StatusBadRequest},
		{"no body", "/api/entities/" + other + "/aliases", human, ``, http.StatusBadRequest},
		{"self", "/api/entities/" + host + "/aliases", human, `{"other_ref":"` + host + `"}`, http.StatusBadRequest},
		{"self by name", "/api/entities/" + host + "/distinct", human, `{"other_hostname":"WIN-FILE01.corp.example"}`, http.StatusBadRequest},
		{"placeholder", "/api/entities/" + other + "/aliases", human, `{"other_hostname":"localhost"}`, http.StatusBadRequest},
		{"address", "/api/entities/" + other + "/aliases", human, `{"other_hostname":"10.0.0.7"}`, http.StatusBadRequest},
		{"not a name", "/api/entities/" + other + "/aliases", human, `{"other_hostname":"two words"}`, http.StatusBadRequest},
		{"unknown other", "/api/entities/" + other + "/aliases", human, `{"other_ref":"x-host--` + uuid.NewString() + `"}`, http.StatusNotFound},
		{"unknown split", "/api/entities/" + other + "/distinct", human, `{"other_hostname":"ws-nowhere"}`, http.StatusNotFound},
		{"unknown self", "/api/entities/x-host--" + uuid.NewString() + "/aliases", human, `{"other_ref":"` + host + `"}`, http.StatusNotFound},
	} {
		store.acts = nil
		code, body := send(t, b, "POST", tc.path, tc.token, tc.body)
		if code != tc.want || len(store.acts) != 0 {
			t.Errorf("%s: status %d, acts %+v; want %d and no act: %s", tc.name, code, store.acts, tc.want, body)
		}
	}

	// The store names the missing end; a join over a live split is 409.
	ghost := "x-host--" + uuid.NewString()
	if code, body := send(t, b, "POST", "/api/entities/"+other+"/aliases", human, `{"other_ref":"`+ghost+`"}`); code != http.StatusNotFound || !strings.Contains(body, ghost) {
		t.Errorf("missing end: status %d, %s; want 404 naming %s", code, body, ghost)
	}
	store.actErr = evidence.ErrSplitLive
	if code, body := send(t, b, "POST", "/api/entities/"+other+"/aliases", human, `{"other_ref":"`+host+`"}`); code != http.StatusConflict || !strings.Contains(body, "withdraw that first") {
		t.Errorf("join over a split: status %d, %s; want 409", code, body)
	}
}

// TestIdentityAct_ViewUnavailable: an act that committed is answered as done
// though the view behind it cannot be read: 200 with the identity null, never
// an empty view that reads as a host with no aliases. A store refusing an act
// with no principal is a 500.
func TestIdentityAct_ViewUnavailable(t *testing.T) {
	b, store, host, other := identityBackend(t)
	store.identityErr = errStoreDown
	store.withdrawEnds = [2]string{host, other}
	for _, path := range []string{"/api/entities/" + other + "/aliases", "/api/entities/" + other + "/distinct", "/api/identity/" + uuid.NewString() + "/withdraw"} {
		store.acts = nil
		code, body := send(t, b, "POST", path, mintToken(t, nil), `{"other_ref":"`+host+`"}`)
		if strings.HasSuffix(path, "/withdraw") {
			code, body = send(t, b, "POST", path, mintToken(t, nil), ``)
		}
		if code != http.StatusOK || len(store.acts) == 0 || strings.TrimSpace(body) != `{"identity":null}` {
			t.Errorf("%s: status %d, acts %+v, body %s; want 200, the act, identity null", path, code, store.acts, body)
		}
	}

	store.identityErr, store.actErr = nil, evidence.ErrNoAsserter
	if code, body := send(t, b, "POST", "/api/entities/"+other+"/aliases", mintToken(t, nil), `{"other_ref":"`+host+`"}`); code != http.StatusInternalServerError || !strings.Contains(body, "no principal") {
		t.Errorf("no asserter: status %d, %s; want 500 no principal", code, body)
	}
}

// TestWithdrawIdentity: the act names the edge and the person; the answer is
// the source host's view, or the asked-for host's; a source's record is 409,
// an unknown edge 404, a delegate 403.
func TestWithdrawIdentity(t *testing.T) {
	b, store, host, other := identityBackend(t)
	edge := uuid.New()
	store.withdrawEnds = [2]string{host, other}

	code, body := send(t, b, "POST", "/api/identity/relationship--"+edge.String()+"/withdraw", mintToken(t, nil), `{"note":"wrong link"}`)
	if code != http.StatusOK || len(store.acts) != 1 || store.acts[0].kind != "withdraw" || store.acts[0].edge != edge ||
		store.acts[0].by.ID != "test-subject" || store.acts[0].note != "wrong link" || store.identityRef != host {
		t.Fatalf("withdraw: status %d, acts %+v, view of %s: %s", code, store.acts, store.identityRef, body)
	}
	if !strings.Contains(body, `"aliases"`) {
		t.Errorf("withdraw answered %s; want the identity view", body)
	}
	// A bare uuid, no body, and the view of the host the inspector shows.
	if code, _ := send(t, b, "POST", "/api/identity/"+edge.String()+"/withdraw", mintToken(t, nil), `{"ref":"`+other+`"}`); code != http.StatusOK || store.identityRef != other {
		t.Errorf("withdraw with ref: status %d, view of %s; want %s", code, store.identityRef, other)
	}
	if code, _ := send(t, b, "POST", "/api/identity/"+edge.String()+"/withdraw", mintToken(t, nil), ``); code != http.StatusOK {
		t.Errorf("withdraw without a body: status %d", code)
	}
	// The ref in upper case is the same end.
	if code, _ := send(t, b, "POST", "/api/identity/"+edge.String()+"/withdraw", mintToken(t, nil), `{"ref":"`+strings.ToUpper(other)+`"}`); code != http.StatusOK || store.identityRef != other {
		t.Errorf("withdraw with an upper-case ref: status %d, view of %s; want %s", code, store.identityRef, other)
	}
	// The other end's object gone: the ref is still an end of the edge row.
	delete(store.objects, host)
	if code, body := send(t, b, "POST", "/api/identity/"+edge.String()+"/withdraw", mintToken(t, nil), `{"ref":"`+other+`"}`); code != http.StatusOK || store.identityRef != other {
		t.Errorf("withdraw with the other end gone: status %d, view of %s: %s", code, store.identityRef, body)
	}

	store.acts = nil
	for _, tc := range []struct {
		name, path, token, body string
		err                     error
		want                    int
	}{
		{"delegate", "/api/identity/" + edge.String() + "/withdraw", mintToken(t, map[string]any{"delegate_kind": "claude"}), ``, nil, http.StatusForbidden},
		{"bad id", "/api/identity/relationship--nope/withdraw", mintToken(t, nil), ``, nil, http.StatusBadRequest},
		{"bad ref", "/api/identity/" + edge.String() + "/withdraw", mintToken(t, nil), `{"ref":"ipv4-addr--` + uuid.NewString() + `"}`, nil, http.StatusBadRequest},
		// A host that is neither end of the edge: refused before the write.
		{"not an end", "/api/identity/" + edge.String() + "/withdraw", mintToken(t, nil), `{"ref":"x-host--` + uuid.NewString() + `"}`, nil, http.StatusBadRequest},
	} {
		if code, body := send(t, b, "POST", tc.path, tc.token, tc.body); code != tc.want || len(store.acts) != 0 {
			t.Errorf("%s: status %d, acts %+v; want %d: %s", tc.name, code, store.acts, tc.want, body)
		}
	}
	// From the edge row, before the write: a source's record is 409, no edge 404.
	store.observedEdge = true
	if code, body := send(t, b, "POST", "/api/identity/"+edge.String()+"/withdraw", mintToken(t, nil), ``); code != http.StatusConflict || len(store.acts) != 0 {
		t.Errorf("observed edge: status %d, acts %+v; want 409 and no act: %s", code, store.acts, body)
	}
	store.observedEdge, store.endsErr = false, evidence.ErrNotFound
	if code, body := send(t, b, "POST", "/api/identity/"+edge.String()+"/withdraw", mintToken(t, nil), ``); code != http.StatusNotFound || len(store.acts) != 0 {
		t.Errorf("no such edge: status %d, acts %+v; want 404 and no act: %s", code, store.acts, body)
	}
	store.endsErr = nil
	for err, want := range map[error]int{
		evidence.ErrObservedEdge: http.StatusConflict,
		evidence.ErrNotFound:     http.StatusNotFound,
		errStoreDown:             http.StatusInternalServerError,
	} {
		store.actErr = err
		if code, body := send(t, b, "POST", "/api/identity/"+edge.String()+"/withdraw", mintToken(t, nil), ``); code != want {
			t.Errorf("store answers %v: status %d; want %d: %s", err, code, want, body)
		}
	}
}
