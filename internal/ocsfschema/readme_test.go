package ocsfschema

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The provenance README names releases and URLs, never the day a file was
// downloaded: git records when a file was added.
func TestReadmeHasNoCalendarDate(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, Dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	date := regexp.MustCompile(`\b(19|20)\d{2}-\d{2}-\d{2}\b`)
	for i, line := range strings.Split(string(data), "\n") {
		if date.MatchString(line) {
			t.Errorf("README.md:%d has a calendar date: %s", i+1, line)
		}
	}
}
