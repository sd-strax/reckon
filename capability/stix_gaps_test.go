package capability

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsf"
)

// The tests in this file cover the rule for each gap between OCSF and STIX
// (adr/0357, "Every gap between OCSF and STIX has a rule"). Every object they
// produce is also checked against the STIX schemas by TestMain.

func normalizeEvent(t *testing.T, p map[string]any) NormalizationResult {
	t.Helper()
	if _, ok := p["time"]; !ok {
		p["time"] = ms("2026-04-20T14:32:11Z")
	}
	res, err := testRegistry().Normalize(makeEvent(p))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func values(scos []SCO) []any {
	var out []any
	for _, s := range scos {
		out = append(out, s.Properties["value"])
	}
	return out
}

// An address is trimmed, and an IPv4-mapped IPv6 address is the IPv4 address.
func TestGapAddressesAreCleanedUp(t *testing.T) {
	ids := identity.NewResolver(testNS)
	res := normalizeEvent(t, map[string]any{
		"class_uid":    4001,
		"src_endpoint": map[string]any{"ip": " 10.0.0.1 "},
		"dst_endpoint": map[string]any{"ip": "::ffff:10.0.0.5"},
	})
	v4 := scosOf(res, identity.TypeIPv4Addr)
	if got := values(v4); len(got) != 2 || got[0] != "10.0.0.1" || got[1] != "10.0.0.5" {
		t.Errorf("ipv4 values = %v; want 10.0.0.1 and 10.0.0.5", got)
	}
	if len(scosOf(res, identity.TypeIPv6Addr)) != 0 {
		t.Error("the mapped address was also written as ipv6-addr")
	}
	if v4[1].ID != ids.IPv4Addr("10.0.0.5") {
		t.Errorf("the mapped address is %s; want the same object as 10.0.0.5", v4[1].ID)
	}
	// An IPv6 address is stored in canonical text, so the stored value, the
	// id and every reader agree (adr/0357).
	embedded := normalizeEvent(t, map[string]any{"class_uid": 4001, "dst_endpoint": map[string]any{"ip": "64:ff9b::192.0.2.1"}})
	if got := values(scosOf(embedded, identity.TypeIPv6Addr)); len(got) != 1 || got[0] != "64:ff9b::c000:201" {
		t.Errorf("an embedded dotted quad: ipv6 values = %v; want the canonical 64:ff9b::c000:201", got)
	}
	var stored []any
	for _, v := range []string{"2001:DB8:0:0:0:0:0:1", "2001:db8::1", "2001:0db8::0001"} {
		res := normalizeEvent(t, map[string]any{"class_uid": 4001, "dst_endpoint": map[string]any{"ip": v}})
		stored = append(stored, values(scosOf(res, identity.TypeIPv6Addr))...)
	}
	if len(stored) != 3 || stored[0] != "2001:db8::1" || stored[1] != stored[0] || stored[2] != stored[0] {
		t.Errorf("one IPv6 address in three spellings: stored %v; want 2001:db8::1 each time", stored)
	}
	// A CIDR block is a value STIX allows (§6.8, §6.9): stored in canonical
	// prefix text, typed by family. A /32 is the bare address.
	for in, want := range map[string][2]string{
		"10.0.0.0/24":         {identity.TypeIPv4Addr, "10.0.0.0/24"},
		"2001:DB8::/32":       {identity.TypeIPv6Addr, "2001:db8::/32"},
		"::ffff:10.0.0.0/120": {identity.TypeIPv4Addr, "10.0.0.0/24"},
		"10.0.0.5/32":         {identity.TypeIPv4Addr, "10.0.0.5"},
		"10.0.0.5/24":         {identity.TypeIPv4Addr, "10.0.0.0/24"},
		"2001:db8::1/32":      {identity.TypeIPv6Addr, "2001:db8::/32"},
		"2001:db8::1/128":     {identity.TypeIPv6Addr, "2001:db8::1"},
	} {
		res := normalizeEvent(t, map[string]any{"class_uid": 4001, "dst_endpoint": map[string]any{"ip": in}})
		got := values(scosOf(res, want[0]))
		if len(got) != 1 || got[0] != want[1] {
			t.Errorf("CIDR %s: %s values = %v; want %s", in, want[0], got, want[1])
		}
	}
}

// A domain that breaks RFC 1034 is left out, with its resolves-to; one with
// an underscore label is kept.
func TestGapDomainNames(t *testing.T) {
	res := normalizeEvent(t, map[string]any{"class_uid": 4003, "query": map[string]any{"hostname": "_ldap._tcp.dc._msdcs.example.com"}})
	if got := values(scosOf(res, identity.TypeDomainName)); len(got) != 1 || got[0] != "_ldap._tcp.dc._msdcs.example.com" {
		t.Errorf("an underscore label: domains = %v; want it kept", got)
	}
	res = normalizeEvent(t, map[string]any{"class_uid": 4001, "dst_endpoint": map[string]any{"ip": "10.0.0.5", "domain": `CONTOSO\`}})
	if d := scosOf(res, identity.TypeDomainName); len(d) != 0 {
		t.Errorf("CONTOSO\\ was written as a domain: %+v", d)
	}
	for _, r := range res.Relationships {
		if r.Type == RelResolvesTo {
			t.Errorf("a resolves-to names a domain that was left out: %+v", r)
		}
	}
	res = normalizeEvent(t, map[string]any{"class_uid": 4003, "answers": []any{map[string]any{"type": "CNAME", "rdata": "not a name"}}})
	if d := scosOf(res, identity.TypeDomainName); len(d) != 0 {
		t.Errorf("a CNAME with a space was written: %+v", d)
	}
}

// A URL that is not a URI, and an email address that is not an addr-spec,
// are left out.
func TestGapURLsAndEmailAddresses(t *testing.T) {
	res := normalizeEvent(t, map[string]any{"class_uid": 4012, "email_uid": "m-1", "url": map[string]any{"url_string": "www.example.com/login"}})
	if u := scosOf(res, identity.TypeURL); len(u) != 0 {
		t.Errorf("a URL with no scheme was written: %+v", u)
	}
	res = normalizeEvent(t, map[string]any{"class_uid": 4012, "email_uid": "m-1", "url": map[string]any{"url_string": "https://example.com/login"}})
	if got := values(scosOf(res, identity.TypeURL)); len(got) != 1 {
		t.Errorf("a URI was left out: %v", got)
	}
	res = normalizeEvent(t, map[string]any{"class_uid": 4009, "email": map[string]any{
		"from": "Bob <bob@example.com>", "to": []any{"jdoe@example.com", "a,b@example.com"}, "subject": "s",
	}})
	if got := values(scosOf(res, identity.TypeEmailAddr)); len(got) != 1 || got[0] != "jdoe@example.com" {
		t.Errorf("email addresses = %v; want jdoe@example.com alone", got)
	}
	msg := scosOf(res, identity.TypeEmailMessage)
	if len(msg) != 1 || msg[0].Properties["from_ref"] != nil {
		t.Errorf("message = %+v; want no from_ref for a display-name sender", msg)
	}
}

// A registry key whose root is a known alias gets the full hive name; a key
// with no hive gets no windows-registry-key.
func TestGapRegistryRoots(t *testing.T) {
	for in, want := range map[string]string{
		`Computer\HKEY_LOCAL_MACHINE\SOFTWARE\Example`:               `HKEY_LOCAL_MACHINE\SOFTWARE\Example`,
		`\REGISTRY\MACHINE\SOFTWARE\Example`:                         `HKEY_LOCAL_MACHINE\SOFTWARE\Example`,
		`\REGISTRY\USER\S-1-5-21-1\Software`:                         `HKEY_USERS\S-1-5-21-1\Software`,
		`HKEY_CURRENT_USER\Software\HKCUTools`:                       `HKEY_CURRENT_USER\Software\HKCUTools`,
		`Software\Microsoft\Windows\CurrentVersion\Run`:              "",
		`\REGISTRY\A\{01234567-89ab-cdef-0123-456789abcdef}\Example`: "",
	} {
		res := normalizeEvent(t, map[string]any{"class_uid": 201001, "reg_key": map[string]any{"path": in}, "device": map[string]any{"hostname": "WS1"}})
		keys := scosOf(res, identity.TypeWindowsRegistryKey)
		switch {
		case want == "" && len(keys) != 0:
			t.Errorf("%s: wrote %v; want no key", in, keys[0].Properties["key"])
		case want != "" && (len(keys) != 1 || keys[0].Properties["key"] != want):
			t.Errorf("%s: keys = %+v; want %s", in, keys, want)
		}
	}
}

// A file with a path, no name and no hash that fits takes its name from the
// path. One with no name left to take is not written.
func TestGapFileNameFromPath(t *testing.T) {
	ids := identity.NewResolver(testNS)
	res := normalizeEvent(t, map[string]any{"class_uid": 1001, "device": map[string]any{"hostname": "WS1"}, "file": map[string]any{
		"name": "", "path": `C:\Temp\a.exe`, "hashes": []any{map[string]any{"algorithm_id": 3, "value": "abc"}},
	}})
	f := scosOf(res, identity.TypeFile)
	if len(f) != 1 || f[0].Properties["name"] != "a.exe" || f[0].ID != ids.File(identity.FileIdentity{Name: "a.exe", ParentDirectory: `C:\Temp`}) {
		t.Errorf("files = %+v; want a.exe, minted from that name", f)
	}
	res = normalizeEvent(t, map[string]any{"class_uid": 1001, "device": map[string]any{"hostname": "WS1"}, "file": map[string]any{"name": "", "path": `C:\Temp\`}})
	if f := scosOf(res, identity.TypeFile); len(f) != 0 {
		t.Errorf("a file with no name and no hash was written: %+v", f)
	}
}

// A hash key shorter than 3 or longer than 250 characters is left out.
func TestGapHashKeyLength(t *testing.T) {
	res := normalizeEvent(t, map[string]any{"class_uid": 1001, "device": map[string]any{"hostname": "WS1"}, "file": map[string]any{
		"name": "a.exe", "hashes": []any{
			map[string]any{"algorithm_id": 99, "algorithm": "H1", "value": "abcd"},
			map[string]any{"algorithm_id": 99, "algorithm": strings.Repeat("h", 251), "value": "abcd"},
			map[string]any{"algorithm_id": 99, "algorithm": "H12", "value": "abcd"},
		},
	}})
	f := scosOf(res, identity.TypeFile)
	if len(f) != 1 {
		t.Fatalf("files = %+v", f)
	}
	if h, _ := f[0].Properties["hashes"].(map[string]string); len(h) != 1 || h["H12"] != "abcd" {
		t.Errorf("hashes = %v; want H12 alone", f[0].Properties["hashes"])
	}
}

// A person with no name gets no identity, and no belongs-to to one. The
// account is still written.
func TestGapPersonWithNoName(t *testing.T) {
	res := normalizeEvent(t, map[string]any{"class_uid": 5003,
		"metadata": map[string]any{"product": map[string]any{"name": "Okta", "vendor_name": "Okta"}},
		"user":     map[string]any{"uid": "00u1abc"},
	})
	if p := scosOf(res, identity.TypeIdentity); len(p) != 0 {
		t.Errorf("a person with no name was written: %+v", p)
	}
	if len(scosOf(res, identity.TypeUserAccount)) == 0 {
		t.Error("the account was not written")
	}
	for _, r := range res.Relationships {
		if r.Type == RelBelongsTo && strings.HasPrefix(string(r.TargetRef), identity.TypeIdentity+"--") {
			t.Errorf("a belongs-to names the person that was left out: %+v", r)
		}
	}
}

// An event whose own time is outside the years 0000 to 9999 is a broken
// record, with its own error. An optional time out of that range is left
// out, and the object kept (adr/0357).
func TestGapTimeOutsideTheYearRange(t *testing.T) {
	for _, when := range []int64{253402300800000, -62167219200001} {
		_, err := testRegistry().Normalize(makeEvent(map[string]any{"class_uid": 1007, "time": when, "device": map[string]any{"hostname": "WS1"}}))
		if !errors.Is(err, ErrEventTimeOutOfRange) || errors.Is(err, ErrNoEventTime) {
			t.Errorf("time %d: err = %v; want ErrEventTimeOutOfRange", when, err)
		}
	}
	res := normalizeEvent(t, map[string]any{
		"class_uid": 5023, "cloud": map[string]any{"provider": "AWS"},
		"resources": []any{map[string]any{"uid": "r-1", "created_time": int64(253402300800000), "modified_time": ms("2026-04-20T14:32:11Z")}},
	})
	ext := res.ObservedData[0].Extensions
	if ext["created_time"] != nil || ext["modified_time"] != "2026-04-20T14:32:11Z" || len(scosOf(res, identity.TypeCloudResource)) != 1 {
		t.Errorf("a resource created_time in the year 10000: extensions = %v; want it left out, modified_time kept, the resource written", ext)
	}
	res = normalizeEvent(t, map[string]any{"class_uid": 1007, "device": map[string]any{"hostname": "WS1"},
		"process": map[string]any{"pid": 7, "name": "a.exe", "created_time": int64(253402300800000)}})
	if p := scosOf(res, identity.TypeProcess); len(p) != 1 || p[0].Properties["created_time"] != (time.Time{}) || p[0].Properties["pid"] != 7 {
		t.Errorf("a process created_time in the year 10000: %+v; want it left out and the process kept", p)
	}
}

// A value the telemetry does not give is left out, never written as 0 or
// false. A value it does give, 0 included, is kept.
func TestGapUnknownValuesAreLeftOut(t *testing.T) {
	proc := func(process map[string]any) SCO {
		t.Helper()
		res := normalizeEvent(t, map[string]any{"class_uid": 1007, "device": map[string]any{"hostname": "WS1"}, "process": process})
		p := scosOf(res, identity.TypeProcess)
		if len(p) != 1 {
			t.Fatalf("processes = %+v", p)
		}
		return p[0]
	}
	if p := proc(map[string]any{"name": "a.exe", "uid": "p-1"}); p.Properties["pid"] != nil {
		t.Errorf("no pid: pid = %v; want it left out", p.Properties["pid"])
	}
	if p := proc(map[string]any{"name": "Idle", "pid": 0}); p.Properties["pid"] != 0 {
		t.Errorf("pid 0: pid = %v; want 0 kept", p.Properties["pid"])
	}

	traffic := func(src, dst map[string]any) SCO {
		t.Helper()
		res := normalizeEvent(t, map[string]any{"class_uid": 4001, "src_endpoint": src, "dst_endpoint": dst})
		nt := scosOf(res, identity.TypeNetworkTraffic)
		if len(nt) != 1 {
			t.Fatalf("network-traffic = %+v", nt)
		}
		return nt[0]
	}
	if nt := traffic(map[string]any{"ip": "10.0.0.1"}, map[string]any{"ip": "10.0.0.2"}); nt.Properties["src_port"] != nil || nt.Properties["dst_port"] != nil {
		t.Errorf("no ports: %v; want both left out", nt.Properties)
	}
	if nt := traffic(map[string]any{"ip": "10.0.0.1", "port": 0}, map[string]any{"ip": "10.0.0.2", "port": 443}); nt.Properties["src_port"] != 0 || nt.Properties["dst_port"] != 443 {
		t.Errorf("ports 0 and 443: %v; want both kept", nt.Properties)
	}

	account := func(user map[string]any) SCO {
		t.Helper()
		res := normalizeEvent(t, map[string]any{"class_uid": 6003, "cloud": map[string]any{"provider": "AWS", "account": map[string]any{"uid": "111122223333"}},
			"actor": map[string]any{"user": user}, "api": map[string]any{"operation": "ListBuckets"}})
		u := scosOf(res, identity.TypeUserAccount)
		if len(u) == 0 {
			t.Fatalf("no account for %v", user)
		}
		return u[0]
	}
	for _, c := range []struct {
		typeID any
		want   any
	}{{nil, nil}, {0, nil}, {99, nil}, {1, false}, {2, false}, {3, true}, {4, true}} {
		user := map[string]any{"uid": "AIDAEXAMPLE", "name": "u"}
		if c.typeID != nil {
			user["type_id"] = c.typeID
		}
		if got := account(user).Properties["is_service_account"]; got != c.want {
			t.Errorf("type_id %v: is_service_account = %v; want %v", c.typeID, got, c.want)
		}
	}
}

// A configuration that is not an object is kept: OCSF types it as any JSON,
// and so does reckon-observation.
func TestGapConfigurationIsAnyJSON(t *testing.T) {
	for _, data := range []any{"raw config text", []any{1, 2}, map[string]any{"a": 1}} {
		res := normalizeEvent(t, map[string]any{"class_uid": 5023, "cloud": map[string]any{"provider": "AWS"},
			"resources": []any{map[string]any{"uid": "r-1", "data": data}}})
		if len(res.ObservedData) != 1 || res.ObservedData[0].Extensions["configuration"] == nil {
			t.Errorf("data %v: configuration not kept: %+v", data, res.ObservedData)
		}
	}
}

// A network-traffic id is minted from what it stores: the ends, the ports and
// the protocols, not the raw protocol_name (adr/0357). So TCP and tcp are one
// connection, and two protocol numbers with no name are two.
func TestGapNetworkTrafficIDFromStoredProtocols(t *testing.T) {
	id := func(conn map[string]any) (identity.STIXID, any) {
		t.Helper()
		res := normalizeEvent(t, map[string]any{"class_uid": 4001,
			"src_endpoint": map[string]any{"ip": "10.0.0.1", "port": 50000}, "dst_endpoint": map[string]any{"ip": "10.0.0.2", "port": 443},
			"connection_info": conn})
		nt := scosOf(res, identity.TypeNetworkTraffic)
		if len(nt) != 1 {
			t.Fatalf("network-traffic = %+v", nt)
		}
		return nt[0].ID, nt[0].Properties["protocols"]
	}
	upper, _ := id(map[string]any{"protocol_name": "TCP"})
	lower, _ := id(map[string]any{"protocol_name": "tcp"})
	if upper != lower {
		t.Errorf("TCP and tcp are two connections: %s, %s", upper, lower)
	}
	tcp, p6 := id(map[string]any{"protocol_num": 6})
	udp, p17 := id(map[string]any{"protocol_num": 17})
	if tcp == udp {
		t.Errorf("protocol_num 6 and 17 share id %s, though they store %v and %v", tcp, p6, p17)
	}
	if tcp != lower {
		t.Errorf("protocol_num 6 and protocol_name tcp store the same protocols and differ: %s, %s", tcp, lower)
	}
	ids := identity.NewResolver(testNS)
	src, dst := ids.IPv4Addr("10.0.0.1"), ids.IPv4Addr("10.0.0.2")
	sp, dp := 50000, 443
	if want := ids.NetworkTraffic(src, dst, &sp, &dp, []string{"tcp"}); tcp != want {
		t.Errorf("id = %s; want %s, from the stored values", tcp, want)
	}
}

// Class rule R3 (adr/0357): an optional number is present only when it
// parses. Ports, pids, sizes and protocol numbers all read through
// ocsf.OptionalNumber.
func TestOptionalNumber(t *testing.T) {
	for _, c := range []struct {
		in   any
		want int64
		ok   bool
	}{
		{"", 0, false}, {"abc", 0, false}, {"443", 0, false}, {nil, 0, false}, {3.5, 0, false},
		{-1, 0, false}, {float64(-1), 0, false}, {float64(70000), 0, false}, {true, 0, false},
		{0, 0, true}, {float64(0), 0, true}, {float64(443), 443, true}, {int64(65535), 65535, true}, {json.Number("22"), 22, true},
	} {
		got, ok := ocsf.OptionalNumber(c.in, maxPort)
		if got != c.want || ok != c.ok {
			t.Errorf("OptionalNumber(%#v) = %d, %v; want %d, %v", c.in, got, ok, c.want, c.ok)
		}
	}
	// Through a normalizer: a port that does not parse is left out, and a
	// pid that does not parse leaves the process without one.
	res := normalizeEvent(t, map[string]any{"class_uid": 4001,
		"src_endpoint": map[string]any{"ip": "10.0.0.1", "port": "abc"}, "dst_endpoint": map[string]any{"ip": "10.0.0.2", "port": -5}})
	nt := scosOf(res, identity.TypeNetworkTraffic)
	if len(nt) != 1 || nt[0].Properties["src_port"] != nil || nt[0].Properties["dst_port"] != nil {
		t.Errorf("ports that do not parse: %+v", nt)
	}
	res = normalizeEvent(t, map[string]any{"class_uid": 1007, "device": map[string]any{"hostname": "WS1"}, "process": map[string]any{"name": "a.exe", "pid": 12.5}})
	if p := scosOf(res, identity.TypeProcess); len(p) != 1 || p[0].Properties["pid"] != nil {
		t.Errorf("a fractional pid: %+v", p)
	}
}

// A directory record names its person in reckon-observation's person_ref,
// with or without an account (adr/0324, adr/0357). TestMain checks the
// objects as STIX and re-mints their ids.
func TestDirectoryRecordNamesItsPerson(t *testing.T) {
	ids := identity.NewResolver(testNS)
	for _, c := range []struct {
		name, product, uid string
		user               map[string]any
		accounts           bool
	}{
		{"a personnel record, no account", "Workday", "W-1001", map[string]any{"uid": "W-1001", "full_name": "Jo Doe"}, false},
		{"a directory record with an account", "Okta", "00u1abc", map[string]any{"uid": "00u1abc", "name": "jdoe@contoso.example", "full_name": "Jo Doe"}, true},
	} {
		res := normalizeEvent(t, map[string]any{"class_uid": 5003, "metadata": map[string]any{"product": map[string]any{"name": c.product}}, "user": c.user})
		person := ids.Person(directorySlug(map[string]any{"metadata": map[string]any{"product": map[string]any{"name": c.product}}}, "fixture:s"), c.uid)
		if len(res.ObservedData) != 1 || res.ObservedData[0].Extensions["person_ref"] != string(person) {
			t.Errorf("%s: observation extensions = %v; want person_ref %s", c.name, res.ObservedData, person)
			continue
		}
		if got := ObservedPersonRefs(res); len(got) != 1 || got[0] != person {
			t.Errorf("%s: ObservedPersonRefs = %v", c.name, got)
		}
		if has := len(scosOf(res, identity.TypeUserAccount)) > 0; has != c.accounts {
			t.Errorf("%s: accounts written = %v; want %v", c.name, has, c.accounts)
		}
	}
	// No person, no person_ref.
	res := normalizeEvent(t, map[string]any{"class_uid": 5003, "metadata": map[string]any{"product": map[string]any{"name": "Okta"}}, "user": map[string]any{"uid": "00u2"}})
	if res.ObservedData[0].Extensions["person_ref"] != nil {
		t.Errorf("a record with no person has person_ref %v", res.ObservedData[0].Extensions["person_ref"])
	}
}

// The resolver's entity refs list the person a directory record describes,
// so a call's produced entities include it.
func TestResolverEntityRefsIncludeThePerson(t *testing.T) {
	dir := &stubAdapter{name: "d", healthy: true, resp: AdapterResponse{SourceTool: "dir", Events: []OcsfPayload{{
		ClassUID: 5003, ClassName: "User Inventory Info", Time: time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC),
		Raw: map[string]any{"class_uid": 5003, "metadata": map[string]any{"product": map[string]any{"name": "Workday"}}, "user": map[string]any{"uid": "W-1001", "full_name": "Jo Doe"}},
	}}}}
	res := newResolver(map[string][]Binding{"v": {{Adapter: "d", Operation: "op", Priority: 1}}}, map[string]Adapter{"d": dir})
	out, err := res.Resolve(context.Background(), "v", CallInput{})
	if err != nil {
		t.Fatal(err)
	}
	person := identity.NewResolver(testNS).Person("workday", "W-1001")
	n := 0
	for _, r := range out.EntityRefs {
		if r == person {
			n++
		}
	}
	if n != 1 {
		t.Errorf("entity refs = %v; want the person once", out.EntityRefs)
	}
}

// The indicator id is minted from the vendor, the pattern, the pattern_type
// and the finding uid (adr/0357): two vendors or two findings with one
// pattern are two indicators, and one finding from one vendor is one. A
// sighting's id includes the observations it cites, so two findings in the
// same second on different evidence are two sightings.
func TestIndicatorAndSightingIDs(t *testing.T) {
	finding := func(vendor, uid, pattern string, evidence map[string]any) NormalizationResult {
		p := detectionPayload()
		p["metadata"] = map[string]any{"product": map[string]any{"vendor_name": vendor}}
		fi := p["finding_info"].(map[string]any)
		fi["uid"] = uid
		p["unmapped"] = map[string]any{"pattern": pattern}
		if evidence != nil {
			p["evidences"] = []any{evidence}
		}
		return normalizeEvent(t, p)
	}
	ind := func(r NormalizationResult) identity.STIXID { return r.Indicators[0].ID }
	sight := func(r NormalizationResult) identity.STIXID { return r.Sightings[0].ID }

	a := finding("CrowdStrike", "det-1", "rule-x", nil)
	if b := finding("SentinelOne", "det-1", "rule-x", nil); ind(a) == ind(b) {
		t.Error("two vendors with one pattern share an indicator id")
	}
	if b := finding("CrowdStrike", "det-2", "rule-x", nil); ind(a) == ind(b) {
		t.Error("two findings with one pattern share an indicator id")
	}
	if b := finding("CrowdStrike", "det-1", "rule-x", nil); ind(a) != ind(b) {
		t.Error("one finding from one vendor gave two indicator ids")
	}

	e1 := finding("CrowdStrike", "det-1", "rule-x", map[string]any{"process": map[string]any{"pid": 1, "name": "a.exe"}})
	e2 := finding("CrowdStrike", "det-1", "rule-x", map[string]any{"process": map[string]any{"pid": 2, "name": "b.exe"}})
	if ind(e1) != ind(e2) {
		t.Fatal("one finding on two pieces of evidence gave two indicators")
	}
	if sight(e1) == sight(e2) {
		t.Error("two sightings in one second on different evidence share an id")
	}
}

// A parent process is read by the same rule as the process and the actor: it
// is written when its pid is present, PID 0 included, and its pid is stored
// as given (adr/0357).
func TestParentProcessPIDZero(t *testing.T) {
	procs := func(parent map[string]any) []SCO {
		t.Helper()
		res := normalizeEvent(t, map[string]any{"class_uid": 1007, "device": map[string]any{"hostname": "WS1"},
			"process": map[string]any{"pid": 4, "name": "smss.exe", "parent_process": parent}})
		return scosOf(res, identity.TypeProcess)
	}
	if p := procs(map[string]any{"pid": 0, "name": "System Idle Process"}); len(p) != 2 || p[1].Properties["pid"] != 0 {
		t.Errorf("a parent with pid 0: processes = %+v; want the parent with pid 0", p)
	}
	if p := procs(map[string]any{"name": "unknown"}); len(p) != 1 {
		t.Errorf("a parent with no pid: processes = %+v; want the process alone", p)
	}
	if p := procs(map[string]any{"pid": "abc", "name": "x"}); len(p) != 1 {
		t.Errorf("a parent whose pid does not parse: processes = %+v; want the process alone", p)
	}
	res := normalizeEvent(t, map[string]any{"class_uid": 4001, "src_endpoint": map[string]any{"ip": "10.0.0.1"}, "dst_endpoint": map[string]any{"ip": "10.0.0.2"},
		"actor": map[string]any{"process": map[string]any{"pid": 0, "name": "System"}}})
	if p := scosOf(res, identity.TypeProcess); len(p) != 1 || p[0].Properties["pid"] != 0 {
		t.Errorf("an actor with pid 0: processes = %+v", p)
	}
}

// A normalizer output the serializer refuses is a normalize error, so the raw
// event is kept with a note and no invalid object is stored (adr/0357).
func TestNormalizeRefusesAnObjectMissingARequiredProperty(t *testing.T) {
	evt := makeEvent(map[string]any{"class_uid": 1007, "time": ms("2026-04-20T14:32:11Z"), "device": map[string]any{"hostname": "WS1"}})
	evt.SourceTool = ""
	if _, err := testRegistry().Normalize(evt); err == nil || !strings.Contains(err.Error(), "reckon-provenance requires tool") {
		t.Errorf("an empty provenance tool: err = %v; want the serializer's refusal", err)
	}
}

// A CIDR block is its network: two spellings of one block are one value and
// one id (adr/0357).
func TestCIDRSpellingsAreOneBlock(t *testing.T) {
	ids := map[identity.STIXID]bool{}
	for _, v := range []string{"10.0.0.5/24", "10.0.0.0/24", " 10.0.0.200/24 "} {
		res := normalizeEvent(t, map[string]any{"class_uid": 4001, "dst_endpoint": map[string]any{"ip": v}})
		for _, s := range scosOf(res, identity.TypeIPv4Addr) {
			ids[s.ID] = true
			if s.Properties["value"] != "10.0.0.0/24" {
				t.Errorf("%q stored as %v; want 10.0.0.0/24", v, s.Properties["value"])
			}
		}
	}
	if len(ids) != 1 {
		t.Errorf("three spellings of one block gave %d ids", len(ids))
	}
}

// A finding uid, title or pattern of white space alone is absent (adr/0357):
// with nothing else, the finding has no rule reference.
func TestDetectionWhitespaceRuleReferenceIsAbsent(t *testing.T) {
	p := detectionPayload()
	p["finding_info"] = map[string]any{"uid": " ", "title": "\t"}
	p["unmapped"] = map[string]any{"pattern": "  "}
	if _, err := testRegistry().Normalize(makeEvent(p)); !errors.Is(err, ErrNoRuleReference) {
		t.Errorf("a white-space uid, title and pattern: err = %v; want ErrNoRuleReference", err)
	}
	p = detectionPayload()
	p["finding_info"] = map[string]any{"uid": " ", "title": " Credential dumping "}
	res := normalizeEvent(t, p)
	if ind := res.Indicators[0]; ind.Pattern != "Credential dumping" || len(ind.ExternalReferences) != 0 {
		t.Errorf("a white-space uid: pattern %q, references %v; want the trimmed title and no uid reference", ind.Pattern, ind.ExternalReferences)
	}
}
