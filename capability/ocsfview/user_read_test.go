package ocsfview

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"regexp"
	"strconv"
	"testing"
)

// userField matches a path that reads a field of a user object: a user or
// owner object's name, ids, domain, full name or mail.
var userField = regexp.MustCompile(`(^|\.)(user|owner)\.(name|uid|uid_alt|domain|full_name|email_addr)$`)

// userReaders are the functions that read a user object, so the view shows
// and links every user alike (adr/0367, adr/0398).
var userReaders = map[string]bool{"userIn": true, "userOf": true, "cloudPrincipalIn": true, "cloudUserIn": true}

// TestUserObjectsReadByTheSharedReader (adr/0398): no template reads a user
// object's fields by hand. Each goes through userOf or cloudUserIn, which
// link it to the account the record holds for its place and list it in
// View.Users. The check is over the source: a string path naming a user
// field may appear only inside the readers.
func TestUserObjectsReadByTheSharedReader(t *testing.T) {
	fset := gotoken.NewFileSet()
	f, err := parser.ParseFile(fset, "ocsfview.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || userReaders[fn.Name.Name] {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != gotoken.STRING {
				return true
			}
			if v, err := strconv.Unquote(lit.Value); err == nil && userField.MatchString(v) {
				t.Errorf("%s: %s reads the user field %q by hand; read the user object through userOf or cloudUserIn", fset.Position(lit.Pos()), fn.Name.Name, v)
			}
			return true
		})
	}
}
