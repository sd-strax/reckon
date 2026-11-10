package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability/ocsfview"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/identity"
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
	// View is the event's rendering (adr/0090): the sentence with its entities,
	// the curated fields and the category word. OCSF only.
	View *ocsfview.View `json:"view,omitempty"`

	// Related is set on an `indicator--` ref (a detection finding, 03 §4.12):
	// the sightings that cite it, the vendor identity that produced it, and the
	// observed-data the finding recursed — so the Detection inspector is one
	// fetch (design/ui 08 §8.6.1). Nil for every other type.
	Related *EvidenceRelated `json:"related,omitempty"`

	// Identity is set on an `x-host--` ref (adr/0211): the aliases and
	// distinct-from edges that touch the host, each with its origin, and what
	// the record has seen under it. Nil for every other type.
	Identity *EvidenceIdentity `json:"identity,omitempty"`
}

// EvidenceIdentity is a host's identity view.
type EvidenceIdentity struct {
	Aliases  []IdentityEdgeView `json:"aliases"`
	Distinct []IdentityEdgeView `json:"distinct"`
	SeenAs   SeenAsView         `json:"seen_as"`
}

// IdentityEdgeView is one aliases or distinct-from edge. Kind "observed" is a
// source's record, Source the tool; "asserted" is a person's, with who, when
// and the note.
type IdentityEdgeView struct {
	EdgeID    string    `json:"edge_id"`
	OtherRef  string    `json:"other_ref"`
	Kind      string    `json:"kind"`
	Source    string    `json:"source,omitempty"`
	By        string    `json:"by,omitempty"`
	ByName    string    `json:"by_name,omitempty"`
	At        time.Time `json:"at"`
	Note      string    `json:"note,omitempty"`
	Withdrawn bool      `json:"withdrawn"`
}

// SeenAsView is what the record has seen under a host: distinct domains, the
// agent and device ids by source, the addresses.
type SeenAsView struct {
	Domains []string       `json:"domains"`
	Agents  []SourceIDView `json:"agents"`
	IPs     []string       `json:"ips"`
}

// SourceIDView is an id a source reported for a host.
type SourceIDView struct {
	Tool string `json:"tool"`
	UID  string `json:"uid"`
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
	ref := r.PathValue("ref")

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
		if obj.Type == identity.TypeHost {
			// The identity view degrades as appearances do: a store error
			// leaves the object readable without it.
			id, err := b.cfg.Evidence.HostIdentity(r.Context(), obj.Ref)
			switch {
			case errors.Is(err, evidence.ErrMalformedRef):
				writeJSONError(w, http.StatusBadRequest, "malformed STIX ref")
				return
			case err != nil:
				slog.Warn("evidence without the identity view", "ref", obj.Ref, "error", err)
			default:
				v.Identity = identityView(id)
			}
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
	entities, err := b.cfg.Evidence.EventEntities(r.Context(), id)
	if err != nil {
		writeInternalError(w, "query derived objects", err)
		return
	}
	view := eventView(e, entities)
	writeJSON(w, http.StatusOK, EvidenceView{
		Ref: ref, Kind: "ocsf", Type: e.ClassName, Payload: e.Payload,
		ClassUID: e.ClassUID, Time: &e.Time, RecordedAt: &e.RecordedAt, SourceTool: e.SourceTool,
		View: &view,
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

func identityView(v evidence.HostIdentityView) *EvidenceIdentity {
	edges := func(in []evidence.IdentityEdge) []IdentityEdgeView {
		out := make([]IdentityEdgeView, 0, len(in))
		for _, e := range in {
			out = append(out, IdentityEdgeView{
				EdgeID: e.EdgeID, OtherRef: e.OtherRef, Kind: e.Kind, Source: e.Source,
				By: e.By, ByName: e.ByName, At: e.At, Note: e.Note, Withdrawn: e.Withdrawn,
			})
		}
		return out
	}
	seen := SeenAsView{Domains: nonNilStrings(v.SeenAs.Domains), Agents: []SourceIDView{}, IPs: nonNilStrings(v.SeenAs.IPs)}
	for _, a := range v.SeenAs.Agents {
		seen.Agents = append(seen.Agents, SourceIDView{Tool: a.Tool, UID: a.UID})
	}
	return &EvidenceIdentity{Aliases: edges(v.Aliases), Distinct: edges(v.Distinct), SeenAs: seen}
}
