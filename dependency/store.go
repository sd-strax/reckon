// Package dependency tracks the work an investigation's conclusion depends on
// that only someone outside reckon can do: rotate a credential, re-image a
// host, confirm a change (design/ui binding §6 gap 17, adr/0101).
//
// A dependency is a record of its own, CRUD + thin history like the comms
// threads (comms/): it is not an x-action (nothing executes, no blast radius,
// no dispatch) and not a message (the channel that carried the ask owns the
// conversation; a dependency keeps the ask, its status, who closed it and
// why, and a pointer out). Only the investigation aggregate is event-sourced.
//
// Lifecycle: OPEN when the analyst records it; SENT when the notify.* action
// it rides on SUCCEEDS (the one transition the engine makes on its own, from
// its own fact about its own send); CONFIRMED or WAIVED by the analyst, with a
// note. A reply on the channel never moves a dependency: the owner's word is a
// human attestation the analyst records. Resolved is final.
package dependency

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Statuses.
const (
	StatusOpen      = "OPEN"
	StatusSent      = "SENT"
	StatusConfirmed = "CONFIRMED"
	StatusWaived    = "WAIVED"
)

// History events.
const (
	EventCreated   = "created"
	EventAttached  = "attached"
	EventDetached  = "detached"
	EventSent      = "sent"
	EventConfirmed = "confirmed"
	EventWaived    = "waived"
)

// ErrNotFound is returned when no dependency has that id.
var ErrNotFound = errors.New("dependency not found")

// ErrResolved is returned when the dependency is CONFIRMED or WAIVED, which is final.
var ErrResolved = errors.New("dependency already resolved")

// ErrInvalid is returned when the input does not make a dependency (no title, no owner, a
// waive without a note).
var ErrInvalid = errors.New("invalid dependency input")

// HistoryEntry is one event on a dependency: who, when, what, and the note or
// reference it carried.
type HistoryEntry struct {
	At     time.Time `json:"at"`
	By     string    `json:"by"`
	ByName string    `json:"by_name,omitempty"`
	Event  string    `json:"event"`
	Note   string    `json:"note,omitempty"`
	Ref    string    `json:"ref,omitempty"`
}

// Dependency is one piece of external work the conclusion depends on.
type Dependency struct {
	ID          uuid.UUID
	AggregateID uuid.UUID
	TenantID    uuid.UUID
	Title       string // what has to be done
	Owner       string // who outside reckon owes it
	Detail      string // why
	Status      string
	// NotifyActionID is the notify.* action the ask rides on; Nil when the
	// dependency is not attached to a message.
	NotifyActionID uuid.UUID
	// ExternalRef points out to where the work lives in the channel that owns
	// it (a ticket, a thread permalink); set at creation or at resolution.
	ExternalRef    string
	CreatedBy      string
	CreatedByName  string
	CreatedAt      time.Time
	ResolvedBy     string
	ResolvedByName string
	ResolvedAt     sql.NullTime
	ResolutionNote string
	UpdatedAt      time.Time
	History        []HistoryEntry
}

// Resolved reports whether the dependency is final.
func (d Dependency) Resolved() bool {
	return d.Status == StatusConfirmed || d.Status == StatusWaived
}

// Actor is who performs a store act, as the token presented them (adr/0077).
type Actor struct {
	ID   string
	Name string
}

// Create parameterizes Create.
type Create struct {
	AggregateID    uuid.UUID
	TenantID       uuid.UUID
	Title          string
	Owner          string
	Detail         string
	ExternalRef    string
	NotifyActionID uuid.UUID // optional
	By             Actor
	At             time.Time
}

// Store is the dependency CRUD over the main DB.
type Store struct {
	db *sql.DB
}

// NewStore wraps the main DB.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Create records a new OPEN dependency. Title and owner are required.
func (s *Store) Create(ctx context.Context, c Create) (Dependency, error) {
	c.Title, c.Owner, c.Detail = strings.TrimSpace(c.Title), strings.TrimSpace(c.Owner), strings.TrimSpace(c.Detail)
	c.ExternalRef = strings.TrimSpace(c.ExternalRef)
	if c.Title == "" || c.Owner == "" {
		return Dependency{}, fmt.Errorf("%w: title and owner are required", ErrInvalid)
	}
	now := c.At.UTC()
	history := []HistoryEntry{{At: now, By: c.By.ID, ByName: c.By.Name, Event: EventCreated, Ref: c.ExternalRef}}
	if c.NotifyActionID != uuid.Nil {
		history = append(history, HistoryEntry{At: now, By: c.By.ID, ByName: c.By.Name, Event: EventAttached, Ref: c.NotifyActionID.String()})
	}
	raw, err := json.Marshal(history)
	if err != nil {
		return Dependency{}, err
	}
	id := uuid.New()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO dependencies (
			dependency_id, aggregate_id, tenant_id, title, owner, detail, status,
			notify_action_id, external_ref, created_by, created_by_name, created_at,
			updated_at, history
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12, $13)
	`, id, c.AggregateID, c.TenantID, c.Title, c.Owner, c.Detail, StatusOpen,
		nullUUID(c.NotifyActionID), c.ExternalRef, c.By.ID, c.By.Name, now, raw)
	if err != nil {
		return Dependency{}, fmt.Errorf("insert dependency: %w", err)
	}
	return s.Get(ctx, id)
}

// Get reads one dependency.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (Dependency, error) {
	rows, err := s.db.QueryContext(ctx, selectDependencies+` WHERE dependency_id = $1`, id)
	if err != nil {
		return Dependency{}, fmt.Errorf("query dependency: %w", err)
	}
	defer rows.Close()
	out, err := scanDependencies(rows)
	if err != nil {
		return Dependency{}, err
	}
	if len(out) == 0 {
		return Dependency{}, ErrNotFound
	}
	return out[0], nil
}

// List returns an investigation's dependencies, oldest first.
func (s *Store) List(ctx context.Context, aggregateID uuid.UUID) ([]Dependency, error) {
	rows, err := s.db.QueryContext(ctx, selectDependencies+` WHERE aggregate_id = $1 ORDER BY created_at, dependency_id`, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("query dependencies: %w", err)
	}
	defer rows.Close()
	return scanDependencies(rows)
}

// Attach rides the dependency on a notify.* action (Nil detaches it). Legal
// while OPEN; a SENT dependency already rode a sent message, a resolved one is
// final. The caller checks the action belongs to the investigation and is a
// pending notify.* action.
func (s *Store) Attach(ctx context.Context, id, actionID uuid.UUID, by Actor, at time.Time) error {
	event, ref := EventAttached, actionID.String()
	if actionID == uuid.Nil {
		event, ref = EventDetached, ""
	}
	return s.transition(ctx, id, []string{StatusOpen}, "notify_action_id = $4", []any{nullUUID(actionID)},
		HistoryEntry{At: at.UTC(), By: by.ID, ByName: by.Name, Event: event, Ref: ref})
}

// MarkSent moves every OPEN dependency riding on the action to SENT. Called
// from the action-result path when a notify.* dispatch SUCCEEDS; idempotent,
// since only OPEN rows move. Returns how many moved.
func (s *Store) MarkSent(ctx context.Context, actionID uuid.UUID, at time.Time) (int, error) {
	entry, err := json.Marshal([]HistoryEntry{{At: at.UTC(), By: "engine", Event: EventSent, Ref: actionID.String()}})
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE dependencies
		SET status = $2, updated_at = $3, history = history || $4::jsonb
		WHERE notify_action_id = $1 AND status = $5
	`, actionID, StatusSent, at.UTC(), entry, StatusOpen)
	if err != nil {
		return 0, fmt.Errorf("mark dependencies sent: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Confirm records the owner's confirmation as the analyst's act: a note on
// what was confirmed and how, and optionally where the work lives outside.
func (s *Store) Confirm(ctx context.Context, id uuid.UUID, by Actor, note, externalRef string, at time.Time) error {
	return s.resolve(ctx, id, StatusConfirmed, EventConfirmed, by, note, externalRef, at)
}

// Waive records that the dependency is knowingly left to no one: a note is
// required, since a conclusion with work waived is legitimate only when the
// record says why.
func (s *Store) Waive(ctx context.Context, id uuid.UUID, by Actor, note, externalRef string, at time.Time) error {
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("%w: a waive needs a note", ErrInvalid)
	}
	return s.resolve(ctx, id, StatusWaived, EventWaived, by, note, externalRef, at)
}

func (s *Store) resolve(ctx context.Context, id uuid.UUID, status, event string, by Actor, note, externalRef string, at time.Time) error {
	note, externalRef = strings.TrimSpace(note), strings.TrimSpace(externalRef)
	now := at.UTC()
	set := "status = $4, resolved_by = $5, resolved_by_name = $6, resolved_at = $7, resolution_note = $8, external_ref = CASE WHEN $9 <> '' THEN $9 ELSE external_ref END"
	return s.transition(ctx, id, []string{StatusOpen, StatusSent}, set,
		[]any{status, by.ID, by.Name, now, note, externalRef},
		HistoryEntry{At: now, By: by.ID, ByName: by.Name, Event: event, Note: note, Ref: externalRef})
}

// transition applies a SET clause to a dependency in one of the allowed
// statuses and appends a history entry. The clause is a package-internal
// constant; its values are bound from $4 on. A dependency in another status
// is reported as resolved (the only statuses outside OPEN/SENT are final) or
// not found.
func (s *Store) transition(ctx context.Context, id uuid.UUID, from []string, set string, args []any, e HistoryEntry) error {
	entry, err := json.Marshal([]HistoryEntry{e})
	if err != nil {
		return err
	}
	placeholders := make([]string, 0, len(from))
	bound := append([]any{id, e.At, entry}, args...)
	for _, f := range from {
		bound = append(bound, f)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(bound)))
	}
	//nolint:gosec // G201: set and the placeholders are built from constants in this file; every value is bound.
	q := `UPDATE dependencies SET ` + set + `, updated_at = $2, history = history || $3::jsonb
	      WHERE dependency_id = $1 AND status IN (` + strings.Join(placeholders, ", ") + `)`
	res, err := s.db.ExecContext(ctx, q, bound...)
	if err != nil {
		return fmt.Errorf("update dependency: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	cur, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if cur.Resolved() {
		return ErrResolved
	}
	return fmt.Errorf("%w: dependency is %s", ErrInvalid, cur.Status)
}

// OpenCount returns how many dependencies are still OPEN or SENT.
func (s *Store) OpenCount(ctx context.Context, aggregateID uuid.UUID) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM dependencies WHERE aggregate_id = $1 AND status IN ($2, $3)
	`, aggregateID, StatusOpen, StatusSent).Scan(&n)
	return n, err
}

const selectDependencies = `
	SELECT dependency_id, aggregate_id, tenant_id, title, owner, detail, status,
	       notify_action_id, external_ref, created_by, created_by_name, created_at,
	       resolved_by, resolved_by_name, resolved_at, resolution_note, updated_at, history
	FROM dependencies`

func scanDependencies(rows *sql.Rows) ([]Dependency, error) {
	var out []Dependency
	for rows.Next() {
		var d Dependency
		var notify uuid.NullUUID
		var history []byte
		var resolvedBy, resolvedByName, note sql.NullString
		if err := rows.Scan(&d.ID, &d.AggregateID, &d.TenantID, &d.Title, &d.Owner, &d.Detail, &d.Status,
			&notify, &d.ExternalRef, &d.CreatedBy, &d.CreatedByName, &d.CreatedAt,
			&resolvedBy, &resolvedByName, &d.ResolvedAt, &note, &d.UpdatedAt, &history); err != nil {
			return nil, fmt.Errorf("scan dependency: %w", err)
		}
		if notify.Valid {
			d.NotifyActionID = notify.UUID
		}
		d.ResolvedBy, d.ResolvedByName, d.ResolutionNote = resolvedBy.String, resolvedByName.String, note.String
		if len(history) > 0 {
			_ = json.Unmarshal(history, &d.History)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func nullUUID(id uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil}
}
