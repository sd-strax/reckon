package evidence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/module"
)

// Identity assertions (adr/0211, adr/0324): the analyst's "same as another
// host" and "not the same host", and the same for two accounts or two
// persons, written as aliases and distinct-from edges in stix_edges; "this
// account belongs to this person", an asserted belongs-to; and their
// withdrawal. One pair has one asserted row per
// relationship type, whichever end the analyst started from; a withdrawal
// marks the row and never deletes it, and every act on it is kept in its
// history (02 §2.2, CRUD with history). An asserted row's id is minted in its
// own namespace, so it never takes the id a source's record of the same pair
// would mint: the two stand side by side.

var (
	// ErrObservedEdge reports a withdrawal of an edge a source's record
	// produced: the record said it, so it cannot be withdrawn; a person who
	// disagrees asserts distinct-from instead.
	ErrObservedEdge = errors.New("evidence: a source's record, not an assertion; assert distinct-from instead")
	// ErrSplitLive reports a join of a pair a live distinct-from holds apart:
	// the split is withdrawn first, never implicitly.
	ErrSplitLive = errors.New("evidence: the pair is asserted apart; withdraw that first")
	// ErrNoAsserter reports an act with no principal: an asserted edge is
	// told from a source's record by a non-empty asserted_by, so a writer
	// never produces an empty one.
	ErrNoAsserter = errors.New("evidence: an identity act names no principal")
	// ErrMixedTypes reports a join or a split between two refs of different
	// entity types: a host is joined to a host, an account to an account, a
	// person to a person (adr/0324). The one mixed pair accepted is a split
	// of an account from a person, "this account is not this person's". It is
	// ErrMalformedRef.
	ErrMixedTypes = fmt.Errorf("evidence: both ends must be the same entity type: %w", ErrMalformedRef)
	// ErrObservedOwner reports a withdrawal of a belongs-to a directory's
	// record produced: it is undone by asserting that the account is not the
	// person's, a distinct-from between the two (adr/0324). It is
	// ErrObservedEdge.
	ErrObservedOwner = fmt.Errorf("evidence: a directory's record; assert that the account is not this person's instead: %w", ErrObservedEdge)
	// ErrOwnerAsserted reports an owner assertion for an account whose alias
	// set a person has already asserted belongs to another person: the live
	// one is withdrawn first (adr/0324).
	ErrOwnerAsserted = errors.New("evidence: the account is asserted to belong to another person; withdraw that first")
	// ErrOwnersSeparated reports a split of two persons, or the withdrawal of
	// their join, that would leave an account asserted to two persons: one
	// owner assertion is withdrawn first (adr/0324). It is ErrOwnerAsserted.
	ErrOwnersSeparated = fmt.Errorf("evidence: the split would leave an account asserted to two persons; withdraw one owner first: %w", ErrOwnerAsserted)
	// ErrOwnersDiffer reports a join of two accounts whose alias sets are
	// asserted to belong to different persons (adr/0324). It is
	// ErrOwnerAsserted.
	ErrOwnersDiffer = fmt.Errorf("evidence: the two are asserted to different persons; withdraw one first: %w", ErrOwnerAsserted)
)

// MissingHostError names an end of an assertion that is not persisted as the
// entity type it was named as: a host, an account or a person (Type, the
// STIX type, x-host when empty). It is ErrNotFound.
type MissingHostError struct {
	Ref  string
	Type string
}

func (e *MissingHostError) Error() string {
	return "evidence: no persisted " + entityWord(e.Type) + " " + e.Ref
}

// entityWord names an identity entity type for a message.
func entityWord(typ string) string {
	switch typ {
	case identity.TypeUserAccount:
		return "account"
	case identity.TypeIdentity:
		return "person"
	}
	return "host"
}

// Unwrap makes a MissingHostError an ErrNotFound.
func (e *MissingHostError) Unwrap() error { return ErrNotFound }

// Asserter is the person acting on an identity edge: the principal's subject
// and display name, from the token.
type Asserter struct {
	ID   string
	Name string
}

// Assertion history acts.
const (
	actAsserted  = "asserted"
	actWithdrawn = "withdrawn"
)

// assertedEdge is the payload of an asserted edge: the relationship's own
// fields beside the Assertion, as identityEdge reads it.
type assertedEdge struct {
	ID        string `json:"id"`
	Type      string `json:"relationship_type"`
	SourceRef string `json:"source_ref"`
	TargetRef string `json:"target_ref"`
	Assertion
}

// assertedNamespace discriminates an asserted edge's minted id from the id a
// source's record of the same pair and type would take.
const assertedNamespace = "/asserted"

// AssertAlias records that ref and otherRef are one entity, two hosts, two
// accounts or two persons (two types is ErrMixedTypes): a live aliases edge
// between them, asserted by by with note. A live assertion of the pair, or an
// observed aliases edge between them, already says so and nothing is written;
// a withdrawn assertion of the pair is asserted again. A pair a live
// distinct-from holds apart is ErrSplitLive. ids mints the edge's
// deterministic id over the ordered pair. An end not persisted is a
// MissingHostError.
func (s *Store) AssertAlias(ctx context.Context, ids *identity.Resolver, ref, otherRef string, by Asserter, note string) error {
	return s.assert(ctx, ids, relAliases, ref, otherRef, by, note)
}

// AssertDistinct records that ref and otherRef are two entities of one type,
// or that an account is not a person's (an account and a person, either way
// round, adr/0324): a live distinct-from edge between them, asserted by by
// with note. It may stand beside an aliases or a belongs-to edge between
// them, observed or asserted: cutting that join is what it is for, and the
// one way to undo an owner a directory's record observed. Idempotent and re-assertable as AssertAlias.
func (s *Store) AssertDistinct(ctx context.Context, ids *identity.Resolver, ref, otherRef string, by Asserter, note string) error {
	return s.assert(ctx, ids, relDistinctFrom, ref, otherRef, by, note)
}

// CanonicalHostRef reports whether ref is a host ref (`x-host--<uuid>`, the
// type in any case) and answers it in canonical (lowercase) form, so one host
// has one spelling of its ref.
func CanonicalHostRef(ref string) (string, bool) {
	c, typ, ok := CanonicalEntityRef(ref)
	return c, ok && typ == identity.TypeHost
}

// identityTypes are the entity types whose identity a person may assert
// (adr/0211, adr/0324): hosts, accounts and persons.
var identityTypes = []string{identity.TypeHost, identity.TypeUserAccount, identity.TypeIdentity}

// CanonicalEntityRef reports whether ref names an entity whose identity can
// be asserted (x-host, user-account or identity, the type in any case) and
// answers it in canonical (lowercase) form with its type, so one entity has
// one spelling of its ref.
func CanonicalEntityRef(ref string) (canonical, typ string, ok bool) {
	c, typ, _, ok := canonicalEntityRef(ref)
	return c, typ, ok
}

func canonicalEntityRef(ref string) (string, string, uuid.UUID, bool) {
	typ, rest, found := strings.Cut(ref, "--")
	if !found {
		return "", "", uuid.UUID{}, false
	}
	typ = strings.ToLower(typ)
	if !slices.Contains(identityTypes, typ) {
		return "", "", uuid.UUID{}, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return "", "", uuid.UUID{}, false
	}
	return typ + "--" + id.String(), typ, id, true
}

// accountPersonPair reports whether the two types are an account and a
// person, either way round: the one mixed pair a distinct-from may hold
// apart, "this account is not this person's" (adr/0324).
func accountPersonPair(a, b string) bool {
	return (a == identity.TypeUserAccount && b == identity.TypeIdentity) || (a == identity.TypeIdentity && b == identity.TypeUserAccount)
}

func (s *Store) assertOnce(ctx context.Context, ids *identity.Resolver, relType, ref, otherRef string, by Asserter, note string) error {
	if ids == nil {
		return errors.New("evidence: no identity resolver to mint the edge id")
	}
	if by.ID == "" {
		return ErrNoAsserter
	}
	ref, typ, a, aok := canonicalEntityRef(ref)
	otherRef, otherTyp, b, bok := canonicalEntityRef(otherRef)
	if !aok || !bok {
		return ErrMalformedRef
	}
	if typ != otherTyp && (relType != relDistinctFrom || !accountPersonPair(typ, otherTyp)) {
		return ErrMixedTypes
	}
	if a == b {
		return fmt.Errorf("evidence: an entity cannot be asserted against itself: %w", ErrMalformedRef)
	}
	// The ordered pair: one row whichever end the analyst started from.
	lo, hi, loID, hiID := ref, otherRef, a, b
	if lo > hi {
		lo, hi, loID, hiID = hi, lo, b, a
	}
	edgeRef := ids.Relationship(relType+assertedNamespace, identity.STIXID(lo), identity.STIXID(hi))
	edgeID, ok := refUUID(string(edgeRef))
	if !ok {
		return fmt.Errorf("evidence: minted a malformed edge id %q", edgeRef)
	}
	// Joining two accounts joins their owners: the two alias sets are locked
	// in the transaction, as an owner assertion locks its set (AssertOwner).
	var setA, setB []uuid.UUID
	accountJoin := relType == relAliases && typ == identity.TypeUserAccount
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin assertion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if accountJoin {
		sets, err := s.lockAliasSets(ctx, tx, []string{ref, otherRef})
		if err != nil {
			return err
		}
		setA, setB = sets[0], sets[1]
	}
	if err := requirePersisted(ctx, tx, [2]string{ref, otherRef}, [2]uuid.UUID{a, b}, [2]string{typ, otherTyp}); err != nil {
		return err
	}
	if accountJoin {
		// Two accounts asserted to different persons are not joined until
		// one assertion is withdrawn: the join would give one account two
		// asserted owners.
		ownersA, err := liveAssertedOwners(ctx, tx, setA)
		if err != nil {
			return err
		}
		ownersB, err := liveAssertedOwners(ctx, tx, setB)
		if err != nil {
			return err
		}
		differ, err := s.ownersDiffer(ctx, ownersA, ownersB)
		if err != nil {
			return err
		}
		if differ {
			return ErrOwnersDiffer
		}
	}

	if relType == relDistinctFrom && typ == identity.TypeIdentity && otherTyp == identity.TypeIdentity {
		// Splitting two persons must not leave an account asserted to both.
		if err := s.refuseOwnersSeparated(ctx, tx, ref, func(g *aliasGraph) {
			g.apart[[2]uuid.UUID{a, b}], g.apart[[2]uuid.UUID{b, a}] = true, true
		}); err != nil {
			return err
		}
	}
	if relType == relAliases {
		// A live split of the pair is withdrawn first, never implicitly;
		// found by pair and type, never by id. A source's record joining the
		// pair is no reason to skip the write: the assertion keeps the
		// analyst's name and note beside the record.
		split, err := liveAssertedPairEdge(ctx, tx, relDistinctFrom, loID, hiID)
		if err != nil {
			return err
		}
		if split {
			return ErrSplitLive
		}
	}
	if err := writeAssertion(ctx, tx, edgeRef, edgeID, relType, [2]string{lo, hi}, [2]uuid.UUID{loID, hiID}, by, note); err != nil {
		return err
	}
	return tx.Commit()
}

// AssertOwner records that the account belongs to the person (adr/0324): a
// live belongs-to edge from accountRef to personRef, asserted by by with note,
// in the asserted namespace, so it stands beside any belongs-to a directory's
// record of the same pair produced. An account's alias set has one live
// owner assertion: asserting another person while any account of the set has
// a live one is ErrOwnerAsserted until it is withdrawn; asserting the same
// person again confirms it (history), and a withdrawn assertion of the pair
// is asserted again. An observed owner from a record is no bar; a live split
// of the account's alias set from the person's is ErrSplitLive until
// withdrawn. The alias sets are locked in the transaction before the checks,
// so assertions over overlapping sets serialize; one that loses a deadlock
// to another is tried once more (withDeadlockRetry). Either end not persisted
// as its type (a user-account; an identity whose identity_class is
// individual) is a MissingHostError naming it; ends of other types are
// ErrMalformedRef.
func (s *Store) AssertOwner(ctx context.Context, ids *identity.Resolver, accountRef, personRef string, by Asserter, note string) error {
	return withDeadlockRetry(func() error { return s.assertOwnerOnce(ctx, ids, accountRef, personRef, by, note) })
}

// assert writes an aliases or distinct-from assertion (assertOnce), tried
// once more when it loses a deadlock (withDeadlockRetry).
func (s *Store) assert(ctx context.Context, ids *identity.Resolver, relType, ref, otherRef string, by Asserter, note string) error {
	return withDeadlockRetry(func() error { return s.assertOnce(ctx, ids, relType, ref, otherRef, by, note) })
}

// injectAttemptError, when set by a test, answers an error for an attempt
// of withDeadlockRetry before the attempt runs (nil runs it).
var injectAttemptError func(attempt int) error

// withDeadlockRetry runs an identity act and, when Postgres aborted it as a
// deadlock victim (isDeadlock), runs it once more: the second attempt reads
// the alias sets afresh, so it succeeds or answers its typed refusal. A
// deadlock on the second attempt is returned (the server answers 409).
func withDeadlockRetry(act func() error) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		err = nil
		if injectAttemptError != nil {
			err = injectAttemptError(attempt)
		}
		if err == nil {
			err = act()
		}
		if !IsDeadlock(err) {
			return err
		}
	}
	return err
}

// IsDeadlock reports whether err is Postgres' deadlock_detected (SQLSTATE
// 40P01): the transaction was aborted to break a lock cycle with another.
func IsDeadlock(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "40P01"
}

func (s *Store) assertOwnerOnce(ctx context.Context, ids *identity.Resolver, accountRef, personRef string, by Asserter, note string) error {
	if ids == nil {
		return errors.New("evidence: no identity resolver to mint the edge id")
	}
	if by.ID == "" {
		return ErrNoAsserter
	}
	account, at, a, aok := canonicalEntityRef(accountRef)
	person, pt, p, pok := canonicalEntityRef(personRef)
	if !aok || !pok || at != identity.TypeUserAccount || pt != identity.TypeIdentity {
		return ErrMalformedRef
	}
	edgeRef := ids.Relationship(relBelongsTo+assertedNamespace, identity.STIXID(account), identity.STIXID(person))
	edgeID, ok := refUUID(string(edgeRef))
	if !ok {
		return fmt.Errorf("evidence: minted a malformed edge id %q", edgeRef)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin owner assertion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// The account's alias set reads as one account, so it has one asserted
	// owner. Lock every object of its alias set and the person's before the
	// checks: two owner assertions over overlapping sets, or an owner
	// assertion and an alias join, serialize here, so only one of them sees
	// no other owner.
	sets, err := s.lockAliasSets(ctx, tx, []string{account, person})
	if err != nil {
		return err
	}
	setIDs, personIDs := sets[0], sets[1]
	if err := requirePersisted(ctx, tx, [2]string{account, person}, [2]uuid.UUID{a, p}, [2]string{at, pt}); err != nil {
		return err
	}
	// An owner over a live split of the account's set from the person's set
	// is refused until the split is withdrawn: one act at a time.
	var split bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM stix_edges
		WHERE tenant_id = $1 AND relationship_type = '`+relDistinctFrom+`' AND payload->>'withdrawn_at' IS NULL
		  AND ((source_ref = ANY($2) AND target_ref = ANY($3)) OR (source_ref = ANY($3) AND target_ref = ANY($2))))
	`, module.SingleTenantUUID, uuidArray(setIDs), uuidArray(personIDs)).Scan(&split); err != nil {
		return fmt.Errorf("query live splits of the account from the person: %w", err)
	}
	if split {
		return ErrSplitLive
	}
	var other bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM stix_edges
		WHERE tenant_id = $1 AND relationship_type = '`+relBelongsTo+`' AND source_ref = ANY($2) AND NOT (target_ref = ANY($3))
		  AND payload->>'withdrawn_at' IS NULL AND COALESCE(payload->>'asserted_by', '') <> '')
	`, module.SingleTenantUUID, uuidArray(setIDs), uuidArray(personIDs)).Scan(&other); err != nil {
		return fmt.Errorf("query live owner assertions: %w", err)
	}
	if other {
		return ErrOwnerAsserted
	}
	if err := writeAssertion(ctx, tx, edgeRef, edgeID, relBelongsTo, [2]string{account, person}, [2]uuid.UUID{a, p}, by, note); err != nil {
		return err
	}
	return tx.Commit()
}

// refIDs answers the uuids of refs, the malformed skipped.
func refIDs(refs []string) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		if id, ok := refUUID(r); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// lockObjects locks the persisted objects among ids, in id order, for the
// transaction.
func lockObjects(ctx context.Context, tx *sql.Tx, ids []uuid.UUID) error {
	if _, err := tx.ExecContext(ctx, `SELECT id FROM stix_objects WHERE tenant_id = $1 AND id = ANY($2) ORDER BY id FOR UPDATE`,
		module.SingleTenantUUID, uuidArray(ids)); err != nil {
		return fmt.Errorf("lock the alias sets: %w", err)
	}
	return nil
}

// afterAliasRead, when set by a test, runs between lockAliasSets' first read
// of the alias sets and its lock: the window a concurrent join commits in.
var afterAliasRead func()

// lockAliasSets reads the alias set of each of refs and locks every object
// of them in tx, in id order. The sets are read outside the transaction, so
// a join that commits between the read and the lock would leave a member
// unlocked: they are read again once the lock is held, and any member they
// gained is locked too (one retry; a join needs the locks held here to grow
// them again). It answers each ref's set, as read the second time.
func (s *Store) lockAliasSets(ctx context.Context, tx *sql.Tx, refs []string) ([][]uuid.UUID, error) {
	read := func() ([][]uuid.UUID, error) {
		out := make([][]uuid.UUID, 0, len(refs))
		for _, r := range refs {
			set, err := s.aliasSets(ctx, []string{r})
			if err != nil {
				return nil, err
			}
			out = append(out, refIDs(set))
		}
		return out, nil
	}
	first, err := read()
	if err != nil {
		return nil, err
	}
	if afterAliasRead != nil {
		afterAliasRead()
	}
	locked := map[uuid.UUID]bool{}
	var all []uuid.UUID
	for _, set := range first {
		for _, id := range set {
			if !locked[id] {
				locked[id] = true
				all = append(all, id)
			}
		}
	}
	if err := lockObjects(ctx, tx, all); err != nil {
		return nil, err
	}
	again, err := read()
	if err != nil {
		return nil, err
	}
	var gained []uuid.UUID
	for _, set := range again {
		for _, id := range set {
			if !locked[id] {
				locked[id] = true
				gained = append(gained, id)
			}
		}
	}
	if len(gained) > 0 {
		if err := lockObjects(ctx, tx, gained); err != nil {
			return nil, err
		}
	}
	return again, nil
}

// ownersDiffer reports whether two sets of asserted owners name different
// persons: some owner of b outside the alias set of some owner of a.
func (s *Store) ownersDiffer(ctx context.Context, a, b []string) (bool, error) {
	for _, pa := range a {
		same, err := s.aliasSets(ctx, []string{pa})
		if err != nil {
			return false, err
		}
		for _, pb := range b {
			if !slices.Contains(same, pb) {
				return true, nil
			}
		}
	}
	return false, nil
}

// liveAssertedOwners reads the persons a live asserted belongs-to attaches
// any of accounts to.
func liveAssertedOwners(ctx context.Context, tx *sql.Tx, accounts []uuid.UUID) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT target_ref FROM stix_edges
		WHERE tenant_id = $1 AND relationship_type = '`+relBelongsTo+`' AND source_ref = ANY($2)
		  AND payload->>'withdrawn_at' IS NULL AND COALESCE(payload->>'asserted_by', '') <> ''
	`, module.SingleTenantUUID, uuidArray(accounts))
	if err != nil {
		return nil, fmt.Errorf("query live owner assertions: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan owner assertion: %w", err)
		}
		out = append(out, identity.TypeIdentity+"--"+id.String())
	}
	return out, rows.Err()
}

// writeAssertion inserts an asserted edge from ends[0] to ends[1], or, when
// the row exists, confirms a live assertion (history) or asserts a withdrawn
// one again; a row that is no assertion is left alone. The caller holds the
// transaction and commits.
func writeAssertion(ctx context.Context, tx *sql.Tx, edgeRef identity.STIXID, edgeID uuid.UUID, relType string, ends [2]string, endIDs [2]uuid.UUID, by Asserter, note string) error {
	at := now()
	event := AssertionEvent{Act: actAsserted, By: by.ID, ByName: by.Name, At: at, Note: note}
	fresh := assertedEdge{
		ID: string(edgeRef), Type: relType, SourceRef: ends[0], TargetRef: ends[1],
		Assertion: Assertion{AssertedBy: by.ID, AssertedByName: by.Name, AssertedAt: at, Note: note, History: []AssertionEvent{event}},
	}
	payload, err := json.Marshal(fresh)
	if err != nil {
		return fmt.Errorf("marshal assertion: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO stix_edges (id, tenant_id, source_ref, target_ref, relationship_type, payload, created, modified)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (id) DO NOTHING
	`, edgeID, module.SingleTenantUUID, endIDs[0], endIDs[1], relType, payload, at)
	if err != nil {
		return fmt.Errorf("insert assertion: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("insert assertion: %w", err)
	}
	if n == 1 {
		return nil
	}

	// The pair has its row: live says it already, withdrawn is asserted again.
	held, err := lockedEdge(ctx, tx, edgeID)
	if err != nil {
		return err
	}
	if held.AssertedBy == "" {
		return nil // not an assertion: nothing of ours to touch
	}
	// Live, the first asserter stands and a second confirmation is kept in
	// the history; withdrawn, the new asserter asserts it again.
	if held.WithdrawnAt != nil {
		held.AssertedBy, held.AssertedByName, held.AssertedAt, held.Note, held.WithdrawnAt = by.ID, by.Name, at, note, nil
	}
	held.History = append(held.History, event)
	return updateEdge(ctx, tx, edgeID, held, at)
}

// requirePersisted answers a MissingHostError naming the first of refs with no
// persisted object of its expected type behind it (types; an object of
// another type under the uuid is not one, and an identity is a person only
// with identity_class individual), and locks the two rows, in id order, so
// acts on one pair serialize.
func requirePersisted(ctx context.Context, tx *sql.Tx, refs [2]string, ids [2]uuid.UUID, types [2]string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, type FROM stix_objects WHERE tenant_id = $1 AND id = ANY($2) AND type = ANY($3::text[])
		  AND (type <> $4 OR payload->'properties'->>'identity_class' = $5)
		ORDER BY id FOR UPDATE
	`, module.SingleTenantUUID, uuidArray(ids[:]), textArray(types[:]), identity.TypeIdentity, identityClassIndividual)
	if err != nil {
		return fmt.Errorf("query asserted entities: %w", err)
	}
	defer rows.Close()
	found := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var typ string
		if err := rows.Scan(&id, &typ); err != nil {
			return fmt.Errorf("scan asserted entity: %w", err)
		}
		found[id] = typ
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate asserted entities: %w", err)
	}
	for i, id := range ids {
		if found[id] != types[i] {
			return &MissingHostError{Ref: refs[i], Type: types[i]}
		}
	}
	return nil
}

// liveAssertedPairEdge reports whether a person's live edge of relType joins a
// and b in either direction.
func liveAssertedPairEdge(ctx context.Context, tx *sql.Tx, relType string, a, b uuid.UUID) (bool, error) {
	var live bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM stix_edges
		WHERE tenant_id = $1 AND relationship_type = $4
		  AND ((source_ref = $2 AND target_ref = $3) OR (source_ref = $3 AND target_ref = $2))
		  AND payload->>'withdrawn_at' IS NULL AND COALESCE(payload->>'asserted_by', '') <> '')
	`, module.SingleTenantUUID, a, b, relType).Scan(&live); err != nil {
		return false, fmt.Errorf("query live %s edges of the pair: %w", relType, err)
	}
	return live, nil
}

// AssertionEnds reads an aliases, distinct-from or belongs-to edge's two ends
// from its row, lo then hi as stored (an owner edge: the account, then the
// person), and whether it is a source's record (no
// asserter) rather than a person's assertion. No such edge is ErrNotFound.
// The ends' objects are not consulted: an end is an end whether or not its
// object is still persisted.
func (s *Store) AssertionEnds(ctx context.Context, edgeID uuid.UUID) (lo, hi string, observed bool, err error) {
	var src, tgt, srcRef, tgtRef, by string
	err = s.db.QueryRowContext(ctx, `
		SELECT source_ref::text, target_ref::text, COALESCE(payload->>'source_ref', ''), COALESCE(payload->>'target_ref', ''),
		       COALESCE(payload->>'asserted_by', '')
		FROM stix_edges
		WHERE tenant_id = $1 AND id = $2 AND relationship_type IN ('`+relAliases+`', '`+relDistinctFrom+`', '`+relBelongsTo+`')
	`, module.SingleTenantUUID, edgeID).Scan(&src, &tgt, &srcRef, &tgtRef, &by)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, ErrNotFound
	}
	if err != nil {
		return "", "", false, fmt.Errorf("query identity edge ends: %w", err)
	}
	// The row's columns hold the uuids, lowercase; the payload names their
	// type. Identity edges join hosts, so a payload that names none is a host.
	end := func(col, named string) string {
		if typ, rest, found := strings.Cut(named, "--"); found && strings.EqualFold(rest, col) {
			return strings.ToLower(typ) + "--" + col
		}
		return identity.TypeHost + "--" + col
	}
	return end(src, srcRef), end(tgt, tgtRef), by == "", nil
}

// WithdrawAssertion withdraws an asserted aliases, distinct-from or belongs-to
// (owner, adr/0324) edge by its id: the row is marked withdrawn, with a
// history event by by with note, and the reads split (or join) again. It
// answers the edge's two ends, lo then hi. An edge already withdrawn is left
// as it is; an observed edge is ErrObservedEdge; no such identity edge is
// ErrNotFound; withdrawing a join of two persons that would leave an account
// asserted to both is ErrOwnersSeparated. One that loses a deadlock is tried
// once more (withDeadlockRetry).
func (s *Store) WithdrawAssertion(ctx context.Context, edgeID uuid.UUID, by Asserter, note string) (lo, hi string, err error) {
	err = withDeadlockRetry(func() error {
		var e error
		lo, hi, e = s.withdrawOnce(ctx, edgeID, by, note)
		return e
	})
	return lo, hi, err
}

func (s *Store) withdrawOnce(ctx context.Context, edgeID uuid.UUID, by Asserter, note string) (lo, hi string, err error) {
	if by.ID == "" {
		return "", "", ErrNoAsserter
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("begin withdrawal: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	held, err := lockedEdge(ctx, tx, edgeID)
	if err != nil {
		return "", "", err
	}
	// A source's record first: it has no assertion to withdraw, whatever of
	// its ends is persisted.
	if held.AssertedBy == "" {
		if held.Type == relBelongsTo {
			return "", "", ErrObservedOwner
		}
		return "", "", ErrObservedEdge
	}
	// An asserted edge carries its ends in canonical form.
	lo, hi = held.SourceRef, held.TargetRef
	if held.WithdrawnAt != nil {
		return lo, hi, nil
	}
	if held.Type == relAliases && strings.HasPrefix(lo, identity.TypeIdentity+"--") && strings.HasPrefix(hi, identity.TypeIdentity+"--") {
		// Withdrawing a join of two persons must not leave an account
		// asserted to both.
		loID, _ := refUUID(lo)
		hiID, _ := refUUID(hi)
		if err := s.refuseOwnersSeparated(ctx, tx, lo, func(g *aliasGraph) {
			g.joined[loID] = removeOnce(g.joined[loID], hiID)
			g.joined[hiID] = removeOnce(g.joined[hiID], loID)
		}); err != nil {
			return "", "", err
		}
	}
	at := now()
	held.WithdrawnAt = &at
	held.History = append(held.History, AssertionEvent{Act: actWithdrawn, By: by.ID, ByName: by.Name, At: at, Note: note})
	if err := updateEdge(ctx, tx, edgeID, held, at); err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("commit withdrawal: %w", err)
	}
	return lo, hi, nil
}

// removeOnce answers list less one occurrence of id: one edge of a pair that
// may be joined by several.
func removeOnce(list []uuid.UUID, id uuid.UUID) []uuid.UUID {
	if i := slices.Index(list, id); i >= 0 {
		return slices.Delete(slices.Clone(list), i, i+1)
	}
	return list
}

// beforeSeparationLock, when set by a test, runs in refuseOwnersSeparated
// after the person set is read and before it is locked: the window an
// owner assertion commits in.
var beforeSeparationLock func()

// refuseOwnersSeparated keeps one live asserted owner per account alias set
// when a person set separates (a split of two persons, or a withdrawn join
// of two): it reads person's alias set, applies change to that
// neighbourhood's alias graph, and refuses with ErrOwnersSeparated when an
// account alias set has live asserted owners that would land in two
// different resulting person sets. The persons and the accounts asserted to
// them are locked in tx, as AssertOwner locks them, the persons before the
// owners are read and the accounts after.
func (s *Store) refuseOwnersSeparated(ctx context.Context, tx *sql.Tx, person string, change func(*aliasGraph)) error {
	persons, err := s.aliasSets(ctx, []string{person})
	if err != nil {
		return err
	}
	personIDs := refIDs(persons)
	if beforeSeparationLock != nil {
		beforeSeparationLock()
	}
	// The persons first: an owner assertion locks them too, so once they are
	// held no owner can land unseen between the read below and the check.
	if err := lockObjects(ctx, tx, personIDs); err != nil {
		return err
	}
	owners, err := assertedOwnerEdges(ctx, tx, personIDs)
	if err != nil || len(owners) == 0 {
		return err
	}
	accounts := make([]uuid.UUID, 0, len(owners))
	for _, o := range owners {
		accounts = append(accounts, o[0])
	}
	if err := lockObjects(ctx, tx, accounts); err != nil {
		return err
	}
	// The resulting person sets, each named by its least member.
	g, err := s.aliasGraph(ctx, personIDs)
	if err != nil {
		return err
	}
	change(&g)
	contested := g.contested()
	setOf := map[uuid.UUID]string{}
	rep := func(p uuid.UUID) string {
		if r, ok := setOf[p]; ok {
			return r
		}
		r := p.String()
		for _, m := range g.walk(p, contested) {
			if m.String() < r {
				r = m.String()
			}
		}
		setOf[p] = r
		return r
	}
	// The owners of each account alias set, by resulting person set.
	byAccountSet := map[string]map[string]bool{}
	for _, o := range owners {
		set, err := s.aliasSets(ctx, []string{identity.TypeUserAccount + "--" + o[0].String()})
		if err != nil {
			return err
		}
		key := slices.Min(set)
		if byAccountSet[key] == nil {
			byAccountSet[key] = map[string]bool{}
		}
		byAccountSet[key][rep(o[1])] = true
		if len(byAccountSet[key]) > 1 {
			return ErrOwnersSeparated
		}
	}
	return nil
}

// assertedOwnerEdges reads the live asserted belongs-to edges into persons,
// as (account, person) pairs.
func assertedOwnerEdges(ctx context.Context, tx *sql.Tx, persons []uuid.UUID) ([][2]uuid.UUID, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT source_ref, target_ref FROM stix_edges
		WHERE tenant_id = $1 AND relationship_type = '`+relBelongsTo+`' AND target_ref = ANY($2)
		  AND payload->>'withdrawn_at' IS NULL AND COALESCE(payload->>'asserted_by', '') <> ''
		ORDER BY source_ref, target_ref
	`, module.SingleTenantUUID, uuidArray(persons))
	if err != nil {
		return nil, fmt.Errorf("query owner assertions of the persons: %w", err)
	}
	defer rows.Close()
	var out [][2]uuid.UUID
	for rows.Next() {
		var a, p uuid.UUID
		if err := rows.Scan(&a, &p); err != nil {
			return nil, fmt.Errorf("scan owner assertion: %w", err)
		}
		out = append(out, [2]uuid.UUID{a, p})
	}
	return out, rows.Err()
}

// lockedEdge reads an identity edge's payload under a row lock.
func lockedEdge(ctx context.Context, tx *sql.Tx, edgeID uuid.UUID) (assertedEdge, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `
		SELECT payload FROM stix_edges
		WHERE tenant_id = $1 AND id = $2 AND relationship_type IN ('`+relAliases+`', '`+relDistinctFrom+`', '`+relBelongsTo+`')
		FOR UPDATE
	`, module.SingleTenantUUID, edgeID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return assertedEdge{}, ErrNotFound
	}
	if err != nil {
		return assertedEdge{}, fmt.Errorf("query identity edge: %w", err)
	}
	var e assertedEdge
	if err := json.Unmarshal(raw, &e); err != nil {
		return assertedEdge{}, fmt.Errorf("decode identity edge: %w", err)
	}
	return e, nil
}

// updateEdge writes an asserted edge's payload back; the caller commits.
func updateEdge(ctx context.Context, tx *sql.Tx, edgeID uuid.UUID, e assertedEdge, at time.Time) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal assertion: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE stix_edges SET payload = $3, modified = $4 WHERE tenant_id = $1 AND id = $2`,
		module.SingleTenantUUID, edgeID, payload, at); err != nil {
		return fmt.Errorf("update assertion: %w", err)
	}
	return nil
}
