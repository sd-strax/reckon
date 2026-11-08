package knowledge

import (
	"context"
	"testing"
)

// TestListBySource: the procedures whose source url starts with a prefix, the
// join behind "procedures learned from this case"; a retired one is out,
// another investigation's is out, and an empty prefix is refused rather than
// answering the whole corpus.
func TestListBySource(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	s := reset(t)
	ctx := context.Background()
	mk := func(title, url string) SOP {
		return SOP{TenantID: testTenant, Title: title, Body: "Step one.", Tags: []string{"x"}, Source: &SOPSource{System: "reckon", URL: url}}
	}
	learned, err := s.Create(ctx, mk("Learned here", InvestigationSourcePrefix+"inv-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, mk("Learned elsewhere", InvestigationSourcePrefix+"inv-2")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, mk("From a file", "file:///sops/a.md")); err != nil {
		t.Fatal(err)
	}
	gone, err := s.Create(ctx, mk("Learned then retired", InvestigationSourcePrefix+"inv-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Retire(ctx, testTenant, gone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, SOP{TenantID: testTenant, Title: "No source", Body: "Step one.", Tags: []string{"x"}}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListBySource(ctx, testTenant, InvestigationSourcePrefix+"inv-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != learned {
		t.Fatalf("got %+v; want only the learned SOP (another investigation's, a file's, a retired one and an unsourced one are out)", got)
	}
	_ = gone
	if got, err := s.ListBySource(ctx, testTenant, InvestigationSourcePrefix+"inv-1/"); err != nil || len(got) != 0 {
		t.Errorf("a prefix no url carries: %v, %v; want none", got, err)
	}
	if _, err := s.ListBySource(ctx, testTenant, ""); err == nil {
		t.Error("empty prefix accepted")
	}
}
