package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsf"
	"github.com/sd-strax/reckon/module"
	"github.com/sd-strax/reckon/stix"
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
	edges, err := s.directCutEdges(ctx, persons)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range edges {
		if !slices.Contains(out, e.AccountRef) {
			out = append(out, e.AccountRef)
		}
	}
	return out, nil
}

// CutEdge is the live distinct-from that cuts an account from a person
// (adr/0324): its id and its two ends, the account and the person it names.
// The account and the person are those the analyst asserted it on, which
// may be an account aliased to the one cut, or a person aliased to the one
// whose view it is.
type CutEdge struct {
	EdgeID     string
	AccountRef string
	PersonRef  string
	// The assertion's origin: who made it, when, and the note, so a row that
	// shows the cut shows who said so and why.
	By     string
	ByName string
	At     time.Time
	Note   string
}

// directCutEdges reads the live distinct-from edges between an account and
// one of persons, oldest first.
func (s *Store) directCutEdges(ctx context.Context, persons []string) ([]CutEdge, error) {
	ids := personIDs(persons)
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, o.id, CASE WHEN e.source_ref = ANY($1) THEN e.source_ref ELSE e.target_ref END, e.payload, e.created
		FROM stix_edges e
		JOIN stix_objects o ON o.tenant_id = $2 AND o.type = $3
		  AND o.id = CASE WHEN e.source_ref = ANY($1) THEN e.target_ref ELSE e.source_ref END
		WHERE e.tenant_id = $2 AND e.relationship_type = '`+relDistinctFrom+`' AND `+eWithdrawnAtSQL+` IS NULL
		  AND (e.source_ref = ANY($1) OR e.target_ref = ANY($1))
		ORDER BY e.created, e.id
	`, uuidArray(ids), module.SingleTenantUUID, identity.TypeUserAccount)
	if err != nil {
		return nil, fmt.Errorf("query accounts cut from the person: %w", err)
	}
	defer rows.Close()
	var out []CutEdge
	for rows.Next() {
		var edge, acct, person uuid.UUID
		var payload []byte
		var created time.Time
		if err := rows.Scan(&edge, &acct, &person, &payload, &created); err != nil {
			return nil, fmt.Errorf("scan cut account: %w", err)
		}
		origin := identityEdge(payload, created)
		out = append(out, CutEdge{
			EdgeID:     "relationship--" + edge.String(),
			AccountRef: identity.TypeUserAccount + "--" + acct.String(),
			PersonRef:  identity.TypeIdentity + "--" + person.String(),
			By:         origin.By, ByName: origin.ByName, At: origin.At, Note: origin.Note,
		})
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

// cutsFrom answers, for each account the cutAccounts rule cuts from
// person's alias set, the edge that cuts it: the reads (PersonAccountSet,
// accountOwners) and the views mark one account alike, and the view names
// the edge, so the analyst can withdraw it from the row that shows the cut.
// An account the rule cuts directly is cut by its own edge; one it reaches
// through an alias, by the first live edge whose account's alias set holds
// it.
func (s *Store) cutsFrom(ctx context.Context, person string) (map[string]CutEdge, error) {
	persons, err := s.aliasSets(ctx, []string{person})
	if err != nil {
		return nil, err
	}
	cut, err := s.cutAccounts(ctx, persons)
	if err != nil || len(cut) == 0 {
		return map[string]CutEdge{}, err
	}
	edges, err := s.directCutEdges(ctx, persons)
	if err != nil {
		return nil, err
	}
	out := map[string]CutEdge{}
	for _, e := range edges {
		if _, ok := out[e.AccountRef]; !ok && cut[e.AccountRef] {
			out[e.AccountRef] = e
		}
	}
	for _, e := range edges {
		set, err := s.aliasSets(ctx, []string{e.AccountRef})
		if err != nil {
			return nil, err
		}
		for _, r := range set {
			if _, ok := out[r]; !ok && cut[r] {
				out[r] = e
			}
		}
	}
	return out, nil
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
		  AND `+eWithdrawnAtSQL+` IS NULL
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
	// CutBy is the distinct-from that cuts it, set when Cut is.
	CutBy CutEdge
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
	cuts := map[string]map[string]CutEdge{} // by person, read once each
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
				byPerson, ok := cuts[e.OtherRef]
				if !ok {
					if byPerson, err = s.cutsFrom(ctx, e.OtherRef); err != nil {
						return AccountIdentityView{}, err
					}
					cuts[e.OtherRef] = byPerson
				}
				by, cut := byPerson[r]
				out.Owners = append(out.Owners, Owner{IdentityEdge: e.IdentityEdge, PersonRef: e.OtherRef, AccountRef: r, Cut: cut, CutBy: by})
			}
		}
	}
	if out.SeenAs, err = s.accountSeenAs(ctx, set); err != nil {
		return AccountIdentityView{}, err
	}
	return out, nil
}

// PersonAccount is one belongs-to edge into a person of the asked-for
// person's alias set, as the person's view lists it: the account and the
// record or person that attached it, and the edge's person end, which is
// the asked-for person or one aliased to it.
type PersonAccount struct {
	IdentityEdge
	AccountRef string
	// PersonRef is the person end of the edge.
	PersonRef string
	// AccountAliases is the account's live alias set, itself included: what
	// a read of the person walks from this account.
	AccountAliases []string
	// Cut is set when a live distinct-from holds the account and the person
	// apart (adr/0324): listed, as a cut alias is, but not read.
	Cut bool
	// CutBy is the distinct-from that cuts it, set when Cut is.
	CutBy CutEdge
}

// PersonIdentityView is a person's identity as the record holds it: the
// aliases and distinct-from edges between it and other persons, and the
// accounts attached to it, each with its origin, withdrawn ones included and
// marked.
type PersonIdentityView struct {
	Aliases  []IdentityEdge
	Distinct []IdentityEdge
	Accounts []PersonAccount
	// ReadAccounts is the accounts a read of the person walks, exactly as
	// PersonAccountSet answers them (accounts only): the effective set, cuts
	// applied. The rows' AccountAliases are for display; a consumer that
	// asks which accounts are the person's reads this.
	ReadAccounts []string
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
	// The accounts are those a read of the person walks (PersonAccountSet):
	// the belongs-to edges into any person of its alias set, each listed with
	// its own two ends and origin.
	self := identity.TypeIdentity + "--" + id.String()
	persons, err := s.aliasSets(ctx, []string{self})
	if err != nil {
		return PersonIdentityView{}, err
	}
	cuts, err := s.cutsFrom(ctx, self)
	if err != nil {
		return PersonIdentityView{}, err
	}
	read, err := s.PersonAccountSet(ctx, self)
	if err != nil {
		return PersonIdentityView{}, err
	}
	out.ReadAccounts = []string{}
	for _, r := range read {
		if strings.HasPrefix(r, identity.TypeUserAccount+"--") {
			out.ReadAccounts = append(out.ReadAccounts, r)
		}
	}
	sets := map[string][]string{}
	for _, p := range personsFirst(self, persons) {
		pid, ok := refUUID(p)
		if !ok || !strings.HasPrefix(p, identity.TypeIdentity+"--") {
			continue
		}
		edges, err := s.edgesTouching(ctx, pid, []string{relBelongsTo}, edgeTarget)
		if err != nil {
			return PersonIdentityView{}, err
		}
		for _, e := range edges {
			if !strings.HasPrefix(e.OtherRef, identity.TypeUserAccount+"--") {
				continue
			}
			aliases, ok := sets[e.OtherRef]
			if !ok {
				all, err := s.aliasSets(ctx, []string{e.OtherRef})
				if err != nil {
					return PersonIdentityView{}, err
				}
				aliases = []string{}
				for _, r := range all {
					if strings.HasPrefix(r, identity.TypeUserAccount+"--") {
						aliases = append(aliases, r)
					}
				}
				if !slices.Contains(aliases, e.OtherRef) {
					aliases = append(aliases, e.OtherRef)
				}
				sets[e.OtherRef] = aliases
			}
			by, cut := cuts[e.OtherRef]
			out.Accounts = append(out.Accounts, PersonAccount{IdentityEdge: e.IdentityEdge, AccountRef: e.OtherRef, PersonRef: p, AccountAliases: aliases, Cut: cut, CutBy: by})
		}
	}
	return out, nil
}

// personsFirst orders an alias set with the asked-for person first and the
// rest as the set holds them, so its own edges list first.
func personsFirst(self string, set []string) []string {
	out := []string{self}
	for _, p := range set {
		if p != self {
			out = append(out, p)
		}
	}
	return out
}

// accountSeenAs collects what the user objects of the account set refs
// report. The normalizer records, in each observation's user_accounts, the
// strong accounts it minted for each user object, keyed by the object's
// place in the event (adr/0398). A user object counts when one of its
// accounts is in refs, so a record lends the set only its own users' forms. Each counted
// object is read by the one rule, ocsf.ReadUser (adr/0367), and yields its
// login, its uid and uid_alt keyed by the event's source tool, and its
// principal name. Placeholders are left out.
func (s *Store) accountSeenAs(ctx context.Context, refs []string) (AccountSeenAs, error) {
	out := AccountSeenAs{Logins: []string{}, IDs: []SourceID{}, PrincipalNames: []string{}}
	if len(refs) == 0 {
		return out, nil
	}
	accounts := stix.ObservationUserAccounts.JSONSQL("o.payload")
	// Every account user_accounts lists is in object_refs, so the
	// observations that name one of refs are read first, and only their
	// user_accounts are expanded.
	// #nosec G202 -- the joined fragments are fixed JSON paths from the stix package, never input.
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.source_tool, e.payload, `+accounts+`
		FROM ocsf_events e
		JOIN observed_data_events ode ON ode.ocsf_event_id = e.id
		JOIN stix_objects o ON o.id = ode.observed_data_id AND o.tenant_id = $2
		WHERE `+observationNamesAny("o.payload", "$1::text[]")+` AND EXISTS (
			SELECT 1 FROM jsonb_each(CASE WHEN jsonb_typeof(`+accounts+`) = 'object' THEN `+accounts+` ELSE '{}'::jsonb END) AS a(place, accts),
				jsonb_array_elements_text(CASE WHEN jsonb_typeof(a.accts) = 'array' THEN a.accts ELSE '[]'::jsonb END) AS r(ref)
			WHERE r.ref = ANY($1::text[]))
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
	field := func(obj map[string]any, key string) any { return obj[key] }
	for rows.Next() {
		var tool string
		var payloadJSON, accountsJSON []byte
		if err := rows.Scan(&tool, &payloadJSON, &accountsJSON); err != nil {
			return AccountSeenAs{}, fmt.Errorf("scan account seen-as: %w", err)
		}
		var payload map[string]any
		var placed map[string][]string
		_ = json.Unmarshal(payloadJSON, &payload)
		_ = json.Unmarshal(accountsJSON, &placed)
		for place, accounts := range placed {
			if !slices.ContainsFunc(accounts, func(a string) bool { return slices.Contains(refs, a) }) {
				continue
			}
			user := ocsf.ObjectAt(payload, place)
			if user == nil {
				continue
			}
			u := ocsf.ReadUser(user, field)
			addStr(&out.Logins, u.Login)
			addStr(&out.PrincipalNames, u.PrincipalName)
			addID(tool, u.UID)
			addID(tool, u.UIDAlt)
		}
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

// AccountMatch is a persisted account a typed token names: its ref, type and
// login.
type AccountMatch struct {
	Ref   string
	Type  string
	Login string
	// UserID is the identifier the account carries (a SID, a provider id).
	// A login-keyed account may carry one too, so whether the account is
	// keyed on it is identity.KeyedOnUserID's to say, not its presence.
	UserID string
}

// FindAccounts reads the persisted user-accounts a token names: its login or
// principal name equal to the token, case folded, or its identifier equal to
// it under the fold the account was minted on (a cloud or LDAP id by
// identity.FoldCloudUID, any other lowercased). It answers
// at most limit matches, oldest first, every match when limit is zero or
// less: a caller that decides whether the token names one account reads
// them all, and only the list it shows is bounded.
func (s *Store) FindAccounts(ctx context.Context, token string, limit int) ([]AccountMatch, error) {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "" {
		return []AccountMatch{}, nil
	}
	var bound any // NULL: LIMIT ALL
	if limit > 0 {
		bound = limit
	}
	// #nosec G202 -- the joined fragments are fixed JSON paths from the stix package, never input.
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(payload->>'account_type', ''), COALESCE(payload->>'account_login', ''),
		       COALESCE(payload->>'user_id', '')
		FROM stix_objects
		WHERE tenant_id = $1 AND type = $2 AND (
			lower(payload->>'account_login') = $3 OR lower(`+accountPrincipalNameSQL+`) = $3
			OR CASE WHEN payload->>'account_type' = ANY($6::text[])
				THEN `+userIDFoldSQL+` = $5
				ELSE lower(payload->>'user_id') = $3 END)
		ORDER BY created, id LIMIT $4::bigint
	`, module.SingleTenantUUID, identity.TypeUserAccount, t, bound, identity.FoldCloudUID(token),
		textArray([]string{identity.AccountCloud, identity.AccountLDAP}))
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
	found, err := s.personsNamed(ctx, token)
	if err != nil {
		return nil, err
	}
	// Every account the token names: an owner past a bound would be a
	// person the answer never weighed.
	accounts, err := s.FindAccounts(ctx, token, 0)
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

// guidForm is a GUID in its hyphenated form, the one form
// identity.FoldCloudUID lowercases.
const guidForm = `'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'`

// userIDFoldSQL and directoryIDFoldSQL are identity.FoldCloudUID in SQL over
// an account's user_id and a person's directory_id: trimmed, a GUID
// lowercased, any other value exact.
var (
	userIDFoldSQL      = foldSQL("payload->>'user_id'")
	directoryIDFoldSQL = foldSQL(stix.PersonDirectoryID.SQL("payload"))
)

// foldSQL is identity.FoldCloudUID in SQL over the text expression v.
func foldSQL(v string) string {
	return `(CASE WHEN btrim(` + v + `) ~* ` + guidForm + ` THEN lower(btrim(` + v + `)) ELSE btrim(` + v + `) END)`
}

// personsNamed reads the persisted persons whose principal name or mail is
// token, case folded, or whose directory id is token under the fold the
// person was minted on (identity.FoldCloudUID).
func (s *Store) personsNamed(ctx context.Context, token string) ([]string, error) {
	// #nosec G202 -- the joined fragments are fixed JSON paths from the stix package, never input.
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM stix_objects
		WHERE tenant_id = $1 AND type = $2 AND payload->>'identity_class' = $4 AND (
			lower(`+personPrincipalNameSQL+`) = $3 OR lower(`+personMailSQL+`) = $3
			OR `+directoryIDFoldSQL+` = $5)
		ORDER BY created, id
	`, module.SingleTenantUUID, identity.TypeIdentity, strings.ToLower(strings.TrimSpace(token)), identityClassIndividual, identity.FoldCloudUID(token))
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
		  AND `+eWithdrawnAtSQL+` IS NULL
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
