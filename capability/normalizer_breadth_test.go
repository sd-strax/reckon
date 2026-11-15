package capability

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsfschema"
	"github.com/sd-strax/reckon/internal/stixschema"
	"github.com/sd-strax/reckon/stix"
)

// registryKeyPath is the Run key the registry tests name, hive first and
// abbreviated; runKey is the key it expands to.
const (
	registryKeyPath = `HKLM\Software\Microsoft\Windows\CurrentVersion\Run`
	runKey          = `HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\Run`
)

// TestRegistryNormalizer (§4.6, adr/0341): a registry_key_activity event
// gives the host-scoped windows-registry-key with its hive expanded and no
// values, the actor process and the host, with the activity in the
// extensions.
func TestRegistryNormalizer(t *testing.T) {
	res, err := testRegistry().Normalize(makeEvent(map[string]any{
		"class_uid":     201001,
		"time":          ms("2026-04-20T14:32:11Z"),
		"activity_name": "Create",
		"reg_key":       map[string]any{"path": registryKeyPath},
		"actor":         map[string]any{"process": map[string]any{"pid": 1234, "created_time": ms("2026-04-20T14:00:00Z")}},
		"device":        map[string]any{"hostname": "WS1"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	bt := countTypes(res.SCOs)
	if bt[identity.TypeWindowsRegistryKey] != 1 || bt[identity.TypeProcess] != 1 || bt[identity.TypeHost] != 1 {
		t.Errorf("want 1 registry-key + 1 process + 1 host; got %v", bt)
	}
	key := scosOf(res, identity.TypeWindowsRegistryKey)[0]
	if want := map[string]any{"key": runKey}; !reflect.DeepEqual(key.Properties, want) {
		t.Errorf("key = %v; want %v", key.Properties, want)
	}
	ids := identity.NewResolver(testNS)
	host := ids.Host(identity.HostIdentity{Hostname: "WS1"})
	if want := ids.RegistryKey(host, runKey, nil); key.ID != want {
		t.Errorf("key id = %s; want %s", key.ID, want)
	}
	od := res.ObservedData[0]
	if od.Extensions["activity"] != "Create" || od.Provenance.Normalizer != "registry_key_activity" {
		t.Errorf("extensions %v, normalizer %q", od.Extensions, od.Provenance.Normalizer)
	}
}

// TestRegistryValueNormalizer (§4.6, adr/0341): a registry_value_activity
// event names the key in reg_value.path and holds the value in values, with
// its data as a string and a STIX data_type. The id carries the value's name
// but not its data, and HKLM and HKEY_LOCAL_MACHINE are one key.
func TestRegistryValueNormalizer(t *testing.T) {
	value := func(v map[string]any) NormalizationResult {
		t.Helper()
		res, err := testRegistry().Normalize(makeEvent(map[string]any{
			"class_uid": 201002, "time": ms("2026-04-20T14:32:11Z"), "activity_id": 2,
			"reg_value": v, "device": map[string]any{"hostname": "WS1"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	key := func(v map[string]any) SCO {
		t.Helper()
		keys := scosOf(value(v), identity.TypeWindowsRegistryKey)
		if len(keys) != 1 {
			t.Fatalf("%v: %d keys; want one", v, len(keys))
		}
		return keys[0]
	}
	// As the fixtures write it: the full hive name.
	res := value(map[string]any{"path": runKey, "name": "Updater", "data": `C:\Temp\evil.exe`, "type": "REG_SZ", "type_id": 10})
	got := scosOf(res, identity.TypeWindowsRegistryKey)[0]
	want := map[string]any{"key": runKey, "values": []any{map[string]any{"name": "Updater", "data": `C:\Temp\evil.exe`, "data_type": "REG_SZ"}}}
	if !reflect.DeepEqual(got.Properties, want) {
		t.Errorf("key = %v; want %v", got.Properties, want)
	}
	ids := identity.NewResolver(testNS)
	host := ids.Host(identity.HostIdentity{Hostname: "WS1"})
	updater := "Updater"
	if id := ids.RegistryKey(host, runKey, &updater); got.ID != id {
		t.Errorf("key id = %s; want %s", got.ID, id)
	}
	if other := key(map[string]any{"path": registryKeyPath, "name": "updater", "data": "other.exe"}); other.ID != got.ID {
		t.Errorf("HKLM, a lower-cased name and other data: id %s; want %s", other.ID, got.ID)
	}
	od := res.ObservedData[0]
	if od.Extensions["activity_id"] != 2 || od.Provenance.Normalizer != "registry_value_activity" {
		t.Errorf("extensions %v, normalizer %q", od.Extensions, od.Provenance.Normalizer)
	}

	// data_type: the caption when STIX has it, else the caption of the id;
	// left out when neither is a STIX name. Data that is not a string is its
	// JSON text, and a number float64 cannot state exactly is left out.
	for _, tc := range []struct {
		in   map[string]any
		want map[string]any
	}{
		{map[string]any{"path": runKey, "name": "Start", "data": float64(4), "type_id": 2},
			map[string]any{"name": "Start", "data": "4", "data_type": "REG_DWORD"}},
		{map[string]any{"path": runKey, "name": "List", "data": []any{"a", "b"}, "type": "Multi", "type_id": 6},
			map[string]any{"name": "List", "data": `["a","b"]`, "data_type": "REG_MULTI_SZ"}},
		{map[string]any{"path": runKey, "name": "Q", "data": true, "type_id": 9},
			map[string]any{"name": "Q", "data": "true"}},
		// A FILETIME past 2^53 arrives rounded, so its data is left out.
		{map[string]any{"path": runKey, "name": "T", "data": float64(133445566778899001), "type": "REG_QWORD"},
			map[string]any{"name": "T", "data_type": "REG_QWORD"}},
		{map[string]any{"path": runKey, "name": ""},
			map[string]any{"name": ""}},
	} {
		if v := key(tc.in).Properties["values"]; !reflect.DeepEqual(v, []any{tc.want}) {
			t.Errorf("%v: values = %v; want [%v]", tc.in, v, tc.want)
		}
	}

	// A value with no name but is_default true is the default value: the
	// same id and values as the value named "".
	named, isDefault := key(map[string]any{"path": runKey, "name": ""}), key(map[string]any{"path": runKey, "is_default": true})
	if isDefault.ID != named.ID || !reflect.DeepEqual(isDefault.Properties, named.Properties) {
		t.Errorf("is_default with no name = %s %v; want %s %v", isDefault.ID, isDefault.Properties, named.ID, named.Properties)
	}

	// A hive alone is a key. An event that names no key mints none.
	if hive := key(map[string]any{"path": `HKLM\`, "name": "x"}); hive.Properties["key"] != "HKEY_LOCAL_MACHINE" {
		t.Errorf("hive-only key = %v", hive.Properties)
	}
	if n := len(scosOf(value(map[string]any{"name": "x"}), identity.TypeWindowsRegistryKey)); n != 0 {
		t.Errorf("no path: %d keys; want none", n)
	}
}

// TestRegistryValueTypedData (§4.6, adr/0359, adr/0341): from OCSF 1.6.0 a
// value's data can arrive in a field for its type. The first typed field
// that holds a value is read, then data, and written by adr/0341's rule: a
// string as it arrives, anything else as its JSON text. An empty typed field
// does not hide the next one, and a number float64 cannot state exactly is
// left out. data_type comes only from the source's type, never from which
// typed field held the data.
func TestRegistryValueTypedData(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value map[string]any
		want  any // the value's data, nil for none
	}{
		{"reg_string_data only", map[string]any{"reg_string_data": `C:\Temp\evil.exe`}, `C:\Temp\evil.exe`},
		{"reg_integer_data only", map[string]any{"reg_integer_data": float64(4)}, "4"},
		{"reg_binary_data only", map[string]any{"reg_binary_data": "3q2+7w=="}, "3q2+7w=="},
		{"reg_string_list_data only", map[string]any{"reg_string_list_data": []any{"a.dll", "b.dll"}}, `["a.dll","b.dll"]`},
		{"the typed field before data", map[string]any{"data": "old", "reg_string_data": "new"}, "new"},
		{"an empty string does not hide an integer", map[string]any{"type_id": 4, "reg_string_data": "", "reg_integer_data": float64(4)}, "4"},
		{"an empty list does not hide data", map[string]any{"reg_string_list_data": []any{}, "data": "x"}, "x"},
		// A JSON number past 2^53 arrives rounded: it is not written.
		{"a rounded integer", map[string]any{"reg_integer_data": float64(9007199254740993), "data": "x"}, "x"},
		{"a rounded integer alone", map[string]any{"reg_integer_data": float64(9007199254740993)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := map[string]any{"path": registryKeyPath, "name": "Updater"}
			for k, x := range tc.value {
				v[k] = x
			}
			e := versioned(201002, "1.9.0", map[string]any{"reg_value": v, "device": map[string]any{"hostname": "WS1"}})
			if bad := ocsfschema.ValidateFields(e); len(bad) > 0 {
				t.Fatalf("OCSF 1.9.0: %v", bad)
			}
			res, err := testRegistry().Normalize(makeEvent(e))
			if err != nil {
				t.Fatal(err)
			}
			values, _ := scosOf(res, identity.TypeWindowsRegistryKey)[0].Properties["values"].([]any)
			if len(values) != 1 {
				t.Fatalf("values = %v; want one", values)
			}
			value := values[0].(map[string]any)
			if got := value["data"]; got != tc.want {
				t.Errorf("data = %#v; want %#v", got, tc.want)
			}
			_, typed := value["data_type"]
			if wantType := tc.value["type_id"] != nil; typed != wantType {
				t.Errorf("value = %v; data_type set = %v, want %v", value, typed, wantType)
			}
		})
	}
}

// TestRegistryClassesReadTheirOwnFields (adr/0356): a key event does not read
// reg_value and a value event does not read reg_key, and class 1003 (OCSF's
// Kernel Activity) is not a registry class.
func TestRegistryClassesReadTheirOwnFields(t *testing.T) {
	reg := testRegistry()
	both := func(class int) map[string]any {
		return map[string]any{
			"class_uid": class, "time": ms("2026-04-20T14:32:11Z"),
			"reg_key":   map[string]any{"path": `HKLM\FromKey`},
			"reg_value": map[string]any{"path": `HKLM\FromValue`, "name": "v"},
			"device":    map[string]any{"hostname": "WS1"},
		}
	}
	for class, want := range map[int]string{201001: "FromKey", 201002: "FromValue"} {
		res, err := reg.Normalize(makeEvent(both(class)))
		if err != nil {
			t.Fatal(err)
		}
		keys := scosOf(res, identity.TypeWindowsRegistryKey)
		if len(keys) != 1 || keys[0].Properties["key"] != `HKEY_LOCAL_MACHINE\`+want {
			t.Errorf("class %d: keys = %+v; want one at %s", class, keys, want)
		}
	}
	evt := makeEvent(both(1003))
	res, err := reg.Normalize(evt)
	if err != nil {
		t.Fatal(err)
	}
	recordArtifactOnly(t, evt, res)
	if res.ObservedData[0].Provenance.Normalizer != "opaque" {
		t.Errorf("class 1003: under %q; want the opaque default", res.ObservedData[0].Provenance.Normalizer)
	}
}

// TestScheduledJobNormalizer (§4.7): host-scoped scheduled task + principal
// user, the task path from job.file.path.
func TestScheduledJobNormalizer(t *testing.T) {
	res, err := testRegistry().Normalize(makeEvent(map[string]any{
		"class_uid": 1006,
		"time":      ms("2026-04-20T14:32:11Z"),
		"job": map[string]any{
			"name": "EvilTask", "file": map[string]any{"path": `\Microsoft\Windows\EvilTask`},
			"cmd_line": "evil.exe", "user": map[string]any{"name": "SYSTEM"},
		},
		"device": map[string]any{"hostname": "WS1"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	bt := countTypes(res.SCOs)
	if bt[identity.TypeScheduledTask] != 1 || bt[identity.TypeUserAccount] != 1 || bt[identity.TypeHost] != 1 {
		t.Errorf("want 1 scheduled-task + 1 user + 1 host; got %v", bt)
	}
	task := scosOf(res, identity.TypeScheduledTask)[0]
	if task.Properties["path"] != `\Microsoft\Windows\EvilTask` || task.Properties["command_line"] != "evil.exe" {
		t.Errorf("task = %v", task.Properties)
	}
	if _, ok := task.Properties["command_lines"]; ok {
		t.Errorf("task = %v; want no command_lines for one command", task.Properties)
	}
}

// TestScheduledJobCommandLines (§4.7, adr/0356): the command line comes from
// the job's actions, else from the deprecated job.cmd_line. command_line is
// the first, and command_lines lists them all when there are several.
func TestScheduledJobCommandLines(t *testing.T) {
	action := func(cmd string) any { return map[string]any{"type_id": 1, "cmd_line": cmd} }
	for _, tc := range []struct {
		name  string
		job   map[string]any
		first string
		all   []string
	}{
		{"one action", map[string]any{"job_actions": []any{action("a.exe")}}, "a.exe", nil},
		{"several actions", map[string]any{"job_actions": []any{action("a.exe"), action("b.exe /q")}}, "a.exe", []string{"a.exe", "b.exe /q"}},
		{"actions outrank the deprecated field", map[string]any{"job_actions": []any{action("a.exe")}, "cmd_line": "old.exe"}, "a.exe", nil},
		{"an action without a command falls back", map[string]any{"job_actions": []any{map[string]any{"type_id": 2}}, "cmd_line": "old.exe"}, "old.exe", nil},
		{"the deprecated field alone", map[string]any{"cmd_line": "old.exe"}, "old.exe", nil},
		{"no command", map[string]any{}, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.job["name"] = "Task"
			res, err := testRegistry().Normalize(makeEvent(map[string]any{
				"class_uid": 1006, "time": ms("2026-04-20T14:32:11Z"), "job": tc.job, "device": map[string]any{"hostname": "WS1"},
			}))
			if err != nil {
				t.Fatal(err)
			}
			task := scosOf(res, identity.TypeScheduledTask)[0]
			if task.Properties["command_line"] != tc.first {
				t.Errorf("command_line = %v; want %q", task.Properties["command_line"], tc.first)
			}
			got, _ := task.Properties["command_lines"].([]string)
			if !reflect.DeepEqual(got, tc.all) {
				t.Errorf("command_lines = %v; want %v", got, tc.all)
			}
		})
	}
}

// TestModuleNormalizer (§4.8): module file + loading process, with a loads
// edge, under OCSF's class 1005. 1009 is Script Activity, not this class.
func TestModuleNormalizer(t *testing.T) {
	payload := func(class int) map[string]any {
		return map[string]any{
			"class_uid": class,
			"time":      ms("2026-04-20T14:32:11Z"),
			"module": map[string]any{"file": map[string]any{
				"name": "evil.dll", "path": `C:\Temp\evil.dll`,
				"hashes": []any{map[string]any{"algorithm": "SHA-256", "value": "88d4266fd4e6338d13b845fcf289579d209c897823b9217da3e161936f031589"}},
			}},
			"actor":  map[string]any{"process": map[string]any{"pid": 999, "created_time": ms("2026-04-20T14:00:00Z")}},
			"device": map[string]any{"hostname": "WS1"},
		}
	}
	res, err := testRegistry().Normalize(makeEvent(payload(1005)))
	if err != nil {
		t.Fatal(err)
	}
	if countTypes(res.SCOs)[identity.TypeFile] != 1 {
		t.Errorf("want 1 file SCO; got %v", countTypes(res.SCOs))
	}
	if !hasRel(res.Relationships, RelLoads) {
		t.Error("missing loads relationship (process → module)")
	}
	oldEvt := makeEvent(payload(1009))
	old, err := testRegistry().Normalize(oldEvt)
	if err != nil {
		t.Fatal(err)
	}
	recordArtifactOnly(t, oldEvt, old)
	if old.ObservedData[0].Provenance.Normalizer != "opaque" {
		t.Errorf("class 1009: under %q; want the opaque default", old.ObservedData[0].Provenance.Normalizer)
	}
}

// TestFileHashesListFormOnly (adr/0356): OCSF writes hashes as a list of
// fingerprints. An object keyed by algorithm is not OCSF and is not read.
func TestFileHashesListFormOnly(t *testing.T) {
	file := func(hashes any) SCO {
		t.Helper()
		res, err := testRegistry().Normalize(makeEvent(map[string]any{
			"class_uid": 1001, "time": ms("2026-04-20T14:32:11Z"),
			"file":   map[string]any{"name": "evil.dll", "hashes": hashes},
			"device": map[string]any{"hostname": "WS1"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		return scosOf(res, identity.TypeFile)[0]
	}
	list := file([]any{map[string]any{"algorithm": "SHA-256", "algorithm_id": 3, "value": testSHA256}, "not a fingerprint", map[string]any{"algorithm": "MD5"}})
	if !reflect.DeepEqual(list.Properties["hashes"], map[string]string{"SHA-256": testSHA256}) {
		t.Errorf("list form: hashes = %v", list.Properties["hashes"])
	}
	object := file(map[string]any{"SHA-256": testSHA256})
	if _, ok := object.Properties["hashes"]; ok || object.ID == list.ID {
		t.Errorf("object form was read: %v", object.Properties)
	}
}

// TestFileHashesByAlgorithmID (adr/0356): a fingerprint's algorithm caption
// is optional in OCSF and its algorithm_id is required. A hash that carries
// only the id is kept under the id's caption, and gives the file the same
// identity as the same hash written with the caption.
func TestFileHashesByAlgorithmID(t *testing.T) {
	file := func(hashes ...any) SCO {
		t.Helper()
		res, err := testRegistry().Normalize(makeEvent(map[string]any{
			"class_uid": 1001, "time": ms("2026-04-20T14:32:11Z"),
			"file":   map[string]any{"name": "evil.dll", "hashes": hashes},
			"device": map[string]any{"hostname": "WS1"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		return scosOf(res, identity.TypeFile)[0]
	}
	byID := file(map[string]any{"algorithm_id": float64(3), "value": testSHA256}, map[string]any{"algorithm_id": float64(1), "value": testMD5})
	if want := map[string]string{"SHA-256": testSHA256, "MD5": testMD5}; !reflect.DeepEqual(byID.Properties["hashes"], want) {
		t.Errorf("hashes = %v; want %v", byID.Properties["hashes"], want)
	}
	byCaption := file(map[string]any{"algorithm": "SHA-256", "algorithm_id": float64(3), "value": testSHA256})
	if byID.ID != byCaption.ID {
		t.Errorf("the same SHA-256 gives %s by id and %s by caption", byID.ID, byCaption.ID)
	}
	if unnamed := file(map[string]any{"algorithm_id": float64(99), "value": testSHA256}); unnamed.Properties["hashes"] != nil {
		t.Errorf("a hash whose algorithm is not named was kept: %v", unnamed.Properties["hashes"])
	}
}

// accountEvent is an account event of class naming alice as the target and
// admin as the initiator, with a group and privileges on it whatever the
// class.
func accountEvent(class int, activity string) map[string]any {
	return map[string]any{
		"class_uid":     class,
		"time":          ms("2026-04-20T14:32:11Z"),
		"activity_name": activity,
		"user":          map[string]any{"name": "alice", "domain": "CONTOSO", "uid": "S-1-5-21-7-1101"},
		"actor":         map[string]any{"user": map[string]any{"name": "admin", "domain": "CONTOSO", "uid": "S-1-5-21-7-500"}},
		"group":         map[string]any{"name": "Domain Admins", "domain": "contoso.com", "uid": "g-512"},
		"privileges":    []any{"SeDebugPrivilege", "SeBackupPrivilege"},
		"device":        map[string]any{"hostname": "DC01"},
	}
}

// TestAccountNormalizer (§4.9, adr/0356): the three account classes share one
// function, and each reads only its own fields. All give the target, the
// initiator and the host. Only group_management gives the group. Only
// user_access and group_management keep the privileges, as a list.
func TestAccountNormalizer(t *testing.T) {
	privileges := []string{"SeDebugPrivilege", "SeBackupPrivilege"}
	for _, tc := range []struct {
		class      int
		name       string
		group      bool
		privileges []string
	}{
		{3001, "account_change", false, nil},
		{3005, "user_access", false, privileges},
		{3006, "group_management", true, privileges},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := testRegistry().Normalize(makeEvent(accountEvent(tc.class, "Other")))
			if err != nil {
				t.Fatal(err)
			}
			bt := countTypes(res.SCOs)
			// Each user carries a SID beside its login: the SID account and the
			// login account aliased to it (adr/0324).
			if bt[identity.TypeUserAccount] != 4 || bt[identity.TypeHost] != 1 {
				t.Errorf("want 2 users and their 2 login accounts + 1 host; got %v", bt)
			}
			if got := bt[identity.TypeGroup] == 1; got != tc.group {
				t.Errorf("group minted = %v; want %v", got, tc.group)
			}
			if got := hasRel(res.Relationships, RelMemberOfGroup); got != tc.group {
				t.Errorf("member-of-group = %v; want %v", got, tc.group)
			}
			od := res.ObservedData[0]
			if od.Extensions["activity"] != "Other" || od.Provenance.Normalizer != tc.name {
				t.Errorf("extensions %v, normalizer %q", od.Extensions, od.Provenance.Normalizer)
			}
			got, _ := od.Extensions["privileges"].([]string)
			if !reflect.DeepEqual(got, tc.privileges) {
				t.Errorf("privileges = %v; want %v", od.Extensions["privileges"], tc.privileges)
			}
			if _, ok := od.Extensions["privilege"]; ok {
				t.Errorf("extensions %v carry the old single privilege", od.Extensions)
			}
		})
	}
}

// TestGroupManagementGroup (§4.9): the group is keyed on its directory and
// name, the member-of-group edge runs from the target, and a group with no
// name is named by its uid.
func TestGroupManagementGroup(t *testing.T) {
	ids := identity.NewResolver(testNS)
	res, err := testRegistry().Normalize(makeEvent(accountEvent(3006, "Add User")))
	if err != nil {
		t.Fatal(err)
	}
	group := ids.Group("contoso.com", "Domain Admins")
	target := ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: "S-1-5-21-7-1101"})
	if rels := relsOf(res, RelMemberOfGroup); len(rels) != 1 || rels[0] != [2]identity.STIXID{target, group} {
		t.Errorf("member-of-group = %v; want %s → %s", rels, target, group)
	}

	byUID := accountEvent(3006, "Add User")
	byUID["group"] = map[string]any{"uid": "g-512"}
	res, err = testRegistry().Normalize(makeEvent(byUID))
	if err != nil {
		t.Fatal(err)
	}
	if g := scosOf(res, identity.TypeGroup); len(g) != 1 || g[0].ID != ids.Group("", "g-512") {
		t.Errorf("group = %+v; want one named by its uid", g)
	}

	noGroup := accountEvent(3006, "Assign Privileges")
	delete(noGroup, "group")
	res, err = testRegistry().Normalize(makeEvent(noGroup))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(scosOf(res, identity.TypeGroup)); n != 0 || hasRel(res.Relationships, RelMemberOfGroup) {
		t.Errorf("no group on the event: %d groups minted", n)
	}
}

// TestGroupManagementUsers (§4.9, adr/0359): from OCSF 1.9.0 the members
// are users[]. Every entry that names a user is a target with its own
// member-of-group edge, and user is read when no entry does. The other account
// classes have no users[] and read user alone.
func TestGroupManagementUsers(t *testing.T) {
	ids := identity.NewResolver(testNS)
	group := ids.Group("contoso.com", "Domain Admins")
	sid := func(s string) identity.STIXID {
		return ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: s})
	}
	members := []any{
		map[string]any{"name": "alice", "domain": "CONTOSO", "uid": "S-1-5-21-7-1101"},
		map[string]any{"name": "bob", "domain": "CONTOSO", "uid": "S-1-5-21-7-1102"},
	}
	e := versioned(3006, "1.9.0", map[string]any{
		"activity_id": 1, "users": members,
		"actor":  map[string]any{"user": map[string]any{"name": "admin", "domain": "CONTOSO", "uid": "S-1-5-21-7-500"}},
		"group":  map[string]any{"name": "Domain Admins", "domain": "contoso.com", "uid": "g-512"},
		"device": map[string]any{"hostname": "DC01"},
	})
	if bad := ocsfschema.ValidateFields(e); len(bad) > 0 {
		t.Fatalf("OCSF 1.9.0: %v", bad)
	}
	res, err := testRegistry().Normalize(makeEvent(e))
	if err != nil {
		t.Fatal(err)
	}
	alice, bob := sid("S-1-5-21-7-1101"), sid("S-1-5-21-7-1102")
	for _, id := range []identity.STIXID{alice, bob, sid("S-1-5-21-7-500")} {
		if !refsContain(res.ObservedData[0].ObjectRefs, id) {
			t.Errorf("object_refs lack %s", id)
		}
	}
	rels := relsOf(res, RelMemberOfGroup)
	if len(rels) != 2 || !slices.Contains(rels, [2]identity.STIXID{alice, group}) || !slices.Contains(rels, [2]identity.STIXID{bob, group}) {
		t.Errorf("member-of-group = %v; want one from each of %s and %s", rels, alice, bob)
	}

	// users[] wins over the deprecated user. An empty users[] reads user.
	both := accountEvent(3006, "Add User")
	both["users"] = members[1:]
	res, err = testRegistry().Normalize(makeEvent(both))
	if err != nil {
		t.Fatal(err)
	}
	if rels := relsOf(res, RelMemberOfGroup); len(rels) != 1 || rels[0] != [2]identity.STIXID{bob, group} {
		t.Errorf("users[] and user: member-of-group = %v; want %s alone", rels, bob)
	}
	empty := accountEvent(3006, "Add User")
	empty["users"] = []any{}
	res, err = testRegistry().Normalize(makeEvent(empty))
	if err != nil {
		t.Fatal(err)
	}
	if rels := relsOf(res, RelMemberOfGroup); len(rels) != 1 || rels[0] != [2]identity.STIXID{alice, group} {
		t.Errorf("empty users[]: member-of-group = %v; want %s from user", rels, alice)
	}

	// An entry that names no user is no member: user is read instead.
	nobody := accountEvent(3006, "Add User")
	nobody["users"] = []any{map[string]any{"uid": ""}}
	res, err = testRegistry().Normalize(makeEvent(nobody))
	if err != nil {
		t.Fatal(err)
	}
	if rels := relsOf(res, RelMemberOfGroup); len(rels) != 1 || rels[0] != [2]identity.STIXID{alice, group} {
		t.Errorf("users[] naming nobody: member-of-group = %v; want %s from user", rels, alice)
	}

	// Account Change has no users[]: a stray one is not read.
	change := accountEvent(3001, "Create")
	change["users"] = members[1:]
	res, err = testRegistry().Normalize(makeEvent(change))
	if err != nil {
		t.Fatal(err)
	}
	if refsContain(res.ObservedData[0].ObjectRefs, bob) || !refsContain(res.ObservedData[0].ObjectRefs, alice) {
		t.Errorf("account_change object_refs = %v; want user, not users[]", res.ObservedData[0].ObjectRefs)
	}
}

// emailEvent is an email_activity event at the OCSF ceiling: attachments in
// email.files[], links in email.urls[], blind copy in email.bcc[].
func emailEvent() map[string]any {
	return map[string]any{
		"class_uid": 4009,
		"time":      ms("2026-04-20T14:32:11Z"),
		"email": map[string]any{
			"from":        "attacker@evil.example",
			"to":          []any{"alice@corp.com", "bob@corp.com"},
			"cc":          []any{"carol@corp.com"},
			"bcc":         []any{"dave@corp.com"},
			"subject":     "Invoice",
			"message_uid": "<abc123@evil.example>",
			"uid":         "gw-0001",
			"files": []any{map[string]any{
				"name":   "invoice.pdf.exe",
				"hashes": []any{map[string]any{"algorithm": "SHA-256", "value": "aa415c4e8890cf0fec7826aec962ffbcc04534faefd2b3266c54f690d40d6e82"}},
			}},
			"urls": []any{map[string]any{"url_string": "https://evil.example/pay"}},
		},
	}
}

// TestEmailNormalizer (§4.10): message + addresses + attachment + url, with
// contains edges from the message.
func TestEmailNormalizer(t *testing.T) {
	res, err := testRegistry().Normalize(makeEvent(emailEvent()))
	if err != nil {
		t.Fatal(err)
	}
	ids := identity.NewResolver(testNS)
	strong := ids.EmailMessage("<abc123@evil.example>", "", "", time.Time{}, nil, "")
	bt := countTypes(res.SCOs)
	if bt[identity.TypeEmailAddr] != 5 { // from + 2 recipients + cc + bcc
		t.Errorf("email-addr SCOs = %d; want 5", bt[identity.TypeEmailAddr])
	}
	if bt[identity.TypeFile] != 1 || bt[identity.TypeURL] != 1 {
		t.Errorf("want 1 attachment file + 1 url; got %v", bt)
	}
	msg, ok := scoByID(res.SCOs, strong)
	if !ok || !refsContain(res.ObservedData[0].ObjectRefs, strong) {
		t.Fatalf("the message keyed on the Message-ID is not in object_refs: %v", bt)
	}
	for key, want := range map[string][]string{
		"to_refs":  {string(ids.EmailAddr("alice@corp.com")), string(ids.EmailAddr("bob@corp.com"))},
		"cc_refs":  {string(ids.EmailAddr("carol@corp.com"))},
		"bcc_refs": {string(ids.EmailAddr("dave@corp.com"))},
	} {
		if got, _ := msg.Properties[key].([]string); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v; want %v", key, msg.Properties[key], want)
		}
	}
	contains := relsOf(res, RelContains)
	if len(contains) != 2 || contains[0][0] != strong || contains[1][0] != strong { // attachment + url
		t.Errorf("contains relationships = %v; want 2 from the message", contains)
	}
}

// TestEmailBlindCopy (§4.10, adr/0356): blind copy is email.bcc[] from OCSF
// 1.9.0. Below that a source reports it in unmapped.bcc[]. email.bcc[] wins
// when both are present.
func TestEmailBlindCopy(t *testing.T) {
	ids := identity.NewResolver(testNS)
	for _, tc := range []struct {
		name            string
		email, unmapped []any
		want            []string
	}{
		{"email.bcc", []any{"dave@corp.com"}, nil, []string{"dave@corp.com"}},
		{"unmapped.bcc", nil, []any{"erin@corp.com"}, []string{"erin@corp.com"}},
		{"both", []any{"dave@corp.com"}, []any{"erin@corp.com"}, []string{"dave@corp.com"}},
		{"neither", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := emailEvent()
			mail := p["email"].(map[string]any)
			delete(mail, "bcc")
			if tc.email != nil {
				mail["bcc"] = tc.email
			}
			if tc.unmapped != nil {
				p["unmapped"] = map[string]any{"bcc": tc.unmapped}
			}
			res, err := testRegistry().Normalize(makeEvent(p))
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, a := range tc.want {
				want = append(want, string(ids.EmailAddr(a)))
			}
			msg := scosOf(res, identity.TypeEmailMessage)[0]
			if got, _ := msg.Properties["bcc_refs"].([]string); !reflect.DeepEqual(got, want) {
				t.Errorf("bcc_refs = %v; want %v", msg.Properties["bcc_refs"], want)
			}
		})
	}
}

// TestEmailIsMultipart (§4.10, adr/0357): STIX requires is_multipart, and
// OCSF has no field for it. It is true when the raw header's Content-Type
// names a multipart type, matched without case, and false otherwise.
func TestEmailIsMultipart(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		want         bool
	}{
		{"multipart", "From: a@b.example\r\nContent-Type: multipart/mixed; boundary=\"x\"\r\nSubject: hi", true},
		{"any case", "content-type: Multipart/Alternative", true},
		{"single part", "Content-Type: text/plain; charset=utf-8", false},
		{"a folded line is not a header", "X-Note: see\r\n Content-Type: multipart/mixed", false},
		// RFC 5322 §2.2.3: a folded header is unfolded before it is read.
		{"a folded Content-Type", "From: a@b.example\r\nContent-Type:\r\n multipart/related;\r\n\tboundary=\"x\"", true},
		{"an upper-case name, folded after the type", "CONTENT-TYPE: Multipart/Mixed;\r\n boundary=\"x\"", true},
		{"lower-case name, folded parameters", "content-type: multipart/signed;\n protocol=\"application/pgp-signature\"", true},
		{"no Content-Type", "From: a@b.example", false},
		{"no raw header", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := emailEvent()
			if tc.header != "" {
				p["email"].(map[string]any)["raw_header"] = tc.header
			}
			res, err := testRegistry().Normalize(makeEvent(p))
			if err != nil {
				t.Fatal(err)
			}
			msgs := scosOf(res, identity.TypeEmailMessage)
			if len(msgs) != 1 {
				t.Fatalf("messages = %+v; want one", msgs)
			}
			if msgs[0].Properties["is_multipart"] != tc.want {
				t.Errorf("is_multipart = %v; want %v", msgs[0].Properties["is_multipart"], tc.want)
			}
		})
	}
}

// TestEmailLeavesUnknownsOut (adr/0357): a property the record does not give
// is left out of the message, not written empty.
func TestEmailLeavesUnknownsOut(t *testing.T) {
	p := emailEvent()
	mail := p["email"].(map[string]any)
	for _, k := range []string{"message_uid", "subject", "to", "cc", "bcc", "from"} {
		delete(mail, k)
	}
	res, err := testRegistry().Normalize(makeEvent(p))
	if err != nil {
		t.Fatal(err)
	}
	msgs := scosOf(res, identity.TypeEmailMessage)
	if len(msgs) != 1 {
		t.Fatalf("messages = %+v", msgs)
	}
	for _, k := range []string{"message_id", "subject", "from_ref", "to_refs", "cc_refs", "bcc_refs"} {
		if v, ok := msgs[0].Properties[k]; ok {
			t.Errorf("%s = %#v; want it left out", k, v)
		}
	}
}

// TestEmailMessageIdentity (§4.10, adr/0356, adr/0359): the Message-ID
// mints the message, else (from, subject, date, recipients, thread_uid)
// does. email.uid is a thread id: it is kept as thread_uid and mints no weak
// message and no aliases edge.
func TestEmailMessageIdentity(t *testing.T) {
	ids := identity.NewResolver(testNS)
	when := makeEvent(emailEvent()).Time
	recipients := []string{"alice@corp.com", "bob@corp.com", "carol@corp.com", "dave@corp.com"}
	strong := ids.EmailMessage("<abc123@evil.example>", "", "", time.Time{}, nil, "")
	byFacts := ids.EmailMessage("", "attacker@evil.example", "Invoice", when, recipients, "")
	byThread := ids.EmailMessage("", "attacker@evil.example", "Invoice", when, recipients, "gw-0001")
	normalize := func(drop ...string) NormalizationResult {
		t.Helper()
		p := emailEvent()
		for _, k := range drop {
			delete(p["email"].(map[string]any), k)
		}
		res, err := testRegistry().Normalize(makeEvent(p))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, tc := range []struct {
		name   string
		drop   []string
		want   identity.STIXID
		thread any
	}{
		{"both ids", nil, strong, "gw-0001"},
		{"the Message-ID alone", []string{"uid"}, strong, nil},
		{"the thread id alone", []string{"message_uid"}, byThread, "gw-0001"},
		{"neither id", []string{"message_uid", "uid"}, byFacts, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := normalize(tc.drop...)
			msgs := scosOf(res, identity.TypeEmailMessage)
			if len(msgs) != 1 || msgs[0].ID != tc.want || msgs[0].Properties["weak"] != nil {
				t.Fatalf("messages = %+v; want %s alone, not weak", msgs, tc.want)
			}
			if got := msgs[0].Properties["thread_uid"]; got != tc.thread {
				t.Errorf("thread_uid = %v; want %v", got, tc.thread)
			}
			if !refsContain(res.ObservedData[0].ObjectRefs, tc.want) {
				t.Error("the message is not in object_refs")
			}
			if hasRel(res.Relationships, RelAliases) {
				t.Errorf("aliases = %v; want none", relsOf(res, RelAliases))
			}
			if c := relsOf(res, RelContains); len(c) != 2 || c[0][0] != tc.want || c[1][0] != tc.want {
				t.Errorf("contains = %v; want 2 from the message", c)
			}
		})
	}
}

// TestEmailMessageWithoutMessageID (§4.10, adr/0359): with no Message-ID a
// message is keyed on its sender, subject, second, recipients and thread
// id. Separate copies of one run, or messages of two threads, stay apart.
// Two records of one message with no thread id join.
func TestEmailMessageWithoutMessageID(t *testing.T) {
	reg := testRegistry()
	message := func(mail map[string]any) identity.STIXID {
		t.Helper()
		e := versioned(4009, "1.9.0", map[string]any{"email": mail})
		if bad := ocsfschema.ValidateFields(e); len(bad) > 0 {
			t.Fatalf("OCSF 1.9.0: %v", bad)
		}
		res, err := reg.Normalize(makeEvent(e))
		if err != nil {
			t.Fatal(err)
		}
		msgs := scosOf(res, identity.TypeEmailMessage)
		if len(msgs) != 1 {
			t.Fatalf("messages = %+v; want one", msgs)
		}
		return msgs[0].ID
	}
	mail := func(fields ...any) map[string]any {
		m := map[string]any{"from": "billing@evil.example", "subject": "Overdue invoice"}
		for i := 0; i < len(fields); i += 2 {
			if fields[i+1] == nil {
				delete(m, fields[i].(string))
				continue
			}
			m[fields[i].(string)] = fields[i+1]
		}
		return m
	}
	for _, tc := range []struct {
		name string
		a, b map[string]any
		same bool
	}{
		{"different recipients", mail("to", []any{"ap@corp.example"}), mail("to", []any{"cfo@corp.example"}), false},
		{"different blind copy", mail("to", []any{"ap@corp.example"}, "bcc", []any{"x@corp.example"}),
			mail("to", []any{"ap@corp.example"}, "bcc", []any{"y@corp.example"}), false},
		{"different envelope recipients", mail("to", []any{"all-staff@corp.example"}, "smtp_to", []any{"ap@corp.example"}),
			mail("to", []any{"all-staff@corp.example"}, "smtp_to", []any{"cfo@corp.example"}), false},
		{"different Delivered-To", mail("to", []any{"all-staff@corp.example"}, "delivered_to_list", []any{"ap@corp.example"}),
			mail("to", []any{"all-staff@corp.example"}, "delivered_to_list", []any{"cfo@corp.example"}), false},
		{"different deprecated Delivered-To", mail("to", []any{"all-staff@corp.example"}, "delivered_to", "ap@corp.example"),
			mail("to", []any{"all-staff@corp.example"}, "delivered_to", "cfo@corp.example"), false},
		{"different thread ids", mail("to", []any{"ap@corp.example"}, "uid", "t-1"), mail("to", []any{"ap@corp.example"}, "uid", "t-2"), false},
		{"the same message, no thread id", mail("to", []any{"ap@corp.example", "cfo@corp.example"}),
			mail("to", []any{"CFO@corp.example"}, "cc", []any{"ap@corp.example"}), true},
		{"no sender or subject, different recipients", mail("from", nil, "subject", nil, "to", []any{"ap@corp.example"}),
			mail("from", nil, "subject", nil, "to", []any{"cfo@corp.example"}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if a, b := message(tc.a), message(tc.b); (a == b) != tc.same {
				t.Errorf("ids %s and %s; want same = %v", a, b, tc.same)
			}
		})
	}
}

// TestEmailThreadAndEnvelopeStored (adr/0357, adr/0359): a message with no
// Message-ID, a thread id and envelope recipients stores the thread as
// thread_uid and each envelope address as an email-addr in
// envelope_to_refs, listed by the observation. The message validates against
// the STIX schemas, and its id re-mints from what it stores.
func TestEmailThreadAndEnvelopeStored(t *testing.T) {
	reg := testRegistry()
	e := versioned(4009, "1.9.0", map[string]any{"email": map[string]any{
		"from": "billing@evil.example", "subject": "Overdue invoice", "uid": "thread-7",
		"to":                []any{"all-staff@corp.example"},
		"smtp_to":           []any{"ap@corp.example", " ", "not an address"},
		"delivered_to_list": []any{"ap@corp.example", "cfo@corp.example"},
	}})
	if bad := ocsfschema.ValidateFields(e); len(bad) > 0 {
		t.Fatalf("OCSF 1.9.0: %v", bad)
	}
	res, err := reg.Normalize(makeEvent(e))
	if err != nil {
		t.Fatal(err)
	}
	msgs := scosOf(res, identity.TypeEmailMessage)
	if len(msgs) != 1 {
		t.Fatalf("messages = %+v; want one", msgs)
	}
	ids := identity.NewResolver(testNS)
	want := []string{string(ids.EmailAddr("ap@corp.example")), string(ids.EmailAddr("cfo@corp.example"))}
	if got, _ := msgs[0].Properties["envelope_to_refs"].([]string); !slices.Equal(got, want) {
		t.Errorf("envelope_to_refs = %v; want %v, each address once", msgs[0].Properties["envelope_to_refs"], want)
	}
	if msgs[0].Properties["thread_uid"] != "thread-7" {
		t.Errorf("thread_uid = %v; want thread-7", msgs[0].Properties["thread_uid"])
	}
	for _, ref := range want {
		if !refsContain(res.ObservedData[0].ObjectRefs, identity.STIXID(ref)) {
			t.Errorf("envelope address %s is not in object_refs", ref)
		}
	}
	var report stixschema.Report
	for _, o := range stixObjects(res) {
		raw, err := stix.Marshal(o)
		if err != nil {
			t.Fatal(err)
		}
		if o.Type == identity.TypeEmailMessage {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			ext := stix.ExtensionFrom(m, stix.ExtEmailMessage)
			if ext["thread_uid"] != "thread-7" || len(ext["envelope_to_refs"].([]any)) != 2 {
				t.Errorf("%s extension = %v; want thread_uid and envelope_to_refs", stix.ExtEmailMessage, ext)
			}
		}
		report.Add(o.ID, raw)
	}
	if report.Failed() {
		t.Errorf("not valid STIX 2.1:\n%s", report.String())
	}
	if p := remintProblems(reg.Identity(), res); len(p) > 0 {
		t.Errorf("R1: %v", p)
	}
}

// TestEmailSpamCopiesByEnvelope (adr/0359): two copies of one spam run,
// alike in their headers and sent to different envelope recipients, are two
// messages.
func TestEmailSpamCopiesByEnvelope(t *testing.T) {
	reg := testRegistry()
	copyTo := func(rcpt string) identity.STIXID {
		t.Helper()
		res, err := reg.Normalize(makeEvent(versioned(4009, "1.9.0", map[string]any{"email": map[string]any{
			"from": "promo@spam.example", "subject": "You won", "to": []any{"undisclosed@spam.example"},
			"smtp_to": []any{rcpt},
		}})))
		if err != nil {
			t.Fatal(err)
		}
		msgs := scosOf(res, identity.TypeEmailMessage)
		if len(msgs) != 1 {
			t.Fatalf("messages = %+v; want one", msgs)
		}
		return msgs[0].ID
	}
	if a, b := copyTo("ap@corp.example"), copyTo("cfo@corp.example"); a == b {
		t.Errorf("both copies are %s; want two ids", a)
	}
}

// TestEmailClickCarriesThread (adr/0359): a click keeps its email_uid on the
// observation as thread_uid and creates no email-message.
func TestEmailClickCarriesThread(t *testing.T) {
	res, err := testRegistry().Normalize(makeEvent(clickEvent()))
	if err != nil {
		t.Fatal(err)
	}
	if msgs := scosOf(res, identity.TypeEmailMessage); len(msgs) != 0 {
		t.Errorf("messages = %+v; want none", msgs)
	}
	if got := res.ObservedData[0].Extensions["thread_uid"]; got != "gw-0001" {
		t.Errorf("thread_uid = %v; want gw-0001", got)
	}
	found := false
	for _, o := range stixObjects(res) {
		if o.Type != "observed-data" {
			continue
		}
		found = true
		raw, err := stix.Marshal(o)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if ext := stix.ExtensionFrom(m, stix.ExtObservation); ext["thread_uid"] != "gw-0001" {
			t.Errorf("%s extension = %v; want thread_uid gw-0001", stix.ExtObservation, ext)
		}
	}
	if !found {
		t.Error("no observed-data was written")
	}
}

// TestEmailThreadDoesNotJoinMessages (adr/0359): two messages of one thread
// share email.uid. From one source, with different Message-IDs, they stay two
// messages with no aliases edge. With no Message-ID they do not share an id
// either.
func TestEmailThreadDoesNotJoinMessages(t *testing.T) {
	reg := testRegistry()
	message := func(messageUID, subject string, minute int) NormalizationResult {
		t.Helper()
		mail := map[string]any{"from": "attacker@evil.example", "to": []any{"alice@corp.example"}, "subject": subject, "uid": "thread-7"}
		if messageUID != "" {
			mail["message_uid"] = messageUID
		}
		res, err := reg.Normalize(makeEvent(versioned(4009, "1.9.0", map[string]any{
			"time": ms(fmt.Sprintf("2026-04-20T14:%02d:00Z", minute)), "email": mail,
		})))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, tc := range []struct {
		name string
		a, b NormalizationResult
	}{
		{"different Message-IDs", message("<one@evil.example>", "Invoice", 30), message("<two@evil.example>", "Re: Invoice", 31)},
		{"no Message-ID", message("", "Invoice", 30), message("", "Re: Invoice", 31)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []identity.STIXID
			for _, res := range []NormalizationResult{tc.a, tc.b} {
				msgs := scosOf(res, identity.TypeEmailMessage)
				if len(msgs) != 1 {
					t.Fatalf("messages = %+v; want one per event", msgs)
				}
				if msgs[0].Properties["thread_uid"] != "thread-7" {
					t.Errorf("thread_uid = %v; want thread-7", msgs[0].Properties["thread_uid"])
				}
				if hasRel(res.Relationships, RelAliases) {
					t.Errorf("aliases = %v; want none", relsOf(res, RelAliases))
				}
				got = append(got, msgs[0].ID)
			}
			if got[0] == got[1] {
				t.Errorf("two messages of one thread share the id %s", got[0])
			}
		})
	}
}

// clickEvent is an email_url_activity event naming its email by email_uid.
func clickEvent() map[string]any {
	return map[string]any{
		"class_uid": 4012,
		"time":      ms("2026-04-20T14:40:00Z"),
		"url":       map[string]any{"url_string": "https://evil.example/pay"},
		"email_uid": "gw-0001",
		"actor":     map[string]any{"user": map[string]any{"name": "alice", "domain": "CORP", "uid": "S-1-5-21-7-1101"}},
		"device":    map[string]any{"hostname": "WS1"},
	}
}

// TestEmailURLNormalizer (§4.11, adr/0359): clicked url + clicker + host,
// with a clicked edge. The click's email_uid names a thread, so it mints no
// email-message and is kept in the extension fields as thread_uid.
func TestEmailURLNormalizer(t *testing.T) {
	res, err := testRegistry().Normalize(makeEvent(clickEvent()))
	if err != nil {
		t.Fatal(err)
	}
	bt := countTypes(res.SCOs)
	if bt[identity.TypeURL] != 1 || bt[identity.TypeUserAccount] != 2 || bt[identity.TypeEmailMessage] != 0 {
		t.Errorf("want 1 url + 1 user and its login account (adr/0324) and no email-message; got %v", bt)
	}
	if !hasRel(res.Relationships, RelClicked) || hasRel(res.Relationships, RelContains) {
		t.Errorf("relationships = %v; want clicked and no contains", res.Relationships)
	}
	if got := res.ObservedData[0].Extensions["thread_uid"]; got != "gw-0001" {
		t.Errorf("thread_uid = %v; want gw-0001", got)
	}

	// A click that names no email keeps no thread id.
	bare := clickEvent()
	bare["email_uid"] = "  "
	res, err = testRegistry().Normalize(makeEvent(bare))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.ObservedData[0].Extensions["thread_uid"]; ok || !hasRel(res.Relationships, RelClicked) {
		t.Errorf("click with a blank email_uid: extensions %v, relationships %v", res.ObservedData[0].Extensions, res.Relationships)
	}

	// 4011 is email_file_activity: a click sent under it is not read as one.
	old := clickEvent()
	old["class_uid"] = 4011
	res, err = testRegistry().Normalize(makeEvent(old))
	if err != nil {
		t.Fatal(err)
	}
	if countTypes(res.SCOs)[identity.TypeURL] != 0 {
		t.Error("class 4011 read a clicked URL")
	}
}

// TestEmailClickAndAttachmentMintNoMessage (§4.11, §4.11a, adr/0359): a
// click and an attachment event that name the same email_uid mint no
// email-message. Each keeps the thread id on its observation, and nothing
// joins them to each other or to the delivered message.
func TestEmailClickAndAttachmentMintNoMessage(t *testing.T) {
	reg := testRegistry()
	click := clickEvent()
	file := map[string]any{
		"class_uid": 4011, "time": ms("2026-04-20T14:41:00Z"), "activity_id": 2, "email_uid": " gw-0001 ",
		"file": map[string]any{"name": "invoice.pdf.exe"}, "device": map[string]any{"hostname": "WS1"},
	}
	for name, e := range map[string]map[string]any{"click": click, "attachment": file} {
		res, err := reg.Normalize(makeEvent(e))
		if err != nil {
			t.Fatal(err)
		}
		if n := len(scosOf(res, identity.TypeEmailMessage)); n != 0 {
			t.Errorf("%s: %d email-messages; want none", name, n)
		}
		// The only aliases are the actor's own login to its SID account.
		for _, r := range res.Relationships {
			if r.Type == RelContains || strings.HasPrefix(string(r.SourceRef), identity.TypeEmailMessage) || strings.HasPrefix(string(r.TargetRef), identity.TypeEmailMessage) {
				t.Errorf("%s: relationship %s %s → %s; want none from or to a message", name, r.Type, r.SourceRef, r.TargetRef)
			}
		}
		if got := res.ObservedData[0].Extensions["thread_uid"]; got != "gw-0001" {
			t.Errorf("%s: thread_uid = %v; want gw-0001, trimmed", name, got)
		}
	}
	mail, err := reg.Normalize(makeEvent(emailEvent()))
	if err != nil {
		t.Fatal(err)
	}
	if hasRel(mail.Relationships, RelAliases) {
		t.Errorf("the delivery aliases %v; want nothing", relsOf(mail, RelAliases))
	}
}

// TestEmailFileNormalizer (§4.11a, adr/0359): an attachment event gives
// the file, the actor and the host, with the activity and the thread id in
// the extensions. It mints no email-message.
func TestEmailFileNormalizer(t *testing.T) {
	payload := map[string]any{
		"class_uid":   4011,
		"time":        ms("2026-04-20T14:32:11Z"),
		"activity_id": 2,
		"email_uid":   "gw-0001",
		"file": map[string]any{
			"name":   "invoice.pdf.exe",
			"hashes": []any{map[string]any{"algorithm": "SHA-256", "value": "aa415c4e8890cf0fec7826aec962ffbcc04534faefd2b3266c54f690d40d6e82"}},
		},
		"actor":  map[string]any{"user": map[string]any{"name": "alice", "domain": "CORP"}},
		"device": map[string]any{"hostname": "WS1"},
	}
	res, err := testRegistry().Normalize(makeEvent(payload))
	if err != nil {
		t.Fatal(err)
	}
	ids := identity.NewResolver(testNS)
	file := ids.File(identity.FileIdentity{Hashes: map[string]string{"SHA-256": "aa415c4e8890cf0fec7826aec962ffbcc04534faefd2b3266c54f690d40d6e82"}, Name: "invoice.pdf.exe"})
	bt := countTypes(res.SCOs)
	if bt[identity.TypeFile] != 1 || bt[identity.TypeEmailMessage] != 0 || bt[identity.TypeUserAccount] != 1 || bt[identity.TypeHost] != 1 {
		t.Errorf("want 1 file + 1 account + 1 host and no message; got %v", bt)
	}
	if hasRel(res.Relationships, RelContains) {
		t.Errorf("relationships = %v; want no contains", res.Relationships)
	}
	od := res.ObservedData[0]
	if od.Extensions["activity_id"] != 2 || od.Extensions["thread_uid"] != "gw-0001" || od.Provenance.Normalizer != "email_file_activity" {
		t.Errorf("extensions %v, normalizer %q", od.Extensions, od.Provenance.Normalizer)
	}

	// The attachment event and the ceiling's email.files[] name one file.
	mail, err := testRegistry().Normalize(makeEvent(emailEvent()))
	if err != nil {
		t.Fatal(err)
	}
	if got := scoOfType(mail.SCOs, identity.TypeFile); got != file {
		t.Errorf("email.files[] file = %s; the attachment event's = %s", got, file)
	}

	// With no email_uid there is no thread id.
	delete(payload, "email_uid")
	res, err = testRegistry().Normalize(makeEvent(payload))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.ObservedData[0].Extensions["thread_uid"]; ok {
		t.Errorf("no email_uid: extensions %v", res.ObservedData[0].Extensions)
	}
}
