package evidence

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/lib/pq"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
)

func accountSCO(id identity.STIXID) capability.SCO {
	return capability.SCO{ID: id, Type: identity.TypeUserAccount, Properties: map[string]any{"account_type": identity.AccountWindowsDomain}}
}

func personSCO(id identity.STIXID, props map[string]any) capability.SCO {
	p := map[string]any{"identity_class": "individual"}
	for k, v := range props {
		p[k] = v
	}
	return capability.SCO{ID: id, Type: identity.TypeIdentity, Properties: p}
}

// observedRel is an edge a source's record produced.
func observedRel(typ string, src, tgt identity.STIXID) capability.Relationship {
	return capability.Relationship{ID: stixID("relationship"), Type: typ, SourceRef: src, TargetRef: tgt,
		Provenance: capability.Provenance{DerivationMode: capability.DerivationDirect, Tool: "dir"}}
}

// TestCut_RecycledLogin (adr/0324): a login aliased to two SIDs both owned by
// John; splitting the second SID from John cuts it and the login, and the
// first SID, which John owns by its own belongs-to, stays.
func TestCut_RecycledLogin(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	sid1, sid2, sam, john := stixID(identity.TypeUserAccount), stixID(identity.TypeUserAccount), stixID(identity.TypeUserAccount), stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{accountSCO(sid1), accountSCO(sid2), accountSCO(sam), personSCO(john, nil)},
		observedRel(capability.RelAliases, sam, sid1), observedRel(capability.RelAliases, sam, sid2),
		observedRel(capability.RelBelongsTo, sid1, john), observedRel(capability.RelBelongsTo, sid2, john))
	if err := s.AssertDistinct(ctx, testIDs, string(sid2), string(john), ana, "the login was recycled"); err != nil {
		t.Fatal(err)
	}
	set, err := s.PersonAccountSet(ctx, string(john))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(set, string(sid1)) || slices.Contains(set, string(sid2)) || slices.Contains(set, string(sam)) {
		t.Errorf("PersonAccountSet = %v; want SID1 in, SID2 and the login out", set)
	}
	pv, _ := s.PersonIdentity(ctx, string(john))
	for _, a := range pv.Accounts {
		if want := a.AccountRef == string(sid2); a.Cut != want {
			t.Errorf("account %s cut = %v; want %v", a.AccountRef, a.Cut, want)
		}
	}
	if got, _ := s.accountOwners(ctx, string(sam)); len(got) != 0 {
		t.Errorf("the login's owners = %v; want none", got)
	}
	if got, _ := s.accountOwners(ctx, string(sid1)); len(got) != 1 || got[0] != string(john) {
		t.Errorf("SID1's owners = %v; want John", got)
	}
}

// TestCut_AliasedPersons (adr/0324): a split from one of two aliased persons
// holds for the other: P2's view marks the account cut, the account has no
// owner, P2's reads leave it out, and an owner assertion over the split is
// refused.
func TestCut_AliasedPersons(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	acct, p1, p2 := stixID(identity.TypeUserAccount), stixID(identity.TypeIdentity), stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{accountSCO(acct), personSCO(p1, nil), personSCO(p2, nil)},
		observedRel(capability.RelAliases, p1, p2), observedRel(capability.RelBelongsTo, acct, p2))
	if err := s.AssertDistinct(ctx, testIDs, string(acct), string(p1), ana, "not theirs"); err != nil {
		t.Fatal(err)
	}
	pv, _ := s.PersonIdentity(ctx, string(p2))
	if len(pv.Accounts) != 1 || !pv.Accounts[0].Cut {
		t.Errorf("P2's accounts = %+v; want the account cut", pv.Accounts)
	}
	if got, _ := s.accountOwners(ctx, string(acct)); len(got) != 0 {
		t.Errorf("owners = %v; want none", got)
	}
	if set, _ := s.PersonAccountSet(ctx, string(p2)); slices.Contains(set, string(acct)) {
		t.Errorf("PersonAccountSet(P2) = %v; want the account out", set)
	}
	if v, _ := s.AccountIdentity(ctx, string(acct)); len(v.Owners) != 1 || !v.Owners[0].Cut {
		t.Errorf("account owners = %+v; want the one edge, cut", v.Owners)
	}
	for _, p := range []identity.STIXID{p1, p2} {
		if err := s.AssertOwner(ctx, testIDs, string(acct), string(p), ben, ""); !errors.Is(err, ErrSplitLive) {
			t.Errorf("owner %s over the split: %v; want ErrSplitLive", p, err)
		}
	}
}

// TestAssertAlias_AccountsOwnedApart: two accounts asserted to different
// persons are not joined; to one person, they are.
func TestAssertAlias_AccountsOwnedApart(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	x, y, z, p1, p2 := stixID(identity.TypeUserAccount), stixID(identity.TypeUserAccount), stixID(identity.TypeUserAccount), stixID(identity.TypeIdentity), stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{accountSCO(x), accountSCO(y), accountSCO(z), personSCO(p1, nil), personSCO(p2, nil)})
	for _, o := range [][2]identity.STIXID{{x, p1}, {y, p2}, {z, p1}} {
		if err := s.AssertOwner(ctx, testIDs, string(o[0]), string(o[1]), ana, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AssertAlias(ctx, testIDs, string(x), string(y), ben, ""); !errors.Is(err, ErrOwnersDiffer) || !errors.Is(err, ErrOwnerAsserted) {
		t.Errorf("joining accounts of two persons: %v; want ErrOwnersDiffer", err)
	}
	if err := s.AssertAlias(ctx, testIDs, string(x), string(z), ben, ""); err != nil {
		t.Errorf("joining two accounts of one person: %v", err)
	}
}

// TestAssertOwner_Concurrent: two owner assertions over one alias set, to
// different persons, run at once in their own transactions; the alias-set
// lock serializes them, so exactly one succeeds and the other is
// ErrOwnerAsserted. A true concurrency test, repeated for a few rounds.
func TestAssertOwner_Concurrent(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for round := 0; round < 5; round++ {
		sid, sam, p1, p2 := stixID(identity.TypeUserAccount), stixID(identity.TypeUserAccount), stixID(identity.TypeIdentity), stixID(identity.TypeIdentity)
		persistObjects(t, s, []capability.SCO{accountSCO(sid), accountSCO(sam), personSCO(p1, nil), personSCO(p2, nil)},
			observedRel(capability.RelAliases, sam, sid))
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, o := range [][2]identity.STIXID{{sid, p1}, {sam, p2}} {
			wg.Add(1)
			go func(i int, account, person identity.STIXID) {
				defer wg.Done()
				errs[i] = s.AssertOwner(ctx, testIDs, string(account), string(person), ana, "")
			}(i, o[0], o[1])
		}
		wg.Wait()
		ok, refused := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrOwnerAsserted):
				refused++
			default:
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if ok != 1 || refused != 1 {
			t.Errorf("round %d: %d succeeded, %d refused; want one each (%v)", round, ok, refused, errs)
		}
	}
}

// TestAssertOwner_JoinInTheWindow: an alias join that commits between an
// owner assertion's read of the alias set and its lock (simulated with the
// afterAliasRead seam: the join runs, to completion, in that window) is
// seen on the re-read after the lock, so the set does not end with two live
// owners.
func TestAssertOwner_JoinInTheWindow(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	x, y, p1, p2 := stixID(identity.TypeUserAccount), stixID(identity.TypeUserAccount), stixID(identity.TypeIdentity), stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{accountSCO(x), accountSCO(y), personSCO(p1, nil), personSCO(p2, nil)})
	if err := s.AssertOwner(ctx, testIDs, string(x), string(p1), ana, ""); err != nil {
		t.Fatal(err)
	}
	var joinErr error
	afterAliasRead = func() {
		afterAliasRead = nil // the join's own read runs no hook
		joinErr = s.AssertAlias(ctx, testIDs, string(x), string(y), ben, "same login")
	}
	t.Cleanup(func() { afterAliasRead = nil })
	err := s.AssertOwner(ctx, testIDs, string(y), string(p2), ana, "")
	if joinErr != nil {
		t.Fatalf("the join in the window: %v", joinErr)
	}
	if !errors.Is(err, ErrOwnerAsserted) {
		t.Errorf("owner assertion over the joined set: %v; want ErrOwnerAsserted", err)
	}
	if got, _ := s.AliasSet(ctx, string(y)); !slices.Contains(got, string(x)) {
		t.Errorf("the join did not land: %v", got)
	}
}

// TestIdentityActs_DeadlockRetry: a deadlock (SQLSTATE 40P01, injected on the
// first attempt through the injectAttemptError seam) is retried once, and the
// second attempt writes or answers its typed refusal; two deadlocks are
// returned as one. A non-deadlock error is never retried.
func TestIdentityActs_DeadlockRetry(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	x, y, p1, p2 := stixID(identity.TypeUserAccount), stixID(identity.TypeUserAccount), stixID(identity.TypeIdentity), stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{accountSCO(x), accountSCO(y), personSCO(p1, nil), personSCO(p2, nil)})
	t.Cleanup(func() { injectAttemptError = nil })
	deadlock := &pq.Error{Code: "40P01"}
	var attempts []int
	injectAttemptError = func(attempt int) error {
		attempts = append(attempts, attempt)
		if attempt == 0 {
			return deadlock
		}
		return nil
	}
	if err := s.AssertOwner(ctx, testIDs, string(x), string(p1), ana, ""); err != nil {
		t.Errorf("owner after a deadlock: %v", err)
	}
	if err := s.AssertOwner(ctx, testIDs, string(x), string(p2), ana, ""); !errors.Is(err, ErrOwnerAsserted) {
		t.Errorf("second owner after a deadlock: %v; want the typed refusal", err)
	}
	if err := s.AssertAlias(ctx, testIDs, string(x), string(y), ana, ""); err != nil {
		t.Errorf("join after a deadlock: %v", err)
	}
	if len(attempts) != 6 {
		t.Errorf("attempts = %v; want two per act", attempts)
	}
	injectAttemptError = func(int) error { return deadlock }
	if err := s.AssertDistinct(ctx, testIDs, string(p1), string(p2), ana, ""); !IsDeadlock(err) {
		t.Errorf("two deadlocks: %v; want the deadlock", err)
	}
	attempts = nil
	injectAttemptError = func(attempt int) error { attempts = append(attempts, attempt); return errors.New("other") }
	if err := s.AssertDistinct(ctx, testIDs, string(p1), string(p2), ana, ""); err == nil || len(attempts) != 1 {
		t.Errorf("a non-deadlock error: %v after %v attempts; want no retry", err, attempts)
	}
	if IsDeadlock(&pq.Error{Code: "23505"}) || IsDeadlock(nil) || !IsDeadlock(fmt.Errorf("wrapped: %w", deadlock)) {
		t.Error("IsDeadlock misreads SQLSTATE")
	}
}

// TestFindPersons_Representative: an aliased pair answers its least ref,
// whichever member the token names.
func TestFindPersons_Representative(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	p1, p2 := stixID(identity.TypeIdentity), stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{
		personSCO(p1, map[string]any{"mail": "jo@contoso.example"}),
		personSCO(p2, map[string]any{"directory_id": "W-1001"}),
	}, observedRel(capability.RelAliases, p1, p2))
	want := min(string(p1), string(p2))
	for _, token := range []string{"jo@contoso.example", "w-1001"} {
		if got, err := s.FindPersons(ctx, token); err != nil || len(got) != 1 || got[0] != want {
			t.Errorf("FindPersons(%q) = %v, %v; want [%s]", token, got, err, want)
		}
	}
}
