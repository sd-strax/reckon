package catalog

import (
	"strings"
	"testing"
)

func TestValidToolName(t *testing.T) {
	for _, name := range []string{"a", "_x", "request_host_isolate", "get-host", "A9", strings.Repeat("a", 64)} {
		if !ValidToolName(name) {
			t.Errorf("ValidToolName(%q) = false; want true", name)
		}
	}
	for _, name := range []string{"", "9a", "-a", "host.isolate", "a:b", "a b", "é", strings.Repeat("a", 65)} {
		if ValidToolName(name) {
			t.Errorf("ValidToolName(%q) = true; want false", name)
		}
	}
}

func TestActionToolName(t *testing.T) {
	if got := ActionToolName("ticket.create"); got != "request_ticket_create" {
		t.Errorf("ActionToolName(ticket.create) = %q", got)
	}
}

// TestValidateActionDescriptor: each refusal names the type and the cause,
// and an action type of 56 characters is the longest that passes.
func TestValidateActionDescriptor(t *testing.T) {
	if err := ValidateActionDescriptor("host.isolate", []string{"host", "duration"}); err != nil {
		t.Errorf("valid descriptor refused: %v", err)
	}
	if err := ValidateActionDescriptor(strings.Repeat("a", 56), nil); err != nil {
		t.Errorf("56-character type refused: %v", err)
	}
	for _, c := range []struct {
		actionType string
		inputs     []string
		want       string
	}{
		{"", nil, "an action type is required"},
		{strings.Repeat("a", 57), nil, "not a valid tool name"},
		{"host:isolate", nil, "not a valid tool name"},
		{"x.act", []string{RequestFieldRationale}, `input "rationale" takes the name of a field every action request carries`},
		{"x.act", []string{RequestFieldTargets}, `input "targets" takes the name`},
		{"x.act", []string{"summary", "summary"}, `input "summary" is declared twice`},
	} {
		err := ValidateActionDescriptor(c.actionType, c.inputs)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), c.actionType) {
			t.Errorf("ValidateActionDescriptor(%q, %v) = %v; want an error naming the type and %q", c.actionType, c.inputs, err, c.want)
		}
	}
}

func TestValidateVerb(t *testing.T) {
	if err := ValidateVerb("get_host_context"); err != nil {
		t.Errorf("valid verb refused: %v", err)
	}
	for verb, want := range map[string]string{
		"get.host":     "not a valid tool name",
		"":             "not a valid tool name",
		ToolReadSOP:    "built-in tools",
		ToolReadResult: "built-in tools",
	} {
		if err := ValidateVerb(verb); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateVerb(%q) = %v; want %q", verb, err, want)
		}
	}
}

func TestBuiltinToolNames_AreValidAndDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, n := range BuiltinToolNames() {
		if !ValidToolName(n) || seen[n] {
			t.Errorf("built-in tool name %q is invalid or repeated", n)
		}
		seen[n] = true
		if !IsBuiltinToolName(n) {
			t.Errorf("IsBuiltinToolName(%q) = false", n)
		}
	}
}
