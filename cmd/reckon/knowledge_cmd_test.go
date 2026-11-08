package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/internal/sopdoc"
	"github.com/sd-strax/reckon/knowledge"
)

func writeDoc(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseSOPDocument_Frontmatter(t *testing.T) {
	path := writeDoc(t, "ransomware.md", `---
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
`)
	doc, err := parseSOPDocument(path)
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

func TestParseSOPDocument_PlainMarkdownFallbacks(t *testing.T) {
	path := writeDoc(t, "phishing-triage.md", "# Phishing Triage\n\nQuarantine the message.\n")
	doc, err := parseSOPDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Phishing Triage" {
		t.Errorf("title should come from the heading: %q", doc.Title)
	}
	// The source URL — the re-import lineage key — defaults to the file path.
	if doc.SourceSystem != "file" || !strings.HasPrefix(doc.SourceURL, "file:") || !strings.HasSuffix(doc.SourceURL, "phishing-triage.md") {
		t.Errorf("source fallback wrong: %s %s", doc.SourceSystem, doc.SourceURL)
	}
}

func TestParseSOPDocument_NoHeadingUsesStem(t *testing.T) {
	path := writeDoc(t, "bec-response.md", "Reset the mailbox and review inbox rules.\n")
	doc, err := parseSOPDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "bec response" {
		t.Errorf("title should come from the filename stem: %q", doc.Title)
	}
}

func TestParseSOPDocument_Errors(t *testing.T) {
	if _, err := parseSOPDocument(writeDoc(t, "empty.md", "")); err == nil {
		t.Error("empty body accepted")
	}
	if _, err := parseSOPDocument(writeDoc(t, "bad.md", "---\ntitle: x\n")); err == nil {
		t.Error("unterminated frontmatter accepted")
	}
	if _, err := parseSOPDocument(writeDoc(t, "badyaml.md", "---\n: : :\n---\nbody\n")); err == nil {
		t.Error("malformed YAML accepted")
	}
}

func TestCollectImportFiles(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.md", "b.markdown", "c.txt", "skip.pdf", "sub/d.md"} {
		path := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := collectImportFiles([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Errorf("collected %d files, want 4 (pdf skipped): %v", len(files), files)
	}
}

// manageServer is a backend standing in for /api/sops: one live SOP and one
// retired, a DELETE that retires, and a token check.
func manageServer(t *testing.T) (*httptest.Server, *knowledge.SOP, *knowledge.SOP) {
	t.Helper()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	live := &knowledge.SOP{
		ID: uuid.New(), Title: "Ransomware Containment Runbook", Body: "# Scope\n\nIsolate first.\n\n# Triage\n\nPreserve volatile evidence.",
		Tags: []string{"ransomware", "T1486"}, Status: knowledge.StatusPublished, Recommendation: "isolate", AuthorID: "Jane Okoro",
		Source:     &knowledge.SOPSource{System: "confluence", URL: "https://wiki.example.com/RUNBOOK-42", Version: "3.2"},
		ImportedBy: "sam", ImportedAt: &at, Revision: 3,
	}
	retired := &knowledge.SOP{
		ID: uuid.New(), Title: "Old BEC Response", Body: "Reset the mailbox.", Status: knowledge.StatusRetired,
		Source: &knowledge.SOPSource{URL: "file:/sops/bec.md"}, Revision: 1,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sops", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		sops := []knowledge.SOP{*live}
		if r.URL.Query().Get("include_retired") == "true" {
			sops = append(sops, *retired)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sops": sops})
	})
	mux.HandleFunc("GET /api/sops/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("id") {
		case live.ID.String():
			_ = json.NewEncoder(w).Encode(live)
		case retired.ID.String():
			_ = json.NewEncoder(w).Encode(retired)
		default:
			http.Error(w, `{"error":"sop not found"}`, http.StatusNotFound)
		}
	})
	mux.HandleFunc("DELETE /api/sops/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != live.ID.String() || live.Status == knowledge.StatusRetired {
			http.Error(w, `{"error":"sop not found or already retired"}`, http.StatusNotFound)
			return
		}
		live.Status = knowledge.StatusRetired
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, live, retired
}

func testClient(srv *httptest.Server) *knowledgeClient {
	return &knowledgeClient{base: srv.URL, token: func(context.Context) (string, error) { return "tok", nil }}
}

// TestKnowledgeList_MarksRetired: the list is one row per SOP with the lineage
// key (source url) and the revision; a retired SOP appears only with
// --include-retired and says so in its row.
func TestKnowledgeList_MarksRetired(t *testing.T) {
	srv, live, retired := manageServer(t)
	c := testClient(srv)
	ctx := context.Background()

	raw, err := c.call(ctx, http.MethodGet, "/api/sops", http.StatusOK)
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ SOPs []knowledge.SOP }
	if err := json.Unmarshal(raw, &out); err != nil || len(out.SOPs) != 1 {
		t.Fatalf("live list: %v %s", err, raw)
	}
	var buf bytes.Buffer
	if err := renderSOPList(&buf, out.SOPs); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, live.ID.String()) || !strings.Contains(got, "https://wiki.example.com/RUNBOOK-42") ||
		!strings.Contains(got, "sam 2026-01-02T03:04:05Z") || strings.Contains(got, "retired") {
		t.Errorf("live list rendered:\n%s", got)
	}

	raw, err = c.call(ctx, http.MethodGet, "/api/sops?include_retired=true", http.StatusOK)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.SOPs) != 2 {
		t.Fatalf("full list: %v %s", err, raw)
	}
	buf.Reset()
	if err := renderSOPList(&buf, out.SOPs); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "ID") {
		t.Fatalf("want a header and two rows:\n%s", buf.String())
	}
	if !strings.Contains(lines[2], retired.ID.String()) || !strings.Contains(lines[2], "retired") || !strings.Contains(lines[2], "file:/sops/bec.md") {
		t.Errorf("retired row: %s", lines[2])
	}
	if strings.Contains(lines[1], "retired") {
		t.Errorf("live row marked retired: %s", lines[1])
	}
}

// TestKnowledgeShow_RoundTrips: the document show prints parses back to the
// same title, body, tags, recommendation, author and source, so importing it
// again is unchanged; the provenance line carries what the document leaves out.
func TestKnowledgeShow_RoundTrips(t *testing.T) {
	srv, live, _ := manageServer(t)
	raw, err := testClient(srv).call(context.Background(), http.MethodGet, "/api/sops/"+live.ID.String(), http.StatusOK)
	if err != nil {
		t.Fatal(err)
	}
	var sop knowledge.SOP
	if err := json.Unmarshal(raw, &sop); err != nil {
		t.Fatal(err)
	}
	doc, err := sopdoc.Render(sopDocument(sop))
	if err != nil {
		t.Fatal(err)
	}
	back, err := sopdoc.Parse(doc, "fallback")
	if err != nil {
		t.Fatalf("parse what show printed: %v\n%s", err, doc)
	}
	if back.Title != live.Title || back.Body != live.Body || back.Recommendation != "isolate" || back.Author != "Jane Okoro" ||
		strings.Join(back.Tags, ",") != "ransomware,T1486" ||
		back.SourceSystem != "confluence" || back.SourceURL != live.Source.URL || back.SourceVersion != "3.2" {
		t.Errorf("round trip changed the document: %+v\n%s", back, doc)
	}
	if strings.Contains(string(doc), "sam") || strings.Contains(string(doc), "imported") {
		t.Errorf("the document carries ingest provenance:\n%s", doc)
	}
	if line := provenanceLine(sop); !strings.Contains(line, live.ID.String()) || !strings.Contains(line, "revision 3") || !strings.Contains(line, "imported by sam at 2026-01-02T03:04:05Z") {
		t.Errorf("provenance line: %s", line)
	}
}

// TestKnowledgeRetire_ReadsBack: DELETE then GET; the read-back names what
// was retired; a second retire is the server's 404, reported with its body.
func TestKnowledgeRetire_ReadsBack(t *testing.T) {
	srv, live, _ := manageServer(t)
	c := testClient(srv)
	ctx := context.Background()
	if _, err := c.call(ctx, http.MethodDelete, "/api/sops/"+live.ID.String(), http.StatusNoContent); err != nil {
		t.Fatal(err)
	}
	raw, err := c.call(ctx, http.MethodGet, "/api/sops/"+live.ID.String(), http.StatusOK)
	if err != nil {
		t.Fatal(err)
	}
	var sop knowledge.SOP
	if err := json.Unmarshal(raw, &sop); err != nil || sop.Status != knowledge.StatusRetired {
		t.Errorf("read back: %v %+v", err, sop)
	}
	_, err = c.call(ctx, http.MethodDelete, "/api/sops/"+live.ID.String(), http.StatusNoContent)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") || !strings.Contains(err.Error(), "already retired") {
		t.Errorf("second retire: %v", err)
	}
}

// TestPrintJSON: --json output is the server's JSON, re-indented, and a
// non-JSON body is an error rather than printed as if it were.
func TestPrintJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := printJSON(&buf, []byte(`{"sops":[{"id":"x","tags":["a","b"]}]}`)); err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil || len(back["sops"].([]any)) != 1 {
		t.Errorf("not valid JSON: %v\n%s", err, buf.String())
	}
	if err := printJSON(&buf, []byte("<html>")); err == nil {
		t.Error("non-JSON printed as JSON")
	}
}

// TestSOPIDArg: the one positional argument must be a UUID, and the flags may
// come before or after it, as the usage line says.
func TestSOPIDArg(t *testing.T) {
	for _, args := range [][]string{{}, {"a", "b"}, {"not-a-uuid"}, {"--json"}} {
		fl := flag.NewFlagSet("t", flag.ContinueOnError)
		fl.Bool("json", false, "")
		if _, err := sopIDArg(fl, args, "show"); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	id := uuid.New()
	for _, args := range [][]string{{id.String()}, {"--json", id.String()}, {id.String(), "--json"}, {"-user", "u", id.String(), "--json"}} {
		fl := flag.NewFlagSet("t", flag.ContinueOnError)
		asJSON := fl.Bool("json", false, "")
		fl.String("user", "", "")
		got, err := sopIDArg(fl, args, "show")
		if err != nil || got != id {
			t.Errorf("%v: %v %s", args, err, got)
		}
		if wantJSON := len(args) > 1 && args[len(args)-1] == "--json" || args[0] == "--json"; *asJSON != wantJSON {
			t.Errorf("%v: --json read as %v", args, *asJSON)
		}
	}
}
