package action

import (
	"reflect"
	"testing"
	"time"
)

// TestCanonicalPolicyCoversEveryField: every Policy field is in the canonical
// form the content hash covers, so a field added to Policy is not left out of
// policy_version by accident.
func TestCanonicalPolicyCoversEveryField(t *testing.T) {
	canonical := reflect.TypeOf(canonicalPolicy{})
	policy := reflect.TypeOf(Policy{})
	for i := 0; i < policy.NumField(); i++ {
		name := policy.Field(i).Name
		if _, ok := canonical.FieldByName(name); !ok {
			t.Errorf("Policy.%s is not in canonicalPolicy: add it there and to Policy.canonical", name)
		}
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
	const want = "a2b3575f470cb6036f656e8e1517a04d3634de1d1d9e59a5c8379900fbb19e2b"
	if got := mustHash(t, samplePolicy()); got != want {
		t.Errorf("ContentHash = %s; want %s", got, want)
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

	zoned := samplePolicy()
	zoned.EffectiveFrom = zoned.EffectiveFrom.In(time.FixedZone("UTC+5", 5*3600))
	if mustHash(t, zoned) != base {
		t.Error("the same effective_from in another zone changed the hash")
	}
}
