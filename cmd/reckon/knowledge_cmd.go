package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sd-strax/reckon/agent"
	"github.com/sd-strax/reckon/config"
	"github.com/sd-strax/reckon/internal/branding"
	"github.com/sd-strax/reckon/internal/sopdoc"
)

// runKnowledge dispatches the `knowledge` subcommands.
func runKnowledge(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: %s knowledge import <files or dirs> | list | show <id> | retire <id> | reindex", branding.CLI)
	}
	switch args[0] {
	case "import":
		return runKnowledgeImport(args[1:])
	case "list":
		return runKnowledgeList(args[1:])
	case "show":
		return runKnowledgeShow(args[1:])
	case "retire":
		return runKnowledgeRetire(args[1:])
	case "reindex":
		return runKnowledgeReindex(args[1:])
	default:
		return fmt.Errorf("unknown knowledge subcommand %q (want: import, list, show, retire, reindex)", args[0])
	}
}

// loginFlags declares the dev-auth login flags every CLI client command takes,
// read from the environment when set; an empty password is the dev-auth
// default (backendLogin).
func loginFlags(fl *flag.FlagSet) (user, pass *string) {
	user = fl.String("user", envOr("RECKON_USER", branding.CLI+"-admin"), "backend login user (or RECKON_USER)")
	pass = fl.String("password", os.Getenv("RECKON_PASSWORD"), "backend login password (or RECKON_PASSWORD)")
	return user, pass
}

// knowledgeSession loads the config and signs in: the backend to call and the
// credential to call it with.
func knowledgeSession(ctx context.Context, user, pass string) (*agent.Credential, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", fmt.Errorf("load config: %w", err)
	}
	cred, err := backendLogin(ctx, cfg, branding.CLI, user, pass)
	if err != nil {
		return nil, "", err
	}
	return cred, localBackendURL(cfg), nil
}

// runKnowledgeImport seeds the SOP corpus from existing institutional
// knowledge (design/06 §2.4): markdown files with optional YAML frontmatter
// carrying attribution. Conversion from richer formats stays client-side by
// design — export Confluence/Word/PDF to markdown first (pandoc does all
// three); the backend only ever receives clean text + structured attribution.
//
// Re-import is an upsert: the source URL (frontmatter source.url, defaulting
// to file:<path>) keys the lineage, so importing an updated document revises
// the SOP in place — the prior version stays hash-addressable for audit.
func runKnowledgeImport(args []string) error {
	fl := flag.NewFlagSet("knowledge import", flag.ContinueOnError)
	user, pass := loginFlags(fl)
	if err := fl.Parse(args); err != nil {
		return err
	}
	paths := fl.Args()
	if len(paths) == 0 {
		return fmt.Errorf("usage: %s knowledge import [-user u] [-password p] <file-or-dir>...\n  files: .md/.markdown/.txt with optional YAML frontmatter (title, author, tags, recommendation, source{system,url,version})\n  richer formats: convert first, e.g. `pandoc runbook.docx -t gfm`", branding.CLI)
	}

	files, err := collectImportFiles(paths)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no importable files (.md/.markdown/.txt) under %s", strings.Join(paths, ", "))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cred, backendURL, err := knowledgeSession(ctx, *user, *pass)
	if err != nil {
		return err
	}

	failed := 0
	for _, path := range files {
		doc, err := parseSOPDocument(path)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "  FAIL  %s — %v\n", path, err)
			continue
		}
		outcome, err := postSOPImport(ctx, cred, backendURL, doc)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "  FAIL  %s — %v\n", path, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "  %-9s %s — %q\n", outcome, path, doc.Title)
	}
	fmt.Fprintf(os.Stderr, "%s: imported %d/%d file(s)\n", branding.CLI, len(files)-failed, len(files))
	if failed > 0 {
		return fmt.Errorf("%d file(s) failed", failed)
	}
	return nil
}

// runKnowledgeReindex asks the running backend to bring the knowledge corpus
// up to date with its section rule and embedding model — the reconciliation
// the backend also runs at startup — and reports how many entries it touched.
func runKnowledgeReindex(args []string) error {
	fl := flag.NewFlagSet("knowledge reindex", flag.ContinueOnError)
	user, pass := loginFlags(fl)
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 0 {
		return fmt.Errorf("usage: %s knowledge reindex [-user u] [-password p]", branding.CLI)
	}
	ctx, cancel := context.WithTimeout(context.Background(), reindexWait)
	defer cancel()
	cred, backendURL, err := knowledgeSession(ctx, *user, *pass)
	if err != nil {
		return err
	}
	// The run outlives one request (one embedder call per stale entry): POST
	// starts it and answers 202, GET reports it until done or failed.
	job, err := reindexCall(ctx, cred, http.MethodPost, backendURL, http.StatusAccepted)
	if err != nil {
		return err
	}
	if job.Run == "" {
		return fmt.Errorf("reindex: the backend named no run (state %s)", job.State)
	}
	run := job.Run
	fmt.Fprintf(os.Stderr, "%s: reindex %s (run %s)\n", branding.CLI, job.State, run)
	// Only this run's outcome counts: a restart answers idle with no run, and
	// a later start replaces the state with another run's.
	for job.Run == run && job.State == "running" {
		select {
		case <-ctx.Done():
			return fmt.Errorf("reindex still running after %s; `%s knowledge reindex` reports it when it finishes", reindexWait, branding.CLI)
		case <-time.After(reindexPoll):
		}
		if job, err = reindexCall(ctx, cred, http.MethodGet, backendURL, http.StatusOK); err != nil {
			return err
		}
	}
	switch {
	case job.Run != run:
		return fmt.Errorf("reindex run %s was lost before it finished (the backend restarted or another run replaced it); run it again", run)
	case job.State == "failed":
		return fmt.Errorf("reindex failed: %s", job.Error)
	case job.State != "done":
		return fmt.Errorf("reindex run %s ended in state %q", run, job.State)
	}
	fmt.Fprintf(os.Stderr, "%s: reindexed %d entr%s\n", branding.CLI, job.Entries, plural(job.Entries, "y", "ies"))
	return nil
}

const (
	reindexWait = 30 * time.Minute
	reindexPoll = 2 * time.Second
)

type reindexJob struct {
	Run     string `json:"run"`
	State   string `json:"state"`
	Entries int    `json:"entries"`
	Error   string `json:"error"`
}

// reindexCall is one POST (start) or GET (state) against /api/knowledge/reindex.
func reindexCall(ctx context.Context, cred *agent.Credential, method, backendURL string, want int) (reindexJob, error) {
	token, err := cred.Token(ctx)
	if err != nil {
		return reindexJob{}, err
	}
	req, err := http.NewRequestWithContext(ctx, method, backendURL+"/api/knowledge/reindex", http.NoBody)
	if err != nil {
		return reindexJob{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return reindexJob{}, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-side close
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != want {
		return reindexJob{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var job reindexJob
	if err := json.Unmarshal(body, &job); err != nil {
		return reindexJob{}, fmt.Errorf("decode response: %w", err)
	}
	return job, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// collectImportFiles expands files and directories into the importable set.
func collectImportFiles(paths []string) ([]string, error) {
	var out []string
	importable := func(name string) bool {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".md", ".markdown", ".txt":
			return true
		}
		return false
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if importable(path) {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// parseSOPDocument reads one markdown/text file via the shared parser, then
// defaults the source pointer — the re-import lineage key — to file:<path>
// when the frontmatter omitted it (only the CLI knows the local path).
func parseSOPDocument(path string) (sopdoc.Doc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return sopdoc.Doc{}, err
	}
	doc, err := sopdoc.Parse(raw, sopdoc.TitleStem(path))
	if err != nil {
		return sopdoc.Doc{}, err
	}
	if doc.SourceURL == "" {
		doc.SourceSystem = "file"
		doc.SourceURL = "file:" + filepath.ToSlash(filepath.Clean(path))
	}
	return doc, nil
}

// postSOPImport submits one document to POST /api/sops/import.
func postSOPImport(ctx context.Context, cred *agent.Credential, backendURL string, doc sopdoc.Doc) (string, error) {
	payload := map[string]any{
		"title": doc.Title, "body": doc.Body, "tags": doc.Tags,
		"recommendation": doc.Recommendation,
		"author":         doc.Author,
		"source": map[string]string{
			"system": doc.SourceSystem, "url": doc.SourceURL, "version": doc.SourceVersion,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	token, err := cred.Token(ctx)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backendURL+"/api/sops/import", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck // read-side close
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Outcome string `json:"outcome"`
	}
	_ = json.Unmarshal(body, &out)
	if out.Outcome == "" {
		out.Outcome = "ok"
	}
	return out.Outcome, nil
}
