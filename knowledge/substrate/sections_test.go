package substrate

import (
	"reflect"
	"strings"
	"testing"
)

// procedureDoc is shaped like a curated procedure: a title heading, a scope
// paragraph under it, then three second-level sections of mixed list and
// prose. The substrate knows no host documents; this stands in for them.
const procedureDoc = `# Lateral Movement — Containment

Scope: an interactive remote logon from an internal host that is not a known
jump box, followed by process creation on the destination.

## Triage
1. Confirm the source→destination session in the authentication telemetry.
2. Pull process activity on the destination in the 15 minutes after the logon.

## Containment
- **Isolate the destination host** if hands-on-keyboard activity is confirmed.
- Disable or force-reset the account if it is being used to pivot.

## Do not
- Do not reimage the destination before evidence collection.
`

func TestSplitSections_HeadingDelimited(t *testing.T) {
	got := SplitSections(procedureDoc, 0)
	wantPaths := [][]string{
		{"Lateral Movement — Containment"},
		{"Lateral Movement — Containment", "Triage"},
		{"Lateral Movement — Containment", "Containment"},
		{"Lateral Movement — Containment", "Do not"},
	}
	if len(got) != len(wantPaths) {
		t.Fatalf("got %d sections, want %d:\n%s", len(got), len(wantPaths), dumpSections(got))
	}
	for i, s := range got {
		if s.Idx != i+1 {
			t.Errorf("section %d: idx %d", i, s.Idx)
		}
		if !reflect.DeepEqual(s.HeadingPath, wantPaths[i]) {
			t.Errorf("section %d: path %q, want %q", i, s.HeadingPath, wantPaths[i])
		}
		if s.Part != 0 || s.Parts != 0 {
			t.Errorf("section %d: unexpected parts %d/%d", i, s.Part, s.Parts)
		}
	}
	// The heading line opens its section; the scope paragraph sits under the
	// title, and the containment steps are in the containment section only.
	if !strings.HasPrefix(got[0].Text, "# Lateral Movement") || !strings.Contains(got[0].Text, "Scope:") {
		t.Errorf("title section text:\n%s", got[0].Text)
	}
	if !strings.HasPrefix(got[2].Text, "## Containment") || !strings.Contains(got[2].Text, "Isolate the destination host") {
		t.Errorf("containment section text:\n%s", got[2].Text)
	}
	if strings.Contains(got[1].Text, "Isolate") || strings.Contains(got[3].Text, "Isolate") {
		t.Error("containment text leaked into a neighbouring section")
	}
	if got[2].Location() != "Lateral Movement — Containment > Containment" {
		t.Errorf("location %q", got[2].Location())
	}
	assertOffsets(t, procedureDoc, got)
}

func TestSplitSections_NoHeadings(t *testing.T) {
	body := "Isolate first.\n\nThen collect volatile evidence.\n"
	got := SplitSections(body, 0)
	if len(got) != 1 {
		t.Fatalf("got %d sections, want 1", len(got))
	}
	s := got[0]
	if s.Idx != 1 || len(s.HeadingPath) != 0 || s.Text != strings.TrimRight(body, "\n") {
		t.Errorf("section: %+v", s)
	}
	if s.Location() != "(before the first heading)" {
		t.Errorf("location %q", s.Location())
	}
	assertOffsets(t, body, got)
}

func TestSplitSections_Blank(t *testing.T) {
	for _, body := range []string{"", "\n\n", "   \n\t\n"} {
		if got := SplitSections(body, 0); got != nil {
			t.Errorf("%q: got %d sections, want none", body, len(got))
		}
	}
}

func TestSplitSections_LeadingTextAndNesting(t *testing.T) {
	body := "Preamble before any heading.\n\n# A\n\n## B\ntext b\n\n### C\ntext c\n\n## D\ntext d\n"
	got := SplitSections(body, 0)
	wantPaths := [][]string{nil, {"A"}, {"A", "B"}, {"A", "B", "C"}, {"A", "D"}}
	if len(got) != len(wantPaths) {
		t.Fatalf("got %d sections, want %d:\n%s", len(got), len(wantPaths), dumpSections(got))
	}
	for i, s := range got {
		if len(s.HeadingPath) != len(wantPaths[i]) || (len(wantPaths[i]) > 0 && !reflect.DeepEqual(s.HeadingPath, wantPaths[i])) {
			t.Errorf("section %d: path %q, want %q", i, s.HeadingPath, wantPaths[i])
		}
	}
	if got[0].Text != "Preamble before any heading." {
		t.Errorf("preamble text %q", got[0].Text)
	}
	// A heading with nothing under it is still a section: the outline shows it.
	if got[1].Text != "# A" {
		t.Errorf("heading-only section text %q", got[1].Text)
	}
	assertOffsets(t, body, got)
}

func TestSplitSections_BlankLeadIsDropped(t *testing.T) {
	body := "\n\n# Only\nbody\n"
	got := SplitSections(body, 0)
	if len(got) != 1 || got[0].Text != "# Only\nbody" {
		t.Fatalf("got:\n%s", dumpSections(got))
	}
	assertOffsets(t, body, got)
}

func TestSplitSections_WhatIsNotAHeading(t *testing.T) {
	body := strings.Join([]string{
		"# Real",
		"#NotAHeading no space after the hashes",
		"####### seven hashes is text",
		" # indented is text",
		"```",
		"# inside a fence",
		"```",
		"~~~",
		"## inside a tilde fence",
		"~~~",
		"## Closing hashes ##",
		"tail",
	}, "\n")
	got := SplitSections(body, 0)
	wantPaths := [][]string{{"Real"}, {"Real", "Closing hashes"}}
	if len(got) != len(wantPaths) {
		t.Fatalf("got %d sections, want %d:\n%s", len(got), len(wantPaths), dumpSections(got))
	}
	for i, s := range got {
		if !reflect.DeepEqual(s.HeadingPath, wantPaths[i]) {
			t.Errorf("section %d: path %q, want %q", i, s.HeadingPath, wantPaths[i])
		}
	}
	if !strings.Contains(got[0].Text, "# inside a fence") || !strings.Contains(got[0].Text, "## inside a tilde fence") {
		t.Errorf("fenced lines left the first section:\n%s", got[0].Text)
	}
	assertOffsets(t, body, got)
}

func TestSplitSections_CRLF(t *testing.T) {
	body := "# Title\r\n\r\nline one\r\n\r\n## Next\r\nline two\r\n"
	got := SplitSections(body, 0)
	if len(got) != 2 {
		t.Fatalf("got %d sections, want 2:\n%s", len(got), dumpSections(got))
	}
	if got[1].HeadingPath[1] != "Next" {
		t.Errorf("heading with CR kept it: %q", got[1].HeadingPath)
	}
	assertOffsets(t, body, got)
}

func TestSplitSections_BudgetCutsOnParagraphs(t *testing.T) {
	para := strings.Repeat
	// Three paragraphs of 40 runes each under one heading. With the heading
	// line and blank separators, the first two paragraphs span 87 runes and
	// all three 129, so a budget of 100 packs two and carries the third.
	body := "## S\n" + para("a", 40) + "\n\n" + para("b", 40) + "\n\n" + para("c", 40) + "\n"
	got := SplitSections(body, 100)
	if len(got) != 2 {
		t.Fatalf("got %d parts, want 2:\n%s", len(got), dumpSections(got))
	}
	for i, s := range got {
		if s.Idx != i+1 || s.Part != i+1 || s.Parts != 2 {
			t.Errorf("part %d: idx %d part %d/%d", i, s.Idx, s.Part, s.Parts)
		}
		if !reflect.DeepEqual(s.HeadingPath, []string{"S"}) {
			t.Errorf("part %d: path %q", i, s.HeadingPath)
		}
		if n := len([]rune(s.Text)); n > 100 {
			t.Errorf("part %d: %d runes over budget", i, n)
		}
	}
	if got[0].Text != "## S\n"+para("a", 40)+"\n\n"+para("b", 40) || got[1].Text != para("c", 40) {
		t.Errorf("parts:\n%s", dumpSections(got))
	}
	if got[1].Location() != "S (part 2/2)" {
		t.Errorf("location %q", got[1].Location())
	}
	// Nothing but blank separators is lost between the parts.
	joined := got[0].Text + "\n\n" + got[1].Text
	if joined != strings.TrimRight(body, "\n") {
		t.Errorf("parts do not reassemble the section:\n%q\n%q", joined, body)
	}
	assertOffsets(t, body, got)
}

func TestSplitSections_BudgetCutsALongLine(t *testing.T) {
	body := "# H\n" + strings.Repeat("x", 250) + "\n"
	got := SplitSections(body, 100)
	// The heading line is one piece; the 250-rune line is cut at 100, 100, 50,
	// and the heading packs with nothing (the first slice alone fills the
	// budget), so four parts.
	if len(got) != 4 {
		t.Fatalf("got %d parts, want 4:\n%s", len(got), dumpSections(got))
	}
	for i, s := range got {
		if n := len([]rune(s.Text)); n > 100 {
			t.Errorf("part %d: %d runes over budget", i, n)
		}
		if s.Parts != 4 || s.Part != i+1 {
			t.Errorf("part %d: %d/%d", i, s.Part, s.Parts)
		}
	}
	assertOffsets(t, body, got)
}

func TestSplitSections_BudgetLeavesShortSectionsWhole(t *testing.T) {
	got := SplitSections(procedureDoc, 10_000)
	if len(got) != 4 {
		t.Fatalf("got %d sections, want 4", len(got))
	}
	for _, s := range got {
		if s.Parts != 0 {
			t.Errorf("section %d was cut under a budget it fits: %d/%d", s.Idx, s.Part, s.Parts)
		}
	}
}

func TestSplitSections_Deterministic(t *testing.T) {
	a := SplitSections(procedureDoc, 120)
	b := SplitSections(procedureDoc, 120)
	if !reflect.DeepEqual(a, b) {
		t.Error("two runs over one body differ")
	}
	if SplitterVersion != 1 {
		t.Errorf("SplitterVersion %d: bump means the rule changed; update the stored rows", SplitterVersion)
	}
}

// assertOffsets checks the location contract: every section's Text is the
// body's runes in [Start, End), and sections are in body order.
func assertOffsets(t *testing.T, body string, secs []Section) {
	t.Helper()
	runes := []rune(body)
	prevEnd := 0
	for _, s := range secs {
		if s.Start < prevEnd || s.End < s.Start || s.End > len(runes) {
			t.Errorf("section %d: offsets [%d,%d) out of order or range", s.Idx, s.Start, s.End)
			continue
		}
		if got := string(runes[s.Start:s.End]); got != s.Text {
			t.Errorf("section %d: text does not match body[%d:%d]:\n got %q\nbody %q", s.Idx, s.Start, s.End, s.Text, got)
		}
		prevEnd = s.End
	}
}

func dumpSections(secs []Section) string {
	var b strings.Builder
	for _, s := range secs {
		b.WriteString(s.Location())
		b.WriteString(": ")
		b.WriteString(strings.ReplaceAll(s.Text, "\n", "⏎"))
		b.WriteString("\n")
	}
	return b.String()
}
