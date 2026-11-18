package server

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sd-strax/reckon/knowledge"
)

// TestWireTS_EveryBodyIsRegistered fails on a response body the generator
// does not know (adr/0278). It type-checks this package and finds every value
// passed to writeJSON or to a json.Encoder's Encode. Each must be a named type
// the backend generator registers, or one of goOnlyBodies, so a shape written as a literal, or a
// struct nobody registered, cannot reach the workbench unchecked. The value
// writeJSON itself encodes is its parameter, which this test checks at each
// call instead.
func TestWireTS_EveryBodyIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, r := range backendGenerator().Decoded() {
		registered[r.PkgPath()+"."+r.Name()] = true
	}
	for _, r := range goOnlyBodies {
		registered[r.PkgPath()+"."+r.Name()] = true
	}

	fset := token.NewFileSet()
	files := parsePackage(t, fset)
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{Importer: exportImporter(t, fset)}
	if _, err := conf.Check("github.com/sd-strax/reckon/server", fset, files, info); err != nil {
		t.Fatal(err)
	}

	var bodies int
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			arg := writtenBody(call)
			if arg == nil {
				return true
			}
			typ := info.Types[arg].Type
			if isParamOfWriteJSON(f, call, typ) {
				return true
			}
			bodies++
			if name := namedBody(typ); !registered[name] {
				t.Errorf("%s: %s is written as a response body but the backend generator does not register it (adr/0278)",
					fset.Position(arg.Pos()), types.TypeString(typ, nil))
			}
			return true
		})
	}
	if bodies == 0 {
		t.Fatal("found no response bodies; the walk is broken")
	}
}

// goOnlyBodies are the response bodies no workbench call reads. Each is read
// by Go, or by no client in this tree, so it has no decoder or sample.
var goOnlyBodies = []reflect.Type{
	reflect.TypeFor[ExportResponse](),           // POST /api/investigations/{id}/export: no client in this tree calls it
	reflect.TypeFor[ReindexJob](),               // cmd/reckon: knowledge reindex
	reflect.TypeFor[knowledge.PriorResult](),    // agent/client.go: the prior_investigations tool
	reflect.TypeFor[SummaryNarrativeResponse](), // agent/client.go: posts the narrative and reads only the status
	reflect.TypeFor[SOPCreatedResponse](),       // POST /api/sops: no client in this tree calls it
	reflect.TypeFor[SOPImportResponse](),        // cmd/reckon: knowledge import
	reflect.TypeFor[DemoSeedResult](),           // cmd/reckon: demo seed
	reflect.TypeFor[DemoNotVirginResponse](),    // cmd/reckon: demo seed, its refusal
}

// writtenBody returns the value a call writes as a response body: the third
// argument of writeJSON, or the argument of an Encode on json.NewEncoder.
func writtenBody(call *ast.CallExpr) ast.Expr {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		if fn.Name == "writeJSON" && len(call.Args) == 3 {
			return call.Args[2]
		}
	case *ast.SelectorExpr:
		inner, ok := fn.X.(*ast.CallExpr)
		if fn.Sel.Name != "Encode" || !ok || len(call.Args) != 1 {
			return nil
		}
		if sel, ok := inner.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewEncoder" {
			return call.Args[0]
		}
	}
	return nil
}

// isParamOfWriteJSON reports whether the call is writeJSON's own Encode of
// its any-typed parameter.
func isParamOfWriteJSON(f *ast.File, call *ast.CallExpr, typ types.Type) bool {
	if _, ok := typ.(*types.Interface); !ok && typ.String() != "any" {
		return false
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if ok && fd.Name.Name == "writeJSON" && fd.Pos() <= call.Pos() && call.End() <= fd.End() {
			return true
		}
	}
	return false
}

// namedBody is a body type's package path and name, through one pointer, or
// "" when it has no name. An alias is read as the type it names, as reflect
// sees it.
func namedBody(typ types.Type) string {
	typ = types.Unalias(typ)
	if p, ok := typ.(*types.Pointer); ok {
		typ = types.Unalias(p.Elem())
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return ""
	}
	return named.Obj().Pkg().Path() + "." + named.Obj().Name()
}

// parsePackage parses this package's files, leaving out its tests.
func parsePackage(t *testing.T, fset *token.FileSet) []*ast.File {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return files
}

// exportImporter reads the package's imports from the export data go list
// builds, so the type check needs nothing beyond the standard library.
func exportImporter(t *testing.T, fset *token.FileSet) types.Importer {
	t.Helper()
	out, err := exec.Command("go", "list", "-export", "-deps", "-f", "{{.ImportPath}}\t{{.Export}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	exports := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		path, file, _ := strings.Cut(sc.Text(), "\t")
		if file != "" {
			exports[path] = file
		}
	}
	return importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		return os.Open(exports[path])
	})
}
