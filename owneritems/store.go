// Package owneritems tracks owner action items (adr/0101; design/ui/08 §8.4):
// work only an owner outside reckon can do — rotate a credential, confirm a
// change, re-image a host — tracked with the investigation until the owner
// confirms it or the analyst waives it.
//
// An item is its own record, CRUD + thin history, modeled on the comms
// threads: it executes nothing in any tool, carries no blast radius and never
// dispatches, so it is not an x-action and does not pass Gate 2 or the
// dispatch ledger. "Add to notification" attaches it to the notify.* action
// that asks the owner for it; when that action SUCCEEDS the result path moves
// the item to SENT. The analyst records the owner's confirmation (CONFIRMED,
// with a note) or waives it (WAIVED, with a reason). The conclude preflight
// counts OPEN and SENT items (decision support at v0, like comms).
package owneritems

import (
	"context"
	"database/sql"
	"encoding/json"
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

// HistoryEntry is one step of an item's thin history.
type HistoryEntry struct {
	At        time.Time `json:"at"`
	Actor     string    `json:"actor"`
	ActorName string    `json:"actor_name,omitempty"`
	Act       string    `json:"act"` // created | attached | sent | confirmed | waived
	Note      string    `json:"note,omitempty"`
}

// Item is one owner action item.
type Item struct {
	ID               uuid.UUID
	AggregateID      uuid.UUID
	TenantID         uuid.UUID
	Title            string
	Owner            string
	Detail           string
	Status           string
	NotifyActionID   uuid.UUID // zero when not attached
	CreatedBy        string
	CreatedByName    string
	CreatedByKind    string // HUMAN | AI_DELEGATED
	CreatedAt        time.Time
	SentAt           sql.NullTime
	ConfirmedBy      string
	ConfirmedByName  string
	ConfirmedAt      sql.NullTime
	ConfirmationNote string
	UpdatedAt        time.Time
	History          []HistoryEntry
}

// Store is the owner-items CRUD over the main DB.
type Store struct{ db *sql.DB }

// NewStore wraps the main DB.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// NewItem is what a create carries.
type NewItem struct {
	AggregateID   uuid.UUID
	TenantID      uuid.UUID
	Title         string
	Owner         string
	Detail        string
	CreatedBy     string
	CreatedByName string
	CreatedByKind string
	At            time.Time
}

// Create records a new OPEN item and returns its id.
func (s *Store) Create(ctx context.Context, n NewItem) (uuid.UUID, error) {
	if strings.TrimSpace(n.Title) == "" || strings.TrimSpace(n.Owner) == "" {
		return uuid.Nil, fmt.Errorf("owner item: title and owner are required")
	}
	if n.CreatedBy == "" {
		return uuid.Nil, fmt.Errorf("owner item: created_by is required")
	}
	kind := n.CreatedByKind
	if kind == "" {
		kind = "HUMAN"
	}
	id := uuid.Must(uuid.NewV7())
	now := n.At.UTC()
	hist, err := json.Marshal([]HistoryEntry{{At: now, Actor: n.CreatedBy, ActorName: n.CreatedByName, Act: "created"}})
	if err != nil {
		return uuid.Nil, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO owner_items (id, tenant_id, aggregate_id, title, owner, detail, status, created_by, created_by_name, created_by_kind, created_at, updated_at, history)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11, $12)
	`, id, n.TenantID, n.AggregateID, strings.TrimSpace(n.Title), strings.TrimSpace(n.Owner), strings.TrimSpace(n.Detail), StatusOpen, n.CreatedBy, n.CreatedByName, kind, now, hist)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert owner_items: %w", err)
	}
	return id, nil
}

// Attach names the notify.* action that asks the owner for the item. The
// status stays OPEN until that action succeeds (MarkSent).
func (s *Store) Attach(ctx context.Context, id, actionID uuid.UUID, principal, name string, at time.Time) error {
	return s.step(ctx, id, HistoryEntry{At: at.UTC(), Actor: principal, ActorName: name, Act: "attached", Note: actionID.String()},
		`notify_action_id = $4`, actionID)
}

// MarkSent moves every OPEN item attached to a succeeded notify.* action to
// SENT. Called from the action-result path; only a SUCCEEDED dispatch lands
// here, so an item never claims to have been asked when it was not.
func (s *Store) MarkSent(ctx context.Context, actionID uuid.UUID, at time.Time) (int, error) {
	now := at.UTC()
	entry, err := json.Marshal([]HistoryEntry{{At: now, Actor: "system", Act: "sent", Note: actionID.String()}})
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE owner_items SET status = $2, sent_at = $3, updated_at = $3, history = history || $4::jsonb
		WHERE notify_action_id = $1 AND status = $5
	`, actionID, StatusSent, now, entry, StatusOpen)
	if err != nil {
		return 0, fmt.Errorf("mark owner items sent: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Confirm records the owner's confirmation as the analyst reports it.
func (s *Store) Confirm(ctx context.Context, id uuid.UUID, principal, name, note string, at time.Time) error {
	return s.step(ctx, id, HistoryEntry{At: at.UTC(), Actor: principal, ActorName: name, Act: "confirmed", Note: note},
		`status = '`+StatusConfirmed+`', confirmed_by = $4, confirmed_by_name = $5, confirmed_at = $2, confirmation_note = $6`, principal, name, note)
}

// Waive closes the item without the owner's confirmation, with the reason.
func (s *Store) Waive(ctx context.Context, id uuid.UUID, principal, name, reason string, at time.Time) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("owner item: a waive needs a reason")
	}
	return s.step(ctx, id, HistoryEntry{At: at.UTC(), Actor: principal, ActorName: name, Act: "waived", Note: reason},
		`status = '`+StatusWaived+`', confirmed_by = $4, confirmed_by_name = $5, confirmed_at = $2, confirmation_note = $6`, principal, name, reason)
}

// step appends one history entry and applies a fixed SET clause (a
// package-internal constant expression, never input) to an item still open
// (OPEN or SENT). The extra args bind from $4 on.
func (s *Store) step(ctx context.Context, id uuid.UUID, e HistoryEntry, set string, args ...any) error {
	entry, err := json.Marshal([]HistoryEntry{e})
	if err != nil {
		return err
	}
	//nolint:gosec // G201: set is a compile-time constant clause from this file.
	q := `UPDATE owner_items SET ` + set + `, updated_at = $2, history = history || $3::jsonb
	      WHERE id = $1 AND status IN ('` + StatusOpen + `', '` + StatusSent + `')`
	all := append([]any{id, e.At, entry}, args...)
	res, err := s.db.ExecContext(ctx, q, all...)
	if err != nil {
		return fmt.Errorf("update owner_items: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("owner item %s not found or already closed", id)
	}
	return nil
}

// List returns an investigation's items, oldest first.
func (s *Store) List(ctx context.Context, aggregateID uuid.UUID) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, aggregate_id, title, owner, detail, status, notify_action_id, created_by, created_by_name, created_by_kind,
		       created_at, sent_at, confirmed_by, confirmed_by_name, confirmed_at, confirmation_note, updated_at, history
		FROM owner_items WHERE aggregate_id = $1 ORDER BY created_at, id
	`, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("query owner_items: %w", err)
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var it Item
		var notify uuid.NullUUID
		var confirmedBy, confirmedByName, note sql.NullString
		var hist []byte
		if err := rows.Scan(&it.ID, &it.TenantID, &it.AggregateID, &it.Title, &it.Owner, &it.Detail, &it.Status, &notify, &it.CreatedBy, &it.CreatedByName, &it.CreatedByKind,
			&it.CreatedAt, &it.SentAt, &confirmedBy, &confirmedByName, &it.ConfirmedAt, &note, &it.UpdatedAt, &hist); err != nil {
			return nil, fmt.Errorf("scan owner item: %w", err)
		}
		if notify.Valid {
			it.NotifyActionID = notify.UUID
		}
		it.ConfirmedBy, it.ConfirmedByName, it.ConfirmationNote = confirmedBy.String, confirmedByName.String, note.String
		if len(hist) > 0 {
			_ = json.Unmarshal(hist, &it.History)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Get returns one item.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (Item, error) {
	var agg uuid.UUID
	if err := s.db.QueryRowContext(ctx, `SELECT aggregate_id FROM owner_items WHERE id = $1`, id).Scan(&agg); err != nil {
		return Item{}, err
	}
	items, err := s.List(ctx, agg)
	if err != nil {
		return Item{}, err
	}
	for _, it := range items {
		if it.ID == id {
			return it, nil
		}
	}
	return Item{}, sql.ErrNoRows
}

// OpenCount returns how many items still await the owner (OPEN or SENT):
// the conclude preflight's input.
func (s *Store) OpenCount(ctx context.Context, aggregateID uuid.UUID) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM owner_items WHERE aggregate_id = $1 AND status IN ($2, $3)`, aggregateID, StatusOpen, StatusSent).Scan(&n)
	return n, err
}
