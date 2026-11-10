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
	"github.com/sd-strax/reckon/module"
)

// Identity reads (adr/0211). A host's id is its short hostname; what the key
// cannot join is an aliases edge in stix_edges, from a source's record or a
// person's assertion, and what it wrongly joins is separated by a
// distinct-from edge. Reads by entity follow the alias set computed here, so
// nothing is merged, re-minted or deleted: withdrawing an edge splits the
// reads again with no data to repair.

// Relationship types the identity reads follow.
const (
	relAliases      = "aliases"
	relDistinctFrom = "distinct-from"
)

// aliasDepth bounds the alias walk: a chain longer than this is not followed.
const aliasDepth = 8

// AliasSet returns the refs that read as one entity with ref, ref first, by
// one rule that breaks no tie (an edge whose payload carries a withdrawn_at
// is not live):
//
//   - live aliases edges join; a live distinct-from pair is never crossed;
//   - a node is contested when it is reachable from both ends of some live
//     distinct-from pair, each end reached without passing through the
//     other; a contested node reads alone;
//   - otherwise ref's set is what it reaches through aliases edges within
//     aliasDepth steps, never crossing a distinct-from pair and never
//     entering a contested node.
//
// Contested does not depend on ref and reaching is symmetric, so b is in a's
// set exactly when a is in b's, and no set holds both ends of a live
// distinct-from pair. Symmetry holds within the neighbourhood the store
// loads, the nodes aliasDepth aliases steps from the asked-for refs: a pair
// or a path beyond it is not seen. The identity views still list every
// alias, so a contested host's dispute is visible. A ref with no edges is its own set.
func (s *Store) AliasSet(ctx context.Context, ref string) ([]string, error) {
	if _, ok := refUUID(ref); !ok {
		return nil, ErrMalformedRef
	}
	return s.aliasSets(ctx, []string{ref})
}

// aliasSets is AliasSet over several refs at once, the union of their sets in
// the order the refs are given, each ref before the members it reaches.
// Malformed refs are skipped.
func (s *Store) aliasSets(ctx context.Context, refs []string) ([]string, error) {
	seeds := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		if id, ok := refUUID(r); ok {
			seeds = append(seeds, id)
		}
	}
	if len(seeds) == 0 {
		return []string{}, nil
	}
	g, err := s.aliasGraph(ctx, seeds)
	if err != nil {
		return nil, err
	}
	contested := g.contested() // one graph, one contested set for every seed
	out := []string{}
	for _, r := range refs {
		id, ok := refUUID(r)
		if !ok {
			continue
		}
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
		for _, m := range g.walk(id, contested) {
			if ref := g.types[m] + "--" + m.String(); !slices.Contains(out, ref) {
				out = append(out, ref)
			}
		}
	}
	return out, nil
}

// aliasGraph is the neighbourhood of some seeds: the persisted nodes within
// aliasDepth live aliases steps, the live aliases edges among them, and the
// live distinct-from pairs among them.
type aliasGraph struct {
	types  map[uuid.UUID]string
	joined map[uuid.UUID][]uuid.UUID
	apart  map[[2]uuid.UUID]bool
}

// aliasGraph reads the seeds' neighbourhood in two queries: the nodes the
// aliases edges reach, then the live identity edges among those nodes.
func (s *Store) aliasGraph(ctx context.Context, seeds []uuid.UUID) (aliasGraph, error) {
	g := aliasGraph{types: map[uuid.UUID]string{}, joined: map[uuid.UUID][]uuid.UUID{}, apart: map[[2]uuid.UUID]bool{}}
	// The store is single-tenant (Persist writes module.SingleTenantUUID); the
	// tenant_id filter is what the edge and object indexes lead with.
	tenant := module.SingleTenantUUID
	rows, err := s.db.QueryContext(ctx, `
		WITH RECURSIVE
		-- Each step reads only the live aliases edges at the frontier, in
		-- both directions; the lateral join keeps the recursive reference
		-- single, as Postgres requires.
		walk (id, depth) AS (
			SELECT seed, 0 FROM unnest($1::uuid[]) AS seed
			UNION
			SELECT n.other, w.depth + 1 FROM walk w
			CROSS JOIN LATERAL (
				SELECT e.target_ref AS other FROM stix_edges e
				WHERE e.tenant_id = $3 AND e.source_ref = w.id
				  AND e.relationship_type = '`+relAliases+`' AND e.payload->>'withdrawn_at' IS NULL
				UNION ALL
				SELECT e.source_ref FROM stix_edges e
				WHERE e.tenant_id = $3 AND e.target_ref = w.id
				  AND e.relationship_type = '`+relAliases+`' AND e.payload->>'withdrawn_at' IS NULL
			) n
			WHERE w.depth < $2
		)
		SELECT DISTINCT w.id, o.type FROM walk w JOIN stix_objects o ON o.id = w.id AND o.tenant_id = $3
	`, uuidArray(seeds), aliasDepth, tenant)
	if err != nil {
		return aliasGraph{}, fmt.Errorf("query alias neighbourhood: %w", err)
	}
	defer rows.Close()
	nodes := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var typ string
		if err := rows.Scan(&id, &typ); err != nil {
			return aliasGraph{}, fmt.Errorf("scan alias neighbourhood: %w", err)
		}
		g.types[id] = typ
		nodes = append(nodes, id)
	}
	if err := rows.Err(); err != nil {
		return aliasGraph{}, fmt.Errorf("iterate alias neighbourhood: %w", err)
	}
	if len(nodes) == 0 {
		return g, nil
	}
	edges, err := s.db.QueryContext(ctx, `
		SELECT source_ref, target_ref, relationship_type FROM stix_edges
		WHERE tenant_id = $2 AND relationship_type IN ('`+relAliases+`', '`+relDistinctFrom+`')
		  AND payload->>'withdrawn_at' IS NULL
		  AND source_ref = ANY($1) AND target_ref = ANY($1)
	`, uuidArray(nodes), tenant)
	if err != nil {
		return aliasGraph{}, fmt.Errorf("query identity edges among the neighbourhood: %w", err)
	}
	defer edges.Close()
	for edges.Next() {
		var a, b uuid.UUID
		var relType string
		if err := edges.Scan(&a, &b, &relType); err != nil {
			return aliasGraph{}, fmt.Errorf("scan identity edge: %w", err)
		}
		if relType == relDistinctFrom {
			g.apart[[2]uuid.UUID{a, b}], g.apart[[2]uuid.UUID{b, a}] = true, true
			continue
		}
		g.joined[a] = append(g.joined[a], b)
		g.joined[b] = append(g.joined[b], a)
	}
	return g, edges.Err()
}

// walk returns seed's alias set by the AliasSet rule, seed excluded, in
// (depth, id) order for a stable answer: nothing when seed is contested,
// else what seed reaches without crossing a distinct-from pair or entering a
// contested node. contested is g.contested(), computed once per graph by the
// caller.
func (g aliasGraph) walk(seed uuid.UUID, contested map[uuid.UUID]bool) []uuid.UUID {
	if contested[seed] {
		return nil
	}
	component := g.reach(seed, contested)
	out := make([]uuid.UUID, 0, len(component))
	for id := range component {
		if id != seed {
			out = append(out, id)
		}
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int {
		if c := component[a] - component[b]; c != 0 {
			return c
		}
		return strings.Compare(a.String(), b.String())
	})
	return out
}

// contested returns the nodes reachable from both ends of some live
// distinct-from pair, each end reached without passing through the other; the
// ends themselves are not contested by their own pair.
func (g aliasGraph) contested() map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	// apart holds each pair in both directions; take each pair once.
	for pair := range g.apart {
		x, y := pair[0], pair[1]
		if strings.Compare(x.String(), y.String()) > 0 {
			continue
		}
		fromY := g.reach(y, map[uuid.UUID]bool{x: true})
		for n := range g.reach(x, map[uuid.UUID]bool{y: true}) {
			if _, ok := fromY[n]; ok && n != x && n != y {
				out[n] = true
			}
		}
	}
	return out
}

// reach walks the persisted nodes joined to seed by aliases edges, at most
// aliasDepth steps, never along an edge whose ends are apart and never into a
// node in skip; it returns each node reached with its depth.
func (g aliasGraph) reach(seed uuid.UUID, skip map[uuid.UUID]bool) map[uuid.UUID]int {
	depth := map[uuid.UUID]int{seed: 0}
	level := []uuid.UUID{seed}
	for d := 1; d <= aliasDepth && len(level) > 0; d++ {
		var next []uuid.UUID
		for _, n := range level {
			for _, m := range g.joined[n] {
				if _, seen := depth[m]; seen || skip[m] || g.types[m] == "" || g.apart[[2]uuid.UUID{n, m}] {
					continue
				}
				depth[m] = d
				next = append(next, m)
			}
		}
		level = next
	}
	return depth
}

// IdentityEdge is one aliases or distinct-from edge that touches a host, as
// its inspector lists it. Kind "observed" is a source's record (the
// normalizer's aliases from one record, Source the tool that gave it);
// "asserted" is a person's, with who, when and the note.
type IdentityEdge struct {
	EdgeID    string
	OtherRef  string
	Kind      string
	Source    string
	By        string
	ByName    string
	At        time.Time
	Note      string
	Withdrawn bool
}

// Identity edge kinds.
const (
	IdentityObserved = "observed"
	IdentityAsserted = "asserted"
)

// SourceID is an agent or device id a source reported for a host.
type SourceID struct {
	Tool string
	UID  string
}

// SeenAs is what the record has seen under a host's id: the distinct domains,
// the agent and device ids by source, and the addresses, from the device
// objects of the events that reference it. It is computed on every read and
// never stored; one hostname under two domains is a fact for the analyst to
// read, not a judgement the engine makes (adr/0211).
type SeenAs struct {
	Domains []string
	Agents  []SourceID
	IPs     []string
}

// HostIdentityView is a host's identity as the record holds it: the aliases
// and distinct-from edges that touch it, withdrawn ones included and marked,
// and what it has been seen as.
type HostIdentityView struct {
	Aliases  []IdentityEdge
	Distinct []IdentityEdge
	SeenAs   SeenAs
}

// HostIdentity reads a host's identity view. The edges are the ones with ref
// at either end. SeenAs reads the events whose observed-data names ref or a
// member of its alias set, and keeps the device objects (device, src_endpoint,
// dst_endpoint, and the same under a detection's evidence) that name one of
// the set's hosts, each object on its own first tier: by hostname, else by
// MAC, else by the (source tool, device uid) a host was minted on. So an
// unjoined weak host shows its own records' facts, and a record naming a
// joined host by its device id alone adds to the strong host's.
func (s *Store) HostIdentity(ctx context.Context, ref string) (HostIdentityView, error) {
	id, ok := refUUID(ref)
	if !ok {
		return HostIdentityView{}, ErrMalformedRef
	}
	out := HostIdentityView{Aliases: []IdentityEdge{}, Distinct: []IdentityEdge{}, SeenAs: SeenAs{Domains: []string{}, Agents: []SourceID{}, IPs: []string{}}}
	if err := s.identityEdges(ctx, id, &out); err != nil {
		return HostIdentityView{}, err
	}
	set, err := s.aliasSets(ctx, []string{ref})
	if err != nil {
		return HostIdentityView{}, err
	}
	match, err := s.hostMatchers(ctx, set)
	if err != nil {
		return HostIdentityView{}, err
	}
	if match.empty() {
		return out, nil // nothing a device object could be matched on
	}
	out.SeenAs, err = s.seenAs(ctx, set, match)
	if err != nil {
		return HostIdentityView{}, err
	}
	return out, nil
}

// identityEdges fills the aliases and distinct-from edges with id at either
// end, oldest first.
func (s *Store) identityEdges(ctx context.Context, id uuid.UUID, out *HostIdentityView) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.relationship_type, CASE WHEN e.source_ref = $1 THEN e.target_ref ELSE e.source_ref END AS other,
		       COALESCE(o.type, ''), e.payload, e.created
		FROM stix_edges e
		LEFT JOIN stix_objects o ON o.tenant_id = $2 AND o.id = CASE WHEN e.source_ref = $1 THEN e.target_ref ELSE e.source_ref END
		WHERE e.tenant_id = $2 AND e.relationship_type IN ('`+relAliases+`', '`+relDistinctFrom+`')
		  AND (e.source_ref = $1 OR e.target_ref = $1)
		ORDER BY e.created, e.id
	`, id, module.SingleTenantUUID)
	if err != nil {
		return fmt.Errorf("query identity edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var edgeID, other uuid.UUID
		var relType, otherType string
		var payload []byte
		var created time.Time
		if err := rows.Scan(&edgeID, &relType, &other, &otherType, &payload, &created); err != nil {
			return fmt.Errorf("scan identity edge: %w", err)
		}
		if otherType == "" {
			continue // the other end is not persisted: nothing to open
		}
		e := identityEdge(payload, created)
		e.EdgeID = "relationship--" + edgeID.String()
		e.OtherRef = otherType + "--" + other.String()
		if relType == relAliases {
			out.Aliases = append(out.Aliases, e)
		} else {
			out.Distinct = append(out.Distinct, e)
		}
	}
	return rows.Err()
}

// Assertion is the payload part of an aliases or distinct-from edge a person
// asserted (adr/0211): who, when, the note, the withdrawal once withdrawn,
// and a thin history of the acts on it (02 §2.2, CRUD with history). It is
// stored beside the relationship's own fields; an observed edge, a
// normalizer's, carries provenance instead.
type Assertion struct {
	AssertedBy     string           `json:"asserted_by"`
	AssertedByName string           `json:"asserted_by_name,omitempty"`
	AssertedAt     time.Time        `json:"asserted_at"`
	Note           string           `json:"note,omitempty"`
	WithdrawnAt    *time.Time       `json:"withdrawn_at"`
	History        []AssertionEvent `json:"history,omitempty"`
}

// AssertionEvent is one act in an assertion's history: asserted or
// withdrawn, by whom, when, with the note given.
type AssertionEvent struct {
	Act    string    `json:"act"`
	By     string    `json:"by"`
	ByName string    `json:"by_name,omitempty"`
	At     time.Time `json:"at"`
	Note   string    `json:"note,omitempty"`
}

// identityEdge reads an edge's origin from its payload: an asserted edge
// carries an Assertion; an observed one carries the normalizer's provenance.
func identityEdge(payload []byte, created time.Time) IdentityEdge {
	var p struct {
		Assertion
		Provenance struct {
			Tool       string    `json:"tool"`
			ObservedAt time.Time `json:"observed_at"`
		} `json:"provenance"`
	}
	_ = json.Unmarshal(payload, &p)
	e := IdentityEdge{At: created, Withdrawn: p.WithdrawnAt != nil, Note: p.Note}
	if p.AssertedBy != "" {
		e.Kind, e.By, e.ByName = IdentityAsserted, p.AssertedBy, p.AssertedByName
		if !p.AssertedAt.IsZero() {
			e.At = p.AssertedAt
		}
		return e
	}
	e.Kind, e.Source = IdentityObserved, p.Provenance.Tool
	if !p.Provenance.ObservedAt.IsZero() {
		e.At = p.Provenance.ObservedAt
	}
	return e
}

// hostMatch is what a device object may name a host set by: canonical short
// hostnames, folded MACs, and (source tool, device uid) pairs, tool folded.
type hostMatch struct {
	names, macs, tools, uids []string
}

func (m hostMatch) empty() bool { return len(m.names) == 0 && len(m.macs) == 0 && len(m.uids) == 0 }

func (m *hostMatch) addName(v string) {
	if short, _ := identity.SplitHostname(v); short != "" && !slices.Contains(m.names, short) {
		m.names = append(m.names, short)
	}
}

func (m *hostMatch) addMAC(v string) {
	if mac := identity.FoldMAC(v); mac != "" && !slices.Contains(m.macs, mac) {
		m.macs = append(m.macs, mac)
	}
}

func (m *hostMatch) addSource(tool, uid string) {
	tool, uid = strings.ToLower(strings.TrimSpace(tool)), strings.TrimSpace(uid)
	if identity.PlaceholderDeviceUID(uid) {
		return
	}
	for i := range m.uids {
		if m.tools[i] == tool && m.uids[i] == uid {
			return
		}
	}
	m.tools, m.uids = append(m.tools, tool), append(m.uids, uid)
}

// literalPlaceholderSQL is true, in the seen-as query, for a device object
// whose hostname field h holds an address literal (literal) that
// identity.PlaceholderHostname refuses as identity.PlaceholderIP does: an
// unspecified, loopback or broadcast address, IPv4-mapped forms included. A
// value Postgres cannot read as a bare address is no address, and neither,
// as net.ParseIP has it, is one with a mask or a dotted-decimal octet with a
// leading zero, which Postgres would read; TestLiteralPlaceholderAgrees holds
// the two to a table.
const literalPlaceholderSQL = `CASE WHEN literal AND strpos(h, '/') = 0 AND h !~ '((^|[.:])0[0-9]+\.|\.0[0-9]+$)'
			AND pg_input_is_valid(h, 'inet') THEN
			h::inet = ANY('{0.0.0.0,255.255.255.255,::,::1,::ffff:0.0.0.0,::ffff:255.255.255.255}'::inet[])
			OR h::inet << '127.0.0.0/8'::inet OR h::inet << '::ffff:127.0.0.0/104'::inet
		ELSE false END`

// macForm matches, on a trimmed lowercase value, exactly the MAC forms
// identity.FoldMAC accepts: six bytes as colon- or dash-separated pairs,
// dotted groups of four, or 12 bare hex digits; eight bytes as colon- or
// dash-separated pairs, or 16 bare hex digits. The seen-as query uses it to
// choose an object's MAC tier as identity.PlaceholderMAC does; the two must
// agree, and TestMACFormAgreesWithFoldMAC holds them to it.
const macForm = `^([0-9a-f]{2}(:[0-9a-f]{2}){5}|[0-9a-f]{2}(-[0-9a-f]{2}){5}|[0-9a-f]{4}(\.[0-9a-f]{4}){2}|[0-9a-f]{12}` +
	`|[0-9a-f]{2}(:[0-9a-f]{2}){7}|[0-9a-f]{2}(-[0-9a-f]{2}){7}|[0-9a-f]{16})$`

// hostMatchers reads what the x-hosts among refs can be matched on: only the
// identifier each was minted on (identity.Host's tiers, a placeholder read as
// absent by the same identity predicates). A strong host
// contributes its short hostname and nothing else; its mac and device ids are
// facts reported about it, joined (or not) through the weak hosts' aliases
// edges. A weak host contributes its mac if it has one, else its
// (source_tool, device_uid). So a cut or contested alias really cuts: a weak
// host outside the set lends the set none of its identifiers.
func (s *Store) hostMatchers(ctx context.Context, refs []string) (hostMatch, error) {
	var m hostMatch
	ids := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		if id, ok := refUUID(r); ok && strings.HasPrefix(r, identity.TypeHost+"--") {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return m, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(payload->'properties', '{}'::jsonb) FROM stix_objects
		WHERE tenant_id = $3 AND id = ANY($1) AND type = $2
	`, uuidArray(ids), identity.TypeHost, module.SingleTenantUUID)
	if err != nil {
		return hostMatch{}, fmt.Errorf("query host identifiers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return hostMatch{}, fmt.Errorf("scan host identifiers: %w", err)
		}
		// The keys capability's host extractor writes: hostname on a strong
		// host; mac, or source_tool and device_uid, on a weak one.
		var p struct {
			Hostname   string `json:"hostname"`
			MAC        string `json:"mac"`
			SourceTool string `json:"source_tool"`
			DeviceUID  string `json:"device_uid"`
		}
		_ = json.Unmarshal(raw, &p)
		// The tier is identity's: a placeholder reads as absent (adr/0211).
		switch {
		case !identity.PlaceholderHostname(p.Hostname):
			m.addName(p.Hostname)
		case !identity.PlaceholderMAC(p.MAC):
			m.addMAC(p.MAC)
		default:
			m.addSource(p.SourceTool, p.DeviceUID)
		}
	}
	return m, rows.Err()
}

// seenAs collects what the device objects of the events referencing any of
// refs report, for the objects that name the host set. The extraction is SQL:
// each device object (device, src_endpoint, dst_endpoint, at the top level
// and under a detection's evidence, 03 §4.12) is matched on its own first
// tier only, the tier identity.Host would mint it on. An object with a
// hostname matches only when the hostname folds to one of the names (an
// address in the hostname field compared whole, as identity.SplitHostname
// keeps it); its MAC and uid are not consulted, because under adr/0211 that
// object mints that hostname's host, and its weaker ids are that host's facts,
// not another's. An object with no hostname but a MAC matches only by the
// folded MAC; one with neither matches only by its (event source tool, uid).
// A matched object yields its domain, the FQDN's remainder, its uid, uid_alt
// and the agent_list uids keyed by the event's source tool, and its address.
// A placeholder (identity.Placeholder*) reads as absent when choosing an
// object's tier and is never yielded as a domain, an agent id or an address.
func (s *Store) seenAs(ctx context.Context, refs []string, match hostMatch) (SeenAs, error) {
	out := SeenAs{Domains: []string{}, Agents: []SourceID{}, IPs: []string{}}
	rows, err := s.db.QueryContext(ctx, `
		WITH ev AS (
			SELECT e.source_tool, e.payload FROM ocsf_events e
			WHERE EXISTS (
				SELECT 1 FROM observed_data_events ode JOIN stix_objects o ON o.id = ode.observed_data_id AND o.tenant_id = $8
				WHERE ode.ocsf_event_id = e.id AND o.payload->'object_refs' ?| $1::text[])
		),
		dev AS (
			SELECT source_tool, payload->'device' AS d FROM ev
			UNION ALL SELECT source_tool, payload->'src_endpoint' FROM ev
			UNION ALL SELECT source_tool, payload->'dst_endpoint' FROM ev
			UNION ALL SELECT source_tool, payload->'evidence'->'device' FROM ev
			UNION ALL SELECT source_tool, payload->'evidence'->'src_endpoint' FROM ev
			UNION ALL SELECT source_tool, payload->'evidence'->'dst_endpoint' FROM ev
		),
		-- literal: an address in the hostname field, compared whole. The rule
		-- must agree with identity.SplitHostname: only digits and dots, or a
		-- colon (every IP the Go side parses is one of the two).
		named AS (
			SELECT source_tool, d, h, (h ~ '^[0-9.]+$' OR h LIKE '%:%') AS literal
			FROM (
				SELECT source_tool, d, lower(rtrim(btrim(COALESCE(d->>'hostname', '')), '.')) AS h
				FROM dev WHERE jsonb_typeof(d) = 'object'
			) z
		),
		-- Each object matches on its own first tier only (hostname, else MAC,
		-- else the source's uid), as identity.Host mints, a placeholder read
		-- as absent: the hostname tier unless the hostname is a placeholder
		-- (noname: the short name is one of identity.PlaceholderHostnames,
		-- $6, which holds '', or it is an address literal identity.PlaceholderIP
		-- refuses, literalPlaceholderSQL), the MAC tier when the MAC has a form
		-- identity.FoldMAC accepts (macForm, $7) and its digits are not all
		-- zero or all f, as identity.PlaceholderMAC rules. The fold of an
		-- accepted form (its hex digits, lowercased) must agree with
		-- identity.FoldMAC.
		tiered AS (
			SELECT source_tool, d, h, literal, short, mac, (short = ANY($6::text[]) OR `+literalPlaceholderSQL+`) AS noname
			FROM (
				SELECT source_tool, d, h, literal,
					CASE WHEN literal THEN h ELSE split_part(h, '.', 1) END AS short,
					lower(btrim(COALESCE(d->>'mac', ''))) AS mac
				FROM named
			) t
		),
		m AS (
			SELECT source_tool, d, h, literal, noname FROM tiered
			WHERE CASE
				WHEN NOT noname THEN short = ANY($2::text[])
				WHEN mac ~ $7 AND regexp_replace(mac, '[^0-9a-f]', '', 'g') !~ '^(0+|f+)$' THEN
					regexp_replace(mac, '[^0-9a-f]', '', 'g') = ANY($3::text[])
				ELSE EXISTS (
					SELECT 1 FROM unnest($4::text[], $5::text[]) AS p(tool, uid)
					WHERE p.tool = lower(btrim(source_tool)) AND p.uid = btrim(d->>'uid'))
			END
		)
		SELECT DISTINCT kind, tool, value FROM (
			SELECT 'domain' AS kind, '' AS tool, lower(btrim(d->>'domain')) AS value FROM m
			-- The FQDN's remainder only when the hostname names a host: the
			-- "localdomain" of localhost.localdomain is no domain.
			UNION ALL SELECT 'domain', '', substr(h, strpos(h, '.') + 1) FROM m WHERE NOT literal AND NOT noname AND strpos(h, '.') > 0
			UNION ALL SELECT 'agent', source_tool, btrim(d->>'uid') FROM m
			UNION ALL SELECT 'agent', source_tool, btrim(d->>'uid_alt') FROM m
			UNION ALL SELECT 'agent', source_tool, btrim(a->>'uid') FROM m,
				jsonb_array_elements(CASE WHEN jsonb_typeof(d->'agent_list') = 'array' THEN d->'agent_list' ELSE '[]'::jsonb END) AS a
			UNION ALL SELECT 'ip', '', btrim(d->>'ip') FROM m
		) x
		-- A placeholder domain is no discriminator: the hostname list, $6.
		WHERE value IS NOT NULL AND value <> '' AND NOT (kind = 'domain' AND value = ANY($6::text[]))
	`, textArray(refs), textArray(match.names), textArray(match.macs), textArray(match.tools), textArray(match.uids),
		textArray(identity.PlaceholderHostnames()), macForm, module.SingleTenantUUID)
	if err != nil {
		return SeenAs{}, fmt.Errorf("query seen-as: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, tool, value string
		if err := rows.Scan(&kind, &tool, &value); err != nil {
			return SeenAs{}, fmt.Errorf("scan seen-as: %w", err)
		}
		// A placeholder id or address is no discriminator (adr/0211); the
		// domains were filtered in the query.
		switch {
		case kind == "domain":
			out.Domains = append(out.Domains, value)
		case kind == "agent" && !identity.PlaceholderDeviceUID(value):
			out.Agents = append(out.Agents, SourceID{Tool: tool, UID: value})
		case kind == "ip" && !identity.PlaceholderIP(value):
			out.IPs = append(out.IPs, value)
		}
	}
	if err := rows.Err(); err != nil {
		return SeenAs{}, fmt.Errorf("iterate seen-as: %w", err)
	}
	slices.Sort(out.Domains)
	slices.Sort(out.IPs)
	slices.SortFunc(out.Agents, func(a, b SourceID) int {
		if c := strings.Compare(a.Tool, b.Tool); c != 0 {
			return c
		}
		return strings.Compare(a.UID, b.UID)
	})
	return out, nil
}

// textArray renders refs as a Postgres text[] literal, each element quoted.
func textArray(refs []string) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		parts = append(parts, `"`+strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(r)+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}
