package ocsfview

import (
	"encoding/base64"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Decoded is a command line's encoded argument read back as text (adr/0300):
// which field it came from, which encoding was recognised, and the text. It is
// a rendering of one telemetry field, like the sentence: not evidence, not
// recorded, not cited; the original field is always shown beside it. It is
// present only when a known encoding decodes cleanly to printable text, since a
// decoder that guesses puts plausible garbage next to evidence.
type Decoded struct {
	Field    string `json:"field"`
	Encoding string `json:"encoding"` // base64-utf16le
	Text     string `json:"text"`
}

// Encodings the view recognises. The list is closed: each has an exact test,
// and anything else is not attempted.
const (
	EncodingBase64UTF16LE = "base64-utf16le" // PowerShell's -EncodedCommand
)

// decodeCommandLine reads back the encoded command of a command line whose
// program is PowerShell. The line is split into the words the program
// receives; the first names the program. When it is PowerShell, its host
// options are read as PowerShell's parser reads them, up to the first word
// that ends them, after which every word belongs to the script; the
// encoded-command switch there names a base64 token of UTF-16LE. A line whose
// program is anything else, a shell or a wrapper included, is not read: the
// view says nothing rather than reading another program's command line.
func decodeCommandLine(cmd string) (encoding, text string, ok bool) {
	inv, found := powerShellInvocation(cmd)
	if !found {
		return "", "", false
	}
	tok, found := encodedArgument(inv)
	if !found {
		return "", "", false
	}
	text, ok = decodeUTF16LE(unquote(tok))
	if !ok {
		return "", "", false
	}
	return EncodingBase64UTF16LE, text, true
}

// --- tokens ---------------------------------------------------------------------

// token is one word of a command as the program receives it: quotes removed,
// and whether any of it was quoted (a quoted word is one argument however many
// spaces it holds, and never the start of an unquoted path).
type token struct {
	text   string
	quoted bool
}

// tokenize splits one command into the words the program receives. A double
// quote toggles a run in which spaces are text; so does a single quote under
// POSIX rules, while Windows programs read an apostrophe as text. A quote may
// toggle anywhere in a word, as both families allow; a quote with no match
// ahead is text, so a line whose closing quote was lost still reads word by
// word. Backslashes are text (they are Windows path separators) except before
// a quote, where argv's rule applies: each pair is one backslash and the quote
// still toggles, and an odd one makes the quote a literal character, so `\"`
// never opens or closes a run; argv's doubled quote inside a Windows run is a
// literal quote. Under POSIX rules a backslash outside quotes makes the next
// character text, a backslash included (an escaped space does not end the
// word); inside a single-quoted run everything is text, and in a
// double-quoted one a backslash escapes only a quote, a backslash, a dollar
// or a backquote and stays before anything else, an apostrophe included.
func tokenize(cmd string, posix bool) []token {
	var out []token
	var b strings.Builder
	inWord, quoted := false, false
	var quote byte
	flush := func() {
		if inWord {
			out = append(out, token{text: b.String(), quoted: quoted})
		}
		b.Reset()
		inWord, quoted = false, false
	}
	isQuote := func(c byte) bool { return c == '"' || (posix && c == '\'') }
	// escapable is whether a backslash before this quote character counts:
	// never inside a POSIX double-quoted run for an apostrophe, which sh keeps
	// with its backslash.
	escapable := func(c byte) bool { return isQuote(c) && (!posix || c != '\'' || quote != '"') }
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case posix && c == '\\' && i+1 < len(cmd) && (quote == 0 || (quote == '"' && strings.IndexByte("\"\\$`", cmd[i+1]) >= 0)):
			inWord = true
			b.WriteByte(cmd[i+1]) // sh: the backslash makes the next character text
			i++
		case c == '\\' && quote != '\'':
			n := 1
			for i+n < len(cmd) && cmd[i+n] == '\\' {
				n++
			}
			inWord = true
			if i+n < len(cmd) && escapable(cmd[i+n]) {
				b.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					b.WriteByte(cmd[i+n]) // a literal quote
					n++
				}
			} else {
				b.WriteString(strings.Repeat(`\`, n))
			}
			i += n - 1
		case quote != 0:
			switch {
			case c != quote:
				b.WriteByte(c)
			case !posix && i+1 < len(cmd) && cmd[i+1] == '"':
				b.WriteByte('"') // argv's doubled quote inside a run is a literal quote
				i++
			default:
				quote = 0
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		case isQuote(c):
			inWord = true
			if !hasMatch(cmd[i+1:], c) {
				b.WriteByte(c) // no match ahead: text
			} else {
				quoted, quote = true, c
			}
		default:
			inWord = true
			b.WriteByte(c)
		}
	}
	flush()
	return out
}

// hasMatch reports whether an unescaped quote character q lies ahead: one
// preceded by an even number of backslashes, since an odd number makes it a
// literal character that closes nothing.
func hasMatch(ahead string, q byte) bool {
	for i := 0; i < len(ahead); i++ {
		if ahead[i] != q {
			continue
		}
		n := 0
		for j := i - 1; j >= 0 && ahead[j] == '\\'; j-- {
			n++
		}
		if n%2 == 0 {
			return true
		}
	}
	return false
}

// --- command position -----------------------------------------------------------

// powerShellInvocation is the line's words from PowerShell on when PowerShell
// is its program: named by the first word, quoted or not, under any path, or
// reached along an unquoted Windows program path with spaces in it. Words
// split as argv splits them, under sh's quoting when the program lives under
// a POSIX path. The unquoted path with spaces, as a Run key or a task often
// records it, is read as CreateProcess tries it, each space-bounded prefix in
// turn: bare words are passed over as parts of the path, and the walk ends at
// a quoted word, a word shaped as a switch (a leading dash or slash) or a
// word that begins a path of its own (a drive, a leading backslash, a leading
// dot), since each of those is an argument and the program is whatever came
// before it; PowerShell is found when a word that continues the path (it
// carries a separator) names it, and another program file ends the path. A
// POSIX argv[0] is exact, a space in it recorded quoted or escaped, so no such
// recovery is made under sh's rules. PowerShell anywhere else is a word on
// some other program's command line, and that program is not read.
func powerShellInvocation(line string) ([]token, bool) {
	tokens := tokenize(line, strings.HasPrefix(strings.TrimLeft(line, " \t\"'"), "/"))
	if len(tokens) == 0 {
		return nil, false
	}
	if isPowerShellName(programName(tokens[0].text)) {
		return tokens, true
	}
	if !isPathStart(tokens[0]) || isProgramFile(tokens[0].text) {
		return nil, false
	}
	for j := 1; j < len(tokens) && continuesPath(tokens[j]); j++ {
		if strings.Contains(tokens[j].text, `\`) && isPowerShellName(programName(tokens[j].text)) {
			return tokens[j:], true
		}
		if isProgramFile(tokens[j].text) {
			return nil, false // the path ends at another program
		}
	}
	return nil, false
}

// continuesPath reports whether a word can be the next part of an unquoted
// program path with spaces: unquoted, not shaped as a switch, and not the
// start of a path of its own.
func continuesPath(t token) bool {
	if t.quoted || t.text == "" {
		return false
	}
	switch t.text[0] {
	case '-', '/', '\\', '.':
		return false
	}
	return len(t.text) < 2 || t.text[1] != ':'
}

// encodedArgument walks one PowerShell invocation's host options as
// PowerShell's parser does: each switch resolved in the parser's order, each
// value-taking switch consuming the next token, and the walk ending at the
// first token that ends the options (-File, -Command, a word that is no
// switch, a switch the parser does not know, a lone dash), after which the
// rest of the line is the script's own arguments.
func encodedArgument(inv []token) (string, bool) {
	for i := 1; i < len(inv); i++ {
		name, isSwitch := switchKey(inv[i].text)
		if !isSwitch {
			return "", false
		}
		switch classifySwitch(name) {
		case switchEncodedCommand:
			if i+1 < len(inv) {
				return inv[i+1].text, true
			}
			return "", false
		case switchEndsOptions:
			return "", false
		case switchTakesValue:
			i++
		case switchFlag:
			// takes no value; the walk continues
		}
	}
	return "", false
}

func isPowerShellName(name string) bool {
	return name == "powershell" || name == "powershell_ise" || name == "pwsh"
}

// isPathStart reports whether an unquoted word begins a Windows path: a drive
// or a directory separator, the shape of an install path with spaces in it.
func isPathStart(t token) bool {
	return !t.quoted && (strings.Contains(t.text, `\`) || (len(t.text) > 1 && t.text[1] == ':'))
}

// isProgramFile reports whether a word ends a program path by its extension.
func isProgramFile(word string) bool {
	switch strings.ToLower(word[strings.LastIndex(word, ".")+1:]) {
	case "exe", "com", "bat", "cmd", "ps1", "vbs", "js", "msi", "scr":
		return strings.Contains(word, ".")
	}
	return false
}

// programName is the program a word names: lower-cased, without its path and
// without a .exe extension.
func programName(word string) string {
	name := strings.ToLower(unquote(word))
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}
	return strings.TrimSuffix(name, ".exe")
}

// --- PowerShell's switches --------------------------------------------------------

// switchKey normalises a word as PowerShell's parser does before matching it:
// a leading dash or slash is removed, and a second dash after a first; the
// name is lower-cased. A word with neither is not a switch: the parser reads
// it as the script file, and the options end.
func switchKey(word string) (name string, isSwitch bool) {
	word = strings.TrimSpace(word)
	if word == "" || (word[0] != '-' && word[0] != '/') {
		return "", false
	}
	name = word[1:]
	if word[0] == '-' && strings.HasPrefix(name, "-") {
		name = name[1:]
	}
	return strings.ToLower(name), true
}

// switchKind is how a PowerShell switch bears on finding the encoded command.
type switchKind int

const (
	switchFlag           switchKind = iota // takes no value; the walk continues
	switchTakesValue                       // takes the next token as its value
	switchEndsOptions                      // the rest of the line is the script's or the command's
	switchEncodedCommand                   // the next token is the encoded command
)

// hostSwitches is PowerShell's switch table, transcribed from its command-line
// parser in the order it tests them, since the first match wins: each entry
// is a name, the shortest abbreviation accepted, and what the switch does with
// the words after it. A switch is matched case-insensitively as a prefix of
// the name no shorter than the abbreviation. The explicit aliases (ec, ep,
// if, of, ea, wd, cwa) are entries of their own, as in the parser.
var hostSwitches = []struct {
	name, min string
	kind      switchKind
}{
	// Windows PowerShell's -Version takes a value; pwsh's prints and exits,
	// with nothing after it that could be an encoded command.
	{"version", "v", switchTakesValue},
	{"help", "h", switchFlag},
	{"?", "?", switchFlag},
	{"login", "l", switchFlag},
	{"noexit", "noe", switchFlag},
	{"noprofile", "nop", switchFlag},
	{"nologo", "nol", switchFlag},
	{"noninteractive", "noni", switchFlag},
	{"socketservermode", "so", switchFlag},
	{"servermode", "s", switchFlag},
	{"namedpipeservermode", "nam", switchFlag},
	{"sshservermode", "sshs", switchFlag},
	{"noprofileloadtime", "noprofileloadtime", switchFlag},
	{"interactive", "i", switchFlag},
	{"configurationfile", "configurationfile", switchTakesValue},
	{"configurationname", "config", switchTakesValue},
	{"custompipename", "cus", switchTakesValue},
	{"commandwithargs", "commandwithargs", switchEndsOptions},
	{"cwa", "cwa", switchEndsOptions},
	{"command", "c", switchEndsOptions},
	{"windowstyle", "w", switchTakesValue},
	{"file", "f", switchEndsOptions},
	{"outputformat", "o", switchTakesValue},
	{"of", "o", switchTakesValue},
	{"inputformat", "inp", switchTakesValue},
	{"if", "if", switchTakesValue},
	{"executionpolicy", "ex", switchTakesValue},
	{"ep", "ep", switchTakesValue},
	{"encodedcommand", "e", switchEncodedCommand},
	{"ec", "e", switchEncodedCommand},
	{"encodedarguments", "encodeda", switchTakesValue},
	{"ea", "ea", switchTakesValue},
	{"settingsfile", "settings", switchTakesValue},
	{"sta", "sta", switchFlag},
	{"mta", "mta", switchFlag},
	{"workingdirectory", "wo", switchTakesValue},
	{"wd", "wd", switchTakesValue},
	{"removeworkingdirectorytrailingcharacter", "removeworkingdirectorytrailingcharacter", switchFlag},
	// Windows PowerShell only; its parser is not published, so the documented
	// full names are accepted and no abbreviation is guessed.
	{"psconsolefile", "psc", switchTakesValue},
	{"importsystemmodules", "importsystemmodules", switchFlag},
}

// classifySwitch resolves a normalised switch name against the table in the
// parser's order. A name that matches nothing is read by the parser as the
// script file, so it ends the options; so does the empty name of a lone dash.
func classifySwitch(name string) switchKind {
	for _, s := range hostSwitches {
		if len(name) >= len(s.min) && len(name) <= len(s.name) && s.name[:len(name)] == name {
			return s.kind
		}
	}
	return switchEndsOptions
}

// --- decoders ---------------------------------------------------------------------

// byteOrderMark is the U+FEFF a UTF-16 encoder may put first.
const byteOrderMark = 0xFEFF

// decodeUTF16LE reads a base64 token as UTF-16LE text: an even number of
// bytes, well-formed code units, printable, and mostly ASCII. The last rule
// is what a PowerShell command looks like (cmdlets, operators, paths) and what
// a short run of random bytes read as UTF-16 does not: those land in the
// graphic ideographic planes and would otherwise pass as text.
func decodeUTF16LE(tok string) (string, bool) {
	raw, ok := base64Bytes(tok)
	if !ok || len(raw) == 0 || len(raw)%2 != 0 {
		return "", false
	}
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	runes := utf16.Decode(units)
	if len(runes) > 0 && runes[0] == byteOrderMark {
		runes = runes[1:]
	}
	text := string(runes)
	if !printable(text) || !mostlyASCII(text) {
		return "", false
	}
	return text, true
}

// mostlyASCII reports whether at least three runes in four are ASCII.
func mostlyASCII(text string) bool {
	total, ascii := 0, 0
	for _, r := range text {
		total++
		if r < utf8.RuneSelf {
			ascii++
		}
	}
	return total > 0 && ascii*4 >= total*3
}

// base64Bytes decodes a token that is wholly base64 with correct padding, in
// the standard or the URL alphabet. A token with other characters, or with
// padding that does not fit, is not a blob.
func base64Bytes(tok string) ([]byte, bool) {
	if tok == "" || len(tok)%4 != 0 {
		return nil, false
	}
	for _, c := range tok {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '+', c == '/', c == '-', c == '_', c == '=':
		default:
			return nil, false
		}
	}
	if raw, err := base64.StdEncoding.DecodeString(tok); err == nil {
		return raw, true
	}
	if raw, err := base64.URLEncoding.DecodeString(tok); err == nil {
		return raw, true
	}
	return nil, false
}

// printable reports whether text is wholly graphic runes, spaces, tabs and line
// breaks, with no replacement character: what a command or a script looks like,
// and what random bytes almost never do.
func printable(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	for _, r := range text {
		if r == utf8.RuneError {
			return false
		}
		if r == '\t' || r == '\n' || r == '\r' || r == ' ' {
			continue
		}
		if !unicode.IsGraphic(r) {
			return false
		}
	}
	return true
}

// unquote strips stray quote characters around a word, matched or not: the
// program's name or its token may carry the quote of a line whose other
// quote was lost, or an apostrophe a Windows program reads as text.
func unquote(tok string) string {
	return strings.Trim(tok, `"'`)
}

// decode records the command line's decoded argument on the view, when the
// field carries one that decodes cleanly. One per view; the first template to
// find one wins.
func (r *renderer) decode(path string) {
	if r.decoded != nil {
		return
	}
	cmd := r.str(path)
	if cmd == "" {
		return
	}
	if encoding, text, ok := decodeCommandLine(cmd); ok {
		r.decoded = &Decoded{Field: path, Encoding: encoding, Text: text}
	}
}
