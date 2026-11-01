package agent

import (
	"fmt"
	"reflect"
	"strings"
)

// A tool's input struct is its schema (adr/0233): the model is shown exactly
// the fields the decoder reads, so the two cannot disagree. Each field carries
//
//	json:"name"     the property name
//	desc:"..."      the description the model reads; a field without one panics
//	required:"true" listed in the schema's required, in field order
//	nonempty:"true" on a list: minItems 1
//
// A struct the generator cannot render panics, as does an override that names no
// field. All are programming errors, reached by every session start and so by
// every test that builds a tool.

// inputSchema renders In as a tool's input schema. overrides replaces the named
// properties with schemas computed elsewhere, for what a Go type cannot state:
// request_action's action_type enum and parameters union, which come from the
// live catalog.
func inputSchema[In any](overrides map[string]any) map[string]any {
	return objectSchema(reflect.TypeFor[In](), overrides)
}

func objectSchema(t reflect.Type, overrides map[string]any) map[string]any {
	props := map[string]any{}
	var required []string
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" {
			panic(fmt.Sprintf("agent: %s.%s has no json name", t.Name(), f.Name))
		}
		if s, ok := overrides[name]; ok {
			props[name] = s
		} else {
			props[name] = fieldSchema(t, f)
		}
		if f.Tag.Get("required") == "true" {
			required = append(required, name)
		}
	}
	for name := range overrides {
		if _, ok := props[name]; !ok {
			panic(fmt.Sprintf("agent: %s has no field %q to override", t.Name(), name))
		}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func fieldSchema(owner reflect.Type, f reflect.StructField) map[string]any {
	desc := f.Tag.Get("desc")
	if desc == "" {
		panic(fmt.Sprintf("agent: %s.%s has no desc", owner.Name(), f.Name))
	}
	s := valueSchema(f.Type)
	s["description"] = desc
	if f.Tag.Get("nonempty") == "true" {
		s["minItems"] = 1
	}
	return s
}

func valueSchema(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int:
		return map[string]any{"type": "integer"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": valueSchema(t.Elem())}
	case reflect.Struct:
		return objectSchema(t, nil)
	default:
		panic(fmt.Sprintf("agent: no tool schema for %s", t))
	}
}
