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

// AliasSet returns the refs that read as one entity with ref, ref first. The
// set is computed in three steps, none of which breaks a tie:
//
//   - the closure: the undirected walk of the live aliases edges from ref, at
//     most aliasDepth steps, never crossing an aliases edge whose two ends a
//     live distinct-from separates (an edge whose payload carries a
//     withdrawn_at is not live);
//   - the cut: drop every member asserted distinct from ref, both ends of
//     every live distinct-from pair whose two ends are both in the closure,
//     and every member aliased (by an edge no distinct-from cuts) to both
//     ends of a live distinct-from pair, whether or not those ends survive;
//     ref itself is never dropped;
//   - the component: what remains connected to ref through aliases edges;
//     a member reachable only through a dropped one falls out.
//
// So a returned set never holds both ends of a live distinct-from pair. A
// weak host two machines asserted apart both claim is disputed: it joins
// neither and neither reads it, so its events are attributed to no machine
// until the analyst says which. The identity views still list every alias,
// so the dispute is visible. A ref with no edges is its own set.
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
	out := []string{}
	for _, r := range refs {
		id, ok := refUUID(r)
		if !ok {
			continue
		}
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
		for _, m := range g.walk(id) {
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
	rows, err := s.db.QueryContext(ctx, `
		WITH RECURSIVE
		joined AS (
			SELECT source_ref AS a, target_ref AS b FROM stix_edges
			WHERE relationship_type = '`+relAliases+`' AND payload->>'withdrawn_at' IS NULL
			UNION SELECT target_ref, source_ref FROM stix_edges
			WHERE relationship_type = '`+relAliases+`' AND payload->>'withdrawn_at' IS NULL
		),
		walk (id, depth) AS (
			SELECT seed, 0 FROM unnest($1::uuid[]) AS seed
			UNION
			SELECT j.b, w.depth + 1 FROM walk w JOIN joined j ON j.a = w.id WHERE w.depth < $2
		)
		SELECT DISTINCT w.id, o.type FROM walk w JOIN stix_objects o ON o.id = w.id
	`, uuidArray(seeds), aliasDepth)
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
		WHERE relationship_type IN ('`+relAliases+`', '`+relDistinctFrom+`')
		  AND payload->>'withdrawn_at' IS NULL
		  AND source_ref = ANY($1) AND target_ref = ANY($1)
	`, uuidArray(nodes))
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
// (depth, id) order for a stable answer.
func (g aliasGraph) walk(seed uuid.UUID) []uuid.UUID {
	closure := g.reach(seed, nil)
	dropped := map[uuid.UUID]bool{}
	// apart holds each pair in both directions, so dropping y covers both ends.
	// The seed is in its closure, so a member apart from it is dropped too.
	for pair := range g.apart {
		x, y := pair[0], pair[1]
		_, inX := closure[x]
		_, inY := closure[y]
		if inX && inY && y != seed {
			dropped[y] = true
		}
	}
	// A member aliased to both ends of a live distinct-from pair is disputed:
	// two machines asserted apart both claim it, so it joins neither.
	for m := range closure {
		if m == seed {
			continue
		}
		for pair := range g.apart {
			if g.aliased(m, pair[0]) && g.aliased(m, pair[1]) {
				dropped[m] = true
				break
			}
		}
	}
	component := g.reach(seed, dropped)
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

// aliased reports whether a live aliases edge joins a and b that no live
// distinct-from between them cuts.
func (g aliasGraph) aliased(a, b uuid.UUID) bool {
	return !g.apart[[2]uuid.UUID{a, b}] && slices.Contains(g.joined[a], b)
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
// the set's hosts: by hostname, by MAC, or by the (source tool, device uid) a
// host was seen under. So an unjoined weak host shows its own records' facts,
// and a record naming a joined host by its device id alone adds to the
// strong host's.
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
		LEFT JOIN stix_objects o ON o.id = CASE WHEN e.source_ref = $1 THEN e.target_ref ELSE e.source_ref END
		WHERE e.relationship_type IN ('`+relAliases+`', '`+relDistinctFrom+`')
		  AND (e.source_ref = $1 OR e.target_ref = $1)
		ORDER BY e.created, e.id
	`, id)
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
	if mac := foldMAC(v); mac != "" && !slices.Contains(m.macs, mac) {
		m.macs = append(m.macs, mac)
	}
}

func (m *hostMatch) addSource(tool, uid string) {
	tool, uid = strings.ToLower(strings.TrimSpace(tool)), strings.TrimSpace(uid)
	if uid == "" {
		return
	}
	for i := range m.uids {
		if m.tools[i] == tool && m.uids[i] == uid {
			return
		}
	}
	m.tools, m.uids = append(m.tools, tool), append(m.uids, uid)
}

// foldMAC folds a MAC to its bare lowercase hex digits, so every form
// identity's canonical MAC accepts (colon, dash, dotted, bare) folds alike.
// The seen-as query folds the same way in SQL; the two must agree.
func foldMAC(v string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return r
		}
		return -1
	}, v))
}

// hostMatchers reads what the x-hosts among refs can be matched on: a strong
// host's hostname, mac, source_ids and (with no source) device_uid; a weak
// host's mac, or source_tool and device_uid.
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
		WHERE id = ANY($1) AND type = $2
	`, uuidArray(ids), identity.TypeHost)
	if err != nil {
		return hostMatch{}, fmt.Errorf("query host identifiers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return hostMatch{}, fmt.Errorf("scan host identifiers: %w", err)
		}
		var p struct {
			Hostname   string            `json:"hostname"`
			MAC        string            `json:"mac"`
			SourceIDs  map[string]string `json:"source_ids"`
			SourceTool string            `json:"source_tool"`
			DeviceUID  string            `json:"device_uid"`
		}
		_ = json.Unmarshal(raw, &p)
		m.addName(p.Hostname)
		m.addMAC(p.MAC)
		for tool, uid := range p.SourceIDs {
			m.addSource(tool, uid)
		}
		m.addSource(p.SourceTool, p.DeviceUID)
	}
	return m, rows.Err()
}

// seenAs collects what the device objects of the events referencing any of
// refs report, for the objects that name the host set. The extraction is SQL:
// each device object (device, src_endpoint, dst_endpoint, at the top level
// and under a detection's evidence, 03 §4.12) whose hostname folds to a name
// (an address in the hostname field compared whole, as identity.SplitHostname
// keeps it), whose MAC folds to a MAC, or whose (event source tool, uid) is a
// pair the set was seen under, yields its domain, the FQDN's remainder, its
// uid, uid_alt and the agent_list uids keyed by the event's source tool, and
// its address.
func (s *Store) seenAs(ctx context.Context, refs []string, match hostMatch) (SeenAs, error) {
	out := SeenAs{Domains: []string{}, Agents: []SourceID{}, IPs: []string{}}
	rows, err := s.db.QueryContext(ctx, `
		WITH ev AS (
			SELECT e.source_tool, e.payload FROM ocsf_events e
			WHERE EXISTS (
				SELECT 1 FROM observed_data_events ode JOIN stix_objects o ON o.id = ode.observed_data_id
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
		-- The MAC fold (bare lowercase hex digits) must agree with foldMAC.
		m AS (
			SELECT source_tool, d, h, literal FROM named
			WHERE (h <> '' AND (CASE WHEN literal THEN h ELSE split_part(h, '.', 1) END) = ANY($2::text[]))
			   OR lower(regexp_replace(COALESCE(d->>'mac', ''), '[^0-9A-Fa-f]', '', 'g')) = ANY($3::text[])
			   OR EXISTS (
				SELECT 1 FROM unnest($4::text[], $5::text[]) AS p(tool, uid)
				WHERE p.tool = lower(btrim(source_tool)) AND p.uid = btrim(d->>'uid'))
		)
		SELECT DISTINCT kind, tool, value FROM (
			SELECT 'domain' AS kind, '' AS tool, lower(btrim(d->>'domain')) AS value FROM m
			UNION ALL SELECT 'domain', '', substr(h, strpos(h, '.') + 1) FROM m WHERE NOT literal AND strpos(h, '.') > 0
			UNION ALL SELECT 'agent', source_tool, btrim(d->>'uid') FROM m
			UNION ALL SELECT 'agent', source_tool, btrim(d->>'uid_alt') FROM m
			UNION ALL SELECT 'agent', source_tool, btrim(a->>'uid') FROM m,
				jsonb_array_elements(CASE WHEN jsonb_typeof(d->'agent_list') = 'array' THEN d->'agent_list' ELSE '[]'::jsonb END) AS a
			UNION ALL SELECT 'ip', '', btrim(d->>'ip') FROM m
		) x
		WHERE value IS NOT NULL AND value <> ''
	`, textArray(refs), textArray(match.names), textArray(match.macs), textArray(match.tools), textArray(match.uids))
	if err != nil {
		return SeenAs{}, fmt.Errorf("query seen-as: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, tool, value string
		if err := rows.Scan(&kind, &tool, &value); err != nil {
			return SeenAs{}, fmt.Errorf("scan seen-as: %w", err)
		}
		switch kind {
		case "domain":
			out.Domains = append(out.Domains, value)
		case "agent":
			out.Agents = append(out.Agents, SourceID{Tool: tool, UID: value})
		case "ip":
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
