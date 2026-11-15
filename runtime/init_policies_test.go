package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sd-strax/reckon/action"
	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/config"
)

// TestInit_SeedsInstallPolicies: a fresh init writes the shipped Gate 2
// policies beside the config and points capability.policy_dir at them
// (adr/0374).
func TestInit_SeedsInstallPolicies(t *testing.T) {
	path := withConfigEnv(t)

	res, err := Init(promptStub())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	want := filepath.Join(filepath.Dir(path), "policies")
	if res.PolicyDir != want {
		t.Errorf("PolicyDir = %q; want %q", res.PolicyDir, want)
	}
	if len(res.PoliciesSeeded) == 0 {
		t.Fatal("fresh init seeded no policies")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Capability.PolicyDir != want {
		t.Errorf("config policy_dir = %q; want %q", cfg.Capability.PolicyDir, want)
	}
}

// TestInit_ExistingInstallWithoutPolicyDir: re-running init on an install that
// predates the shipped policies seeds them and sets policy_dir; a later re-run
// writes nothing.
func TestInit_ExistingInstallWithoutPolicyDir(t *testing.T) {
	path := withConfigEnv(t)
	if _, err := Init(promptStub()); err != nil {
		t.Fatal(err)
	}
	// Turn it into an install from before this change.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Capability.PolicyDir = ""
	if err := config.Overwrite(cfg, path); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(filepath.Dir(path), "policies")); err != nil {
		t.Fatal(err)
	}

	res, err := Init(promptStub())
	if err != nil {
		t.Fatalf("re-init: %v", err)
	}
	if !res.AlreadyExisted || len(res.PoliciesSeeded) == 0 || res.PolicyDir == "" {
		t.Fatalf("re-init = %+v; want the policies seeded on the existing install", res)
	}
	if cfg, _ = config.Load(); cfg.Capability.PolicyDir != res.PolicyDir {
		t.Errorf("config policy_dir = %q; want %q", cfg.Capability.PolicyDir, res.PolicyDir)
	}

	again, err := Init(promptStub())
	if err != nil {
		t.Fatal(err)
	}
	if len(again.PoliciesSeeded) != 0 {
		t.Errorf("third init seeded %v; want nothing", again.PoliciesSeeded)
	}
}

// TestInit_LeavesOperatorPolicyDirAlone: an install whose policy_dir is set is
// the operator's; init neither changes the setting nor writes into it, and it
// never overwrites a policy file that exists.
func TestInit_LeavesOperatorPolicyDirAlone(t *testing.T) {
	path := withConfigEnv(t)
	if _, err := Init(promptStub()); err != nil {
		t.Fatal(err)
	}
	own := t.TempDir()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Capability.PolicyDir = own
	if err := config.Overwrite(cfg, path); err != nil {
		t.Fatal(err)
	}

	res, err := Init(promptStub())
	if err != nil {
		t.Fatal(err)
	}
	if res.PolicyDir != own || len(res.PoliciesSeeded) != 0 {
		t.Errorf("re-init = dir %q seeded %v; want %q untouched", res.PolicyDir, res.PoliciesSeeded, own)
	}
	if entries, _ := os.ReadDir(own); len(entries) != 0 {
		t.Errorf("init wrote into the operator's policy dir: %v", entries)
	}

	// An edited shipped file survives seeding.
	dir := t.TempDir()
	edited := filepath.Join(dir, "ticket-own-auto-approve.policy.yaml")
	if err := os.WriteFile(edited, []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := seedInstallPolicies(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(edited); string(b) != "edited" {
		t.Errorf("seeding overwrote an existing policy file: %q", b)
	}
}

// TestInstallPolicy_OwnTicketing: the shipped policy loads and compiles, and
// approves a person's own T2 ticketing credited to that person — never the
// agent's, never a T3, never a non-ticket action.
func TestInstallPolicy_OwnTicketing(t *testing.T) {
	withConfigEnv(t)
	res, err := Init(promptStub())
	if err != nil {
		t.Fatal(err)
	}
	policies, err := action.LoadPolicies(res.PolicyDir)
	if err != nil {
		t.Fatalf("LoadPolicies: %v", err)
	}
	g, err := action.NewGate2(policies)
	if err != nil {
		t.Fatalf("NewGate2: %v", err)
	}
	in := func(actionType, tier, kind string) action.EvalInput {
		return action.EvalInput{
			ActionType: actionType, Tier: tier,
			Targets:         []aggregate.TargetSpec{{EntityRef: "x", ResolvedIdentifier: "SEC"}},
			RequestedByKind: kind, RequestedByID: "alice-sub", Now: time.Now(),
		}
	}

	for _, typ := range []string{"ticket.create", "ticket.comment", "ticket.transition", "ticket.close"} {
		d, err := g.Evaluate(in(typ, aggregate.TierT2, aggregate.ActorHuman))
		if err != nil {
			t.Fatal(err)
		}
		if !d.AutoApproves() || d.PolicyAccountable != "alice-sub" {
			t.Errorf("%s by a person: mode=%q accountable=%q; want AUTO_POLICY credited to alice-sub", typ, d.Mode, d.PolicyAccountable)
		}
	}
	for name, input := range map[string]action.EvalInput{
		"agent ticket":   in("ticket.create", aggregate.TierT2, aggregate.ActorAIDelegated),
		"escalated T3":   in("ticket.create", aggregate.TierT3, aggregate.ActorHuman),
		"non-ticket act": in("host.isolate", aggregate.TierT2, aggregate.ActorHuman),
	} {
		d, err := g.Evaluate(input)
		if err != nil {
			t.Fatal(err)
		}
		if d.AutoApproves() {
			t.Errorf("%s auto-approved by %s; want manual", name, d.MatchedPolicyRef)
		}
	}
}
