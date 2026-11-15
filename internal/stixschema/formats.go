package stixschema

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sd-strax/reckon/stix"
)

// google/jsonschema-go does not check the "format" keyword (its doc.go). The
// STIX schemas carry four of the spec's rules as formats, so this file checks
// them itself (adr/0357 rule 8). It walks the schema beside the object and
// checks each string value whose schema names a format.

// formatChecks are the formats the OASIS and reckon schemas use, with the
// rule each stands for.
var formatChecks = map[string]struct {
	check func(string) bool
	rule  string
}{
	"date-time":    {stix.IsTimestamp, "an RFC 3339 date-time"},
	"uri":          {stix.IsURI, "an RFC 3986 URI"},
	"email":        {stix.IsAddrSpec, "an RFC 5322 addr-spec"},
	"idn-hostname": {stix.IsDomainName, "an RFC 1034 name with RFC 5890 labels"},
}

// checkFormats walks the schema with $id id beside v and reports each value
// that breaks its format. name is the schema's short name for the report.
func checkFormats(id, name string, v any) []Problem {
	doc, err := rawSchema(id)
	if err != nil {
		return []Problem{{Schema: name, Message: err.Error()}}
	}
	w := formatWalk{seen: map[string]bool{}}
	w.walk(id, doc, doc, v, "")
	for i := range w.out {
		w.out[i].Schema = name
	}
	return w.out
}

type formatWalk struct {
	out  []Problem
	seen map[string]bool // $ref targets entered for the current value, so a cycle ends
}

func (w *formatWalk) walk(id string, doc, node map[string]any, v any, at string) {
	if ref, ok := node["$ref"].(string); ok {
		w.ref(id, doc, ref, v, at)
	}
	if f, ok := node["format"].(string); ok {
		if c, known := formatChecks[f]; known {
			if s, isString := v.(string); isString && !c.check(s) {
				w.out = append(w.out, Problem{Message: fmt.Sprintf("%s: %q is not %s (format %s)", pathOr(at), s, c.rule, f)})
			}
		}
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		list, _ := node[key].([]any)
		for _, sub := range list {
			if m, ok := sub.(map[string]any); ok {
				w.walk(id, doc, m, v, at)
			}
		}
	}
	if props, ok := node["properties"].(map[string]any); ok {
		if obj, isObj := v.(map[string]any); isObj {
			for name, sub := range props {
				m, ok := sub.(map[string]any)
				val, present := obj[name]
				if ok && present {
					w.walk(id, doc, m, val, at+"."+name)
				}
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		if list, isList := v.([]any); isList {
			for i, item := range list {
				w.walk(id, doc, items, item, fmt.Sprintf("%s[%d]", at, i))
			}
		}
	}
	if extra, ok := node["additionalProperties"].(map[string]any); ok {
		if obj, isObj := v.(map[string]any); isObj {
			props, _ := node["properties"].(map[string]any)
			for name, val := range obj {
				if _, listed := props[name]; !listed {
					w.walk(id, doc, extra, val, at+"."+name)
				}
			}
		}
	}
}

// ref follows a $ref: a fragment in the same file, or another file named
// relative to this one.
func (w *formatWalk) ref(id string, doc map[string]any, ref string, v any, at string) {
	base, err := url.Parse(id)
	if err != nil {
		return
	}
	target, err := base.Parse(ref)
	if err != nil {
		return
	}
	frag := target.Fragment
	target.Fragment = ""
	targetID := target.String()
	key := targetID + "#" + frag + "@" + at
	if w.seen[key] {
		return
	}
	w.seen[key] = true
	targetDoc := doc
	if targetID != id {
		if targetDoc, err = rawSchema(targetID); err != nil {
			return
		}
	}
	node := targetDoc
	for _, part := range strings.Split(strings.TrimPrefix(frag, "/"), "/") {
		if part == "" {
			continue
		}
		next, ok := node[part].(map[string]any)
		if !ok {
			return
		}
		node = next
	}
	w.walk(targetID, targetDoc, node, v, at)
}

func pathOr(at string) string {
	if at == "" {
		return "the value"
	}
	return strings.TrimPrefix(at, ".")
}

// rawSchema reads a schema file as plain JSON, by its $id.
func rawSchema(id string) (map[string]any, error) {
	mu.Lock()
	defer mu.Unlock()
	if doc, ok := rawCache[id]; ok {
		return doc, nil
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
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("stixschema: parse %s: %w", id, err)
	}
	rawCache[id] = doc
	return doc, nil
}

var rawCache = map[string]map[string]any{}
