package server

import (
	"strings"
	"testing"

	"github.com/sd-strax/reckon/capability/ocsfview"
)

// TestDecodedSuffix: an export line names the encoding and carries the decoded
// text on one line, clipped (adr/0300); a view without one adds nothing.
func TestDecodedSuffix(t *testing.T) {
	if got := decodedSuffix(ocsfview.View{}); got != "" {
		t.Errorf("no decoded = %q; want nothing", got)
	}
	v := ocsfview.View{Decoded: &ocsfview.Decoded{Field: "process.cmd_line", Encoding: ocsfview.EncodingBase64UTF16LE, Text: "IEX (New-Object\n Net.WebClient)"}}
	if got := decodedSuffix(v); got != " · decoded (base64-utf16le): IEX (New-Object Net.WebClient)" {
		t.Errorf("suffix = %q", got)
	}
	// Decoded text is the attacker's and lands in a code span: a backtick
	// would close it.
	v.Decoded.Text = "echo `whoami`; <img src=x>"
	if got := decodedSuffix(v); strings.Contains(got, "`") || !strings.Contains(got, "echo 'whoami'; <img src=x>") {
		t.Errorf("backticks survive into the code span: %q", got)
	}
	v.Decoded.Text = strings.Repeat("a", 500)
	if got := decodedSuffix(v); len([]rune(got)) != len([]rune(" · decoded (base64-utf16le): "))+exportDecodedMax || !strings.HasSuffix(got, "…") {
		t.Errorf("long text not clipped to %d runes with an ellipsis: %d", exportDecodedMax, len([]rune(got)))
	}
}
