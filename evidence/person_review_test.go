package evidence

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
)

// persistNormalized normalizes events through the real registry and
// persists them as one call of an investigation.
func persistNormalized(t *testing.T, s *Store, inv uuid.UUID, evts ...capability.OcsfEvent) {
	t.Helper()
	reg := capability.NewRegistry(testIDs)
	res := capability.CapabilityResult{Coverage: capability.CoverageComplete, Events: evts}
	for _, e := range evts {
		nr, err := reg.Normalize(e)
		if err != nil {
			t.Fatal(err)
		}
		nr.EventID = e.ID
		res.Normalized = append(res.Normalized, nr)
	}
	persistCall(t, s, Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &inv, Verb: "v", StartedAt: time.Now()}, res)
}

func ocsfEvent(tool string, class, minute int, payload map[string]any) capability.OcsfEvent {
	payload["class_uid"] = class
	at := time.Date(2026, 1, 2, 0, minute, 0, 0, time.UTC)
	return capability.OcsfEvent{ID: uuid.Must(uuid.NewV7()), ClassUID: class, ClassName: "c", Time: at, RecordedAt: at, SourceTool: tool, Payload: payload}
}

// TestInventoryBeforeLogon: a directory record that names an on-premises SID
// persists the SID account with the SID as its user_id, so the logon that
// follows lands on the same account, a lookup by the SID finds it, and its
// seen-as includes the logon.
func TestInventoryBeforeLogon(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	const sid = "S-1-5-21-7-1101"
	inv := uuid.New()
	persistNormalized(t, s, inv, ocsfEvent("idp", 5003, 1, map[string]any{
		"metadata": map[string]any{"product": map[string]any{"vendor_name": "fixture-idp"}},
		"user":     map[string]any{"uid": "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c", "uid_alt": sid, "name": "jdoe@contoso.example"},
	}))
	persistNormalized(t, s, inv, ocsfEvent("dc", 3002, 2, map[string]any{
		"actor":        map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": sid}},
		"dst_endpoint": map[string]any{"hostname": "DC1", "os": map[string]any{"type_id": 100}},
	}))
	acct := string(testIDs.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: sid}))
	got, err := s.FindAccounts(ctx, sid, 10)
	if err != nil || len(got) != 1 || got[0].Ref != acct || got[0].UserID != sid {
		t.Fatalf("FindAccounts(SID) = %+v, %v; want the one SID account %s", got, err, acct)
	}
	v, err := s.AccountIdentity(ctx, acct)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(v.SeenAs.IDs, SourceID{Tool: "dc", UID: sid}) || !slices.Contains(v.SeenAs.Logins, `CONTOSO\jdoe`) {
		t.Errorf("seen as = %+v; want the logon", v.SeenAs)
	}
}

// TestOwnersSeparated (adr/0324): one live asserted owner per account alias
// set holds when a person set separates. While P1 and P2 are one person an
// account may be asserted to both; splitting them, or withdrawing their
// join, is refused until one owner is withdrawn.
func TestOwnersSeparated(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	setup := func() (acct, p1, p2 identity.STIXID, join, owner2 uuid.UUID) {
		acct, p1, p2 = stixID(identity.TypeUserAccount), stixID(identity.TypeIdentity), stixID(identity.TypeIdentity)
		persistObjects(t, s, []capability.SCO{accountSCO(acct), personSCO(p1, nil), personSCO(p2, nil)})
		if err := s.AssertAlias(ctx, testIDs, string(p1), string(p2), ana, "one human"); err != nil {
			t.Fatal(err)
		}
		for _, p := range []identity.STIXID{p1, p2} {
			if err := s.AssertOwner(ctx, testIDs, string(acct), string(p), ana, ""); err != nil {
				t.Fatalf("owner %s while aliased: %v", p, err)
			}
		}
		join = edgeUUID(pairEdge(relAliases, p1, p2))
		owner2 = edgeUUID(testIDs.Relationship(relBelongsTo+assertedNamespace, acct, p2))
		return acct, p1, p2, join, owner2
	}

	_, p1, p2, _, owner2 := setup()
	if err := s.AssertDistinct(ctx, testIDs, string(p1), string(p2), ben, "two people"); !errors.Is(err, ErrOwnersSeparated) || !errors.Is(err, ErrOwnerAsserted) {
		t.Errorf("split over two owners: %v; want ErrOwnersSeparated", err)
	}
	if _, _, err := s.WithdrawAssertion(ctx, owner2, ben, "the wrong one"); err != nil {
		t.Fatal(err)
	}
	if err := s.AssertDistinct(ctx, testIDs, string(p1), string(p2), ben, "two people"); err != nil {
		t.Errorf("split after one owner withdrawn: %v", err)
	}

	_, _, _, join, owner2 := setup()
	if _, _, err := s.WithdrawAssertion(ctx, join, ben, "not one human"); !errors.Is(err, ErrOwnersSeparated) {
		t.Errorf("withdrawing the join over two owners: %v; want ErrOwnersSeparated", err)
	}
	if _, _, err := s.WithdrawAssertion(ctx, owner2, ben, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.WithdrawAssertion(ctx, join, ben, "not one human"); err != nil {
		t.Errorf("withdrawing the join after one owner withdrawn: %v", err)
	}
}

// TestOwnersSeparated_OwnerInTheWindow: an owner assertion that commits
// after the withdrawal read the person set but before it locked the persons
// (simulated with the beforeSeparationLock seam: the assertion runs, to
// completion, in that window) is seen, since the owners are read after the
// persons are locked: the withdrawal of the join is refused.
func TestOwnersSeparated_OwnerInTheWindow(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	acct, p1, p2 := stixID(identity.TypeUserAccount), stixID(identity.TypeIdentity), stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{accountSCO(acct), personSCO(p1, nil), personSCO(p2, nil)})
	if err := s.AssertAlias(ctx, testIDs, string(p1), string(p2), ana, "one human"); err != nil {
		t.Fatal(err)
	}
	if err := s.AssertOwner(ctx, testIDs, string(acct), string(p1), ana, ""); err != nil {
		t.Fatal(err)
	}
	var ownerErr error
	beforeSeparationLock = func() {
		beforeSeparationLock = nil
		ownerErr = s.AssertOwner(ctx, testIDs, string(acct), string(p2), ben, "")
	}
	t.Cleanup(func() { beforeSeparationLock = nil })
	_, _, err := s.WithdrawAssertion(ctx, edgeUUID(pairEdge(relAliases, p1, p2)), ben, "not one human")
	if ownerErr != nil {
		t.Fatalf("the owner in the window: %v", ownerErr)
	}
	if !errors.Is(err, ErrOwnersSeparated) {
		t.Errorf("withdrawal with an owner landed in the window: %v; want ErrOwnersSeparated", err)
	}
}

// TestOwnersSeparated_JoinAndOwnerInTheWindow: a join P2~P3 and an owner
// assertion A→P3, both committed after the withdrawal of P1~P2 first read
// the person set and before it locked it (the beforeSeparationLock seam),
// are seen: the set is read again under the lock, P3 joins it, and A, owned
// by P1 and P3, would land in two persons, so the withdrawal is refused.
func TestOwnersSeparated_JoinAndOwnerInTheWindow(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	acct, p1, p2, p3 := stixID(identity.TypeUserAccount), stixID(identity.TypeIdentity), stixID(identity.TypeIdentity), stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{accountSCO(acct), personSCO(p1, nil), personSCO(p2, nil), personSCO(p3, nil)})
	if err := s.AssertAlias(ctx, testIDs, string(p1), string(p2), ana, "one human"); err != nil {
		t.Fatal(err)
	}
	if err := s.AssertOwner(ctx, testIDs, string(acct), string(p1), ana, ""); err != nil {
		t.Fatal(err)
	}
	var joinErr, ownerErr error
	beforeSeparationLock = func() {
		beforeSeparationLock = nil
		joinErr = s.AssertAlias(ctx, testIDs, string(p2), string(p3), ben, "the personnel record")
		ownerErr = s.AssertOwner(ctx, testIDs, string(acct), string(p3), ben, "")
	}
	t.Cleanup(func() { beforeSeparationLock = nil })
	_, _, err := s.WithdrawAssertion(ctx, edgeUUID(pairEdge(relAliases, p1, p2)), ben, "not one human")
	if joinErr != nil || ownerErr != nil {
		t.Fatalf("the acts in the window: join %v, owner %v", joinErr, ownerErr)
	}
	if !errors.Is(err, ErrOwnersSeparated) {
		t.Errorf("withdrawal with a join and an owner landed in the window: %v; want ErrOwnersSeparated", err)
	}
}

// TestOwnersSeparated_SetGrowsTwice: the person set grows in the window
// before the first lock (a join P2~P3) and again after the second read (a
// join P3~P4 and an owner A→P4, written straight to the edge table as a
// writer that took no lock would): the reads continue until the set is
// stable, so P4 and its owner are seen and the withdrawal of P1~P2 is
// refused.
func TestOwnersSeparated_SetGrowsTwice(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	acct := stixID(identity.TypeUserAccount)
	p1, p2, p3, p4 := stixID(identity.TypeIdentity), stixID(identity.TypeIdentity), stixID(identity.TypeIdentity), stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{accountSCO(acct), personSCO(p1, nil), personSCO(p2, nil), personSCO(p3, nil), personSCO(p4, nil)})
	if err := s.AssertAlias(ctx, testIDs, string(p1), string(p2), ana, "one human"); err != nil {
		t.Fatal(err)
	}
	if err := s.AssertOwner(ctx, testIDs, string(acct), string(p1), ana, ""); err != nil {
		t.Fatal(err)
	}
	var joinErr error
	beforeSeparationLock = func() {
		beforeSeparationLock = nil
		joinErr = s.AssertAlias(ctx, testIDs, string(p2), string(p3), ben, "")
	}
	afterEachSetRead = func(round int) {
		if round == 1 {
			afterEachSetRead = nil
			assertEdge(t, relAliases, p3, p4, false)
			assertEdge(t, relBelongsTo, acct, p4, false)
		}
	}
	t.Cleanup(func() { beforeSeparationLock, afterEachSetRead = nil, nil })
	_, _, err := s.WithdrawAssertion(ctx, edgeUUID(pairEdge(relAliases, p1, p2)), ben, "not one human")
	if joinErr != nil {
		t.Fatalf("the join in the window: %v", joinErr)
	}
	if !errors.Is(err, ErrOwnersSeparated) {
		t.Errorf("withdrawal over a set that grew twice: %v; want ErrOwnersSeparated", err)
	}
}

// TestLockAliasSets_Cap: a set that gains a member on every read is
// ErrIdentityRace after lockRounds locks; an act that meets one is tried
// once more, and a second one is returned.
func TestLockAliasSets_Cap(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	last := stixID(identity.TypeIdentity)
	start := last
	persistObjects(t, s, []capability.SCO{personSCO(last, nil)})
	reads := 0
	afterEachSetRead = func(int) {
		reads++
		next := stixID(identity.TypeIdentity)
		persistObjects(t, s, []capability.SCO{personSCO(next, nil)})
		assertEdge(t, relAliases, last, next, false)
		last = next
	}
	t.Cleanup(func() { afterEachSetRead = nil })
	tx, err := testDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.lockAliasSets(ctx, tx, []string{string(start)})
	_ = tx.Rollback()
	afterEachSetRead = nil
	if !errors.Is(err, ErrIdentityRace) || reads != lockRounds+1 {
		t.Errorf("an ever-growing set: %v after %d reads; want ErrIdentityRace after %d", err, reads, lockRounds+1)
	}

	acct, p := stixID(identity.TypeUserAccount), stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{accountSCO(acct), personSCO(p, nil)})
	t.Cleanup(func() { injectAttemptError = nil })
	injectAttemptError = func(attempt int) error {
		if attempt == 0 {
			return ErrIdentityRace
		}
		return nil
	}
	if err := s.AssertOwner(ctx, testIDs, string(acct), string(p), ana, ""); err != nil {
		t.Errorf("an owner after one race: %v", err)
	}
	injectAttemptError = func(int) error { return ErrIdentityRace }
	if err := s.AssertOwner(ctx, testIDs, string(acct), string(p), ana, ""); !errors.Is(err, ErrIdentityRace) {
		t.Errorf("two races: %v; want ErrIdentityRace", err)
	}
}

// TestFindByIDFold: an identifier is compared under the fold it was minted
// on: a non-GUID provider or directory id exact, a GUID in any case.
func TestFindByIDFold(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	pa, pb := stixID(identity.TypeIdentity), stixID(identity.TypeIdentity)
	aa, ab := stixID(identity.TypeUserAccount), stixID(identity.TypeUserAccount)
	cloud := func(id identity.STIXID, uid string) capability.SCO {
		return capability.SCO{ID: id, Type: identity.TypeUserAccount, Properties: map[string]any{"account_type": identity.AccountCloud, "user_id": uid}}
	}
	pg := stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{
		personSCO(pa, map[string]any{"directory_id": "00uAbC"}), personSCO(pb, map[string]any{"directory_id": "00uabc"}),
		personSCO(pg, map[string]any{"directory_id": "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c"}),
		cloud(aa, "00uAbC"), cloud(ab, "00uabc"),
	})
	for token, want := range map[string]identity.STIXID{"00uAbC": pa, "00uabc": pb, "8F6A0B5C-1D2E-4F3A-9B8C-7D6E5F4A3B2C": pg} {
		if got, err := s.FindPersons(ctx, token); err != nil || len(got) != 1 || got[0] != string(want) {
			t.Errorf("FindPersons(%q) = %v, %v; want only %s", token, got, err, want)
		}
	}
	for token, want := range map[string]identity.STIXID{"00uAbC": aa, "00uabc": ab} {
		if got, err := s.FindAccounts(ctx, token, 10); err != nil || len(got) != 1 || got[0].Ref != string(want) {
			t.Errorf("FindAccounts(%q) = %+v, %v; want only %s", token, got, err, want)
		}
	}
	if got, _ := s.FindPersons(ctx, "00UABC"); len(got) != 0 {
		t.Errorf("FindPersons(00UABC) = %v; want none", got)
	}
}

// TestSeenAs_WeakLogin: a weak login account is keyed on its login, so a
// later record of the same login with another uid shows in its seen-as.
func TestSeenAs_WeakLogin(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	inv := uuid.New()
	radius := func(minute int, uid string) capability.OcsfEvent {
		return ocsfEvent("radius", 3002, minute, map[string]any{
			"actor": map[string]any{"user": map[string]any{"name": "jdoe", "uid": uid}}, "src_endpoint": map[string]any{"ip": "10.0.4.55"},
		})
	}
	persistNormalized(t, s, inv, radius(1, "u-1"))
	persistNormalized(t, s, inv, radius(2, "u-9"))
	acct := string(testIDs.Account(identity.AccountIdentity{Type: identity.AccountLogin, Login: "jdoe"}))
	v, err := s.AccountIdentity(ctx, acct)
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"u-1", "u-9"} {
		if !slices.Contains(v.SeenAs.IDs, SourceID{Tool: "radius", UID: uid}) {
			t.Errorf("seen as = %+v; want %s", v.SeenAs, uid)
		}
	}
}
