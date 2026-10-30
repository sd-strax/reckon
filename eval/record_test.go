package eval

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestParseTranscript_DecodesContent: content comes back as written, so a tool
// result whose JSON holds escaped quotes and backslashes still parses
// (adr/0235). The line below is how the session escapes it.
func TestParseTranscript_DecodesContent(t *testing.T) {
	result := `{"cmd":"say \"hi\"","path":"C:\\new\\data"}`
	escaped := strings.ReplaceAll(result, `\`, `\\`)
	body := "[user] go\n[tool_result run id=t1 error=false] " + escaped + "\n"
	evs := parseTranscript(body)
	if len(evs) != 2 || evs[1].Content != result {
		t.Fatalf("events = %+v; want the result as written", evs)
	}
	var v map[string]string
	if err := json.Unmarshal([]byte(evs[1].Content), &v); err != nil || v["path"] != `C:\new\data` {
		t.Errorf("decoded result does not parse back: %v %v", v, err)
	}
}
