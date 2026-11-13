package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// fixtureOperation is the single operation fixture bindings name; the verb being
// replayed travels in params under ParamVerb.
const fixtureOperation = "replay"

// ParamVerb is the reserved params key carrying the verb name into an adapter.
// The resolver populates it (the spec exposes ${verb} as a template root, §3.3);
// the fixture adapter matches on it against each fixture's matches.verb.
const ParamVerb = "verb"

// fixtureFile is the on-disk fixture format (§9.1): OCSF-shaped events with the
// binding metadata needed to replay them.
type fixtureFile struct {
	FixtureMeta struct {
		Scenario string `json:"scenario"`
		Matches  struct {
			// Verb is the verb (or verbs) the fixture answers: one string or a
			// list, so one recorded event can answer two questions (a DNS
			// resolution under resolve_dns and under pivot_on_indicator)
			// without a second copy of the payload. Empty answers any verb.
			Verb   fixtureVerbs   `json:"verb"`
			Params map[string]any `json:"params"`
		} `json:"matches"`
		DelayMS int `json:"delay_ms"`
		// TimeScoped makes the fixture answer to the caller's time window: it
		// replays only when the event's time falls inside the window the
		// binding passed under the reserved `window.from` / `window.to` params
		// (absent/zero bounds are open). This is how a scenario carries
		// history — a baseline the question "is this normal for this account
		// over 30 days?" reaches, that a 24-hour query does not — so the same
		// scenario answers differently to different questions (01
		// §x-hypothesis: discriminating tests need fixtures that can
		// discriminate). Unscoped fixtures replay regardless of window, as
		// before.
		TimeScoped bool `json:"time_scoped"`
	} `json:"fixture_meta"`
	OCSF map[string]any `json:"ocsf"`
}

// fixtureVerbs is matches.verb: a single verb or a list of verbs in the JSON.
type fixtureVerbs []string

func (v *fixtureVerbs) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		if one == "" {
			*v = nil
		} else {
			*v = fixtureVerbs{one}
		}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("matches.verb: want a verb or a list of verbs: %w", err)
	}
	*v = many
	return nil
}

// contains reports whether verb is one the fixture answers; an empty list
// answers any verb.
func (v fixtureVerbs) contains(verb string) bool {
	if len(v) == 0 {
		return true
	}
	for _, x := range v {
		if x == verb {
			return true
		}
	}
	return false
}

// fixtureWindow reads the reserved window params a binding may pass
// (`window: {from, to}` rendered from the `${window.*}` roots, RFC 3339).
// Zero or unparseable bounds are open.
func fixtureWindow(params map[string]any) (from, to time.Time) {
	w, _ := params["window"].(map[string]any)
	if w == nil {
		return from, to
	}
	return toTime(w["from"]), toTime(w["to"])
}

// FixtureAdapter replays recorded OCSF events from fixtures/<scenario>/*.json.
// It is a regular Adapter with Class() == ClassFixture (§9.2); the resolver and
// normalizer treat it like any other. It loads its scenario lazily on first use
// and caches it, so it is safe for concurrent Invoke.
type FixtureAdapter struct {
	root     string // fixtures root directory
	scenario string // active scenario subdirectory

	// applyDelays honors each fixture's delay_ms to mimic real latency for UX
	// testing. Tests set it false to run instantly.
	applyDelays bool

	mu       sync.Mutex
	loaded   bool
	fixtures []fixtureFile
}

// NewFixtureAdapter constructs a fixture adapter for the given scenario under
// root (e.g. NewFixtureAdapter("fixtures", "lateral-movement-via-rdp")).
func NewFixtureAdapter(root, scenario string) *FixtureAdapter {
	return &FixtureAdapter{root: root, scenario: scenario, applyDelays: true}
}

// SetApplyDelays toggles honoring fixture delay_ms (default true). Tests disable
// it to avoid sleeping.
func (a *FixtureAdapter) SetApplyDelays(v bool) { a.applyDelays = v }

// Name returns "fixture".
func (a *FixtureAdapter) Name() string { return "fixture" }

// Class returns ClassFixture.
func (a *FixtureAdapter) Class() AdapterClass { return ClassFixture }

// SupportedOperations returns the single replay operation.
func (a *FixtureAdapter) SupportedOperations() []string { return []string{fixtureOperation} }

// sourceTool is the label stamped on replayed events, e.g.
// "fixture:lateral-movement-via-rdp".
func (a *FixtureAdapter) sourceTool() string { return "fixture:" + a.scenario }

// load reads and parses every fixture JSON in the scenario directory. Success
// is cached; a failure is NOT — the next call re-tests, per the §6.2 UNHEALTHY
// semantic ("unusable until re-tested"). Each attempt parses into a fresh slice
// so a retry can never double-append. Sidecar files (asset-criticality.json)
// and raw_response fixtures are skipped.
func (a *FixtureAdapter) load() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loaded {
		return nil
	}

	dir := filepath.Join(a.root, a.scenario)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read scenario dir %s: %w", dir, err)
	}

	var fixtures []fixtureFile
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		// Write-side action fixtures (*.action.json) share the scenario dir but
		// belong to the action FixtureWriteAdapter — never parse them as read
		// events. Without this, a write fixture with an empty matches.verb and a
		// "*" param wildcard matches every read verb and injects a phantom event.
		if strings.HasSuffix(name, ".action.json") {
			continue
		}
		if name == "asset-criticality.json" || strings.HasPrefix(name, "raw_response") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read fixture %s: %w", name, err)
		}
		var f fixtureFile
		if err := json.Unmarshal(data, &f); err != nil {
			return fmt.Errorf("parse fixture %s: %w", name, err)
		}
		fixtures = append(fixtures, f)
	}
	// Stable order so multi-event results are deterministic.
	sort.SliceStable(fixtures, func(i, j int) bool {
		return fixtureOrder(fixtures[i]) < fixtureOrder(fixtures[j])
	})

	a.fixtures = fixtures
	a.loaded = true
	return nil
}

// fixtureOrder keys fixtures by event time then verb for a stable, meaningful
// ordering (chronological within a verb).
func fixtureOrder(f fixtureFile) string {
	// A fixed-width UTC form sorts as the instants do.
	t := ocsfTime(f.OCSF, "time").Format("2006-01-02T15:04:05.000Z")
	return t + "|" + strings.Join(f.FixtureMeta.Matches.Verb, ",")
}

// Invoke replays the fixtures whose matches block satisfies the incoming verb
// and params, honoring each fixture's delay_ms. An unknown operation or a
// missing scenario is a classified AdapterError.
func (a *FixtureAdapter) Invoke(ctx context.Context, operation string, params map[string]any) (AdapterResponse, error) {
	if operation != fixtureOperation {
		return AdapterResponse{}, adapterErrorf(ErrFallthrough, "fixture adapter: unsupported operation %q", operation)
	}
	if err := a.load(); err != nil {
		return AdapterResponse{}, adapterErrorf(ErrUnhealthy, "fixture adapter: %v", err)
	}

	verb, _ := params[ParamVerb].(string)
	from, to := fixtureWindow(params)

	var events []OcsfPayload
	for _, f := range a.fixtures {
		if !fixtureMatches(f, verb, params) {
			continue
		}
		if f.FixtureMeta.TimeScoped {
			t := ocsfTime(f.OCSF, "time")
			if (!from.IsZero() && t.Before(from)) || (!to.IsZero() && t.After(to)) {
				continue
			}
		}
		if a.applyDelays && f.FixtureMeta.DelayMS > 0 {
			select {
			case <-ctx.Done():
				return AdapterResponse{}, adapterErrorf(ErrRetry, "fixture adapter: %v", ctx.Err())
			case <-time.After(time.Duration(f.FixtureMeta.DelayMS) * time.Millisecond):
			}
		}
		events = append(events, toOcsfPayload(f.OCSF))
	}
	return AdapterResponse{SourceTool: a.sourceTool(), Events: events}, nil
}

// Health reports healthy once the scenario has loaded without error.
func (a *FixtureAdapter) Health() HealthStatus {
	if err := a.load(); err != nil {
		return HealthStatus{Healthy: false, Message: err.Error()}
	}
	return HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("scenario %q, %d fixtures", a.scenario, len(a.fixtures)),
	}
}

// fixtureMatches reports whether a fixture applies to the incoming (verb,
// params): the verb must be among matches.verb, and every matches.param must equal
// the value at its dotted path in params. "*" matches anything; a list of
// values matches any one of them (§9.1) — how a fixture answers to
// `outcome: SUCCESS` and `outcome: ALL` without also answering to `FAILURE`,
// so a scenario can carry failed and successful logons that a query tells
// apart. A null in the list matches a param the caller did not pass, so a
// fixture can answer "this account, or no account asked for" and stay out of
// a query about another account.
func fixtureMatches(f fixtureFile, verb string, params map[string]any) bool {
	if !f.FixtureMeta.Matches.Verb.contains(verb) {
		return false
	}
	for path, want := range f.FixtureMeta.Matches.Params {
		if s, ok := want.(string); ok && s == "*" {
			continue
		}
		got, ok := lookupPath(params, path)
		alternatives, isList := want.([]any)
		if !ok {
			if isList && anyNil(alternatives) {
				continue
			}
			return false
		}
		if isList {
			if !anyValueEqual(got, alternatives) {
				return false
			}
			continue
		}
		if !valuesEqual(got, want) {
			return false
		}
	}
	return true
}

// anyNil reports whether the alternatives include null (matches an absent param).
func anyNil(alternatives []any) bool {
	for _, alt := range alternatives {
		if alt == nil {
			return true
		}
	}
	return false
}

// anyValueEqual reports whether got equals any of the alternatives.
func anyValueEqual(got any, alternatives []any) bool {
	for _, alt := range alternatives {
		if valuesEqual(got, alt) {
			return true
		}
	}
	return false
}

// lookupHook, when set, is told of every field read through lookupPath and
// fieldOf: the object read from, the path read within it, and the value found
// (nil when absent). normalizeHook, when set, is called as Registry.Normalize
// starts on an event, and the function it returns is called when Normalize
// returns. Both are nil outside tests. The package's tests set them to record
// which OCSF fields the normalizers read (adr/0356).
var (
	lookupHook    func(obj map[string]any, path string, found any)
	normalizeHook func(evt OcsfEvent) (done func())
)

// lookupPath resolves a dotted path ("target.hostname") into a nested params
// map, falling back to a direct flat key of the same name. Every read of an
// OCSF event goes through it or through fieldOf, so the tests can record it.
func lookupPath(params map[string]any, path string) (any, bool) {
	v, ok := walkPath(params, path)
	if lookupHook != nil {
		lookupHook(params, path, v)
	}
	return v, ok
}

// fieldOf reads one key of an object inside an OCSF event, such as an entry
// of a list. It is obj[key], recorded like a lookupPath read.
func fieldOf(obj map[string]any, key string) any {
	v := obj[key]
	if lookupHook != nil {
		lookupHook(obj, key, v)
	}
	return v
}

// walkPath is the walk behind lookupPath.
func walkPath(params map[string]any, path string) (any, bool) {
	if v, ok := params[path]; ok {
		return v, true
	}
	segs := strings.Split(path, ".")
	var cur any = params
	for _, s := range segs {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[s]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// valuesEqual compares a matcher value against an incoming value, tolerating the
// JSON number/string ambiguity by comparing canonical string forms.
func valuesEqual(got, want any) bool {
	return fmt.Sprintf("%v", got) == fmt.Sprintf("%v", want)
}

// toOcsfPayload extracts the OCSF header fields (class_uid, class_name, time)
// from a raw OCSF map and wraps the whole map as an OcsfPayload.
func toOcsfPayload(ocsf map[string]any) OcsfPayload {
	return OcsfPayload{
		ClassUID:  toInt(ocsf["class_uid"]),
		ClassName: toString(ocsf["class_name"]),
		Time:      ocsfTime(ocsf, "time"),
		Raw:       ocsf,
	}
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64: // JSON numbers decode as float64
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

// toTime parses a time a binding passes in its params, such as a window
// bound: an RFC 3339 string, or epoch milliseconds. Returns the zero time on
// failure. A time inside an OCSF event is read with ocsfTime.
func toTime(v any) time.Time {
	if s, ok := v.(string); ok {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			return parsed.UTC()
		}
		return time.Time{}
	}
	t, _ := epochMillis(v)
	return t
}

// ocsfTime reads an OCSF timestamp at path of obj (adr/0356). A timestamp_t
// field such as "time" or "process.created_time" holds epoch milliseconds as
// a whole number. OCSF's datetime profile adds a twin with the same name and
// "_dt", an RFC 3339 string. The number is read first and the twin as a
// fallback. A string in the number's field is not OCSF and is not read.
// Returns the zero time when neither gives a time.
func ocsfTime(obj map[string]any, path string) time.Time {
	if v, ok := lookupPath(obj, path); ok {
		if t, ok := epochMillis(v); ok {
			return t
		}
	}
	if v, ok := lookupPath(obj, path+"_dt"); ok {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(toString(v))); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

// epochMillis reads a whole number of milliseconds since the epoch as a
// time. A number with a fraction, or one too large for a JSON number to hold
// exactly (2^53 and up), is not a timestamp.
func epochMillis(v any) (time.Time, bool) {
	var ms int64
	switch n := v.(type) {
	case float64:
		if n != math.Trunc(n) || math.Abs(n) >= 1<<53 {
			return time.Time{}, false
		}
		ms = int64(n)
	case int:
		ms = int64(n)
	case int64:
		ms = n
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return time.Time{}, false
		}
		ms = i
	default:
		return time.Time{}, false
	}
	return time.UnixMilli(ms).UTC(), true
}
