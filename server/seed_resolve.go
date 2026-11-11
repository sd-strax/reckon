package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/evidence"
	"github.com/sd-strax/reckon/identity"
)

// seedResolution is the outcome of interpreting a raw analyst-typed seed: the
// aggregate Seed to persist, plus the human display line the workbench renders
// (the chip and the derived title). display never contains a STIX id.
type seedResolution struct {
	Seed    aggregate.Seed
	Display string
}

// hexHash matches a bare md5/sha1/sha256 digest (the three algorithms the
// identity resolver ranks). Any other length is not treated as a hash.
var hexHash = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// hostName is what a host seed may look like (#159): letters, digits, '-',
// '_' and '.', starting and ending with a letter or digit. A value outside it
// is a typo, not a host.
var hostName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,251}[A-Za-z0-9])?$`)

// classifySeedKind applies the two rules an analyst can hold in their head: a
// value that contains whitespace or ends in a question mark is a question
// (a hunt); anything else is an entity (a host/IP/hash to root on). It never
// tries to extract an entity out of a sentence — the host enters the graph the
// moment the agent touches it anyway (design/ui/02 §2.7).
func classifySeedKind(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if strings.ContainsAny(v, " \t\n") || strings.HasSuffix(v, "?") {
		return aggregate.SeedQuestion
	}
	return aggregate.SeedEntity
}

// resolveSeedInput turns a raw analyst identifier into a persistable seed. kind
// is the human's confirmed choice ("entity" | "question"); when empty it is
// inferred by classifySeedKind. The entity path mints the STIX id through the
// identity resolver (the single arbiter of ids, 03 §7) so a human never types
// or sees one. Alerts do not flow through here — they carry an explicit
// id+source seed built by the caller.
func resolveSeedInput(res *identity.Resolver, value, kind string) (seedResolution, error) {
	return resolveSeedInputAs(res, value, kind, "")
}

// The kinds of entity a seed may name (adr/0324). A person is resolved
// against the record (resolvePersonSeed), not minted here.
const (
	entityKindHost    = "host"
	entityKindAccount = "account"
	entityKindPerson  = "person"
)

// seedEntityKind is the entity kind a token names when the analyst did not
// choose one: a token with a backslash or an "@" is an account
// ("DOMAIN\sam", "user@domain"); anything else, an address and a hash
// included, is resolved as it always was (seedEntityKindHost covers the
// three).
func seedEntityKind(token string) string {
	if strings.ContainsAny(token, `\@`) {
		return entityKindAccount
	}
	return entityKindHost
}

// resolveSeedInputAs is resolveSeedInput with the entity kind the analyst
// chose (host, account or person; empty to default it from the token,
// seedEntityKind). A person seed is not resolved here: the caller looks it up
// in the record (resolvePersonSeed).
func resolveSeedInputAs(res *identity.Resolver, value, kind, entityKind string) (seedResolution, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return seedResolution{}, fmt.Errorf("empty seed")
	}
	if kind == "" {
		kind = classifySeedKind(v)
	}
	if kind == aggregate.SeedEntity {
		switch ek := entityKindOf(v, entityKind); ek {
		case entityKindAccount:
			return resolveAccountSeed(res, v)
		case entityKindPerson:
			return seedResolution{}, fmt.Errorf("a person seed is resolved against the record")
		case entityKindHost:
		default:
			return seedResolution{}, fmt.Errorf("unknown entity kind %q (host | account | person)", entityKind)
		}
	}

	switch kind {
	case aggregate.SeedQuestion:
		return seedResolution{
			Seed:    aggregate.Seed{Type: aggregate.SeedQuestion, HypothesisStatement: v},
			Display: v,
		}, nil

	case aggregate.SeedEntity:
		if res == nil {
			return seedResolution{}, fmt.Errorf("identity resolver unavailable")
		}
		if strings.ContainsAny(v, " \t\n") {
			return seedResolution{}, fmt.Errorf("%q looks like a question, not a host/IP/hash", v)
		}
		if net.ParseIP(v) == nil && !hexHash.MatchString(v) && !hostName.MatchString(v) {
			return seedResolution{}, fmt.Errorf("%q is not a host name, IP address or file hash — check it for a typo", v)
		}
		ref, label := resolveEntityToken(res, v)
		if ref == "" {
			// A placeholder (localhost, 0.0.0.0, the empty file's hash) names
			// nothing, so the resolver mints no id to root on (adr/0211).
			return seedResolution{}, fmt.Errorf("%q is a placeholder value that names no host, address or file", v)
		}
		return seedResolution{
			Seed: aggregate.Seed{
				Type:             aggregate.SeedEntity,
				EntityRef:        ref,
				EntityIdentifier: v,
			},
			Display: label + " " + v,
		}, nil

	default:
		return seedResolution{}, fmt.Errorf("unknown seed kind %q (entity | question)", kind)
	}
}

// deriveSeedTitle turns a seed's display line into an investigation title,
// clipped so a long hunt question does not become an unwieldy title. Clipped by
// RUNES, not bytes — a byte slice could split a multi-byte character and stamp
// an invalid-UTF-8 title.
func deriveSeedTitle(display string) string {
	t := strings.TrimSpace(display)
	const max = 80
	if r := []rune(t); len(r) > max {
		t = strings.TrimSpace(string(r[:max])) + "…"
	}
	return t
}

// seedSummaryOf is the nil-safe Summary() used in the create response.
func seedSummaryOf(s *aggregate.Seed) string {
	if s == nil {
		return ""
	}
	return s.Summary()
}

// resolveEntityToken mints the STIX id for a single artifact token and returns
// a human type label for the chip. The sub-classification (IP → IPv4/IPv6, a
// 32/40/64-hex string → file, everything else → host) mirrors the identity
// resolver's own shaping; the label is display only. A host is minted on the
// token as typed: the resolver folds case and the FQDN to the short name
// (adr/0211), so "WIN-FILE01" and "win-file01.corp.example" seed the host the
// events mint. A placeholder token mints nothing and ref is "".
func resolveEntityToken(res *identity.Resolver, token string) (ref, label string) {
	if ip := net.ParseIP(token); ip != nil {
		if ip.To4() != nil {
			return string(res.IPv4Addr(token)), "IP"
		}
		return string(res.IPv6Addr(token)), "IP"
	}
	if hexHash.MatchString(token) {
		switch len(token) {
		case 32:
			return string(res.File(identity.FileIdentity{Hashes: map[string]string{"md5": token}})), "file"
		case 40:
			return string(res.File(identity.FileIdentity{Hashes: map[string]string{"sha1": token}})), "file"
		case 64:
			return string(res.File(identity.FileIdentity{Hashes: map[string]string{"sha256": token}})), "file"
		}
	}
	return string(res.Host(identity.HostIdentity{Hostname: token})), "host"
}

// entityKindOf is the entity kind a seed names: the analyst's choice, else
// the token's shape (seedEntityKind).
func entityKindOf(token, chosen string) string {
	if c := strings.ToLower(strings.TrimSpace(chosen)); c != "" {
		return c
	}
	return seedEntityKind(token)
}

// accountSeedAccount is the account an account seed's token names, by the
// account rule (adr/0324) read from the token's shape: "DOMAIN\sam" and
// "user@domain" are domain accounts keyed on that login (a principal name is
// a domain account's, never a cloud sign-in's, unless the record says so);
// a bare login is the weak windows account. A placeholder names none.
func accountSeedAccount(res *identity.Resolver, token string) identity.STIXID {
	typ := identity.AccountWindowsDomain
	if !strings.ContainsAny(token, `\@`) {
		typ = identity.AccountWindows
	}
	if d, n, ok := strings.Cut(token, `\`); ok && (identity.PlaceholderLogin(n) || strings.TrimSpace(d) == "") {
		return ""
	}
	return res.Account(identity.AccountIdentity{Type: typ, Login: token})
}

// accountToken is what an account seed may look like: no whitespace, at most
// one backslash, not starting or ending with a separator.
var accountToken = regexp.MustCompile(`^[^\s\\@]+(?:\\[^\s\\@]+|@[^\s\\@]+)?$`)

func resolveAccountSeed(res *identity.Resolver, v string) (seedResolution, error) {
	if res == nil {
		return seedResolution{}, fmt.Errorf("identity resolver unavailable")
	}
	if !accountToken.MatchString(v) {
		return seedResolution{}, fmt.Errorf(`%q is not an account (DOMAIN\name, name@domain or a bare login) — check it for a typo`, v)
	}
	ref := accountSeedAccount(res, v)
	if ref == "" {
		return seedResolution{}, fmt.Errorf("%q is a placeholder value that names no account", v)
	}
	return seedResolution{
		Seed:    aggregate.Seed{Type: aggregate.SeedEntity, EntityRef: string(ref), EntityIdentifier: v},
		Display: "account " + v,
	}, nil
}

// resolvePersonSeed resolves a person seed against the record (adr/0324): the
// persisted person whose principal name, mail or directory id is the token,
// or the owner of the account the token names. No person is a 404-style
// error; several are a conflict the analyst settles by naming the person
// more precisely. The seed's entity is the person's ref.
func (b *Backend) resolvePersonSeed(r *http.Request, value string) (seedResolution, int, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return seedResolution{}, http.StatusBadRequest, fmt.Errorf("empty seed")
	}
	if b.cfg.Evidence == nil {
		return seedResolution{}, http.StatusServiceUnavailable, fmt.Errorf("evidence store not configured: no person can be looked up")
	}
	persons, err := b.cfg.Evidence.FindPersons(r.Context(), v)
	if err != nil {
		return seedResolution{}, http.StatusInternalServerError, fmt.Errorf("look up persons: %w", err)
	}
	switch len(persons) {
	case 0:
		return seedResolution{}, http.StatusNotFound, fmt.Errorf("no person known as %q: a person is known from a directory's record of them", v)
	case 1:
	default:
		return seedResolution{}, http.StatusConflict, fmt.Errorf("%q names %d persons; seed on one by a more precise name", v, len(persons))
	}
	return seedResolution{
		Seed:    aggregate.Seed{Type: aggregate.SeedEntity, EntityRef: persons[0], EntityIdentifier: v},
		Display: "person " + v,
	}, http.StatusOK, nil
}

// resolveAccountRefSeed seeds on a persisted user-account named by its ref
// (adr/0324), the candidate an unknown-account report listed: the entity is
// the ref, and the identifier and the display line are the record's login
// and type, never the ref (the typed value when the record has neither a
// login nor an identifier). A ref that is not a user-account is 400; one the
// record does not hold as a user-account, unknown or another type's uuid, is
// 404.
func (b *Backend) resolveAccountRefSeed(r *http.Request, ref, typed string) (seedResolution, int, error) {
	canon, typ, ok := evidence.CanonicalEntityRef(ref)
	if !ok || typ != identity.TypeUserAccount {
		return seedResolution{}, http.StatusBadRequest, fmt.Errorf("account_ref must be a user-account ref")
	}
	if b.cfg.Evidence == nil {
		return seedResolution{}, http.StatusServiceUnavailable, fmt.Errorf("evidence store not configured: no account can be looked up")
	}
	// The record is read by uuid: the object under it must itself be an
	// account, and any other is answered as an unknown ref is, so a probe
	// learns nothing about what else the record holds.
	obj, err := b.cfg.Evidence.Object(r.Context(), canon)
	switch {
	case errors.Is(err, evidence.ErrNotFound) || (err == nil && obj.Type != identity.TypeUserAccount):
		return seedResolution{}, http.StatusNotFound, fmt.Errorf("no account known by that ref")
	case err != nil:
		return seedResolution{}, http.StatusInternalServerError, fmt.Errorf("read account: %w", err)
	}
	var p struct {
		Properties struct {
			Login  string `json:"account_login"`
			Type   string `json:"account_type"`
			UserID string `json:"user_id"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(obj.Payload, &p)
	name := strings.TrimSpace(p.Properties.Login)
	if name == "" {
		name = strings.TrimSpace(p.Properties.UserID)
	}
	if name == "" {
		name = strings.TrimSpace(typed) // what the analyst sees in the box
	}
	display := "account " + name
	if t := strings.TrimSpace(p.Properties.Type); t != "" {
		display += " (" + t + ")"
	}
	return seedResolution{
		Seed:    aggregate.Seed{Type: aggregate.SeedEntity, EntityRef: canon, EntityIdentifier: name},
		Display: display,
	}, http.StatusOK, nil
}

// preferSignInAccount settles an account seed on a principal name the record
// holds only as an identity provider's sign-in (adr/0324): when the domain
// account the token mints is not persisted and every account the record holds
// under the name is a cloud one of one alias set, the seed roots on the
// set's strong account, the one keyed on the provider's id. Two or more sets,
// or another kind, leave the seed as it was, for unknownAccount to list.
func (b *Backend) preferSignInAccount(r *http.Request, s seedResolution) seedResolution {
	if b.cfg.Evidence == nil || !strings.HasPrefix(s.Seed.EntityRef, identity.TypeUserAccount+"--") || !strings.Contains(s.Seed.EntityIdentifier, "@") {
		return s
	}
	if _, err := b.cfg.Evidence.Object(r.Context(), s.Seed.EntityRef); !errors.Is(err, evidence.ErrNotFound) {
		return s
	}
	// Every match: "one alias set" is decided over all of them.
	matches, err := b.cfg.Evidence.FindAccounts(r.Context(), s.Seed.EntityIdentifier, 0)
	if err != nil || len(matches) == 0 {
		return s
	}
	// The matches as accounts: one alias set is one account, the sign-in
	// name's account aliased to the one keyed on the provider's id.
	set, err := b.cfg.Evidence.AliasSet(r.Context(), matches[0].Ref)
	if err != nil {
		return s
	}
	strong := ""
	for _, m := range matches {
		if m.Type != identity.AccountCloud || !slices.Contains(set, m.Ref) {
			return s // another kind, or a second account: unknownAccount lists them
		}
		if strong == "" && m.UserID != "" {
			strong = m.Ref
		}
	}
	if strong == "" {
		strong = matches[0].Ref
	}
	s.Seed.EntityRef = strong
	return s
}

// unknownAccount is unknownHost's sibling for an account seed (adr/0324): the
// account the token mints is unknown when the record holds no object under
// it. The report lists the persisted accounts the token does name (a
// different type or spelling of the same login), so the analyst can seed on
// one of them; with no evidence store nothing can vouch and the seed stands.
func (b *Backend) unknownAccount(r *http.Request, s seedResolution) (bool, string, []evidence.AccountMatch) {
	if !strings.HasPrefix(s.Seed.EntityRef, identity.TypeUserAccount+"--") || b.cfg.Evidence == nil {
		return false, "", nil
	}
	_, err := b.cfg.Evidence.Object(r.Context(), s.Seed.EntityRef)
	if err == nil || !errors.Is(err, evidence.ErrNotFound) {
		return false, "", nil
	}
	// Every match is counted; the list shown is bounded (candidateCap).
	matches, err := b.cfg.Evidence.FindAccounts(r.Context(), s.Seed.EntityIdentifier, 0)
	if err != nil {
		return false, "", nil
	}
	msg := fmt.Sprintf("no account %q in the record reckon holds — check the name, or start anyway", s.Seed.EntityIdentifier)
	if len(matches) > 0 {
		msg = fmt.Sprintf("no account %q of that kind in the record; the record has %d account(s) under that name — seed on one, or start anyway", s.Seed.EntityIdentifier, len(matches))
	}
	shown, _ := capped(matches)
	return true, msg, shown
}

// unknownHost asks the tenant's host inventory whether a host seed exists
// (#159). Only a COMPLETE answer with nothing in it reports the host unknown;
// with no inventory connected, or an answer that was not complete, nothing can
// vouch either way and the seed stands.
func (b *Backend) unknownHost(r *http.Request, s seedResolution) (bool, string) {
	if !strings.HasPrefix(s.Seed.EntityRef, "x-host--") {
		return false, ""
	}
	resolver, catalog := b.capabilitySurface()
	if resolver == nil || catalog == nil || !slices.Contains(resolver.AvailableVerbs(catalog), "get_host_context") {
		return false, ""
	}
	host := s.Seed.EntityIdentifier
	// The inventory is asked with the name as typed, since folding is for ids, not for a tool's query.
	res, err := resolver.Resolve(r.Context(), "get_host_context", capability.CallInput{Entity: map[string]any{"host": map[string]any{"hostname": host}}})
	if err != nil || res.Coverage != capability.CoverageComplete || len(res.ObservedDataRefs) > 0 || len(res.EntityRefs) > 0 {
		return false, ""
	}
	return true, fmt.Sprintf("no host named %q in the host inventory reckon can see — check the name, or start anyway", host)
}
