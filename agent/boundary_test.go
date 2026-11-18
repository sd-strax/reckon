package agent

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// enginePackages are the packages the agent must never depend on (adr/0385):
// it reaches the engine only through the HTTP API, where the AI-delegate token
// and the engine's gates apply.
var enginePackages = []string{
	"github.com/sd-strax/reckon/aggregate",
	"github.com/sd-strax/reckon/server",
	"github.com/sd-strax/reckon/action",
	"github.com/sd-strax/reckon/capability",
	"github.com/sd-strax/reckon/temporal",
	"github.com/sd-strax/reckon/knowledge",
}

// goListDeps returns the full, non-test dependency list of the package in dir.
func goListDeps(t *testing.T, dir string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("go list in %s: %v\n%s", dir, err, exit.Stderr)
		}
		t.Fatalf("go list in %s: %v", dir, err)
	}
	return strings.Fields(string(out))
}

// TestAgent_DoesNotLinkTheEngine (adr/0385): the agent may use shared
// packages, but no engine package and nothing under one, directly or through
// another package.
func TestAgent_DoesNotLinkTheEngine(t *testing.T) {
	for _, dep := range goListDeps(t, ".") {
		for _, engine := range enginePackages {
			if dep == engine || strings.HasPrefix(dep, engine+"/") {
				t.Errorf("agent depends on %s", dep)
			}
		}
	}
}

// TestCatalog_ImportsNoReckonPackage (adr/0385): internal/catalog is shared by
// the backend and the agent, so it depends on no other reckon package.
func TestCatalog_ImportsNoReckonPackage(t *testing.T) {
	const self = "github.com/sd-strax/reckon/internal/catalog"
	for _, dep := range goListDeps(t, "../internal/catalog") {
		if strings.HasPrefix(dep, "github.com/sd-strax/reckon") && dep != self {
			t.Errorf("internal/catalog depends on %s", dep)
		}
	}
}

// TestAgentAPI_ImportsNoEnginePackage (adr/0278): internal/agentapi holds the
// shapes the backend serves and the agent decodes, so it depends on no engine
// package. The reckon packages it may use are leaves, such as
// internal/catalog: each depends on no other reckon package.
func TestAgentAPI_ImportsNoEnginePackage(t *testing.T) {
	const self = "github.com/sd-strax/reckon/internal/agentapi"
	for _, dep := range goListDeps(t, "../internal/agentapi") {
		if !strings.HasPrefix(dep, "github.com/sd-strax/reckon/") || dep == self {
			continue
		}
		for _, engine := range enginePackages {
			if dep == engine || strings.HasPrefix(dep, engine+"/") {
				t.Errorf("internal/agentapi depends on %s", dep)
			}
		}
		dir := "../" + strings.TrimPrefix(dep, "github.com/sd-strax/reckon/")
		for _, inner := range goListDeps(t, dir) {
			if strings.HasPrefix(inner, "github.com/sd-strax/reckon/") && inner != dep {
				t.Errorf("internal/agentapi depends on %s, which is not a leaf: it depends on %s", dep, inner)
			}
		}
	}
}
