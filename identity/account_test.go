package identity

import "testing"

// TestAccountRule (adr/0324): each account type keys on what its source
// repeats.
func TestAccountRule(t *testing.T) {
	r := NewResolver(nsA)
	host1 := r.Host(HostIdentity{Hostname: "ws1"})
	host2 := r.Host(HostIdentity{Hostname: "ws2"})
	acct := func(typ, login, uid string, host STIXID) STIXID {
		return r.Account(AccountIdentity{Type: typ, Login: login, UserID: uid, HostRef: host})
	}

	// windows-domain: the SID first, whatever login sits beside it.
	sid := acct(AccountWindowsDomain, `CONTOSO\jdoe`, "S-1-5-21-1001", "")
	if sid != acct(AccountWindowsDomain, "jdoe@contoso.example", "S-1-5-21-1001", "") || sid != acct(AccountWindowsDomain, "", "s-1-5-21-1001", "") {
		t.Error("a SID-keyed account moved with its login")
	}
	// The login keys when there is no SID, and the identifier is no part of it.
	login := acct(AccountWindowsDomain, `CONTOSO\jdoe`, "", "")
	if login == sid || login != acct(AccountWindowsDomain, `contoso\JDOE`, "not-a-sid", "") {
		t.Error("a login-keyed account depends on a non-SID identifier, or met the SID account")
	}
	upn := acct(AccountWindowsDomain, "JDoe@Contoso.Example", "", "")
	if upn == login || upn != acct(AccountWindowsDomain, "jdoe@contoso.example", "", "") {
		t.Error("the principal name is not its own folded key")
	}
	// A placeholder SID or login reads as absent.
	if acct(AccountWindowsDomain, `CONTOSO\jdoe`, "S-1-0-0", "") != login || acct(AccountWindowsDomain, "", "S-1-5-7", "") != "" ||
		acct(AccountWindowsDomain, "ANONYMOUS LOGON", "", "") != "" || acct(AccountWindowsDomain, "-", "-", "") != "" {
		t.Error("a placeholder SID or login was used as a key")
	}

	// windows: the bare login, never the domain account's key.
	bare := acct(AccountWindows, "JDOE", "", "")
	if bare == "" || bare == login || bare != acct(AccountWindows, "jdoe", "", "") {
		t.Error("the weak windows account is not its own folded key")
	}

	// windows-local and unix: per host.
	if acct(AccountWindowsLocal, `WS1\Administrator`, "", host1) == acct(AccountWindowsLocal, `WS2\administrator`, "", host2) {
		t.Error("two machines' local administrators are one account")
	}
	if acct(AccountWindowsLocal, `WS1\Administrator`, "", host1) != acct(AccountWindowsLocal, "administrator", "", host1) {
		t.Error("the local account's key carries the machine name beside the host")
	}
	system1 := acct(AccountWindowsLocal, `NT AUTHORITY\SYSTEM`, "S-1-5-18", host1)
	if system1 == "" || system1 == acct(AccountWindowsLocal, `NT AUTHORITY\SYSTEM`, "S-1-5-18", host2) ||
		system1 != acct(AccountWindowsLocal, "", "S-1-5-18", host1) {
		t.Error("a well-known principal is not per host by its SID")
	}
	if acct(AccountWindowsLocal, "admin", "", "") != "" || acct(AccountUnix, "root", "", "") != "" {
		t.Error("a host-scoped account was minted with no host")
	}
	if acct(AccountUnix, "root", "", host1) == acct(AccountUnix, "root", "", host2) {
		t.Error("root on two hosts is one account")
	}

	// cloud: the provider id, else the sign-in name.
	cloudID := acct(AccountCloud, "jdoe@contoso.example", "8F6A0B5C-1D2E-4F3A-9B8C-7D6E5F4A3B2C", "")
	if cloudID != acct(AccountCloud, "", "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c", "") {
		t.Error("the cloud account moved with its sign-in name")
	}
	if cloudName := acct(AccountCloud, "jdoe@contoso.example", "", ""); cloudName == cloudID || cloudName == upn {
		t.Error("a name-keyed cloud account met the id-keyed one or the domain principal name")
	}

	// A non-GUID provider id keeps its case, as a person's directory id does.
	if acct(AccountCloud, "", "00uAbC", "") == acct(AccountCloud, "", "00uabc", "") {
		t.Error("a non-GUID cloud id was case-folded")
	}

	// login: the weak bare login of a source that names no OS and no host.
	weakLogin := acct(AccountLogin, "JDOE", "", "")
	if weakLogin == "" || weakLogin != acct(AccountLogin, "jdoe", "", "") || weakLogin == bare {
		t.Error("the login account is not its own folded key")
	}

	// A control-plane principal keeps its historical key.
	want := r.mint(TypeUserAccount, map[string]any{"account_login": "", "user_id": "arn:aws:iam::1:role/deploy", "account_type": "aws"})
	if got := acct("aws", "", "arn:aws:iam::1:role/Deploy", ""); got != want {
		t.Errorf("control-plane principal = %s; want %s", got, want)
	}
}

// TestPerson (adr/0324): a person is keyed by directory and id, never takes
// a vendor identity's id, and a placeholder id names no one.
func TestPerson(t *testing.T) {
	r := NewResolver(nsA)
	p := r.Person(" Entra ", "8F6A0B5C-1D2E-4F3A-9B8C-7D6E5F4A3B2C")
	if p == "" || p != r.Person("entra", "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c") {
		t.Error("the person does not fold its directory and GUID")
	}
	gotType, _ := splitID(t, p)
	if gotType != TypeIdentity {
		t.Errorf("person type = %s", gotType)
	}
	if p == r.Person("okta", "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c") {
		t.Error("two directories' persons are one")
	}
	if r.Person("okta", "00uAbC") == r.Person("okta", "00uabc") {
		t.Error("a non-GUID directory id was case-folded")
	}
	if r.Person("entra", "00000000-0000-0000-0000-000000000000") != "" || r.Person("", "x1") != "" || r.Person("entra", " ") != "" {
		t.Error("a placeholder minted a person")
	}
	if r.Person("entra", "acme") == r.Identity("acme") {
		t.Error("a person collided with a vendor identity")
	}
}

func TestAccountPlaceholders(t *testing.T) {
	for in, want := range map[string]bool{
		"S-1-0-0": true, "s-1-5-7": true, "-": true, "": true, "unknown": true,
		"S-1-5-18": false, "S-1-5-32-544": false, "S-1-5-21-1-2-3-500": false,
	} {
		if got := PlaceholderSID(in); got != want {
			t.Errorf("PlaceholderSID(%q) = %v, want %v", in, got, want)
		}
	}
	for in, want := range map[string]bool{
		"S-1-5-18": true, "s-1-5-19": true, "S-1-5-20": true, "S-1-5-32-544": true,
		"S-1-1-0": true, "S-1-5-4": true, "S-1-5-80-956008885-3418522649": true, "S-1-5-82-3006700770": true,
		"S-1-5-83-1-2": true, "S-1-5-90-0-1": true, "S-1-5-96-0-1": true,
		"S-1-15-2-1": true, "S-1-2-0": true, "S-1-3-0": true, "S-1-16-12288": true, "S-1-5-64-10": true, "S-1-5-84-0-0-0-0-0": true,
		"S-1-5-113": true, "S-1-5-114": true, "S-1-15-3-1": false, "S-1-5-115": false,
		"S-1-5-21-1-500": false, "S-1-5-32": false, "S-1-5-21": false, "S-1-5-7": false, "S-1-0-0": false, "S-1-5-81-1": false,
	} {
		if got := WellKnownLocalSID(in); got != want {
			t.Errorf("WellKnownLocalSID(%q) = %v, want %v", in, got, want)
		}
	}
	for in, want := range map[string]bool{
		"": true, " - ": true, "N/A": true, "Unknown": true, "ANONYMOUS LOGON": true,
		"administrator": false, "root": false, "jdoe": false,
	} {
		if got := PlaceholderLogin(in); got != want {
			t.Errorf("PlaceholderLogin(%q) = %v, want %v", in, got, want)
		}
	}
	for in, want := range map[string]bool{
		"{00000000-0000-0000-0000-000000000000}": true, "S-1-0-0": true, "0": true,
		"S-1-5-21-1001": false, "00u1abcd": false, "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c": false,
	} {
		if got := PlaceholderUserID(in); got != want {
			t.Errorf("PlaceholderUserID(%q) = %v, want %v", in, got, want)
		}
	}
}
