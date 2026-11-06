package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/internal/branding"
	"github.com/sd-strax/reckon/internal/sopdoc"
	"github.com/sd-strax/reckon/knowledge"
)

// The SOP management subcommands: thin clients over the routes the backend
// already serves (GET /api/sops, GET and DELETE /api/sops/{id}). They give an
// operator a window onto the corpus without curl and a dev token; a library
// view in the workbench stays later work. Each prints a human form by default
// and the server's JSON for the object(s) it touched under --json.

const manageWait = time.Minute

// runKnowledgeList prints the corpus, retired SOPs included on request. The
// source url is in the row because it is the lineage key: an operator reads it
// to predict what a re-import will revise.
func runKnowledgeList(args []string) error {
	fl := flag.NewFlagSet("knowledge list", flag.ContinueOnError)
	user, pass := loginFlags(fl)
	includeRetired := fl.Bool("include-retired", false, "list retired SOPs too")
	asJSON := fl.Bool("json", false, "print the server's JSON")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 0 {
		return fmt.Errorf("usage: %s knowledge list [--include-retired] [--json] [-user u] [-password p]", branding.CLI)
	}
	ctx, cancel := context.WithTimeout(context.Background(), manageWait)
	defer cancel()
	c, err := newKnowledgeClient(ctx, *user, *pass)
	if err != nil {
		return err
	}
	path := "/api/sops"
	if *includeRetired {
		path += "?include_retired=true"
	}
	var out struct {
		SOPs []knowledge.SOP `json:"sops"`
	}
	raw, err := c.call(ctx, http.MethodGet, path, http.StatusOK)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(os.Stdout, raw)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return renderSOPList(os.Stdout, out.SOPs)
}

// runKnowledgeShow prints one SOP's current revision as the document import
// reads (frontmatter + body, sopdoc.Render), so `show > f.md` followed by
// `import f.md` is an unchanged re-import. The ingest provenance (id,
// revision, importer, time, status) goes to stderr, where it does not join
// the document.
func runKnowledgeShow(args []string) error {
	fl := flag.NewFlagSet("knowledge show", flag.ContinueOnError)
	user, pass := loginFlags(fl)
	asJSON := fl.Bool("json", false, "print the server's JSON")
	id, err := sopIDArg(fl, args, "show")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), manageWait)
	defer cancel()
	c, err := newKnowledgeClient(ctx, *user, *pass)
	if err != nil {
		return err
	}
	raw, err := c.call(ctx, http.MethodGet, "/api/sops/"+id.String(), http.StatusOK)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(os.Stdout, raw)
	}
	var sop knowledge.SOP
	if err := json.Unmarshal(raw, &sop); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	doc, err := sopdoc.Render(sopDocument(sop))
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, provenanceLine(sop))
	_, err = os.Stdout.Write(doc)
	return err
}

// runKnowledgeRetire retires one SOP and prints what it retired. No prompt and
// no dry run: retire is soft, the content stays hash-addressable, and a
// re-import of the same source revives the lineage.
func runKnowledgeRetire(args []string) error {
	fl := flag.NewFlagSet("knowledge retire", flag.ContinueOnError)
	user, pass := loginFlags(fl)
	asJSON := fl.Bool("json", false, "print the server's JSON of the retired SOP")
	id, err := sopIDArg(fl, args, "retire")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), manageWait)
	defer cancel()
	c, err := newKnowledgeClient(ctx, *user, *pass)
	if err != nil {
		return err
	}
	if _, err := c.call(ctx, http.MethodDelete, "/api/sops/"+id.String(), http.StatusNoContent); err != nil {
		return err
	}
	// Read it back retired: the title for the line, the record for --json.
	raw, err := c.call(ctx, http.MethodGet, "/api/sops/"+id.String(), http.StatusOK)
	if err != nil {
		return fmt.Errorf("retired %s, then could not read it back: %w", id, err)
	}
	if *asJSON {
		return printJSON(os.Stdout, raw)
	}
	var sop knowledge.SOP
	if err := json.Unmarshal(raw, &sop); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	_, err = fmt.Fprintf(os.Stdout, "retired %q (%s, revision %d)\n", sop.Title, sop.ID, sop.Revision)
	return err
}

// sopIDArg parses args as flags around one positional argument, the SOP id,
// in either order: the flag package stops at the first non-flag, so flags
// after the id are parsed in a second pass over what it left.
func sopIDArg(fl *flag.FlagSet, args []string, verb string) (uuid.UUID, error) {
	positional, err := parseInterspersed(fl, args)
	if err != nil {
		return uuid.Nil, err
	}
	if len(positional) != 1 {
		return uuid.Nil, fmt.Errorf("usage: %s knowledge %s <id> [--json] [-user u] [-password p]", branding.CLI, verb)
	}
	id, err := uuid.Parse(positional[0])
	if err != nil {
		return uuid.Nil, fmt.Errorf("%q is not a SOP id: %w", positional[0], err)
	}
	return id, nil
}

// parseInterspersed parses flags and positional arguments in any order,
// returning the positional ones. A `--` is consumed by the flag package, so
// what follows one is parsed as flags again; no knowledge argument starts
// with a dash, so nothing is lost.
func parseInterspersed(fl *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fl.Parse(args); err != nil {
			return nil, err
		}
		rest := fl.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// knowledgeClient is the authenticated HTTP side of the management commands.
// token is a function so a test can stand in for the credential.
type knowledgeClient struct {
	base  string
	token func(context.Context) (string, error)
}

func newKnowledgeClient(ctx context.Context, user, pass string) (*knowledgeClient, error) {
	cred, backendURL, err := knowledgeSession(ctx, user, pass)
	if err != nil {
		return nil, err
	}
	return &knowledgeClient{base: backendURL, token: cred.Token}, nil
}

// call makes one bodiless request and returns the response body when the
// status is the one wanted; any other status is an error carrying the body.
func (c *knowledgeClient) call(ctx context.Context, method, path string, want int) ([]byte, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, http.NoBody) //nolint:gosec // the configured local backend plus a route with a parsed uuid
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // same request
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-side close
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != want {
		return nil, fmt.Errorf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// printJSON re-indents the server's JSON for a terminal.
func printJSON(w io.Writer, raw []byte) error {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return fmt.Errorf("the server's response is not JSON: %w", err)
	}
	_, err := fmt.Fprintln(w, out.String())
	return err
}

// renderSOPList writes one row per SOP: id, revision, status, title, tags,
// source url, importer and time. A retired SOP says so in its status column,
// plainly, in the same list.
func renderSOPList(w io.Writer, sops []knowledge.SOP) error {
	// The tabwriter buffers; a write error surfaces from Flush.
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tREV\tSTATUS\tTITLE\tTAGS\tSOURCE\tIMPORTED")
	for _, s := range sops {
		source := ""
		if s.Source != nil {
			source = s.Source.URL
		}
		imported := s.ImportedBy
		if s.ImportedAt != nil {
			imported = strings.TrimSpace(imported + " " + s.ImportedAt.UTC().Format(time.RFC3339))
		}
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Revision, s.Status, s.Title, strings.Join(s.Tags, ","), source, imported)
	}
	return tw.Flush()
}

// sopDocument maps a SOP onto the document shape import reads: the content
// fields and the source, never the ingest provenance.
func sopDocument(s knowledge.SOP) sopdoc.Doc {
	doc := sopdoc.Doc{
		Title:          s.Title,
		Author:         s.AuthorID,
		Tags:           s.Tags,
		Recommendation: s.Recommendation,
		Body:           s.Body,
	}
	if s.Source != nil {
		doc.SourceSystem, doc.SourceURL, doc.SourceVersion = s.Source.System, s.Source.URL, s.Source.Version
	} else {
		// Authored in-product: the lineage id is the pointer, so an import of
		// this document revises this SOP rather than opening a second lineage.
		doc.SourceSystem, doc.SourceURL = "reckon", knowledge.SOPSourcePrefix+s.ID.String()
	}
	return doc
}

// provenanceLine is the ingest side of a SOP in one line, for stderr.
func provenanceLine(s knowledge.SOP) string {
	line := fmt.Sprintf("# %s revision %d, %s", s.ID, s.Revision, s.Status)
	if s.ImportedBy != "" {
		line += ", imported by " + s.ImportedBy
	}
	if s.ImportedAt != nil {
		line += " at " + s.ImportedAt.UTC().Format(time.RFC3339)
	}
	return line
}
