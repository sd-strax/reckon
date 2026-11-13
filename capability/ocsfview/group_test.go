package ocsfview

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// TestSlots_EveryTemplateRegistersItsCatalogue: a template registers exactly
// the slots the catalogue lists for its class, in order, so a key chooser
// built from Slots() names values the fold will find.
func TestSlots_EveryTemplateRegistersItsCatalogue(t *testing.T) {
	for _, class := range Classes() {
		v := Render(Event{ClassUID: class, Payload: map[string]any{}}, nil)
		var got []string
		for _, s := range v.Slots {
			got = append(got, s.Name)
		}
		var want []string
		for _, s := range Slots(class) {
			want = append(want, s.Name)
		}
		if !slices.Equal(got, want) {
			t.Errorf("class %d registers %v; catalogue %v", class, got, want)
		}
		for _, n := range DefaultKey(class) {
			if !hasSlot(class, n) {
				t.Errorf("class %d default key names %q, not a slot", class, n)
			}
		}
	}
	if Slots(8001) != nil || len(DefaultKey(8001)) != 0 {
		t.Error("a class without a template has slots")
	}
}

func authItem(id string, at string, js string, ents []Entity) GroupItem {
	ts, err := time.Parse(time.RFC3339, at)
	if err != nil {
		panic(err)
	}
	v := Render(Event{ClassUID: classAuthentication, Payload: payload(js)}, ents)
	return GroupItem{ID: id, ClassUID: classAuthentication, Time: ts, Slots: v.Slots}
}

// The reference scenario's logons on the file server (fixtures/lateral-movement-via-rdp).
func scenarioLogons() []GroupItem {
	ents := []Entity{
		ent("user-account--svc", map[string]any{"account_login": `CONTOSO\svc_backup`}),
		ent("user-account--jdoe", map[string]any{"account_login": `CONTOSO\jdoe`}),
		ent("x-host--file01", map[string]any{"hostname": "WIN-FILE01"}),
		ent("ipv4-addr--bkp", map[string]any{"value": "10.0.9.10"}),
		ent("ipv4-addr--ws20", map[string]any{"value": "10.0.4.20"}),
		ent("ipv4-addr--ws55", map[string]any{"value": "10.0.4.55"}),
	}
	nightly := `{"user":{"name":"svc_backup","domain":"CONTOSO"},"dst_endpoint":{"hostname":"WIN-FILE01"},"src_endpoint":{"ip":"10.0.9.10"},"logon_type":"Network","status":"SUCCESS"}`
	var items []GroupItem
	for d := 13; d <= 19; d++ {
		items = append(items, authItem(fmt.Sprintf("n%d", d), fmt.Sprintf("2026-04-%02dT02:00:10Z", d), nightly, ents))
	}
	items = append(items,
		authItem("jdoe", "2026-04-20T14:32:11Z", `{"user":{"name":"jdoe","domain":"CONTOSO"},"dst_endpoint":{"hostname":"WIN-FILE01"},"src_endpoint":{"ip":"10.0.4.55"},"logon_type":"RemoteInteractive","status":"SUCCESS"}`, ents),
		authItem("rdp", "2026-04-20T14:32:47Z", `{"user":{"name":"svc_backup","domain":"CONTOSO"},"dst_endpoint":{"hostname":"WIN-FILE01"},"src_endpoint":{"ip":"10.0.4.20"},"logon_type":"RemoteInteractive","status":"SUCCESS"}`, ents),
	)
	return items
}

// TestFold_Scenario: the seven nightly logons fold into one group over seven
// days and the two RDP logons stand apart, newest group first.
func TestFold_Scenario(t *testing.T) {
	groups := Fold(scenarioLogons(), nil, "")
	if len(groups) != 3 {
		t.Fatalf("groups = %d; want 3: %+v", len(groups), groups)
	}
	// Newest last event first: the two Apr 20 logons, then the nightly group.
	if groups[0].EventIDs[0] != "rdp" || groups[1].EventIDs[0] != "jdoe" {
		t.Errorf("order = %v, %v", groups[0].EventIDs, groups[1].EventIDs)
	}
	g := groups[2]
	if g.Count != 7 || g.Days != 7 || g.First.Day() != 13 || g.Last.Day() != 19 || len(g.EventIDs) != 7 || g.EventIDs[0] != "n19" {
		t.Errorf("nightly group = %+v", g)
	}
	var key []string
	for _, p := range g.Key {
		key = append(key, p.Slot+"="+p.Text+"@"+p.Ref)
	}
	want := []string{"user=svc_backup@user-account--svc", "logon_type=Network@", "src=10.0.9.10@ipv4-addr--bkp", "dst=WIN-FILE01@x-host--file01"}
	if !slices.Equal(key, want) {
		t.Errorf("key = %v; want %v", key, want)
	}
	if g.Key[0].Label != "Account" {
		t.Errorf("label = %q", g.Key[0].Label)
	}
}

// TestFold_PivotDropsTheEntity: pivoted to the backup account its slot leaves
// the key; pivoted to the host, the account stays and the two RDP logons by
// different users do not fold.
func TestFold_PivotDropsTheEntity(t *testing.T) {
	groups := Fold(scenarioLogons(), nil, "user-account--svc")
	for _, g := range groups {
		for _, p := range g.Key {
			if p.Ref == "user-account--svc" {
				t.Errorf("pivoted entity in the key: %+v", g.Key)
			}
		}
	}
	if len(groups) != 3 {
		t.Errorf("groups pivoted on the account = %d", len(groups))
	}
	groups = Fold(scenarioLogons(), nil, "x-host--file01")
	if len(groups) != 3 {
		t.Errorf("groups pivoted on the host = %d; want the two users apart", len(groups))
	}
	// With the account out of the key by choice, the two RDP logons fold.
	groups = Fold(scenarioLogons(), Keys{classAuthentication: {"logon_type", "dst"}}, "")
	if len(groups) != 2 || groups[0].Count != 2 {
		t.Errorf("groups by logon type and destination = %+v", groups)
	}
}

// TestFold_ImageKeysByHash: two launches of one binary under different names
// fold when the record minted the file by hash; two binaries of one name do
// not; a missing image falls back to the process name.
func TestFold_ImageKeysByHash(t *testing.T) {
	at := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	item := func(id, js string, ents []Entity) GroupItem {
		v := Render(Event{ClassUID: classProcess, Payload: payload(js)}, ents)
		return GroupItem{ID: id, ClassUID: classProcess, Time: at, Slots: v.Slots}
	}
	same := []Entity{ent("file--abc", map[string]any{"name": "", "hashes": map[string]any{"SHA-256": "abc"}})}
	items := []GroupItem{
		item("a", `{"process":{"pid":1,"name":"tool.exe","file":{"name":"tool.exe","hashes":[{"algorithm":"SHA-256","value":"abc"}]},"parent_process":{"name":"cmd.exe"}}}`, same),
		item("b", `{"process":{"pid":2,"name":"renamed.exe","file":{"name":"renamed.exe","hashes":[{"algorithm":"SHA-256","value":"abc"}]},"parent_process":{"name":"cmd.exe"}}}`, same),
		item("c", `{"process":{"pid":3,"name":"tool.exe","file":{"name":"tool.exe","hashes":[{"algorithm":"SHA-256","value":"zzz"}]},"parent_process":{"name":"cmd.exe"}}}`,
			[]Entity{ent("file--zzz", map[string]any{"name": "tool.exe", "hashes": map[string]any{"SHA-256": "zzz"}})}),
		item("d", `{"process":{"pid":4,"name":"tool.exe"}}`, nil),
		item("e", `{"process":{"pid":5,"name":"tool.exe"}}`, nil),
	}
	groups := Fold(items, Keys{classProcess: {"image"}}, "")
	counts := map[string]int{}
	for _, g := range groups {
		counts[g.EventIDs[0]] = g.Count
	}
	// a+b by hash; c alone (other hash, same name); d+e by name with no object.
	if len(groups) != 3 || counts["a"] != 2 || counts["c"] != 1 || counts["d"] != 2 {
		t.Errorf("groups = %+v", groups)
	}
}

// TestFold_NoValueNeverFolds: items whose key has no value each stand alone,
// and a class without slots never folds.
func TestFold_NoValueNeverFolds(t *testing.T) {
	at := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	items := []GroupItem{
		{ID: "a", ClassUID: classNetwork, Time: at, Slots: Render(Event{ClassUID: classNetwork, Payload: map[string]any{}}, nil).Slots},
		{ID: "b", ClassUID: classNetwork, Time: at, Slots: Render(Event{ClassUID: classNetwork, Payload: map[string]any{}}, nil).Slots},
		{ID: "c", ClassUID: 8001, Time: at},
		{ID: "d", ClassUID: 8001, Time: at},
	}
	if groups := Fold(items, nil, ""); len(groups) != 4 {
		t.Errorf("groups = %d; want each alone", len(groups))
	}
}

func TestParseKeys(t *testing.T) {
	for _, spec := range []string{"", "default", " default "} {
		if k, err := ParseKeys(spec); k != nil || err != nil {
			t.Errorf("ParseKeys(%q) = %v, %v", spec, k, err)
		}
	}
	k, err := ParseKeys("3002:user,logon_type; 4001:dst")
	if err != nil || !slices.Equal(k[classAuthentication], []string{"user", "logon_type"}) || !slices.Equal(k[classNetwork], []string{"dst"}) {
		t.Errorf("ParseKeys = %v, %v", k, err)
	}
	for _, bad := range []string{"3002", "9999:user", "3002:nope", "x:user"} {
		if _, err := ParseKeys(bad); err == nil {
			t.Errorf("ParseKeys(%q) accepted", bad)
		}
	}
}

// TestFold_ChangedObservationsStayApart: the all-slots default keeps the
// values that change between otherwise identical events — a DNS answer, a
// registry value's data, a job's command line — so a one-off change never
// disappears into a repeated row. Opaque values compare exactly (-I is not
// -i) while names, hosts and keys fold for case; a list keeps its boundaries
// ("a,b" is not "a" and "b"); and a domain-only destination keys by the
// domain.
func TestFold_ChangedObservationsStayApart(t *testing.T) {
	at := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	item := func(id string, class int, js string) GroupItem {
		return GroupItem{ID: id, ClassUID: class, Time: at, Slots: Render(Event{ClassUID: class, Payload: payload(js)}, nil).Slots}
	}
	cases := []struct {
		name  string
		items []GroupItem
		keys  Keys // nil: the default key
		want  int
	}{
		{"dns answers", []GroupItem{
			item("a", classDNS, `{"query":{"hostname":"x.example"},"answers":[{"rdata":"1.1.1.1"},{"rdata":"2.2.2.2"}]}`),
			item("b", classDNS, `{"query":{"hostname":"x.example"},"answers":[{"rdata":"2.2.2.2"},{"rdata":"1.1.1.1"}]}`),
			item("c", classDNS, `{"query":{"hostname":"x.example"},"answers":[{"rdata":"9.9.9.9"}]}`),
		}, nil, 2},
		{"registry data", []GroupItem{
			item("a", classRegistryValue, `{"reg_value":{"path":"HKLM\\Run","name":"U","data":"C:\\a.exe"}}`),
			item("b", classRegistryValue, `{"reg_value":{"path":"HKLM\\Run","name":"U","data":"C:\\b.exe"}}`),
		}, nil, 2},
		{"job command line", []GroupItem{
			item("a", classScheduledJob, `{"job":{"name":"Updater","cmd_line":"u.exe -q"}}`),
			item("b", classScheduledJob, `{"job":{"name":"Updater","cmd_line":"u.exe -q -x"}}`),
		}, nil, 2},
		{"txt answer boundaries", []GroupItem{
			item("a", classDNS, `{"query":{"hostname":"t.example"},"answers":[{"rdata":"a,b"}]}`),
			item("b", classDNS, `{"query":{"hostname":"t.example"},"answers":[{"rdata":"a"},{"rdata":"b"}]}`),
			item("c", classDNS, `{"query":{"hostname":"t.example"},"answers":[{"rdata":"b"},{"rdata":"a"}]}`),
		}, nil, 2},
		{"command line case", []GroupItem{
			item("a", classScheduledJob, `{"job":{"name":"Probe","cmd_line":"curl.exe -I https://example.com"}}`),
			item("b", classScheduledJob, `{"job":{"name":"Probe","cmd_line":"curl.exe -i https://example.com"}}`),
			item("c", classScheduledJob, `{"job":{"name":"probe","cmd_line":"curl.exe -I https://example.com"}}`),
		}, nil, 2},
		{"process command line case, keyed on it", []GroupItem{
			item("a", classProcess, `{"process":{"name":"curl.exe","cmd_line":"curl.exe -I https://example.com"},"actor":{"process":{"name":"cmd.exe"}}}`),
			item("b", classProcess, `{"process":{"name":"curl.exe","cmd_line":"curl.exe -i https://example.com"},"actor":{"process":{"name":"cmd.exe"}}}`),
		}, Keys{classProcess: {"image", "cmd_line"}}, 2},
		{"registry data case", []GroupItem{
			item("a", classRegistryValue, `{"reg_value":{"path":"HKLM\\Run","name":"U","data":"C:\\a.exe -Q"}}`),
			item("b", classRegistryValue, `{"reg_value":{"path":"hklm\\run","name":"u","data":"C:\\a.exe -q"}}`),
		}, nil, 2},
		{"domain-only destinations", []GroupItem{
			item("a", classNetwork, `{"dst_endpoint":{"domain":"a.example","port":443}}`),
			item("b", classNetwork, `{"dst_endpoint":{"domain":"b.example","port":443}}`),
			item("c", classNetwork, `{"dst_endpoint":{"domain":"A.example","port":443}}`),
		}, nil, 2},
	}
	for _, tc := range cases {
		if got := len(Fold(tc.items, tc.keys, "")); got != tc.want {
			t.Errorf("%s: %d groups; want %d", tc.name, got, tc.want)
		}
	}
}

// TestFold_ExplicitEmptyKey: a class named with no slots keys on nothing and
// never folds, where an absent class folds on its default; a class without
// slots in the catalogue is accepted that way.
func TestFold_ExplicitEmptyKey(t *testing.T) {
	keys, err := ParseKeys("3002:;8001:")
	if err != nil || keys[classAuthentication] == nil || len(keys[classAuthentication]) != 0 || keys[8001] == nil {
		t.Fatalf("ParseKeys = %v, %v; want present, empty entries", keys, err)
	}
	if groups := Fold(scenarioLogons(), keys, ""); len(groups) != 9 {
		t.Errorf("groups with no key = %d; want every logon alone", len(groups))
	}
	if _, err := ParseKeys("8001:user"); err == nil {
		t.Error("a slot on a class without slots accepted")
	}
}
