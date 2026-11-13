package ocsfschema

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// NumberTables lists the package-level variables in dir's Go files (tests
// left out) that are a map literal with whole-number keys or values:
// map[int]string, map[string]int and the like. That is the shape of an OCSF
// enum written into the code (adr/0356).
//
// A package that reads OCSF uses it to check that a test covers each such
// table: the test lists the tables it checks against the schema and the ones
// that are not OCSF enums, and fails on a table in neither list.
func NumberTables(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("ocsfschema: %w", err)
	}
	fset := token.NewFileSet()
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("ocsfschema: %w", err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, id := range vs.Names {
					typ := vs.Type
					if typ == nil && i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.CompositeLit); ok {
							typ = lit.Type
						}
					}
					if m, ok := typ.(*ast.MapType); ok && (isWholeNumber(m.Key) || isWholeNumber(m.Value)) {
						out = append(out, id.Name)
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func isWholeNumber(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	switch id.Name {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return true
	}
	return false
}

// UncoveredTables returns the number tables in dir that are in neither list:
// checked, the tables a test checks against the schema, and exempt, the ones
// that are not OCSF enums. It also returns the names in either list that are
// no longer a table in dir, so the lists cannot go stale.
func UncoveredTables(dir string, checked, exempt []string) (uncovered, stale []string, err error) {
	tables, err := NumberTables(dir)
	if err != nil {
		return nil, nil, err
	}
	listed := map[string]bool{}
	for _, n := range append(append([]string(nil), checked...), exempt...) {
		listed[n] = true
	}
	found := map[string]bool{}
	for _, n := range tables {
		found[n] = true
		if !listed[n] {
			uncovered = append(uncovered, n)
		}
	}
	for n := range listed {
		if !found[n] {
			stale = append(stale, n)
		}
	}
	sort.Strings(stale)
	return uncovered, stale, nil
}
