package knowledge

import (
	"strings"
	"unicode/utf8"
)

// Location says where in a procedure's source an excerpt sits (adr/0097;
// binding gap 15): the markdown heading path above it and its rune offset, or
// the page when the import recorded pages. Whatever the format yields.
type Location struct {
	HeadingPath []string `json:"heading_path"`
	Offset      int      `json:"offset"`
	Page        *int     `json:"page,omitempty"`
}

// Locate finds excerpt in body (an excerpt may end in the substrate's
// ellipsis) and walks the markdown headings above it. An excerpt not found in
// the body locates at the start, under the document's first heading path.
func Locate(body, excerpt string) Location {
	needle := strings.TrimSuffix(strings.TrimSpace(excerpt), "…")
	offset := 0
	if needle != "" {
		if i := strings.Index(body, needle); i >= 0 {
			offset = utf8.RuneCountInString(body[:i])
		} else if head := firstLine(needle); head != "" {
			if i := strings.Index(body, head); i >= 0 {
				offset = utf8.RuneCountInString(body[:i])
			}
		}
	}
	return Location{HeadingPath: headingPath(body, offset), Offset: offset}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// headingPath is the chain of markdown headings in force at the rune offset:
// the nearest heading at or above the offset and its ancestors by level. The
// heading an excerpt starts on counts as in force for it.
func headingPath(body string, offset int) []string {
	path := []string{}
	levels := []int{}
	pos := 0
	for _, line := range strings.Split(body, "\n") {
		lineRunes := utf8.RuneCountInString(line) + 1
		if pos > offset {
			break
		}
		if level, text, ok := heading(line); ok {
			for len(levels) > 0 && levels[len(levels)-1] >= level {
				levels = levels[:len(levels)-1]
				path = path[:len(path)-1]
			}
			levels = append(levels, level)
			path = append(path, text)
		}
		pos += lineRunes
	}
	return path
}

func heading(line string) (level int, text string, ok bool) {
	t := strings.TrimSpace(line)
	for level < len(t) && t[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(t) || t[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(t[level:]), true
}
