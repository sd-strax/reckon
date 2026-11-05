package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/dependency"
)

// The dependencies read+act surface (binding §6 gap 17, adr/0101): the work
// the conclusion depends on that only someone outside reckon can do. The
// analyst creates a dependency, rides it on a notify.* action, and resolves it
// by recording the owner's confirmation or waiving it. The one transition
// these routes never make is OPEN → SENT: the action-result path makes it when
// the notify.* dispatch SUCCEEDS.

// DependencyHistoryView is one event on a dependency as served.
type DependencyHistoryView struct {
	At     time.Time `json:"at"`
	By     string    `json:"by"`
	ByName string    `json:"by_name,omitempty"`
	Event  string    `json:"event"`
	Note   string    `json:"note,omitempty"`
	Ref    string    `json:"ref,omitempty"`
}

// DependencyView is one dependency as served.
type DependencyView struct {
	DependencyID   string                  `json:"dependency_id"`
	Title          string                  `json:"title"`
	Owner          string                  `json:"owner"`
	Detail         string                  `json:"detail,omitempty"`
	Status         string                  `json:"status"`
	NotifyActionID string                  `json:"notify_action_id,omitempty"`
	ExternalRef    string                  `json:"external_ref,omitempty"`
	CreatedBy      string                  `json:"created_by"`
	CreatedByName  string                  `json:"created_by_name,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
	ResolvedBy     string                  `json:"resolved_by,omitempty"`
	ResolvedByName string                  `json:"resolved_by_name,omitempty"`
	ResolvedAt     *time.Time              `json:"resolved_at,omitempty"`
	ResolutionNote string                  `json:"resolution_note,omitempty"`
	History        []DependencyHistoryView `json:"history,omitempty"`
}

func dependencyView(d dependency.Dependency) DependencyView {
	v := DependencyView{
		DependencyID:   d.ID.String(),
		Title:          d.Title,
		Owner:          d.Owner,
		Detail:         d.Detail,
		Status:         d.Status,
		ExternalRef:    d.ExternalRef,
		CreatedBy:      d.CreatedBy,
		CreatedByName:  d.CreatedByName,
		CreatedAt:      d.CreatedAt,
		ResolvedBy:     d.ResolvedBy,
		ResolvedByName: d.ResolvedByName,
		ResolutionNote: d.ResolutionNote,
	}
	if d.NotifyActionID != uuid.Nil {
		v.NotifyActionID = d.NotifyActionID.String()
	}
	if d.ResolvedAt.Valid {
		at := d.ResolvedAt.Time
		v.ResolvedAt = &at
	}
	for _, e := range d.History {
		v.History = append(v.History, DependencyHistoryView(e))
	}
	return v
}

// listInvestigationDependencies serves GET /api/investigations/{id}/dependencies.
func (b *Backend) listInvestigationDependencies(w http.ResponseWriter, r *http.Request, invID uuid.UUID) {
	if !b.dependenciesConfigured(w) {
		return
	}
	deps, err := b.cfg.Dependencies.List(r.Context(), invID)
	if err != nil {
		writeInternalError(w, "list dependencies", err)
		return
	}
	out := make([]DependencyView, 0, len(deps))
	for _, d := range deps {
		out = append(out, dependencyView(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"dependencies": out})
}

// CreateDependencyBody is POST /api/investigations/{id}/dependencies.
type CreateDependencyBody struct {
	Title          string `json:"title"`
	Owner          string `json:"owner"`
	Detail         string `json:"detail"`
	ExternalRef    string `json:"external_ref"`
	NotifyActionID string `json:"notify_action_id"`
}

// createDependency records a new OPEN dependency, optionally riding on a
// pending notify.* action of the same investigation.
func (b *Backend) createDependency(w http.ResponseWriter, r *http.Request, invID uuid.UUID) {
	if !b.dependenciesConfigured(w) {
		return
	}
	var body CreateDependencyBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	if strings.TrimSpace(body.Title) == "" || strings.TrimSpace(body.Owner) == "" {
		writeJSONError(w, http.StatusBadRequest, "title and owner are required")
		return
	}
	actionID := uuid.Nil
	if body.NotifyActionID != "" {
		id, ok := b.notifyActionOf(w, r, invID, body.NotifyActionID)
		if !ok {
			return
		}
		actionID = id
	}
	claims, _ := authz.FromContext(r.Context())
	d, err := b.cfg.Dependencies.Create(r.Context(), dependency.Create{
		AggregateID: invID, TenantID: newEnvelope(invID, actorFromClaims(claims), commandNow()).TenantID,
		Title: body.Title, Owner: body.Owner, Detail: body.Detail, ExternalRef: body.ExternalRef,
		NotifyActionID: actionID, By: dependencyActor(claims), At: time.Now(),
	})
	if err != nil {
		writeDependencyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dependencyView(d))
}

// AttachDependencyBody is POST /api/dependencies/{id}/attach: the notify.*
// action the ask rides on; empty detaches.
type AttachDependencyBody struct {
	NotifyActionID string `json:"notify_action_id"`
}

func (b *Backend) attachDependency(w http.ResponseWriter, r *http.Request, depID uuid.UUID) {
	if !b.dependenciesConfigured(w) {
		return
	}
	var body AttachDependencyBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	d, err := b.cfg.Dependencies.Get(r.Context(), depID)
	if err != nil {
		writeDependencyError(w, err)
		return
	}
	actionID := uuid.Nil
	if body.NotifyActionID != "" {
		id, ok := b.notifyActionOf(w, r, d.AggregateID, body.NotifyActionID)
		if !ok {
			return
		}
		actionID = id
	}
	claims, _ := authz.FromContext(r.Context())
	if err := b.cfg.Dependencies.Attach(r.Context(), depID, actionID, dependencyActor(claims), time.Now()); err != nil {
		writeDependencyError(w, err)
		return
	}
	b.serveDependency(w, r, depID)
}

// ResolveDependencyBody is POST /api/dependencies/{id}/confirm|waive: the
// analyst's note (required on waive) and where the work lives outside.
type ResolveDependencyBody struct {
	Note        string `json:"note"`
	ExternalRef string `json:"external_ref"`
}

func (b *Backend) confirmDependency(w http.ResponseWriter, r *http.Request, depID uuid.UUID) {
	b.resolveDependency(w, r, depID, b.cfg.Dependencies.Confirm)
}

func (b *Backend) waiveDependency(w http.ResponseWriter, r *http.Request, depID uuid.UUID) {
	b.resolveDependency(w, r, depID, b.cfg.Dependencies.Waive)
}

type dependencyResolve func(ctx context.Context, id uuid.UUID, by dependency.Actor, note, externalRef string, at time.Time) error

func (b *Backend) resolveDependency(w http.ResponseWriter, r *http.Request, depID uuid.UUID, act dependencyResolve) {
	if !b.dependenciesConfigured(w) {
		return
	}
	var body ResolveDependencyBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	claims, _ := authz.FromContext(r.Context())
	if err := act(r.Context(), depID, dependencyActor(claims), body.Note, body.ExternalRef, time.Now()); err != nil {
		writeDependencyError(w, err)
		return
	}
	b.serveDependency(w, r, depID)
}

func (b *Backend) serveDependency(w http.ResponseWriter, r *http.Request, depID uuid.UUID) {
	d, err := b.cfg.Dependencies.Get(r.Context(), depID)
	if err != nil {
		writeDependencyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dependencyView(d))
}

// notifyActionOf checks that the named action is a notify.* action of the
// investigation that is still pending (a dependency rides on a message that
// has not gone out yet; a sent one already moved it). It writes the error.
func (b *Backend) notifyActionOf(w http.ResponseWriter, r *http.Request, invID uuid.UUID, raw string) (uuid.UUID, bool) {
	actionID, err := uuid.Parse(raw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "notify_action_id is not a uuid")
		return uuid.Nil, false
	}
	ac, err := b.cfg.Reads.LoadActionCurrent(r.Context(), actionID)
	if err != nil || ac.AggregateID != invID {
		writeJSONError(w, http.StatusUnprocessableEntity, "notify_action_id is not an action of this investigation")
		return uuid.Nil, false
	}
	if !strings.HasPrefix(ac.ActionType, "notify.") {
		writeJSONError(w, http.StatusUnprocessableEntity, "a dependency rides on a notify.* action")
		return uuid.Nil, false
	}
	switch ac.Status {
	case "REQUESTED", "PENDING_SECONDARY", "APPROVED", "EXECUTING":
		return actionID, true
	default:
		writeJSONError(w, http.StatusUnprocessableEntity, "the notify action is "+ac.Status+"; a dependency rides on one not yet sent")
		return uuid.Nil, false
	}
}

func dependencyActor(c authz.Claims) dependency.Actor {
	return dependency.Actor{ID: c.Subject, Name: c.PreferredUsername}
}

// writeDependencyError maps the store's errors: not found 404, resolved 409,
// invalid input 422, anything else 500.
func writeDependencyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, dependency.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, dependency.ErrResolved):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, dependency.ErrInvalid):
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeInternalError(w, "dependency", err)
	}
}

// dependenciesConfigured answers 503 when there is no dependency store.
func (b *Backend) dependenciesConfigured(w http.ResponseWriter) bool {
	if b.cfg.Dependencies == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "dependencies not configured")
		return false
	}
	return true
}
