package wirets

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// field is one key a struct sends, as encoding/json chooses it.
type field struct {
	name      string
	typ       reflect.Type
	index     []int
	tagged    bool
	omitEmpty bool
	omitZero  bool
	// viaPointer: the field is promoted from an embedded pointer, so it is
	// left out whenever that pointer is nil.
	viaPointer bool
}

// fields lists the keys t sends, in the order encoding/json writes them. It
// follows encoding/json's own rules: unexported fields and `json:"-"` are
// skipped, an embedded struct without a tag name is flattened into its
// parent, and of two fields with one name the shallower wins, then the
// tagged one, and a tie drops both.
func fields(t reflect.Type) ([]field, error) {
	type level struct {
		typ        reflect.Type
		index      []int
		viaPointer bool
	}
	var out []field
	next := []level{{typ: t}}
	var count, nextCount map[reflect.Type]int
	visited := map[reflect.Type]bool{}
	for len(next) > 0 {
		current := next
		next = nil
		count, nextCount = nextCount, map[reflect.Type]int{}
		for _, lv := range current {
			if visited[lv.typ] {
				continue
			}
			visited[lv.typ] = true
			for i := range lv.typ.NumField() {
				sf := lv.typ.Field(i)
				if sf.Anonymous {
					et := sf.Type
					if et.Kind() == reflect.Pointer {
						et = et.Elem()
					}
					if !sf.IsExported() && et.Kind() != reflect.Struct {
						continue
					}
				} else if !sf.IsExported() {
					continue
				}
				tag := sf.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name, opts, _ := strings.Cut(tag, ",")
				if !validTagName(name) {
					name = ""
				}
				index := append(slices.Clone(lv.index), i)
				ft := sf.Type
				if ft.Name() == "" && ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if name != "" || !sf.Anonymous || ft.Kind() != reflect.Struct {
					if hasOption(opts, "string") {
						return nil, fmt.Errorf("%s.%s: the `,string` option changes how a value is sent; drop it", t, sf.Name)
					}
					f := field{
						name:       name,
						typ:        sf.Type,
						index:      index,
						tagged:     name != "",
						omitEmpty:  hasOption(opts, "omitempty"),
						omitZero:   hasOption(opts, "omitzero"),
						viaPointer: lv.viaPointer,
					}
					if f.name == "" {
						f.name = sf.Name
					}
					out = append(out, f)
					if count[lv.typ] > 1 {
						// The same struct embedded twice at one depth: its
						// fields annihilate each other, as in encoding/json.
						out = append(out, f)
					}
					continue
				}
				nextCount[ft]++
				if nextCount[ft] == 1 {
					next = append(next, level{
						typ:        ft,
						index:      index,
						viaPointer: lv.viaPointer || sf.Type.Kind() == reflect.Pointer,
					})
				}
			}
		}
	}

	slices.SortStableFunc(out, func(a, b field) int {
		if c := strings.Compare(a.name, b.name); c != 0 {
			return c
		}
		if c := len(a.index) - len(b.index); c != 0 {
			return c
		}
		if a.tagged != b.tagged {
			if a.tagged {
				return -1
			}
			return 1
		}
		return slices.Compare(a.index, b.index)
	})
	var kept []field
	for i := 0; i < len(out); {
		j := i + 1
		for j < len(out) && out[j].name == out[i].name {
			j++
		}
		if f, ok := dominant(out[i:j]); ok {
			kept = append(kept, f)
		}
		i = j
	}
	slices.SortFunc(kept, func(a, b field) int { return slices.Compare(a.index, b.index) })
	return kept, nil
}

// dominant picks the field encoding/json sends of several with one name,
// sorted shallowest and tagged first. Two at the same depth with the same
// tagging hide each other, and none is sent.
func dominant(fs []field) (field, bool) {
	if len(fs) > 1 && len(fs[0].index) == len(fs[1].index) && fs[0].tagged == fs[1].tagged {
		return field{}, false
	}
	return fs[0], true
}

// hasOption reports whether a tag's comma-separated options name opt.
func hasOption(opts, opt string) bool {
	for o := range strings.SplitSeq(opts, ",") {
		if o == opt {
			return true
		}
	}
	return false
}

// validTagName is encoding/json's test of a tag name.
func validTagName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}
