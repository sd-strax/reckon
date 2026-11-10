package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/authz"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/identity"
)

// The analyst's acts on a host's identity (adr/0211): "same as another host"
// writes an aliases edge, "not the same host" a distinct-from edge, and an
// asserted edge can be withdrawn. Each answers the identity view the
// inspector re-renders from, in the shape GET /api/evidence/{ref} carries.
// They are human acts: the agent may say in the thread that two hosts look
// like one, never record it. Hosts only, for now.

// IdentityAssertBody names the other host of a join or a split: its ref, or
// a hostname minted as the seed mints one; exactly one of the two.
type IdentityAssertBody struct {
	OtherRef      string `json:"other_ref,omitempty"`
	OtherHostname string `json:"other_hostname,omitempty"`
	Note          string `json:"note,omitempty"`
}

// IdentityWithdrawBody is the optional body of a withdrawal. Ref is the host
// whose view to answer (the one the inspector shows); the edge's source host
// when absent.
type IdentityWithdrawBody struct {
	Note string `json:"note,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

type identityAssert func(ctx context.Context, ids *identity.Resolver, ref, otherRef string, by evidence.Asserter, note string) error

func (b *Backend) assertAlias(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Evidence == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "evidence store not configured")
		return
	}
	b.assertIdentity(w, r, b.cfg.Evidence.AssertAlias)
}

func (b *Backend) assertDistinct(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Evidence == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "evidence store not configured")
		return
	}
	b.assertIdentity(w, r, b.cfg.Evidence.AssertDistinct)
}

// assertIdentity serves POST /api/entities/{ref}/aliases and .../distinct.
func (b *Backend) assertIdentity(w http.ResponseWriter, r *http.Request, act identityAssert) {
	claims, ok := identityPrincipal(w, r)
	if !ok {
		return
	}
	ref, isHost := evidence.CanonicalHostRef(r.PathValue("ref"))
	if !isHost {
		writeJSONError(w, http.StatusBadRequest, "only a host's identity can be asserted: the ref must be an x-host")
		return
	}
	var body IdentityAssertBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	other, hostname := strings.TrimSpace(body.OtherRef), strings.TrimSpace(body.OtherHostname)
	if (other == "") == (hostname == "") {
		writeJSONError(w, http.StatusBadRequest, "name the other host by exactly one of other_ref and other_hostname")
		return
	}
	ids := b.identityResolver()
	if ids == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "identity resolver unavailable")
		return
	}
	if hostname != "" {
		var msg string
		if other, msg = hostRefFor(ids, hostname); msg != "" {
			writeJSONError(w, http.StatusBadRequest, msg)
			return
		}
	}
	other, isHost = evidence.CanonicalHostRef(other)
	if !isHost {
		writeJSONError(w, http.StatusBadRequest, "the other end must be a host: other_ref must be an x-host")
		return
	}
	if other == ref {
		writeJSONError(w, http.StatusBadRequest, "a host is the same as itself: name another host")
		return
	}
	if err := act(r.Context(), ids, ref, other, asserter(claims), body.Note); err != nil {
		writeIdentityError(w, err, "no persisted host for the pair")
		return
	}
	b.serveHostIdentity(w, r, ref)
}

// withdrawIdentity serves POST /api/identity/{edge_id}/withdraw. The edge id
// is the edge_id the identity view lists (relationship--<uuid>), or its uuid.
func (b *Backend) withdrawIdentity(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Evidence == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "evidence store not configured")
		return
	}
	claims, ok := identityPrincipal(w, r)
	if !ok {
		return
	}
	edgeID, err := uuid.Parse(strings.TrimPrefix(r.PathValue("edge_id"), "relationship--"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "edge_id is not an identity edge id")
		return
	}
	var body IdentityWithdrawBody
	if !decodeOptionalBody(w, r, &body, maxBodyBytes) {
		return
	}
	// Checked from the edge row before anything is written: a source's record
	// is not withdrawn, and the view asked for is one of the edge's ends.
	lo, hi, observed, err := b.cfg.Evidence.AssertionEnds(r.Context(), edgeID)
	if err != nil {
		writeIdentityError(w, err, "no identity edge "+edgeID.String())
		return
	}
	if observed {
		writeIdentityError(w, evidence.ErrObservedEdge, "")
		return
	}
	view := lo
	if body.Ref != "" {
		c, isHost := evidence.CanonicalHostRef(body.Ref)
		if !isHost || (c != lo && c != hi) {
			writeJSONError(w, http.StatusBadRequest, "ref must be one of the edge's two hosts")
			return
		}
		view = c
	}
	if _, _, err := b.cfg.Evidence.WithdrawAssertion(r.Context(), edgeID, asserter(claims), body.Note); err != nil {
		writeIdentityError(w, err, "no identity edge "+edgeID.String())
		return
	}
	b.serveHostIdentity(w, r, view)
}

// serveHostIdentity answers a host's identity view after an act on it. The act
// has committed, so a failed read is not a failed act: it is read once more,
// and on a second failure the answer is 200 with an empty view, as
// getEvidence degrades (the inspector re-reads the host).
func (b *Backend) serveHostIdentity(w http.ResponseWriter, r *http.Request, ref string) {
	v, err := b.cfg.Evidence.HostIdentity(r.Context(), ref)
	if err != nil {
		v, err = b.cfg.Evidence.HostIdentity(r.Context(), ref)
	}
	if err != nil {
		slog.Warn("identity act recorded, view unavailable", "ref", ref, "error", err)
		v = evidence.HostIdentityView{}
	}
	writeJSON(w, http.StatusOK, identityView(v))
}

// identityPrincipal reads the caller and refuses an AI delegate, as approval
// does: asserting and withdrawing a host's identity are a person's acts.
func identityPrincipal(w http.ResponseWriter, r *http.Request) (authz.Claims, bool) {
	return humanClaims(w, r)
}

func asserter(c authz.Claims) evidence.Asserter {
	return evidence.Asserter{ID: c.Subject, Name: c.PreferredUsername}
}

// hostRefFor mints the host a typed hostname names, through the seed's path
// (resolveEntityToken, folding case and the FQDN, adr/0211), or says why it
// names none.
func hostRefFor(ids *identity.Resolver, hostname string) (string, string) {
	if !hostName.MatchString(hostname) {
		return "", fmt.Sprintf("other_hostname %q is not a host name", hostname)
	}
	ref, label := resolveEntityToken(ids, hostname)
	switch {
	case ref == "":
		return "", fmt.Sprintf("other_hostname %q is a placeholder that names no host", hostname)
	case label != "host":
		return "", fmt.Sprintf("other_hostname %q names an address or a file, not a host", hostname)
	}
	return ref, ""
}

// writeIdentityError maps the evidence store's errors: a missing host 404
// naming it, other not found 404, a malformed ref 400, a source's record or a
// live split under a join 409, anything else 500.
func writeIdentityError(w http.ResponseWriter, err error, notFound string) {
	var missing *evidence.MissingHostError
	switch {
	case errors.Is(err, evidence.ErrNoAsserter):
		writeJSONError(w, http.StatusInternalServerError, "no principal")
	case errors.As(err, &missing):
		writeJSONError(w, http.StatusNotFound, "no persisted host "+missing.Ref)
	case errors.Is(err, evidence.ErrSplitLive):
		writeJSONError(w, http.StatusConflict, "the pair is asserted apart; withdraw that first")
	case errors.Is(err, evidence.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, notFound)
	case errors.Is(err, evidence.ErrMalformedRef):
		writeJSONError(w, http.StatusBadRequest, "malformed STIX ref")
	case errors.Is(err, evidence.ErrObservedEdge):
		writeJSONError(w, http.StatusConflict, "a source's record; assert distinct-from instead")
	default:
		writeInternalError(w, "identity assertion", err)
	}
}
