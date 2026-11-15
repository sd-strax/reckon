package capability

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/stixschema"
	"github.com/sd-strax/reckon/stix"
)

// produced is every result Registry.Normalize returned while the package's
// tests ran. TestMain serializes each object in it after the tests, so every
// input a normalizer test feeds a normalizer is checked too (adr/0357).
var produced results

type results struct {
	mu  sync.Mutex
	all []NormalizationResult
	// resolvers is, for each result, the resolver that minted its ids.
	resolvers []*identity.Resolver
	// tests names, for each result, the test whose input produced it, so a
	// failure points at that input.
	tests []string
}

func (r *results) add(ids *identity.Resolver, res NormalizationResult) {
	test := callingTest()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.all = append(r.all, res)
	r.resolvers = append(r.resolvers, ids)
	r.tests = append(r.tests, test)
}

// callingTest is the name of the outermost Test function on the call stack,
// or "" when there is none.
func callingTest() string {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)])
	name := ""
	for {
		f, more := frames.Next()
		if i := strings.LastIndex(f.Function, ".Test"); i >= 0 {
			name, _, _ = strings.Cut(f.Function[i+1:], ".")
		}
		if !more {
			return name
		}
	}
}

// stixObjects is every object of a result as the serializer takes it, with a
// created time on the ones that carry one.
func stixObjects(res NormalizationResult) []stix.Object {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var out []stix.Object
	add := func(o stix.Object) {
		if stix.Timestamped(o.Type) {
			o.Created = at
		}
		out = append(out, o)
	}
	for _, o := range res.ObservedData {
		add(o.STIXObject())
	}
	for _, o := range res.SCOs {
		add(o.STIXObject())
	}
	for _, o := range res.Relationships {
		add(o.STIXObject())
	}
	for _, o := range res.Indicators {
		add(o.STIXObject())
	}
	for _, o := range res.Sightings {
		add(o.STIXObject())
	}
	for _, o := range res.Identities {
		add(o.STIXObject())
	}
	return out
}

// checkProduced serializes every produced object, checks it against the
// STIX 2.1 and reckon schemas (adr/0357 rule 8), and returns the process exit
// code. A property the serializer cannot place, or an object that is not
// valid STIX, fails the run.
func checkProduced(r *results) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	var failures []string
	var report stixschema.Report
	for i, res := range r.all {
		for _, o := range stixObjects(res) {
			raw, err := stix.Marshal(o)
			if err != nil {
				if !seen[err.Error()] {
					seen[err.Error()] = true
					failures = append(failures, err.Error())
				}
				continue
			}
			report.Add(sourceOf(o)+", input from "+r.tests[i], raw)
		}
		for _, p := range remintProblems(r.resolvers[i], res) {
			if !seen[p] {
				seen[p] = true
				failures = append(failures, "R1, input from "+r.tests[i]+": "+p)
			}
		}
	}
	if testing.Verbose() {
		fmt.Fprintf(os.Stderr, "normalizer output as STIX: checked %d objects: %v\n", report.Total(), report.Counts())
	}
	if len(failures) == 0 && !report.Failed() {
		return 0
	}
	fmt.Fprintln(os.Stderr, "--- FAIL: normalizer output as STIX (adr/0357)")
	for _, f := range failures {
		fmt.Fprintln(os.Stderr, "    "+f)
	}
	if report.Failed() {
		fmt.Fprintln(os.Stderr, "    objects that are not valid STIX 2.1, by the normalizer that wrote them:")
		for _, line := range strings.Split(strings.TrimRight(report.String(), "\n"), "\n") {
			fmt.Fprintln(os.Stderr, "    "+line)
		}
	}
	fmt.Fprintln(os.Stderr, "FAIL")
	return 1
}

// sourceOf names what produced an object: its normalizer and version, from
// its provenance.
func sourceOf(o stix.Object) string {
	if p, ok := o.Extensions[stix.ExtProvenance]; ok {
		return fmt.Sprintf("normalizer %v v%v", p["normalizer"], p["normalizer_version"])
	}
	return "no provenance (" + o.Type + ")"
}

// TestBundledFixturesSerialize runs every bundled fixture event through the
// registry, serializes what comes out, and checks each object against the
// STIX 2.1 and reckon schemas (adr/0357 rule 8). A failure is grouped by the
// fixture file and the normalizer.
func TestBundledFixturesSerialize(t *testing.T) {
	events := loadFixtureEvents(t)
	reg := testRegistry()
	var report stixschema.Report
	for name, evt := range events {
		res, err := reg.Normalize(evt)
		if err != nil {
			t.Errorf("%s: normalize: %v", name, err)
			continue
		}
		for _, o := range stixObjects(res) {
			raw, err := stix.Marshal(o)
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			report.Add(name+", "+sourceOf(o), raw)
		}
		for _, p := range remintProblems(reg.Identity(), res) {
			t.Errorf("%s: R1: %s", name, p)
		}
	}
	if report.Total() == 0 {
		t.Fatal("no object was produced")
	}
	if report.Failed() {
		t.Errorf("objects that are not valid STIX 2.1, by fixture and normalizer:\n%s", report.String())
	}
	t.Logf("checked %d objects: %v", report.Total(), report.Counts())
}

// TestNullObservationIsValidSTIX checks the null observation the resolver
// mints, with and without a window and parameters, as the evidence store
// writes it (created is its observed time).
func TestNullObservationIsValidSTIX(t *testing.T) {
	ids := identity.NewResolver(testNS)
	at := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	inputs := map[string]CallInput{
		"with window and parameters": {
			Entity: map[string]any{"host": map[string]any{"hostname": "h1"}},
			Window: TimeWindow{From: at.Add(-24 * time.Hour), To: at},
			Extra:  map[string]any{"filter": "change window"},
		},
		"bare": {Entity: map[string]any{"user": map[string]any{"name": "alice"}}},
	}
	var report stixschema.Report
	for name, in := range inputs {
		n := nullObservation(ids, "query_logs", in, []string{"stub:s"}, at)
		o := n.STIXObject()
		o.Created = stix.Truncate(n.ObservedAt)
		raw, err := stix.Marshal(o)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		report.Add("null observation, "+name, raw)
	}
	if report.Failed() {
		t.Errorf("not valid STIX 2.1:\n%s", report.String())
	}
}

func TestProvenanceIsAPropertyExtension(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	sco := SCO{
		ID:         identity.STIXID("process--" + uuid.NewString()),
		Type:       identity.TypeProcess,
		Properties: map[string]any{"pid": 42, "name": "cmd.exe", "command_line": "cmd /c whoami"},
		Provenance: Provenance{DerivationMode: DerivationDirect, Tool: "edr", Normalizer: "process_activity", NormalizerVersion: 4, ObservedAt: at},
	}
	got, err := stix.Build(sco.STIXObject())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["provenance"]; ok {
		t.Error("provenance is a top-level property")
	}
	prov := stix.ExtensionFrom(got, stix.ExtProvenance)
	if prov["tool"] != "edr" || prov["normalizer"] != "process_activity" || prov["normalizer_version"] != 4 ||
		prov["derivation_mode"] != "DIRECT" || prov["observed_at"] != "2026-01-02T03:04:05Z" || prov["extension_type"] != "property-extension" {
		t.Errorf("reckon-provenance = %v", prov)
	}
	if got["pid"] != 42 || got["command_line"] != "cmd /c whoami" {
		t.Errorf("native properties are not at the top level: %v", got)
	}
	if _, ok := got["name"]; ok || stix.ProcessName.String(got) != "cmd.exe" {
		t.Errorf("process name is not in reckon-process: %v", got)
	}
}

func TestObservedDataExtensionsAreReckonObservation(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	od := ObservedData{
		ID:             identity.STIXID("observed-data--" + uuid.NewString()),
		FirstObserved:  at,
		LastObserved:   at,
		NumberObserved: 1,
		ObjectRefs:     []identity.STIXID{"reckon-host--" + identity.STIXID(uuid.NewString())},
		Extensions:     map[string]any{"logon_type": "Interactive", "status": "Success", "labels": []any{"a"}},
		Provenance:     Provenance{DerivationMode: DerivationDirect, Tool: "edr", Normalizer: "authentication", NormalizerVersion: 5, ObservedAt: at},
	}
	o := od.STIXObject()
	o.Created = at
	got, err := stix.Build(o)
	if err != nil {
		t.Fatal(err)
	}
	obs := stix.ExtensionFrom(got, stix.ExtObservation)
	if obs["logon_type"] != "Interactive" || obs["status"] != "Success" || obs["labels"] == nil {
		t.Errorf("reckon-observation = %v", obs)
	}
	if _, ok := got["labels"]; ok {
		t.Error("an observation field reached the top level")
	}
	if got["first_observed"] != "2026-01-02T03:04:05Z" || got["number_observed"] != 1 || got["created"] != "2026-01-02T03:04:05.000Z" {
		t.Errorf("observed-data = %v", got)
	}
}

func TestVendorIdentityIsAnOrganization(t *testing.T) {
	v := VendorIdentity{ID: identity.STIXID("identity--" + uuid.NewString()), Name: "edr vendor"}
	o := v.STIXObject()
	o.Created = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got, err := stix.Build(o)
	if err != nil {
		t.Fatal(err)
	}
	if got["identity_class"] != "organization" || got["name"] != "edr vendor" || got["created"] == nil || got["modified"] == nil {
		t.Errorf("vendor identity = %v", got)
	}
}

// loadFixtureEvents reads every bundled fixture event, keyed by its file.
func loadFixtureEvents(t *testing.T) map[string]OcsfEvent {
	t.Helper()
	files, err := filepath.Glob("../fixtures/*/*.json")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]OcsfEvent{}
	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasSuffix(base, ".action.json") || nonEventFixtures[base] {
			continue
		}
		raw, err := os.ReadFile(f) // #nosec G304 -- a fixture path from Glob
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			OCSF map[string]any `json:"ocsf"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil || doc.OCSF == nil {
			t.Fatalf("%s: no ocsf event: %v", f, err)
		}
		evt := makeEvent(doc.OCSF)
		evt.SourceTool = "fixture:" + filepath.Base(filepath.Dir(f))
		out[filepath.Join(filepath.Base(filepath.Dir(f)), base)] = evt
	}
	if len(out) < 60 {
		t.Fatalf("read %d fixture events; want every scenario's", len(out))
	}
	return out
}
