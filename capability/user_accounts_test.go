package capability

import (
	"slices"
	"strings"
	"testing"

	"github.com/sd-strax/reckon/capability/ocsfview"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsf"
	"github.com/sd-strax/reckon/stix"
)

// userAccountsOf is the observation's user_accounts (adr/0398).
func userAccountsOf(res NormalizationResult) map[string][]string {
	if len(res.ObservedData) == 0 {
		return nil
	}
	m, _ := res.ObservedData[0].Extensions[stix.ObservationUserAccounts.Name()].(map[string][]string)
	return m
}

// timed is p with an event time, which every record needs.
func timed(p map[string]any) map[string]any {
	p["time"] = ms("2026-01-02T00:00:00Z")
	return p
}

// handEvents are events of the classes no fixture holds, or holds without
// the user object a test needs, keyed by a name.
func handEvents(t *testing.T) map[string]OcsfEvent {
	t.Helper()
	out := map[string]OcsfEvent{}
	for name, p := range map[string]map[string]any{
		"account change": {"class_uid": 3001, "activity_name": "Disable", "device": map[string]any{"hostname": "DC1", "os": map[string]any{"type_id": 100}},
			"user": map[string]any{"name": "jdoe", "domain": "CONTOSO"}, "actor": map[string]any{"user": map[string]any{"name": "admin", "domain": "CONTOSO"}}},
		"user access": {"class_uid": 3005, "user": map[string]any{"name": "jdoe", "domain": "CONTOSO"}, "privileges": []any{"SeDebugPrivilege"}},
		"email file": {"class_uid": 4011, "actor": map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO"}},
			"device": map[string]any{"hostname": "WS1"}, "file": map[string]any{"name": "a.docx"}},
		"email url": {"class_uid": 4012, "actor": map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO"}},
			"device": map[string]any{"hostname": "WS1"}, "url": map[string]any{"url_string": "https://x.example/a"}},
		"cloud inventory": {"class_uid": 5023, "cloud": map[string]any{"provider": "AWS", "account": map[string]any{"uid": "111122223333"}},
			"resources": []any{map[string]any{"uid": "arn:aws:s3:::b", "name": "b", "owner": map[string]any{"uid": "AIDAEXAMPLE", "name": "deploy"}}}},
		"registry key": {"class_uid": 201001, "activity_name": "Create", "device": map[string]any{"hostname": "WS1", "os": map[string]any{"type_id": 100}},
			"actor":   map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO"}, "process": map[string]any{"pid": 4, "name": "reg.exe"}},
			"reg_key": map[string]any{"path": `HKEY_LOCAL_MACHINE\SOFTWARE\Run`}},
		"device owner": {"class_uid": 5001, "device": map[string]any{"hostname": "WS1", "os": map[string]any{"type_id": 100, "name": "Windows 11"},
			"owner": map[string]any{"name": "jdoe", "domain": "CONTOSO"}}},
		"cloud owner": {"class_uid": 5023, "cloud": map[string]any{"provider": "AWS"},
			"resources": []any{map[string]any{"uid": "arn:aws:s3:::c", "owner": map[string]any{"uid": "AIDAOWNER", "name": "ops"}}}},
	} {
		evt := makeEvent(timed(p))
		if _, err := testRegistry().Normalize(evt); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = evt
	}
	return out
}

// mintingClasses are the classes whose normalizer mints a user's account.
var mintingClasses = []int{1001, 1006, 1007, 2004, 3001, 3002, 3005, 3006, 4011, 4012, 5001, 5003, 5023, 6003, 201001, 201002}

// TestUserAccounts_EveryMintingClass (adr/0398): every class that mints a
// user's account records it under the user object's place. Every fixture
// event is run, with one event for each minting class no fixture holds.
//
// A place names a user object in the payload, and each account it lists is
// in object_refs, once. Each user-account in object_refs is listed at some
// place, so no user object that minted an account is left out.
func TestUserAccounts_EveryMintingClass(t *testing.T) {
	reg := testRegistry()
	events := loadFixtureEvents(t)
	for name, evt := range handEvents(t) {
		events[name] = evt
	}
	recorded := map[int]bool{}
	for name, evt := range events {
		res, err := reg.Normalize(evt)
		if err != nil {
			continue // a fixture of a broken record; its own tests cover it
		}
		accounts := userAccountsOf(res)
		for place, refs := range accounts {
			// A cloud caller named by its session issuer alone is recorded at
			// its actor.user, the event's or an artifact's, with no user
			// object there.
			if ocsf.ObjectAt(evt.Payload, place) == nil && !strings.HasSuffix(place, "actor.user") {
				t.Errorf("%s: %s names no object in the payload", name, place)
			}
			if len(refs) == 0 || len(slices.Compact(slices.Sorted(slices.Values(refs)))) != len(refs) {
				t.Errorf("%s: %s lists %v; want each account once", name, place, refs)
			}
			for _, ref := range refs {
				if !refsContain(res.ObservedData[0].ObjectRefs, identity.STIXID(ref)) {
					t.Errorf("%s: %s records %s, which is not in object_refs", name, place, ref)
				}
			}
		}
		var minted []string
		for _, r := range res.ObservedData[0].ObjectRefs {
			if s, ok := scoByID(res.SCOs, r); ok && s.Type == identity.TypeUserAccount {
				minted = append(minted, string(r))
			}
		}
		if len(minted) > 0 {
			recorded[evt.ClassUID] = true
		}
		for _, ref := range minted {
			found := false
			for _, v := range accounts {
				found = found || slices.Contains(v, ref)
			}
			if !found {
				t.Errorf("%s (class %d): account %s is recorded at no place: %v", name, evt.ClassUID, ref, accounts)
			}
		}
	}
	for _, c := range mintingClasses {
		if !recorded[c] {
			t.Errorf("class %d: no event minted a user's account", c)
		}
	}
}

// TestUserAccounts_EveryShownUser (adr/0398): every user object the event
// view shows has an entry in user_accounts, for every class the view
// renders. A user object of placeholders only shows nothing and needs none.
// So the view never shows a user unlinked because its class minted no
// account for it.
func TestUserAccounts_EveryShownUser(t *testing.T) {
	reg := testRegistry()
	events := loadFixtureEvents(t)
	for name, evt := range handEvents(t) {
		events[name] = evt
	}
	rendered := map[int]bool{}
	for _, c := range ocsfview.Classes() {
		rendered[c] = true
	}
	for name, evt := range events {
		if !rendered[evt.ClassUID] {
			continue
		}
		res, err := reg.Normalize(evt)
		if err != nil {
			continue // a fixture of a broken record; its own tests cover it
		}
		accounts := userAccountsOf(res)
		v := ocsfview.Render(ocsfview.Event{ClassUID: evt.ClassUID, Payload: evt.Payload}, nil, accounts)
		for _, u := range v.Users {
			if len(accounts[u.Place]) == 0 {
				t.Errorf("%s (class %d): the view shows %q at %s, which has no entry: %v", name, evt.ClassUID, u.Text, u.Place, accounts)
			}
		}
	}
}

// TestUserAccounts_PayloadIndex (adr/0398): a list entry's place carries
// its index in the payload, entries that are not objects counted, and only
// the strong account is recorded.
func TestUserAccounts_PayloadIndex(t *testing.T) {
	reg := testRegistry()
	const sid = "S-1-5-21-7-1101"
	res, err := reg.Normalize(makeEvent(timed(map[string]any{
		"class_uid": 3006, "activity_name": "Add User", "group": map[string]any{"name": "G"},
		"users": []any{"not a user", map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": sid}},
	})))
	if err != nil {
		t.Fatal(err)
	}
	strong := testRegistry().Identity().Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: sid})
	if got := userAccountsOf(res); len(got) != 1 || !slices.Equal(got["users[1]"], []string{string(strong)}) {
		t.Errorf("group change user_accounts = %v; want users[1] → the SID account %s", got, strong)
	}

	res, err = reg.Normalize(makeEvent(timed(map[string]any{
		"class_uid": 2004, "finding_info": map[string]any{"uid": "d-1", "title": "T"},
		"evidences": []any{"not an artifact", map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": sid}}},
	})))
	if err != nil {
		t.Fatal(err)
	}
	if got := userAccountsOf(res); len(got) != 1 || !slices.Equal(got["evidences[1].user"], []string{string(strong)}) {
		t.Errorf("detection user_accounts = %v; want evidences[1].user → %s", got, strong)
	}

	res, err = reg.Normalize(makeEvent(timed(map[string]any{
		"class_uid": 5023, "cloud": map[string]any{"provider": "AWS"},
		"resources": []any{7, map[string]any{"uid": "arn:aws:s3:::b", "owner": map[string]any{"uid": "AIDAEXAMPLE"}}},
	})))
	if err != nil {
		t.Fatal(err)
	}
	owner := testRegistry().Identity().Account(identity.AccountIdentity{Type: "aws", UserID: "AIDAEXAMPLE"})
	if got := userAccountsOf(res); len(got) != 1 || !slices.Equal(got["resources[1].owner"], []string{string(owner)}) {
		t.Errorf("cloud inventory user_accounts = %v; want resources[1].owner → %s", got, owner)
	}
}

// TestUserAccounts_DirectoryRecord (adr/0398): a directory record's one user
// object mints the sign-in account and the domain account, two strong
// accounts. Both are listed at user, the sign-in account first.
func TestUserAccounts_DirectoryRecord(t *testing.T) {
	const oid, sid = "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c", "S-1-5-21-1001"
	reg := testRegistry()
	res, err := reg.Normalize(makeEvent(timed(map[string]any{
		"class_uid": 5003, "metadata": map[string]any{"product": map[string]any{"name": "Okta"}},
		"user":     map[string]any{"uid": oid, "uid_alt": sid, "name": "jdoe@contoso.example", "full_name": "Jo Doe", "domain": "CONTOSO"},
		"unmapped": map[string]any{"on_premises_sam_account_name": "jdoe"},
	})))
	if err != nil {
		t.Fatal(err)
	}
	cloud := reg.Identity().Account(identity.AccountIdentity{Type: identity.AccountCloud, UserID: oid})
	domain := reg.Identity().Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: sid})
	if got := userAccountsOf(res); !slices.Equal(got["user"], []string{string(cloud), string(domain)}) {
		t.Errorf("user_accounts = %v; want user → [%s %s]", got, cloud, domain)
	}
}

// renderNormalized normalizes p and renders it with the result's entities
// and user_accounts, as the evidence store hands them to the view.
func renderNormalized(t *testing.T, p map[string]any) (ocsfview.View, *Registry) {
	t.Helper()
	reg := testRegistry()
	evt := makeEvent(timed(p))
	res, err := reg.Normalize(evt)
	if err != nil {
		t.Fatal(err)
	}
	ents := make([]ocsfview.Entity, 0, len(res.SCOs))
	for _, s := range res.SCOs {
		b, err := stix.Marshal(s.STIXObject())
		if err != nil {
			t.Fatal(err)
		}
		ents = append(ents, ocsfview.EntityFromObject(string(s.ID), s.Type, b))
	}
	return ocsfview.Render(ocsfview.Event{ClassUID: evt.ClassUID, Payload: p}, ents, userAccountsOf(res)), reg
}

// accountRows are the view's Account rows as "value ref".
func accountRows(v ocsfview.View) []string {
	var out []string
	for _, f := range v.Fields {
		if f.Label == "Account" {
			out = append(out, f.Value+" "+f.Ref)
		}
	}
	return out
}

// TestView_SharedGUIDMembers (#398): a cloud member and an LDAP member of
// one group change carry one GUID. The normalizer mints two accounts, and
// the view shows two members, each linked to its own account.
func TestView_SharedGUIDMembers(t *testing.T) {
	const guid = "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c"
	v, reg := renderNormalized(t, map[string]any{
		"class_uid": 3006, "activity_name": "Add User", "group": map[string]any{"name": "G"},
		"users": []any{
			map[string]any{"name": "alice@contoso.com", "uid": guid, "account": map[string]any{"type_id": 6}},
			map[string]any{"name": "alice", "uid": guid, "account": map[string]any{"type_id": 1}},
		},
	})
	cloud := reg.Identity().Account(identity.AccountIdentity{Type: identity.AccountCloud, UserID: guid})
	ldap := reg.Identity().Account(identity.AccountIdentity{Type: identity.AccountLDAP, UserID: guid})
	want := []string{"alice@contoso.com " + string(cloud), "alice " + string(ldap)}
	if got := accountRows(v); !slices.Equal(got, want) {
		t.Errorf("Account rows = %v; want %v", got, want)
	}
}

// TestView_MemberWithNoType (adr/0398): a member that names no account
// type links to the account the normalizer minted for it.
func TestView_MemberWithNoType(t *testing.T) {
	v, reg := renderNormalized(t, map[string]any{
		"class_uid": 3006, "activity_name": "Add User", "group": map[string]any{"name": "G"},
		"device": map[string]any{"hostname": "DC1", "os": map[string]any{"type_id": 100}},
		"users":  []any{map[string]any{"name": "jdoe", "domain": "CONTOSO"}},
	})
	acct := reg.Identity().Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, Login: `CONTOSO\jdoe`})
	if got := accountRows(v); !slices.Equal(got, []string{`CONTOSO\jdoe ` + string(acct)}) {
		t.Errorf("Account rows = %v; want CONTOSO\\jdoe linked to %s", got, acct)
	}
}

// TestView_UnixActorWithDomain (adr/0398): a Unix account is stored under
// its bare name, whatever domain the record names. An actor named with a
// domain on a Linux device links to it.
func TestView_UnixActorWithDomain(t *testing.T) {
	p := map[string]any{
		"class_uid": 1007, "activity_name": "Launch",
		"device":  map[string]any{"hostname": "web01", "os": map[string]any{"type_id": 200}},
		"actor":   map[string]any{"user": map[string]any{"name": "deploy", "domain": "CORP"}},
		"process": map[string]any{"name": "sh", "pid": 7},
	}
	v, reg := renderNormalized(t, p)
	host := reg.Identity().Host(identity.HostIdentity{Hostname: "web01"})
	acct := reg.Identity().Account(identity.AccountIdentity{Type: identity.AccountUnix, Login: "deploy", HostRef: host})
	if f := fieldNamed(v, "User"); f.Ref != string(acct) {
		t.Errorf("User = %+v; want linked to the unix account %s", f, acct)
	}
}

func fieldNamed(v ocsfview.View, label string) ocsfview.Field {
	for _, f := range v.Fields {
		if f.Label == label {
			return f
		}
	}
	return ocsfview.Field{}
}

// TestView_ScheduledJobActorIsRunAs (adr/0398): a scheduled job whose actor
// is also the account it runs as links both chips to that one account.
func TestView_ScheduledJobActorIsRunAs(t *testing.T) {
	const sid = "S-1-5-21-7-2002"
	user := map[string]any{"name": "svc_backup", "domain": "CONTOSO", "uid": sid}
	v, reg := renderNormalized(t, map[string]any{
		"class_uid": 1006, "activity_name": "Create",
		"device": map[string]any{"hostname": "WIN-FILE01", "os": map[string]any{"type_id": 100}},
		"actor":  map[string]any{"user": user},
		"job":    map[string]any{"name": "Updater", "user": user, "cmd_line": "u.exe"},
	})
	acct := string(reg.Identity().Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: sid}))
	var chips []string
	for _, tok := range v.Sentence {
		if strings.HasPrefix(tok.Ref, identity.TypeUserAccount+"--") {
			chips = append(chips, tok.Ref)
		}
	}
	if !slices.Equal(chips, []string{acct, acct}) {
		t.Errorf("user chips = %v; want the actor and the run-as user both linked to %s", chips, acct)
	}
	if f := fieldNamed(v, "Actor"); f.Ref != acct {
		t.Errorf("Actor = %+v; want linked to %s", f, acct)
	}
	if f := fieldNamed(v, "Runs as"); f.Ref != acct {
		t.Errorf("Runs as = %+v; want linked to %s", f, acct)
	}
}
