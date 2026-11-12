package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/internal/adapterplugin"
)

// queryRuleExemptions are in-tree query adapters not yet brought to the
// recorded-profile rule (11 §4.4), each with the reason. An exemption is a
// decision: each name must appear on an "Exempt: `<name>` …" line in the
// Decision section of adr/0334, and every such line must name an entry here
// (TestExemptionsAreRecorded). The list only shrinks: an entry whose adapter already satisfies
// the rule, or is no longer an in-tree query adapter, fails
// TestInTreeAdapters_SchemaSource.
var queryRuleExemptions = map[string]string{}

// exemptionADR is the decision record that must name every exemption.
const exemptionADR = "adr/0334-external-store-schema.md"

// describeTimeout bounds one adapter's initialize + describe.
const describeTimeout = 20 * time.Second

// builtDir is the shared adapter build directory, set once builtAdapters has
// created it; empty when no test asked for a build.
var builtDir string

// builtAdapters builds every in-tree adapter once for the package's tests.
var builtAdapters = sync.OnceValues(func() (string, error) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return "", err
	}
	dirs, err := filepath.Glob(filepath.Join(root, "cmd", "reckon-adapter-*"))
	if err != nil {
		return "", err
	}
	if len(dirs) == 0 {
		return "", errors.New("no in-tree adapters found under cmd/reckon-adapter-*")
	}
	bin, err := os.MkdirTemp("", "reckon-adapters-")
	if err != nil {
		return "", err
	}
	builtDir = bin
	args := []string{"build", "-o", bin + string(os.PathSeparator)}
	for _, d := range dirs {
		args = append(args, "./cmd/"+filepath.Base(d))
	}
	build := exec.Command("go", args...)
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		return "", errors.New("build adapters: " + err.Error() + "\n" + string(out))
	}
	return bin, nil
})

// TestMain removes the shared adapter build after the package's tests, if
// any test built it; it never triggers the build itself.
func TestMain(m *testing.M) {
	code := m.Run()
	if builtDir != "" {
		_ = os.RemoveAll(builtDir)
	}
	os.Exit(code)
}

// adapterBin is the shared build directory, failing the test if the build did.
func adapterBin(t *testing.T) string {
	t.Helper()
	bin, err := builtAdapters()
	if err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestValidateSchemaSource(t *testing.T) {
	profile := adapterplugin.SchemaProfile{Name: "p", Store: "s", Version: "1", Fields: []string{"f"}}
	cases := []struct {
		name string
		src  *adapterplugin.SchemaSource
		fail string // substring of the expected failure; "" = passes
	}{
		{"missing", nil, "no schema_source"},
		{"empty kind", &adapterplugin.SchemaSource{}, `kind ""`},
		{"unknown kind", &adapterplugin.SchemaSource{Kind: "lake"}, `kind "lake"`},
		{"response", &adapterplugin.SchemaSource{Kind: adapterplugin.SchemaSourceResponse}, ""},
		{"query without profiles", &adapterplugin.SchemaSource{Kind: adapterplugin.SchemaSourceQuery}, "lists no profile"},
		{"query", &adapterplugin.SchemaSource{Kind: adapterplugin.SchemaSourceQuery, Profiles: []adapterplugin.SchemaProfile{profile}}, ""},
		{"profile without name", querySource(func(p *adapterplugin.SchemaProfile) { p.Name = "" }), "lacks name"},
		{"profile without store", querySource(func(p *adapterplugin.SchemaProfile) { p.Store = "" }), "lacks store"},
		{"profile without version", querySource(func(p *adapterplugin.SchemaProfile) { p.Version = "" }), "lacks version"},
		{"profile without fields", querySource(func(p *adapterplugin.SchemaProfile) { p.Fields = nil }), "lacks fields"},
		{"profile name with a slash", querySource(func(p *adapterplugin.SchemaProfile) { p.Name = "a/b" }), "single path element"},
		{"profile name with a backslash", querySource(func(p *adapterplugin.SchemaProfile) { p.Name = `a\b` }), "single path element"},
		{"profile name dot", querySource(func(p *adapterplugin.SchemaProfile) { p.Name = "." }), "single path element"},
		{"profile name dot-dot", querySource(func(p *adapterplugin.SchemaProfile) { p.Name = ".." }), "single path element"},
		{"duplicate profile names", &adapterplugin.SchemaSource{Kind: adapterplugin.SchemaSourceQuery, Profiles: []adapterplugin.SchemaProfile{profile, profile}}, "duplicate"},
		{"two distinct profiles", &adapterplugin.SchemaSource{Kind: adapterplugin.SchemaSourceQuery, Profiles: []adapterplugin.SchemaProfile{profile, {Name: "q", Store: "s", Version: "1", Fields: []string{"f"}}}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := &confReport{}
			validateSchemaSource(&adapterplugin.DescribeResult{SchemaSource: tc.src}, rep)
			assertReport(t, rep, tc.fail)
		})
	}
}

func querySource(mutate func(*adapterplugin.SchemaProfile)) *adapterplugin.SchemaSource {
	p := adapterplugin.SchemaProfile{Name: "p", Store: "s", Version: "1", Fields: []string{"f"}}
	mutate(&p)
	return &adapterplugin.SchemaSource{Kind: adapterplugin.SchemaSourceQuery, Profiles: []adapterplugin.SchemaProfile{p}}
}

func TestValidateHealthDegraded(t *testing.T) {
	full := capability.DegradedCapability{Operation: "op", Capability: "c", Reason: "r"}
	cases := []struct {
		name      string
		degraded  []capability.DegradedCapability
		decodeErr error
		fail      string
	}{
		{"absent", nil, nil, ""},
		{"well-formed", []capability.DegradedCapability{full}, nil, ""},
		{"not a list", nil, errors.New("degraded is not a list"), "degraded is not a list"},
		{"no operation", []capability.DegradedCapability{{Capability: "c", Reason: "r"}}, nil, "lacks operation"},
		{"no capability", []capability.DegradedCapability{{Operation: "op", Reason: "r"}}, nil, "lacks capability"},
		{"no reason", []capability.DegradedCapability{full, {Operation: "op", Capability: "c"}}, nil, "entry 1 lacks reason"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := &confReport{}
			validateHealthDegraded(capability.HealthStatus{Healthy: true, Degraded: tc.degraded}, tc.decodeErr, rep)
			assertReport(t, rep, tc.fail)
		})
	}
}

func assertReport(t *testing.T, rep *confReport, fail string) {
	t.Helper()
	if fail == "" {
		if len(rep.failures) != 0 {
			t.Fatalf("failures = %q; want none", rep.failures)
		}
		return
	}
	if len(rep.failures) != 1 || !strings.Contains(rep.failures[0], fail) {
		t.Fatalf("failures = %q; want one containing %q", rep.failures, fail)
	}
}

// TestInTreeAdapters_SchemaSource is the repository test for 11 §4.4. It
// builds every in-tree adapter under cmd/reckon-adapter-*, reads each one's
// describe with adapterplugin.DescribeExecutable (initialize + describe;
// configure would need the vendor's credentials), and requires that the
// describe passes the conformance checks, and that a query adapter ships, for
// every profile it lists, cmd/reckon-adapter-<name>/testdata/profiles/<profile>/
// holding schema.json (valid JSON) and pages/ with at least one page, plus a
// live-check runbook at examples/adapters/<name>/LIVE.md, each as
// missingQueryArtifacts defines it — unless exempted, and an exemption must
// still be needed. That each exemption is recorded in adr/0334 is checked by
// TestExemptionsAreRecorded.
func TestInTreeAdapters_SchemaSource(t *testing.T) {
	root := repoRoot(t)
	bin := adapterBin(t)
	dirs, err := filepath.Glob(filepath.Join(root, "cmd", "reckon-adapter-*"))
	if err != nil {
		t.Fatal(err)
	}

	// queryMissing maps each in-tree query adapter to what its source lacks;
	// failed holds the adapters whose describe or conformance failed, which
	// no exemption check judges.
	queryMissing := map[string][]string{}
	failed := map[string]bool{}
	for _, d := range dirs {
		base := filepath.Base(d)
		name := strings.TrimPrefix(base, "reckon-adapter-")
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), describeTimeout)
			defer cancel()
			desc, err := adapterplugin.DescribeExecutable(ctx, []string{filepath.Join(bin, base)}, t.TempDir())
			if err != nil {
				failed[name] = true
				t.Fatalf("describe: %v", err)
			}
			// Recorded before any check can stop the subtest, so a failing
			// adapter is not also reported as a stale exemption.
			isQuery := desc.SchemaSource != nil && desc.SchemaSource.Kind == adapterplugin.SchemaSourceQuery
			if isQuery {
				queryMissing[name] = nil
			}
			rep := &confReport{}
			validateDescribe(desc, rep)
			validateSchemaSource(desc, rep)
			if len(rep.failures) > 0 {
				failed[name] = true
				t.Fatalf("conformance failures: %q", rep.failures)
			}
			if !isQuery {
				return
			}
			missing := missingQueryArtifacts(root, d, name, desc.SchemaSource.Profiles)
			queryMissing[name] = missing
			if reason, exempt := queryRuleExemptions[name]; exempt && len(missing) > 0 {
				t.Logf("exempt (%s); missing %s", reason, strings.Join(missing, ", "))
			}
		})
	}
	for _, p := range queryRuleProblems(queryMissing, failed, queryRuleExemptions) {
		t.Error(p)
	}
}

// queryRuleProblems applies the recorded-profile rule to the in-tree query
// adapters (name → what its source lacks): a non-exempt adapter must lack
// nothing; an exemption must name an in-tree query adapter and must still be
// needed. An adapter in failed (its describe or conformance failed, a failure
// of its own) is judged by none of these.
func queryRuleProblems(queryMissing map[string][]string, failed map[string]bool, exemptions map[string]string) []string {
	var out []string
	for name, missing := range queryMissing {
		if failed[name] {
			continue
		}
		reason, exempt := exemptions[name]
		switch {
		case exempt && len(missing) == 0:
			out = append(out, fmt.Sprintf("%s satisfies the recorded-profile rule; remove its exemption (%s)", name, reason))
		case !exempt && len(missing) > 0:
			out = append(out, fmt.Sprintf("query adapter %s lacks %s (11 §4.4)", name, strings.Join(missing, ", ")))
		}
	}
	for name, reason := range exemptions {
		if _, ok := queryMissing[name]; !ok && !failed[name] {
			out = append(out, fmt.Sprintf("exemption for %s (%s) names no in-tree query adapter; remove it", name, reason))
		}
	}
	sort.Strings(out)
	return out
}

// adrDecision is the Decision section of the exemption ADR.
func adrDecision(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, exemptionADR))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(data), "\n## Decision\n")
	if !ok {
		t.Fatalf("%s has no Decision section", exemptionADR)
	}
	decision, _, _ := strings.Cut(rest, "\n## ")
	return decision
}

func TestQueryRuleProblems(t *testing.T) {
	lacks := []string{"LIVE.md"}
	cases := []struct {
		name       string
		missing    map[string][]string
		failed     map[string]bool
		exemptions map[string]string
		want       string // substring of the one problem; "" = none
	}{
		{"exempt and still missing", map[string][]string{"old": lacks}, nil, map[string]string{"old": "r"}, ""},
		{"compliant, no exemption", map[string][]string{"new": nil}, nil, nil, ""},
		{"missing, not exempt", map[string][]string{"new": lacks}, nil, nil, "query adapter new lacks LIVE.md"},
		{"exemption no longer needed", map[string][]string{"old": nil}, nil, map[string]string{"old": "r"}, "remove its exemption"},
		{"exemption names no query adapter", map[string][]string{}, nil, map[string]string{"old": "r"}, "names no in-tree query adapter"},
		{"exempt adapter failed to describe", map[string][]string{}, map[string]bool{"old": true}, map[string]string{"old": "r"}, ""},
		{"exempt query adapter failed conformance", map[string][]string{"old": nil}, map[string]bool{"old": true}, map[string]string{"old": "r"}, ""},
		{"non-exempt query adapter failed conformance", map[string][]string{"new": nil}, map[string]bool{"new": true}, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := queryRuleProblems(tc.missing, tc.failed, tc.exemptions)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("problems = %q; want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], tc.want) {
				t.Fatalf("problems = %q; want one containing %q", got, tc.want)
			}
		})
	}
}

// exemptMarkerRE is a backticked name on an "Exempt: " line.
var exemptMarkerRE = regexp.MustCompile("`([^`]+)`")

// recordedExemptions are the names on the "Exempt: " lines of a Decision
// section.
func recordedExemptions(decision string) map[string]bool {
	out := map[string]bool{}
	for line := range strings.SplitSeq(decision, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Exempt: ")
		if !ok {
			continue
		}
		for _, m := range exemptMarkerRE.FindAllStringSubmatch(rest, -1) {
			out[m[1]] = true
		}
	}
	return out
}

// exemptionRecordProblems compares the exemption list with the ADR's
// "Exempt: " markers, both ways.
func exemptionRecordProblems(exemptions map[string]string, decision string) []string {
	recorded := recordedExemptions(decision)
	var out []string
	for name, reason := range exemptions {
		if !recorded[name] {
			out = append(out, fmt.Sprintf("exemption for %s (%s) has no \"Exempt: `%s`\" line in %s's Decision section; an exemption is a recorded decision", name, reason, name, exemptionADR))
		}
	}
	for name := range recorded {
		if _, ok := exemptions[name]; !ok {
			out = append(out, fmt.Sprintf("%s's Decision section exempts %s, which has no queryRuleExemptions entry", exemptionADR, name))
		}
	}
	sort.Strings(out)
	return out
}

// TestExemptionsAreRecorded: the exemption list and adr/0334's "Exempt: "
// markers agree. This is the one place the ADR is checked; it lives apart from
// TestInTreeAdapters_SchemaSource because it needs no adapter build.
func TestExemptionsAreRecorded(t *testing.T) {
	for _, p := range exemptionRecordProblems(queryRuleExemptions, adrDecision(t, repoRoot(t))) {
		t.Error(p)
	}
}

func TestExemptionRecordProblems(t *testing.T) {
	decision := "Some prose naming `mentioned` in passing.\nExempt: `old` (until later).\n  Exempt: `a`, `b` (two at once).\nNot Exempt: `fake`\n"
	cases := []struct {
		name       string
		exemptions map[string]string
		want       []string // substrings, one per expected problem
	}{
		{"all recorded", map[string]string{"old": "r", "a": "r", "b": "r"}, nil},
		{"backticked but no marker", map[string]string{"old": "r", "a": "r", "b": "r", "mentioned": "r"}, []string{"exemption for mentioned"}},
		{"marker without an entry", map[string]string{"old": "r", "a": "r"}, []string{"exempts b"}},
		{"not a marker line", map[string]string{"old": "r", "a": "r", "b": "r", "fake": "r"}, []string{"exemption for fake"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := exemptionRecordProblems(tc.exemptions, decision)
			if len(got) != len(tc.want) {
				t.Fatalf("problems = %q; want %d", got, len(tc.want))
			}
			for i, w := range tc.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("problem %d = %q; want it to contain %q", i, got[i], w)
				}
			}
		})
	}
}

// missingQueryArtifacts lists what a query adapter's source lacks, one entry
// per missing artifact (03 §9.1, 11 §4.4): for each described profile, a
// non-empty JSON testdata/profiles/<profile>/schema.json and a pages/
// directory holding at least one page; and the adapter's non-empty live-check
// runbook. Only regular files count, so a directory or symlink in their place
// is missing, as is a symlinked pages/; a dotfile in pages/ is not a page.
func missingQueryArtifacts(root, dir, name string, profiles []adapterplugin.SchemaProfile) []string {
	var missing []string
	for _, p := range profiles {
		rel := filepath.Join("testdata", "profiles", p.Name)
		at := filepath.Join("cmd", filepath.Base(dir), rel)
		schema := filepath.Join(dir, rel, "schema.json")
		if !isNonEmptyRegular(schema) {
			missing = append(missing, filepath.Join(at, "schema.json"))
		} else if b, err := os.ReadFile(schema); err != nil || !json.Valid(b) {
			missing = append(missing, filepath.Join(at, "schema.json")+" (not JSON)")
		}
		if !hasPage(filepath.Join(dir, rel, "pages")) {
			missing = append(missing, filepath.Join(at, "pages")+"/")
		}
	}
	live := filepath.Join("examples", "adapters", name, "LIVE.md")
	if !isNonEmptyRegular(filepath.Join(root, live)) {
		missing = append(missing, live)
	}
	return missing
}

// isNonEmptyRegular reports whether path is itself a regular file (not a
// symlink or directory) with content.
func isNonEmptyRegular(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// hasPage reports whether dir is a real directory (not a symlink) directly
// holding a page: a non-dot, regular, non-empty file.
func hasPage(dir string) bool {
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") && isNonEmptyRegular(filepath.Join(dir, e.Name())) {
			return true
		}
	}
	return false
}

func TestMissingQueryArtifacts(t *testing.T) {
	const (
		schema = "cmd/x/testdata/profiles/p/schema.json"
		pages  = "cmd/x/testdata/profiles/p/pages/"
		live   = "examples/adapters/x/LIVE.md"
		pSch   = "x/testdata/profiles/p/schema.json"
		pPage  = "x/testdata/profiles/p/pages/1.json"
		pPages = "x/testdata/profiles/p/pages"
		pLive  = "examples/adapters/x/LIVE.md"
	)
	complete := map[string]string{
		pSch:                                 "{}",
		pPage:                                "{}",
		"x/testdata/profiles/q/schema.json":  "{}",
		"x/testdata/profiles/q/pages/1.json": "{}",
		pLive:                                "run",
	}
	cases := []struct {
		name     string
		profiles []string    // nil = just p
		edit     [][2]string // {path, content} in order; "" deletes, "/" makes a directory, "empty" an empty file, "->t" a symlink to t
		want     []string
	}{
		{"complete", nil, nil, nil},
		{"no schema.json", nil, [][2]string{{pSch, ""}}, []string{schema}},
		{"empty schema.json", nil, [][2]string{{pSch, "empty"}}, []string{schema}},
		{"schema.json is a directory", nil, [][2]string{{pSch, "/"}}, []string{schema}},
		{"schema.json is a symlink", nil, [][2]string{{"x/elsewhere.json", "{}"}, {pSch, "->x/elsewhere.json"}}, []string{schema}},
		{"schema.json not JSON", nil, [][2]string{{pSch, "not json"}}, []string{schema + " (not JSON)"}},
		{"no pages/", nil, [][2]string{{pPage, ""}, {pPages, ""}}, []string{pages}},
		{"pages/ empty", nil, [][2]string{{pPage, ""}}, []string{pages}},
		{"pages/ holds an empty file", nil, [][2]string{{pPage, "empty"}}, []string{pages}},
		{"pages/ holds a subdirectory", nil, [][2]string{{pPage, ""}, {pPages + "/sub", "/"}}, []string{pages}},
		{"pages/ holds a dotfile", nil, [][2]string{{pPage, ""}, {pPages + "/.DS_Store", "junk"}}, []string{pages}},
		{"pages/ is a symlink", nil, [][2]string{{"x/real/1.json", "{}"}, {pPage, ""}, {pPages, ""}, {pPages, "->x/real"}}, []string{pages}},
		{"no LIVE.md", nil, [][2]string{{pLive, ""}}, []string{live}},
		{"empty LIVE.md", nil, [][2]string{{pLive, "empty"}}, []string{live}},
		{"LIVE.md is a directory", nil, [][2]string{{pLive, "/"}}, []string{live}},
		{"bare profile directory", nil, [][2]string{{pSch, ""}, {pPage, ""}, {"x/testdata/profiles/p/README.md", "notes"}}, []string{schema, pages}},
		{"only page is a symlink", nil, [][2]string{{"x/real.json", "{}"}, {pPage, "->x/real.json"}}, []string{pages}},
		{"page plus dotfile", nil, [][2]string{{pPages + "/.gitkeep", "keep"}}, nil},
		{"page plus junk", nil, [][2]string{{"x/testdata/profiles/p/README.md", "notes"}, {pPages + "/sub", "/"}}, nil},
		{"second profile incomplete", []string{"p", "q"}, [][2]string{{"x/testdata/profiles/q/schema.json", ""}}, []string{"cmd/x/testdata/profiles/q/schema.json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for path, content := range complete {
				writeTestFile(t, filepath.Join(root, path), content)
			}
			for _, e := range tc.edit {
				full, content := filepath.Join(root, e[0]), e[1]
				var err error
				switch {
				case content == "":
					err = os.Remove(full)
				case content == "/":
					_ = os.Remove(full)
					err = os.MkdirAll(full, 0o750)
				case content == "empty":
					writeTestFile(t, full, "")
				case strings.HasPrefix(content, "->"):
					_ = os.Remove(full)
					err = os.Symlink(filepath.Join(root, strings.TrimPrefix(content, "->")), full)
				default:
					writeTestFile(t, full, content)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			names := tc.profiles
			if names == nil {
				names = []string{"p"}
			}
			var profiles []adapterplugin.SchemaProfile
			for _, n := range names {
				profiles = append(profiles, adapterplugin.SchemaProfile{Name: n})
			}
			got := missingQueryArtifacts(root, filepath.Join(root, "x"), "x", profiles)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("missing = %q; want %q", got, tc.want)
			}
		})
	}
}

// writeTestFile writes content at path, creating its parent directories.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// repoRoot is the module root, two levels above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}
	return root
}

// TestAdapterTest_EchoEndToEnd runs the conformance verb against the echo
// adapter: it passes with no degradation and with a well-formed degraded
// health result, which the report lists.
func TestAdapterTest_EchoEndToEnd(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(adapterBin(t), "reckon-adapter-echo")
	manifest := "manifest_version: 1\nname: echo\nversion: 0.0.1\nprotocol_versions: [1]\nclass: CUSTOM\nexec: [\"" + bin + "\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"degraded": true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runAdapterTest([]string{dir}); err != nil {
		t.Errorf("echo conformance: %v", err)
	}
	if err := runAdapterTest([]string{dir, "--config", cfg}); err != nil {
		t.Errorf("echo conformance with degraded health: %v", err)
	}
	// A degraded value that is not a list: the engine drops it, conformance
	// fails it.
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"degraded_value": "none"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rep := &confReport{}
	if err := adapterTest([]string{dir, "--config", bad}, rep); err == nil {
		t.Error("echo conformance with a malformed degraded value passed; want FAIL")
	}
	if len(rep.failures) != 1 || !strings.Contains(rep.failures[0], "degraded is not a list") {
		t.Errorf("failures = %q; want exactly the degraded decode", rep.failures)
	}
}
