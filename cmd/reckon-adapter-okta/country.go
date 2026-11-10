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

var (
	countriesOnce   sync.Once
	countriesByName map[string]string // lowercased English name → alpha-2
)

// countryCode is the alpha-2 code for a country as Okta names it, or for a
// code given as one; false when the name is unknown. A deprecated code ("UK")
// resolves to the current one ("GB"), by name or as given.
func countryCode(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	if len(s) == 2 {
		if r, err := language.ParseRegion(s); err == nil && r.IsCountry() {
			return r.Canonicalize().String(), true
		}
	}
	countriesOnce.Do(func() {
		countriesByName = map[string]string{}
		names := display.English.Regions()
		for a := 'A'; a <= 'Z'; a++ {
			for b := 'A'; b <= 'Z'; b++ {
				r, err := language.ParseRegion(string([]rune{a, b}))
				if err != nil || !r.IsCountry() || r.Canonicalize() != r {
					continue // not a country, or an alias of one listed under its own code
				}
				if name := names.Name(r); name != "" {
					countriesByName[strings.ToLower(name)] = r.String()
				}
			}
		}
	})
	code, ok := countriesByName[strings.ToLower(s)]
	return code, ok
}
