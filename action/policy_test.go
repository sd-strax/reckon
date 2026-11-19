package action

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestCanonicalPolicyCoversEveryField: setting any one Policy field changes the
// hash, so a field added to Policy cannot be left out of policy_version by
// accident, whether it is missing from canonicalPolicy or from canonical().
func TestCanonicalPolicyCoversEveryField(t *testing.T) {
	empty := mustHash(t, Policy{})
	typ := reflect.TypeOf(Policy{})
	for i := 0; i < typ.NumField(); i++ {
		var p Policy
		f := reflect.ValueOf(&p).Elem().Field(i)
		setNonZero(t, f)
		if mustHash(t, p) == empty {
			t.Errorf("setting Policy.%s left the hash unchanged: it does not reach the canonical form", typ.Field(i).Name)
		}
	}
}

// setNonZero gives a field of a kind Policy uses a non-zero value; a new kind
// fails the test until it is handled here.
func setNonZero(t *testing.T, f reflect.Value) {
	t.Helper()
	timeType := reflect.TypeOf(time.Time{})
	switch {
	case f.Kind() == reflect.String:
		f.SetString("x")
	case f.Kind() == reflect.Bool:
		f.SetBool(true)
	case f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.String:
		f.Set(reflect.Append(f, reflect.ValueOf("x").Convert(f.Type().Elem())))
	case f.Type() == timeType:
		f.Set(reflect.ValueOf(time.Unix(1, 0)))
	case f.Kind() == reflect.Pointer && f.Type().Elem() == timeType:
		at := time.Unix(1, 0)
		f.Set(reflect.ValueOf(&at))
	default:
		t.Fatalf("setNonZero: no case for %s", f.Type())
	}
}

func samplePolicy() Policy {
	until := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	return Policy{
		ID: "policy/auto-isolate/1.0.0", ActionMatch: []string{"host.isolate"},
		Predicate: "true", Effect: EffectAutoApprove, Shadow: true,
		SecondaryApproverPool: []string{"bob"}, Accountable: AccountableAuthor,
		AuthoredBy: "alice", SignedOffBy: []string{"secops-lead"},
		EffectiveFrom:  time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		EffectiveUntil: &until,
	}
}

func mustHash(t *testing.T, p Policy) string {
	t.Helper()
	h, err := p.ContentHash()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TestContentHash_Golden pins the canonical form: a change to it changes every
// recorded policy_version, so it is made on purpose or not at all.
func TestContentHash_Golden(t *testing.T) {
	const want = "61b0197c0e508e071b9c4accb7740ab5ad7efb11ee2f44cf2aa9aa5aba03adac"
	if got := mustHash(t, samplePolicy()); got != want {
		t.Errorf("ContentHash = %s; want %s", got, want)
	}
}

// TestContentHash_GoldenSparse pins the parts of the form a full policy does
// not reach: unset fields left out, the zero effective_from left out, false
// shadow left out, and "&&" in a predicate hashed as written. The baseline is
// such a policy.
func TestContentHash_GoldenSparse(t *testing.T) {
	const want = "d4e1fa2269731dcf2e4a4e593aef31525f2294af5c993066bc84b9fd1eafb062"
	if got := mustHash(t, BaselineDenyPolicy()); got != want {
		t.Errorf("ContentHash(baseline) = %s; want %s", got, want)
	}
}

// TestContentHash_FollowsThePolicy: each field changes the hash, and the same
// instant written in another zone does not.
func TestContentHash_FollowsThePolicy(t *testing.T) {
	base := mustHash(t, samplePolicy())
	edits := map[string]func(*Policy){
		"id":                      func(p *Policy) { p.ID = "policy/auto-isolate/1.0.1" },
		"action_match":            func(p *Policy) { p.ActionMatch = []string{"host.*"} },
		"predicate":               func(p *Policy) { p.Predicate = "false" },
		"effect":                  func(p *Policy) { p.Effect = EffectDeny },
		"shadow":                  func(p *Policy) { p.Shadow = false },
		"secondary_approver_pool": func(p *Policy) { p.SecondaryApproverPool = []string{"carol"} },
		"accountable":             func(p *Policy) { p.Accountable = AccountableRequester },
		"authored_by":             func(p *Policy) { p.AuthoredBy = "dave" },
		"signed_off_by":           func(p *Policy) { p.SignedOffBy = nil },
		"effective_from":          func(p *Policy) { p.EffectiveFrom = p.EffectiveFrom.Add(time.Second) },
		"effective_until":         func(p *Policy) { p.EffectiveUntil = nil },
	}
	for name, edit := range edits {
		p := samplePolicy()
		edit(&p)
		if mustHash(t, p) == base {
			t.Errorf("changing %s left the hash unchanged", name)
		}
	}

	// Expiring at the zero time is always expired; no expiry never is.
	never, expired := samplePolicy(), samplePolicy()
	never.EffectiveUntil, expired.EffectiveUntil = nil, &time.Time{}
	if mustHash(t, never) == mustHash(t, expired) {
		t.Error("an effective_until at the zero time hashes the same as none")
	}

	zoned := samplePolicy()
	zoned.EffectiveFrom = zoned.EffectiveFrom.In(time.FixedZone("UTC+5", 5*3600))
	if mustHash(t, zoned) != base {
		t.Error("the same effective_from in another zone changed the hash")
	}
}

// TestLoadPolicies_IDsNameTheFiles: a policy file with no id, or two files with
// one id, are refused with the files named.
func TestLoadPolicies_IDsNameTheFiles(t *testing.T) {
	write := func(dir, name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const deny = "action_match: [\"*\"]\neffect: DENY\npredicate: \"true\"\n"

	noID := t.TempDir()
	write(noID, "a.policy.yaml", deny)
	if _, err := LoadPolicies(noID); err == nil || !strings.Contains(err.Error(), "a.policy.yaml") {
		t.Errorf("no id: err = %v; want it refused, naming a.policy.yaml", err)
	}

	dup := t.TempDir()
	write(dup, "a.policy.yaml", "id: p/x/1\n"+deny)
	write(dup, "b.policy.yaml", "id: p/x/1\n"+deny)
	_, err := LoadPolicies(dup)
	if err == nil || !strings.Contains(err.Error(), "a.policy.yaml") || !strings.Contains(err.Error(), "b.policy.yaml") {
		t.Errorf("shared id: err = %v; want it refused, naming both files", err)
	}
}
