package sopdoc

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse_Frontmatter(t *testing.T) {
	doc, err := Parse([]byte(`---
title: Ransomware Containment Runbook
author: Jane Okoro
tags: [ransomware, T1486]
recommendation: isolate
source:
  system: confluence
  url: https://wiki.example.com/RUNBOOK-42
  version: "3.2"
---
Isolate first, preserve volatile evidence.
`), "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Ransomware Containment Runbook" || doc.Author != "Jane Okoro" {
		t.Errorf("attribution wrong: %+v", doc)
	}
	if len(doc.Tags) != 2 || doc.Recommendation != "isolate" {
		t.Errorf("tags/recommendation wrong: %+v", doc)
	}
	if doc.SourceSystem != "confluence" || doc.SourceURL != "https://wiki.example.com/RUNBOOK-42" || doc.SourceVersion != "3.2" {
		t.Errorf("source wrong: %+v", doc)
	}
	if !strings.Contains(doc.Body, "Isolate first") || strings.Contains(doc.Body, "---") {
		t.Errorf("body wrong: %q", doc.Body)
	}
}

func TestParse_TitleFallbacks(t *testing.T) {
	// Heading wins when frontmatter has no title.
	doc, err := Parse([]byte("# Phishing Triage\n\nQuarantine the message."), "stem-name")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Phishing Triage" {
		t.Errorf("title should come from the heading: %q", doc.Title)
	}
	// No heading, no frontmatter → the fallback (filename stem).
	doc, err = Parse([]byte("Reset the mailbox and review inbox rules."), "bec response")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "bec response" {
		t.Errorf("title should fall back to the stem: %q", doc.Title)
	}
	// Source is NOT defaulted here — the caller owns that.
	if doc.SourceURL != "" {
		t.Errorf("Parse must not default source.url: %q", doc.SourceURL)
	}
}

func TestParse_Errors(t *testing.T) {
	if _, err := Parse([]byte(""), "x"); err == nil {
		t.Error("empty body accepted")
	}
	if _, err := Parse([]byte("---\ntitle: x\n"), "x"); err == nil {
		t.Error("unterminated frontmatter accepted")
	}
	if _, err := Parse([]byte("---\n: : :\n---\nbody\n"), "x"); err == nil {
		t.Error("malformed YAML accepted")
	}
}

func TestTitleStem(t *testing.T) {
	if got := TitleStem("/a/b/ransomware-containment.md"); got != "ransomware containment" {
		t.Errorf("TitleStem = %q", got)
	}
}

// TestRender_RoundTrip: what Render writes, Parse reads back as the same
// document, so `show > f.md` then `import f.md` is an unchanged re-import.
func TestRender_RoundTrip(t *testing.T) {
	for name, doc := range map[string]Doc{
		"full": {
			Title: "Ransomware Containment Runbook", Author: "Jane Okoro",
			Tags: []string{"ransomware", "T1486"}, Recommendation: "isolate",
			SourceSystem: "confluence", SourceURL: "https://wiki.example.com/RUNBOOK-42", SourceVersion: "3.2",
			Body: "# Scope\n\nIsolate first: preserve volatile evidence.\n\n```\n---\nnot frontmatter\n---\n```\n\n# Triage\n\n- step one\n- step two",
		},
		"sparse": {Title: "x: y", SourceURL: "file:/tmp/x.md", Body: "Reset the mailbox."},
	} {
		raw, err := Render(doc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := Parse(raw, "fallback")
		if err != nil {
			t.Fatalf("%s: parse what Render wrote: %v\n%s", name, err, raw)
		}
		if !reflect.DeepEqual(got, doc) {
			t.Errorf("%s: round trip changed the document\n got %+v\nwant %+v\n%s", name, got, doc, raw)
		}
	}
}

// TestCanonicalBody: line endings, leading blank lines and trailing
// whitespace are not content; the first content line's indentation is.
func TestCanonicalBody(t *testing.T) {
	for in, want := range map[string]string{
		"    Get-Process\r\n":        "    Get-Process",
		"\n  \n\tcode\n\nmore  \n\n": "\tcode\n\nmore",
		"a\r\nb\r\n":                 "a\nb",
		"plain":                      "plain",
		"   \n\t\n":                  "",
	} {
		if got := CanonicalBody(in); got != want {
			t.Errorf("CanonicalBody(%q) = %q, want %q", in, got, want)
		}
	}
	// Parse keeps an opening indented code block, with or without frontmatter.
	for _, raw := range []string{"---\ntitle: t\n---\n\n    Get-Process\r\n", "\n    Get-Process\n"} {
		doc, err := Parse([]byte(raw), "t")
		if err != nil || doc.Body != "    Get-Process" {
			t.Errorf("Parse(%q): body %q err %v", raw, doc.Body, err)
		}
	}
	// And Render of a stored body that is already canonical round-trips it.
	doc := Doc{Title: "t", SourceURL: "file:x", Body: "    Get-Process\n\nthen"}
	raw, _ := Render(doc)
	if back, err := Parse(raw, "t"); err != nil || back.Body != doc.Body {
		t.Errorf("round trip of an indented opening: %q err %v\n%s", back.Body, err, raw)
	}
}
