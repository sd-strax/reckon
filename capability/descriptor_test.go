package capability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// huntVerbs are the §2 verbs a hypothesis-rooted hunt asks with (#210).
var huntVerbs = []string{
	"get_process_executions", "get_persistence_artifacts", "get_file_activity",
	"resolve_dns", "get_privilege_changes", "query_logs", "pivot_on_indicator",
}

// verbInputTypes are the InputParam types the agent's verb tool renderer knows
// (agent.verbToolDef / verbScalarSchema). A descriptor using another type
// would still render, as a string, which is the silent drift this pins.
var verbInputTypes = map[string]bool{
	"entity": true, "time_window": true, "string": true, "int": true,
	"enum": true, "alert_filter": true, "list<string>": true,
}

// TestDefaultCatalog_HuntVerbs: every hunt verb is registered, its inputs use
// known types, and the fleet-wide pair takes its entity optionally while the
// rest require one (or, for query_logs, take none).
func TestDefaultCatalog_HuntVerbs(t *testing.T) {
	c := DefaultCatalog()
	for _, v := range huntVerbs {
		d, ok := c.Descriptor(v)
		if !ok {
			t.Errorf("%s: not in DefaultCatalog", v)
			continue
		}
		if d.Intent == "" || d.Output == "" {
			t.Errorf("%s: intent/output empty", v)
		}
		var entity *InputParam
		for i := range d.Inputs {
			in := d.Inputs[i]
			if !verbInputTypes[in.Type] {
				t.Errorf("%s.%s: input type %q is not one the tool renderer knows", v, in.Name, in.Type)
			}
			if in.Type == "entity" {
				if entity != nil {
					t.Errorf("%s: two entity inputs; the tool shape carries one", v)
				}
				entity = &in
			}
		}
		switch v {
		case "get_process_executions", "get_persistence_artifacts":
			if entity == nil || entity.Required {
				t.Errorf("%s: fleet-wide verb must take an optional entity; got %+v", v, entity)
			}
		case "query_logs":
			if entity != nil {
				t.Errorf("%s: takes no entity; got %+v", v, entity)
			}
		default:
			if entity == nil || !entity.Required {
				t.Errorf("%s: must require an entity; got %+v", v, entity)
			}
		}
	}
}

// cloudVerbs are the §2.10 cloud control-plane verbs (#321).
var cloudVerbs = []string{"get_cloud_api_activity", "get_cloud_resource_context", "get_cloud_resource_history"}

// TestDefaultCatalog_CloudVerbs: every cloud verb is registered with inputs of
// known types and at most one entity; the activity search takes its entity
// optionally (the across-accounts question), the per-resource pair requires
// one. Their text is held to the vocabulary guard below with the rest.
func TestDefaultCatalog_CloudVerbs(t *testing.T) {
	c := DefaultCatalog()
	for _, v := range cloudVerbs {
		d, ok := c.Descriptor(v)
		if !ok {
			t.Errorf("%s: not in DefaultCatalog", v)
			continue
		}
		if d.Intent == "" || d.Output == "" {
			t.Errorf("%s: intent/output empty", v)
		}
		var entity *InputParam
		for i := range d.Inputs {
			in := d.Inputs[i]
			if !verbInputTypes[in.Type] {
				t.Errorf("%s.%s: input type %q is not one the tool renderer knows", v, in.Name, in.Type)
			}
			if in.Type == "entity" {
				if entity != nil {
					t.Errorf("%s: two entity inputs; the tool shape carries one", v)
				}
				entity = &in
			}
		}
		if v == "get_cloud_api_activity" {
			if entity == nil || entity.Required {
				t.Errorf("%s: must take an optional entity; got %+v", v, entity)
			}
		} else if entity == nil || !entity.Required {
			t.Errorf("%s: must require an entity; got %+v", v, entity)
		}
	}
}

// identifierKeys are the fixture leaves whose values name a scenario: the
// same set the agent's prompt guard uses, so the catalog's model-facing text
// is held to the rule the prompt is (adr/0057): state the verb without
// quoting the fixtures.
var identifierKeys = map[string]bool{
	"hostname": true, "name": true, "uid": true, "ip": true, "domain": true,
	"rdata": true, "cmd_line": true, "path": true, "data": true,
}

// descriptorGuardExemptions are identifiers the guard tolerates, each with the
// reason. The list only shrinks: an exemption that no longer matches fails.
var descriptorGuardExemptions = map[string]string{
	// get_external_case_details quotes a case number as the shape of the id;
	// the text predates this guard and is mirrored by the out-of-process case
	// adapter's describe, so it is changed in both places or neither.
	"inc0010042": "example case id in get_external_case_details",
}

// TestDefaultCatalog_NoScenarioVocabulary: no descriptor's intent or input
// description contains an identifier from the shipped fixtures.
func TestDefaultCatalog_NoScenarioVocabulary(t *testing.T) {
	idents := map[string]bool{}
	err := filepath.WalkDir("../fixtures", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") || strings.HasSuffix(p, "asset-criticality.json") {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		collectIdentifiers(v, "", idents)
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixtures: %v", err)
	}
	if len(idents) < 10 {
		t.Fatalf("only %d identifiers collected; the guard is not seeing the fixtures", len(idents))
	}
	c := DefaultCatalog()
	found := map[string]bool{}
	for _, v := range c.Verbs() {
		d, _ := c.Descriptor(v)
		texts := []string{d.Intent}
		for _, in := range d.Inputs {
			texts = append(texts, in.Desc)
		}
		lower := strings.ToLower(strings.Join(texts, " "))
		for id := range idents {
			if !strings.Contains(lower, id) {
				continue
			}
			found[id] = true
			if _, exempt := descriptorGuardExemptions[id]; !exempt {
				t.Errorf("%s: scenario identifier %q appears in its descriptor text", v, id)
			}
		}
	}
	for id := range descriptorGuardExemptions {
		if !found[id] {
			t.Errorf("exemption %q no longer matches anything — delete it", id)
		}
	}
}

func collectIdentifiers(v any, key string, set map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			collectIdentifiers(val, k, set)
		}
	case []any:
		for _, val := range x {
			collectIdentifiers(val, key, set)
		}
	case string:
		if identifierKeys[key] && len(x) >= 4 {
			set[strings.ToLower(x)] = true
		}
	}
}
