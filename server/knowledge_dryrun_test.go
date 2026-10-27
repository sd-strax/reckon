package server

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/internal/sopdoc"
	"github.com/sd-strax/reckon/knowledge"
)

// TestImportDryRun covers the preview rules (ui/09 §9.5): the recommended
// fields are flagged only when the document lacks them, a defaulted source
// counts as missing, and the outcome names the procedure a re-import would
// revise.
func TestImportDryRun(t *testing.T) {
	full, err := sopdoc.Parse([]byte("---\ntitle: Host Isolation\nauthor: SOC Runbooks\ntags: [containment]\nsource:\n  system: wiki\n  url: https://wiki.example.test/HOST-ISO\n  version: \"2.1\"\n---\n# Host Isolation\n\nIsolate first.\n"), "host-isolation")
	if err != nil {
		t.Fatal(err)
	}
	rep := dryRunReport(full, full.FrontmatterTitle, false, nil)
	if len(rep.Missing) != 0 || rep.Outcome != "added" || rep.Source.Version != "2.1" || rep.Title != "Host Isolation" {
		t.Errorf("complete document: %+v", rep)
	}

	bare, err := sopdoc.Parse([]byte("# Bare runbook\n\nNo frontmatter at all.\n"), "bare-runbook")
	if err != nil {
		t.Fatal(err)
	}
	bare.SourceSystem, bare.SourceURL = "file", "file:/sops/bare-runbook.md"
	existing := &knowledge.SOP{ID: uuid.MustParse("7f2b4f6e-1d3c-4c9a-9b1e-2a4d6c8e0f12"), Title: "Bare runbook (imported earlier)"}
	rep = dryRunReport(bare, bare.FrontmatterTitle, true, existing)
	if want := []string{"title", "author", "tags", "source_url"}; !reflect.DeepEqual(rep.Missing, want) {
		t.Errorf("missing = %v; want %v", rep.Missing, want)
	}
	if rep.Title != "Bare runbook" || !rep.SourceDefaulted || rep.Outcome != "revised" || rep.ExistingID != existing.ID.String() || rep.ExistingTitle != existing.Title {
		t.Errorf("bare document against an existing lineage: %+v", rep)
	}
	if rep.Tags == nil {
		t.Error("tags must serialize as an empty list, not null")
	}
}
