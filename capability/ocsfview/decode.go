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
	Encoding string `json:"encoding"` // base64-utf16le | base64
	Text     string `json:"text"`
}

// Encodings the view recognises. The list is closed: each has an exact test,
// and anything else is not attempted.
const (
	EncodingBase64UTF16LE = "base64-utf16le" // PowerShell's -EncodedCommand
	EncodingBase64        = "base64"         // a shell one-liner's blob
)

// minBase64Token is the shortest bare token the view will try as base64. Short
// alphanumeric words are often valid base64 by accident; at this length an
// accidental decode to wholly printable text is vanishingly rare.
const minBase64Token = 16

// decodeCommandLine finds the one encoded argument in a command line and reads
// it back. PowerShell's encoded command is tried first, and only where
// PowerShell's own command-line grammar puts it: PowerShell invoked as the
// program (the first word, or the word a shell launches), then its host
// options, which end at -File, -Command or a lone dash, after which every word
// belongs to the script. The encoded-command switch there, in any spelling
// PowerShell accepts, names a base64 token of UTF-16LE. A flagged token that is
// not UTF-16LE is not a PowerShell command and gets no second guess. Then each
// other bare token long enough to be meant as a blob is tried as base64 of
// UTF-8. The first clean decode wins.
func decodeCommandLine(cmd string) (encoding, text string, ok bool) {
	tokens := strings.Fields(cmd)
	flagged := -1
	if at := powerShellEncodedArgument(tokens); at >= 0 {
		flagged = at
		if text, ok := decodeUTF16LE(unquote(tokens[at])); ok {
			return EncodingBase64UTF16LE, text, true
		}
	}
	for i, tok := range tokens {
		if i == flagged {
			continue
		}
		tok = unquote(tok)
		if len(tok) < minBase64Token {
			continue
		}
		if text, ok := decodeUTF8(tok); ok {
			return EncodingBase64, text, true
		}
	}
	return "", "", false
}

// powerShellEncodedArgument is the index of the token PowerShell would read as
// its encoded command, or -1. It walks PowerShell's host options from the
// invocation, consuming the value of each option that takes one, and stops
// where the host options end: -File, -Command, a lone dash, or a word that is
// no option and no option's value (PowerShell reads it as the file or the
// command, and the rest of the line as that script's own arguments).
func powerShellEncodedArgument(tokens []string) int {
	start := powerShellInvocation(tokens)
	if start < 0 {
		return -1
	}
	for i := start + 1; i < len(tokens); i++ {
		name, isSwitch := powerShellSwitch(tokens[i])
		if !isSwitch {
			return -1
		}
		switch classifySwitch(name) {
		case switchEncodedCommand:
			if i+1 < len(tokens) {
				return i + 1
			}
			return -1
		case switchEndsOptions:
			return -1
		case switchTakesValue:
			i++
		case switchFlag:
			// takes no value; the walk continues
		}
	}
	return -1
}

// powerShellInvocation is the index of the token that invokes PowerShell as
// the program, or -1: the first word of the line, or the word a Windows shell
// launches (after cmd's /c, /k or /r, after start or call, or after a chain
// operator). PowerShell named anywhere else is data on someone else's command
// line, and its words are not PowerShell's options.
func powerShellInvocation(tokens []string) int {
	for i, tok := range tokens {
		if !isPowerShell(unquote(tok)) {
			continue
		}
		if i == 0 || launchesNext(tokens[i-1]) {
			return i
		}
	}
	return -1
}

// launchesNext reports whether a Windows shell reads the token after this one
// as a program to run.
func launchesNext(tok string) bool {
	switch strings.ToLower(unquote(tok)) {
	case "/c", "/k", "/r", "start", "call", "&&", "||", "|", ";", "&":
		return true
	}
	return false
}

// isPowerShell recognises the PowerShell program by its name, with or without
// a path and an extension: powershell, powershell_ise, pwsh.
func isPowerShell(tok string) bool {
	name := strings.ToLower(tok)
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".exe")
	return name == "powershell" || name == "powershell_ise" || name == "pwsh"
}

// powerShellSwitch reads a token as one of PowerShell's switches: a dash or a
// slash, an optional second dash (PowerShell strips one before matching, so
// --EncodedCommand is accepted), then the lower-cased name. A lone dash is the
// switch that reads the command from standard input; it is returned as an
// empty name. Anything else is not a switch.
func powerShellSwitch(tok string) (name string, ok bool) {
	tok = unquote(tok)
	if tok == "" || (tok[0] != '-' && tok[0] != '/') {
		return "", false
	}
	name = strings.ToLower(strings.TrimPrefix(tok[1:], "-"))
	return name, true
}

// switchKind is how a PowerShell switch bears on finding the encoded command.
type switchKind int

const (
	switchFlag           switchKind = iota // takes no value; skip it
	switchTakesValue                       // takes the next token as its value
	switchEndsOptions                      // -File, -Command, a lone dash: the rest is the script's
	switchEncodedCommand                   // the next token is the encoded command
)

// classifySwitch names what PowerShell does with a switch, by the switch names
// and shortest abbreviations its parser accepts. A name that matches nothing is
// a flag: it takes no value and the walk continues.
func classifySwitch(name string) switchKind {
	switch {
	case name == "":
		return switchEndsOptions
	case name == "ec", abbreviates(name, "encodedcommand", "e"):
		return switchEncodedCommand
	case abbreviates(name, "file", "f"), abbreviates(name, "command", "c"):
		return switchEndsOptions
	}
	for _, opt := range valueSwitches {
		if abbreviates(name, opt.full, opt.min) {
			return switchTakesValue
		}
	}
	return switchFlag
}

// valueSwitches are PowerShell's host options that take a value, with the
// shortest abbreviation PowerShell accepts for each, so the value is stepped
// over rather than read as the end of the options.
var valueSwitches = []struct{ full, min string }{
	{"executionpolicy", "ex"},
	{"windowstyle", "w"},
	{"inputformat", "inp"},
	{"outputformat", "o"},
	{"version", "v"},
	{"psconsolefile", "psc"},
	{"configurationname", "config"},
	{"configurationfile", "configurationfile"},
	{"workingdirectory", "wd"},
	{"settingsfile", "settings"},
	{"custompipename", "cus"},
	{"encodedarguments", "encodeda"},
}

// abbreviates reports whether name is a prefix of full no shorter than min,
// which is how PowerShell matches a switch.
func abbreviates(name, full, min string) bool {
	return len(name) >= len(min) && len(name) <= len(full) && full[:len(name)] == name
}

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
	text := strings.TrimPrefix(string(runes), "\ufeff")
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

// decodeUTF8 reads a base64 token as UTF-8 text: valid, printable, non-empty.
func decodeUTF8(tok string) (string, bool) {
	raw, ok := base64Bytes(tok)
	if !ok || len(raw) == 0 || !utf8.Valid(raw) {
		return "", false
	}
	text := string(raw)
	if !printable(text) {
		return "", false
	}
	return text, true
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

// unquote strips the quote characters around a token, matched or not: the
// blob is often the last word inside a quoted sub-command, carrying only the
// closing quote.
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
