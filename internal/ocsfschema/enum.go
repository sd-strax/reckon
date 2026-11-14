package ocsfschema

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Enum returns the values an enum field allows on a class, as value →
// caption: {1: "Low", 2: "Medium", 3: "High"} for a detection finding's
// confidence_id. The path is in Resolve's syntax. It reports false when the
// release has no such class or field, or the field is not an enum of whole
// numbers.
func (s *Schema) Enum(classUID int, path string) (map[int]string, bool) {
	attr, err := s.attributeAt(classUID, path)
	if err != nil || len(attr.Enum) == 0 {
		return nil, false
	}
	return attr.intEnum()
}

// intEnum reads an attribute's enum as whole number → caption.
func (a attribute) intEnum() (map[int]string, bool) {
	out := make(map[int]string, len(a.Enum))
	for key, v := range a.Enum {
		id, err := strconv.Atoi(key)
		if err != nil {
			return nil, false
		}
		out[id] = v.Caption
	}
	return out, len(out) > 0
}

// CheckCaptions checks a table of enum values the code holds by number
// against the enum at path of a class, in every release checked in
// (adr/0356). It returns one line per mismatch, and nothing when the table
// agrees with OCSF:
//
//   - The field must be an enum in the newest release, and the newest
//     release must define every value in the table.
//   - In each release that has the field and defines a value, that value's
//     caption must be the table's, letter for letter.
//
// A release older than the class or the field, and a value a release does
// not have yet, are passed over: the code reads a newer value only when an
// event carries it.
func CheckCaptions(classUID int, path string, table map[int]string) []string {
	return checkEnum(classUID, path, func(v string, enum map[int]string, newest bool) []string {
		var out []string
		for _, id := range sortedKeys(table) {
			got, ok := enum[id]
			switch {
			case !ok && newest:
				out = append(out, fmt.Sprintf("OCSF %s class %d %s has no value %d (the code calls it %q)", v, classUID, path, id, table[id]))
			case ok && got != table[id]:
				out = append(out, fmt.Sprintf("OCSF %s class %d %s: %d is %q, and the code calls it %q", v, classUID, path, id, got, table[id]))
			}
		}
		return out
	})
}

// CheckCaptionNames checks a list of enum captions the code holds by name,
// such as the keys of a table of verbs by activity name. Each name must be a
// caption of the enum at path of one of the classes, in at least one release
// checked in. Letter case is not compared. It returns one line per name that
// no release has.
func CheckCaptionNames(classUIDs []int, path string, names []string) []string {
	versions, err := Versions()
	if err != nil {
		return []string{err.Error()}
	}
	known := map[string]bool{}
	for _, v := range versions {
		s, err := Load(v)
		if err != nil {
			return []string{err.Error()}
		}
		for _, uid := range classUIDs {
			enum, _ := s.Enum(uid, path)
			for _, caption := range enum {
				known[strings.ToLower(caption)] = true
			}
		}
	}
	if len(known) == 0 {
		return []string{fmt.Sprintf("classes %v: %s is not an enum in any OCSF release checked in", classUIDs, path)}
	}
	var out []string
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	for _, name := range sorted {
		if !known[strings.ToLower(name)] {
			out = append(out, fmt.Sprintf("classes %v %s: no OCSF release has a value called %q", classUIDs, path, name))
		}
	}
	return out
}

// CheckClassCaptions checks a table of class captions by class number against
// every release checked in: a class the newest release lacks, or a caption
// that differs in a release that has the class, is one line.
func CheckClassCaptions(table map[int]string) []string {
	versions, err := Versions()
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	for i, v := range versions {
		s, err := Load(v)
		if err != nil {
			return []string{err.Error()}
		}
		for _, uid := range sortedKeys(table) {
			c, ok := s.Class(uid)
			switch {
			case !ok && i == len(versions)-1:
				out = append(out, fmt.Sprintf("OCSF %s has no class %d (the code calls it %q)", v, uid, table[uid]))
			case ok && c.Caption != table[uid]:
				out = append(out, fmt.Sprintf("OCSF %s: class %d is %q, and the code calls it %q", v, uid, c.Caption, table[uid]))
			}
		}
	}
	return out
}

// checkEnum runs check on the enum at path of a class in every release that
// has it, and requires the newest release to have it.
func checkEnum(classUID int, path string, check func(version string, enum map[int]string, newest bool) []string) []string {
	versions, err := Versions()
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	for i, v := range versions {
		s, err := Load(v)
		if err != nil {
			return []string{err.Error()}
		}
		newest := i == len(versions)-1
		enum, ok := s.Enum(classUID, path)
		if !ok {
			if newest {
				out = append(out, fmt.Sprintf("OCSF %s class %d: %s is not an enum", v, classUID, path))
			}
			continue
		}
		out = append(out, check(v, enum, newest)...)
	}
	return out
}

func sortedKeys(m map[int]string) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
