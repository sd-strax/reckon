package server

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/module"
)

// Cross-investigation appearances + transcript-open: the two "memory" read
// surfaces (design/ui binding §6.1; 02 §2.8/§2.9's data).

// AppearanceView is one investigation a ref appears in.
type AppearanceView struct {
	InvestigationID string     `json:"investigation_id"`
	Title           string     `json:"title"`
	Status          string     `json:"status"`
	SeedSummary     string     `json:"seed_summary,omitempty"`
	FirstSeen       *time.Time `json:"first_seen,omitempty"`
	LastSeen        *time.Time `json:"last_seen,omitempty"`
	Mentions        int        `json:"mentions"`
}

// listRefAppearances serves GET /api/entities/{ref}/appearances: every
// investigation whose thread cites the ref — "appears in N other
// investigations", powered by deterministic identity (03 §7) — or, when the
// evidence store is configured, any member of its alias set (adr/0211), so a
// case that cited a host under the weak id one source gave it is listed under
// the host. Any reader.
func (b *Backend) listRefAppearances(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	ref := r.PathValue("ref")
	refs := []string{ref}
	if b.cfg.Evidence != nil {
		set, err := b.cfg.Evidence.AliasSet(r.Context(), ref)
		switch {
		case errors.Is(err, evidence.ErrMalformedRef):
			// Not a STIX ref the store can walk: the ref alone, as before.
		case err != nil:
			// The appearances are the aggregate's read and stand without the
			// evidence store: the ref alone, the alias set noted as lost.
			slog.Warn("appearances without the alias set", "ref", ref, "error", err)
		default:
			refs = set
		}
	}
	apps, err := b.refAppearances(r, refs)
	if err != nil {
		writeInternalError(w, "list appearances", err)
		return
	}
	out := []AppearanceView{}
	for _, a := range apps {
		v := AppearanceView{
			InvestigationID: a.AggregateID.String(),
			Title:           a.Title,
			Status:          a.Status,
			SeedSummary:     a.SeedSummary,
			Mentions:        a.Mentions,
		}
		if a.FirstSeen.Valid {
			t := a.FirstSeen.Time
			v.FirstSeen = &t
		}
		if a.LastSeen.Valid {
			t := a.LastSeen.Time
			v.LastSeen = &t
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ref": ref, "appearances": out})
}

// refAppearances lists the appearances of every ref, one row per
// investigation: mentions summed, the earliest first sighting and the latest
// last, most recently touched first.
func (b *Backend) refAppearances(r *http.Request, refs []string) ([]aggregate.RefAppearance, error) {
	if len(refs) == 1 {
		return b.cfg.Reads.ListRefAppearances(r.Context(), refs[0])
	}
	var out []aggregate.RefAppearance
	index := map[aggregate.AggregateID]int{}
	for _, ref := range refs {
		apps, err := b.cfg.Reads.ListRefAppearances(r.Context(), ref)
		if err != nil {
			return nil, err
		}
		for _, a := range apps {
			i, ok := index[a.AggregateID]
			if !ok {
				index[a.AggregateID] = len(out)
				out = append(out, a)
				continue
			}
			m := &out[i]
			m.Mentions += a.Mentions
			if a.FirstSeen.Valid && (!m.FirstSeen.Valid || a.FirstSeen.Time.Before(m.FirstSeen.Time)) {
				m.FirstSeen = a.FirstSeen
			}
			if a.LastSeen.Valid && (!m.LastSeen.Valid || a.LastSeen.Time.After(m.LastSeen.Time)) {
				m.LastSeen = a.LastSeen
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastSeen.Time.After(out[j].LastSeen.Time)
	})
	return out, nil
}

// getInterpretationTranscript serves GET /api/interpretations/{id}/transcript:
// the full committed turn record behind a thread step (02 §6 Layer B — the
// content-addressed side store; the event carries the hash, this serves the
// bytes). Any reader: the transcript is the audit record.
func (b *Backend) getInterpretationTranscript(w http.ResponseWriter, r *http.Request, interpID uuid.UUID) {
	if b.cfg.Handler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
		return
	}
	reads := b.cfg.Reads

	rec, err := reads.LoadInterpretation(r.Context(), interpID)
	if errors.Is(err, aggregate.ErrNoRecord) {
		writeJSONError(w, http.StatusNotFound, "no such interpretation")
		return
	}
	if err != nil {
		writeInternalError(w, "read interpretation", err)
		return
	}
	if rec.TranscriptRef == nil {
		writeJSONError(w, http.StatusNotFound, "this reasoning act carries no transcript")
		return
	}

	body, err := reads.TranscriptBody(r.Context(), module.SingleTenantUUID, rec.TranscriptRef.ContentHash)
	if errors.Is(err, aggregate.ErrNoRecord) {
		writeJSONError(w, http.StatusNotFound, "transcript body not in the side store")
		return
	}
	if err != nil {
		writeInternalError(w, "query transcript", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"interpretation_id": interpID.String(),
		"turn_id":           rec.TranscriptRef.TurnID,
		"content_hash":      rec.TranscriptRef.ContentHash,
		"body":              string(body),
	})
}
