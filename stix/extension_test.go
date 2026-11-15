package stix

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDefinitions_IDsAreUniqueUUIDv4(t *testing.T) {
	seen := map[string]bool{Publisher().ID: true}
	for _, d := range Definitions() {
		rest, ok := strings.CutPrefix(d.ID, "extension-definition--")
		if !ok {
			t.Errorf("%s: id %q is not an extension-definition id", d.Name, d.ID)
			continue
		}
		u, err := uuid.Parse(rest)
		if err != nil || u.Version() != 4 || u.Variant() != uuid.RFC4122 || u.String() != rest {
			t.Errorf("%s: id %q is not a lowercase UUIDv4", d.Name, d.ID)
		}
		if seen[d.ID] {
			t.Errorf("%s: id %q is used twice", d.Name, d.ID)
		}
		seen[d.ID] = true
	}
	pub, _ := strings.CutPrefix(Publisher().ID, "identity--")
	if u, err := uuid.Parse(pub); err != nil || u.Version() != 4 {
		t.Errorf("publisher id %q is not a UUIDv4", Publisher().ID)
	}
}

func TestDefinitions_NamesAreReckonAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Definitions() {
		if !strings.HasPrefix(d.Name, "reckon-") {
			t.Errorf("name %q does not start with reckon-", d.Name)
		}
		if seen[d.Name] {
			t.Errorf("name %q is defined twice", d.Name)
		}
		seen[d.Name] = true
		if got, ok := Lookup(d.Name); !ok || got.ID != d.ID {
			t.Errorf("Lookup(%q) = %v, %v", d.Name, got.ID, ok)
		}
		if len(d.ExtensionTypes) != 1 {
			t.Errorf("%s: extension_types %v, want one", d.Name, d.ExtensionTypes)
		}
	}
	if _, ok := Lookup("x-host"); ok {
		t.Error("Lookup found a definition for x-host")
	}
}

func TestDefinitions_SchemaMatchesItsURL(t *testing.T) {
	used := map[string]bool{}
	for _, d := range Definitions() {
		path, ok := strings.CutPrefix(d.SchemaURL(), SchemaBaseURL)
		if !ok {
			t.Errorf("%s: schema URL %q is not under %q", d.Name, d.SchemaURL(), SchemaBaseURL)
			continue
		}
		b, err := fs.ReadFile(SchemaFS(), path)
		if err != nil {
			t.Errorf("%s: schema %s is not embedded: %v", d.Name, path, err)
			continue
		}
		used[path] = true
		var s struct {
			Schema     string                     `json:"$schema"`
			ID         string                     `json:"$id"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(b, &s); err != nil {
			t.Errorf("%s: schema does not parse: %v", d.Name, err)
			continue
		}
		if s.Schema != "https://json-schema.org/draft/2020-12/schema" {
			t.Errorf("%s: $schema %q, want draft 2020-12", d.Name, s.Schema)
		}
		if s.ID != d.SchemaURL() {
			t.Errorf("%s: $id %q, want %q", d.Name, s.ID, d.SchemaURL())
		}
		if len(s.Properties) == 0 {
			t.Errorf("%s: schema lists no properties", d.Name)
		}
		// A new type's schema pins the type, and a property-extension's
		// schema pins its extension_type.
		key, want := "type", strconv.Quote(d.Name)
		if d.ExtensionTypes[0] == PropertyExtension {
			key, want = "extension_type", strconv.Quote(string(PropertyExtension))
		}
		var c struct {
			Const json.RawMessage `json:"const"`
		}
		if err := json.Unmarshal(s.Properties[key], &c); err != nil || string(c.Const) != want {
			t.Errorf("%s: schema property %s const = %s, want %s", d.Name, key, c.Const, want)
		}
	}
	// Every embedded schema file belongs to a definition.
	err := fs.WalkDir(SchemaFS(), "schemas", func(path string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && !used[path] {
			t.Errorf("embedded schema %s belongs to no definition", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDefinitions_MarshalAsExtensionDefinition(t *testing.T) {
	for _, d := range Definitions() {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("%s: %v", d.Name, err)
		}
		var got map[string]any
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("%s: %v", d.Name, err)
		}
		want := map[string]any{
			"type":           "extension-definition",
			"spec_version":   "2.1",
			"id":             d.ID,
			"created_by_ref": Publisher().ID,
			"name":           d.Name,
			"schema":         d.SchemaURL(),
			"version":        "1.0.0",
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, want %v", d.Name, k, got[k], v)
			}
		}
		for _, k := range []string{"created", "modified", "description"} {
			if s, _ := got[k].(string); s == "" {
				t.Errorf("%s: %s is missing", d.Name, k)
			}
		}
		if types, _ := got["extension_types"].([]any); len(types) != 1 || types[0] != string(d.ExtensionTypes[0]) {
			t.Errorf("%s: extension_types = %v", d.Name, got["extension_types"])
		}
	}
}

func TestPublisher(t *testing.T) {
	p := Publisher()
	if p.Type != "identity" || p.SpecVersion != "2.1" || p.IdentityClass != "organization" || p.Name == "" || p.Created == "" || p.Modified == "" {
		t.Errorf("Publisher() = %+v", p)
	}
}

// TestDefinitions_CoverEveryCustomType checks that the new-type definitions
// are exactly the reckon- type constants in the identity package, so a new
// custom type without a definition fails here.
func TestDefinitions_CoverEveryCustomType(t *testing.T) {
	want := reckonTypeConstants(t, filepath.Join("..", "identity"))
	if len(want) == 0 {
		t.Fatal("found no reckon- type constants in identity")
	}
	var got []string
	for _, d := range Definitions() {
		switch d.ExtensionTypes[0] {
		case NewSDO, NewSCO:
			got = append(got, d.Name)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("new-type definitions %v, want the identity constants %v", got, want)
	}
}

// reckonTypeConstants reads the string constants named Type* whose value
// starts with reckon- from the non-test Go files in dir.
func reckonTypeConstants(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range spec.Names {
				if i >= len(spec.Values) || !strings.HasPrefix(name.Name, "Type") {
					continue
				}
				lit, ok := spec.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if v, err := strconv.Unquote(lit.Value); err == nil && strings.HasPrefix(v, "reckon-") {
					out = append(out, v)
				}
			}
			return true
		})
	}
	slices.Sort(out)
	return out
}
