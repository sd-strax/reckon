package capability

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/identity"
)

// toolEvent is an event of class from tool with payload.
func toolEvent(tool string, class int, payload map[string]any) OcsfEvent {
	payload["class_uid"] = class
	return OcsfEvent{
		ID: uuid.Must(uuid.NewV7()), ClassUID: class, Time: time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC),
		RecordedAt: time.Now(), SourceTool: tool, Payload: payload,
	}
}

// relsOf lists the relationships of typ in res as source → target pairs.
func relsOf(res NormalizationResult, typ string) [][2]identity.STIXID {
	var out [][2]identity.STIXID
	for _, r := range res.Relationships {
		if r.Type == typ {
			out = append(out, [2]identity.STIXID{r.SourceRef, r.TargetRef})
		}
	}
	return out
}

// accountsIn lists the user-accounts among res's object_refs.
func accountsIn(res NormalizationResult) []identity.STIXID {
	var out []identity.STIXID
	for _, r := range res.ObservedData[0].ObjectRefs {
		if s, ok := scoByID(res.SCOs, r); ok && s.Type == identity.TypeUserAccount {
			out = append(out, r)
		}
	}
	return out
}

// TestAccountIdentityConformance is the account identity conformance check
// (adr/0324, `make conformance`): one person as three sources name them (a
// domain controller's logon with the SID and CONTOSO\sam, an endpoint's bare
// login, an identity provider's sign-in by principal name and provider id)
// normalizes to the accounts the rule yields, each record aliasing its own
// lesser identifiers to its strongest and the bare login joining nothing;
// the directory record that names the person then mints one person owning
// the SID account, the principal name account, the sam account and the
// sign-in account, each by a DIRECT belongs-to, and the bare windows account
// stays unowned, since nothing named it. An adapter for a live source is
// held to the same properties over its own captures.
func TestAccountIdentityConformance(t *testing.T) {
	reg := testRegistry()
	ids := reg.Identity()
	const (
		sid      = "S-1-5-21-1001"
		upn      = "jdoe@contoso.example"
		objectID = "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c"
	)
	acct := func(typ, login, uid string) identity.STIXID {
		return ids.Account(identity.AccountIdentity{Type: typ, Login: login, UserID: uid})
	}
	sidAcct := acct(identity.AccountWindowsDomain, "", sid)
	samAcct := acct(identity.AccountWindowsDomain, `CONTOSO\jdoe`, "")
	upnAcct := acct(identity.AccountWindowsDomain, upn, "")
	bareAcct := acct(identity.AccountWindows, "jdoe", "")
	cloudAcct := acct(identity.AccountCloud, "", objectID)
	cloudName := acct(identity.AccountCloud, upn, "")
	windows := map[string]any{"type": "Windows", "type_id": 100}

	// 1. The domain controller: SID beside CONTOSO\jdoe.
	dc, err := reg.Normalize(toolEvent("dc", 3002, map[string]any{
		"actor":        map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": sid}},
		"dst_endpoint": map[string]any{"hostname": "WIN-DC01", "os": windows},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := accountsIn(dc); len(got) != 1 || got[0] != sidAcct {
		t.Fatalf("dc logon object_refs accounts = %v; want the SID account %s", got, sidAcct)
	}
	if got := relsOf(dc, RelAliases); len(got) != 1 || got[0] != [2]identity.STIXID{samAcct, sidAcct} {
		t.Errorf("dc logon aliases = %v; want CONTOSO\\jdoe → SID", got)
	}
	if s, ok := scoByID(dc.SCOs, sidAcct); !ok || s.Properties["account_type"] != identity.AccountWindowsDomain ||
		s.Properties["account_login"] != `CONTOSO\jdoe` || s.Properties["user_id"] != sid {
		t.Errorf("SID account = %+v", s)
	}
	if s, ok := scoByID(dc.SCOs, samAcct); !ok || refsContain(dc.ObservedData[0].ObjectRefs, samAcct) {
		t.Errorf("the sam account %s is missing or in object_refs (%+v)", samAcct, s)
	}

	// 2. The endpoint: a bare login on a Windows machine, the weak account.
	edr, err := reg.Normalize(toolEvent("edr", 1007, map[string]any{
		"process": map[string]any{"pid": 42, "name": "cmd.exe"},
		"device":  map[string]any{"hostname": "WS1", "os": windows},
		"actor":   map[string]any{"user": map[string]any{"name": "jdoe"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := accountsIn(edr); len(got) != 1 || got[0] != bareAcct {
		t.Fatalf("endpoint accounts = %v; want the bare windows account %s", got, bareAcct)
	}
	if s, _ := scoByID(edr.SCOs, bareAcct); s.Properties["weak"] != true || s.Properties["account_type"] != identity.AccountWindows {
		t.Errorf("bare account = %+v; want weak, windows", s)
	}
	if got := relsOf(edr, RelAliases); len(got) != 0 {
		t.Errorf("the bare login joined something by itself: %v", got)
	}
	if slices.Contains([]identity.STIXID{sidAcct, samAcct, upnAcct}, bareAcct) {
		t.Error("the bare login keyed into a domain account")
	}

	// 3. The identity provider: the provider id beside the principal name.
	idp, err := reg.Normalize(toolEvent("idp", 3002, map[string]any{
		"metadata": map[string]any{"product": map[string]any{"vendor_name": "fixture-idp"}},
		"actor": map[string]any{"user": map[string]any{
			"name": upn, "uid": objectID, "account": map[string]any{"type_id": 6, "type": "Azure AD Account"},
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := accountsIn(idp); len(got) != 1 || got[0] != cloudAcct {
		t.Fatalf("idp accounts = %v; want the cloud account %s", got, cloudAcct)
	}
	if got := relsOf(idp, RelAliases); len(got) != 1 || got[0] != [2]identity.STIXID{cloudName, cloudAcct} {
		t.Errorf("idp aliases = %v; want sign-in name → provider id", got)
	}
	if s, _ := scoByID(idp.SCOs, cloudAcct); s.Properties["provider"] != "fixture-idp" || s.Properties["principal_name"] != upn {
		t.Errorf("cloud account = %+v", s)
	}

	// 4. The directory record names the person and every identifier but the
	// bare login.
	dir, err := reg.Normalize(toolEvent("idp", 5003, map[string]any{
		"metadata": map[string]any{"product": map[string]any{"vendor_name": "fixture-idp"}},
		"user": map[string]any{
			"uid": objectID, "uid_alt": sid, "name": upn, "email_addr": upn, "full_name": "Jo Doe", "domain": "CONTOSO",
			"groups": []any{map[string]any{"name": "finance-users"}},
		},
		"unmapped": map[string]any{"on_premises_sam_account_name": "jdoe", "on_premises_domain_name": "CONTOSO"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	person := ids.Person("fixture-idp", objectID)
	p, ok := scoByID(dir.SCOs, person)
	if !ok || p.Type != identity.TypeIdentity || p.Properties["identity_class"] != "individual" || p.Properties["principal_name"] != upn ||
		p.Properties["mail"] != upn || p.Properties["directory"] != "fixture-idp" || p.Properties["directory_id"] != objectID {
		t.Fatalf("person = %+v (found %v)", p, ok)
	}
	if !refsContain(dir.ObservedData[0].ObjectRefs, person) {
		t.Error("the person is not in the directory record's object_refs")
	}
	owned := map[identity.STIXID]bool{}
	for _, r := range dir.Relationships {
		if r.Type == RelBelongsTo && r.TargetRef == person {
			owned[r.SourceRef] = r.Provenance.DerivationMode == DerivationDirect
		}
	}
	for name, a := range map[string]identity.STIXID{"SID": sidAcct, "principal name": upnAcct, "sam": samAcct, "sign-in": cloudAcct, "sign-in name": cloudName} {
		if !owned[a] {
			t.Errorf("the %s account %s has no DIRECT belongs-to the person", name, a)
		}
	}
	if _, ok := owned[bareAcct]; ok {
		t.Error("the bare windows account is owned, though nothing named it")
	}
	// The SID account the directory mints carries its key as user_id, so a
	// lookup or a seen-as match by the SID finds it before any logon does.
	if s, _ := scoByID(dir.SCOs, sidAcct); s.Properties["user_id"] != sid {
		t.Errorf("the directory's SID account = %+v; want user_id %s", s, sid)
	}
	wantAliases := [][2]identity.STIXID{{upnAcct, sidAcct}, {samAcct, sidAcct}, {cloudName, cloudAcct}}
	if got := relsOf(dir, RelAliases); !sameRels(got, wantAliases) {
		t.Errorf("directory aliases = %v; want %v", got, wantAliases)
	}
	if got := relsOf(dir, RelMemberOfGroup); len(got) != 1 || got[0][0] != cloudAcct {
		t.Errorf("group memberships = %v; want one from the sign-in account", got)
	}

	// Events never reference a person.
	for _, res := range []NormalizationResult{dc, edr, idp} {
		for _, s := range res.SCOs {
			if s.Type == identity.TypeIdentity {
				t.Errorf("an event minted a person: %+v", s)
			}
		}
	}
}

func sameRels(got, want [][2]identity.STIXID) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			return false
		}
	}
	return true
}

// TestAccountShapingTypes (adr/0324): the type each source's record reads
// as, and what keys it.
func TestAccountShapingTypes(t *testing.T) {
	reg := testRegistry()
	ids := reg.Identity()
	one := func(t *testing.T, payload map[string]any) (NormalizationResult, SCO) {
		t.Helper()
		res, err := reg.Normalize(toolEvent("edr", 1007, payload))
		if err != nil {
			t.Fatal(err)
		}
		a := accountsIn(res)
		if len(a) != 1 {
			t.Fatalf("accounts = %v; want one", a)
		}
		s, _ := scoByID(res.SCOs, a[0])
		return res, s
	}
	ws1 := ids.Host(identity.HostIdentity{Hostname: "ws1"})

	t.Run("principal name from a Windows source is a domain account", func(t *testing.T) {
		_, s := one(t, map[string]any{"device": map[string]any{"hostname": "WS1"}, "actor": map[string]any{"user": map[string]any{"name": "jdoe@contoso.example", "uid": "S-1-5-21-9"}}})
		if s.Properties["account_type"] != identity.AccountWindowsDomain || s.ID != ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: "S-1-5-21-9"}) {
			t.Errorf("account = %+v", s)
		}
	})
	t.Run("an @ with no source signal is no cloud account", func(t *testing.T) {
		_, s := one(t, map[string]any{"device": map[string]any{"hostname": "WS1"}, "actor": map[string]any{"user": map[string]any{"name": "jdoe@contoso.example"}}})
		if s.Properties["account_type"] != identity.AccountWindowsDomain {
			t.Errorf("account = %+v; want the principal name as a domain account", s)
		}
	})
	t.Run("the machine's own domain is windows-local", func(t *testing.T) {
		_, s := one(t, map[string]any{"device": map[string]any{"hostname": "WS1"}, "actor": map[string]any{"user": map[string]any{"name": "Administrator", "domain": "WS1", "uid": "S-1-5-21-77-500"}}})
		if s.Properties["account_type"] != identity.AccountWindowsLocal || s.ID != ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsLocal, Login: "administrator", HostRef: ws1}) {
			t.Errorf("account = %+v", s)
		}
	})
	t.Run("a well-known principal is per host by its SID, its name aliased", func(t *testing.T) {
		res, s := one(t, map[string]any{"device": map[string]any{"hostname": "WS1"}, "actor": map[string]any{"user": map[string]any{"name": "SYSTEM", "domain": "NT AUTHORITY", "uid": "S-1-5-18"}}})
		want := ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsLocal, UserID: "S-1-5-18", HostRef: ws1})
		if s.ID != want {
			t.Errorf("account = %+v; want %s", s, want)
		}
		name := ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsLocal, Login: "system", HostRef: ws1})
		if got := relsOf(res, RelAliases); len(got) != 1 || got[0] != [2]identity.STIXID{name, want} {
			t.Errorf("aliases = %v", got)
		}
	})
	t.Run("unix is per host", func(t *testing.T) {
		_, s := one(t, map[string]any{"device": map[string]any{"hostname": "WS1", "os": map[string]any{"type_id": 200}}, "actor": map[string]any{"user": map[string]any{"name": "root", "uid": "0"}}})
		if s.Properties["account_type"] != identity.AccountUnix || s.ID != ids.Account(identity.AccountIdentity{Type: identity.AccountUnix, Login: "root", HostRef: ws1}) {
			t.Errorf("account = %+v", s)
		}
	})
	t.Run("an IIS application pool is per host", func(t *testing.T) {
		var got []identity.STIXID
		for _, h := range []string{"WEB1", "WEB2"} {
			_, s := one(t, map[string]any{"device": map[string]any{"hostname": h}, "actor": map[string]any{"user": map[string]any{"name": "DefaultAppPool", "domain": "IIS APPPOOL"}}})
			if s.Properties["account_type"] != identity.AccountWindowsLocal {
				t.Errorf("%s: account = %+v", h, s)
			}
			got = append(got, s.ID)
		}
		if got[0] == got[1] {
			t.Error("two hosts' application pools are one account")
		}
	})
	t.Run("a machine-local domain on a Unix device is unix", func(t *testing.T) {
		_, s := one(t, map[string]any{"device": map[string]any{"hostname": "WS1", "os": map[string]any{"type": "Linux"}}, "actor": map[string]any{"user": map[string]any{"name": "svc", "domain": "WS1"}}})
		if s.Properties["account_type"] != identity.AccountUnix || s.ID != ids.Account(identity.AccountIdentity{Type: identity.AccountUnix, Login: "svc", HostRef: ws1}) {
			t.Errorf("account = %+v", s)
		}
	})
	t.Run("an identity provider's sign-in without an account type is cloud", func(t *testing.T) {
		res, err := reg.Normalize(toolEvent("idp", 3002, map[string]any{
			"metadata": map[string]any{"product": map[string]any{"vendor_name": "Okta"}},
			"actor":    map[string]any{"user": map[string]any{"name": "jdoe@contoso.example", "uid": "00u1AbCd"}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		strong := ids.Account(identity.AccountIdentity{Type: identity.AccountCloud, UserID: "00u1AbCd"})
		name := ids.Account(identity.AccountIdentity{Type: identity.AccountCloud, Login: "jdoe@contoso.example"})
		if a := accountsIn(res); len(a) != 1 || a[0] != strong {
			t.Errorf("accounts = %v; want the cloud account %s", a, strong)
		}
		if got := relsOf(res, RelAliases); len(got) != 1 || got[0] != [2]identity.STIXID{name, strong} {
			t.Errorf("aliases = %v", got)
		}
	})
	t.Run("a managed domain service naming a SID is no identity provider", func(t *testing.T) {
		res, err := reg.Normalize(toolEvent("aadds", 3002, map[string]any{
			"metadata": map[string]any{"product": map[string]any{"vendor_name": "Microsoft", "name": "Microsoft Entra Domain Services"}},
			"actor":    map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": "S-1-5-21-7-1101"}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		want := ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: "S-1-5-21-7-1101"})
		if a := accountsIn(res); len(a) != 1 || a[0] != want {
			t.Errorf("accounts = %v; want the domain SID account %s", a, want)
		}
		// Named whole: a product that only mentions a provider is not one.
		res, err = reg.Normalize(toolEvent("siem", 3002, map[string]any{
			"metadata": map[string]any{"product": map[string]any{"vendor_name": "Acme", "name": "Acme SIEM for Okta"}},
			"actor":    map[string]any{"user": map[string]any{"name": "jdoe@contoso.example", "uid": "00u1AbCd"}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if a := accountsIn(res); len(a) != 1 {
			t.Fatalf("accounts = %v", a)
		} else if s, _ := scoByID(res.SCOs, a[0]); s.Properties["account_type"] == identity.AccountCloud {
			t.Errorf("a product naming a provider read as the provider: %+v", s)
		}
	})
	t.Run("an identity provider's log product is the provider by its vendor", func(t *testing.T) {
		okta := map[string]any{"product": map[string]any{"vendor_name": "Okta", "name": "Okta System Log"}}
		res, err := reg.Normalize(toolEvent("okta", 3002, map[string]any{
			"metadata": okta,
			// A principal name beside the tenant's DNS domain is no domain\sam.
			"actor": map[string]any{"user": map[string]any{"name": "jdoe@contoso.example", "domain": "contoso.example", "uid": "00u1AbCd"}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		want := ids.Account(identity.AccountIdentity{Type: identity.AccountCloud, UserID: "00u1AbCd"})
		if a := accountsIn(res); len(a) != 1 || a[0] != want {
			t.Errorf("accounts = %v; want the cloud account %s", a, want)
		}
		if got := directorySlug(map[string]any{"metadata": okta}, "okta"); got != "okta" {
			t.Errorf("slug = %q; want okta", got)
		}
		aadds := map[string]any{"metadata": map[string]any{"product": map[string]any{"vendor_name": "Microsoft", "name": "Microsoft Entra Domain Services"}}}
		if identityProviderSource(aadds) || directorySlug(aadds, "t") != "microsoft-entra-domain-services" {
			t.Errorf("Entra Domain Services: idp %v, slug %q; want no idp, its own slug", identityProviderSource(aadds), directorySlug(aadds, "t"))
		}
	})
	t.Run("a provider's agent on a machine is no identity provider", func(t *testing.T) {
		meta := map[string]any{"product": map[string]any{"vendor_name": "Okta", "name": "Okta Advanced Server Access"}}
		res, err := reg.Normalize(toolEvent("asa", 3002, map[string]any{
			"metadata":     meta,
			"dst_endpoint": map[string]any{"hostname": "WS1", "os": map[string]any{"type": "Linux"}},
			"actor":        map[string]any{"user": map[string]any{"name": "jdoe"}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		want := ids.Account(identity.AccountIdentity{Type: identity.AccountUnix, Login: "jdoe", HostRef: ws1})
		if a := accountsIn(res); len(a) != 1 || a[0] != want {
			t.Errorf("accounts = %v; want the host-scoped unix account %s", a, want)
		}
		if got := directorySlug(map[string]any{"metadata": meta}, "asa"); got != "okta-advanced-server-access" {
			t.Errorf("slug = %q; want the product's", got)
		}
		// The vendor alone, with a device OS: the slug, not the provider mark.
		vendorOnly := map[string]any{"metadata": map[string]any{"product": map[string]any{"vendor_name": "Okta"}},
			"device": map[string]any{"os": map[string]any{"type_id": 200}}}
		if identityProviderSource(vendorOnly) || directorySlug(vendorOnly, "t") != "okta" {
			t.Errorf("vendor with a device OS: idp %v, slug %q", identityProviderSource(vendorOnly), directorySlug(vendorOnly, "t"))
		}
	})
	t.Run("an explicit Unix signal outweighs the domain", func(t *testing.T) {
		ws2 := ids.Host(identity.HostIdentity{Hostname: "ws2"})
		linux := map[string]any{"type": "Linux"}
		var got []identity.STIXID
		for _, h := range []string{"WS1", "WS2"} {
			_, s := one(t, map[string]any{"device": map[string]any{"hostname": h, "os": linux},
				"actor": map[string]any{"user": map[string]any{"name": "svc", "domain": "CORP", "account": map[string]any{"type_id": 9}}}})
			if s.Properties["account_type"] != identity.AccountUnix {
				t.Errorf("%s: account = %+v; want unix", h, s)
			}
			got = append(got, s.ID)
		}
		if got[0] == got[1] || got[1] != ids.Account(identity.AccountIdentity{Type: identity.AccountUnix, Login: "svc", HostRef: ws2}) {
			t.Errorf("accounts = %v; want one per host", got)
		}
		_, s := one(t, map[string]any{"device": map[string]any{"hostname": "WS1", "os": linux},
			"actor": map[string]any{"user": map[string]any{"name": "svc", "account": map[string]any{"type_id": 9}}}})
		if s.ID != got[0] {
			t.Errorf("omitting the domain moved the account: %s, want %s", s.ID, got[0])
		}
	})
	t.Run("a SID outranks a Unix signal", func(t *testing.T) {
		linux := map[string]any{"type": "Linux"}
		_, s := one(t, map[string]any{"device": map[string]any{"hostname": "WS1", "os": linux},
			"actor": map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": "S-1-5-21-7-1101", "account": map[string]any{"type_id": 9}}}})
		if s.Properties["account_type"] != identity.AccountWindowsDomain || s.ID != ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: "S-1-5-21-7-1101"}) {
			t.Errorf("domain SID on a Unix-flagged record = %+v; want the domain SID account", s)
		}
		_, s = one(t, map[string]any{"device": map[string]any{"hostname": "WS1", "os": linux},
			"actor": map[string]any{"user": map[string]any{"name": "SYSTEM", "domain": "NT AUTHORITY", "uid": "S-1-5-18"}}})
		if s.Properties["account_type"] != identity.AccountWindowsLocal || s.ID != ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsLocal, UserID: "S-1-5-18", HostRef: ws1}) {
			t.Errorf("well-known SID on a Unix device = %+v; want windows-local", s)
		}
	})
	t.Run("machine pseudo-domains are per host", func(t *testing.T) {
		for _, d := range []string{"APPLICATION PACKAGE AUTHORITY", "Mandatory Label"} {
			_, s := one(t, map[string]any{"device": map[string]any{"hostname": "WS1"}, "actor": map[string]any{"user": map[string]any{"name": "X", "domain": d}}})
			if s.Properties["account_type"] != identity.AccountWindowsLocal {
				t.Errorf("%s: account = %+v", d, s)
			}
		}
	})
	t.Run("a bare login with no host and no OS is a weak login account", func(t *testing.T) {
		res, err := reg.Normalize(toolEvent("radius", 3002, map[string]any{
			"actor":        map[string]any{"user": map[string]any{"name": "jdoe"}},
			"src_endpoint": map[string]any{"ip": "10.0.4.55"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		want := ids.Account(identity.AccountIdentity{Type: identity.AccountLogin, Login: "jdoe"})
		a := accountsIn(res)
		if len(a) != 1 || a[0] != want {
			t.Fatalf("accounts = %v; want the weak login account %s", a, want)
		}
		if s, _ := scoByID(res.SCOs, want); s.Properties["weak"] != true || s.Properties["account_type"] != identity.AccountLogin {
			t.Errorf("account = %+v", s)
		}
		if got := relsOf(res, RelAliases); len(got) != 0 {
			t.Errorf("the bare login joined something: %v", got)
		}
	})
	t.Run("a placeholder login and SID mint nothing", func(t *testing.T) {
		res, err := reg.Normalize(toolEvent("edr", 1007, map[string]any{"device": map[string]any{"hostname": "WS1"}, "actor": map[string]any{"user": map[string]any{"name": "ANONYMOUS LOGON", "domain": "NT AUTHORITY", "uid": "S-1-5-7"}}}))
		if err != nil {
			t.Fatal(err)
		}
		if got := countTypes(res.SCOs)[identity.TypeUserAccount]; got != 0 {
			t.Errorf("a placeholder minted %d accounts", got)
		}
	})
}

// TestDirectorySlugExact: a directory is scoped by its product named whole:
// a managed domain service that names a provider is its own directory, so
// its person with the same id is not the provider's person.
func TestDirectorySlugExact(t *testing.T) {
	reg := testRegistry()
	ids := reg.Identity()
	const oid = "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c"
	persons := map[string]identity.STIXID{}
	for _, product := range []string{"Microsoft Entra ID", "Microsoft Entra Domain Services"} {
		res, err := reg.Normalize(toolEvent("dir", 5003, map[string]any{
			"metadata": map[string]any{"product": map[string]any{"vendor_name": "Microsoft", "name": product}},
			"user":     map[string]any{"uid": oid, "name": "jdoe@contoso.example"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		persons[product] = scoOfType(res.SCOs, identity.TypeIdentity)
	}
	if persons["Microsoft Entra ID"] != ids.Person("entra", oid) {
		t.Errorf("Entra ID person = %s; want the entra-scoped person", persons["Microsoft Entra ID"])
	}
	if want := ids.Person("microsoft-entra-domain-services", oid); persons["Microsoft Entra Domain Services"] != want || want == persons["Microsoft Entra ID"] {
		t.Errorf("Entra Domain Services person = %s; want its own slug's person %s", persons["Microsoft Entra Domain Services"], want)
	}
	for product, want := range map[string]string{
		"Active Directory Domain Services": "ad", "Workday": "workday", "Okta": "okta", "Google Workspace": "google", "Acme SIEM for Okta": "acme-siem-for-okta",
	} {
		if got := directorySlug(map[string]any{"metadata": map[string]any{"product": map[string]any{"name": product}}}, "t"); got != want {
			t.Errorf("directorySlug(%q) = %q; want %q", product, got, want)
		}
	}
	if got := directorySlug(map[string]any{"metadata": map[string]any{"product": map[string]any{"vendor_name": "Fixture IdP"}}}, "t"); got != "fixture-idp" {
		t.Errorf("vendor fallback = %q", got)
	}
	if got := directorySlug(map[string]any{}, "fixture:dir"); got != "fixture:dir" {
		t.Errorf("tool fallback = %q", got)
	}
}

// TestUserInventoryOnPremises: an on-premises directory keyed by SID mints
// the SID account as the family's strongest, the sam aliased to it, and no
// cloud account.
func TestUserInventoryOnPremises(t *testing.T) {
	reg := testRegistry()
	ids := reg.Identity()
	res, err := reg.Normalize(toolEvent("dc", 5003, map[string]any{
		"metadata": map[string]any{"product": map[string]any{"vendor_name": "Microsoft", "name": "Active Directory"}},
		"user":     map[string]any{"uid": "S-1-5-21-7-1101", "name": "jdoe", "domain": "CONTOSO"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	sid := ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: "S-1-5-21-7-1101"})
	sam := ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, Login: `CONTOSO\jdoe`})
	if a := accountsIn(res); len(a) != 1 || a[0] != sid {
		t.Errorf("accounts = %v; want the SID account %s", a, sid)
	}
	if got := relsOf(res, RelAliases); len(got) != 1 || got[0] != [2]identity.STIXID{sam, sid} {
		t.Errorf("aliases = %v; want sam → SID", got)
	}
	if p, ok := scoByID(res.SCOs, ids.Person("ad", "S-1-5-21-7-1101")); !ok || p.Type != identity.TypeIdentity {
		t.Errorf("no person keyed (ad, SID): %+v", res.SCOs)
	}
}

// TestRelationshipsDedupe: one user as actor and target (privilege-01) names
// its login alias once.
func TestRelationshipsDedupe(t *testing.T) {
	reg := testRegistry()
	res, err := reg.Normalize(makeEvent(readFixtureOCSF(t, "../fixtures/lateral-movement-via-rdp/privilege-01.json")))
	if err != nil {
		t.Fatal(err)
	}
	if got := relsOf(res, RelAliases); len(got) != 1 {
		t.Errorf("aliases = %v; want one", got)
	}
	seen := map[identity.STIXID]bool{}
	for _, r := range res.Relationships {
		if seen[r.ID] {
			t.Errorf("relationship %s recorded twice", r.ID)
		}
		seen[r.ID] = true
	}
}

// TestUserInventoryFixture: the impossible-travel scenario's directory record
// (class 5003) mints one person owning the SID account the scenario's
// sign-ins mint, with the sam account those sign-ins alias to it.
func TestUserInventoryFixture(t *testing.T) {
	ocsf := readFixtureOCSF(t, "../fixtures/impossible-travel-signin/user-inventory-1.json")
	reg := testRegistry()
	ids := reg.Identity()
	evt := makeEvent(ocsf)
	evt.SourceTool = "fixture:impossible-travel-signin"
	res, err := reg.Normalize(evt)
	if err != nil {
		t.Fatal(err)
	}
	person := ids.Person("fixture-directory", "5b0d1c7e-2f4a-4c8e-9a63-0e1f2d3c4b5a")
	p, ok := scoByID(res.SCOs, person)
	if !ok || p.Properties["title"] != "Treasury Analyst" || p.Properties["department"] != "Treasury Operations" || p.Properties["enabled"] != true {
		t.Fatalf("person = %+v (found %v)", p, ok)
	}
	logon := readFixtureOCSF(t, "../fixtures/impossible-travel-signin/logon-office-1.json")
	lres, err := reg.Normalize(makeEvent(logon))
	if err != nil {
		t.Fatal(err)
	}
	signin := accountsIn(lres)
	if len(signin) != 1 {
		t.Fatalf("sign-in accounts = %v", signin)
	}
	var owns bool
	for _, r := range res.Relationships {
		owns = owns || (r.Type == RelBelongsTo && r.SourceRef == signin[0] && r.TargetRef == person)
	}
	if !owns {
		t.Errorf("the directory record does not attach the sign-ins' account %s to the person", signin[0])
	}
}

// TestBareLoginFixture: the lateral-movement scenario's endpoint record with
// a bare login mints the weak windows account, which the domain account of
// the scenario's logons does not join.
func TestBareLoginFixture(t *testing.T) {
	reg := testRegistry()
	res, err := reg.Normalize(makeEvent(readFixtureOCSF(t, "../fixtures/lateral-movement-via-rdp/process-bare-login-01.json")))
	if err != nil {
		t.Fatal(err)
	}
	a := accountsIn(res)
	want := reg.Identity().Account(identity.AccountIdentity{Type: identity.AccountWindows, Login: "jdoe"})
	if len(a) != 1 || a[0] != want {
		t.Fatalf("accounts = %v; want the weak windows account %s", a, want)
	}
	if got := relsOf(res, RelAliases); len(got) != 0 {
		t.Errorf("aliases = %v; want none", got)
	}
}

func readFixtureOCSF(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		OCSF map[string]any `json:"ocsf"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f.OCSF
}
