package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/module"
)

// Account and person reads (adr/0324). An account's events are its own and
// its alias set's; a person owns accounts by belongs-to edges, from a
// directory's record or a person's assertion, and reading a person walks
// person, aliased persons, owned accounts, each account's alias set. Nothing
// is merged: a withdrawn owner assertion or a cut alias moves the reads with
// no data to repair.

// relBelongsTo is the account-to-person ownership edge (and, elsewhere, the
// cloud resource-to-account edge, 03 §4.15).
const relBelongsTo = "belongs-to"

// identityClassIndividual marks a STIX identity that is a person, not a
// vendor identity (03 §4.12).
const identityClassIndividual = "individual"

// PersonAccountSet returns what reads as one person with personRef, personRef
// first: the person's alias set (AliasSet's rule), then every account a live
// belongs-to edge attaches to one of those persons, each widened to its own
// alias set. A withdrawn owner assertion attaches nothing; a cut alias
// between accounts holds. A ref that is no person owns nothing and is its
// own set.
func (s *Store) PersonAccountSet(ctx context.Context, personRef string) ([]string, error) {
	if _, ok := refUUID(personRef); !ok {
		return nil, ErrMalformedRef
	}
	return s.personAccountSets(ctx, []string{personRef})
}

// personAccountSets is PersonAccountSet over several person refs at once.
func (s *Store) personAccountSets(ctx context.Context, refs []string) ([]string, error) {
	persons, err := s.aliasSets(ctx, refs)
	if err != nil {
		return nil, err
	}
	accounts, err := s.ownedAccounts(ctx, persons)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(persons)
	if len(accounts) == 0 {
		return out, nil
	}
	sets, err := s.aliasSets(ctx, accounts)
	if err != nil {
		return nil, err
	}
	cut, err := s.cutAccounts(ctx, persons)
	if err != nil {
		return nil, err
	}
	for _, r := range sets {
		if !cut[r] && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// cutAccounts reads the accounts a person said are not one of persons' (the
// person's alias set): a live distinct-from between an account and a person
// of the set, either way round, cuts that account, and reaches every account
// of its alias set, since the family is one account to the person (adr/0324),
// the scope the one-live-owner rule uses, except an account the persons own
// by a live belongs-to of its own that no split names: a recycled login
// aliased to two SIDs is cut with the SID split from the person, the other
// SID the person owns stays. Such an account is left out of the person's
// reads whatever attaches it, a directory's belongs-to or an alias to an
// owned account: the one way to undo an owner a record observed.
func (s *Store) cutAccounts(ctx context.Context, persons []string) (map[string]bool, error) {
	direct, err := s.directCuts(ctx, persons)
	if err != nil || len(direct) == 0 {
		return map[string]bool{}, err
	}
	out := map[string]bool{}
	for _, r := range direct {
		out[r] = true
	}
	reach, err := s.aliasSets(ctx, direct)
	if err != nil {
		return nil, err
	}
	owned, err := s.ownedAccounts(ctx, persons)
	if err != nil {
		return nil, err
	}
	for _, r := range reach {
		if !out[r] && !slices.Contains(owned, r) {
			out[r] = true
		}
	}
	return out, nil
}

// directCuts reads the accounts a live distinct-from holds apart from one of
// persons, the accounts named, not widened.
func (s *Store) directCuts(ctx context.Context, persons []string) ([]string, error) {
	ids := personIDs(persons)
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT o.id FROM stix_edges e
		JOIN stix_objects o ON o.tenant_id = $2 AND o.type = $3
		  AND o.id = CASE WHEN e.source_ref = ANY($1) THEN e.target_ref ELSE e.source_ref END
		WHERE e.tenant_id = $2 AND e.relationship_type = '`+relDistinctFrom+`' AND e.payload->>'withdrawn_at' IS NULL
		  AND (e.source_ref = ANY($1) OR e.target_ref = ANY($1))
	`, uuidArray(ids), module.SingleTenantUUID, identity.TypeUserAccount)
	if err != nil {
		return nil, fmt.Errorf("query accounts cut from the person: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan cut account: %w", err)
		}
		out = append(out, identity.TypeUserAccount+"--"+id.String())
	}
	return out, rows.Err()
}

// personIDs answers the uuids of the identity refs among refs.
func personIDs(refs []string) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		if id, ok := refUUID(r); ok && strings.HasPrefix(r, identity.TypeIdentity+"--") {
			ids = append(ids, id)
		}
	}
	return ids
}

// cutFrom reports whether the account is cut from person by the cutAccounts
// rule, over the person's alias set: the reads (PersonAccountSet,
// accountOwners) and the views mark one account alike.
func (s *Store) cutFrom(ctx context.Context, account string, person uuid.UUID) (bool, error) {
	cut, err := s.cutFor(ctx, identity.TypeIdentity+"--"+person.String())
	if err != nil {
		return false, err
	}
	return cut[account], nil
}

// cutFor is cutAccounts over person's alias set.
func (s *Store) cutFor(ctx context.Context, person string) (map[string]bool, error) {
	persons, err := s.aliasSets(ctx, []string{person})
	if err != nil {
		return nil, err
	}
	return s.cutAccounts(ctx, persons)
}

// EntitySet widens one ref for a read by entity as the events filter does
// (entitySets): a person to its accounts and their alias sets, any other ref
// to its alias set.
func (s *Store) EntitySet(ctx context.Context, ref string) ([]string, error) {
	if _, ok := refUUID(ref); !ok {
		return nil, ErrMalformedRef
	}
	return s.entitySets(ctx, []string{ref})
}

// ownedAccounts reads the persisted user-accounts a live belongs-to edge
// attaches to any of persons, in edge order.
func (s *Store) ownedAccounts(ctx context.Context, persons []string) ([]string, error) {
	ids := make([]uuid.UUID, 0, len(persons))
	for _, r := range persons {
		if id, ok := refUUID(r); ok && strings.HasPrefix(r, identity.TypeIdentity+"--") {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.source_ref FROM stix_edges e
		JOIN stix_objects o ON o.tenant_id = $2 AND o.id = e.source_ref AND o.type = $3
		WHERE e.tenant_id = $2 AND e.relationship_type = '`+relBelongsTo+`' AND e.target_ref = ANY($1)
		  AND e.payload->>'withdrawn_at' IS NULL
		ORDER BY e.created, e.id
	`, uuidArray(ids), module.SingleTenantUUID, identity.TypeUserAccount)
	if err != nil {
		return nil, fmt.Errorf("query owned accounts: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan owned account: %w", err)
		}
		if ref := identity.TypeUserAccount + "--" + id.String(); !slices.Contains(out, ref) {
			out = append(out, ref)
		}
	}
	return out, rows.Err()
}

// entitySets widens refs for a read by entity: a person (an identity ref) to
// PersonAccountSet, any other ref, an account included, to its alias set
// only (adr/0324). A vendor identity owns no account, so it reads as its own
// alias set.
func (s *Store) entitySets(ctx context.Context, refs []string) ([]string, error) {
	var persons, others []string
	for _, r := range refs {
		if strings.HasPrefix(r, identity.TypeIdentity+"--") {
			persons = append(persons, r)
		} else {
			others = append(others, r)
		}
	}
	out, err := s.aliasSets(ctx, others)
	if err != nil {
		return nil, err
	}
	if len(persons) == 0 {
		return out, nil
	}
	ps, err := s.personAccountSets(ctx, persons)
	if err != nil {
		return nil, err
	}
	for _, r := range ps {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// AccountSeenAs is what the record has seen under an account's alias set,
// from the user objects of the events that reference it: the logins as the
// sources wrote them ("domain\sam", a bare name), the identifiers each source
// gave (uid, uid_alt), and the principal names. Computed on every read.
type AccountSeenAs struct {
	Logins         []string
	IDs            []SourceID
	PrincipalNames []string
}

// Owner is one belongs-to edge from an account to a person, as the account's
// view lists it: observed (a directory's record, Source the tool) or asserted
// (a person's, with who, when and the note).
type Owner struct {
	IdentityEdge
	PersonRef string
	// AccountRef is the account of the alias set the edge is from: the asked-for
	// account, or one aliased to it.
	AccountRef string
	// Cut is set when a live distinct-from holds the account and the person
	// apart (adr/0324): the edge stands, the reads do not follow it.
	Cut bool
}

// AccountIdentityView is an account's identity as the record holds it: the
// aliases and distinct-from edges that touch it, what its alias set has been
// seen as, and the persons it belongs to, withdrawn assertions included and
// marked. An account with no owner is shown as one (adr/0324: a service or
// shared account has none until a person says so).
type AccountIdentityView struct {
	Aliases  []IdentityEdge
	Distinct []IdentityEdge
	SeenAs   AccountSeenAs
	Owners   []Owner
}

// AccountIdentity reads an account's identity view. Owners are the
// belongs-to edges from any account of its alias set, so an owner a
// directory attached to the SID account shows on the login account aliased
// to it.
func (s *Store) AccountIdentity(ctx context.Context, ref string) (AccountIdentityView, error) {
	id, ok := refUUID(ref)
	if !ok {
		return AccountIdentityView{}, ErrMalformedRef
	}
	out := AccountIdentityView{Owners: []Owner{}, SeenAs: AccountSeenAs{Logins: []string{}, IDs: []SourceID{}, PrincipalNames: []string{}}}
	var err error
	if out.Aliases, out.Distinct, err = s.identityEdges(ctx, id); err != nil {
		return AccountIdentityView{}, err
	}
	set, err := s.aliasSets(ctx, []string{ref})
	if err != nil {
		return AccountIdentityView{}, err
	}
	for _, r := range set {
		rid, ok := refUUID(r)
		if !ok || !strings.HasPrefix(r, identity.TypeUserAccount+"--") {
			continue
		}
		edges, err := s.edgesTouching(ctx, rid, []string{relBelongsTo}, edgeSource)
		if err != nil {
			return AccountIdentityView{}, err
		}
		for _, e := range edges {
			if strings.HasPrefix(e.OtherRef, identity.TypeIdentity+"--") {
				pid, _ := refUUID(e.OtherRef)
				cut, err := s.cutFrom(ctx, r, pid)
				if err != nil {
					return AccountIdentityView{}, err
				}
				out.Owners = append(out.Owners, Owner{IdentityEdge: e.IdentityEdge, PersonRef: e.OtherRef, AccountRef: r, Cut: cut})
			}
		}
	}
	keys, err := s.accountKeys(ctx, set)
	if err != nil {
		return AccountIdentityView{}, err
	}
	if out.SeenAs, err = s.accountSeenAs(ctx, set, keys); err != nil {
		return AccountIdentityView{}, err
	}
	return out, nil
}

// PersonAccount is one belongs-to edge into a person, as the person's view
// lists it: the account and the record or person that attached it.
type PersonAccount struct {
	IdentityEdge
	AccountRef string
	// Cut is set when a live distinct-from holds the account and the person
	// apart (adr/0324): listed, as a cut alias is, but not read.
	Cut bool
}

// PersonIdentityView is a person's identity as the record holds it: the
// aliases and distinct-from edges between it and other persons, and the
// accounts attached to it, each with its origin, withdrawn ones included and
// marked.
type PersonIdentityView struct {
	Aliases  []IdentityEdge
	Distinct []IdentityEdge
	Accounts []PersonAccount
}

// PersonIdentity reads a person's identity view.
func (s *Store) PersonIdentity(ctx context.Context, ref string) (PersonIdentityView, error) {
	id, ok := refUUID(ref)
	if !ok {
		return PersonIdentityView{}, ErrMalformedRef
	}
	out := PersonIdentityView{Accounts: []PersonAccount{}}
	var err error
	if out.Aliases, out.Distinct, err = s.identityEdges(ctx, id); err != nil {
		return PersonIdentityView{}, err
	}
	edges, err := s.edgesTouching(ctx, id, []string{relBelongsTo}, edgeTarget)
	if err != nil {
		return PersonIdentityView{}, err
	}
	for _, e := range edges {
		if strings.HasPrefix(e.OtherRef, identity.TypeUserAccount+"--") {
			cut, err := s.cutFrom(ctx, e.OtherRef, id)
			if err != nil {
				return PersonIdentityView{}, err
			}
			out.Accounts = append(out.Accounts, PersonAccount{IdentityEdge: e.IdentityEdge, AccountRef: e.OtherRef, Cut: cut})
		}
	}
	return out, nil
}

// accountKeySet is what the accounts of a set were minted on, folded: the
// SIDs and provider ids, and the logins (principal names, "domain\sam", bare
// names). A user object names the set when its own first identifier is one
// of these (accountSeenAs).
type accountKeySet struct {
	ids, logins []string
}

// accountKeys reads the identifier each user-account among refs was keyed on
// (identity.Resolver.Account): a domain account's SID when its user_id is
// one, else its login; a cloud or LDAP account's user_id, else its login; a
// well-known local principal's SID; any other account's login (the sam part
// of a local one). A placeholder reads as absent.
func (s *Store) accountKeys(ctx context.Context, refs []string) (accountKeySet, error) {
	var k accountKeySet
	ids := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		if id, ok := refUUID(r); ok && strings.HasPrefix(r, identity.TypeUserAccount+"--") {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return k, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(payload->'properties', '{}'::jsonb) FROM stix_objects
		WHERE tenant_id = $3 AND id = ANY($1) AND type = $2
	`, uuidArray(ids), identity.TypeUserAccount, module.SingleTenantUUID)
	if err != nil {
		return k, fmt.Errorf("query account identifiers: %w", err)
	}
	defer rows.Close()
	add := func(list *[]string, v string) {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" && !slices.Contains(*list, v) {
			*list = append(*list, v)
		}
	}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return k, fmt.Errorf("scan account identifiers: %w", err)
		}
		var p struct {
			Login  string `json:"account_login"`
			UserID string `json:"user_id"`
			Type   string `json:"account_type"`
		}
		_ = json.Unmarshal(raw, &p)
		uid := p.UserID
		if identity.PlaceholderUserID(uid) {
			uid = ""
		}
		login := p.Login
		if identity.PlaceholderLogin(login) {
			login = ""
		}
		switch p.Type {
		case identity.AccountWindowsDomain:
			if identity.IsSID(uid) {
				add(&k.ids, uid)
				continue
			}
		case identity.AccountCloud, identity.AccountLDAP:
			if uid != "" {
				add(&k.ids, uid)
				continue
			}
		case identity.AccountWindowsLocal:
			if identity.WellKnownLocalSID(uid) {
				add(&k.ids, uid)
				continue
			}
			if i := strings.LastIndex(login, `\`); i >= 0 {
				login = login[i+1:]
			}
		case identity.AccountWindows, identity.AccountUnix, identity.AccountLocal:
		default: // a control-plane principal: keyed on its id
			if uid != "" {
				add(&k.ids, uid)
				continue
			}
		}
		add(&k.logins, login)
	}
	return k, rows.Err()
}

// accountSeenAs collects what the user objects of the events referencing any
// of refs report, for the objects that name the set. The user objects are
// the ones the normalizers read (actor.user, user, job.user, and the same
// under a detection's evidence, 03 §4.12). An object names the set when its
// own first identifier, the one the account rule keys it on, is one of keys:
// a SID that names a principal, else any other uid, else the principal name,
// else "domain\sam", else the bare name; so a record keyed elsewhere lends
// the set none of its identifiers. A matched object yields its login as
// written, its uid and uid_alt keyed by the event's source tool, and its
// principal name; placeholders are left out.
func (s *Store) accountSeenAs(ctx context.Context, refs []string, keys accountKeySet) (AccountSeenAs, error) {
	out := AccountSeenAs{Logins: []string{}, IDs: []SourceID{}, PrincipalNames: []string{}}
	if len(keys.ids) == 0 && len(keys.logins) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH ev AS (
			SELECT e.source_tool, e.payload FROM ocsf_events e
			WHERE EXISTS (
				SELECT 1 FROM observed_data_events ode JOIN stix_objects o ON o.id = ode.observed_data_id AND o.tenant_id = $2
				WHERE ode.ocsf_event_id = e.id AND o.payload->'object_refs' ?| $1::text[])
		)
		SELECT source_tool, u FROM (
			SELECT source_tool, payload->'actor'->'user' AS u FROM ev
			UNION ALL SELECT source_tool, payload->'user' FROM ev
			UNION ALL SELECT source_tool, payload->'job'->'user' FROM ev
			UNION ALL SELECT source_tool, payload->'evidence'->'actor'->'user' FROM ev
			UNION ALL SELECT source_tool, payload->'evidence'->'user' FROM ev
			UNION ALL SELECT source_tool, payload->'evidence'->'job'->'user' FROM ev
		) x WHERE jsonb_typeof(u) = 'object'
	`, textArray(refs), module.SingleTenantUUID)
	if err != nil {
		return AccountSeenAs{}, fmt.Errorf("query account seen-as: %w", err)
	}
	defer rows.Close()
	addStr := func(list *[]string, v string) {
		if v != "" && !slices.Contains(*list, v) {
			*list = append(*list, v)
		}
	}
	addID := func(tool, v string) {
		if v == "" || identity.PlaceholderUserID(v) {
			return
		}
		id := SourceID{Tool: tool, UID: v}
		if !slices.Contains(out.IDs, id) {
			out.IDs = append(out.IDs, id)
		}
	}
	for rows.Next() {
		var tool string
		var raw []byte
		if err := rows.Scan(&tool, &raw); err != nil {
			return AccountSeenAs{}, fmt.Errorf("scan account seen-as: %w", err)
		}
		var u struct {
			Name   string `json:"name"`
			Domain string `json:"domain"`
			UID    string `json:"uid"`
			UIDAlt string `json:"uid_alt"`
		}
		_ = json.Unmarshal(raw, &u)
		o := seenUser(u.Name, u.Domain, u.UID, u.UIDAlt)
		if !o.matches(keys) {
			continue
		}
		addStr(&out.Logins, o.login)
		addStr(&out.PrincipalNames, o.principal)
		addID(tool, o.uid)
		addID(tool, o.uidAlt)
	}
	if err := rows.Err(); err != nil {
		return AccountSeenAs{}, fmt.Errorf("iterate account seen-as: %w", err)
	}
	slices.Sort(out.Logins)
	slices.Sort(out.PrincipalNames)
	slices.SortFunc(out.IDs, func(a, b SourceID) int {
		if c := strings.Compare(a.Tool, b.Tool); c != 0 {
			return c
		}
		return strings.Compare(a.UID, b.UID)
	})
	return out, nil
}

// seenObject is one user object as the seen-as read folds it.
type seenObject struct {
	login, principal, uid, uidAlt string
	first                         string // the identifier it is keyed on, folded
	firstIsID                     bool
}

func seenUser(name, domain, uid, uidAlt string) seenObject {
	name, domain, uid, uidAlt = strings.TrimSpace(name), strings.TrimSpace(domain), strings.TrimSpace(uid), strings.TrimSpace(uidAlt)
	if domain == "" {
		if d, n, ok := strings.Cut(name, `\`); ok {
			domain, name = d, n
		}
	}
	if identity.PlaceholderLogin(name) {
		name = ""
	}
	if identity.PlaceholderDomain(domain) {
		domain = ""
	}
	if identity.PlaceholderUserID(uid) {
		uid = ""
	}
	o := seenObject{uid: uid, uidAlt: uidAlt}
	switch {
	case strings.Contains(name, "@"):
		o.principal = name
	case strings.Contains(uidAlt, "@"):
		o.principal = uidAlt
	}
	o.login = name
	if domain != "" && name != "" && !strings.Contains(name, "@") {
		o.login = domain + `\` + name
	}
	switch {
	case uid != "":
		o.first, o.firstIsID = strings.ToLower(uid), true
	case o.principal != "":
		o.first = strings.ToLower(o.principal)
	default:
		o.first = strings.ToLower(o.login)
	}
	return o
}

// matches reports whether the object's first identifier is one the set is
// keyed on. A bare login also matches a set keyed on that sam alone (a local
// account's key carries no domain).
func (o seenObject) matches(k accountKeySet) bool {
	if o.first == "" {
		return false
	}
	if o.firstIsID {
		if slices.Contains(k.ids, o.first) {
			return true
		}
		// A uid that keys no account of the set (a non-SID id beside a
		// domain login) leaves the login to match.
		if identity.IsSID(o.first) {
			return false
		}
		first := strings.ToLower(firstNonEmptyString(o.principal, o.login))
		return first != "" && slices.Contains(k.logins, first)
	}
	return slices.Contains(k.logins, o.first)
}

func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// AccountMatch is a persisted account a typed token names: its ref, type and
// login.
type AccountMatch struct {
	Ref   string
	Type  string
	Login string
	// UserID is the identifier the account carries (a SID, a provider id),
	// empty on an account keyed on its login.
	UserID string
}

// FindAccounts reads the persisted user-accounts a token names: its login,
// principal name or identifier equal to the token, case folded. It answers
// at most limit matches, oldest first; the seed check lists them when the
// account it would mint is not among them.
func (s *Store) FindAccounts(ctx context.Context, token string, limit int) ([]AccountMatch, error) {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "" {
		return []AccountMatch{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(payload->'properties'->>'account_type', ''), COALESCE(payload->'properties'->>'account_login', ''),
		       COALESCE(payload->'properties'->>'user_id', '')
		FROM stix_objects
		WHERE tenant_id = $1 AND type = $2 AND (
			lower(payload->'properties'->>'account_login') = $3 OR lower(payload->'properties'->>'principal_name') = $3
			OR lower(payload->'properties'->>'user_id') = $3)
		ORDER BY created, id LIMIT $4
	`, module.SingleTenantUUID, identity.TypeUserAccount, t, limit)
	if err != nil {
		return nil, fmt.Errorf("query accounts by name: %w", err)
	}
	defer rows.Close()
	out := []AccountMatch{}
	for rows.Next() {
		var id uuid.UUID
		var m AccountMatch
		if err := rows.Scan(&id, &m.Type, &m.Login, &m.UserID); err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		m.Ref = identity.TypeUserAccount + "--" + id.String()
		out = append(out, m)
	}
	return out, rows.Err()
}

// FindPersons reads the persons a typed token names (adr/0324): a persisted
// person whose principal name, mail or directory id equals the token, case
// folded, and the owners, by live belongs-to, of the accounts the token
// names (FindAccounts), so an address or a principal name reaches the person
// through the account it names. Persons first, owners after, one ref per
// alias set: two directories' persons a record or an analyst joined are one
// human and answer once, as the first of them found.
func (s *Store) FindPersons(ctx context.Context, token string) ([]string, error) {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "" {
		return []string{}, nil
	}
	found, err := s.personsNamed(ctx, t)
	if err != nil {
		return nil, err
	}
	accounts, err := s.FindAccounts(ctx, t, 50)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		owners, err := s.accountOwners(ctx, a.Ref)
		if err != nil {
			return nil, err
		}
		for _, o := range owners {
			if !slices.Contains(found, o) {
				found = append(found, o)
			}
		}
	}
	out := []string{}
	covered := map[string]bool{}
	for _, p := range found {
		if covered[p] {
			continue
		}
		set, err := s.aliasSets(ctx, []string{p})
		if err != nil {
			return nil, err
		}
		// The set's least ref represents it, so the answer does not depend
		// on which member the token happened to name.
		rep := p
		for _, m := range set {
			covered[m] = true
			if strings.HasPrefix(m, identity.TypeIdentity+"--") && m < rep {
				rep = m
			}
		}
		out = append(out, rep)
	}
	return out, nil
}

// personsNamed reads the persisted persons whose principal name, mail or
// directory id is t, already folded.
func (s *Store) personsNamed(ctx context.Context, t string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM stix_objects
		WHERE tenant_id = $1 AND type = $2 AND payload->'properties'->>'identity_class' = $4 AND (
			lower(payload->'properties'->>'principal_name') = $3 OR lower(payload->'properties'->>'mail') = $3
			OR lower(payload->'properties'->>'directory_id') = $3)
		ORDER BY created, id
	`, module.SingleTenantUUID, identity.TypeIdentity, t, identityClassIndividual)
	if err != nil {
		return nil, fmt.Errorf("query persons by name: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan person: %w", err)
		}
		out = append(out, identity.TypeIdentity+"--"+id.String())
	}
	return out, rows.Err()
}

// accountOwners reads the persons a live belongs-to edge attaches account
// to, through the account's alias set, less any person the account is cut
// from (cutAccounts' rule over that person's alias set), so the owners agree
// with PersonAccountSet and the views.
func (s *Store) accountOwners(ctx context.Context, account string) ([]string, error) {
	candidates, err := s.ownerCandidates(ctx, account)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range candidates {
		cut, err := s.cutFor(ctx, p)
		if err != nil {
			return nil, err
		}
		if !cut[account] {
			out = append(out, p)
		}
	}
	return out, nil
}

// ownerCandidates reads the persons a live belongs-to edge from any account
// of account's alias set attaches it to, sorted.
func (s *Store) ownerCandidates(ctx context.Context, account string) ([]string, error) {
	set, err := s.aliasSets(ctx, []string{account})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(set))
	for _, r := range set {
		if id, ok := refUUID(r); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT e.target_ref FROM stix_edges e
		JOIN stix_objects o ON o.tenant_id = $2 AND o.id = e.target_ref AND o.type = $3
		WHERE e.tenant_id = $2 AND e.relationship_type = '`+relBelongsTo+`' AND e.source_ref = ANY($1)
		  AND e.payload->>'withdrawn_at' IS NULL
	`, uuidArray(ids), module.SingleTenantUUID, identity.TypeIdentity)
	if err != nil {
		return nil, fmt.Errorf("query account owners: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan account owner: %w", err)
		}
		out = append(out, identity.TypeIdentity+"--"+id.String())
	}
	slices.Sort(out)
	return out, rows.Err()
}
