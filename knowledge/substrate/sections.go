package substrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// SplitterVersion names the rule that cut a body into sections. Stored with
// every section row so a later rule can tell its own rows from older ones and
// Reindex can rebuild them. Bump it when SplitSections changes its output
// for any input.
const SplitterVersion = 1

// DefaultSectionBudgetRunes bounds one section at write time (WithSectionBudget
// overrides). A rune stand-in for the consumer's token budget: four runes to
// a token is the usual ratio for English prose, so 16000 runes is about 4000
// tokens. A recall query may ask for a smaller excerpt; it cannot ask for a
// bigger section than this.
const DefaultSectionBudgetRunes = 16_000

// Section is one heading-delimited piece of an entry's body (§2.3). The
// section is the unit of RANK recall: each is embedded on its own, ranked on
// its own, and returned as the excerpt with its place in the document.
//
// Text is the body's runes in [Start, End) — the heading line included, so a
// reader sees what the piece is called — and the offsets locate it in the
// hashed content. The content hash still covers the whole body (§3): sections
// are a view over it, never a second copy of record.
type Section struct {
	// Idx is the section's 1-based position in the body, counting parts.
	Idx int
	// HeadingPath is the chain of headings above and including this
	// section's own, outermost first. Empty for text before the first
	// heading and for a body with no headings.
	HeadingPath []string
	// Part and Parts are set when a section longer than the budget was cut
	// on paragraph boundaries: this is part Part of Parts. Zero otherwise.
	Part, Parts int
	// Start and End are rune offsets into the body: Text == body[Start:End]
	// in runes.
	Start, End int
	Text       string
	// SplitterVersion and BudgetRunes name the cut this section belongs to:
	// the rule and the section budget that made it. Set on sections read from
	// the store; together they are Cut, which a reader pins beside the
	// revision, because a changed budget re-cuts the same revision.
	SplitterVersion, BudgetRunes int
}

// Cut names the cut a stored section belongs to, "<splitter>/<budget>". Section
// indexes are stable within one revision and one cut; a re-cut under another
// budget or splitter renumbers them, so a reader that was handed an index
// passes the cut back and is refused when it has moved.
func (s Section) Cut() string { return fmt.Sprintf("%d/%d", s.SplitterVersion, s.BudgetRunes) }

// Location renders where the section sits, for rationale text and the
// record: the heading path joined with " > ", with the part when cut.
func (s Section) Location() string {
	loc := strings.Join(s.HeadingPath, " > ")
	if loc == "" {
		loc = "(before the first heading)"
	}
	if s.Parts > 1 {
		loc += fmt.Sprintf(" (part %d/%d)", s.Part, s.Parts)
	}
	return loc
}

// SplitSections cuts a body into sections on ATX headings ("#" to "######"
// at the start of a line, followed by a space). Text before the first heading
// is its own section when it is not blank; a body with no headings is one
// section. Headings inside fenced code blocks are text, not headings. A
// section longer than budgetRunes is cut on paragraph boundaries into parts
// (budgetRunes <= 0 means no budget). The result is deterministic in the body
// and SplitterVersion; a blank body yields nil.
func SplitSections(body string, budgetRunes int) []Section {
	runes := []rune(body)
	var lines []bodyLine
	for at := 0; at < len(runes); {
		end := at
		for end < len(runes) && runes[end] != '\n' {
			end++
		}
		text := string(runes[at:end])
		text = strings.TrimSuffix(text, "\r")
		lines = append(lines, bodyLine{start: at, end: end, text: text})
		at = end + 1
	}

	// Pass 1: find heading lines, tracking fences so a "#" in a code block
	// is text.
	type headingAt struct {
		line  int
		level int
		title string
	}
	var headings []headingAt
	var fence struct {
		open bool
		ch   byte
		n    int
	}
	for i, l := range lines {
		if ch, n, rest, ok := fenceLine(l.text); ok {
			switch {
			case !fence.open:
				fence.open, fence.ch, fence.n = true, ch, n
				continue
			case ch == fence.ch && n >= fence.n && strings.TrimSpace(rest) == "":
				// A fence closes only on a run of its own character at least
				// as long as the opening, with nothing after it (CommonMark);
				// a shorter or different run inside it is content.
				fence.open = false
				continue
			}
		}
		if fence.open {
			continue
		}
		if level, title, ok := atxHeading(l.text); ok {
			headings = append(headings, headingAt{line: i, level: level, title: title})
		}
	}

	// Pass 2: cut raw sections (start line, end line exclusive, path).
	type raw struct {
		from, to int
		path     []string
	}
	var raws []raw
	if len(headings) == 0 {
		raws = append(raws, raw{from: 0, to: len(lines)})
	} else {
		if headings[0].line > 0 {
			raws = append(raws, raw{from: 0, to: headings[0].line})
		}
		var stack []headingAt
		for i, h := range headings {
			for len(stack) > 0 && stack[len(stack)-1].level >= h.level {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, h)
			path := make([]string, len(stack))
			for j, s := range stack {
				path[j] = s.title
			}
			to := len(lines)
			if i+1 < len(headings) {
				to = headings[i+1].line
			}
			raws = append(raws, raw{from: h.line, to: to, path: path})
		}
	}

	// Pass 3: trim blank edges, drop empty pieces, apply the budget.
	var out []Section
	for _, r := range raws {
		from, to := r.from, r.to
		for from < to && strings.TrimSpace(lines[from].text) == "" {
			from++
		}
		for to > from && strings.TrimSpace(lines[to-1].text) == "" {
			to--
		}
		if from >= to {
			continue
		}
		start, end := lines[from].start, lines[to-1].end
		whole := Section{HeadingPath: r.path, Start: start, End: end, Text: string(runes[start:end])}
		if budgetRunes <= 0 || end-start <= budgetRunes {
			out = append(out, whole)
			continue
		}
		parts := cutOnParagraphs(runes, lines[from:to], budgetRunes)
		for i := range parts {
			parts[i].HeadingPath = r.path
			parts[i].Part, parts[i].Parts = i+1, len(parts)
		}
		out = append(out, parts...)
	}
	for i := range out {
		out[i].Idx = i + 1
	}
	return out
}

// fenceLine reports whether a line opens or closes a fenced code block: up to
// three spaces of indent, then a run of at least three backticks or tildes.
// It returns the fence character, the run length and what follows the run.
func fenceLine(line string) (ch byte, n int, rest string, ok bool) {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) || (line[i] != '`' && line[i] != '~') {
		return 0, 0, "", false
	}
	ch = line[i]
	for i < len(line) && line[i] == ch {
		i++
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	return ch, n, line[i:], true
}

// bodyLine is one line of a body: its rune offsets (end excludes the
// newline) and its text without a trailing carriage return.
type bodyLine struct {
	start, end int
	text       string
}

// atxHeading reports whether a line is an ATX heading: one to six "#" at
// column 0, then at least one space, then text. Closing "#" are stripped.
func atxHeading(line string) (level int, title string, ok bool) {
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(line) || line[level] != ' ' {
		return 0, "", false
	}
	title = strings.TrimSpace(line[level:])
	title = strings.TrimSpace(strings.TrimRight(title, "#"))
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}

// cutOnParagraphs packs a section's paragraphs (blank-line separated) into
// parts no longer than budget runes. A single paragraph over the budget is
// cut on its line boundaries; a single line over the budget is cut at the
// budget. lines is the section's own lines, already trimmed of blank edges.
func cutOnParagraphs(runes []rune, lines []bodyLine, budget int) []Section {
	// Paragraphs as [from, to) line ranges.
	type span struct{ from, to int }
	var paras []span
	for i := 0; i < len(lines); {
		for i < len(lines) && strings.TrimSpace(lines[i].text) == "" {
			i++
		}
		from := i
		for i < len(lines) && strings.TrimSpace(lines[i].text) != "" {
			i++
		}
		if from < i {
			paras = append(paras, span{from, i})
		}
	}

	// Pieces are rune ranges; a paragraph over budget is reduced to line
	// ranges, and a line over budget to budget-sized slices.
	type piece struct{ start, end int }
	var pieces []piece
	for _, p := range paras {
		ps, pe := lines[p.from].start, lines[p.to-1].end
		if pe-ps <= budget {
			pieces = append(pieces, piece{ps, pe})
			continue
		}
		for _, l := range lines[p.from:p.to] {
			if l.end-l.start <= budget {
				pieces = append(pieces, piece{l.start, l.end})
				continue
			}
			for at := l.start; at < l.end; at += budget {
				e := min(at+budget, l.end)
				pieces = append(pieces, piece{at, e})
			}
		}
	}

	// Greedy pack: a piece joins the open part when the span from the
	// part's start to the piece's end stays within budget.
	var out []Section
	cur := piece{-1, -1}
	flush := func() {
		if cur.start >= 0 {
			out = append(out, Section{Start: cur.start, End: cur.end, Text: string(runes[cur.start:cur.end])})
		}
	}
	for _, pc := range pieces {
		if cur.start >= 0 && pc.end-cur.start <= budget {
			cur.end = pc.end
			continue
		}
		flush()
		cur = pc
	}
	flush()
	return out
}

// sectionEmbedText is what a section embeds as: the entry's title, the
// heading path and the text, so a section's vector carries its document's
// context. A section with no heading path (a body with no headings, or the
// text before the first) embeds exactly as the document does, so a
// one-section entry has one vector under two keys and nothing drifts.
func sectionEmbedText(title string, s Section) string {
	if len(s.HeadingPath) == 0 {
		return embedText(title, s.Text)
	}
	return title + " > " + strings.Join(s.HeadingPath, " > ") + "\n\n" + s.Text
}

// Sections returns the entry's sections in body order (§2.3): the cut the
// store made at write time under its splitter version and budget. An entry
// written before sections existed has none; Reindex builds them.
func (p *Postgres) Sections(ctx context.Context, ns, corpus string, id uuid.UUID) ([]Section, error) {
	if err := validNamespace(ns); err != nil {
		return nil, err
	}
	if _, err := p.corpus(corpus); err != nil {
		return nil, err
	}
	if _, err := getEntry(ctx, p.db, ns, corpus, id, false); err != nil {
		return nil, err
	}
	return loadSections(ctx, p.db, id)
}

// sectionCols is the scan order shared by every section query.
const sectionCols = `idx, heading_path, part, parts, start_rune, end_rune, text, splitter_version, budget_runes`

type rowQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func loadSections(ctx context.Context, q rowQuerier, id uuid.UUID) ([]Section, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+sectionCols+` FROM substrate_sections WHERE entry_id = $1 ORDER BY idx`, id)
	if err != nil {
		return nil, fmt.Errorf("substrate: load sections: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-side close
	var out []Section
	for rows.Next() {
		var s Section
		if err := rows.Scan(&s.Idx, pq.Array(&s.HeadingPath), &s.Part, &s.Parts, &s.Start, &s.End, &s.Text, &s.SplitterVersion, &s.BudgetRunes); err != nil {
			return nil, fmt.Errorf("substrate: scan section: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// writeSections replaces the entry's section rows with a fresh cut of its
// body under the store's splitter version and budget. Section vectors for
// every model go with the old rows: a re-cut section is a different text.
func (p *Postgres) writeSections(ctx context.Context, tx *sql.Tx, e Entry) ([]Section, error) {
	if _, err := tx.ExecContext(ctx, `DELETE FROM substrate_sections WHERE entry_id = $1`, e.ID); err != nil {
		return nil, fmt.Errorf("substrate: clear sections for %s: %w", e.ID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM substrate_embeddings WHERE entry_id = $1 AND section <> 0`, e.ID); err != nil {
		return nil, fmt.Errorf("substrate: clear section vectors for %s: %w", e.ID, err)
	}
	secs := SplitSections(e.Body, p.sectionBudget)
	for i := range secs {
		s := &secs[i]
		s.SplitterVersion, s.BudgetRunes = SplitterVersion, p.sectionBudget
		path := s.HeadingPath
		if path == nil {
			path = []string{} // pq encodes a nil slice as NULL; the column is an empty array
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO substrate_sections
			   (entry_id, idx, heading_path, part, parts, start_rune, end_rune, text, splitter_version, budget_runes)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			e.ID, s.Idx, pq.Array(path), s.Part, s.Parts, s.Start, s.End, s.Text, s.SplitterVersion, s.BudgetRunes); err != nil {
			return nil, fmt.Errorf("substrate: insert section %d of %s: %w", s.Idx, e.ID, err)
		}
	}
	return secs, nil
}
