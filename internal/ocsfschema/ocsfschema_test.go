package ocsfschema

import (
	"strings"
	"testing"
)

func mustLoad(t *testing.T, version string) *Schema {
	t.Helper()
	s, err := Load(version)
	if err != nil {
		t.Fatalf("Load(%s): %v", version, err)
	}
	return s
}

// Every release from the floor to the ceiling is checked in, and each file
// declares the version its directory names (Load checks that).
func TestVersions(t *testing.T) {
	got, err := Versions()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.1.0", "1.2.0", "1.3.0", "1.4.0", "1.5.0", "1.6.0", "1.7.0", "1.8.0", "1.9.0"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Versions() = %v, want %v", got, want)
	}
	if got[0] != Floor || got[len(got)-1] != Ceiling {
		t.Errorf("range is %s to %s, want %s to %s", got[0], got[len(got)-1], Floor, Ceiling)
	}
	for _, v := range got {
		if s := mustLoad(t, v); s.Version() != v {
			t.Errorf("Load(%s).Version() = %s", v, s.Version())
		}
	}
}

func TestLoadUnknownVersion(t *testing.T) {
	if _, err := Load("0.0.1"); err == nil {
		t.Fatal("Load of a release that is not checked in returned no error")
	}
}

func TestLoadIsCached(t *testing.T) {
	if a, b := mustLoad(t, Floor), mustLoad(t, Floor); a != b {
		t.Error("Load read the same release twice")
	}
}

func TestClass(t *testing.T) {
	tests := []struct {
		version string
		uid     int
		want    Class
		absent  bool
	}{
		{version: "1.1.0", uid: 1003, want: Class{UID: 1003, Key: "kernel_activity", Name: "kernel_activity", Caption: "Kernel Activity"}},
		{version: "1.1.0", uid: 201002, want: Class{UID: 201002, Key: "win/registry_value_activity", Name: "registry_value_activity", Caption: "Registry Value Activity", Extension: "win"}},
		{version: "1.9.0", uid: 201001, want: Class{UID: 201001, Key: "win/registry_key_activity", Name: "registry_key_activity", Caption: "Registry Key Activity", Extension: "win"}},
		{version: "1.9.0", uid: 3001, want: Class{UID: 3001, Key: "account_change", Name: "account_change", Caption: "Account Change"}},
		{version: "1.9.0", uid: 4012, want: Class{UID: 4012, Key: "email_url_activity", Name: "email_url_activity", Caption: "Email URL Activity"}},
		{version: "1.9.0", uid: 0, want: Class{UID: 0, Key: "base_event", Name: "base_event", Caption: "Base Event"}},
		// Cloud Resources Inventory arrives in 1.4.0.
		{version: "1.1.0", uid: 5023, absent: true},
		{version: "1.3.0", uid: 5023, absent: true},
		{version: "1.4.0", uid: 5023, want: Class{UID: 5023, Key: "cloud_resources_inventory_info", Name: "cloud_resources_inventory_info", Caption: "Cloud Resources Inventory Info"}},
		// Script Activity is not in 1.1.0.
		{version: "1.1.0", uid: 1009, absent: true},
		{version: "1.9.0", uid: 999999, absent: true},
	}
	for _, tt := range tests {
		s := mustLoad(t, tt.version)
		got, ok := s.Class(tt.uid)
		if ok == tt.absent || s.HasClass(tt.uid) == tt.absent {
			t.Errorf("%s class %d: present = %v, want %v", tt.version, tt.uid, ok, !tt.absent)
			continue
		}
		if !tt.absent && got != tt.want {
			t.Errorf("%s class %d = %+v, want %+v", tt.version, tt.uid, got, tt.want)
		}
	}
}

func TestClassUIDs(t *testing.T) {
	uids := mustLoad(t, Floor).ClassUIDs()
	if len(uids) != 50 { // the base event (uid 0) included
		t.Errorf("1.1.0 has %d class uids, want 50", len(uids))
	}
	for i := 1; i < len(uids); i++ {
		if uids[i-1] >= uids[i] {
			t.Fatalf("ClassUIDs not sorted: %v", uids)
		}
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		version string
		uid     int
		path    string
		want    bool
	}{
		// The registry classes: a value event names its key in reg_value.path,
		// and no registry class has a hive.
		{"1.1.0", 201002, "reg_value.path", true},
		{"1.1.0", 201002, "reg_value.name", true},
		{"1.1.0", 201001, "reg_key.path", true},
		{"1.1.0", 201001, "reg_key.hive", false},
		{"1.1.0", 1003, "reg_key.hive", false},
		{"1.9.0", 1003, "reg_key.hive", false},
		{"1.9.0", 1003, "reg_key", false},

		// Fields that arrive inside the range.
		{"1.1.0", 6003, "resources[].zone", false},
		{"1.7.0", 6003, "resources[].zone", true},
		{"1.8.0", 4009, "email.bcc[]", false},
		{"1.9.0", 4009, "email.bcc[]", true},
		{"1.9.0", 4009, "email.bcc", true},
		{"1.3.0", 4009, "email.files[].name", false},
		{"1.4.0", 4009, "email.files[].name", true},

		// Plain paths, through objects and extension objects.
		{"1.1.0", 1007, "process.parent_process.pid", true},
		{"1.1.0", 1007, "device.hostname", true},
		{"1.1.0", 1007, "metadata.version", true},
		{"1.1.0", 1007, "class_uid", true},
		{"1.1.0", 1007, "process.file.hashes[].value", true},
		{"1.1.0", 1007, "process.nope", false},
		{"1.1.0", 1007, "nope", false},

		// A list is written with [] before a further segment, and only a list
		// takes [].
		{"1.7.0", 6003, "resources.zone", false},
		{"1.7.0", 6003, "resources", true},
		{"1.7.0", 6003, "resources[]", true},
		{"1.1.0", 1007, "device[].hostname", false},
		{"1.1.0", 1007, "device.hostname[]", false},

		// A scalar has no fields.
		{"1.1.0", 1007, "device.hostname.short", false},

		// unmapped is the generic object, and resources[].data is json_t: both
		// accept anything below.
		{"1.1.0", 1007, "unmapped", true},
		{"1.1.0", 1007, "unmapped.anything.at[].all", true},
		{"1.7.0", 5023, "resources[].data.Bucket.Policy", true},

		// Malformed paths and unknown classes.
		{"1.1.0", 1007, "", false},
		{"1.1.0", 1007, "device..hostname", false},
		{"1.1.0", 1007, "[]", false},
		{"1.1.0", 5023, "resources", false},
	}
	for _, tt := range tests {
		s := mustLoad(t, tt.version)
		err := s.Resolve(tt.uid, tt.path)
		if (err == nil) != tt.want || s.HasPath(tt.uid, tt.path) != tt.want {
			t.Errorf("%s class %d %q: resolves = %v (%v), want %v", tt.version, tt.uid, tt.path, err == nil, err, tt.want)
		}
	}
}

// The error names the first segment that fails, so a failing guard test says
// where the path leaves the schema.
func TestResolveErrorNamesTheSegment(t *testing.T) {
	err := mustLoad(t, "1.9.0").Resolve(1006, "job.file.nope.deeper")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"1.9.0", "1006", "scheduled_job_activity", `"job.file.nope"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestVersionLess(t *testing.T) {
	if !versionLess("1.9.0", "1.10.0") || versionLess("1.10.0", "1.9.0") || versionLess("1.9.0", "1.9.0") {
		t.Error("versionLess does not order 1.9.0 before 1.10.0")
	}
}
