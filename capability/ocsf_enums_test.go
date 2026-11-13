package capability

import (
	"sort"
	"testing"

	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// Every OCSF enum the normalizers hold in a table agrees with the schema
// checked in, in every release (adr/0356). The tables shared with the event
// view (hash algorithms, severities, confidences, statuses) are checked in
// internal/ocsf.
func TestEnumTablesMatchSchema(t *testing.T) {
	tests := []struct {
		name  string
		class int
		path  string
		table map[int]string
	}{
		{"userTypes", 6003, "actor.user.type_id", userTypes},
		{"accountTypes", 6003, "cloud.account.type_id", accountTypes},
		{"accountTypeIDs", 3002, "user.account.type_id", accountTypeIDs},
		{"apiActivities", 6003, "activity_id", apiActivities},
		{"inventoryActivities", 5023, "activity_id", inventoryActivities},
		{"osTypes", 1007, "device.os.type_id", osTypes},
	}
	for _, tt := range tests {
		for _, problem := range ocsfschema.CheckCaptions(tt.class, tt.path, tt.table) {
			t.Errorf("%s: %s", tt.name, problem)
		}
	}

	// confidenceLevels is keyed by the level's caption.
	levels := make([]string, 0, len(confidenceLevels))
	for caption := range confidenceLevels {
		levels = append(levels, caption)
	}
	sort.Strings(levels)
	for _, problem := range ocsfschema.CheckCaptionNames([]int{2004}, "confidence_id", levels) {
		t.Errorf("confidenceLevels: %s", problem)
	}
}

// osFamily reads the id through osTypes, so the id and the caption decide
// the same family.
func TestOSFamilyByIDAndCaption(t *testing.T) {
	for id, caption := range osTypes {
		byID := osFamily(at{obj: map[string]any{"device": map[string]any{"os": map[string]any{"type_id": id}}}, path: "device"})
		byCaption := osFamily(at{obj: map[string]any{"device": map[string]any{"os": map[string]any{"type": caption}}}, path: "device"})
		if byID == "" || byID != byCaption {
			t.Errorf("os %d (%s): family %q by id, %q by caption", id, caption, byID, byCaption)
		}
	}
	if got := osFamily(at{obj: map[string]any{"device": map[string]any{"os": map[string]any{"type_id": 201}}}, path: "device"}); got != "" {
		t.Errorf("Android decides %q; want nothing", got)
	}
}
