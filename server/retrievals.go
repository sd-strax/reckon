package server

// The retrieved-excerpt reads (design/06 §6.2 Layer B; adr/0097): what a
// turn's provenance says the model saw of a procedure, with where in the
// source it sat and whether the procedure has changed since.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/knowledge"
	"github.com/sd-strax/reckon/module"
)

// RetrievalView is one retrieved excerpt as a turn consulted it.
type RetrievalView struct {
	InterpretationID string          `json:"interpretation_id"`
	SOPID            string          `json:"sop_id"`
	ExcerptHash      string          `json:"excerpt_hash"`
	RevisionHash     string          `json:"revision_hash"`
	Excerpt          string          `json:"excerpt"`
	Location         json.RawMessage `json:"location,omitempty"`
	Score            float64         `json:"score,omitempty"`
	Band             string          `json:"band,omitempty"`
	RetrievedAt      time.Time       `json:"retrieved_at"`
	// MatchesCurrent says the revision retrieved is the procedure's current
	// one; nil when the knowledge service could not be asked.
	MatchesCurrent *bool  `json:"matches_current,omitempty"`
	CurrentHash    string `json:"current_hash,omitempty"`
	// ChunkIndex / ChunkCount: the excerpt's place among the revision's
	// retrievable chunks. The substrate serves one budgeted excerpt per
	// revision today, so these read 1 of 1.
	ChunkIndex int `json:"chunk_index"`
	ChunkCount int `json:"chunk_count"`
}

// investigationRetrievalsRoute serves GET /investigations/{id}/retrievals[?sop=<id>]:
// the excerpts this investigation's turns retrieved, oldest first.
func (b *Backend) investigationRetrievalsRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	b.requireRolesOrDeny(w, r, anyReader, func(w http.ResponseWriter, r *http.Request) {
		invID, ok := investigationSubresourceID(r.URL.Path, "retrievals")
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid investigation id in path")
			return
		}
		if b.cfg.Handler == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "aggregate handler not configured")
			return
		}
		var sopFilter any
		if v := r.URL.Query().Get("sop"); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "sop is not an id")
				return
			}
			sopFilter = id
		}
		rows, err := b.cfg.Handler.DB().QueryContext(r.Context(), `
			SELECT u.interpretation_id, k.sop_id, k.excerpt_hash, k.revision_hash, k.excerpt, k.location, u.score, u.band, u.retrieved_at
			FROM knowledge_retrieval_uses u
			JOIN knowledge_retrievals k ON k.tenant_id = u.tenant_id AND k.excerpt_hash = u.excerpt_hash
			WHERE u.tenant_id = $1 AND u.investigation_id = $2 AND ($3::uuid IS NULL OR k.sop_id = $3::uuid)
			ORDER BY u.retrieved_at, u.interpretation_id
		`, module.SingleTenantUUID, invID, sopFilter)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "retrievals: "+err.Error())
			return
		}
		defer rows.Close()
		out := []RetrievalView{}
		current := map[string]string{}
		for rows.Next() {
			var v RetrievalView
			var interp, sop uuid.UUID
			var loc []byte
			var score sql.NullFloat64
			var band sql.NullString
			if err := rows.Scan(&interp, &sop, &v.ExcerptHash, &v.RevisionHash, &v.Excerpt, &loc, &score, &band, &v.RetrievedAt); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "scan retrieval: "+err.Error())
				return
			}
			v.InterpretationID, v.SOPID, v.Score, v.Band, v.ChunkIndex, v.ChunkCount = interp.String(), sop.String(), score.Float64, band.String, 1, 1
			if len(loc) > 0 {
				v.Location = loc
			}
			if b.cfg.Knowledge != nil {
				h, seen := current[v.SOPID]
				if !seen {
					if s, err := b.cfg.Knowledge.Get(r.Context(), module.SingleTenantUUID, sop); err == nil {
						h = s.ContentHash
					}
					current[v.SOPID] = h
				}
				if h != "" {
					m := h == v.RevisionHash
					v.MatchesCurrent, v.CurrentHash = &m, h
				}
			}
			out = append(out, v)
		}
		if err := rows.Err(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "retrievals: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"retrievals": out})
	})
}

var _ = knowledge.ErrNotFound
