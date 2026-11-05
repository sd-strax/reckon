package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/dependency"
)

// The dependencies read+act surface (binding §6 gap 17, adr/0101): the work
// the conclusion depends on that only someone outside reckon can do. The
// analyst creates a dependency, rides it on a notify.* action, and resolves it
// by recording the owner's confirmation or waiving it. Every write is a
// person's act on an open investigation: an AI delegate is refused, as it is
// for an action's outcome, and a concluded record is sealed. The one
// transition these routes never make on their own is OPEN → SENT: the
// action-result path makes it when the notify.* dispatch SUCCEEDS, and the
// routes only catch up a send that completed under them.

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
	claims, ok := dependencyPrincipal(w, r)
	if !ok {
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
	if !b.openInvestigation(w, r, invID) {
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
	d, err := b.cfg.Dependencies.Create(r.Context(), dependency.Create{
		AggregateID: invID, TenantID: newEnvelope(invID, actorFromClaims(claims), commandNow()).TenantID,
		Title: body.Title, Owner: body.Owner, Detail: body.Detail, ExternalRef: body.ExternalRef,
		NotifyActionID: actionID, By: dependencyActor(claims), At: time.Now(),
	})
	if err != nil {
		writeDependencyError(w, err)
		return
	}
	if actionID != uuid.Nil && b.settleSentUnder(r.Context(), actionID) {
		b.serveDependency(w, r, d.ID, http.StatusCreated)
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
	claims, ok := dependencyPrincipal(w, r)
	if !ok {
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
	if !b.openInvestigation(w, r, d.AggregateID) {
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
	// The current ride is the store's to judge, under its locks: a send in
	// flight keeps the dependency (409), one that already succeeded marks it
	// SENT and refuses the move.
	if err := b.cfg.Dependencies.Attach(r.Context(), depID, actionID, dependencyActor(claims), time.Now()); err != nil {
		writeDependencyError(w, err)
		return
	}
	if actionID != uuid.Nil {
		b.settleSentUnder(r.Context(), actionID)
	}
	b.serveDependency(w, r, depID, http.StatusOK)
}

// ResolveDependencyBody is POST /api/dependencies/{id}/confirm|waive: the
// analyst's note (required on waive) and where the work lives outside.
type ResolveDependencyBody struct {
	Note        string `json:"note"`
	ExternalRef string `json:"external_ref"`
}

func (b *Backend) confirmDependency(w http.ResponseWriter, r *http.Request, depID uuid.UUID) {
	// The store is checked before its method is taken: a method value on a nil
	// interface panics, and the advertised answer is 503.
	if !b.dependenciesConfigured(w) {
		return
	}
	b.resolveDependency(w, r, depID, b.cfg.Dependencies.Confirm)
}

func (b *Backend) waiveDependency(w http.ResponseWriter, r *http.Request, depID uuid.UUID) {
	if !b.dependenciesConfigured(w) {
		return
	}
	b.resolveDependency(w, r, depID, b.cfg.Dependencies.Waive)
}

type dependencyResolve func(ctx context.Context, id uuid.UUID, by dependency.Actor, note, externalRef string, at time.Time) error

func (b *Backend) resolveDependency(w http.ResponseWriter, r *http.Request, depID uuid.UUID, act dependencyResolve) {
	claims, ok := dependencyPrincipal(w, r)
	if !ok {
		return
	}
	var body ResolveDependencyBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	d, err := b.cfg.Dependencies.Get(r.Context(), depID)
	if err != nil {
		writeDependencyError(w, err)
		return
	}
	if !b.openInvestigation(w, r, d.AggregateID) {
		return
	}
	if err := act(r.Context(), depID, dependencyActor(claims), body.Note, body.ExternalRef, time.Now()); err != nil {
		writeDependencyError(w, err)
		return
	}
	b.serveDependency(w, r, depID, http.StatusOK)
}

func (b *Backend) serveDependency(w http.ResponseWriter, r *http.Request, depID uuid.UUID, status int) {
	d, err := b.cfg.Dependencies.Get(r.Context(), depID)
	if err != nil {
		writeDependencyError(w, err)
		return
	}
	writeJSON(w, status, dependencyView(d))
}

// dependencyPrincipal reads the caller and refuses an AI delegate. Recording,
// attaching, confirming and waiving are the analyst's acts, and a confirmation
// is an attestation in the person's name (adr/0101, as adr/0054 for a manual
// test); the analyst route role admits a delegate token carrying the human's
// subject, so the handler has to look.
func dependencyPrincipal(w http.ResponseWriter, r *http.Request) (authz.Claims, bool) {
	claims, ok := authz.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "auth context missing")
		return authz.Claims{}, false
	}
	if claims.DelegateKind != "" {
		writeJSONError(w, http.StatusForbidden, "an AI delegate cannot record or resolve a dependency")
		return authz.Claims{}, false
	}
	return claims, true
}

// openInvestigation checks that the investigation exists and is still open to
// its record changing: not found 404; CONCLUDED, ARCHIVED or DISCARDED 409. A
// concluded investigation's dependencies are part of what was exported and
// signed, so they are as sealed as the rest of it.
func (b *Backend) openInvestigation(w http.ResponseWriter, r *http.Request, invID uuid.UUID) bool {
	ic, err := b.cfg.Reads.LoadInvestigationCurrent(r.Context(), invID)
	if errors.Is(err, aggregate.ErrNoRecord) {
		writeJSONError(w, http.StatusNotFound, "investigation not found")
		return false
	}
	if err != nil {
		writeInternalError(w, "load investigation", err)
		return false
	}
	switch ic.Status {
	case aggregate.StatusConcluded, aggregate.StatusArchived, aggregate.StatusDiscarded:
		writeJSONError(w, http.StatusConflict, "investigation is "+ic.Status+"; its dependencies are part of the concluded record")
		return false
	}
	return true
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

// settleSentUnder catches the send that completed between the pending check
// and the write: the action is read again after the attachment, and if it
// SUCCEEDED meanwhile the dependencies riding on it move to SENT here, since
// the result activity ran before there was anything to move. Reports whether
// anything moved. Best-effort: a failure logs and the record still holds the
// attachment.
func (b *Backend) settleSentUnder(ctx context.Context, actionID uuid.UUID) bool {
	ac, err := b.cfg.Reads.LoadActionCurrent(ctx, actionID)
	if err != nil || ac.Status != aggregate.ActionStatusSucceeded {
		return false
	}
	return b.markDependenciesSent(ctx, ac) > 0
}

// markDependenciesSent moves the OPEN dependencies riding on a SUCCEEDED
// notify.* action to SENT (adr/0101): the engine's own fact about its own
// send. Idempotent; a failure logs, since the action record already holds the
// result and reconcileDependencies will move the rows on its next pass. The
// result activity is the usual caller's twin; this one serves the routes that
// establish a success without it (a person resolving an OUTCOME_UNKNOWN send,
// an attachment racing the send's completion) and the reconciler.
func (b *Backend) markDependenciesSent(ctx context.Context, ac aggregate.ActionCurrent) int {
	if b.cfg.Dependencies == nil || ac.Status != aggregate.ActionStatusSucceeded || !strings.HasPrefix(ac.ActionType, "notify.") {
		return 0
	}
	n, err := b.cfg.Dependencies.MarkSent(ctx, ac.ActionID, time.Now().UTC())
	if err != nil {
		slog.Warn("dependencies not marked sent", "action_id", ac.ActionID, "error", err)
		return 0
	}
	return n
}

func dependencyActor(c authz.Claims) dependency.Actor {
	return dependency.Actor{ID: c.Subject, Name: c.PreferredUsername}
}

// writeDependencyError maps the store's errors: not found 404; resolved,
// sealed by the investigation's closure, or riding on a send in flight 409;
// invalid input 422; anything else 500.
func writeDependencyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, dependency.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, dependency.ErrResolved), errors.Is(err, dependency.ErrClosed), errors.Is(err, dependency.ErrInFlight):
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
