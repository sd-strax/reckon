package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
)

var (
	testIDs = identity.NewResolver(uuid.MustParse("6f1c0c55-7d2e-4f1e-9a51-0b6f0e5d3a10"))
	ana     = Asserter{ID: "u-1", Name: "Ana Analyst"}
	ben     = Asserter{ID: "u-2", Name: "Ben Analyst"}
)

// edgeRow reads one identity edge row by its id: its ends and its payload.
func edgeRow(t *testing.T, id identity.STIXID) (src, tgt uuid.UUID, p assertedEdge) {
	t.Helper()
	eid, _ := refUUID(string(id))
	var raw []byte
	if err := testDB.QueryRow(`SELECT source_ref, target_ref, payload FROM stix_edges WHERE id = $1`, eid).Scan(&src, &tgt, &raw); err != nil {
		t.Fatalf("read edge %s: %v", id, err)
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return src, tgt, p
}

// pairEdge is the id an assertion of relType over a and b is stored under.
func pairEdge(relType string, a, b identity.STIXID) identity.STIXID {
	if a > b {
		a, b = b, a
	}
	return testIDs.Relationship(relType+assertedNamespace, a, b)
}

func edgeUUID(id identity.STIXID) uuid.UUID {
	u, _ := refUUID(string(id))
	return u
}

// TestAssertAlias_JoinsBothWays: "same as" joins two hosts the record did
// not, read from either end; the row is one, deterministic over the ordered
// pair whichever end the analyst started from, and carries who, when, the
// note and one history event; the view lists it as asserted.
func TestAssertAlias_JoinsBothWays(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b := stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1"), hostSCO(b, "ws1-old")})

	if err := s.AssertAlias(ctx, testIDs, string(a), string(b), ana, "same serial in the asset register"); err != nil {
		t.Fatalf("AssertAlias: %v", err)
	}
	for _, from := range []identity.STIXID{a, b} {
		if got, _ := s.AliasSet(ctx, string(from)); !sameSet(got, a, b) {
			t.Errorf("AliasSet(%s) = %v; want both", from, got)
		}
	}
	// From the other end: the same row, no second one.
	if err := s.AssertAlias(ctx, testIDs, string(b), string(a), ben, "again"); err != nil {
		t.Fatalf("AssertAlias reversed: %v", err)
	}
	if n := rowCount(t, "stix_edges"); n != 1 {
		t.Fatalf("stix_edges = %d; want one row for the pair", n)
	}
	id := pairEdge(relAliases, a, b)
	src, tgt, p := edgeRow(t, id)
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	if src != edgeUUID(lo) || tgt != edgeUUID(hi) || p.SourceRef != string(lo) || p.TargetRef != string(hi) || p.ID != string(id) || p.Type != relAliases {
		t.Errorf("row = %s→%s %+v; want the ordered pair", src, tgt, p)
	}
	// A live assertion is not re-asserted: the first asserter and note stand,
	// and the second person's confirmation is in the history.
	if p.AssertedBy != "u-1" || p.AssertedByName != "Ana Analyst" || p.Note != "same serial in the asset register" || p.AssertedAt.IsZero() ||
		p.WithdrawnAt != nil || len(p.History) != 2 || p.History[0].Act != "asserted" || p.History[0].By != "u-1" ||
		p.History[1].Act != "asserted" || p.History[1].By != "u-2" || p.History[1].ByName != "Ben Analyst" || p.History[1].Note != "again" {
		t.Errorf("assertion = %+v", p.Assertion)
	}
	v, err := s.HostIdentity(ctx, string(b))
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Aliases) != 1 || v.Aliases[0].Kind != IdentityAsserted || v.Aliases[0].OtherRef != string(a) || v.Aliases[0].EdgeID != string(id) ||
		v.Aliases[0].ByName != "Ana Analyst" || v.Aliases[0].Note != "same serial in the asset register" || v.Aliases[0].Withdrawn {
		t.Errorf("view = %+v", v.Aliases)
	}
}

// TestAssertDistinct_Cuts: "not the same host" cuts an observed join; the
// distinct-from row stands beside the aliases edge, and the view lists both.
func TestAssertDistinct_Cuts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b := stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1"), hostSCO(b, "")}, observedAlias(b, a, "edr"))
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a, b) {
		t.Fatalf("observed join missing: %v", got)
	}

	if err := s.AssertDistinct(ctx, testIDs, string(a), string(b), ana, "two machines"); err != nil {
		t.Fatalf("AssertDistinct: %v", err)
	}
	for _, from := range []identity.STIXID{a, b} {
		if got, _ := s.AliasSet(ctx, string(from)); !sameSet(got, from) {
			t.Errorf("AliasSet(%s) = %v; want it alone", from, got)
		}
	}
	v, _ := s.HostIdentity(ctx, string(a))
	if len(v.Aliases) != 1 || v.Aliases[0].Kind != IdentityObserved || len(v.Distinct) != 1 || v.Distinct[0].Kind != IdentityAsserted || v.Distinct[0].Note != "two machines" {
		t.Errorf("view = %+v", v)
	}
}

// TestWithdrawAssertion_RestoresAndReasserts: withdrawing a split joins the
// reads again and keeps the row, marked, with the act in its history; a
// second withdrawal changes nothing; asserting again clears the mark, takes
// the new asserter and note, and the history grows.
func TestWithdrawAssertion_RestoresAndReasserts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b := stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1"), hostSCO(b, "")}, observedAlias(b, a, "edr"))
	if err := s.AssertDistinct(ctx, testIDs, string(b), string(a), ana, "two machines"); err != nil {
		t.Fatal(err)
	}
	id := pairEdge(relDistinctFrom, a, b)

	gotLo, gotHi, err := s.WithdrawAssertion(ctx, edgeUUID(id), ben, "same machine after all")
	if err != nil {
		t.Fatalf("WithdrawAssertion: %v", err)
	}
	lo, hi := a, b
	if b < a {
		lo, hi = b, a
	}
	if gotLo != string(lo) || gotHi != string(hi) {
		t.Errorf("ends = %s, %s; want the ordered pair %s, %s", gotLo, gotHi, lo, hi)
	}
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a, b) {
		t.Errorf("withdrawn split still cuts: %v", got)
	}
	_, _, p := edgeRow(t, id)
	if p.WithdrawnAt == nil || len(p.History) != 2 || p.History[1].Act != "withdrawn" || p.History[1].By != "u-2" || p.History[1].Note != "same machine after all" {
		t.Errorf("withdrawn = %+v", p.Assertion)
	}
	v, _ := s.HostIdentity(ctx, string(a))
	if len(v.Distinct) != 1 || !v.Distinct[0].Withdrawn {
		t.Errorf("view hides or unmarks the withdrawn edge: %+v", v.Distinct)
	}

	// Withdrawn again: a no-op.
	if _, _, err := s.WithdrawAssertion(ctx, edgeUUID(id), ana, "again"); err != nil {
		t.Fatalf("second withdrawal: %v", err)
	}
	if _, _, p := edgeRow(t, id); len(p.History) != 2 {
		t.Errorf("a second withdrawal wrote history: %+v", p.History)
	}

	// Asserted again: live, the new asserter, the history grows.
	if err := s.AssertDistinct(ctx, testIDs, string(a), string(b), ben, "checked twice"); err != nil {
		t.Fatal(err)
	}
	_, _, p = edgeRow(t, id)
	if p.WithdrawnAt != nil || p.AssertedBy != "u-2" || p.Note != "checked twice" || len(p.History) != 3 || p.History[2].Act != "asserted" {
		t.Errorf("re-asserted = %+v", p.Assertion)
	}
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a) {
		t.Errorf("re-asserted split does not cut: %v", got)
	}
	if n := rowCount(t, "stix_edges"); n != 2 {
		t.Errorf("stix_edges = %d; want the observed alias and the one split", n)
	}
}

// TestAssertAlias_OverObserved: a join of a pair a source's record already
// joins is written beside the record, so the analyst's name and note are kept
// and the view lists both origins; withdrawing the assertion leaves the
// record's join; withdrawing the record is refused, and AssertionEnds says it
// is one, whether or not its weak end is still persisted.
func TestAssertAlias_OverObserved(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b := stixID("x-host"), stixID("x-host")
	rel := observedAlias(b, a, "edr")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1"), hostSCO(b, "")}, rel)

	if err := s.AssertAlias(ctx, testIDs, string(a), string(b), ana, "asset register"); err != nil {
		t.Fatalf("AssertAlias: %v", err)
	}
	if n := rowCount(t, "stix_edges"); n != 2 {
		t.Fatalf("stix_edges = %d; want the record and the assertion", n)
	}
	v, _ := s.HostIdentity(ctx, string(a))
	kinds := map[string]bool{}
	for _, e := range v.Aliases {
		if !e.Withdrawn {
			kinds[e.Kind] = true
		}
	}
	if len(v.Aliases) != 2 || !kinds[IdentityObserved] || !kinds[IdentityAsserted] {
		t.Errorf("view = %+v; want both origins live", v.Aliases)
	}
	asserted := pairEdge(relAliases, a, b)
	if _, _, err := s.WithdrawAssertion(ctx, edgeUUID(asserted), ana, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a, b) {
		t.Errorf("the record's join went with the assertion: %v", got)
	}

	if _, _, err := s.WithdrawAssertion(ctx, edgeUUID(rel.ID), ana, ""); !errors.Is(err, ErrObservedEdge) {
		t.Errorf("withdraw an observed edge: %v; want ErrObservedEdge", err)
	}
	// The weak end gone: still a source's record with both its ends.
	if _, err := testDB.Exec(`DELETE FROM stix_objects WHERE id = $1`, edgeUUID(b)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.WithdrawAssertion(ctx, edgeUUID(rel.ID), ana, ""); !errors.Is(err, ErrObservedEdge) {
		t.Errorf("withdraw an observed edge whose weak host is gone: %v; want ErrObservedEdge", err)
	}
	src, tgt, observed, err := s.AssertionEnds(ctx, edgeUUID(rel.ID))
	if err != nil || src != string(b) || tgt != string(a) || !observed {
		t.Errorf("AssertionEnds(record) = %s, %s, %v, %v; want %s, %s, observed", src, tgt, observed, err, b, a)
	}
}

// TestAssertionEnds: an assertion's ends as stored, lo then hi, the edge not a
// record; an unknown edge is not found.
func TestAssertionEnds(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b := stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1"), hostSCO(b, "ws2")})
	if err := s.AssertDistinct(ctx, testIDs, string(a), string(b), ana, ""); err != nil {
		t.Fatal(err)
	}
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	gotLo, gotHi, observed, err := s.AssertionEnds(ctx, edgeUUID(pairEdge(relDistinctFrom, a, b)))
	if err != nil || gotLo != string(lo) || gotHi != string(hi) || observed {
		t.Errorf("AssertionEnds = %s, %s, %v, %v; want %s, %s, asserted", gotLo, gotHi, observed, err, lo, hi)
	}
	if _, _, _, err := s.AssertionEnds(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown edge: %v", err)
	}
}

// TestAssert_NonHostUUID: a persisted object of another type, its uuid behind
// an x-host prefix, is no host.
func TestAssert_NonHostUUID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, addr := stixID("x-host"), stixID("ipv4-addr")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1"), {ID: addr, Type: "ipv4-addr", Properties: map[string]any{"value": "10.0.0.7"}}})
	posing := "x-host--" + strings.TrimPrefix(string(addr), "ipv4-addr--")
	err := s.AssertAlias(ctx, testIDs, string(a), posing, ana, "")
	var missing *MissingHostError
	if !errors.As(err, &missing) || missing.Ref != posing {
		t.Errorf("an address as a host: %v; want a MissingHostError naming %s", err, posing)
	}
	if n := rowCount(t, "stix_edges"); n != 0 {
		t.Errorf("stix_edges = %d; want none", n)
	}
}

func TestCanonicalHostRef(t *testing.T) {
	id := uuid.NewString()
	for in, want := range map[string]string{
		"x-host--" + id:                  "x-host--" + id,
		"X-HOST--" + strings.ToUpper(id): "x-host--" + id,
		"ipv4-addr--" + id:               "",
		"x-host--nope":                   "",
		"win-file01":                     "",
	} {
		got, ok := CanonicalHostRef(in)
		if got != want || ok != (want != "") {
			t.Errorf("CanonicalHostRef(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

// TestAssert_CaseFolds: a ref spelled in upper case names the same host, so
// the pair has one row and its ends are stored in canonical form.
func TestAssert_CaseFolds(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b := stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1"), hostSCO(b, "ws2")})
	if err := s.AssertDistinct(ctx, testIDs, string(a), string(b), ana, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AssertDistinct(ctx, testIDs, strings.ToUpper(string(b)), strings.ToUpper(string(a)), ana, ""); err != nil {
		t.Fatalf("upper case: %v", err)
	}
	if n := rowCount(t, "stix_edges"); n != 1 {
		t.Errorf("stix_edges = %d; want one row for the pair in either case", n)
	}
	if _, _, p := edgeRow(t, pairEdge(relDistinctFrom, a, b)); p.SourceRef != strings.ToLower(p.SourceRef) || p.TargetRef != strings.ToLower(p.TargetRef) {
		t.Errorf("ends not canonical: %s, %s", p.SourceRef, p.TargetRef)
	}
}

// TestAssertAlias_BesideObserved: an assertion and a source's record of the
// same pair are two rows, the record's id never taken by the assertion, so a
// record seen after the assertion is kept; withdrawing the assertion leaves
// the record and its join.
func TestAssertAlias_BesideObserved(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b := stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1"), hostSCO(b, "")})
	if err := s.AssertAlias(ctx, testIDs, string(a), string(b), ana, "asset register"); err != nil {
		t.Fatal(err)
	}
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	// The id a normalizer would mint for the same pair, in the same order.
	rel := observedAlias(lo, hi, "edr")
	rel.ID = testIDs.Relationship(relAliases, lo, hi)
	persistObjects(t, s, nil, rel)
	if n := rowCount(t, "stix_edges"); n != 2 {
		t.Fatalf("stix_edges = %d; want the assertion and the record", n)
	}
	if _, _, err := s.WithdrawAssertion(ctx, edgeUUID(pairEdge(relAliases, a, b)), ana, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a, b) {
		t.Errorf("the record's join went with the assertion: %v", got)
	}
	v, _ := s.HostIdentity(ctx, string(a))
	if len(v.Aliases) != 2 {
		t.Errorf("view = %+v; want the record and the withdrawn assertion", v.Aliases)
	}
}

// TestAssert_SplitAndJoin: a join over a live split is refused, never an
// implicit withdrawal; a split over a live asserted join is allowed and cuts.
func TestAssert_SplitAndJoin(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b := stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1"), hostSCO(b, "ws2")})
	if err := s.AssertAlias(ctx, testIDs, string(a), string(b), ana, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AssertDistinct(ctx, testIDs, string(b), string(a), ben, "two machines"); err != nil {
		t.Fatalf("split over a join: %v", err)
	}
	if got, _ := s.AliasSet(ctx, string(a)); !sameSet(got, a) {
		t.Errorf("the split does not cut: %v", got)
	}
	// Withdraw the join, then ask for it again while the split is live.
	if _, _, err := s.WithdrawAssertion(ctx, edgeUUID(pairEdge(relAliases, a, b)), ana, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AssertAlias(ctx, testIDs, string(a), string(b), ana, ""); !errors.Is(err, ErrSplitLive) {
		t.Errorf("join over a live split: %v; want ErrSplitLive", err)
	}
	if _, _, p := edgeRow(t, pairEdge(relAliases, a, b)); p.WithdrawnAt == nil {
		t.Errorf("a refused join re-asserted the alias")
	}
	if _, _, p := edgeRow(t, pairEdge(relDistinctFrom, a, b)); p.WithdrawnAt != nil {
		t.Errorf("a refused join withdrew the split")
	}
}

// TestAssert_Refusals: an end not persisted is not found; a malformed ref or
// the host against itself is malformed; an unknown edge is not found.
func TestAssert_Refusals(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, ghost := stixID("x-host"), stixID("x-host")
	persistObjects(t, s, []capability.SCO{hostSCO(a, "ws1")})

	// No principal: an empty asserted_by would read as a source's record.
	if err := s.AssertAlias(ctx, testIDs, string(a), string(ghost), Asserter{Name: "nobody"}, ""); !errors.Is(err, ErrNoAsserter) {
		t.Errorf("no asserter: %v; want ErrNoAsserter", err)
	}
	if _, _, err := s.WithdrawAssertion(ctx, uuid.New(), Asserter{}, ""); !errors.Is(err, ErrNoAsserter) {
		t.Errorf("withdraw with no asserter: %v; want ErrNoAsserter", err)
	}

	err := s.AssertAlias(ctx, testIDs, string(a), string(ghost), ana, "")
	var missing *MissingHostError
	if !errors.Is(err, ErrNotFound) || !errors.As(err, &missing) || missing.Ref != string(ghost) {
		t.Errorf("unknown other: %v; want a MissingHostError naming %s", err, ghost)
	}
	if err := s.AssertDistinct(ctx, testIDs, string(a), "nope", ana, ""); !errors.Is(err, ErrMalformedRef) {
		t.Errorf("malformed: %v", err)
	}
	if err := s.AssertDistinct(ctx, testIDs, string(a), string(a), ana, ""); !errors.Is(err, ErrMalformedRef) {
		t.Errorf("self: %v", err)
	}
	if _, _, err := s.WithdrawAssertion(ctx, uuid.New(), ana, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown edge: %v", err)
	}
	if n := rowCount(t, "stix_edges"); n != 0 {
		t.Errorf("stix_edges = %d; want none", n)
	}
}
