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

// TestParseTranscript_ToolFrameIDs: both tool frames yield the call's id, so a
// call pairs with its result by id rather than by position.
func TestParseTranscript_ToolFrameIDs(t *testing.T) {
	body := "[tool_use list_actions id=tu_7] {}\n[tool_result list_actions id=tu_7 error=true] boom\n"
	evs := parseTranscript(body)
	if len(evs) != 2 {
		t.Fatalf("events = %+v; want two", evs)
	}
	if evs[0].Kind != "tool_use" || evs[0].ID != "tu_7" || evs[0].Tool != "list_actions" {
		t.Errorf("tool_use = %+v; want list_actions id tu_7", evs[0])
	}
	if evs[1].Kind != "tool_result" || evs[1].ID != "tu_7" || !evs[1].IsError || evs[1].Content != "boom" {
		t.Errorf("tool_result = %+v; want id tu_7, error, content boom", evs[1])
	}
}
