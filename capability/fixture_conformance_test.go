package capability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// nonEventFixtures are the fixture files that hold no OCSF event: a lookup
// table the fixture adapter reads beside the events.
var nonEventFixtures = map[string]bool{"asset-criticality.json": true}

// TestFixturesAreOCSF is check 3 of adr/0356 for the fixtures. It walks every
// event under fixtures/ field by field. Each field must exist for the event's
// class at the OCSF version the fixture declares in metadata.version, and
// hold the shape the schema gives it (a list, an object or a scalar) and, for
// a scalar, the schema's type: a time is a whole number of epoch
// milliseconds, a string type a string, and so on.
// unmapped.* passes, as OCSF puts a source's own fields there.
//
// Write fixtures (*.action.json) hold no event and are skipped. So is the
// fixture_meta block, which is the replay's own and not OCSF. A fixture that
// declares no version fails.
func TestFixturesAreOCSF(t *testing.T) {
	files, err := filepath.Glob("../fixtures/*/*.json")
	if err != nil {
		t.Fatal(err)
	}
	events := 0
	for _, f := range files {
		base := filepath.Base(f)
		name := filepath.Join(filepath.Base(filepath.Dir(f)), base)
		if strings.HasSuffix(base, ".action.json") || nonEventFixtures[base] {
			continue
		}
		raw, err := os.ReadFile(f) // #nosec G304 -- a fixture path from Glob
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			OCSF map[string]any `json:"ocsf"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if doc.OCSF == nil {
			t.Errorf("%s: no ocsf event; a file that holds none is listed in nonEventFixtures", name)
			continue
		}
		events++
		for _, p := range ocsfschema.ValidateEvent(doc.OCSF) {
			t.Errorf("%s: class %d, OCSF %s: %s", name, toInt(doc.OCSF["class_uid"]), pathStr(doc.OCSF, "metadata.version"), p)
		}
	}
	// Three scenarios ship. A walk that finds few events is looking in the
	// wrong place.
	if events < 60 {
		t.Errorf("walked %d fixture events; want every scenario's", events)
	}
}
