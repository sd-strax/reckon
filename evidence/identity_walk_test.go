package evidence

import (
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// TestAliasGraphWalk_Rule: the alias-set rule over hand-built graphs, and
// its symmetry: in every shape, b is in a's set exactly when a is in b's.
func TestAliasGraphWalk_Rule(t *testing.T) {
	type edge [2]string
	tests := []struct {
		name    string
		nodes   []string
		aliases []edge
		apart   []edge
		want    map[string][]string
	}{
		{
			name:    "weak host between a pair reads alone",
			nodes:   []string{"A", "B", "W"},
			aliases: []edge{{"A", "W"}, {"W", "B"}},
			apart:   []edge{{"A", "B"}},
			want:    map[string][]string{"A": {"A"}, "B": {"B"}, "W": {"W"}},
		},
		{
			name:    "weak host claimed by a pair carries no third across",
			nodes:   []string{"A", "B", "C", "W"},
			aliases: []edge{{"W", "A"}, {"W", "B"}, {"W", "C"}},
			apart:   []edge{{"A", "B"}},
			want:    map[string][]string{"A": {"A"}, "B": {"B"}, "C": {"C"}, "W": {"W"}},
		},
		{
			name:    "no pair: the star reads as one",
			nodes:   []string{"A", "B", "C", "W"},
			aliases: []edge{{"W", "A"}, {"W", "B"}, {"W", "C"}},
			want: map[string][]string{
				"A": {"A", "B", "C", "W"}, "B": {"A", "B", "C", "W"},
				"C": {"A", "B", "C", "W"}, "W": {"A", "B", "C", "W"},
			},
		},
		{
			name:    "chain with its ends apart: every node reads alone",
			nodes:   []string{"A", "W", "B", "V", "C"},
			aliases: []edge{{"A", "W"}, {"W", "B"}, {"B", "V"}, {"V", "C"}},
			apart:   []edge{{"A", "C"}},
			want:    map[string][]string{"A": {"A"}, "W": {"W"}, "B": {"B"}, "V": {"V"}, "C": {"C"}},
		},
		{
			name:    "chain cut in the middle as well: two sides",
			nodes:   []string{"A", "W", "B", "V", "C"},
			aliases: []edge{{"A", "W"}, {"W", "B"}, {"B", "V"}, {"V", "C"}},
			apart:   []edge{{"A", "C"}, {"B", "V"}},
			want: map[string][]string{
				"A": {"A", "W", "B"}, "W": {"A", "W", "B"}, "B": {"A", "W", "B"},
				"V": {"V", "C"}, "C": {"V", "C"},
			},
		},
		{
			name:    "a branch hanging off one end is not contested",
			nodes:   []string{"A", "W", "B", "V", "C", "X"},
			aliases: []edge{{"A", "W"}, {"W", "B"}, {"B", "V"}, {"V", "C"}, {"X", "A"}},
			apart:   []edge{{"A", "C"}},
			want: map[string][]string{
				"A": {"A", "X"}, "X": {"A", "X"},
				"W": {"W"}, "B": {"B"}, "V": {"V"}, "C": {"C"},
			},
		},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := map[string]uuid.UUID{}
			name := map[uuid.UUID]string{}
			g := aliasGraph{types: map[uuid.UUID]string{}, joined: map[uuid.UUID][]uuid.UUID{}, apart: map[[2]uuid.UUID]bool{}}
			for j, n := range tt.nodes {
				u := uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%06d%06d", i, j))
				id[n], name[u] = u, n
				g.types[u] = "x-host"
			}
			for _, e := range tt.aliases {
				a, b := id[e[0]], id[e[1]]
				g.joined[a] = append(g.joined[a], b)
				g.joined[b] = append(g.joined[b], a)
			}
			for _, e := range tt.apart {
				a, b := id[e[0]], id[e[1]]
				g.apart[[2]uuid.UUID{a, b}], g.apart[[2]uuid.UUID{b, a}] = true, true
			}

			contested := g.contested()
			set := map[string][]string{}
			for _, n := range tt.nodes {
				got := []string{n}
				for _, m := range g.walk(id[n], contested) {
					got = append(got, name[m])
				}
				set[n] = got
				want := slices.Clone(tt.want[n])
				slices.Sort(want)
				sorted := slices.Clone(got)
				slices.Sort(sorted)
				if !slices.Equal(sorted, want) {
					t.Errorf("set(%s) = %v; want %v", n, got, tt.want[n])
				}
			}
			for _, a := range tt.nodes {
				for _, b := range tt.nodes {
					if slices.Contains(set[a], b) != slices.Contains(set[b], a) {
						t.Errorf("asymmetric: set(%s) = %v, set(%s) = %v", a, set[a], b, set[b])
					}
				}
			}
		})
	}
}
