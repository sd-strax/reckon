package ocsfschema

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Each schema file is one long line, so git must not try to diff it
// (adr/0356). The test asks git itself, so a pattern that stops matching
// fails here.
func TestSchemaFilesAreNotDiffed(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Skip("not a git checkout")
	}
	versions, err := Versions()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range versions {
		path := filepath.ToSlash(filepath.Join(Dir, v, "schema.json"))
		cmd := exec.Command(git, "check-attr", "diff", "--", path) // #nosec G204 -- a fixed command on a checked-in path
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git check-attr %s: %v", path, err)
		}
		if !strings.HasSuffix(strings.TrimSpace(string(out)), "diff: unset") {
			t.Errorf("%s is diffable (%s); .gitattributes must mark it -diff", path, strings.TrimSpace(string(out)))
		}
	}
}
