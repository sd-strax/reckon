package main

import (
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/sd-strax/reckon/internal/adapterplugin"
)

// TestSchemaSource_ProfileMatchesQueries: describe declares the query kind
// with one profile whose fields are exactly the top-level columns the queries
// read plus the nested paths the queries themselves name — rendered here from
// real activity and history calls covering every filter, independently of the
// health probe the profile is built from — compared as sets both ways.
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
	}{
		{cloudCall{principalName: "p", credential: "k", accountUID: "a", operation: "o", service: "s", outcome: "SUCCESS",
			resource: resourceScope{name: "n", typ: "t", region: "r", accountUID: "a"}, limit: 1, from: from, to: to}, false},
		{cloudCall{outcome: "FAILURE", resource: resourceScope{region: "r"}, limit: 1, from: from, to: to}, false},
		{cloudCall{resource: resourceScope{uid: "u"}, limit: 1, from: from, to: to}, false},
		{cloudCall{resource: resourceScope{name: "n", region: "r"}, limit: 1, from: from, to: to}, true},
		{cloudCall{resource: resourceScope{uid: "u"}, limit: 1, from: from, to: to}, true},
	}
	want := requiredColumns()
	for _, c := range calls {
		q, err := activityQuery("db", "tbl", []string{"000000000000"}, c.call, c.history)
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
	sql := `WHERE a1.b_2.c3 = ? AND any_match(resources, x9 -> x9.name = ? AND x9.region = ?) AND any_match( items ,it->it.k_1 = ?) AND "db"."tbl" AND a1.b_2.c3 = ?`
	got := sqlFieldPaths(sql)
	want := []string{"a1.b_2.c3", "resources[].name", "resources[].region", "items[].k_1"}
	if !slices.Equal(got, want) {
		t.Errorf("paths = %q; want %q", got, want)
	}
}
