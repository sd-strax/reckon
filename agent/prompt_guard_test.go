package agent

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fixtureRoot is the bundled scenario data, read from the repo checkout: the
// agent package's tests run with the package directory as cwd.
const fixtureRoot = "../fixtures"

// identifierKeys are the OCSF/fixture fields whose string values name a thing
// in a scenario — a host, an account, an address, a process, a finding, a
// queue — rather than vocabulary the engine owns (class names, statuses,
// protocols). A value under one of these keys, or in a `types` list (MITRE
// ids), is scenario vocabulary the prompt must not contain.
var identifierKeys = map[string]bool{
	"hostname": true, "name": true, "uid": true, "ip": true, "domain": true,
	"title": true, "desc": true, "message": true, "cmd_line": true, "path": true,
	"email": true, "url": true, "subject": true, "link": true, "channel": true,
	"resolved_identifier": true, "assignment_group": true, "queue": true,
	"vendor_name": true, "types": true,
}

// minIdentifierLen keeps two- and three-character values ("OK", "tcp") out of
// the denylist; nothing that short is a scenario identifier.
const minIdentifierLen = 4

// promptGuardExemptions are the scenario identifiers the prompt and tool
// descriptions contain TODAY. They are listed, dated, and removed in the prompt
// revision (#60); the guard refuses any identifier not on this list, so the set
// can only shrink. Do not add to it: a new example in the prompt is written
// without scenario vocabulary, or it is not written.
var promptGuardExemptions = map[string]string{
	"svc_backup": "framing example in the leading_null rule — removed in #60",
	"t1021.001":  "MITRE example in the mitre-labels rule and the propose_hypothesis labels field — removed in #60",
	"win-file01": "entity example in every verb tool's entity description — removed in #60",
	"jdoe":       "entity example in every verb tool's entity description — removed in #60",
}

// TestPrompt_NoScenarioVocabulary is the mechanical half of adr/0057: the base
// prompt and the tool descriptions the model reads must not contain an
// identifier from any fixture scenario. A prompt that quotes the test set is
// tuned to it, and the eval cannot tell a rule that generalizes from one that
// names the fixture. The denylist is built from the fixtures themselves, so a
// new scenario extends the guard with no edit here.
func TestPrompt_NoScenarioVocabulary(t *testing.T) {
	idents, err := fixtureIdentifiers(fixtureRoot)
	if err != nil {
		t.Fatalf("collect fixture identifiers: %v", err)
	}
	if len(idents) < 10 {
		t.Fatalf("only %d fixture identifiers collected — the guard is not seeing the fixtures", len(idents))
	}
	t.Logf("guarding against %d fixture identifiers, e.g. %s", len(idents), strings.Join(idents[:min(8, len(idents))], ", "))

	texts := map[string]string{
		"prompt/" + framingLeadingNull: systemPrompt(Investigation{HypothesisFraming: framingLeadingNull}, nil),
		"prompt/" + framingLeadingOnly: systemPrompt(Investigation{HypothesisFraming: framingLeadingOnly}, nil),
	}
	for _, td := range intrinsicTools(nil) {
		texts["tool/"+td.Name] = toolText(td)
	}
	// A verb tool's entity/window descriptions are rendered by the loop, not
	// the backend: cover them through a synthetic descriptor.
	var cap Capability
	cap.Descriptor.Verb = "verb"
	cap.Descriptor.Inputs = append(cap.Descriptor.Inputs,
		struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Required bool   `json:"required"`
			Desc     string `json:"desc,omitempty"`
		}{Name: "target", Type: "entity", Required: true},
		struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Required bool   `json:"required"`
			Desc     string `json:"desc,omitempty"`
		}{Name: "window", Type: "time_window"},
	)
	texts["tool/<verb>"] = toolText(verbToolDef(cap))

	found := map[string][]string{} // identifier → where
	for where, text := range texts {
		lower := strings.ToLower(text)
		for _, id := range idents {
			if strings.Contains(lower, id) {
				found[id] = append(found[id], where)
			}
		}
	}
	for id, wheres := range found {
		sort.Strings(wheres)
		if _, exempt := promptGuardExemptions[id]; exempt {
			continue
		}
		t.Errorf("scenario identifier %q appears in %s — the prompt must not quote the fixtures; state the rule without it", id, strings.Join(wheres, ", "))
	}
	// An exemption that no longer bites is stale: remove it so the list only shrinks.
	for id := range promptGuardExemptions {
		if _, still := found[id]; !still {
			t.Errorf("exemption %q no longer matches anything — delete it", id)
		}
	}
}

// fixtureIdentifiers walks every fixture JSON under root and returns the
// lowercased identifier values (identifierKeys leaves, `types` list items, and
// the asset-criticality sidecar's entity keys), deduplicated and sorted.
func fixtureIdentifiers(root string) ([]string, error) {
	set := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if d.Name() == "asset-criticality.json" {
			if m, ok := doc.(map[string]any); ok {
				for k := range m {
					addIdentifier(set, k)
				}
			}
			return nil
		}
		collectIdentifiers(doc, "", set)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func collectIdentifiers(v any, key string, set map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			collectIdentifiers(child, k, set)
		}
	case []any:
		for _, child := range x {
			collectIdentifiers(child, key, set)
		}
	case string:
		if identifierKeys[key] {
			addIdentifier(set, x)
		}
	}
}

func addIdentifier(set map[string]bool, s string) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < minIdentifierLen || s == "*" {
		return
	}
	set[s] = true
}

// toolText flattens a tool definition to the text the model reads: its
// description and every description string in its input schema.
func toolText(td ToolDef) string {
	var b strings.Builder
	b.WriteString(td.Description)
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				if k == "description" {
					if s, ok := child.(string); ok {
						b.WriteString("\n")
						b.WriteString(s)
					}
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(td.InputSchema)
	return b.String()
}
