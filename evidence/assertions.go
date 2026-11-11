package evidence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/module"
)

// Identity assertions (adr/0211): the analyst's "same as another host" and
// "not the same host", written as aliases and distinct-from edges in
// stix_edges, and their withdrawal. One pair has one asserted row per
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
)

// MissingHostError names an end of an assertion that is not persisted. It is
// ErrNotFound.
type MissingHostError struct{ Ref string }

func (e *MissingHostError) Error() string { return "evidence: no persisted host " + e.Ref }

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

// AssertAlias records that ref and otherRef are one host: a live aliases edge
// between them, asserted by by with note. A live assertion of the pair, or an
// observed aliases edge between them, already says so and nothing is written;
// a withdrawn assertion of the pair is asserted again. A pair a live
// distinct-from holds apart is ErrSplitLive. ids mints the edge's
// deterministic id over the ordered pair. An end not persisted is a
// MissingHostError.
func (s *Store) AssertAlias(ctx context.Context, ids *identity.Resolver, ref, otherRef string, by Asserter, note string) error {
	return s.assert(ctx, ids, relAliases, ref, otherRef, by, note)
}

// AssertDistinct records that ref and otherRef are two hosts: a live
// distinct-from edge between them, asserted by by with note. It may stand
// beside an aliases edge between them, observed or asserted: cutting that
// join is what it is for. Idempotent and re-assertable as AssertAlias.
func (s *Store) AssertDistinct(ctx context.Context, ids *identity.Resolver, ref, otherRef string, by Asserter, note string) error {
	return s.assert(ctx, ids, relDistinctFrom, ref, otherRef, by, note)
}

// CanonicalHostRef reports whether ref is a host ref (`x-host--<uuid>`, the
// type in any case) and answers it in canonical (lowercase) form, so one host
// has one spelling of its ref.
func CanonicalHostRef(ref string) (string, bool) {
	c, _, ok := canonicalHostRef(ref)
	return c, ok
}

func canonicalHostRef(ref string) (string, uuid.UUID, bool) {
	typ, rest, found := strings.Cut(ref, "--")
	if !found || !strings.EqualFold(typ, identity.TypeHost) {
		return "", uuid.UUID{}, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return "", uuid.UUID{}, false
	}
	return identity.TypeHost + "--" + id.String(), id, true
}

func (s *Store) assert(ctx context.Context, ids *identity.Resolver, relType, ref, otherRef string, by Asserter, note string) error {
	if ids == nil {
		return errors.New("evidence: no identity resolver to mint the edge id")
	}
	if by.ID == "" {
		return ErrNoAsserter
	}
	ref, a, aok := canonicalHostRef(ref)
	otherRef, b, bok := canonicalHostRef(otherRef)
	if !aok || !bok {
		return ErrMalformedRef
	}
	if a == b {
		return fmt.Errorf("evidence: a host cannot be asserted against itself: %w", ErrMalformedRef)
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
	tenant := module.SingleTenantUUID

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin assertion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := requirePersisted(ctx, tx, [2]string{ref, otherRef}, [2]uuid.UUID{a, b}); err != nil {
		return err
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

	at := now()
	event := AssertionEvent{Act: actAsserted, By: by.ID, ByName: by.Name, At: at, Note: note}
	fresh := assertedEdge{
		ID: string(edgeRef), Type: relType, SourceRef: lo, TargetRef: hi,
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
	`, edgeID, tenant, loID, hiID, relType, payload, at)
	if err != nil {
		return fmt.Errorf("insert assertion: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("insert assertion: %w", err)
	}
	if n == 1 {
		return tx.Commit()
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
	if err := updateEdge(ctx, tx, edgeID, held, at); err != nil {
		return err
	}
	return tx.Commit()
}

// requirePersisted answers a MissingHostError naming the first of refs with no
// persisted x-host behind it (an object of another type under the uuid is no
// host), and locks the two host rows, in id order, so acts on one pair
// serialize.
func requirePersisted(ctx context.Context, tx *sql.Tx, refs [2]string, ids [2]uuid.UUID) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM stix_objects WHERE tenant_id = $1 AND type = $3 AND id = ANY($2)
		ORDER BY id FOR UPDATE
	`, module.SingleTenantUUID, uuidArray(ids[:]), identity.TypeHost)
	if err != nil {
		return fmt.Errorf("query asserted hosts: %w", err)
	}
	defer rows.Close()
	found := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan asserted host: %w", err)
		}
		found[id] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate asserted hosts: %w", err)
	}
	for i, id := range ids {
		if !found[id] {
			return &MissingHostError{Ref: refs[i]}
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

// AssertionEnds reads an aliases or distinct-from edge's two ends from its
// row, lo then hi as stored, and whether it is a source's record (no
// asserter) rather than a person's assertion. No such edge is ErrNotFound.
// The ends' objects are not consulted: an end is an end whether or not its
// object is still persisted.
func (s *Store) AssertionEnds(ctx context.Context, edgeID uuid.UUID) (lo, hi string, observed bool, err error) {
	var src, tgt, srcRef, tgtRef, by string
	err = s.db.QueryRowContext(ctx, `
		SELECT source_ref::text, target_ref::text, COALESCE(payload->>'source_ref', ''), COALESCE(payload->>'target_ref', ''),
		       COALESCE(payload->>'asserted_by', '')
		FROM stix_edges
		WHERE tenant_id = $1 AND id = $2 AND relationship_type IN ('`+relAliases+`', '`+relDistinctFrom+`')
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

// WithdrawAssertion withdraws an asserted aliases or distinct-from edge by its
// id: the row is marked withdrawn, with a history event by by with note, and
// the reads split (or join) again. It answers the edge's two ends, lo then
// hi. An edge already withdrawn is left as it is; an observed edge is
// ErrObservedEdge; no such identity edge is ErrNotFound.
func (s *Store) WithdrawAssertion(ctx context.Context, edgeID uuid.UUID, by Asserter, note string) (lo, hi string, err error) {
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
		return "", "", ErrObservedEdge
	}
	// An asserted edge carries its ends in canonical form.
	lo, hi = held.SourceRef, held.TargetRef
	if held.WithdrawnAt != nil {
		return lo, hi, nil
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

// lockedEdge reads an identity edge's payload under a row lock.
func lockedEdge(ctx context.Context, tx *sql.Tx, edgeID uuid.UUID) (assertedEdge, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `
		SELECT payload FROM stix_edges
		WHERE tenant_id = $1 AND id = $2 AND relationship_type IN ('`+relAliases+`', '`+relDistinctFrom+`')
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
