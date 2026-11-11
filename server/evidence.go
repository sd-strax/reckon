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

	// AccountIdentity is set on a `user-account--` ref (adr/0324): the
	// aliases and distinct-from edges that touch the account, what its alias
	// set has been seen as, and the persons it belongs to, each with its
	// origin. Nil for every other type; an account with no owner has an
	// empty owners list.
	AccountIdentity *EvidenceAccountIdentity `json:"account_identity,omitempty"`

	// PersonIdentity is set on an `identity--` ref that is a person
	// (identity_class individual, adr/0324): the aliases and distinct-from
	// edges between it and other persons, and the accounts attached to it,
	// each with the record or person that attached it. Nil for every other
	// type, a vendor identity included.
	PersonIdentity *EvidencePersonIdentity `json:"person_identity,omitempty"`
}

// EvidenceAccountIdentity is an account's identity view:
//
//	{"aliases": [IdentityEdgeView], "distinct": [IdentityEdgeView],
//	 "seen_as": {"logins": [string], "ids": [{"tool","uid"}], "principal_names": [string]},
//	 "owners": [{"person_ref", "account_ref", "edge_id", "kind", "source"?, "by"?,
//	             "by_name"?, "at", "note"?, "withdrawn", "cut"}]}
type EvidenceAccountIdentity struct {
	Aliases  []IdentityEdgeView `json:"aliases"`
	Distinct []IdentityEdgeView `json:"distinct"`
	SeenAs   AccountSeenAsView  `json:"seen_as"`
	Owners   []OwnerView        `json:"owners"`
}

// AccountSeenAsView is what the record has seen under an account: the logins
// as sources wrote them, the identifiers by source, the principal names.
type AccountSeenAsView struct {
	Logins         []string       `json:"logins"`
	IDs            []SourceIDView `json:"ids"`
	PrincipalNames []string       `json:"principal_names"`
}

// OwnerView is one belongs-to edge from an account of the alias set
// (AccountRef) to a person: observed (a directory's record, Source the tool)
// or asserted (a person's, with who, when and the note).
type OwnerView struct {
	PersonRef  string    `json:"person_ref"`
	AccountRef string    `json:"account_ref"`
	EdgeID     string    `json:"edge_id"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source,omitempty"`
	By         string    `json:"by,omitempty"`
	ByName     string    `json:"by_name,omitempty"`
	At         time.Time `json:"at"`
	Note       string    `json:"note,omitempty"`
	Withdrawn  bool      `json:"withdrawn"`
	// Cut is set when a live distinct-from says the account is not the
	// person's (adr/0324): the edge is listed, the reads do not follow it.
	Cut bool `json:"cut"`
}

// EvidencePersonIdentity is a person's identity view:
//
//	{"aliases": [IdentityEdgeView], "distinct": [IdentityEdgeView],
//	 "accounts": [{"account_ref", "edge_id", "kind", "source"?, "by"?, "by_name"?,
//	               "at", "note"?, "withdrawn", "cut"}]}
type EvidencePersonIdentity struct {
	Aliases  []IdentityEdgeView  `json:"aliases"`
	Distinct []IdentityEdgeView  `json:"distinct"`
	Accounts []PersonAccountView `json:"accounts"`
}

// PersonAccountView is one account attached to a person, with its origin.
type PersonAccountView struct {
	AccountRef string    `json:"account_ref"`
	EdgeID     string    `json:"edge_id"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source,omitempty"`
	By         string    `json:"by,omitempty"`
	ByName     string    `json:"by_name,omitempty"`
	At         time.Time `json:"at"`
	Note       string    `json:"note,omitempty"`
	Withdrawn  bool      `json:"withdrawn"`
	// Cut is set when a live distinct-from says the account is not the
	// person's (adr/0324): listed, as a cut alias is, not read.
	Cut bool `json:"cut"`
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
		// The identity view degrades as appearances do: a store error leaves
		// the object readable without it.
		if err := b.attachIdentity(r, obj, &v); err != nil {
			if errors.Is(err, evidence.ErrMalformedRef) {
				writeJSONError(w, http.StatusBadRequest, "malformed STIX ref")
				return
			}
			slog.Warn("evidence without the identity view", "ref", obj.Ref, "error", err)
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

// attachIdentity sets the identity view the object's type carries: a host's
// (adr/0211), an account's or a person's (adr/0324). Any other object, a
// vendor identity among them, carries none.
func (b *Backend) attachIdentity(r *http.Request, obj evidence.Object, v *EvidenceView) error {
	ctx := r.Context()
	switch {
	case obj.Type == identity.TypeHost:
		id, err := b.cfg.Evidence.HostIdentity(ctx, obj.Ref)
		if err != nil {
			return err
		}
		v.Identity = identityView(id)
	case obj.Type == identity.TypeUserAccount:
		id, err := b.cfg.Evidence.AccountIdentity(ctx, obj.Ref)
		if err != nil {
			return err
		}
		v.AccountIdentity = accountIdentityView(id)
	case obj.Type == identity.TypeIdentity && isPerson(obj.Payload):
		id, err := b.cfg.Evidence.PersonIdentity(ctx, obj.Ref)
		if err != nil {
			return err
		}
		v.PersonIdentity = personIdentityView(id)
	}
	return nil
}

// isPerson reports whether a persisted identity's payload is a person's
// (identity_class individual), not a vendor's.
func isPerson(payload []byte) bool {
	var p struct {
		Properties struct {
			IdentityClass string `json:"identity_class"`
		} `json:"properties"`
	}
	return json.Unmarshal(payload, &p) == nil && p.Properties.IdentityClass == "individual"
}

func edgeViews(in []evidence.IdentityEdge) []IdentityEdgeView {
	out := make([]IdentityEdgeView, 0, len(in))
	for _, e := range in {
		out = append(out, IdentityEdgeView{
			EdgeID: e.EdgeID, OtherRef: e.OtherRef, Kind: e.Kind, Source: e.Source,
			By: e.By, ByName: e.ByName, At: e.At, Note: e.Note, Withdrawn: e.Withdrawn,
		})
	}
	return out
}

func accountIdentityView(v evidence.AccountIdentityView) *EvidenceAccountIdentity {
	seen := AccountSeenAsView{Logins: nonNilStrings(v.SeenAs.Logins), IDs: []SourceIDView{}, PrincipalNames: nonNilStrings(v.SeenAs.PrincipalNames)}
	for _, a := range v.SeenAs.IDs {
		seen.IDs = append(seen.IDs, SourceIDView{Tool: a.Tool, UID: a.UID})
	}
	owners := make([]OwnerView, 0, len(v.Owners))
	for _, o := range v.Owners {
		owners = append(owners, OwnerView{
			PersonRef: o.PersonRef, AccountRef: o.AccountRef, EdgeID: o.EdgeID, Kind: o.Kind, Source: o.Source,
			By: o.By, ByName: o.ByName, At: o.At, Note: o.Note, Withdrawn: o.Withdrawn, Cut: o.Cut,
		})
	}
	return &EvidenceAccountIdentity{Aliases: edgeViews(v.Aliases), Distinct: edgeViews(v.Distinct), SeenAs: seen, Owners: owners}
}

func personIdentityView(v evidence.PersonIdentityView) *EvidencePersonIdentity {
	accounts := make([]PersonAccountView, 0, len(v.Accounts))
	for _, a := range v.Accounts {
		accounts = append(accounts, PersonAccountView{
			AccountRef: a.AccountRef, EdgeID: a.EdgeID, Kind: a.Kind, Source: a.Source,
			By: a.By, ByName: a.ByName, At: a.At, Note: a.Note, Withdrawn: a.Withdrawn, Cut: a.Cut,
		})
	}
	return &EvidencePersonIdentity{Aliases: edgeViews(v.Aliases), Distinct: edgeViews(v.Distinct), Accounts: accounts}
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
	edges := edgeViews
	seen := SeenAsView{Domains: nonNilStrings(v.SeenAs.Domains), Agents: []SourceIDView{}, IPs: nonNilStrings(v.SeenAs.IPs)}
	for _, a := range v.SeenAs.Agents {
		seen.Agents = append(seen.Agents, SourceIDView{Tool: a.Tool, UID: a.UID})
	}
	return &EvidenceIdentity{Aliases: edges(v.Aliases), Distinct: edges(v.Distinct), SeenAs: seen}
}
