package ocsfschema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
)

// Problem is one field of an event that does not match the OCSF schema.
type Problem struct {
	// Path is the field, in Resolve's syntax: "resources[].zone". It is ""
	// for a problem with the event as a whole, such as an unknown class.
	Path string
	// Reason says what is wrong, in words.
	Reason string
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Reason
	}
	return p.Path + ": " + p.Reason
}

// ValidateEvent checks an OCSF event against the schema of the release it
// declares in metadata.version, for the class it declares in class_uid
// (adr/0356, check 3). An event that declares no version, a version that is
// not checked in, or a class that version does not define is itself a
// problem. See Schema.Validate for what is checked.
func ValidateEvent(payload map[string]any) []Problem {
	meta, _ := payload["metadata"].(map[string]any)
	version, _ := meta["version"].(string)
	if version == "" {
		return []Problem{{Path: "metadata.version", Reason: "the event does not say which OCSF version it is written in"}}
	}
	schema, err := Load(version)
	if err != nil {
		return []Problem{{Path: "metadata.version", Reason: fmt.Sprintf("OCSF %s is not checked in under %s", version, Dir)}}
	}
	class, ok := intOf(payload["class_uid"])
	if !ok {
		return []Problem{{Path: "class_uid", Reason: "the event does not say which class it is"}}
	}
	return schema.Validate(class, payload)
}

// Validate walks an event of the given class field by field and reports
// every field that does not match the schema, sorted by path:
//
//   - A field the class, or the object it sits in, does not define.
//   - A list field that does not hold a list, an object field that does not
//     hold an object, and a scalar field that holds a list or an object.
//
// A json_t field, and a field whose object defines no fields of its own (the
// generic "object", which "unmapped" is), accept any value. A null value is
// an absent field. A scalar's own type (string or number) is not checked.
//
// The payload is JSON as encoding/json decodes it. Typed Go maps and slices
// are read the same way, so an adapter can pass the event it built.
func (s *Schema) Validate(classUID int, payload map[string]any) []Problem {
	c, ok := s.classes[classUID]
	if !ok {
		return []Problem{{Reason: fmt.Sprintf("OCSF %s has no class %d", s.version, classUID)}}
	}
	v := validator{schema: s}
	v.object("", c, reflect.ValueOf(payload))
	sort.SliceStable(v.problems, func(i, j int) bool { return v.problems[i].Path < v.problems[j].Path })
	return v.problems
}

type validator struct {
	schema   *Schema
	problems []Problem
}

func (v *validator) fail(path, format string, args ...any) {
	v.problems = append(v.problems, Problem{Path: path, Reason: fmt.Sprintf(format, args...)})
}

// object checks every field of one object against the entity that defines it.
// prefix is the object's own path, "" for the event.
func (v *validator) object(prefix string, def *entity, obj reflect.Value) {
	iter := obj.MapRange()
	for iter.Next() {
		key := iter.Key().String()
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		attr, ok := def.Attributes[key]
		if !ok {
			v.fail(path, "OCSF %s does not define this field on %s", v.schema.version, def.key)
			continue
		}
		v.value(path, attr, iter.Value())
	}
}

// value checks one field's value against its attribute.
func (v *validator) value(path string, attr attribute, val reflect.Value) {
	val = concrete(val)
	if !val.IsValid() || attr.Type == "json_t" {
		return // null is an absent field; json_t is free-form
	}
	var def *entity
	if attr.Type == "object_t" {
		obj, ok := v.schema.objects[attr.ObjectType]
		if !ok {
			v.fail(path, "the schema names object %q, which the export does not define", attr.ObjectType)
			return
		}
		if len(obj.Attributes) == 0 {
			return // the generic object: its content is the source's own
		}
		def = obj
	}
	if !attr.IsArray {
		v.single(path, attr, def, val)
		return
	}
	if kindOf(val) != kindList {
		v.fail(path, "OCSF defines a list here, and the event has %s", describe(val))
		return
	}
	for i := 0; i < val.Len(); i++ {
		v.single(path+"[]", attr, def, concrete(val.Index(i)))
	}
}

// single checks one value that is not a list: the field itself, or one entry
// of a list field. def is the object's definition for an object_t.
func (v *validator) single(path string, attr attribute, def *entity, val reflect.Value) {
	if !val.IsValid() {
		return
	}
	kind := kindOf(val)
	if def != nil {
		if kind != kindObject {
			v.fail(path, "OCSF defines an object (%s) here, and the event has %s", def.key, describe(val))
			return
		}
		v.object(path, def, val)
		return
	}
	if kind != kindScalar {
		v.fail(path, "OCSF defines a %s here, and the event has %s", attr.Type, describe(val))
	}
}

const (
	kindScalar = iota
	kindObject
	kindList
)

// kindOf sorts a value into the three JSON shapes. Only a map keyed by
// strings is an object.
func kindOf(val reflect.Value) int {
	switch val.Kind() {
	case reflect.Map:
		if val.Type().Key().Kind() == reflect.String {
			return kindObject
		}
	case reflect.Slice, reflect.Array:
		return kindList
	}
	return kindScalar
}

func describe(val reflect.Value) string {
	switch kindOf(val) {
	case kindObject:
		return "an object"
	case kindList:
		return "a list"
	}
	return "a scalar"
}

// concrete unwraps an interface or pointer to the value it holds. A nil one
// gives the zero Value.
func concrete(val reflect.Value) reflect.Value {
	for val.IsValid() && (val.Kind() == reflect.Interface || val.Kind() == reflect.Pointer) {
		if val.IsNil() {
			return reflect.Value{}
		}
		val = val.Elem()
	}
	if val.IsValid() && (val.Kind() == reflect.Map || val.Kind() == reflect.Slice) && val.IsNil() {
		return reflect.Value{}
	}
	return val
}

// intOf reads a class uid as JSON decodes it or as Go code writes it.
func intOf(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		if x != float64(int(x)) {
			return 0, false
		}
		return int(x), true
	case json.Number:
		n, err := strconv.Atoi(x.String())
		return n, err == nil
	}
	return 0, false
}
