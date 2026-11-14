package ocsfschema

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnum(t *testing.T) {
	s := mustLoad(t, Floor)
	got, ok := s.Enum(2004, "confidence_id")
	want := map[int]string{0: "Unknown", 1: "Low", 2: "Medium", 3: "High", 99: "Other"}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("Enum(2004, confidence_id) = %v, %v; want %v", got, ok, want)
	}
	if got, ok := s.Enum(1007, "process.file.hashes[].algorithm_id"); !ok || got[3] != "SHA-256" {
		t.Errorf("Enum through a list = %v, %v", got, ok)
	}
	for _, path := range []string{"activity_name", "no_such_field", "http_request.http_method"} {
		if got, ok := s.Enum(4002, path); ok {
			t.Errorf("Enum(4002, %s) = %v; want none: it is not an enum of whole numbers", path, got)
		}
	}
	if _, ok := s.Enum(424242, "activity_id"); ok {
		t.Error("Enum of an unknown class reported an enum")
	}
}

func TestCheckCaptions(t *testing.T) {
	if problems := CheckCaptions(2004, "confidence_id", map[int]string{1: "Low", 2: "Medium", 3: "High"}); len(problems) != 0 {
		t.Errorf("a correct table: %v", problems)
	}
	// 7 Listen arrives in 1.3.0: older releases are passed over.
	if problems := CheckCaptions(4001, "activity_id", map[int]string{1: "Open", 7: "Listen"}); len(problems) != 0 {
		t.Errorf("a value newer than the floor: %v", problems)
	}
	tests := []struct {
		name  string
		class int
		path  string
		table map[int]string
		want  string
	}{
		{"wrong caption", 2004, "confidence_id", map[int]string{1: "Lowest"}, `1 is "Low"`},
		{"wrong letter case", 2004, "confidence_id", map[int]string{1: "low"}, `1 is "Low"`},
		{"a value OCSF does not have", 2004, "confidence_id", map[int]string{7: "Certain"}, "no value 7"},
		{"not an enum", 2004, "confidence_score", map[int]string{1: "Low"}, "is not an enum"},
		{"no such field", 2004, "no_such_id", map[int]string{1: "Low"}, "is not an enum"},
	}
	for _, tt := range tests {
		problems := CheckCaptions(tt.class, tt.path, tt.table)
		if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), tt.want) {
			t.Errorf("%s: problems = %v; want one saying %q", tt.name, problems, tt.want)
		}
	}
}

func TestCheckCaptionNames(t *testing.T) {
	if problems := CheckCaptionNames([]int{201001, 201002}, "activity_id", []string{"create", "Set Security", "get", "set"}); len(problems) != 0 {
		t.Errorf("correct names: %v", problems)
	}
	problems := CheckCaptionNames([]int{3006}, "activity_id", []string{"add user", "GROUP_ADD"})
	if len(problems) != 1 || !strings.Contains(problems[0], "GROUP_ADD") {
		t.Errorf("problems = %v; want one naming GROUP_ADD", problems)
	}
	if problems := CheckCaptionNames([]int{3006}, "activity_name", []string{"add user"}); len(problems) != 1 {
		t.Errorf("a field that is not an enum: %v", problems)
	}
}

func TestCheckClassCaptions(t *testing.T) {
	if problems := CheckClassCaptions(map[int]string{3002: "Authentication", 5023: "Cloud Resources Inventory Info"}); len(problems) != 0 {
		t.Errorf("correct captions: %v", problems)
	}
	if problems := CheckClassCaptions(map[int]string{3002: "Logon", 424242: "Nothing"}); len(problems) < 2 {
		t.Errorf("problems = %v; want the wrong caption and the unknown class", problems)
	}
}

func TestNumberTables(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", `package a

var captions = map[int]string{1: "Low"}

var (
	ids    = map[string]int{"low": 1}
	typed  map[int64]string
	verbs  = map[string]string{"create": "created"}
	number = 3
)

func f() { local := map[int]string{}; _ = local }
`)
	write("a_test.go", `package a

var inTest = map[int]string{1: "Low"}
`)
	got, err := NumberTables(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"captions", "ids", "typed"}; !reflect.DeepEqual(got, want) {
		t.Errorf("NumberTables = %v; want %v", got, want)
	}

	uncovered, stale, err := UncoveredTables(dir, []string{"captions", "gone"}, []string{"typed"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(uncovered, []string{"ids"}) || !reflect.DeepEqual(stale, []string{"gone"}) {
		t.Errorf("UncoveredTables = %v, %v; want [ids], [gone]", uncovered, stale)
	}
}
