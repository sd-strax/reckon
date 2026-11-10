package main

import (
	"strings"
	"sync"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// Okta's System Log names the client's country in full ("Canada", "United
// States"); OCSF's location.country is the ISO 3166-1 alpha-2 code. The code
// is found by the country's English name in the Unicode CLDR data x/text
// carries, so no table is kept here and nothing is guessed: a name CLDR does
// not know is not a code (adr/0208).

// countries is the CLDR country set, built once: every alpha-2 code that is a
// current country with an English name, and those names folded for matching.
type countries struct {
	codes  map[string]bool   // "CA"
	byName map[string]string // foldName("Canada") → "CA"
}

var (
	countriesOnce sync.Once
	countryTable  countries
)

func countrySet() countries {
	countriesOnce.Do(func() {
		countryTable = countries{codes: map[string]bool{}, byName: map[string]string{}}
		names := display.English.Regions()
		for a := 'A'; a <= 'Z'; a++ {
			for b := 'A'; b <= 'Z'; b++ {
				r, err := language.ParseRegion(string([]rune{a, b}))
				if err != nil || !r.IsCountry() || r.Canonicalize() != r {
					continue // not a country, or an alias of one listed under its own code
				}
				name := names.Name(r)
				if name == "" {
					continue // a code CLDR names no country by (withdrawn: AN, SU, YU)
				}
				countryTable.codes[r.String()] = true
				countryTable.byName[foldName(name)] = r.String()
			}
		}
	})
	return countryTable
}

// countryCode is the alpha-2 code for a country as Okta names it, or for a
// code given as one; false when the name is unknown. A code CLDR
// canonicalizes to a current country ("UK" to "GB", "DD" to "DE") resolves to
// it; a withdrawn one CLDR names no country by ("AN", "SU") is not a code.
// Names are matched after foldName on both sides, so the spellings a
// geolocation source uses ("Trinidad and Tobago", "Hong Kong", "Saint Kitts
// and Nevis", "Myanmar", "The Bahamas") meet CLDR's ("Trinidad & Tobago",
// "Hong Kong SAR China", "St. Kitts & Nevis", "Myanmar (Burma)", "Bahamas").
// A name that is not the English name in the shipped CLDR tables under that
// folding is unknown here and is carried as the location's desc by the
// caller: an older or ISO-official form ("Czech Republic", "Russian
// Federation", "Viet Nam"), and equally a newer one the shipped tables
// predate ("Eswatini", "North Macedonia", "Cabo Verde", "Türkiye" against
// x/text v0.33.0's "Swaziland", "Macedonia", "Cape Verde", "Turkey"); the
// latter close when x/text moves.
func countryCode(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	set := countrySet()
	if len(s) == 2 {
		if r, err := language.ParseRegion(s); err == nil {
			code := r.Canonicalize().String()
			return code, set.codes[code]
		}
	}
	code, ok := set.byName[foldName(s)]
	return code, ok
}

// foldName makes a country name comparable across the spellings geolocation
// sources and CLDR use: case; "&" for "and"; a leading "St." for "Saint";
// the article "the" ("The Bahamas", "Saint Vincent and the Grenadines"); the
// curly apostrophe; a trailing parenthetical ("Myanmar (Burma)"); the "SAR
// China" suffix on Hong Kong and Macau, and Macao's two spellings; runs of
// spaces. Diacritics are not folded.
func foldName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndex(s, " ("); i > 0 && strings.HasSuffix(s, ")") {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, " sar china")
	if s == "macao" {
		s = "macau"
	}
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.ReplaceAll(s, "’", "'")
	for _, prefix := range []string{"st. ", "st "} {
		if strings.HasPrefix(s, prefix) {
			s = "saint " + s[len(prefix):]
			break
		}
	}
	words := strings.Fields(s)
	kept := words[:0]
	for _, w := range words {
		if w != "the" {
			kept = append(kept, w)
		}
	}
	return strings.Join(kept, " ")
}
