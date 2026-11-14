package ocsf

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// Every table in enums.go agrees with the OCSF schema, in every release
// checked in (adr/0356).
func TestEnumsMatchSchema(t *testing.T) {
	tests := []struct {
		name  string
		class int
		path  string
		table map[int]string
	}{
		{"HashAlgorithms", 1007, "process.file.hashes[].algorithm_id", HashAlgorithms},
		{"Severities", 2004, "severity_id", Severities},
		{"Severities on an activity", 1007, "severity_id", Severities},
		{"Confidences", 2004, "confidence_id", Confidences},
		{"Statuses", 6003, "status_id", Statuses},
		{"Statuses on a logon", 3002, "status_id", Statuses},
		{"IncidentStatuses", 2005, "status_id", IncidentStatuses},
		{"RegistryValueTypes", 201002, "reg_value.type_id", RegistryValueTypes},
	}
	for _, tt := range tests {
		for _, problem := range ocsfschema.CheckCaptions(tt.class, tt.path, tt.table) {
			t.Errorf("%s: %s", tt.name, problem)
		}
		// A source's own "Unknown" is OCSF's 0, not Other; a word OCSF
		// does not have is Other.
		if got := CaptionID(tt.table, "UNKNOWN"); got != Unknown {
			t.Errorf("%s: CaptionID of UNKNOWN = %d; want 0", tt.name, got)
		}
		if got := CaptionID(tt.table, "no such caption"); got != Other {
			t.Errorf("%s: CaptionID of a word OCSF does not have = %d; want 99", tt.name, got)
		}
	}
}

// HashAlgorithms names every algorithm the newest release has, so a new
// release's additions fail here until the table has them.
func TestHashAlgorithmsAreComplete(t *testing.T) {
	schema, err := ocsfschema.Load(ocsfschema.Ceiling)
	if err != nil {
		t.Fatal(err)
	}
	enum, ok := schema.Enum(1007, "process.file.hashes[].algorithm_id")
	if !ok {
		t.Fatal("fingerprint.algorithm_id is not an enum")
	}
	for id, caption := range enum {
		if id == 0 || id == 99 {
			continue
		}
		if HashAlgorithms[id] != caption {
			t.Errorf("algorithm_id %d (%s) is missing from HashAlgorithms", id, caption)
		}
	}
}

func TestHashes(t *testing.T) {
	read := func(m map[string]any, k string) any { return m[k] }
	tests := []struct {
		name string
		in   any
		want map[string]string
	}{
		{"caption and id", []any{map[string]any{"algorithm": "SHA-256", "algorithm_id": float64(3), "value": "ab"}}, map[string]string{"SHA-256": "ab"}},
		{"id only", []any{
			map[string]any{"algorithm_id": float64(3), "value": "ab"},
			map[string]any{"algorithm_id": float64(1), "value": "cd"},
			map[string]any{"algorithm_id": 2, "value": "ef"},
		}, map[string]string{"SHA-256": "ab", "MD5": "cd", "SHA-1": "ef"}},
		{"caption only", []any{map[string]any{"algorithm": "SHA-1", "value": "ef"}}, map[string]string{"SHA-1": "ef"}},
		{"the caption wins, as OCSF says for Other", []any{map[string]any{"algorithm": "BLAKE3", "algorithm_id": float64(99), "value": "aa"}}, map[string]string{"BLAKE3": "aa"}},
		{"unknown, other and undefined ids name nothing", []any{
			map[string]any{"algorithm_id": float64(0), "value": "aa"},
			map[string]any{"algorithm_id": float64(99), "value": "bb"},
			map[string]any{"algorithm_id": float64(4242), "value": "cc"},
			map[string]any{"value": "dd"},
		}, nil},
		{"no value", []any{map[string]any{"algorithm_id": float64(3)}}, nil},
		{"not a list", map[string]any{"SHA-256": "ab"}, nil},
		{"an entry that is not an object", []any{"ab"}, nil},
	}
	for _, tt := range tests {
		if got := Hashes(tt.in, read); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: Hashes = %v; want %v", tt.name, got, tt.want)
		}
	}
}

// Hashes reads through the caller's reader, so the caller's schema check sees
// each field.
func TestHashesReadsThroughTheCaller(t *testing.T) {
	seen := map[string]bool{}
	Hashes([]any{map[string]any{"algorithm_id": float64(3), "value": "ab"}}, func(m map[string]any, k string) any {
		seen[k] = true
		return m[k]
	})
	for _, k := range []string{"value", "algorithm", "algorithm_id"} {
		if !seen[k] {
			t.Errorf("%s was not read through the caller's reader", k)
		}
	}
}

// numberTables says, for each package that reads or writes OCSF, which of its
// number tables (ocsfschema.NumberTables) a test checks against the schema,
// and which are not OCSF enums. A table in neither list fails
// TestEveryNumberTableIsChecked, so a new enum table cannot be added without
// a schema check.
var numberTables = map[string]struct{ checked, exempt []string }{
	"internal/ocsf": {
		// TestEnumsMatchSchema, and TestAccountTypeKindsCoverTheEnum for
		// AccountTypeKinds.
		checked: []string{"HashAlgorithms", "Severities", "Confidences", "Statuses", "IncidentStatuses", "RegistryValueTypes", "AccountTypeKinds"},
	},
	"capability": {
		// capability.TestEnumTablesMatchSchema
		checked: []string{"userTypes", "accountTypes", "accountTypeIDs", "apiActivities", "inventoryActivities", "osTypes", "confidenceLevels"},
	},
	"capability/ocsfview": {
		checked: []string{"networkActivities"}, // ocsfview.TestEnumTablesMatchSchema
		// Keyed by class number, which the registration check covers.
		exempt: []string{"classSlots", "defaultKeys"},
	},
	"cmd/reckon-adapter-aws-security-lake": {
		checked: []string{"classNames"}, // TestClassNamesAreOCSF
	},
}

func TestEveryNumberTableIsChecked(t *testing.T) {
	root := filepath.Join("..", "..")
	dirs := []string{"internal/ocsf", "capability", "capability/ocsfview"}
	adapters, err := filepath.Glob(filepath.Join(root, "cmd", "reckon-adapter-*"))
	if err != nil || len(adapters) == 0 {
		t.Fatalf("no adapters found under cmd/: %v", err)
	}
	for _, a := range adapters {
		dirs = append(dirs, "cmd/"+filepath.Base(a))
	}
	for dir := range numberTables {
		if !slices.Contains(dirs, dir) {
			t.Errorf("numberTables lists %s, which is not a package this test scans", dir)
		}
	}
	for _, dir := range dirs {
		lists := numberTables[dir]
		uncovered, stale, err := ocsfschema.UncoveredTables(filepath.Join(root, filepath.FromSlash(dir)), lists.checked, lists.exempt)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range uncovered {
			t.Errorf("%s: the table %s is not checked against the OCSF schema. Add a check and list it in numberTables, or list it as exempt with the reason.", dir, name)
		}
		for _, name := range stale {
			t.Errorf("%s: numberTables lists %s, which is no longer a table there", dir, name)
		}
	}
}

// TestAccountTypeKindsCoverTheEnum (adr/0359): AccountTypeKinds classifies
// every value of the ceiling's account.type_id enum, and no value the enum
// lacks. A value left out would read as no signal without anyone deciding
// so. A principal type names a provider with a principal-form or session
// rule, or one of the three the rule knows, and is never also a host type.
func TestAccountTypeKindsCoverTheEnum(t *testing.T) {
	schema, err := ocsfschema.Load(ocsfschema.Ceiling)
	if err != nil {
		t.Fatal(err)
	}
	enum, ok := schema.Enum(2004, "evidences[].actor.user.account.type_id")
	if !ok {
		t.Fatal("OCSF has no account.type_id enum on a detection's evidence actor")
	}
	for _, problem := range ocsfschema.CheckCaptions(2004, "evidences[].actor.user.account.type_id", accountTypeCaptions()) {
		t.Errorf("AccountTypeKinds: %s", problem)
	}
	for id, caption := range enum {
		if _, ok := AccountTypeKinds[id]; !ok {
			t.Errorf("account.type_id %d (%s) is not classified", id, caption)
		}
	}
	for id, kind := range AccountTypeKinds {
		if _, ok := enum[id]; !ok {
			t.Errorf("AccountTypeKinds classifies %d, which OCSF %s does not define", id, ocsfschema.Ceiling)
		}
		if kind.Host && kind.Provider != "" {
			t.Errorf("account.type_id %d is both a host type and a %s principal", id, kind.Provider)
		}
		if p := kind.Provider; p != "" && p != "aws" && p != "azure" && p != "gcp" {
			t.Errorf("account.type_id %d names the unknown provider %q", id, p)
		}
	}
}

// AccountTypeID reads the id, else the caption by OCSF's enum, letter case
// aside (adr/0359).
func TestAccountTypeID(t *testing.T) {
	for _, tc := range []struct {
		id      any
		caption string
		want    int
	}{
		{float64(9), "Windows Account", 9},
		{nil, "linux account", 9},
		{nil, " AWS IAM Role ", 4},
		{nil, "", Unknown},
		{nil, "Unknown", Unknown},
		{nil, "a word OCSF lacks", Other},
	} {
		if got := AccountTypeID(tc.id, tc.caption); got != tc.want {
			t.Errorf("AccountTypeID(%v, %q) = %d; want %d", tc.id, tc.caption, got, tc.want)
		}
	}
}
