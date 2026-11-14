package main

import (
	"encoding/json"
	"testing"

	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// TestEchoedEventIsOCSF is check 3 of adr/0356 for the reference adapter:
// the event an invoke returns, read as the engine receives it, holds only
// fields OCSF defines for its class at the version it declares, and carries
// the echoed params under unmapped.
func TestEchoedEventIsOCSF(t *testing.T) {
	res, werr := (&adapter{}).invoke(json.RawMessage(`{"operation":"get_host_context","params":{"host":"WIN-FILE01"}}`))
	if werr != nil {
		t.Fatalf("invoke: %+v", werr)
	}
	wire, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Events []struct {
			ClassUID int            `json:"class_uid"`
			Raw      map[string]any `json:"raw"`
		} `json:"events"`
	}
	if err := json.Unmarshal(wire, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 {
		t.Fatalf("events = %d; want 1", len(out.Events))
	}
	e := out.Events[0]
	if got, _ := e.Raw["class_uid"].(float64); int(got) != e.ClassUID || e.ClassUID != 3002 {
		t.Errorf("sent as class %d, and the event says %v", e.ClassUID, e.Raw["class_uid"])
	}
	// OCSF requires a user on a logon. The echoed event is about no account:
	// it exists to carry the operation and the params back.
	for _, p := range ocsfschema.WithoutRequired(ocsfschema.ValidateEvent(e.Raw), "user") {
		t.Errorf("class %d: %s", e.ClassUID, p)
	}
	um, _ := e.Raw["unmapped"].(map[string]any)
	echo, _ := um["echo"].(map[string]any)
	if um["operation"] != "get_host_context" || echo["host"] != "WIN-FILE01" {
		t.Errorf("unmapped = %v; want the operation and the params", um)
	}
}
