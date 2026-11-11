package main

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sd-strax/reckon/internal/adapterplugin"
)

// TestSchemaSource_ProfileMatchesQueries: describe declares the query kind
// with one profile whose fields are exactly the top-level columns the queries
// may read plus the nested paths the queries themselves name against a table
// carrying every candidate — rendered here from real activity and history
// calls covering every filter, independently of candidateFields — compared as
// sets both ways.
func TestSchemaSource_ProfileMatchesQueries(t *testing.T) {
	src := describe().SchemaSource
	if src == nil || src.Kind != adapterplugin.SchemaSourceQuery || len(src.Profiles) != 1 {
		t.Fatalf("schema_source = %+v; want kind query with one profile", src)
	}
	p := src.Profiles[0]
	if p.Name != profileName || p.Store == "" || p.Version == "" {
		t.Fatalf("profile = %+v", p)
	}

	from, to := time.Unix(0, 0).UTC(), time.Unix(3600, 0).UTC()
	calls := []struct {
		call    cloudCall
		history bool
		schema  *tableSchema
	}{
		{cloudCall{principalName: "p", credential: "k", accountUID: "a", operation: "o", service: "s", outcome: "SUCCESS",
			resource: resourceScope{name: "n", typ: "t", region: "r", accountUID: "a"}, limit: 1, from: from, to: to}, false, nil},
		{cloudCall{outcome: "FAILURE", resource: resourceScope{region: "r"}, limit: 1, from: from, to: to}, false, nil},
		{cloudCall{resource: resourceScope{uid: "u"}, limit: 1, from: from, to: to}, false, nil},
		{cloudCall{resource: resourceScope{name: "n", region: "r"}, limit: 1, from: from, to: to}, true, nil},
		{cloudCall{resource: resourceScope{uid: "u"}, limit: 1, from: from, to: to}, true, nil},
		{cloudCall{resource: resourceScope{name: "n", zone: "z", group: "g", namespace: "ns"}, limit: 1, from: from, to: to}, false, nil},
		// cloud.region is the event region only on a table without the
		// region column.
		{cloudCall{resource: resourceScope{region: "r"}, limit: 1, from: from, to: to}, false, schemaWithout("region")},
	}
	// The scope fields are conditions in the query, but resources[].zone,
	// .group and .namespace themselves are listed beside the paths the SQL
	// names (resources[].group.name, …) as the fields filterScope's agreeing
	// check reads.
	want := slices.Concat(predicateOnlyColumns, postScopeFields)
	for _, c := range projection {
		want = append(want, c.source)
	}
	for _, c := range calls {
		s := c.schema
		if s == nil {
			s = fullSchema
		}
		q, err := activityQuery(s, "db", "tbl", []string{"000000000000"}, c.call, c.history)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range sqlFieldPaths(q.sql) {
			if !slices.Contains(want, f) {
				want = append(want, f)
			}
		}
	}
	got := slices.Clone(p.Fields)
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, slices.Compact(slices.Clone(got))) {
		t.Errorf("profile lists a field twice: %q", p.Fields)
	}
	for _, f := range want {
		if !slices.Contains(got, f) {
			t.Errorf("profile lacks %q, which the queries read", f)
		}
	}
	for _, f := range got {
		if !slices.Contains(want, f) {
			t.Errorf("profile lists %q, which no query reads", f)
		}
	}
}

// TestSQLFieldPaths: dotted paths of letters, digits and underscores are
// read, and any lambda variable is replaced by the array it walks.
func TestSQLFieldPaths(t *testing.T) {
	sql := `WHERE a1.b_2.c3 = ? AND any_match(resources, x9 -> x9.name = ? AND x9."group".uid = ? AND x9.region = ?) AND any_match( items ,it->it.k_1 = ?) AND "db"."tbl" AND a1.b_2.c3 = ?`
	got := sqlFieldPaths(sql)
	want := []string{"a1.b_2.c3", "resources[].name", "resources[].group.uid", "resources[].region", "items[].k_1"}
	if !slices.Equal(got, want) {
		t.Errorf("paths = %q; want %q", got, want)
	}
}

// Patterns that read field paths out of rendered SQL: a dotted path (a
// segment after the first may be a quoted identifier, `r."group".name`), and
// an any_match lambda binding its variable to the array it walks.
var (
	fieldPathRE = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*(?:\.(?:[A-Za-z_][A-Za-z0-9_]*|"[A-Za-z_][A-Za-z0-9_]*"))+`)
	lambdaRE    = regexp.MustCompile(`any_match\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*,\s*([A-Za-z_][A-Za-z0-9_]*)\s*->`)
)

// sqlFieldPaths lists, in order of first use, the nested field paths a
// statement names: each dotted path, with a lambda variable replaced by the
// array it walks (`r.name` in `any_match(resources, r -> …)` is
// `resources[].name`).
func sqlFieldPaths(sql string) []string {
	arrays := map[string]string{}
	for _, m := range lambdaRE.FindAllStringSubmatch(sql, -1) {
		arrays[m[2]] = m[1]
	}
	var out []string
	for _, p := range fieldPathRE.FindAllString(sql, -1) {
		p = strings.ReplaceAll(p, `"`, "")
		head, rest, _ := strings.Cut(p, ".")
		if arr, ok := arrays[head]; ok {
			p = arr + "[]." + rest
		}
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// projectedNames are the result column names of the full projection, in
// order.
func projectedNames() []string {
	out := make([]string, len(projection))
	for i, c := range projection {
		out[i] = c.name
	}
	return out
}
