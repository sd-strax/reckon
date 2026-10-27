package server

// Owner action items (adr/0101; design/ui/08 §8.4): the read+act surface over
// the owneritems store. Items are created here (by the analyst, or by the
// agent through its tool as a proposal), attached to the notification that
// asks for them, and confirmed or waived by the analyst; SENT is written by
// the action-result path when the notification succeeds, never here.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/module"
	"github.com/sd-strax/reckon/owneritems"
)

// OwnerItemView is one item as served.
type OwnerItemView struct {
	ID               string                    `json:"id"`
	Title            string                    `json:"title"`
	Owner            string                    `json:"owner"`
	Detail           string                    `json:"detail,omitempty"`
	Status           string                    `json:"status"`
	NotifyActionID   string                    `json:"notify_action_id,omitempty"`
	CreatedBy        string                    `json:"created_by"`
	CreatedByName    string                    `json:"created_by_name,omitempty"`
	CreatedByKind    string                    `json:"created_by_kind"`
	CreatedAt        time.Time                 `json:"created_at"`
	SentAt           *time.Time                `json:"sent_at,omitempty"`
	ConfirmedBy      string                    `json:"confirmed_by,omitempty"`
	ConfirmedByName  string                    `json:"confirmed_by_name,omitempty"`
	ConfirmedAt      *time.Time                `json:"confirmed_at,omitempty"`
	ConfirmationNote string                    `json:"confirmation_note,omitempty"`
	UpdatedAt        time.Time                 `json:"updated_at"`
	History          []owneritems.HistoryEntry `json:"history"`
}

func ownerItemView(it owneritems.Item) OwnerItemView {
	v := OwnerItemView{
		ID: it.ID.String(), Title: it.Title, Owner: it.Owner, Detail: it.Detail, Status: it.Status,
		CreatedBy: it.CreatedBy, CreatedByName: it.CreatedByName, CreatedByKind: it.CreatedByKind, CreatedAt: it.CreatedAt,
		ConfirmedBy: it.ConfirmedBy, ConfirmedByName: it.ConfirmedByName, ConfirmationNote: it.ConfirmationNote, UpdatedAt: it.UpdatedAt,
		History: it.History,
	}
	if v.History == nil {
		v.History = []owneritems.HistoryEntry{}
	}
	if it.NotifyActionID != uuid.Nil {
		v.NotifyActionID = it.NotifyActionID.String()
	}
	if it.SentAt.Valid {
		t := it.SentAt.Time
		v.SentAt = &t
	}
	if it.ConfirmedAt.Valid {
		t := it.ConfirmedAt.Time
		v.ConfirmedAt = &t
	}
	return v
}

// OwnerItemBody is the create payload.
type OwnerItemBody struct {
	Title  string `json:"title"`
	Owner  string `json:"owner"`
	Detail string `json:"detail,omitempty"`
}

// investigationOwnerItemsRoute serves GET (any reader) and POST (analyst) on
// /investigations/{id}/owner_items.
func (b *Backend) investigationOwnerItemsRoute(w http.ResponseWriter, r *http.Request) {
	if b.cfg.OwnerItems == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "owner items not configured")
		return
	}
	invID, ok := investigationSubresourceID(r.URL.Path, "owner_items")
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid investigation id in path")
		return
	}
	switch r.Method {
	case http.MethodGet:
		b.requireRolesOrDeny(w, r, anyReader, func(w http.ResponseWriter, r *http.Request) {
			items, err := b.cfg.OwnerItems.List(r.Context(), invID)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "list owner items: "+err.Error())
				return
			}
			out := make([]OwnerItemView, 0, len(items))
			for _, it := range items {
				out = append(out, ownerItemView(it))
			}
			writeJSON(w, http.StatusOK, map[string]any{"owner_items": out})
		})
	case http.MethodPost:
		b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, func(w http.ResponseWriter, r *http.Request) {
			var body OwnerItemBody
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
				writeJSONError(w, http.StatusBadRequest, "bad request body: "+err.Error())
				return
			}
			claims, _ := authz.FromContext(r.Context())
			kind := "HUMAN"
			if claims.DelegateKind != "" {
				kind = "AI_DELEGATED" // the agent proposes; the analyst decides what is asked of whom
			}
			id, err := b.cfg.OwnerItems.Create(r.Context(), owneritems.NewItem{
				AggregateID: invID, TenantID: module.SingleTenantUUID, Title: body.Title, Owner: body.Owner, Detail: body.Detail,
				CreatedBy: claims.Subject, CreatedByName: claims.PreferredUsername, CreatedByKind: kind, At: time.Now(),
			})
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			it, err := b.cfg.OwnerItems.Get(r.Context(), id)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "read owner item: "+err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, ownerItemView(it))
		})
	default:
		methodNotAllowed(w, "GET, POST")
	}
}

// ownerItemsRoute routes POST /owner_items/{id}/(confirm|waive|attach) (analyst).
func (b *Backend) ownerItemsRoute(w http.ResponseWriter, r *http.Request) {
	if b.cfg.OwnerItems == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "owner items not configured")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	rest, ok := strings.CutPrefix(strings.TrimSuffix(r.URL.Path, "/"), "/owner_items/")
	if !ok {
		writeJSONError(w, http.StatusNotFound, "path must be /owner_items/{id}/{confirm|waive|attach}")
		return
	}
	idStr, verb, ok := strings.Cut(rest, "/")
	id, perr := uuid.Parse(idStr)
	if !ok || perr != nil {
		writeJSONError(w, http.StatusBadRequest, "path must be /owner_items/{id}/{confirm|waive|attach}")
		return
	}
	b.requireRolesOrDeny(w, r, []string{authz.RoleAnalyst}, func(w http.ResponseWriter, r *http.Request) {
		claims, _ := authz.FromContext(r.Context())
		if claims.DelegateKind != "" {
			writeJSONError(w, http.StatusForbidden, "confirming, waiving or attaching an owner item is a human act")
			return
		}
		var body struct {
			Note     string `json:"note"`
			Reason   string `json:"reason"`
			ActionID string `json:"action_id"`
		}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
				writeJSONError(w, http.StatusBadRequest, "bad request body: "+err.Error())
				return
			}
		}
		now := time.Now()
		var err error
		switch verb {
		case "confirm":
			err = b.cfg.OwnerItems.Confirm(r.Context(), id, claims.Subject, claims.PreferredUsername, strings.TrimSpace(body.Note), now)
		case "waive":
			err = b.cfg.OwnerItems.Waive(r.Context(), id, claims.Subject, claims.PreferredUsername, strings.TrimSpace(body.Reason), now)
		case "attach":
			actionID, perr := uuid.Parse(body.ActionID)
			if perr != nil {
				writeJSONError(w, http.StatusBadRequest, "action_id must be the notify action's id")
				return
			}
			err = b.cfg.OwnerItems.Attach(r.Context(), id, actionID, claims.Subject, claims.PreferredUsername, now)
		default:
			writeJSONError(w, http.StatusNotFound, "unknown owner item act "+verb)
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		it, gerr := b.cfg.OwnerItems.Get(r.Context(), id)
		if errors.Is(gerr, sql.ErrNoRows) || gerr != nil {
			writeJSON(w, http.StatusOK, map[string]any{"status": verb})
			return
		}
		writeJSON(w, http.StatusOK, ownerItemView(it))
	})
}
