// Package sopdoc parses institutional-knowledge documents for SOP import
// (design/06 §2.4): markdown with optional YAML frontmatter carrying
// attribution. Shared by the `reckon knowledge import` CLI and the workbench's
// import endpoint so there is ONE robust parser, not a reimplementation.
//
// Conversion from richer formats (PDF, Word, Confluence) stays at the edge
// (pandoc → markdown); this package only splits frontmatter from prose. That is
// not a "format zoo" — it is a trivial, well-bounded text split.
package sopdoc

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Doc is one parsed SOP document: frontmatter attribution plus the prose body.
// SourceURL is empty when the frontmatter omitted it — the caller supplies the
// default (the CLI uses the file path, the workbench the filename), since only
// the caller knows the origin.
type Doc struct {
	Title          string
	Author         string
	Tags           []string
	Recommendation string
	SourceSystem   string
	SourceURL      string
	SourceVersion  string
	Body           string
}

// frontmatter is the YAML block a document may open with.
type frontmatter struct {
	Title          string   `yaml:"title"`
	Author         string   `yaml:"author,omitempty"`
	Tags           []string `yaml:"tags,omitempty"`
	Recommendation string   `yaml:"recommendation,omitempty"`
	Source         struct {
		System  string `yaml:"system,omitempty"`
		URL     string `yaml:"url"`
		Version string `yaml:"version,omitempty"`
	} `yaml:"source,omitempty"`
}

// Parse parses markdown with optional leading `---` YAML frontmatter.
// fallbackTitle seeds the title when neither frontmatter nor a leading `# `
// heading supplies one. It does NOT default the source pointer — the caller
// owns that (it knows the origin). Errors on an empty body or malformed
// frontmatter.
func Parse(raw []byte, fallbackTitle string) (Doc, error) {
	fm, body, err := splitFrontmatter(raw)
	if err != nil {
		return Doc{}, err
	}
	doc := Doc{
		Title:          fm.Title,
		Author:         fm.Author,
		Tags:           fm.Tags,
		Recommendation: fm.Recommendation,
		SourceSystem:   fm.Source.System,
		SourceURL:      fm.Source.URL,
		SourceVersion:  fm.Source.Version,
		Body:           CanonicalBody(body),
	}
	if doc.Body == "" {
		return Doc{}, fmt.Errorf("empty body")
	}
	if doc.Title == "" {
		doc.Title = firstHeading(doc.Body)
	}
	if doc.Title == "" {
		doc.Title = fallbackTitle
	}
	return doc, nil
}

// CanonicalBody is the one form a SOP body is stored and compared in: line
// endings are LF, leading blank lines and trailing whitespace are gone. None
// of that carries Markdown meaning, and the indentation of the first content
// line (an opening code block) is kept. Parse applies it to what it reads and
// the knowledge store to every body it writes, so a body shown and imported
// again is byte-identical to the one stored.
func CanonicalBody(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	for {
		line, rest, found := strings.Cut(body, "\n")
		if !found || strings.TrimSpace(line) != "" {
			break
		}
		body = rest
	}
	return strings.TrimRight(body, " \t\n")
}

// Render is the inverse of Parse: the document as a markdown file with a YAML
// frontmatter block carrying the attribution Parse reads, then the body. A
// document rendered, saved and imported again is unchanged, because the
// frontmatter holds exactly the fields the import compares and nothing of the
// ingest (no importer, no time), and the body is written in its canonical
// form. Empty fields are left out.
func Render(doc Doc) ([]byte, error) {
	fm := frontmatter{
		Title:          doc.Title,
		Author:         doc.Author,
		Tags:           doc.Tags,
		Recommendation: doc.Recommendation,
	}
	fm.Source.System = doc.SourceSystem
	fm.Source.URL = doc.SourceURL
	fm.Source.Version = doc.SourceVersion
	head, err := yaml.Marshal(fm)
	if err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(head)
	out.WriteString("---\n\n")
	out.WriteString(CanonicalBody(doc.Body))
	out.WriteString("\n")
	return out.Bytes(), nil
}

// TitleStem derives a human title from a filename (dashes → spaces), for the
// fallbackTitle argument.
func TitleStem(name string) string {
	stem := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	return strings.ReplaceAll(stem, "-", " ")
}

// splitFrontmatter separates an optional leading `---` YAML block from the
// prose. No frontmatter is fine — the whole content is body.
func splitFrontmatter(raw []byte) (frontmatter, string, error) {
	var fm frontmatter
	content := strings.ReplaceAll(string(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))), "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return fm, content, nil
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		if trimmed, ok := strings.CutSuffix(rest, "\n---"); ok {
			end = len(trimmed)
			rest = trimmed + "\n---\n"
		} else {
			return fm, "", fmt.Errorf("unterminated frontmatter (opening --- without closing ---)")
		}
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return fm, "", fmt.Errorf("frontmatter: %w", err)
	}
	return fm, rest[end+len("\n---\n"):], nil
}

// firstHeading returns the first `# ` heading's text, or "".
func firstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok && strings.TrimSpace(h) != "" {
			return strings.TrimSpace(h)
		}
	}
	return ""
}
