// Package wirefixture keeps the golden JSON samples the workbench's decoders
// are tested against (adr/0244). Each wire type has two: its zero value, which
// shows what Go omits and what it sends as null, and every field filled, which
// shows every key the type can send. Both come from the type itself, so a
// changed tag or a new field changes the sample.
package wirefixture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Words is a named string type that lists the words it may hold, in a fixed
// order (adr/0278). The filled sample writes its first word, for a field and
// for a map key alike.
type Words interface {
	WireValues() []string
}

// updateEnv, set, rewrites the samples instead of comparing them.
const updateEnv = "RECKON_UPDATE_WIRE"

// maxDepth bounds a recursive type (evidence nests evidence).
const maxDepth = 2

var (
	timeType  = reflect.TypeOf(time.Time{})
	rawType   = reflect.TypeOf(json.RawMessage{})
	wordsType = reflect.TypeOf((*Words)(nil)).Elem()
)

// Check compares T's two samples with those under dir. tune adjusts the filled
// sample, for a field whose shape the type does not state (a map[string]any).
func Check[T any](t *testing.T, dir, name string, tune ...func(*T)) {
	t.Helper()
	var zero T
	CheckValue(t, dir, name+".zero", zero)
	var filled T
	fill(t, reflect.ValueOf(&filled).Elem(), map[reflect.Type]int{})
	for _, f := range tune {
		f(&filled)
	}
	CheckValue(t, dir, name+".filled", filled)
}

// CheckValue compares one sample with its file under dir.
func CheckValue(t *testing.T, dir, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	got = append(got, '\n')
	path := filepath.Join(dir, name+".json")
	if os.Getenv(updateEnv) != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (make wire-fixtures writes it)", name, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s changed on the wire: run make wire-fixtures, and update the workbench's decoder\n got: %s\nwant: %s", name, got, want)
	}
}

func fill(t *testing.T, v reflect.Value, depth map[reflect.Type]int) {
	t.Helper()
	switch v.Type() {
	case timeType:
		v.Set(reflect.ValueOf(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
		return
	case rawType:
		v.SetBytes([]byte(`{"x":1}`))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(firstWord(t, v.Type()))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf("x"))
		}
	case reflect.Pointer:
		if depth[v.Type().Elem()] < maxDepth {
			v.Set(reflect.New(v.Type().Elem()))
			fill(t, v.Elem(), depth)
		}
	case reflect.Slice:
		if depth[v.Type().Elem()] < maxDepth {
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
			fill(t, v.Index(0), depth)
		}
	case reflect.Map:
		if key := v.Type().Key(); key.Kind() == reflect.String {
			elem := reflect.New(v.Type().Elem()).Elem()
			fill(t, elem, depth)
			v.Set(reflect.MakeMap(v.Type()))
			v.SetMapIndex(reflect.ValueOf(firstWord(t, key)).Convert(key), elem)
		}
	case reflect.Struct:
		depth[v.Type()]++
		defer func() { depth[v.Type()]-- }()
		for i := range v.NumField() {
			if v.Field(i).CanSet() {
				fill(t, v.Field(i), depth)
			}
		}
	}
}

// firstWord is what the filled sample writes for a string of type typ: the
// first word a Words type lists, or "x". It records a Words type as met, so
// RequireWordsChecked can find one no test checks.
func firstWord(t *testing.T, typ reflect.Type) string {
	t.Helper()
	if !typ.Implements(wordsType) {
		return "x"
	}
	words.mu.Lock()
	words.met[typ] = true
	words.mu.Unlock()
	list := reflect.Zero(typ).Interface().(Words).WireValues()
	if len(list) == 0 {
		t.Fatalf("%s lists no words, so its sample has none to write", typ)
	}
	return list[0]
}

// words records the Words types the samples met and the ones CheckWords
// checked.
var words = struct {
	mu      sync.Mutex
	met     map[reflect.Type]bool
	checked map[reflect.Type]bool
}{met: map[reflect.Type]bool{}, checked: map[reflect.Type]bool{}}

// RequireWordsChecked fails for every Words type a sample in this test binary
// met that no CheckWords call checked. A package calls it after its samples
// and its CheckWords calls, so a new word type on the wire cannot skip the
// check.
func RequireWordsChecked(t *testing.T) {
	t.Helper()
	words.mu.Lock()
	defer words.mu.Unlock()
	var missing []string
	for typ := range words.met {
		if !words.checked[typ] {
			missing = append(missing, typ.String())
		}
	}
	slices.Sort(missing)
	for _, name := range missing {
		t.Errorf("%s is on the wire but no CheckWords call checks it", name)
	}
}

// CheckWords fails unless w lists at least one word and none twice. When
// consts names constants, it also reads the const blocks that declare them in
// the non-test Go files of dir: every value in those blocks must be a string
// literal, or a conversion of one such as T("x"), and the list must equal
// those values in declaration order, block after block. So a constant added to
// a block without a list entry fails.
func CheckWords(t *testing.T, w Words, dir string, consts ...string) {
	t.Helper()
	typ := reflect.TypeOf(w)
	words.mu.Lock()
	words.checked[typ] = true
	words.mu.Unlock()
	name := typ.String()
	got := w.WireValues()
	if len(got) == 0 {
		t.Errorf("%s lists no words", name)
	}
	seen := map[string]bool{}
	for _, word := range got {
		if seen[word] {
			t.Errorf("%s lists %q twice", name, word)
		}
		seen[word] = true
	}
	if len(consts) == 0 {
		return
	}
	var want []string
	for _, c := range consts {
		block, err := constBlock(dir, c)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want = append(want, block...)
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s lists %q; its constants are %q", name, got, want)
	}
}

// constBlock returns the string values of the const block in dir that
// declares name, in declaration order.
func constBlock(dir, name string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST || !declares(gen, name) {
				continue
			}
			return stringValues(gen)
		}
	}
	return nil, fmt.Errorf("no const block in %s declares %s", dir, name)
}

// declares reports whether gen declares name.
func declares(gen *ast.GenDecl, name string) bool {
	for _, spec := range gen.Specs {
		for _, id := range spec.(*ast.ValueSpec).Names {
			if id.Name == name {
				return true
			}
		}
	}
	return false
}

// stringValues returns the values gen declares, in order. Each name must have
// a value that is a string literal, or a one-argument conversion of one.
func stringValues(gen *ast.GenDecl) ([]string, error) {
	var out []string
	for _, spec := range gen.Specs {
		vs := spec.(*ast.ValueSpec)
		if len(vs.Values) != len(vs.Names) {
			return nil, fmt.Errorf("const %s has no value of its own", vs.Names[0].Name)
		}
		for i, v := range vs.Values {
			s, ok := stringLiteral(v)
			if !ok {
				return nil, fmt.Errorf("const %s is not a string literal", vs.Names[i].Name)
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// stringLiteral reads a string literal, unwrapping a conversion such as
// T("x") or pkg.T("x").
func stringLiteral(e ast.Expr) (string, bool) {
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) == 1 {
		switch call.Fun.(type) {
		case *ast.Ident, *ast.SelectorExpr:
			e = call.Args[0]
		}
	}
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}
