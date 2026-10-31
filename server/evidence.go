package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/evidence"
)

// GET /api/evidence/{ref} opens any cited ref read-only (design/ui 02 §2.8
// "every citation opens"); the evidence package owns persistence and the reads.

// EvidenceView is one opened citation: either a STIX object or a raw OCSF
// telemetry record (EvidenceRef = StixId | OcsfEventId, 01).
type EvidenceView struct {
	Ref     string          `json:"ref"`
	Kind    string          `json:"kind"` // "stix" | "ocsf"
	Type    string          `json:"type"` // STIX type, or OCSF class_name
	Payload json.RawMessage `json:"payload"`

	// OCSF-only metadata.
	ClassUID   int        `json:"class_uid,omitempty"`
	Time       *time.Time `json:"time,omitempty"`
	RecordedAt *time.Time `json:"recorded_at,omitempty"`
	SourceTool string     `json:"source_tool,omitempty"`

	// Related is set on an `indicator--` ref (a detection finding, 03 §4.12):
	// the sightings that cite it, the vendor identity that produced it, and the
	// observed-data the finding recursed — so the Detection inspector is one
	// fetch (design/ui 08 §8.6.1). Nil for every other type.
	Related *EvidenceRelated `json:"related,omitempty"`
}

// EvidenceRelated is the detection finding's neighbourhood in the object store.
type EvidenceRelated struct {
	Sightings []EvidenceView `json:"sightings"`
	Identity  *EvidenceView  `json:"identity,omitempty"`
	Evidence  []EvidenceView `json:"evidence"`
}

// getEvidence serves GET /api/evidence/{ref}: the citation-open surface. A
// `<type>--<uuid>` ref reads a STIX object; a bare uuid reads a telemetry record.
// Any authenticated reader (enforced by the caller).
func (b *Backend) getEvidence(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Evidence == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "evidence store not configured")
		return
	}
	ref, ok := strings.CutPrefix(strings.TrimSuffix(r.URL.Path, "/"), "/evidence/")
	if !ok || ref == "" {
		writeJSONError(w, http.StatusBadRequest, "path must be /evidence/{ref}")
		return
	}

	if strings.Contains(ref, "--") {
		obj, err := b.cfg.Evidence.Object(r.Context(), ref)
		switch {
		case errors.Is(err, evidence.ErrMalformedRef):
			writeJSONError(w, http.StatusBadRequest, "malformed STIX ref")
			return
		case errors.Is(err, evidence.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, "no persisted object for "+ref)
			return
		case err != nil:
			writeInternalError(w, "query stix object", err)
			return
		}
		v := stixView(obj)
		if obj.Type == "indicator" {
			n, err := b.cfg.Evidence.IndicatorNeighbourhood(r.Context(), obj)
			if err != nil {
				writeInternalError(w, "query related objects", err)
				return
			}
			v.Related = relatedView(n)
		}
		writeJSON(w, http.StatusOK, v)
		return
	}

	id, err := uuid.Parse(ref)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "ref is neither a STIX id nor an event id")
		return
	}
	e, err := b.cfg.Evidence.Event(r.Context(), id)
	if errors.Is(err, evidence.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "no persisted telemetry record "+ref)
		return
	}
	if err != nil {
		writeInternalError(w, "query ocsf event", err)
		return
	}
	writeJSON(w, http.StatusOK, EvidenceView{
		Ref: ref, Kind: "ocsf", Type: e.ClassName, Payload: e.Payload,
		ClassUID: e.ClassUID, Time: &e.Time, RecordedAt: &e.RecordedAt, SourceTool: e.SourceTool,
	})
}

func stixView(o evidence.Object) EvidenceView {
	return EvidenceView{Ref: o.Ref, Kind: "stix", Type: o.Type, Payload: o.Payload}
}

func relatedView(n evidence.Neighbourhood) *EvidenceRelated {
	out := &EvidenceRelated{Sightings: []EvidenceView{}, Evidence: []EvidenceView{}}
	for _, o := range n.Sightings {
		out.Sightings = append(out.Sightings, stixView(o))
	}
	if n.Identity != nil {
		v := stixView(*n.Identity)
		out.Identity = &v
	}
	for _, o := range n.Evidence {
		out.Evidence = append(out.Evidence, stixView(o))
	}
	return out
}
