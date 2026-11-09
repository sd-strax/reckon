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
// PowerShell's own command-line grammar puts it: PowerShell in command
// position (the program of the line, or of a command a shell on the line
// launches), then its host options read as its parser reads them, up to the
// first token that ends them, after which every word belongs to the script.
// The encoded-command switch there names a base64 token of UTF-16LE. A flagged
// token that is not UTF-16LE is not a PowerShell command and gets no second
// guess. Then each other bare word long enough to be meant as a blob is tried
// as base64 of UTF-8. The first clean decode wins.
func decodeCommandLine(cmd string) (encoding, text string, ok bool) {
	flagged := powerShellEncodedArguments(cmd)
	for _, tok := range flagged {
		if text, ok := decodeUTF16LE(unquote(tok)); ok {
			return EncodingBase64UTF16LE, text, true
		}
	}
	for _, tok := range strings.Fields(cmd) {
		tok = unquote(tok)
		if len(tok) < minBase64Token || isFlagged(tok, flagged) {
			continue
		}
		if text, ok := decodeUTF8(tok); ok {
			return EncodingBase64, text, true
		}
	}
	return "", "", false
}

// isFlagged reports whether a whitespace-split word is one PowerShell was
// given as an encoded command, which gets no second guess. The two readings
// split differently (a quoted word is one token, a word may carry a stray
// quote), so a word is flagged when either contains the other. A flagged
// token too short to be a blob flags nothing: it is not one, and the words
// that happen to contain it are not it.
func isFlagged(word string, flagged []string) bool {
	for _, f := range flagged {
		if len(f) >= minBase64Token && (strings.Contains(f, word) || strings.Contains(word, f)) {
			return true
		}
	}
	return false
}

// --- tokens ---------------------------------------------------------------------

// shell is whose rules a command line is read under: the shell decides where
// one command ends and the next begins, and how the words of each are quoted.
type shell int

const (
	windowsShell shell = iota // cmd's rules for operators, argv's for words
	posixShell                // sh's rules for both
)

// shellOf is the shell whose rules a whole command line is read under: POSIX
// when its program is an sh-family shell, a POSIX wrapper such as sudo, or
// lives under a POSIX path; Windows otherwise.
func shellOf(line string) shell {
	f := strings.Fields(line)
	if len(f) == 0 {
		return windowsShell
	}
	if strings.HasPrefix(strings.TrimLeft(f[0], `'"`), "/") {
		return posixShell // a POSIX path, whatever it contains
	}
	if strings.Contains(f[0], `\`) || (len(f[0]) > 1 && f[0][1] == ':') {
		return windowsShell // a Windows path, whatever program it names
	}
	// The wrapper check wants the words of the first command as the cut
	// command carries them, glued operators ending them too.
	var rest []token
	if cmds := splitCommands(line, windowsShell); len(cmds) > 0 {
		if words := strings.Fields(cmds[0].text); len(words) > 1 {
			for _, w := range words[1:] {
				rest = append(rest, token{text: w})
			}
		}
	}
	if name := programName(f[0]); isShFamily(name) || isWrapper(name, rest) {
		return posixShell
	}
	return windowsShell
}

// token is one word of a command as the program receives it: quotes removed,
// whether any of it was quoted (a quoted word is one argument however many
// spaces it holds; a shell reads it as neither an operator nor one of its own
// switches, while a program's argv has no quotes left to see), and where it
// starts in the command, so a shell that takes the rest of the command can be
// handed exactly that.
type token struct {
	text   string
	quoted bool
	start  int // byte offset in the command tokenize was given
	plain  int // bytes of text, from its start, that were neither quoted nor escaped
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
// word), except a line break, which it joins: both characters vanish (shell
// command language §2.2.1); inside a single-quoted run everything is text
// (§2.2.2), and in a double-quoted one a backslash escapes only a quote, a
// backslash, a dollar, a backquote or a line break and stays before anything
// else, an apostrophe included (§2.2.3).
func tokenize(cmd string, posix bool) []token {
	var out []token
	var b strings.Builder
	inWord, quoted := false, false
	start, plain := 0, -1
	var quote byte
	flush := func() {
		if inWord {
			if plain < 0 {
				plain = b.Len()
			}
			out = append(out, token{text: b.String(), quoted: quoted, start: start, plain: plain})
		}
		b.Reset()
		inWord, quoted, plain = false, false, -1
	}
	begin := func(i int) {
		if !inWord {
			start, inWord = i, true
		}
	}
	// marked notes where quoting or escaping first changed the word's text.
	marked := func() {
		if plain < 0 {
			plain = b.Len()
		}
	}
	isQuote := func(c byte) bool { return c == '"' || (posix && c == '\'') }
	closing := findLastQuotes(cmd, posix)
	// escapable is whether a backslash before this quote character counts:
	// never inside a POSIX double-quoted run for an apostrophe, which sh keeps
	// with its backslash.
	escapable := func(c byte) bool { return isQuote(c) && (!posix || c != '\'' || quote != '"') }
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case posix && c == '\\' && quote != '\'' && i+1 < len(cmd) && cmd[i+1] == '\n':
			i++ // sh: a line continuation; both characters vanish
		case posix && quote != '\'' && (c == '`' || strings.HasPrefix(cmd[i:], "$(")):
			// A command substitution or arithmetic expansion is part of the
			// word, the spaces and quotes inside it included (§2.3).
			end := substitutionClose(cmd, i)
			begin(i)
			b.WriteString(cmd[i:end])
			i = end - 1
		case posix && c == '\\' && i+1 < len(cmd) && (quote == 0 || (quote == '"' && strings.IndexByte("\"\\$`", cmd[i+1]) >= 0)):
			begin(i)
			marked()
			b.WriteByte(cmd[i+1]) // sh: the backslash makes the next character text
			i++
		case c == '\\' && quote != '\'':
			n := 1
			for i+n < len(cmd) && cmd[i+n] == '\\' {
				n++
			}
			begin(i)
			if i+n < len(cmd) && escapable(cmd[i+n]) {
				marked()
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
			begin(i)
			if !closing.ahead(i, c) {
				b.WriteByte(c) // no match ahead: text
			} else {
				marked()
				quoted, quote = true, c
			}
		default:
			begin(i)
			b.WriteByte(c)
		}
	}
	flush()
	return out
}

// lastQuotes are the positions of the last quote characters in a command
// that can close a run: for a double quote, one preceded by an even number
// of backslashes, since an odd number makes it a literal character that
// closes nothing; for an apostrophe under POSIX rules, any, since nothing
// inside single quotes escapes. Found in one pass, so a quote-heavy line is
// read in linear time.
type lastQuotes struct{ double, single int }

func findLastQuotes(cmd string, posix bool) lastQuotes {
	l := lastQuotes{double: -1, single: -1}
	for i := 0; i < len(cmd); i++ {
		switch cmd[i] {
		case '"':
			n := 0
			for j := i - 1; j >= 0 && cmd[j] == '\\'; j-- {
				n++
			}
			if n%2 == 0 {
				l.double = i
			}
		case '\'':
			if posix {
				l.single = i
			}
		}
	}
	return l
}

// ahead reports whether a quote character q that can close a run lies after
// index i.
func (l lastQuotes) ahead(i int, q byte) bool {
	if q == '"' {
		return l.double > i
	}
	return l.single > i
}

// splitCommands cuts a command line into its commands at the shell's
// operators, glued to words or not, outside the shell's own quoting. cmd cuts
// at &&, ||, & and |, at the parentheses of a group opening a command,
// toggles a quoted region at every double quote and escapes with a caret; an
// & that completes a redirection (2>&1) is no operator for either shell. sh
// cuts at those operators, at a semicolon, at a line break, at
// a parenthesis, an operator of its own whose subshell runs what it holds; a
// command substitution, backquoted or $( ), inside double quotes as well as
// outside, yields its own commands while staying a word of the command that
// holds it (shell command language §2.2.3, §2.3, §2.6.3, §2.9.3); it quotes with either character,
// escapes with a backslash (a backslash before a line break joins the lines),
// an unquoted # where a word would begin starts a comment that runs to the
// end of the line, and the lines after a << operator's line, up to its
// delimiter, are a here-document's body and no command (§2.7.4). Each command
// comes back with its byte offset in the line.
func splitCommands(line string, sh shell) []command {
	var out []command
	start := 0
	var quote byte
	posix := sh == posixShell
	inWord := false        // whether the previous character continues a word
	afterRedirect := false // whether the previous character was an unquoted < or >
	groups := 0            // cmd's open parenthesised groups
	var heredocs []heredoc // announced on the current line, bodies to follow
	cut := func(end, next int) {
		if c := strings.TrimSpace(line[start:end]); c != "" {
			out = append(out, command{text: c, start: start + strings.Index(line[start:end], c)})
		}
		start = next
		inWord = false
	}
	substitutes := func(i int) bool { // $( or a backquote, which double quotes do not quote
		return posix && (line[i] == '`' || (line[i] == '$' && i+1 < len(line) && line[i+1] == '(' && !strings.HasPrefix(line[i:], "$((")))
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		wasRedirect := afterRedirect
		afterRedirect = false
		switch {
		case posix && c == '\\' && quote != '\'' && i+1 < len(line) && line[i+1] == '\n':
			i++ // a line continuation vanishes; the word state is the previous character's
		case quote != 0 && (quote != '"' || !substitutes(i)):
			switch {
			case posix && quote == '"' && c == '\\':
				i++ // sh: escapes the next character inside double quotes too
			case c == quote:
				quote = 0
			}
		case sh == windowsShell && c == '^', posix && c == '\\':
			i++ // escapes the next character
			inWord = true
		case c == '"', posix && c == '\'':
			quote = c
			inWord = true
		case posix && c == '#' && !inWord:
			end := strings.IndexByte(line[i:], '\n')
			if end < 0 {
				end = len(line) - i
			}
			cut(i, i+end) // the comment runs to the line break, which then cuts
			i += end - 1
		case substitutes(i):
			// The substitution's text is commands of its own, read first as
			// the shell runs them first; the enclosing command keeps the
			// substitution as part of its word.
			inner, end := substitutionBounds(line, i)
			for _, sub := range splitCommands(line[inner:end], sh) {
				sub.start += inner
				out = append(out, sub)
			}
			i = end
			inWord = true
		case posix && strings.HasPrefix(line[i:], "$(("):
			i = arithmeticEnd(line, i) - 1 // an arithmetic expansion is a word; its << is a shift
			inWord = true
		case posix && (c == '(' || c == ')'):
			cut(i, i+1) // a subshell
		case sh == windowsShell && c == '(' && opensGroup(line[start:i]):
			cut(i, i+1) // cmd groups commands in parentheses that open a command
			groups++
		case sh == windowsShell && c == ')' && groups > 0:
			cut(i, i+1)
			groups--
		case posix && strings.HasPrefix(line[i:], "<<<"):
			i += 2 // a here-string: its word is data
			inWord = true
		case posix && strings.HasPrefix(line[i:], "<<"):
			var h heredoc
			h, i = readHeredocOperator(line, i)
			heredocs = append(heredocs, h)
			inWord = true
		case c == '&' && wasRedirect:
			inWord = true // a redirection's & (2>&1, <&0), no operator
		case c == '&' || c == '|':
			if i+1 < len(line) && line[i+1] == c {
				cut(i, i+2)
				i++
			} else {
				cut(i, i+1)
			}
		case posix && c == '\n':
			next := i + 1
			if len(heredocs) > 0 {
				next = skipHeredocBodies(line, next, heredocs)
				heredocs = nil
			}
			cut(i, next)
			i = next - 1
		case posix && c == ';':
			cut(i, i+1)
		case c == ' ' || c == '\t':
			inWord = false
		default:
			inWord = true
			afterRedirect = c == '<' || c == '>'
		}
	}
	cut(len(line), len(line))
	return out
}

// substitutionBounds is the text of the command substitution opening at i, a
// backquote or $(: the index its commands begin at and the index of the
// closing backquote or parenthesis, or the end of the line when none closes
// it. Quotes and backslashes inside are honoured when matching.
func substitutionBounds(line string, i int) (inner, end int) {
	if line[i] == '`' {
		for j := i + 1; j < len(line); j++ {
			switch line[j] {
			case '\\':
				j++
			case '`':
				return i + 1, j
			}
		}
		return i + 1, len(line)
	}
	depth := 1
	var quote byte
	for j := i + 2; j < len(line); j++ {
		c := line[j]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				j++
			} else if c == quote {
				quote = 0
			}
		case c == '\\':
			j++
		case c == '\'' || c == '"':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i + 2, j
			}
		}
	}
	return i + 2, len(line)
}

// substitutionClose is the index just past the command substitution or
// arithmetic expansion opening at i, or the end of the line when nothing
// closes it.
func substitutionClose(cmd string, i int) int {
	if strings.HasPrefix(cmd[i:], "$((") {
		return arithmeticEnd(cmd, i)
	}
	_, end := substitutionBounds(cmd, i)
	return min(end+1, len(cmd))
}

// arithmeticEnd is the index just past the )) closing the $(( at i, or the
// end of the line when none closes it.
func arithmeticEnd(line string, i int) int {
	depth := 0
	for j := i + 1; j < len(line); j++ {
		switch line[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j + 1
			}
		}
	}
	return len(line)
}

// opensGroup reports whether a parenthesis after the given text of a command
// opens a cmd group: at the command's start, or at the start of the string
// cmd's /c, /k or /r runs, which is a command line of its own; anywhere else
// a parenthesis is text, as in an install path.
func opensGroup(before string) bool {
	f := strings.Fields(before)
	if len(f) == 0 {
		return true
	}
	switch strings.ToLower(f[len(f)-1]) {
	case "/c", "/k", "/r":
		return true
	}
	return false
}

// heredoc is a here-document announced by << on a line: its delimiter, with
// any quoting removed, and whether <<- strips leading tabs from the body.
type heredoc struct {
	delimiter string
	stripTabs bool
}

// readHeredocOperator reads << or <<- at i and the delimiter word after it;
// it returns the here-document and the index of the word's last byte.
func readHeredocOperator(line string, i int) (heredoc, int) {
	j := i + 2
	h := heredoc{stripTabs: j < len(line) && line[j] == '-'}
	if h.stripTabs {
		j++
	}
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	k := j
	for k < len(line) && strings.IndexByte(" \t\n;&|()<>", line[k]) < 0 {
		k++
	}
	h.delimiter = strings.NewReplacer(`"`, "", `'`, "", `\`, "").Replace(line[j:k])
	return h, k - 1
}

// skipHeredocBodies returns the index just past the bodies of the given
// here-documents, which begin at from, each ending at the first line equal to
// its delimiter; a body no delimiter ends runs to the end of the input.
func skipHeredocBodies(line string, from int, docs []heredoc) int {
	for _, h := range docs {
		for from < len(line) {
			end := strings.IndexByte(line[from:], '\n')
			if end < 0 {
				end = len(line) - from
			}
			body := line[from : from+end]
			if h.stripTabs {
				body = strings.TrimLeft(body, "\t")
			}
			from = min(from+end+1, len(line))
			if body == h.delimiter {
				break
			}
		}
	}
	return from
}

// command is one command of a line: its text and where it starts in the line.
type command struct {
	text  string
	start int
}

// --- command position -----------------------------------------------------------

// maxNesting bounds how many shells deep the search for PowerShell goes.
const maxNesting = 8

// powerShellEncodedArguments are the tokens PowerShell would read as encoded
// commands on the line: one per PowerShell in command position whose host
// options carry the switch, in order.
func powerShellEncodedArguments(line string) []string {
	var out []string
	for _, inv := range powerShellInvocations(line, shellOf(line), 0) {
		if tok, ok := encodedArgument(inv); ok {
			out = append(out, tok)
		}
	}
	return out
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

// powerShellInvocations finds every PowerShell in command position on a
// command line, each as its words from the program on. The line is cut into
// commands at the shell's operators; the first word of each is its program.
// PowerShell is in command position when it is that program, or when that
// program is a shell this view knows and PowerShell is in command position on
// the command line the shell launches. PowerShell anywhere else is a word on
// some other program's command line.
func powerShellInvocations(line string, sh shell, depth int) [][]token {
	if depth > maxNesting {
		return nil
	}
	var out [][]token
	posix := sh == posixShell
	for _, c := range splitCommands(line, sh) {
		tokens := tokenize(c.text, posix)
		if posix {
			tokens = afterCommandPrefix(tokens)
		}
		out = append(out, programInvocations(c.text, tokens, depth)...)
	}
	return out
}

// commandOpeners are sh's reserved words that a command follows, so the word
// after one of them is that command's program (shell command language §2.4);
// the closing words (done, fi, esac, }) and case, for and in, which a name
// follows, are not among them. With them, the utilities that run the command
// named by their next word: exec, command and time.
var commandOpeners = set("if", "then", "elif", "else", "while", "until", "do", "{", "!", "exec", "command", "time")

// utilityOptions are, for exec, command and time, the options that take a
// value; the three run the command after their options (exec -a name, time
// -f format).
var utilityOptions = map[string]map[string]bool{"exec": set("-a"), "command": {}, "time": set("-f", "-o")}

// afterCommandPrefix drops what sh reads before a command's program: the
// reserved words a command follows, written without quoting or escaping
// (a quoted or escaped one is a program name), the NAME=value assignments
// and the redirections that precede a simple command (§2.9.1), and exec,
// command and time, which run the command that follows their options.
// command -v and -V describe a command and run nothing.
func afterCommandPrefix(tokens []token) []token {
	for len(tokens) > 1 {
		t := tokens[0]
		switch {
		case !t.quoted && t.plain == len(t.text) && commandOpeners[t.text]:
			if t.text == "command" && (tokens[1].text == "-v" || tokens[1].text == "-V") {
				return tokens // describes the command and runs nothing
			}
			valued, utility := utilityOptions[t.text]
			tokens = tokens[1:]
			for utility && len(tokens) > 1 && len(tokens[0].text) > 1 && strings.HasPrefix(tokens[0].text, "-") {
				if takesValue(tokens[0].text, valued) {
					tokens = tokens[1:]
				}
				tokens = tokens[1:]
			}
			continue
		case isAssignmentWord(t):
		case t.plain > 0 && isRedirection(t.text):
			if redirectionTarget(t.text) == "" { // the operator alone: the next word is its target
				if len(tokens) < 3 {
					return tokens
				}
				tokens = tokens[1:]
			}
		default:
			return tokens
		}
		tokens = tokens[1:]
	}
	return tokens
}

// isRedirection reports whether a word begins with a redirection operator,
// optionally after a file descriptor number: <, >, >>, <<, <<-, <&, >&, <>
// or >| (§2.7).
func isRedirection(word string) bool {
	return redirectionOperator(word) != ""
}

// redirectionOperator is the redirection operator a word begins with, after
// any file descriptor digits, or empty.
func redirectionOperator(word string) string {
	rest := strings.TrimLeft(word, "0123456789")
	for _, op := range []string{"<<-", "<<", "<&", ">&", "<>", ">>", ">|", "<", ">"} {
		if strings.HasPrefix(rest, op) {
			return op
		}
	}
	return ""
}

// redirectionTarget is what follows the redirection operator in a word.
func redirectionTarget(word string) string {
	rest := strings.TrimLeft(word, "0123456789")
	return strings.TrimPrefix(rest, redirectionOperator(word))
}

// isAssignmentWord reports whether a word is NAME=value with the name and the
// equals sign written plainly (§2.10.2 rule 7): a name of letters, digits
// and underscores, not starting with a digit, then an equals sign.
func isAssignmentWord(t token) bool {
	word := t.text
	eq := strings.IndexByte(word, '=')
	if eq <= 0 || eq >= t.plain {
		return false
	}
	for i, r := range word[:eq] {
		letter := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		digit := i > 0 && r >= '0' && r <= '9'
		if !letter && !digit {
			return false
		}
	}
	return true
}

// programInvocations reads one command (a program and its words) and returns
// PowerShell's invocations: the command itself when the program is
// PowerShell, or those in command position on the command line a known shell
// launches: cmd's /c, /k or /r string under cmd's quote rule, start's program
// after its switches and title, call's command, a wrapper's command after its
// options, an sh-family -c string.
func programInvocations(cmd string, tokens []token, depth int) [][]token {
	if len(tokens) == 0 {
		return nil
	}
	rest := tokens[1:]
	switch name := programName(tokens[0].text); {
	case isPowerShellName(name):
		return [][]token{tokens}
	case name == "cmd":
		// Only /c, /k or /r make cmd run the string that follows; with other
		// switches alone it opens a shell and the rest is nothing.
		runs := false
		for len(rest) > 0 && isCmdSwitch(rest[0]) {
			if l := strings.ToLower(rest[0].text); l == "/c" || l == "/k" || l == "/r" {
				runs = true
			}
			rest = rest[1:]
		}
		if !runs || len(rest) == 0 {
			return nil
		}
		for _, cl := range cmdCommandLines(cmd[rest[0].start:]) {
			if out := powerShellInvocations(cl, windowsShell, depth+1); out != nil {
				return out
			}
		}
		return nil
	case name == "start":
		// start's switches, then its first quoted word as the window title
		// (a quoted program path on its own opens a window so titled and
		// runs nothing, cmd's well-known trap), then the program.
		titled := false
		for len(rest) > 0 {
			switch {
			case isCmdSwitch(rest[0]):
				if l := strings.ToLower(rest[0].text); (l == "/d" || l == "/node" || l == "/affinity") && len(rest) > 1 {
					rest = rest[1:]
				}
				rest = rest[1:]
			case rest[0].quoted && !titled:
				rest, titled = rest[1:], true
			default:
				return powerShellInvocations(cmd[rest[0].start:], windowsShell, depth+1)
			}
		}
		return nil
	case name == "call":
		if len(rest) == 0 {
			return nil
		}
		return powerShellInvocations(cmd[rest[0].start:], windowsShell, depth+1)
	case isWrapper(name, rest):
		if name == "env" {
			if line, ok := splitStringCommand(cmd, rest); ok {
				return powerShellInvocations(line, posixShell, depth+1)
			}
		}
		if i := wrappedCommand(name, rest); i >= 0 {
			return powerShellInvocations(cmd[rest[i].start:], posixShell, depth+1)
		}
		return nil
	case isShFamily(name):
		// Options first: -o, -O, +O, --rcfile and --init-file take a value;
		// short options may be clustered (-lc) and -- ends them. When a
		// cluster carried c, the first word that is no option is the command
		// string, that word alone: the shell runs that one argv element as a
		// command line of its own and the words after it are its positional
		// parameters. The shell sees argv without quotes, so quoting decides
		// nothing here.
		commandString := false
		for i := 0; i < len(rest); i++ {
			t := rest[i]
			switch {
			case t.text == "--":
				if commandString && i+1 < len(rest) {
					return powerShellInvocations(rest[i+1].text, posixShell, depth+1)
				}
				return nil
			case !strings.HasPrefix(t.text, "-") && !strings.HasPrefix(t.text, "+"):
				if commandString {
					return powerShellInvocations(t.text, posixShell, depth+1)
				}
				return nil // a script file
			default:
				if !strings.HasPrefix(t.text, "--") && strings.ContainsRune(t.text, 'c') {
					commandString = true // a cluster carrying c (-c, -lc, -co pipefail)
				}
				if takesValue(t.text, shValued) {
					i++
				}
			}
		}
		return nil
	case isPathStart(tokens[0]) && !isProgramFile(tokens[0].text):
		// No program the view knows: an unquoted program path with spaces, as
		// a Run key or a task often records it. CreateProcess tries each
		// space-bounded prefix as the program; the view takes the first whose
		// last word names PowerShell or another program file, which is then
		// the program, read with the words after it as its command line.
		for j := 1; j < len(tokens) && !tokens[j].quoted && !strings.HasPrefix(tokens[j].text, "-") && !strings.HasPrefix(tokens[j].text, "/"); j++ {
			name := programName(tokens[j].text)
			if isPowerShellName(name) {
				return [][]token{tokens[j:]}
			}
			if isProgramFile(tokens[j].text) {
				return programInvocations(cmd, tokens[j:], depth+1)
			}
		}
		return nil
	}
	return nil
}

// cmdCommandLines is how cmd reads the string after /c or /k when it starts
// with a quote: either the first and last quotes are stripped and the rest is
// the command line, or the quotes are kept because the quoted string is the
// path of the program itself. cmd decides by looking the file up; the view
// cannot, so it returns both readings, the stripped one first, and the caller
// takes whichever yields PowerShell in command position.
func cmdCommandLines(rest string) []string {
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, `"`) || strings.Count(rest, `"`) < 2 {
		return []string{rest}
	}
	stripped := rest[1:]
	if i := strings.LastIndex(stripped, `"`); i >= 0 {
		stripped = stripped[:i] + stripped[i+1:]
	}
	return []string{stripped, rest}
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

func isCmdSwitch(t token) bool {
	return !t.quoted && strings.HasPrefix(t.text, "/")
}

// Wrappers run the command that follows their own options: sudo, env, nohup,
// timeout and nice.
func isWrapper(name string, rest []token) bool {
	switch name {
	case "sudo", "env", "nohup", "nice":
		return true
	case "timeout":
		// Also a cmd builtin (timeout /t 5, timeout 5), which runs nothing;
		// the wrapper has a duration and then a command.
		return len(rest) > 1 && !strings.HasPrefix(rest[0].text, "/")
	}
	return false
}

// wrapperValued are the wrapper options that take the next word as a value.
var wrapperValued = map[string]map[string]bool{
	"sudo":    set("-u", "--user", "-g", "--group", "-C", "--close-from", "-D", "--chdir", "-h", "--host", "-p", "--prompt", "-U", "--other-user", "-r", "--role", "-t", "--type", "-T", "--command-timeout"),
	"env":     set("-u", "--unset", "-C", "--chdir", "-S", "--split-string"),
	"timeout": set("-k", "--kill-after", "-s", "--signal"),
	"nice":    set("-n", "--adjustment"),
}

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// wrappedCommand is the index in rest of the command a wrapper runs, after
// its options (a valued option takes the next word unless written as
// --name=value), the VAR=value assignments sudo and env accept, and
// timeout's duration; -1 when nothing follows. The wrapper sees argv without
// quotes, so each word is classified by its text alone: a quoted assignment
// or duration is still an assignment or a duration.
func wrappedCommand(name string, rest []token) int {
	valued := wrapperValued[name]
	assignments := name == "sudo" || name == "env"
	positional := 0
	if name == "timeout" {
		positional = 1
	}
	for i := 0; i < len(rest); i++ {
		t := rest[i]
		switch {
		case len(t.text) > 1 && strings.HasPrefix(t.text, "-"):
			if takesValue(t.text, valued) {
				i++
			}
		case assignments && strings.Contains(t.text, "="):
		case positional > 0:
			positional--
		default:
			return i
		}
	}
	return -1
}

// splitStringCommand is the command line env -S (--split-string) runs: the
// option's value, which env splits into words, followed by the words after
// it; false when env has no such option before its command.
func splitStringCommand(cmd string, rest []token) (string, bool) {
	for i := 0; i < len(rest); i++ {
		t := rest[i]
		var value string
		switch {
		case t.text == "-S" || t.text == "--split-string":
			if i+1 >= len(rest) {
				return "", false
			}
			i++
			value = rest[i].text
		case strings.HasPrefix(t.text, "--split-string="):
			value = strings.TrimPrefix(t.text, "--split-string=")
		case strings.HasPrefix(t.text, "-S"):
			value = t.text[2:]
		case strings.HasPrefix(t.text, "-") && !strings.HasPrefix(t.text, "--") && strings.HasSuffix(t.text, "S") && len(t.text) > 2:
			if i+1 >= len(rest) { // a cluster ending in S (env -iS): the next word is the string
				return "", false
			}
			i++
			value = rest[i].text
		case len(t.text) > 1 && strings.HasPrefix(t.text, "-"):
			if takesValue(t.text, wrapperValued["env"]) {
				i++
			}
			continue
		case strings.Contains(t.text, "="):
			continue
		default:
			return "", false // the command comes first
		}
		if i+1 < len(rest) {
			value += " " + cmd[rest[i+1].start:]
		}
		return value, true
	}
	return "", false
}

// takesValue reports whether an option word takes the next word as its
// value: a valued long option without =, a valued short option, or a cluster
// of short options whose last letter is valued (sudo -Eu root).
func takesValue(word string, valued map[string]bool) bool {
	if strings.Contains(word, "=") {
		return false
	}
	if valued[word] {
		return true
	}
	if strings.HasPrefix(word, "--") || len(word) < 3 {
		return false
	}
	for i := 1; i < len(word); i++ {
		if valued["-"+word[i:i+1]] {
			// The first valued letter of a cluster takes the rest of the word
			// as its value (sudo -uroot); only as the last letter does it take
			// the next word (sudo -Eu root).
			return i == len(word)-1
		}
	}
	return false
}

// shValued are the sh-family options that take the next word as a value.
var shValued = set("-o", "-O", "+O", "--rcfile", "--init-file")

func isShFamily(name string) bool {
	switch name {
	case "sh", "bash", "zsh", "dash", "ksh":
		return true
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

// decodeUTF8 reads a base64 token as UTF-8 text: valid, printable, non-empty.
func decodeUTF8(tok string) (string, bool) {
	raw, ok := base64Bytes(tok)
	if !ok || len(raw) == 0 || !utf8.Valid(raw) {
		return "", false
	}
	text := strings.TrimPrefix(string(raw), string(rune(byteOrderMark)))
	if text == "" || !printable(text) {
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

// unquote strips stray quote characters around a word, matched or not: the
// bare pass reads whitespace-split words, where a blob often carries the
// closing quote of the sub-command it ends.
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
