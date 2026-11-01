// Package wirefixture keeps the golden JSON samples the workbench's decoders
// are tested against (adr/0244). Each wire type has two: its zero value, which
// shows what Go omits and what it sends as null, and every field filled, which
// shows every key the type can send. Both come from the type itself, so a
// changed tag or a new field changes the sample.
package wirefixture

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// updateEnv, set, rewrites the samples instead of comparing them.
const updateEnv = "RECKON_UPDATE_WIRE"

// maxDepth bounds a recursive type (evidence nests evidence).
const maxDepth = 2

var (
	timeType = reflect.TypeOf(time.Time{})
	rawType  = reflect.TypeOf(json.RawMessage{})
)

// Check compares T's two samples with those under dir. tune adjusts the filled
// sample, for a field whose shape the type does not state (a map[string]any).
func Check[T any](t *testing.T, dir, name string, tune ...func(*T)) {
	t.Helper()
	var zero T
	CheckValue(t, dir, name+".zero", zero)
	var filled T
	fill(reflect.ValueOf(&filled).Elem(), map[reflect.Type]int{})
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

func fill(v reflect.Value, depth map[reflect.Type]int) {
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
		v.SetString("x")
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
			fill(v.Elem(), depth)
		}
	case reflect.Slice:
		if depth[v.Type().Elem()] < maxDepth {
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
			fill(v.Index(0), depth)
		}
	case reflect.Map:
		if v.Type().Key().Kind() == reflect.String {
			elem := reflect.New(v.Type().Elem()).Elem()
			fill(elem, depth)
			v.Set(reflect.MakeMap(v.Type()))
			v.SetMapIndex(reflect.ValueOf("x").Convert(v.Type().Key()), elem)
		}
	case reflect.Struct:
		depth[v.Type()]++
		defer func() { depth[v.Type()]-- }()
		for i := range v.NumField() {
			if v.Field(i).CanSet() {
				fill(v.Field(i), depth)
			}
		}
	}
}
