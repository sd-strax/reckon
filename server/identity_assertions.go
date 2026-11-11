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

// The analyst's acts on an entity's identity (adr/0211, adr/0324): "same as"
// writes an aliases edge, "not the same" a distinct-from edge, between two
// hosts, two accounts or two persons; "belongs to" writes an asserted
// belongs-to from an account to a person; an asserted edge can be withdrawn.
// Each answers the identity view the inspector re-renders from, in the shape
// GET /api/evidence/{ref} carries for that type. They are human acts: the
// agent may say in the thread that two entities look like one, never record
// it.

// IdentityAssertBody names the other end of a join or a split: its ref, of
// the same entity type as the path's, or, for a host, a hostname minted as
// the seed mints one; exactly one of the two.
type IdentityAssertBody struct {
	OtherRef      string `json:"other_ref,omitempty"`
	OtherHostname string `json:"other_hostname,omitempty"`
	Note          string `json:"note,omitempty"`
}

// IdentityOwnerBody names the person an account belongs to: its ref, or a
// hint the persisted persons are looked up by (a principal name, a mail
// address, a directory id, or an account the person's directory record
// names); exactly one of the two.
type IdentityOwnerBody struct {
	PersonRef  string `json:"person_ref,omitempty"`
	PersonHint string `json:"person_hint,omitempty"`
	Note       string `json:"note,omitempty"`
}

// IdentityWithdrawBody is the optional body of a withdrawal. Ref is the
// entity whose view to answer (the one the inspector shows), one of the
// edge's two ends; the edge's first end when absent (for an owner edge, the
// account).
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
	b.assertIdentity(w, r, b.cfg.Evidence.AssertAlias, false)
}

func (b *Backend) assertDistinct(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Evidence == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "evidence store not configured")
		return
	}
	b.assertIdentity(w, r, b.cfg.Evidence.AssertDistinct, true)
}

// entityWord names an identity entity type in a message.
func entityWord(typ string) string {
	switch typ {
	case identity.TypeUserAccount:
		return "account"
	case identity.TypeIdentity:
		return "person"
	}
	return "host"
}

// accountPersonPair reports whether the two types are an account and a
// person, either way round.
func accountPersonPair(a, b string) bool {
	return (a == identity.TypeUserAccount && b == identity.TypeIdentity) || (a == identity.TypeIdentity && b == identity.TypeUserAccount)
}

// assertIdentity serves POST /api/entities/{ref}/aliases and .../distinct. A
// split (split set) also takes an account and a person, either way round:
// "this account is not this person's" (adr/0324), the one mixed pair.
func (b *Backend) assertIdentity(w http.ResponseWriter, r *http.Request, act identityAssert, split bool) {
	claims, ok := identityPrincipal(w, r)
	if !ok {
		return
	}
	ref, typ, ok := evidence.CanonicalEntityRef(r.PathValue("ref"))
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "only a host's, an account's or a person's identity can be asserted: the ref must be an x-host, a user-account or an identity")
		return
	}
	var body IdentityAssertBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	other, hostname := strings.TrimSpace(body.OtherRef), strings.TrimSpace(body.OtherHostname)
	word := entityWord(typ)
	if (other == "") == (hostname == "") {
		writeJSONError(w, http.StatusBadRequest, "name the other "+word+" by exactly one of other_ref and other_hostname")
		return
	}
	if hostname != "" && typ != identity.TypeHost {
		writeJSONError(w, http.StatusBadRequest, "other_hostname names a host; name the other "+word+" by other_ref")
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
	other, otherTyp, ok := evidence.CanonicalEntityRef(other)
	if !ok || (otherTyp != typ && (!split || !accountPersonPair(typ, otherTyp))) {
		writeJSONError(w, http.StatusBadRequest, "the other end must be a "+word+" too: other_ref must be of the ref's type")
		return
	}
	if other == ref {
		writeJSONError(w, http.StatusBadRequest, "a "+word+" is the same as itself: name another "+word)
		return
	}
	if err := act(r.Context(), ids, ref, other, asserter(claims), body.Note); err != nil {
		writeIdentityError(w, err, "no persisted "+word+" for the pair")
		return
	}
	b.serveIdentity(w, r, ref, typ)
}

// assertOwner serves POST /api/entities/{ref}/owner: the account at ref
// belongs to the person named (adr/0324), an asserted belongs-to. A hint
// that names no person is 404; one that names several is 409, to be named by
// ref. A live owner assertion to another person is 409 until withdrawn.
func (b *Backend) assertOwner(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Evidence == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "evidence store not configured")
		return
	}
	claims, ok := identityPrincipal(w, r)
	if !ok {
		return
	}
	ref, typ, ok := evidence.CanonicalEntityRef(r.PathValue("ref"))
	if !ok || typ != identity.TypeUserAccount {
		writeJSONError(w, http.StatusBadRequest, "only an account has an owner: the ref must be a user-account")
		return
	}
	var body IdentityOwnerBody
	if !decodeBody(w, r, &body, maxBodyBytes) {
		return
	}
	person, hint := strings.TrimSpace(body.PersonRef), strings.TrimSpace(body.PersonHint)
	if (person == "") == (hint == "") {
		writeJSONError(w, http.StatusBadRequest, "name the person by exactly one of person_ref and person_hint")
		return
	}
	ids := b.identityResolver()
	if ids == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "identity resolver unavailable")
		return
	}
	if hint != "" {
		persons, err := b.cfg.Evidence.FindPersons(r.Context(), hint)
		if err != nil {
			writeInternalError(w, "look up persons", err)
			return
		}
		switch len(persons) {
		case 0:
			writeJSONError(w, http.StatusNotFound, fmt.Sprintf("no person known as %q", hint))
			return
		case 1:
			person = persons[0]
		default:
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   fmt.Sprintf("%q names %d persons; name one by person_ref", hint, len(persons)),
				"persons": persons,
			})
			return
		}
	}
	person, ptyp, ok := evidence.CanonicalEntityRef(person)
	if !ok || ptyp != identity.TypeIdentity {
		writeJSONError(w, http.StatusBadRequest, "person_ref must be a person: an identity")
		return
	}
	if err := b.cfg.Evidence.AssertOwner(r.Context(), ids, ref, person, asserter(claims), body.Note); err != nil {
		writeIdentityError(w, err, "no persisted account or person for the pair")
		return
	}
	b.serveIdentity(w, r, ref, typ)
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
		err := evidence.ErrObservedEdge
		if strings.HasPrefix(lo, identity.TypeUserAccount+"--") && strings.HasPrefix(hi, identity.TypeIdentity+"--") {
			err = evidence.ErrObservedOwner // a directory's belongs-to
		}
		writeIdentityError(w, err, "")
		return
	}
	view := lo
	if body.Ref != "" {
		c, _, ok := evidence.CanonicalEntityRef(body.Ref)
		if !ok || (c != lo && c != hi) {
			writeJSONError(w, http.StatusBadRequest, "ref must be one of the edge's two ends")
			return
		}
		view = c
	}
	if _, _, err := b.cfg.Evidence.WithdrawAssertion(r.Context(), edgeID, asserter(claims), body.Note); err != nil {
		writeIdentityError(w, err, "no identity edge "+edgeID.String())
		return
	}
	_, typ, _ := evidence.CanonicalEntityRef(view)
	b.serveIdentity(w, r, view, typ)
}

// IdentityActResponse answers an identity act. A 200 always means the act is
// recorded. Identity is a host's view after it, AccountIdentity an
// account's, PersonIdentity a person's: the one of the entity acted on is
// set, and identity is null when the view could not be read or the entity
// is no host, which is never shown as an entity with no aliases.
type IdentityActResponse struct {
	Identity        *EvidenceIdentity        `json:"identity"`
	AccountIdentity *EvidenceAccountIdentity `json:"account_identity,omitempty"`
	PersonIdentity  *EvidencePersonIdentity  `json:"person_identity,omitempty"`
}

// serveIdentity answers an entity's identity view after an act on it. The
// act has committed, so a failed read is not a failed act: it is read once
// more, and on a second failure the view is null (the inspector re-reads the
// entity).
func (b *Backend) serveIdentity(w http.ResponseWriter, r *http.Request, ref, typ string) {
	read := func() (IdentityActResponse, error) {
		ctx := r.Context()
		switch typ {
		case identity.TypeUserAccount:
			v, err := b.cfg.Evidence.AccountIdentity(ctx, ref)
			return IdentityActResponse{AccountIdentity: accountIdentityView(v)}, err
		case identity.TypeIdentity:
			v, err := b.cfg.Evidence.PersonIdentity(ctx, ref)
			return IdentityActResponse{PersonIdentity: personIdentityView(v)}, err
		}
		v, err := b.cfg.Evidence.HostIdentity(ctx, ref)
		return IdentityActResponse{Identity: identityView(v)}, err
	}
	resp, err := read()
	if err != nil {
		resp, err = read()
	}
	if err != nil {
		slog.Warn("identity act recorded, view unavailable", "ref", ref, "error", err)
		writeJSON(w, http.StatusOK, IdentityActResponse{})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// identityPrincipal reads the caller and refuses an AI delegate, as approval
// does: asserting and withdrawing an entity's identity are a person's acts.
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

// writeIdentityError maps the evidence store's errors: a missing end 404
// naming it, other not found 404, a malformed or mixed-type ref 400, a
// source's record, a live split under a join or another live owner 409,
// anything else 500.
func writeIdentityError(w http.ResponseWriter, err error, notFound string) {
	var missing *evidence.MissingHostError
	switch {
	case errors.Is(err, evidence.ErrNoAsserter):
		writeJSONError(w, http.StatusInternalServerError, "no principal")
	case evidence.IsDeadlock(err):
		// The store retried once; a second deadlock is another act racing
		// this one, and the person repeats it.
		writeJSONError(w, http.StatusConflict, "a concurrent identity change; try again")
	case errors.As(err, &missing):
		writeJSONError(w, http.StatusNotFound, strings.TrimPrefix(missing.Error(), "evidence: "))
	case errors.Is(err, evidence.ErrSplitLive):
		writeJSONError(w, http.StatusConflict, "the pair is asserted apart; withdraw that first")
	case errors.Is(err, evidence.ErrOwnersDiffer):
		writeJSONError(w, http.StatusConflict, "the two are asserted to different persons; withdraw one first")
	case errors.Is(err, evidence.ErrOwnerAsserted):
		writeJSONError(w, http.StatusConflict, "the account is asserted to belong to another person; withdraw that first")
	case errors.Is(err, evidence.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, notFound)
	case errors.Is(err, evidence.ErrMixedTypes):
		writeJSONError(w, http.StatusBadRequest, "both ends must be the same entity type")
	case errors.Is(err, evidence.ErrMalformedRef):
		writeJSONError(w, http.StatusBadRequest, "malformed STIX ref")
	case errors.Is(err, evidence.ErrObservedOwner):
		writeJSONError(w, http.StatusConflict, "a directory's record; assert that the account is not this person's instead")
	case errors.Is(err, evidence.ErrObservedEdge):
		writeJSONError(w, http.StatusConflict, "a source's record; assert distinct-from instead")
	default:
		writeInternalError(w, "identity assertion", err)
	}
}
