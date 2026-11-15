// Package stixschema checks that a STIX object is valid STIX 2.1 and follows
// reckon's extension definitions (adr/0357, design/01 "STIX conformance and
// change", rule 8).
//
// Check validates an object against the OASIS STIX 2.1 JSON schemas checked
// in under third_party/stix2-json-schemas/, and against the schema of each
// reckon definition the object uses, from the stix package. It then makes the
// checks the schemas cannot make:
//
//   - A sighting's sighting_of_ref names an SDO.
//   - Every top-level property is one the type defines.
//   - Every extensions key is a known definition id, and each value's
//     extension_type is the one that definition declares.
//
// It is used by tests. The OASIS files are found by walking up from the
// working directory to the module root, so the package works from any
// package's test without configuration. Nothing is fetched from the network.
package stixschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/sd-strax/reckon/stix"
)

// Dir is the directory of the OASIS schema files, relative to the module
// root.
const Dir = "third_party/stix2-json-schemas/schemas"

// oasisIDPrefix is the start of every OASIS schema's $id. The files refer to
// each other by these URLs.
const oasisIDPrefix = "http://raw.githubusercontent.com/oasis-open/cti-stix2-json-schemas/stix2.1/schemas/"

// Problem is one thing wrong with an object.
type Problem struct {
	// Schema is the schema that failed, such as "sdos/indicator.json" or
	// "reckon-host/1.0.0.json". It is "" for a hand-written check.
	Schema string
	// Message says what is wrong, in the validator's words or in ours.
	Message string
}

func (p Problem) String() string {
	if p.Schema == "" || strings.HasPrefix(p.Message, "validating "+p.Schema+":") {
		return p.Message
	}
	return p.Schema + ": " + p.Message
}

// CheckJSON decodes an object's STIX JSON and checks it.
func CheckJSON(raw []byte) []Problem {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return []Problem{{Message: "not a JSON object: " + err.Error()}}
	}
	return Check(obj)
}

// Check checks one decoded STIX object: a map as encoding/json decodes it.
func Check(obj map[string]any) []Problem {
	typ, _ := obj["type"].(string)
	if typ == "" {
		return []Problem{{Message: "the object has no type"}}
	}
	var out []Problem
	validate := func(rel string) {
		r, err := oasis(rel)
		if err != nil {
			out = append(out, Problem{Schema: rel, Message: err.Error()})
			return
		}
		// The schema sees the corrected copy; the spec's own rule for each
		// corrected value is checked on the object (corrections.go).
		cp, fixed := applyCorrections(typ, obj)
		out = append(out, fixed...)
		if err := r.Validate(cp); err != nil {
			out = append(out, Problem{Schema: rel, Message: shorten(err)})
		}
		out = append(out, checkFormats(oasisIDPrefix+rel, rel, obj)...)
	}

	custom, isCustom := customType(typ)
	switch {
	case typ == "extension-definition":
		validate("common/extension-definition.json")
	case isCustom:
		if custom.ExtensionTypes[0] == stix.NewSCO {
			validate("common/cyber-observable-core.json")
		} else {
			validate("common/core.json")
		}
		out = append(out, validateReckon(custom, obj)...)
	default:
		rel, ok := nativeSchema(typ)
		if !ok {
			out = append(out, Problem{Message: fmt.Sprintf("%s is neither a STIX 2.1 type nor a reckon type", typ)})
			break
		}
		validate(rel)
	}

	out = append(out, checkPropertyNames(obj)...)
	out = append(out, checkTopLevel(typ, obj)...)
	out = append(out, checkExtensions(typ, obj)...)
	if typ == "sighting" {
		out = append(out, checkSightingOf(obj)...)
	}
	return out
}

// checkTopLevel reports each top-level property the type does not define. The
// OASIS schemas accept any property, so a custom property outside an
// extension would pass them.
func checkTopLevel(typ string, obj map[string]any) []Problem {
	allowed := []string{"type", "spec_version", "id", "extensions"}
	if typ == "extension-definition" {
		// STIX 2.1 §7.3, beyond the common properties of an SDO.
		allowed = append(allowed, "created", "modified", "created_by_ref", "revoked", "labels",
			"external_references", "object_marking_refs", "granular_markings",
			"name", "description", "schema", "version", "extension_types", "extension_properties")
	} else {
		props, ok := stix.TypeProperties(typ)
		if !ok {
			return []Problem{{Message: fmt.Sprintf("%s is not a type reckon writes", typ)}}
		}
		allowed = append(allowed, props...)
		if stix.Timestamped(typ) {
			allowed = append(allowed, "created", "modified")
		}
	}
	var out []Problem
	for _, name := range sortedKeys(obj) {
		if !slices.Contains(allowed, name) {
			out = append(out, Problem{Message: fmt.Sprintf("top-level property %q is not a property of %s", name, typ)})
		}
	}
	return out
}

// checkExtensions checks each extensions entry: a known definition, the
// extension type that definition declares, a property-extension that is
// valid against its schema, and a new-type entry only on its own type. A
// reckon type must carry its own definition's entry.
func checkExtensions(typ string, obj map[string]any) []Problem {
	var out []Problem
	exts, _ := obj["extensions"].(map[string]any)
	if custom, ok := customType(typ); ok {
		if _, has := exts[custom.ID]; !has {
			out = append(out, Problem{Message: fmt.Sprintf("%s does not carry its own definition %s in extensions", typ, custom.ID)})
		}
	}
	for _, key := range sortedKeys(exts) {
		d, ok := byID(key)
		if !ok {
			out = append(out, Problem{Message: fmt.Sprintf("extensions key %q is not a reckon extension definition", key)})
			continue
		}
		val, ok := exts[key].(map[string]any)
		if !ok {
			out = append(out, Problem{Message: fmt.Sprintf("extensions[%s] (%s) is not an object", key, d.Name)})
			continue
		}
		want := string(d.ExtensionTypes[0])
		if got, _ := val["extension_type"].(string); got != want {
			out = append(out, Problem{Message: fmt.Sprintf("extensions[%s] (%s) has extension_type %q; the definition declares %q", key, d.Name, got, want)})
		}
		switch d.ExtensionTypes[0] {
		case stix.PropertyExtension:
			out = append(out, validateReckon(d, val)...)
		case stix.NewSDO, stix.NewSCO:
			if d.Name != typ {
				out = append(out, Problem{Message: fmt.Sprintf("extensions[%s] declares the type %s, but the object is a %s", key, d.Name, typ)})
			}
		}
	}
	return out
}

// checkSightingOf reports a sighting_of_ref that names anything but an SDO.
// The OASIS sighting schema accepts any identifier, but STIX 2.1 §5.2 says
// the property must name an SDO.
func checkSightingOf(obj map[string]any) []Problem {
	ref, _ := obj["sighting_of_ref"].(string)
	typ, _, _ := strings.Cut(ref, "--")
	if isSDO(typ) {
		return nil
	}
	return []Problem{{Message: fmt.Sprintf("sighting_of_ref %q does not name an SDO", ref)}}
}

// isSDO reports whether a type is a STIX domain object: an OASIS SDO, or a
// reckon type declared as a new SDO.
func isSDO(typ string) bool {
	if d, ok := customType(typ); ok {
		return d.ExtensionTypes[0] == stix.NewSDO
	}
	root, err := oasisRoot()
	if err != nil || typ == "" {
		return false
	}
	_, err = os.Stat(filepath.Join(root, "sdos", typ+".json"))
	return err == nil
}

// customType returns the definition of a reckon custom type.
func customType(typ string) (stix.Definition, bool) {
	d, ok := stix.Lookup(typ)
	if !ok || d.ExtensionTypes[0] == stix.PropertyExtension {
		return stix.Definition{}, false
	}
	return d, true
}

// byID returns the reckon definition with an id.
func byID(id string) (stix.Definition, bool) {
	for _, d := range stix.Definitions() {
		if d.ID == id {
			return d, true
		}
	}
	return stix.Definition{}, false
}

// nativeSchema returns the OASIS schema file of a STIX 2.1 type.
func nativeSchema(typ string) (string, bool) {
	root, err := oasisRoot()
	if err != nil {
		return "", false
	}
	for _, dir := range []string{"sdos", "sros", "observables"} {
		rel := dir + "/" + typ + ".json"
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			return rel, true
		}
	}
	return "", false
}

// validateReckon validates v against a reckon definition's schema.
func validateReckon(d stix.Definition, v any) []Problem {
	name := d.Name + "/" + d.Version + ".json"
	r, err := reckon(d)
	if err != nil {
		return []Problem{{Schema: name, Message: err.Error()}}
	}
	var out []Problem
	if err := r.Validate(v); err != nil {
		out = append(out, Problem{Schema: name, Message: shorten(err)})
	}
	return append(out, checkFormats(d.SchemaURL(), name, v)...)
}

// shorten is the validator's message with each schema's $id cut to its path,
// so a line names "common/hashes-type.json" rather than the whole URL.
func shorten(err error) string {
	return strings.NewReplacer(oasisIDPrefix, "", stix.SchemaBaseURL+"schemas/", "").Replace(err.Error())
}

var (
	mu       sync.Mutex
	resolved = map[string]*jsonschema.Resolved{}
)

// oasis returns the resolved OASIS schema at a path below Dir.
func oasis(rel string) (*jsonschema.Resolved, error) {
	return resolve(oasisIDPrefix + rel)
}

// reckon returns the resolved schema of a reckon definition.
func reckon(d stix.Definition) (*jsonschema.Resolved, error) {
	return resolve(d.SchemaURL())
}

func resolve(id string) (*jsonschema.Resolved, error) {
	mu.Lock()
	defer mu.Unlock()
	if r, ok := resolved[id]; ok {
		return r, nil
	}
	u, err := url.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("stixschema: %w", err)
	}
	s, err := load(u)
	if err != nil {
		return nil, err
	}
	r, err := s.Resolve(&jsonschema.ResolveOptions{BaseURI: id, Loader: load})
	if err != nil {
		return nil, fmt.Errorf("stixschema: resolve %s: %w", id, err)
	}
	resolved[id] = r
	return r, nil
}

// load is the resolver's loader. It maps an OASIS $id onto the checked-in
// files, and a reckon $id onto the schemas embedded in the stix package. Any
// other URL is an error, so a schema never reaches the network.
func load(u *url.URL) (*jsonschema.Schema, error) {
	id := u.String()
	if i := strings.IndexByte(id, '#'); i >= 0 {
		id = id[:i]
	}
	var (
		raw []byte
		err error
	)
	switch {
	case strings.HasPrefix(id, oasisIDPrefix):
		root, rerr := oasisRoot()
		if rerr != nil {
			return nil, rerr
		}
		raw, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(id, oasisIDPrefix)))) // #nosec G304 -- a path below the checked-in schema directory
	case strings.HasPrefix(id, stix.SchemaBaseURL):
		raw, err = fs.ReadFile(stix.SchemaFS(), strings.TrimPrefix(id, stix.SchemaBaseURL))
	default:
		return nil, fmt.Errorf("stixschema: %s is neither an OASIS nor a reckon schema", id)
	}
	if err != nil {
		return nil, fmt.Errorf("stixschema: load %s: %w", id, err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("stixschema: parse %s: %w", id, err)
	}
	return &s, nil
}

// Files lists every OASIS schema file, as a path below Dir.
func Files() ([]string, error) {
	root, err := oasisRoot()
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() && strings.HasSuffix(p, ".json") {
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return rerr
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// Resolve resolves one OASIS schema file, given as a path below Dir.
func Resolve(rel string) error {
	_, err := oasis(rel)
	return err
}

var (
	rootOnce sync.Once
	rootDir  string
	errRoot  error
)

// oasisRoot is the absolute path of Dir, found from the working directory.
func oasisRoot() (string, error) {
	rootOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			errRoot = fmt.Errorf("stixschema: %w", err)
			return
		}
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				rootDir = filepath.Join(dir, filepath.FromSlash(Dir))
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				errRoot = errors.New("stixschema: no go.mod above the working directory")
				return
			}
			dir = parent
		}
	})
	return rootDir, errRoot
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
