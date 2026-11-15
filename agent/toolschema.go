package agent

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// A tool's input struct is its schema (adr/0233): the model is shown exactly
// the fields the decoder reads, so the two cannot disagree. Each field carries
//
//	json:"name"     the property name
//	desc:"..."      the description the model reads; a field without one panics
//	required:"true" listed in the schema's required, in field order
//	nonempty:"true" on a list: minItems 1
//	nullable:"true" typed as its type or null; a null decodes as the zero value
//
// A struct the generator cannot render panics, as does an override that names no
// field. All are programming errors, reached by every session start and so by
// every test that builds a tool.
//
// A strict tool (adr/0376) has closed objects and no optional key. The
// generator closes every object it renders. It panics when any object in the
// final schema, an override's included, is not closed or has a key that is not
// required. A key that may have no value is required and nullable instead.

// inputSchema renders In as a tool's input schema. overrides replaces the named
// properties with schemas computed elsewhere, for what a Go type cannot state:
// request_action's action_type enum and parameters shapes, which come from the
// live catalog.
func inputSchema[In any](overrides map[string]any, strict bool) map[string]any {
	schema := objectSchema(reflect.TypeFor[In](), overrides, strict)
	if strict {
		checkStrict(reflect.TypeFor[In]().Name(), schema)
	}
	return schema
}

func objectSchema(t reflect.Type, overrides map[string]any, strict bool) map[string]any {
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
			props[name] = fieldSchema(t, f, strict)
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
	if strict {
		schema["additionalProperties"] = false
	}
	return schema
}

func fieldSchema(owner reflect.Type, f reflect.StructField, strict bool) map[string]any {
	desc := f.Tag.Get("desc")
	if desc == "" {
		panic(fmt.Sprintf("agent: %s.%s has no desc", owner.Name(), f.Name))
	}
	s := valueSchema(f.Type, strict)
	s["description"] = desc
	if f.Tag.Get("nonempty") == "true" {
		s["minItems"] = 1
	}
	if f.Tag.Get("nullable") == "true" {
		typ, ok := s["type"].(string)
		if !ok {
			panic(fmt.Sprintf("agent: %s.%s cannot be nullable", owner.Name(), f.Name))
		}
		s["type"] = []string{typ, "null"}
	}
	return s
}

func valueSchema(t reflect.Type, strict bool) map[string]any {
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int:
		return map[string]any{"type": "integer"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": valueSchema(t.Elem(), strict)}
	case reflect.Struct:
		return objectSchema(t, nil, strict)
	default:
		panic(fmt.Sprintf("agent: no tool schema for %s", t))
	}
}

// checkStrict panics unless every object in schema is closed and requires
// every key it lists. It walks properties, items and anyOf, so it reaches the
// objects an override brings.
func checkStrict(tool string, schema map[string]any) {
	walkSchema(schema, func(path string, s map[string]any) {
		if !isObjectSchema(s) {
			return
		}
		if open, ok := s["additionalProperties"].(bool); !ok || open {
			panic(fmt.Sprintf("agent: strict tool %s: the object at %s is not closed", tool, path))
		}
		props, _ := s["properties"].(map[string]any)
		required, _ := s["required"].([]string)
		for name := range props {
			if !slices.Contains(required, name) {
				panic(fmt.Sprintf("agent: strict tool %s: %s.%s is not required; make it required and nullable", tool, path, name))
			}
		}
	})
}

// walkSchema calls visit on schema and on every schema under it, through
// properties, items and anyOf, with a path that names where each one sits.
func walkSchema(schema map[string]any, visit func(path string, s map[string]any)) {
	var walk func(path string, s map[string]any)
	walk = func(path string, s map[string]any) {
		visit(path, s)
		if props, ok := s["properties"].(map[string]any); ok {
			names := make([]string, 0, len(props))
			for name := range props {
				names = append(names, name)
			}
			slices.Sort(names) // a deterministic first panic
			for _, name := range names {
				if p, ok := props[name].(map[string]any); ok {
					walk(path+"."+name, p)
				}
			}
		}
		if items, ok := s["items"].(map[string]any); ok {
			walk(path+"[]", items)
		}
		if shapes, ok := s["anyOf"].([]any); ok {
			for i, sh := range shapes {
				if m, ok := sh.(map[string]any); ok {
					walk(fmt.Sprintf("%s|%d", path, i), m)
				}
			}
		}
	}
	walk("input", schema)
}

// isObjectSchema reports whether s describes an object: its type is "object",
// or it lists properties.
func isObjectSchema(s map[string]any) bool {
	if _, ok := s["properties"]; ok {
		return true
	}
	switch typ := s["type"].(type) {
	case string:
		return typ == "object"
	case []string:
		return slices.Contains(typ, "object")
	case []any:
		return slices.Contains(typ, any("object"))
	}
	return false
}

// strictUnionLimit is the Messages API's limit on parameters that use anyOf or
// a list of types, counted across every strict tool of one request. Only
// request_action is strict, so its schema alone is counted. The API also
// limits optional parameters, but a strict tool has none: checkStrict refuses
// a key that is not required before the count is taken.
const strictUnionLimit = 16

// strictUnions counts the parameters of schema that use anyOf or a list of
// types, at any depth. The top-level schema is not a parameter.
func strictUnions(schema map[string]any) int {
	unions := 0
	walkSchema(schema, func(_ string, s map[string]any) {
		props, _ := s["properties"].(map[string]any)
		for _, p := range props {
			if m, ok := p.(map[string]any); ok && isUnion(m) {
				unions++
			}
		}
	})
	return unions
}

func isUnion(s map[string]any) bool {
	if _, ok := s["anyOf"]; ok {
		return true
	}
	switch s["type"].(type) {
	case []string, []any:
		return true
	}
	return false
}
