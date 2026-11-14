package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	nsA = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	nsB = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

// splitID parses "<type>--<uuid>" and asserts the uuid is a valid version-5
// UUID (deterministic, SHA-1 based) unless allowRandom is set.
func splitID(t *testing.T, id STIXID) (string, uuid.UUID) {
	t.Helper()
	parts := strings.SplitN(string(id), "--", 2)
	if len(parts) != 2 {
		t.Fatalf("malformed STIXID %q", id)
	}
	u, err := uuid.Parse(parts[1])
	if err != nil {
		t.Fatalf("STIXID %q has invalid uuid: %v", id, err)
	}
	return parts[0], u
}

// TestFormatAndVersion: every deterministic id is "<type>--<uuidv5>".
func TestFormatAndVersion(t *testing.T) {
	r := NewResolver(nsA)
	cases := map[string]STIXID{
		TypeIPv4Addr:     r.IPv4Addr("8.8.8.8"),
		TypeDomainName:   r.DomainName("example.com"),
		TypeEmailAddr:    r.EmailAddr("a@b.com"),
		TypeUserAccount:  r.Account(AccountIdentity{Type: AccountWindowsDomain, Login: "contoso\\jdoe", UserID: "S-1-5-21-1"}),
		TypeHost:         r.Host(HostIdentity{Hostname: "win-dc01"}),
		TypeObservedData: r.ObservedData(1007, time.Now(), "fixture", 1, map[string]any{"a": 1}),
	}
	for wantType, id := range cases {
		gotType, u := splitID(t, id)
		if gotType != wantType {
			t.Errorf("id %q has type %q; want %q", id, gotType, wantType)
		}
		if u.Version() != 5 {
			t.Errorf("id %q is UUID v%d; want v5 (deterministic)", id, u.Version())
		}
	}
}

// TestDeterminism: identical inputs always produce the identical id.
func TestDeterminism(t *testing.T) {
	r := NewResolver(nsA)
	ip1, ip2 := r.IPv4Addr("8.8.8.8"), r.IPv4Addr("8.8.8.8")
	if ip1 != ip2 {
		t.Error("IPv4Addr not deterministic")
	}
	ts := time.Date(2026, 4, 20, 14, 32, 11, 0, time.UTC)
	host := r.Host(HostIdentity{Hostname: "h1"})
	p1, ok1 := r.Process(host, 4242, ts)
	p2, ok2 := r.Process(host, 4242, ts)
	if p1 != p2 || !ok1 || !ok2 {
		t.Errorf("Process not deterministic: %q/%v vs %q/%v", p1, ok1, p2, ok2)
	}
}

// TestTenantScoping: the same value in different tenant namespaces must produce
// different ids — stitching can never leak across tenants (§7.1).
func TestTenantScoping(t *testing.T) {
	ra, rb := NewResolver(nsA), NewResolver(nsB)
	if ra.IPv4Addr("8.8.8.8") == rb.IPv4Addr("8.8.8.8") {
		t.Error("same IP in different tenants collided")
	}
	if ra.DomainName("evil.com") == rb.DomainName("evil.com") {
		t.Error("same domain in different tenants collided")
	}
}

// TestCanonicalizationEquivalence: values that differ only by case, whitespace,
// default port, query order, or trailing dot must resolve to the same id.
func TestCanonicalizationEquivalence(t *testing.T) {
	r := NewResolver(nsA)
	eq := []struct {
		name string
		a, b STIXID
	}{
		{"ipv6 case", r.IPv6Addr("2001:DB8::1"), r.IPv6Addr("2001:db8::1")},
		{"ip whitespace", r.IPv4Addr(" 8.8.8.8 "), r.IPv4Addr("8.8.8.8")},
		{"domain case+dot", r.DomainName("Example.COM."), r.DomainName("example.com")},
		{"url scheme/host/port/query", r.URL("HTTP://Example.com:80/p?b=2&a=1"), r.URL("http://example.com/p?a=1&b=2")},
		{"url ipv6 host keeps brackets", r.URL("http://[2001:DB8::1]:80/x"), r.URL("http://[2001:db8::1]/x")},
		{"email case", r.EmailAddr("Alice@Contoso.com"), r.EmailAddr("alice@contoso.com")},
		{"user case", r.Account(AccountIdentity{Type: AccountWindowsDomain, Login: "CONTOSO\\Alice"}), r.Account(AccountIdentity{Type: AccountWindowsDomain, Login: "contoso\\alice"})},
		{"sid case", r.Account(AccountIdentity{Type: AccountWindowsDomain, UserID: "S-1-5-21-7"}), r.Account(AccountIdentity{Type: AccountWindowsDomain, UserID: "s-1-5-21-7"})},
		{"host hostname case", r.Host(HostIdentity{Hostname: "WS1"}), r.Host(HostIdentity{Hostname: "ws1"})},
		{"host fqdn vs short", r.Host(HostIdentity{Hostname: "WS1.Corp.Example"}), r.Host(HostIdentity{Hostname: "ws1"})},
		{"host trailing dot", r.Host(HostIdentity{Hostname: " ws1.corp.example. "}), r.Host(HostIdentity{Hostname: "ws1"})},
		{"mac format", r.Host(HostIdentity{MAC: "00-1A-2B-3C-4D-5E"}), r.Host(HostIdentity{MAC: "00:1a:2b:3c:4d:5e"})},
	}
	for _, c := range eq {
		if c.a != c.b {
			t.Errorf("%s: expected equal ids, got %q vs %q", c.name, c.a, c.b)
		}
	}
}

// TestRegistryKey (adr/0341): the key carries its hive expanded, and the
// identity is the host, the lower-cased key and the lower-cased value name.
func TestRegistryKey(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`HKLM\Software\Run`, `HKEY_LOCAL_MACHINE\Software\Run`},
		{`hklm\Software\Run`, `HKEY_LOCAL_MACHINE\Software\Run`},
		{`\REGISTRY\MACHINE\Software\Run`, `HKEY_LOCAL_MACHINE\Software\Run`},
		{`hkey_local_machine\Software\Run\`, `HKEY_LOCAL_MACHINE\Software\Run`},
		{` HKU\S-1-5-21\Run `, `HKEY_USERS\S-1-5-21\Run`},
		{`\registry\user\S-1-5-21`, `HKEY_USERS\S-1-5-21`},
		{`HKEY_USERS\S-1-5-21`, `HKEY_USERS\S-1-5-21`},
		{`HKCU\Software`, `HKEY_CURRENT_USER\Software`},
		{`HKCR\.exe`, `HKEY_CLASSES_ROOT\.exe`},
		{`HKCC\System`, `HKEY_CURRENT_CONFIG\System`},
		{`HKLM\`, `HKEY_LOCAL_MACHINE`},
		{`\REGISTRY\A\x`, `REGISTRY\A\x`},
		{`Software\Run`, `Software\Run`},
		{"", ""},
	} {
		if got := RegistryKeyPath(tc.in); got != tc.want {
			t.Errorf("RegistryKeyPath(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}

	r := NewResolver(nsA)
	name := func(s string) *string { return &s }
	run := `HKEY_LOCAL_MACHINE\Software\Run`
	for _, tc := range []struct {
		name string
		a, b STIXID
		same bool
	}{
		{"abbreviated hive", r.RegistryKey("x-host--1", `HKLM\Software\Run`, name("Updater")), r.RegistryKey("x-host--1", run, name("Updater")), true},
		{"key case", r.RegistryKey("x-host--1", `hklm\software\run`, nil), r.RegistryKey("x-host--1", run, nil), true},
		{"value name case", r.RegistryKey("x-host--1", run, name("Updater")), r.RegistryKey("x-host--1", run, name("updater")), true},
		{"key only vs default value", r.RegistryKey("x-host--1", run, nil), r.RegistryKey("x-host--1", run, name("")), false},
		{"two values", r.RegistryKey("x-host--1", run, name("Updater")), r.RegistryKey("x-host--1", run, name("Other")), false},
		{"two hosts", r.RegistryKey("x-host--1", run, name("Updater")), r.RegistryKey("x-host--2", run, name("Updater")), false},
	} {
		if (tc.a == tc.b) != tc.same {
			t.Errorf("%s: %q vs %q; want same = %v", tc.name, tc.a, tc.b, tc.same)
		}
	}
	if typ, _, _ := strings.Cut(string(r.RegistryKey("x-host--1", run, nil)), "--"); typ != TypeWindowsRegistryKey {
		t.Errorf("type = %s; want %s", typ, TypeWindowsRegistryKey)
	}
}

// TestDistinctnessAcrossType: the same literal value under different object
// types must not collide.
func TestDistinctnessAcrossType(t *testing.T) {
	r := NewResolver(nsA)
	if r.IPv4Addr("1.2.3.4") == r.DomainName("1.2.3.4") {
		t.Error("ipv4 and domain with same value collided")
	}
}

// TestFileHashPriority: SHA-256 wins over SHA-1/MD5; hash matching is
// case/format-insensitive; absent hashes fall back to name/dir/size.
func TestFileHashPriority(t *testing.T) {
	r := NewResolver(nsA)
	full := r.File(FileIdentity{Hashes: map[string]string{"SHA-256": "ABC", "SHA-1": "def", "MD5": "123"}})
	sha256Only := r.File(FileIdentity{Hashes: map[string]string{"sha256": "abc"}})
	if full != sha256Only {
		t.Errorf("SHA-256 not prioritized / not case-insensitive: %q vs %q", full, sha256Only)
	}

	sha1Only := r.File(FileIdentity{Hashes: map[string]string{"SHA1": "def"}})
	if full == sha1Only {
		t.Error("file with SHA-256 collided with file identified by SHA-1 only")
	}

	// The algorithm participates in identity: an identical digest string under
	// a different algorithm is a different file, never a merge.
	if r.File(FileIdentity{Hashes: map[string]string{"sha1": "abc"}}) ==
		r.File(FileIdentity{Hashes: map[string]string{"md5": "abc"}}) {
		t.Error("same digest under different algorithms collided")
	}

	fallback1 := r.File(FileIdentity{Name: "evil.exe", ParentDirectory: "C:\\Temp", Size: 100})
	fallback2 := r.File(FileIdentity{Name: "evil.exe", ParentDirectory: "C:\\Temp", Size: 100})
	if fallback1 != fallback2 {
		t.Error("file name/dir/size fallback not deterministic")
	}
	if fallback1 == r.File(FileIdentity{Name: "evil.exe", ParentDirectory: "C:\\Temp", Size: 101}) {
		t.Error("different size produced same file id")
	}
}

// TestProcessDeviation covers the three specified behaviors: stitch within the
// same one-second bucket, split across seconds, and anonymous-random when
// created_time is zero.
func TestProcessDeviation(t *testing.T) {
	r := NewResolver(nsA)
	host := r.Host(HostIdentity{Hostname: "h1"})
	base := time.Date(2026, 4, 20, 14, 32, 11, 0, time.UTC)

	// Same second, sub-second differences → same id.
	a, okA := r.Process(host, 900, base.Add(200*time.Millisecond))
	b, okB := r.Process(host, 900, base.Add(900*time.Millisecond))
	if !okA || !okB || a != b {
		t.Errorf("processes in same second did not stitch: %q vs %q", a, b)
	}

	// Next second → different id (false-split behavior, as specified).
	c, _ := r.Process(host, 900, base.Add(time.Second))
	if a == c {
		t.Error("processes across a second boundary stitched; want split")
	}

	// Zero created_time → random, non-deterministic, ok=false.
	z1, okZ1 := r.Process(host, 900, time.Time{})
	z2, okZ2 := r.Process(host, 900, time.Time{})
	if okZ1 || okZ2 {
		t.Error("zero created_time should be non-deterministic (ok=false)")
	}
	if z1 == z2 {
		t.Error("anonymous processes should get distinct random ids")
	}
	if typ, _ := splitID(t, z1); typ != TypeProcess {
		t.Errorf("anonymous process id has type %q; want %q", typ, TypeProcess)
	}

	// Empty host_ref → anonymous too: hashing ("", pid, time) would false-merge
	// same-pid processes across different hosts.
	h1, okH1 := r.Process("", 900, base)
	h2, okH2 := r.Process("", 900, base)
	if okH1 || okH2 {
		t.Error("empty host_ref should be non-deterministic (ok=false)")
	}
	if h1 == h2 {
		t.Error("hostless processes should get distinct random ids, never merge")
	}
}

// TestUserAccountTypeNotFolded: account_login case folds, but a different
// account_type is a deliberately distinct account (no implicit merge).
func TestUserAccountTypeNotFolded(t *testing.T) {
	r := NewResolver(nsA)
	cloud := r.Account(AccountIdentity{Type: AccountCloud, Login: "alice@corp.com"})
	domain := r.Account(AccountIdentity{Type: AccountWindowsDomain, Login: "alice@corp.com"})
	if cloud == domain {
		t.Error("cloud and windows-domain accounts with same login collided; account_type must not fold")
	}
}

// TestObservedDataIdentity: same (class, second, tool, payload) → same id;
// changing any input changes the id.
func TestObservedDataIdentity(t *testing.T) {
	r := NewResolver(nsA)
	ts := time.Date(2026, 4, 20, 14, 32, 11, 500_000, time.UTC)
	payload := map[string]any{"src": "1.2.3.4", "n": 3}

	base := r.ObservedData(4001, ts, "crowdstrike", 1, payload)
	sameSecond := r.ObservedData(4001, ts.Add(400*time.Millisecond), "crowdstrike", 1, payload)
	if base != sameSecond {
		t.Error("ObservedData not stable within the same second")
	}
	if base == r.ObservedData(4001, ts, "splunk", 1, payload) {
		t.Error("different source_tool produced same ObservedData id")
	}
	if base == r.ObservedData(4001, ts, "crowdstrike", 1, map[string]any{"src": "9.9.9.9"}) {
		t.Error("different payload produced same ObservedData id")
	}
	if base == r.ObservedData(2004, ts, "crowdstrike", 1, payload) {
		t.Error("different class_uid produced same ObservedData id")
	}
	// Re-normalization: a newer normalizer version must mint a NEW id (§4.13) —
	// old ObservedData stays valid, the new id reflects the new interpretation.
	if base == r.ObservedData(4001, ts, "crowdstrike", 2, payload) {
		t.Error("newer normalizer version produced the same ObservedData id; re-normalization would be a no-op")
	}
}

// TestScopedNamespace covers the §3.5 per-scope namespace derivation: an empty
// scope returns the receiver unchanged; a non-empty scope derives a distinct,
// deterministic namespace, so the same entity resolves to different ids across
// organizations but the same id within one.
func TestScopedNamespace(t *testing.T) {
	base := NewResolver(nsA)

	if base.Scoped("") != base {
		t.Fatal("empty scope must return the receiver unchanged (single-organization path)")
	}

	acme := base.Scoped("acme")
	meridian := base.Scoped("meridian")
	acmeAgain := base.Scoped("acme")

	if acme.Namespace() == base.Namespace() {
		t.Error("scoped namespace equals the tenant namespace — entities would not be isolated")
	}
	if acme.Namespace() == meridian.Namespace() {
		t.Error("different scopes derived the same namespace")
	}
	if acme.Namespace() != acmeAgain.Namespace() {
		t.Error("same scope derived different namespaces — derivation is not deterministic")
	}

	// The load-bearing property: org A's DC01 is not org B's DC01.
	if acme.IPv4Addr("10.0.0.1") == meridian.IPv4Addr("10.0.0.1") {
		t.Error("same entity resolved to the same id across scopes (silent cross-organization merge)")
	}
	// ...but within one organization, the same entity is the same id.
	if acme.IPv4Addr("10.0.0.1") != acmeAgain.IPv4Addr("10.0.0.1") {
		t.Error("same entity resolved to different ids within one scope")
	}
}

// TestHostTiers covers adr/0211: the short hostname is the identity and
// outranks every weak tier; MAC, then (source tool, device uid), identify a
// host only when no hostname is present; nothing present mints nothing.
func TestHostTiers(t *testing.T) {
	r := NewResolver(nsA)
	strong := r.Host(HostIdentity{Hostname: "ws1"})

	if got := r.Host(HostIdentity{Hostname: "WS1", MAC: "00:1a:2b:3c:4d:5e", SourceTool: "edr", SourceUID: "a-1"}); got != strong {
		t.Errorf("hostname must outrank MAC and source id: %q vs %q", got, strong)
	}

	mac := r.Host(HostIdentity{MAC: "00:1a:2b:3c:4d:5e"})
	if mac == "" || mac == strong {
		t.Errorf("MAC tier: got %q", mac)
	}
	if got := r.Host(HostIdentity{MAC: "00:1a:2b:3c:4d:5e", SourceTool: "edr", SourceUID: "a-1"}); got != mac {
		t.Error("MAC must outrank the source id")
	}

	src := r.Host(HostIdentity{SourceTool: "EDR ", SourceUID: " a-1"})
	if src == "" || src != r.Host(HostIdentity{SourceTool: "edr", SourceUID: "a-1"}) {
		t.Errorf("source-id tier must fold the tool name and trim the uid: %q", src)
	}
	if src == r.Host(HostIdentity{SourceTool: "ids", SourceUID: "a-1"}) {
		t.Error("the same uid from two tools must be two weak hosts")
	}
	if bare := r.Host(HostIdentity{SourceUID: "a-1"}); bare == "" || bare == src {
		t.Errorf("a uid without a tool must mint its own weak host: %q", bare)
	}
	if r.Host(HostIdentity{SourceTool: "edr"}) != "" {
		t.Error("a tool without a uid must mint nothing")
	}

	// An IP literal in the hostname field is kept whole: two addresses are two
	// hosts, never the host "10".
	v4a, v4b := r.Host(HostIdentity{Hostname: "10.0.0.5"}), r.Host(HostIdentity{Hostname: "10.0.0.6"})
	if v4a == "" || v4a == v4b || v4a == r.Host(HostIdentity{Hostname: "10"}) {
		t.Errorf("IPv4 literal hostnames: %q %q", v4a, v4b)
	}
	if r.Host(HostIdentity{Hostname: "FE80::1"}) != r.Host(HostIdentity{Hostname: "fe80::1"}) {
		t.Error("an IPv6 literal hostname must fold case")
	}

	for _, empty := range []HostIdentity{{}, {Hostname: "  "}, {Hostname: "."}} {
		if got := r.Host(empty); got != "" {
			t.Errorf("Host(%+v) = %q, want empty", empty, got)
		}
	}
}

// TestHostPlaceholders: a placeholder reads as an absent field (adr/0211), so
// the host falls to its next tier, and nothing at all mints nothing.
func TestHostPlaceholders(t *testing.T) {
	r := NewResolver(nsA)
	mac := r.Host(HostIdentity{MAC: "00:1a:2b:3c:4d:5e"})
	if got := r.Host(HostIdentity{Hostname: "localhost", MAC: "00:1a:2b:3c:4d:5e"}); got != mac {
		t.Errorf("a localhost hostname must fall to the MAC: %q vs %q", got, mac)
	}
	for _, addr := range []string{"127.0.0.1", "0.0.0.0", "::1"} {
		if got := r.Host(HostIdentity{Hostname: addr, MAC: "00:1a:2b:3c:4d:5e"}); got != mac {
			t.Errorf("a %s hostname must fall to the MAC: %q vs %q", addr, got, mac)
		}
	}
	uid := r.Host(HostIdentity{SourceTool: "edr", SourceUID: "a-1"})
	if got := r.Host(HostIdentity{Hostname: "-", MAC: "00:00:00:00:00:00", SourceTool: "edr", SourceUID: "a-1"}); got != uid {
		t.Errorf("a dash hostname and a zero MAC must fall to the uid: %q vs %q", got, uid)
	}
	if got := r.Host(HostIdentity{MAC: "00:00:00:00:00:00"}); got != "" {
		t.Errorf("a zero MAC alone must mint nothing: %q", got)
	}
	if got := r.Host(HostIdentity{MAC: "00:00:00:00:00:00", SourceTool: "edr", SourceUID: "{00000000-0000-0000-0000-000000000000}"}); got != "" {
		t.Errorf("a zero MAC and the nil GUID must mint nothing: %q", got)
	}
}

// TestMACForms: the accepted MAC forms are identity's fixed rule, not the
// toolchain's: every accepted form of one address mints one host, and any
// other form is no MAC at all.
func TestMACForms(t *testing.T) {
	r := NewResolver(nsA)
	want := r.Host(HostIdentity{MAC: "00:1a:2b:3c:4d:5e"})
	for _, v := range []string{"00-1A-2B-3C-4D-5E", "001a.2b3c.4d5e", "001A2B3C4D5E", " 00:1a:2b:3c:4d:5e "} {
		if got := r.Host(HostIdentity{MAC: v}); got != want {
			t.Errorf("Host(MAC %q) = %q; want %q", v, got, want)
		}
	}
	eui64 := r.Host(HostIdentity{MAC: "00:1a:2b:3c:4d:5e:6f:70"})
	if eui64 == "" || eui64 == want || r.Host(HostIdentity{MAC: "001a2b3c4d5e6f70"}) != eui64 || r.Host(HostIdentity{MAC: "00-1a-2b-3c-4d-5e-6f-70"}) != eui64 {
		t.Errorf("EUI-64 forms: %q", eui64)
	}
	if got := canonicalMAC("001A.2B3C.4D5E"); got != "00:1a:2b:3c:4d:5e" {
		t.Errorf("canonicalMAC = %q", got)
	}
	for _, v := range []string{
		"00:1a-2b:3c:4d:5e", "001a.2b3c.4d5e.6f70", "001a2b3c4d5", "00:1a:2b:3c:4d", "00.1a.2b.3c.4d.5e",
		"00:00:5e:10:00:00:00:00:fe:80:00:00:00:00:00:00:02:00:5e:10", "zz:1a:2b:3c:4d:5e",
	} {
		if FoldMAC(v) != "" || !PlaceholderMAC(v) || r.Host(HostIdentity{MAC: v}) != "" {
			t.Errorf("%q accepted as a MAC", v)
		}
	}
}

// TestCloudAccount: "<x-cloud-account>--<uuidv5>", deterministic, provider
// folded for case, the uid's case kept unless it is a GUID, and a different
// account or provider a different id.
func TestCloudAccount(t *testing.T) {
	r := NewResolver(nsA)
	id := r.CloudAccount("aws", "111122223333")
	if typ, u := splitID(t, id); typ != TypeCloudAccount || u.Version() != 5 {
		t.Errorf("id %q: type %q, v%d", id, typ, u.Version())
	}
	if r.CloudAccount("aws", "111122223333") != id {
		t.Error("CloudAccount not deterministic")
	}
	if r.CloudAccount(" AWS ", " 111122223333 ") != id {
		t.Error("provider case and whitespace must fold")
	}
	if r.CloudAccount("aws", "444455556666") == id || r.CloudAccount("gcp", "111122223333") == id {
		t.Error("a different account or provider must be a different id")
	}
	guid := "0F8FAD5B-D9CB-469F-A165-70867728950E"
	if r.CloudAccount("azure", guid) != r.CloudAccount("azure", strings.ToLower(guid)) {
		t.Error("a GUID-shaped account uid must fold for case")
	}
	if r.CloudAccount("gcp", "Acme-Prod") == r.CloudAccount("gcp", "acme-prod") {
		t.Error("a non-GUID account uid keeps its case")
	}
}

// TestCloudResourcePriority: the provider's unique id beats the scoped name,
// one uid seen under two names is one resource, and a name resolves only
// under an account.
func TestCloudResourcePriority(t *testing.T) {
	r := NewResolver(nsA)
	acct := r.CloudAccount("aws", "111122223333")
	other := r.CloudAccount("aws", "444455556666")
	const arn = "arn:aws:s3:::acme-internal-exports"

	byUID, rule := r.CloudResource(CloudResourceIdentity{Provider: "aws", UID: arn, AccountRef: acct, Type: "AWS::S3::Bucket", Name: "acme-internal-exports"})
	if rule != CloudResourceByUID {
		t.Fatalf("a resource with a uid resolved by rule %d; want by uid", rule)
	}
	if typ, u := splitID(t, byUID); typ != TypeCloudResource || u.Version() != 5 {
		t.Errorf("id %q: type %q, v%d", byUID, typ, u.Version())
	}
	// The uid alone is the identity: the name, type and account do not move it.
	for _, c := range []CloudResourceIdentity{
		{Provider: "aws", UID: arn},
		{Provider: "AWS", UID: " " + arn + " ", AccountRef: other, Name: "exports-renamed"},
	} {
		if got, _ := r.CloudResource(c); got != byUID {
			t.Errorf("CloudResource(%+v) = %s; want the uid's id %s", c, got, byUID)
		}
	}
	if got, _ := r.CloudResource(CloudResourceIdentity{Provider: "aws", UID: "arn:aws:s3:::ACME-INTERNAL-EXPORTS"}); got == byUID {
		t.Error("a non-GUID resource uid keeps its case")
	}

	byName, rule := r.CloudResource(CloudResourceIdentity{Provider: "aws", AccountRef: acct, Type: "AWS::S3::Bucket", Name: "acme-internal-exports"})
	if rule != CloudResourceByName || byName == byUID {
		t.Fatalf("by name: %s, rule %d; want a scoped id distinct from the uid's", byName, rule)
	}
	if got, _ := r.CloudResource(CloudResourceIdentity{AccountRef: acct, Type: " aws::s3::bucket ", Name: " acme-internal-exports "}); got != byName {
		t.Error("type must fold for case and whitespace, the name for whitespace")
	}
	orders, _ := r.CloudResource(CloudResourceIdentity{AccountRef: acct, Type: "AWS::DynamoDB::Table", Name: "Orders"})
	lower, _ := r.CloudResource(CloudResourceIdentity{AccountRef: acct, Type: "AWS::DynamoDB::Table", Name: "orders"})
	if orders == lower {
		t.Error("names differing only in case must be two resources")
	}
	east, _ := r.CloudResource(CloudResourceIdentity{AccountRef: acct, Type: "AWS::DynamoDB::Table", Region: "us-east-1", Name: "Orders"})
	west, _ := r.CloudResource(CloudResourceIdentity{AccountRef: acct, Type: "AWS::DynamoDB::Table", Region: "us-west-2", Name: "Orders"})
	eastUpper, _ := r.CloudResource(CloudResourceIdentity{AccountRef: acct, Type: "AWS::DynamoDB::Table", Region: " US-EAST-1 ", Name: "Orders"})
	if east == west || east != eastUpper || east == orders {
		t.Errorf("regions: east=%s west=%s eastUpper=%s none=%s; want two regions apart, region folded, absent region distinct", east, west, eastUpper, orders)
	}
	if again, _ := r.CloudResource(CloudResourceIdentity{AccountRef: acct, Type: "AWS::DynamoDB::Table", Name: "Orders"}); again != orders {
		t.Error("two region-less sightings of one name must be one resource")
	}
	vm := func(group, namespace string) STIXID {
		id, _ := r.CloudResource(CloudResourceIdentity{AccountRef: acct, Type: "Microsoft.Compute/virtualMachines", Region: "eastus", Group: group, Namespace: namespace, Name: "api"})
		return id
	}
	if vm("rg-a", "") == vm("rg-b", "") || vm("RG-A ", "") != vm("rg-a", "") || vm(" ", "") != vm("", "") || vm("", "") == vm("rg-a", "") {
		t.Error("group: rg-a and rg-b must be apart, case folded, absent on both one id")
	}
	if vm("", "team-a") == vm("", "team-b") || vm("", " team-a ") != vm("", "team-a") || vm("", "team-a") == vm("", "") {
		t.Error("namespace: team-a and team-b must be apart, whitespace trimmed, absent distinct")
	}
	if vm("", "SourceA") == vm("", "sourcea") {
		t.Error("namespace: names differing only in case must be two resources (no case rule in OCSF)")
	}
	gce := func(zone string) STIXID {
		id, _ := r.CloudResource(CloudResourceIdentity{AccountRef: acct, Type: "compute.googleapis.com/Instance", Region: "us-central1", Zone: zone, Name: "api"})
		return id
	}
	if gce("us-central1-a") == gce("us-central1-b") {
		t.Error("zone: one name in two zones must be two resources")
	}
	if gce(" US-CENTRAL1-A ") != gce("us-central1-a") {
		t.Error("zone: case and whitespace must fold, like region")
	}
	if gce("") == gce("us-central1-a") {
		t.Error("zone: a sighting with a zone splits from one without")
	}
	if absent, _ := r.CloudResource(CloudResourceIdentity{AccountRef: acct, Type: "compute.googleapis.com/Instance", Region: "us-central1", Name: "api"}); absent != gce("") || gce(" ") != gce("") {
		t.Error("zone: absent, empty and blank must be one id")
	}
	if got, _ := r.CloudResource(CloudResourceIdentity{AccountRef: other, Type: "AWS::S3::Bucket", Name: "acme-internal-exports"}); got == byName {
		t.Error("the same name under another account must be another resource")
	}

	for _, c := range []CloudResourceIdentity{
		{},
		{Provider: "aws", Type: "AWS::S3::Bucket", Name: "acme-internal-exports"}, // no account to scope the name
		{Provider: "aws", AccountRef: acct, Type: "AWS::S3::Bucket"},              // no name
		{Provider: "aws", UID: "   ", AccountRef: acct},
	} {
		if id, rule := r.CloudResource(c); rule != CloudResourceNone || id != "" {
			t.Errorf("CloudResource(%+v) = %q, rule %d; want nothing", c, id, rule)
		}
	}
}

// TestAddressAndFilePlaceholders: a placeholder address mints nothing; a
// file whose only hash is a placeholder takes the hash-less path.
func TestAddressAndFilePlaceholders(t *testing.T) {
	r := NewResolver(nsA)
	if got := r.IPv4Addr("0.0.0.0"); got != "" {
		t.Errorf("IPv4Addr(0.0.0.0) = %q, want empty", got)
	}
	if got := r.IPv6Addr("::1"); got != "" {
		t.Errorf("IPv6Addr(::1) = %q, want empty", got)
	}
	if r.IPv4Addr("10.0.0.1") == "" {
		t.Error("a private address must mint")
	}

	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	named := FileIdentity{Hashes: map[string]string{"SHA-256": emptySHA256}, Name: "empty.txt", ParentDirectory: "/tmp"}
	if got, want := r.File(named), r.File(FileIdentity{Name: "empty.txt", ParentDirectory: "/tmp"}); got != want {
		t.Errorf("an empty-file hash must fall to the name triple: %q vs %q", got, want)
	}
	if got := r.File(FileIdentity{Hashes: map[string]string{"SHA-256": emptySHA256}}); got != "" {
		t.Errorf("a file with only a placeholder hash must mint nothing: %q", got)
	}
	if got, want := r.File(FileIdentity{Hashes: map[string]string{"sha256": emptySHA256, "md5": "0123456789abcdef0123456789abcdef"}}),
		r.File(FileIdentity{Hashes: map[string]string{"md5": "0123456789abcdef0123456789abcdef"}}); got != want {
		t.Errorf("a placeholder hash must be skipped for the next usable one: %q vs %q", got, want)
	}
}

// TestSplitHostname: the short name and the DNS domain, both folded.
func TestSplitHostname(t *testing.T) {
	cases := []struct{ in, host, domain string }{
		{"WS1", "ws1", ""},
		{"WS1.Corp.Example.", "ws1", "corp.example"},
		{" ws1.corp ", "ws1", "corp"},
		{"", "", ""},
		{".", "", ""},
		{"10.0.0.5", "10.0.0.5", ""},
		{" 10.0.0.5. ", "10.0.0.5", ""},
		{"FE80::1", "fe80::1", ""},
		{"1.2.3", "1.2.3", ""},
		{"999.1.1.1", "999.1.1.1", ""},
		{"WS1.corp:3389", "ws1.corp:3389", ""},
	}
	for _, c := range cases {
		h, d := SplitHostname(c.in)
		if h != c.host || d != c.domain {
			t.Errorf("SplitHostname(%q) = (%q, %q), want (%q, %q)", c.in, h, d, c.host, c.domain)
		}
	}
}

// TestCloudScoped: the same account and resource in two source scopes are
// distinct ids, and stable within each.
func TestCloudScoped(t *testing.T) {
	base := NewResolver(nsA)
	a, b := base.Scoped("org-a"), base.Scoped("org-b")
	if a.CloudAccount("aws", "111122223333") == b.CloudAccount("aws", "111122223333") {
		t.Error("an account must not stitch across scopes")
	}
	c := CloudResourceIdentity{Provider: "aws", UID: "arn:aws:s3:::acme-release-artifacts"}
	ra, _ := a.CloudResource(c)
	rb, _ := b.CloudResource(c)
	again, _ := base.Scoped("org-a").CloudResource(c)
	if ra == rb || ra != again {
		t.Errorf("resource across scopes: a=%s b=%s again=%s", ra, rb, again)
	}
	if NewResolver(nsB).CloudAccount("aws", "111122223333") == base.CloudAccount("aws", "111122223333") {
		t.Error("an account must not stitch across tenants")
	}
}

// TestWeakEmailMessage covers adr/0356: a source's own message id mints a
// weak email-message on (source tool, uid), apart from the message the
// Message-ID mints, per tool, and nothing for an empty uid.
func TestWeakEmailMessage(t *testing.T) {
	r := NewResolver(nsA)
	weak := r.WeakEmailMessage("MailGW ", " m-42 ")
	if typ, _ := splitID(t, weak); typ != TypeEmailMessage {
		t.Fatalf("type = %q, want %q", typ, TypeEmailMessage)
	}
	if weak != r.WeakEmailMessage("mailgw", "m-42") {
		t.Error("the tool name must fold and the uid must be trimmed")
	}
	if weak == r.WeakEmailMessage("other", "m-42") {
		t.Error("the same uid from two tools must be two weak messages")
	}
	if weak == r.WeakEmailMessage("mailgw", "M-42") {
		t.Error("the uid is the source's own and must not be folded for case")
	}
	if weak == r.EmailMessage("m-42", "", "", time.Time{}) {
		t.Error("a weak message must not be the message its uid would mint as a Message-ID")
	}
	if weak == NewResolver(nsB).WeakEmailMessage("mailgw", "m-42") {
		t.Error("a weak message is tenant-scoped")
	}
	for _, uid := range []string{"", "   "} {
		if got := r.WeakEmailMessage("mailgw", uid); got != "" {
			t.Errorf("WeakEmailMessage(%q) = %q, want none", uid, got)
		}
	}
}
