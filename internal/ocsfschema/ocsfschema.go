// Package ocsfschema answers questions about the OCSF schema from the copies
// checked in under third_party/ocsf-schema/ (adr/0356): does a class exist at
// a version, what is it called, does a field path exist on it, and does an
// event hold only fields its class defines, each in the shape and the scalar
// type the schema gives it, with its enum values and the fields the class
// requires (Validate).
//
// It is used by tests. The schema files are found by walking up from the
// working directory to the module root, so the package works from any
// package's test without configuration.
package ocsfschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Dir is the schema directory, relative to the module root.
const Dir = "third_party/ocsf-schema"

// Floor and Ceiling are the oldest and newest releases the normalizers
// support (design/03 §4.16).
const (
	Floor   = "1.1.0"
	Ceiling = "1.9.0"
)

// Class describes one event class.
type Class struct {
	UID int
	// Key is the class's key in the export. An extension class carries its
	// extension as a prefix: "win/registry_value_activity".
	Key string
	// Name is the class name without the extension: "registry_value_activity".
	Name string
	// Caption is the display name: "Registry Value Activity".
	Caption string
	// Extension is the extension that defines the class, or "" for a core
	// class.
	Extension string
}

// Schema is one OCSF release.
type Schema struct {
	version string
	classes map[int]*entity    // by class uid
	objects map[string]*entity // by export key, e.g. "file", "win/reg_value"
	types   map[string]string  // a derived type's parent: "username_t" → "string_t"
}

// entity is a class or an object as the export describes it. Only the fields
// the lookups need are decoded.
type entity struct {
	key        string
	UID        int                  `json:"uid"`
	Name       string               `json:"name"`
	Caption    string               `json:"caption"`
	Extension  string               `json:"extension"`
	Attributes map[string]attribute `json:"attributes"`
}

type attribute struct {
	Type       string `json:"type"`        // "string_t", "object_t", "json_t", ...
	ObjectType string `json:"object_type"` // the key in objects, for an object_t
	IsArray    bool   `json:"is_array"`
	// Enum is the field's allowed values, keyed by the value as text ("3"),
	// or nil when the field is not an enum.
	Enum map[string]enumValue `json:"enum"`
	// Sibling is the field that holds an enum value's caption:
	// "activity_name" for activity_id. Some releases leave it out.
	Sibling string `json:"sibling"`
	// Requirement is "required", "recommended" or "optional".
	Requirement string `json:"requirement"`
	// Profile is the profile that adds the field to the class, or "" for a
	// field the class has by itself.
	Profile string `json:"profile"`
}

type enumValue struct {
	Caption string `json:"caption"`
}

// export is the top level of a schema.json file.
type export struct {
	Version string             `json:"version"`
	Classes map[string]*entity `json:"classes"`
	Objects map[string]*entity `json:"objects"`
	Types   map[string]struct {
		Type string `json:"type"` // the parent type, "" for a base type
	} `json:"types"`
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*Schema{}

	rootOnce sync.Once
	rootDir  string
	errRoot  error
)

// Load returns the schema for a release such as "1.9.0". A file is read once
// and then served from memory.
func Load(version string) (*Schema, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if s, ok := cache[version]; ok {
		return s, nil
	}
	root, err := moduleRoot()
	if err != nil {
		return nil, err
	}
	s, err := loadFile(filepath.Join(root, Dir, version, "schema.json"), version)
	if err != nil {
		return nil, err
	}
	cache[version] = s
	return s, nil
}

// Versions lists the releases checked in, oldest first.
func Versions() ([]string, error) {
	root, err := moduleRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, Dir))
	if err != nil {
		return nil, fmt.Errorf("ocsfschema: %w", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, Dir, e.Name(), "schema.json")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Slice(out, func(i, j int) bool { return versionLess(out[i], out[j]) })
	return out, nil
}

func loadFile(path, version string) (*Schema, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- a fixed path under the module root
	if err != nil {
		return nil, fmt.Errorf("ocsfschema: load %s: %w", version, err)
	}
	var ex export
	if err := json.Unmarshal(data, &ex); err != nil {
		return nil, fmt.Errorf("ocsfschema: parse %s: %w", path, err)
	}
	if ex.Version != version {
		return nil, fmt.Errorf("ocsfschema: %s declares version %q, want %q", path, ex.Version, version)
	}
	s := &Schema{
		version: version,
		classes: make(map[int]*entity, len(ex.Classes)),
		objects: ex.Objects,
		types:   make(map[string]string, len(ex.Types)),
	}
	for name, t := range ex.Types {
		s.types[name] = t.Type
	}
	for key, obj := range ex.Objects {
		obj.key = key
	}
	for key, c := range ex.Classes {
		c.key = key
		if prev, dup := s.classes[c.UID]; dup {
			return nil, fmt.Errorf("ocsfschema: %s: classes %q and %q share uid %d", path, prev.key, key, c.UID)
		}
		s.classes[c.UID] = c
	}
	return s, nil
}

// moduleRoot finds the directory holding go.mod, walking up from the working
// directory. A test runs in its package's directory, so this is the module
// the test belongs to.
func moduleRoot() (string, error) {
	rootOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			errRoot = fmt.Errorf("ocsfschema: %w", err)
			return
		}
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				rootDir = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				errRoot = errors.New("ocsfschema: no go.mod above the working directory")
				return
			}
			dir = parent
		}
	})
	return rootDir, errRoot
}

// Version is the release this schema describes.
func (s *Schema) Version() string { return s.version }

// Class returns the class with the given uid. UID 0 is the base event, which
// the export lists among the classes.
func (s *Schema) Class(uid int) (Class, bool) {
	c, ok := s.classes[uid]
	if !ok {
		return Class{}, false
	}
	return Class{UID: c.UID, Key: c.key, Name: c.Name, Caption: c.Caption, Extension: c.Extension}, true
}

// HasClass reports whether the release defines a class with the given uid.
func (s *Schema) HasClass(uid int) bool {
	_, ok := s.classes[uid]
	return ok
}

// ClassUIDs lists every class uid in the release, sorted.
func (s *Schema) ClassUIDs() []int {
	out := make([]int, 0, len(s.classes))
	for uid := range s.classes {
		out = append(out, uid)
	}
	sort.Ints(out)
	return out
}

// HasPath reports whether a field path exists on a class. See Resolve for the
// path syntax.
func (s *Schema) HasPath(classUID int, path string) bool {
	return s.Resolve(classUID, path) == nil
}

// Resolve checks that a field path exists on a class. It returns nil when the
// path exists, and an error naming the first segment that does not.
//
// A path is dotted, and a segment that steps into a list ends in "[]":
// "device.hostname", "resources[].zone", "email.to[]".
//
//   - Each segment must be an attribute of the class or object reached so far.
//   - A list attribute must be written with "[]" before a further segment. As
//     the last segment it may be written with or without it.
//   - An attribute that is not a list must not be written with "[]".
//   - A json_t attribute, and an attribute whose object defines no attributes
//     of its own (the generic "object", which "unmapped" is), accept any
//     deeper path.
func (s *Schema) Resolve(classUID int, path string) error {
	_, err := s.attributeAt(classUID, path)
	return err
}

// attributeAt is the walk behind Resolve. It returns the attribute the path
// ends on. For a path that ends below a free-form field (a json_t, or the
// generic object), it returns that free-form field.
func (s *Schema) attributeAt(classUID int, path string) (attribute, error) {
	c, ok := s.classes[classUID]
	if !ok {
		return attribute{}, fmt.Errorf("OCSF %s has no class %d", s.version, classUID)
	}
	if path == "" {
		return attribute{}, errors.New("empty path")
	}
	cur := c
	segs := strings.Split(path, ".")
	for i, seg := range segs {
		name, list := strings.CutSuffix(seg, "[]")
		last := i == len(segs)-1
		at := strings.Join(segs[:i+1], ".")
		attr, ok := cur.Attributes[name]
		if !ok || name == "" {
			return attribute{}, fmt.Errorf("OCSF %s class %d (%s): no field %q", s.version, classUID, c.key, at)
		}
		if attr.Type == "json_t" {
			return attr, nil // free-form: anything below it is the source's own
		}
		if list && !attr.IsArray {
			return attribute{}, fmt.Errorf("OCSF %s class %d (%s): %q is not a list", s.version, classUID, c.key, strings.TrimSuffix(at, "[]"))
		}
		if last {
			return attr, nil
		}
		if attr.IsArray && !list {
			return attribute{}, fmt.Errorf("OCSF %s class %d (%s): %q is a list, write it %q", s.version, classUID, c.key, at, at+"[]")
		}
		if attr.Type != "object_t" {
			return attribute{}, fmt.Errorf("OCSF %s class %d (%s): %q is a %s, it has no fields", s.version, classUID, c.key, at, attr.Type)
		}
		obj, ok := s.objects[attr.ObjectType]
		if !ok {
			return attribute{}, fmt.Errorf("OCSF %s class %d (%s): %q names object %q, which the export does not define", s.version, classUID, c.key, at, attr.ObjectType)
		}
		if len(obj.Attributes) == 0 {
			return attr, nil // the generic object: anything below it is the source's own
		}
		cur = obj
	}
	return attribute{}, errors.New("empty path")
}

// versionLess orders release numbers numerically, so 1.10.0 sorts after 1.9.0.
func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		if len(as[i]) != len(bs[i]) {
			return len(as[i]) < len(bs[i])
		}
		return as[i] < bs[i]
	}
	return len(as) < len(bs)
}
