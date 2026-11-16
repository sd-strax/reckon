package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegrade_RefusesARecordWithNoToolList: a trial record without the tool
// list predates it (adr/0380), and the graders cannot read it. The regrade
// refuses it before anything is written, so report.json is left as it was.
func TestRegrade_RefusesARecordWithNoToolList(t *testing.T) {
	const scenarioPath = "scenarios/lateral-movement.yaml"
	s, err := LoadScenario(scenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	report, err := json.Marshal(Report{Attribution: Attribution{ScenarioID: s.ID, DriverHash: s.Hash, Trials: 1}})
	if err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(dir, "report.json")
	if err := os.WriteFile(reportPath, report, 0o600); err != nil {
		t.Fatal(err)
	}
	trial, err := json.Marshal(TrialRecord{Trial: 1, Turns: []TurnRecord{{ToolCalls: []ToolCall{{ToolName: "list_actions"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "trial-1.json"), trial, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Regrade(dir, scenarioPath); err == nil || !strings.Contains(err.Error(), "predates") {
		t.Fatalf("err = %v; want a refusal saying the run predates the tool list", err)
	}
	after, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(report) {
		t.Error("report.json was rewritten by a refused regrade")
	}
}
