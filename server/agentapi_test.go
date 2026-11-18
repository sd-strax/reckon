package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentAPI_WordsAreRedeclared (adr/0278): the engine re-declares the
// words internal/agentapi defines, and CheckWords reads only agentapi's
// blocks. So every constant in a re-declared block must name its agentapi
// constant. A literal word added there would be on the wire unchecked, and
// this fails on it.
func TestAgentAPI_WordsAreRedeclared(t *testing.T) {
	for _, c := range []struct {
		dir    string
		consts []string // the first constant of each re-declared block
	}{
		{"../aggregate", []string{
			"ActionStatusRequested", "TierT1", "AuthModeManual", "StatusDraft", "VerdictBenign",
			"ConfidenceHigh", "HypothesisProposed", "PredictionUntested", "TestKindTool",
		}},
		{"../capability", []string{"CoverageComplete"}},
		{"../action", []string{"ReversibilityReversible"}},
		{"../knowledge", []string{"CoverageComplete", "CoverageEmptyUnderFilter", "StatusDraft"}},
		{".", []string{"RequestPendingManual"}},
	} {
		for _, name := range c.consts {
			block := constBlockIn(t, c.dir, name)
			for _, spec := range block.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, v := range vs.Values {
					if sel, ok := v.(*ast.SelectorExpr); ok {
						if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "agentapi" {
							continue
						}
					}
					t.Errorf("%s: %s is not declared from internal/agentapi", c.dir, vs.Names[i].Name)
				}
			}
		}
	}
}

// constBlockIn returns the const declaration in the non-test Go files of dir
// that declares name.
func constBlockIn(t *testing.T, dir, name string) *ast.GenDecl {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				for _, n := range spec.(*ast.ValueSpec).Names {
					if n.Name == name {
						return gen
					}
				}
			}
		}
	}
	t.Fatalf("no const block in %s declares %s", dir, name)
	return nil
}
