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

// personWorld persists the account conformance records (adr/0324) through
// the real normalizers: a domain controller's logon (SID beside
// CONTOSO\jdoe), an endpoint's bare login, an identity provider's sign-in,
// and the directory record naming the person. It answers the investigation,
// the events in that order, and the ids the rule mints.
type personWorld struct {
	inv                                                  uuid.UUID
	dc, edr, idp, dir                                    uuid.UUID
	person, sidAcct, samAcct, upnAcct, bare, cloud, name identity.STIXID
}

func persistPersonWorld(t *testing.T, s *Store) personWorld {
	t.Helper()
	reg := capability.NewRegistry(testIDs)
	const (
		sid = "S-1-5-21-1001"
		upn = "jdoe@contoso.example"
		oid = "8f6a0b5c-1d2e-4f3a-9b8c-7d6e5f4a3b2c"
	)
	base := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	windows := map[string]any{"type_id": 100}
	idpMeta := map[string]any{"product": map[string]any{"vendor_name": "fixture-idp"}}
	mk := func(tool string, class, minute int, payload map[string]any) capability.OcsfEvent {
		payload["class_uid"] = class
		return capability.OcsfEvent{ID: uuid.Must(uuid.NewV7()), ClassUID: class, ClassName: "c", Time: base.Add(time.Duration(minute) * time.Minute),
			RecordedAt: base, SourceTool: tool, Payload: payload}
	}
	evts := []capability.OcsfEvent{
		mk("dc", 3002, 1, map[string]any{
			"actor":        map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": sid}},
			"dst_endpoint": map[string]any{"hostname": "WIN-DC01", "os": windows},
		}),
		mk("edr", 1007, 2, map[string]any{
			"process": map[string]any{"pid": float64(42), "name": "cmd.exe"},
			"device":  map[string]any{"hostname": "WS1", "os": windows},
			"actor":   map[string]any{"user": map[string]any{"name": "jdoe"}},
		}),
		mk("idp", 3002, 3, map[string]any{
			"metadata": idpMeta,
			"actor":    map[string]any{"user": map[string]any{"name": upn, "uid": oid, "account": map[string]any{"type_id": float64(6)}}},
		}),
		mk("idp", 5003, 4, map[string]any{
			"metadata": idpMeta,
			"user":     map[string]any{"uid": oid, "uid_alt": sid, "name": upn, "email_addr": upn, "full_name": "Jo Doe", "domain": "CONTOSO"},
			"unmapped": map[string]any{"on_premises_sam_account_name": "jdoe"},
		}),
	}
	res := capability.CapabilityResult{Coverage: capability.CoverageComplete, Events: evts}
	for _, e := range evts {
		nr, err := reg.Normalize(e)
		if err != nil {
			t.Fatal(err)
		}
		nr.EventID = e.ID
		res.Normalized = append(res.Normalized, nr)
	}
	inv := uuid.New()
	persistCall(t, s, Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &inv, Verb: "v", StartedAt: base}, res)
	acct := func(typ, login, uid string) identity.STIXID {
		return testIDs.Account(identity.AccountIdentity{Type: typ, Login: login, UserID: uid})
	}
	return personWorld{
		inv: inv, dc: evts[0].ID, edr: evts[1].ID, idp: evts[2].ID, dir: evts[3].ID,
		person:  testIDs.Person("fixture-idp", oid),
		sidAcct: acct(identity.AccountWindowsDomain, "", sid), samAcct: acct(identity.AccountWindowsDomain, `CONTOSO\jdoe`, ""),
		upnAcct: acct(identity.AccountWindowsDomain, upn, ""), bare: acct(identity.AccountWindows, "jdoe", ""),
		cloud: acct(identity.AccountCloud, "", oid), name: acct(identity.AccountCloud, upn, ""),
	}
}

func eventIDs(rows []EventRow) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// TestPersonReads (adr/0324): a person reads with the accounts it owns and
// their alias sets; an account reads with its own alias set only; the bare
// login nothing named stays apart.
func TestPersonReads(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	w := persistPersonWorld(t, s)

	set, err := s.PersonAccountSet(ctx, string(w.person))
	if err != nil {
		t.Fatal(err)
	}
	if set[0] != string(w.person) || !sameSet(set, w.person, w.sidAcct, w.samAcct, w.upnAcct, w.cloud, w.name) {
		t.Errorf("PersonAccountSet = %v", set)
	}

	for _, c := range []struct {
		name   string
		ref    identity.STIXID
		events []uuid.UUID
	}{
		{"person", w.person, []uuid.UUID{w.dc, w.idp, w.dir}},
		{"sam account", w.samAcct, []uuid.UUID{w.dc, w.dir}},
		{"bare account", w.bare, []uuid.UUID{w.edr}},
		{"sign-in account", w.cloud, []uuid.UUID{w.idp, w.dir}},
	} {
		got, err := s.InvestigationEvents(ctx, w.inv, EventFilter{Entity: string(c.ref)})
		if err != nil {
			t.Fatal(err)
		}
		if ids := eventIDs(got.Rows); !slices.Equal(ids, c.events) {
			t.Errorf("%s: events = %v; want %v", c.name, ids, c.events)
		}
	}

	// Around the directory record: it names the person, so the window is the
	// person's accounts' events.
	around, err := s.InvestigationEvents(ctx, w.inv, EventFilter{Around: w.dir, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if ids := eventIDs(around.Rows); !slices.Equal(ids, []uuid.UUID{w.dc, w.idp, w.dir}) {
		t.Errorf("around the directory record = %v", ids)
	}

	v, err := s.AccountIdentity(ctx, string(w.sidAcct))
	if err != nil {
		t.Fatal(err)
	}
	// The directory attached the SID, sam and principal name accounts, all in
	// the SID account's alias set: three edges, one owner.
	if len(v.Owners) != 3 {
		t.Errorf("owners = %+v; want the directory's three edges", v.Owners)
	}
	for _, o := range v.Owners {
		if o.PersonRef != string(w.person) || o.Kind != IdentityObserved || o.Source != "idp" ||
			!slices.Contains([]identity.STIXID{w.sidAcct, w.samAcct, w.upnAcct}, identity.STIXID(o.AccountRef)) {
			t.Errorf("owner = %+v", o)
		}
	}
	if !slices.Contains(v.SeenAs.Logins, `CONTOSO\jdoe`) || !slices.Contains(v.SeenAs.IDs, SourceID{Tool: "dc", UID: "S-1-5-21-1001"}) ||
		!slices.Contains(v.SeenAs.PrincipalNames, "jdoe@contoso.example") {
		t.Errorf("seen as = %+v", v.SeenAs)
	}
	if len(v.Aliases) != 2 {
		t.Errorf("aliases = %+v; want the sam and the principal name", v.Aliases)
	}
	bare, err := s.AccountIdentity(ctx, string(w.bare))
	if err != nil {
		t.Fatal(err)
	}
	if len(bare.Owners) != 0 || !slices.Equal(bare.SeenAs.Logins, []string{"jdoe"}) {
		t.Errorf("bare account view = %+v; want unowned, seen as jdoe", bare)
	}

	pv, err := s.PersonIdentity(ctx, string(w.person))
	if err != nil {
		t.Fatal(err)
	}
	var accounts []identity.STIXID
	for _, a := range pv.Accounts {
		if a.Kind != IdentityObserved {
			t.Errorf("account edge %+v; want observed", a)
		}
		accounts = append(accounts, identity.STIXID(a.AccountRef))
	}
	if len(accounts) != 5 || !slices.Contains(accounts, w.sidAcct) || slices.Contains(accounts, w.bare) {
		t.Errorf("person accounts = %v", accounts)
	}

	for token, want := range map[string]identity.STIXID{"JDoe@Contoso.Example": w.person, `CONTOSO\jdoe`: w.person} {
		got, err := s.FindPersons(ctx, token)
		if err != nil || len(got) != 1 || got[0] != string(w.person) {
			t.Errorf("FindPersons(%q) = %v, %v; want %s", token, got, err, want)
		}
	}
	if got, _ := s.FindPersons(ctx, "nobody@contoso.example"); len(got) != 0 {
		t.Errorf("FindPersons(unknown) = %v", got)
	}
	if got, _ := s.FindAccounts(ctx, "jdoe", 10); len(got) != 1 || got[0].Ref != string(w.bare) || got[0].Type != identity.AccountWindows {
		t.Errorf("FindAccounts(jdoe) = %+v", got)
	}
}

// TestFindPersons_CollapsesAliases: two directories' persons both owning the
// principal name's account answer twice, until a person joins them; then one.
func TestFindPersons_CollapsesAliases(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	w := persistPersonWorld(t, s)
	hr := stixID(identity.TypeIdentity)
	owns := capability.Relationship{ID: stixID("relationship"), Type: capability.RelBelongsTo, SourceRef: w.upnAcct, TargetRef: hr,
		Provenance: capability.Provenance{DerivationMode: capability.DerivationDirect, Tool: "hr"}}
	persistObjects(t, s, []capability.SCO{{ID: hr, Type: identity.TypeIdentity, Properties: map[string]any{"identity_class": "individual"}}}, owns)
	if got, _ := s.FindPersons(ctx, "jdoe@contoso.example"); len(got) != 2 {
		t.Fatalf("FindPersons before the join = %v; want two persons", got)
	}
	if err := s.AssertAlias(ctx, testIDs, string(w.person), string(hr), ana, "one human, two directories"); err != nil {
		t.Fatal(err)
	}
	// One answer, the pair's least ref (TestFindPersons_Representative).
	if got, err := s.FindPersons(ctx, "jdoe@contoso.example"); err != nil || len(got) != 1 || got[0] != min(string(w.person), string(hr)) {
		t.Errorf("FindPersons after the join = %v, %v; want the pair's least ref", got, err)
	}
}

// TestPersonAccountCut (adr/0324): a directory's owner cannot be withdrawn;
// "this account is not this person's", a distinct-from between the two,
// takes the account and the accounts reached through its alias set out of
// the person's reads, but for those the person owns by a belongs-to of their
// own, and marks the split account's edges cut; its withdrawal brings them
// back.
func TestPersonAccountCut(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	w := persistPersonWorld(t, s)
	pv, _ := s.PersonIdentity(ctx, string(w.person))
	var observed uuid.UUID
	for _, a := range pv.Accounts {
		if a.AccountRef == string(w.cloud) {
			observed = uuid.MustParse(a.EdgeID[len("relationship--"):])
		}
	}
	if _, _, err := s.WithdrawAssertion(ctx, observed, ana, ""); !errors.Is(err, ErrObservedOwner) || !errors.Is(err, ErrObservedEdge) {
		t.Errorf("withdrawing a directory's owner: %v; want ErrObservedOwner", err)
	}

	// The endpoint's bare login joined to the SID account by a person: owned
	// by no belongs-to of its own, it reads under the person through the SID.
	if err := s.AssertAlias(ctx, testIDs, string(w.bare), string(w.sidAcct), ana, "the endpoint's jdoe"); err != nil {
		t.Fatal(err)
	}
	// Splitting the SID account reaches its alias set (the bare login leaves
	// with it), except the accounts the directory attached by their own
	// belongs-to: the sam and principal-name accounts stay.
	cutOut := []identity.STIXID{w.sidAcct, w.bare}
	kept := []identity.STIXID{w.samAcct, w.upnAcct, w.cloud, w.name}
	if err := s.AssertDistinct(ctx, testIDs, string(w.sidAcct), string(w.person), ana, "a shared admin login"); err != nil {
		t.Fatalf("account split from the person: %v", err)
	}
	set, _ := s.PersonAccountSet(ctx, string(w.person))
	for _, a := range cutOut {
		if slices.Contains(set, string(a)) {
			t.Errorf("PersonAccountSet after the cut holds %s: %v", a, set)
		}
	}
	for _, a := range kept {
		if !slices.Contains(set, string(a)) {
			t.Errorf("PersonAccountSet after the cut lacks %s: %v", a, set)
		}
	}
	pv, _ = s.PersonIdentity(ctx, string(w.person))
	for _, a := range pv.Accounts {
		if want := a.AccountRef == string(w.sidAcct); a.Cut != want {
			t.Errorf("account %s cut = %v; want %v", a.AccountRef, a.Cut, want)
		}
	}
	if len(pv.Distinct) != 1 || pv.Distinct[0].OtherRef != string(w.sidAcct) {
		t.Errorf("person distinct = %+v; want the split listed", pv.Distinct)
	}
	if v, _ := s.AccountIdentity(ctx, string(w.samAcct)); len(v.Owners) != 3 {
		t.Errorf("sam account owners = %+v; want three edges", v.Owners)
	} else {
		for _, o := range v.Owners {
			if want := o.AccountRef == string(w.sidAcct); o.Cut != want {
				t.Errorf("owner edge from %s cut = %v; want %v", o.AccountRef, o.Cut, want)
			}
		}
	}
	if got, _ := s.accountOwners(ctx, string(w.bare)); len(got) != 0 {
		t.Errorf("the bare login's owners = %v; want none", got)
	}
	split := pairEdge(relDistinctFrom, w.sidAcct, w.person)
	if _, _, err := s.WithdrawAssertion(ctx, edgeUUID(split), ben, "not shared after all"); err != nil {
		t.Fatal(err)
	}
	set, _ = s.PersonAccountSet(ctx, string(w.person))
	for _, a := range append(cutOut, kept...) {
		if !slices.Contains(set, string(a)) {
			t.Errorf("PersonAccountSet after the withdrawal lacks %s: %v", a, set)
		}
	}
	pv, _ = s.PersonIdentity(ctx, string(w.person))
	for _, a := range pv.Accounts {
		if a.Cut {
			t.Errorf("account %s still cut after the withdrawal", a.AccountRef)
		}
	}
	// The mixed pair is a split only.
	if err := s.AssertAlias(ctx, testIDs, string(w.cloud), string(w.person), ana, ""); !errors.Is(err, ErrMixedTypes) {
		t.Errorf("joining an account to a person: %v; want ErrMixedTypes", err)
	}
}

// TestAssertOwner_PerAliasSet: one live owner assertion per alias set: the
// SID account asserted to one person, its aliased sam account to another is
// refused.
func TestAssertOwner_PerAliasSet(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	w := persistPersonWorld(t, s)
	p2 := stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{{ID: p2, Type: identity.TypeIdentity, Properties: map[string]any{"identity_class": "individual"}}})
	if err := s.AssertOwner(ctx, testIDs, string(w.sidAcct), string(w.person), ana, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AssertOwner(ctx, testIDs, string(w.samAcct), string(p2), ben, ""); !errors.Is(err, ErrOwnerAsserted) {
		t.Errorf("an aliased account to another person: %v; want ErrOwnerAsserted", err)
	}
	if err := s.AssertOwner(ctx, testIDs, string(w.samAcct), string(w.person), ben, ""); err != nil {
		t.Errorf("the same person through the alias: %v", err)
	}
}

// TestAssertOwner (adr/0324): a person says an account belongs to a person;
// the person's reads gain it; another owner is refused until the first is
// withdrawn; the withdrawal takes it out again and keeps the row.
func TestAssertOwner(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	w := persistPersonWorld(t, s)
	other := stixID(identity.TypeIdentity)
	vendor := stixID(identity.TypeIdentity)
	persistObjects(t, s, []capability.SCO{
		{ID: other, Type: identity.TypeIdentity, Properties: map[string]any{"identity_class": "individual", "name": "Someone Else"}},
		{ID: vendor, Type: identity.TypeIdentity, Properties: map[string]any{"name": "a vendor"}},
	})

	if err := s.AssertOwner(ctx, testIDs, string(w.bare), string(w.person), ana, "same desk"); err != nil {
		t.Fatalf("AssertOwner: %v", err)
	}
	if set, _ := s.PersonAccountSet(ctx, string(w.person)); !slices.Contains(set, string(w.bare)) {
		t.Errorf("an asserted owner does not widen the person: %v", set)
	}
	v, _ := s.AccountIdentity(ctx, string(w.bare))
	if len(v.Owners) != 1 || v.Owners[0].Kind != IdentityAsserted || v.Owners[0].ByName != "Ana Analyst" || v.Owners[0].Note != "same desk" {
		t.Fatalf("owners = %+v", v.Owners)
	}
	if err := s.AssertOwner(ctx, testIDs, string(w.bare), string(w.person), ben, "agreed"); err != nil {
		t.Errorf("confirming the same owner: %v", err)
	}
	if err := s.AssertOwner(ctx, testIDs, string(w.bare), string(other), ben, ""); !errors.Is(err, ErrOwnerAsserted) {
		t.Errorf("a second owner: %v; want ErrOwnerAsserted", err)
	}
	// A directory's owner is no bar to an assertion.
	if err := s.AssertOwner(ctx, testIDs, string(w.sidAcct), string(other), ben, "shared"); err != nil {
		t.Errorf("an assertion beside an observed owner: %v", err)
	}

	edge, err := uuid.Parse(v.Owners[0].EdgeID[len("relationship--"):])
	if err != nil {
		t.Fatal(err)
	}
	lo, hi, observed, err := s.AssertionEnds(ctx, edge)
	if err != nil || observed || lo != string(w.bare) || hi != string(w.person) {
		t.Errorf("ends = %s, %s, %v, %v", lo, hi, observed, err)
	}
	if _, _, err := s.WithdrawAssertion(ctx, edge, ben, "wrong desk"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if set, _ := s.PersonAccountSet(ctx, string(w.person)); slices.Contains(set, string(w.bare)) {
		t.Errorf("a withdrawn owner still widens the person: %v", set)
	}
	if v, _ := s.AccountIdentity(ctx, string(w.bare)); len(v.Owners) != 1 || !v.Owners[0].Withdrawn {
		t.Errorf("the withdrawn owner is hidden or unmarked: %+v", v.Owners)
	}
	if err := s.AssertOwner(ctx, testIDs, string(w.bare), string(other), ben, ""); err != nil {
		t.Errorf("another owner after the withdrawal: %v", err)
	}

	// The ends are typed: a vendor identity is no person, an account no host.
	var missing *MissingHostError
	if err := s.AssertOwner(ctx, testIDs, string(w.bare), string(vendor), ana, ""); !errors.As(err, &missing) || missing.Ref != string(vendor) {
		t.Errorf("a vendor as owner: %v", err)
	}
	if err := s.AssertOwner(ctx, testIDs, string(w.person), string(w.bare), ana, ""); !errors.Is(err, ErrMalformedRef) {
		t.Errorf("ends swapped: %v", err)
	}
	host := testIDs.Host(identity.HostIdentity{Hostname: "ws1"})
	if err := s.AssertAlias(ctx, testIDs, string(host), string(w.bare), ana, ""); !errors.Is(err, ErrMixedTypes) {
		t.Errorf("a host joined to an account: %v; want ErrMixedTypes", err)
	}
	// Two accounts and two persons join as hosts do.
	if err := s.AssertAlias(ctx, testIDs, string(w.bare), string(w.samAcct), ana, "the endpoint's jdoe"); err != nil {
		t.Errorf("account join: %v", err)
	}
	if got, _ := s.AliasSet(ctx, string(w.bare)); !slices.Contains(got, string(w.sidAcct)) {
		t.Errorf("the asserted account join does not read: %v", got)
	}
	if err := s.AssertDistinct(ctx, testIDs, string(w.person), string(other), ana, "two people"); err != nil {
		t.Errorf("person split: %v", err)
	}
	if err := s.AssertAlias(ctx, testIDs, string(w.person), string(vendor), ana, ""); !errors.As(err, &missing) {
		t.Errorf("a person joined to a vendor identity: %v", err)
	}
}

// TestAccountIdentity_DetectionEvidence (adr/0356): a detection's evidence is
// the list evidences[]. The user objects of its artifacts, actor.user and
// user, are read for seen-as like an event's own.
func TestAccountIdentity_DetectionEvidence(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	reg := capability.NewRegistry(testIDs)
	base := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	const (
		sid = "S-1-5-21-1001"
		upn = "jdoe@contoso.example"
	)
	evt := capability.OcsfEvent{
		ID: uuid.Must(uuid.NewV7()), ClassUID: 2004, ClassName: "Detection Finding", Time: base, RecordedAt: base, SourceTool: "edr",
		Payload: map[string]any{
			"class_uid":    2004,
			"finding_info": map[string]any{"uid": "det-1", "title": "Beacon"},
			"evidences": []any{
				map[string]any{"actor": map[string]any{"user": map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": sid}}},
				map[string]any{"user": map[string]any{"name": upn, "uid": sid}},
				"not an artifact",
			},
		},
	}
	nr, err := reg.Normalize(evt)
	if err != nil {
		t.Fatal(err)
	}
	nr.EventID = evt.ID
	inv := uuid.New()
	persistCall(t, s, Call{ID: uuid.Must(uuid.NewV7()), InvestigationID: &inv, Verb: "v", StartedAt: base},
		capability.CapabilityResult{Coverage: capability.CoverageComplete, Events: []capability.OcsfEvent{evt}, Normalized: []capability.NormalizationResult{nr}})

	account := testIDs.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: sid})
	v, err := s.AccountIdentity(ctx, string(account))
	if err != nil {
		t.Fatalf("AccountIdentity: %v", err)
	}
	if !slices.Contains(v.SeenAs.Logins, `CONTOSO\jdoe`) || !slices.Contains(v.SeenAs.IDs, SourceID{Tool: "edr", UID: sid}) ||
		!slices.Contains(v.SeenAs.PrincipalNames, upn) {
		t.Errorf("seen as = %+v; want both artifacts' user objects", v.SeenAs)
	}
}
