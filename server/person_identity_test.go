package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/identity"
)

// accountBackend is a fake backend with two persisted accounts and a person
// (adr/0324).
func accountBackend(t *testing.T) (b *Backend, store *fakeEvidence, acct, other, person string) {
	t.Helper()
	b = newFakeBackend(t)
	b.cfg.TenantNamespace = testTenantNamespace
	acct, other, person = "user-account--"+uuid.NewString(), "user-account--"+uuid.NewString(), "identity--"+uuid.NewString()
	store = &fakeEvidence{
		objects: map[string]evidence.Object{
			acct:   {Ref: acct, Type: identity.TypeUserAccount},
			other:  {Ref: other, Type: identity.TypeUserAccount},
			person: {Ref: person, Type: identity.TypeIdentity, Payload: []byte(`{"properties":{"identity_class":"individual"}}`)},
		},
		accountIdentity: evidence.AccountIdentityView{
			Owners: []evidence.Owner{{IdentityEdge: evidence.IdentityEdge{EdgeID: "relationship--o", Kind: evidence.IdentityObserved, Source: "idp"}, PersonRef: person, AccountRef: acct}},
		},
		personIdentity: evidence.PersonIdentityView{
			Accounts: []evidence.PersonAccount{{IdentityEdge: evidence.IdentityEdge{EdgeID: "relationship--o", Kind: evidence.IdentityObserved}, AccountRef: acct}},
		},
		personsByName: map[string][]string{"jdoe@contoso.example": {person}, "j.doe": {person, "identity--" + uuid.NewString()}},
	}
	b.cfg.Evidence = store
	return b, store, acct, other, person
}

// TestEvidence_AccountAndPersonIdentity: an account's evidence read carries
// its identity view and a person's carries its own; a vendor identity
// carries neither; the host field stays absent on both.
func TestEvidence_AccountAndPersonIdentity(t *testing.T) {
	b, store, acct, _, person := accountBackend(t)
	code, body := send(t, b, "GET", "/api/evidence/"+acct, mintToken(t, nil), "")
	var v EvidenceView
	if code != http.StatusOK || json.Unmarshal([]byte(body), &v) != nil || v.AccountIdentity == nil || v.Identity != nil || v.PersonIdentity != nil ||
		len(v.AccountIdentity.Owners) != 1 || v.AccountIdentity.Owners[0].PersonRef != person || v.AccountIdentity.SeenAs.Logins == nil {
		t.Fatalf("account read: %d %s", code, body)
	}
	code, body = send(t, b, "GET", "/api/evidence/"+person, mintToken(t, nil), "")
	v = EvidenceView{}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &v) != nil || v.PersonIdentity == nil || v.AccountIdentity != nil ||
		len(v.PersonIdentity.Accounts) != 1 || v.PersonIdentity.Accounts[0].AccountRef != acct {
		t.Fatalf("person read: %d %s", code, body)
	}
	vendor := "identity--" + uuid.NewString()
	store.objects[vendor] = evidence.Object{Ref: vendor, Type: identity.TypeIdentity, Payload: []byte(`{"properties":{"name":"a vendor"}}`)}
	if _, body := send(t, b, "GET", "/api/evidence/"+vendor, mintToken(t, nil), ""); strings.Contains(body, "identity\":{") || strings.Contains(body, "person_identity") {
		t.Errorf("a vendor identity carries an identity view: %s", body)
	}
}

// TestRefAppearances_PersonWalksAccounts (adr/0324): a person's appearances
// are the union over its entity set, its owned accounts among them.
func TestRefAppearances_PersonWalksAccounts(t *testing.T) {
	b, store, acct, _, person := accountBackend(t)
	inv1, inv2 := uuid.New(), uuid.New()
	b.cfg.Reads = &fakeReads{appearances: map[string][]aggregate.RefAppearance{
		person: {{AggregateID: inv1, Title: "seeded on the person", Mentions: 1}},
		acct:   {{AggregateID: inv2, Title: "the account acted", Mentions: 2}},
	}}
	store.entitySets = map[string][]string{person: {person, acct}}
	code, body := send(t, b, "GET", "/api/entities/"+person+"/appearances", mintToken(t, nil), "")
	var got struct {
		Appearances []AppearanceView `json:"appearances"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &got) != nil || len(got.Appearances) != 2 {
		t.Fatalf("person appearances: %d %s; want both investigations", code, body)
	}
}

// TestAssertIdentity_AccountsAndPersons: two accounts join and split, two
// persons too, answered with the account's or the person's view; mixed types
// and a hostname for an account are 400.
func TestAssertIdentity_AccountsAndPersons(t *testing.T) {
	b, store, acct, other, person := accountBackend(t)
	human := mintToken(t, nil)
	code, body := send(t, b, "POST", "/api/entities/"+acct+"/aliases", human, `{"other_ref":"`+other+`","note":"same login"}`)
	var got IdentityActResponse
	if code != http.StatusOK || json.Unmarshal([]byte(body), &got) != nil || got.AccountIdentity == nil || got.Identity != nil || store.identityRef != acct {
		t.Fatalf("account join: %d %s", code, body)
	}
	if len(store.acts) != 1 || store.acts[0].kind != "aliases" || store.acts[0].other != other {
		t.Errorf("acts = %+v", store.acts)
	}
	person2 := "identity--" + uuid.NewString()
	store.objects[person2] = evidence.Object{Ref: person2, Type: identity.TypeIdentity}
	code, body = send(t, b, "POST", "/api/entities/"+person+"/distinct", human, `{"other_ref":"`+person2+`"}`)
	got = IdentityActResponse{}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &got) != nil || got.PersonIdentity == nil {
		t.Fatalf("person split: %d %s", code, body)
	}
	// "This account is not this person's": the one mixed split, either way.
	for _, tc := range []struct{ from, to string }{{acct, person}, {person, acct}} {
		store.acts = nil
		code, body := send(t, b, "POST", "/api/entities/"+tc.from+"/distinct", human, `{"other_ref":"`+tc.to+`"}`)
		if code != http.StatusOK || len(store.acts) != 1 || store.acts[0].kind != "distinct-from" || store.identityRef != tc.from {
			t.Errorf("split %s from %s: %d %s, acts %+v", tc.from, tc.to, code, body, store.acts)
		}
	}
	host := "x-host--" + uuid.NewString()
	store.acts = nil
	for _, tc := range []struct{ name, path, body string }{
		{"account to host", "/api/entities/" + acct + "/aliases", `{"other_ref":"` + host + `"}`},
		{"person joined to account", "/api/entities/" + person + "/aliases", `{"other_ref":"` + acct + `"}`},
		{"account split from host", "/api/entities/" + acct + "/distinct", `{"other_ref":"` + host + `"}`},
		{"hostname for an account", "/api/entities/" + acct + "/aliases", `{"other_hostname":"ws1"}`},
	} {
		if code, body := send(t, b, "POST", tc.path, human, tc.body); code != http.StatusBadRequest || len(store.acts) != 0 {
			t.Errorf("%s: %d %s; want 400 and no act", tc.name, code, body)
		}
	}
	// A deadlock the store's retry did not clear: 409, try again.
	store.actErr = fmt.Errorf("insert assertion: %w", &pq.Error{Code: "40P01"})
	if code, body := send(t, b, "POST", "/api/entities/"+acct+"/aliases", human, `{"other_ref":"`+other+`"}`); code != http.StatusConflict ||
		!strings.Contains(body, "a concurrent identity change; try again") {
		t.Errorf("deadlock: %d %s; want 409", code, body)
	}
	// Alias sets that kept growing past the store's retry: the same 409.
	store.actErr = evidence.ErrIdentityRace
	if code, body := send(t, b, "POST", "/api/entities/"+acct+"/aliases", human, `{"other_ref":"`+other+`"}`); code != http.StatusConflict ||
		!strings.Contains(body, "a concurrent identity change; try again") {
		t.Errorf("identity race: %d %s; want 409", code, body)
	}
	// A person split that would leave an account with two owners: 409.
	store.actErr = evidence.ErrOwnersSeparated
	if code, body := send(t, b, "POST", "/api/entities/"+acct+"/aliases", human, `{"other_ref":"`+other+`"}`); code != http.StatusConflict ||
		!strings.Contains(body, "the split would leave an account asserted to two persons; withdraw one owner first") {
		t.Errorf("owners separated: %d %s; want 409", code, body)
	}
	// Two accounts asserted to different persons: 409.
	store.actErr = evidence.ErrOwnersDiffer
	if code, body := send(t, b, "POST", "/api/entities/"+acct+"/aliases", human, `{"other_ref":"`+other+`"}`); code != http.StatusConflict ||
		!strings.Contains(body, "the two are asserted to different persons; withdraw one first") {
		t.Errorf("join across owners: %d %s; want 409", code, body)
	}
}

// TestAssertOwner_Endpoint: an account's owner by ref or by hint, human only;
// a hint naming no person is 404, several 409; a live owner elsewhere 409.
func TestAssertOwner_Endpoint(t *testing.T) {
	b, store, acct, _, person := accountBackend(t)
	human := mintToken(t, nil)
	for _, body := range []string{`{"person_ref":"` + person + `","note":"badge photo"}`, `{"person_hint":"jdoe@contoso.example"}`} {
		store.acts = nil
		code, resp := send(t, b, "POST", "/api/entities/"+acct+"/owner", human, body)
		var got IdentityActResponse
		if code != http.StatusOK || json.Unmarshal([]byte(resp), &got) != nil || got.AccountIdentity == nil ||
			len(store.acts) != 1 || store.acts[0].kind != "owner" || store.acts[0].ref != acct || store.acts[0].other != person || store.acts[0].by.ID != "test-subject" {
			t.Errorf("%s: %d %s, acts %+v", body, code, resp, store.acts)
		}
	}
	store.acts = nil
	for _, tc := range []struct {
		name, path, token, body string
		want                    int
	}{
		{"delegate", "/api/entities/" + acct + "/owner", mintToken(t, map[string]any{"delegate_kind": "claude"}), `{"person_ref":"` + person + `"}`, http.StatusForbidden},
		{"not an account", "/api/entities/" + person + "/owner", human, `{"person_ref":"` + person + `"}`, http.StatusBadRequest},
		{"not a person", "/api/entities/" + acct + "/owner", human, `{"person_ref":"` + acct + `"}`, http.StatusBadRequest},
		{"neither", "/api/entities/" + acct + "/owner", human, `{"note":"x"}`, http.StatusBadRequest},
		{"unknown hint", "/api/entities/" + acct + "/owner", human, `{"person_hint":"nobody"}`, http.StatusNotFound},
		{"ambiguous hint", "/api/entities/" + acct + "/owner", human, `{"person_hint":"j.doe"}`, http.StatusConflict},
		{"unknown person", "/api/entities/" + acct + "/owner", human, `{"person_ref":"identity--` + uuid.NewString() + `"}`, http.StatusNotFound},
	} {
		if code, body := send(t, b, "POST", tc.path, tc.token, tc.body); code != tc.want || len(store.acts) != 0 {
			t.Errorf("%s: %d %s, acts %+v; want %d and no act", tc.name, code, body, store.acts, tc.want)
		}
	}
	store.actErr = evidence.ErrOwnerAsserted
	if code, body := send(t, b, "POST", "/api/entities/"+acct+"/owner", human, `{"person_ref":"`+person+`"}`); code != http.StatusConflict {
		t.Errorf("another live owner: %d %s; want 409", code, body)
	}
}

// TestWithdrawOwner: withdrawing an owner edge answers the account's view, or
// the person's when asked.
func TestWithdrawOwner(t *testing.T) {
	b, store, acct, _, person := accountBackend(t)
	store.withdrawEnds = [2]string{acct, person}
	edge := uuid.NewString()
	code, body := send(t, b, "POST", "/api/identity/"+edge+"/withdraw", mintToken(t, nil), ``)
	var got IdentityActResponse
	if code != http.StatusOK || json.Unmarshal([]byte(body), &got) != nil || got.AccountIdentity == nil || store.identityRef != acct {
		t.Fatalf("withdraw: %d %s", code, body)
	}
	code, body = send(t, b, "POST", "/api/identity/"+edge+"/withdraw", mintToken(t, nil), `{"ref":"`+person+`"}`)
	got = IdentityActResponse{}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &got) != nil || got.PersonIdentity == nil || store.identityRef != person {
		t.Fatalf("withdraw, the person's view: %d %s", code, body)
	}
	// A directory's owner is not withdrawn: it is cut by a split.
	store.observedEdge, store.acts = true, nil
	code, body = send(t, b, "POST", "/api/identity/"+edge+"/withdraw", mintToken(t, nil), ``)
	if code != http.StatusConflict || !strings.Contains(body, "assert that the account is not this person's instead") || len(store.acts) != 0 {
		t.Errorf("observed owner: %d %s; want 409 naming the split", code, body)
	}
}

// TestSeedEntityKind (adr/0324): a token with a backslash or an "@" seeds an
// account by default, minted by the account rule; a chosen kind overrides
// the default; a bare name stays a host.
func TestSeedEntityKind(t *testing.T) {
	res := identity.NewResolver(uuid.New())
	for token, want := range map[string]identity.STIXID{
		`CONTOSO\jdoe`:         res.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, Login: `contoso\jdoe`}),
		"jdoe@contoso.example": res.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, Login: "jdoe@contoso.example"}),
		"WIN-FILE01":           res.Host(identity.HostIdentity{Hostname: "win-file01"}),
	} {
		got, err := resolveSeedInputAs(res, token, "", "")
		if err != nil || got.Seed.EntityRef != string(want) {
			t.Errorf("%q: %+v, %v; want %s", token, got.Seed, err, want)
		}
	}
	got, err := resolveSeedInputAs(res, "jdoe", "", entityKindAccount)
	if want := res.Account(identity.AccountIdentity{Type: identity.AccountWindows, Login: "jdoe"}); err != nil || got.Seed.EntityRef != string(want) || got.Display != "account jdoe" {
		t.Errorf("bare account: %+v, %v; want %s", got, err, want)
	}
	for _, bad := range []struct{ token, kind string }{
		{`CONTOSO\-`, ""}, {"ANONYMOUS LOGON", entityKindAccount}, {`a\b\c`, ""}, {"jdoe", "thing"}, {`CONTOSO\jdoe`, entityKindHost},
	} {
		if r, err := resolveSeedInputAs(res, bad.token, aggregate.SeedEntity, bad.kind); err == nil {
			t.Errorf("%q as %q accepted: %+v", bad.token, bad.kind, r)
		}
	}
}

// TestCreateInvestigation_PersonSeed: a person seed roots on the person the
// record knows by the token; an unknown one is 404, an ambiguous one 409.
func TestCreateInvestigation_PersonSeed(t *testing.T) {
	b, _, _, _, person := accountBackend(t)
	cmds := &fakeCommands{}
	b.cfg.Handler = cmds
	code, body := send(t, b, "POST", "/api/investigations", mintToken(t, nil),
		`{"seed_input":{"value":"jdoe@contoso.example","kind":"entity","entity_kind":"person"}}`)
	if code != http.StatusCreated {
		t.Fatalf("person seed: %d %s", code, body)
	}
	cmd, ok := cmds.cmd.(aggregate.CreateInvestigation)
	if !ok || cmd.Seed == nil || cmd.Seed.EntityRef != person || cmd.Title != "person jdoe@contoso.example" {
		t.Errorf("command = %+v", cmds.cmd)
	}
	for value, want := range map[string]int{"nobody@contoso.example": http.StatusNotFound, "j.doe": http.StatusConflict} {
		if code, body := send(t, b, "POST", "/api/investigations", mintToken(t, nil),
			`{"seed_input":{"value":"`+value+`","entity_kind":"person"}}`); code != want {
			t.Errorf("%s: %d %s; want %d", value, code, body, want)
		}
	}
}

// TestCreateInvestigation_UnknownAccount: an account seed the record holds no
// object for is reported with the accounts it does hold under the name,
// unless the analyst starts anyway.
func TestCreateInvestigation_UnknownAccount(t *testing.T) {
	b, store, acct, other, _ := accountBackend(t)
	cmds := &fakeCommands{}
	b.cfg.Handler = cmds
	// One sign-in account under the principal name: the seed roots on it.
	store.accountsByName = map[string][]evidence.AccountMatch{"jdoe@contoso.example": {{Ref: acct, Type: identity.AccountCloud, Login: "jdoe@contoso.example"}}}
	code, body := send(t, b, "POST", "/api/investigations", mintToken(t, nil), `{"seed_input":{"value":"jdoe@contoso.example"}}`)
	if cmd, ok := cmds.cmd.(aggregate.CreateInvestigation); code != http.StatusCreated || !ok || cmd.Seed == nil || cmd.Seed.EntityRef != acct {
		t.Fatalf("one sign-in account: %d %s, command %+v; want the seed on %s", code, body, cmds.cmd, acct)
	}
	// Two accounts under the name: reported, listed.
	store.accountsByName["jdoe@contoso.example"] = append(store.accountsByName["jdoe@contoso.example"],
		evidence.AccountMatch{Ref: other, Type: identity.AccountCloud, Login: "jdoe@contoso.example"})
	code, body = send(t, b, "POST", "/api/investigations", mintToken(t, nil), `{"seed_input":{"value":"jdoe@contoso.example"}}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, `"unknown_entity":true`) || !strings.Contains(body, acct) || !strings.Contains(body, other) {
		t.Fatalf("unknown account: %d %s; want 422 listing both", code, body)
	}
	// The sign-in's two accounts, the provider id's and the sign-in name's
	// aliased to it, are one account: the seed roots on the id's.
	nameAcct := "user-account--" + uuid.NewString()
	store.accountsByName["jdoe@contoso.example"] = []evidence.AccountMatch{
		{Ref: nameAcct, Type: identity.AccountCloud, Login: "jdoe@contoso.example"},
		{Ref: other, Type: identity.AccountCloud, Login: "jdoe@contoso.example", UserID: "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c"},
	}
	store.aliases = []string{nameAcct, other}
	code, body = send(t, b, "POST", "/api/investigations", mintToken(t, nil), `{"seed_input":{"value":"jdoe@contoso.example"}}`)
	if cmd, ok := cmds.cmd.(aggregate.CreateInvestigation); code != http.StatusCreated || !ok || cmd.Seed == nil || cmd.Seed.EntityRef != other {
		t.Fatalf("one sign-in alias set: %d %s, command %+v; want the seed on the id-keyed %s", code, body, cmds.cmd, other)
	}
	store.aliases = nil
	if code, body := send(t, b, "POST", "/api/investigations", mintToken(t, nil), `{"seed_input":{"value":"jdoe@contoso.example","allow_unknown":true}}`); code != http.StatusCreated {
		t.Errorf("start anyway: %d %s", code, body)
	}
	// An account the record holds seeds without a report.
	ids := identity.NewResolver(uuid.MustParse(testTenantNamespace))
	known := string(ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, Login: `CONTOSO\jdoe`}))
	store.objects[known] = evidence.Object{Ref: known, Type: identity.TypeUserAccount}
	if code, body := send(t, b, "POST", "/api/investigations", mintToken(t, nil), `{"seed_input":{"value":"CONTOSO\\jdoe"}}`); code != http.StatusCreated {
		t.Errorf("known account: %d %s", code, body)
	}
}
