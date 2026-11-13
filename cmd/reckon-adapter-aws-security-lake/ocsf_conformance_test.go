package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// assertOCSF is check 3 of adr/0356 for this adapter: every event it emits,
// read as the engine receives it over the invoke wire, holds only fields
// OCSF defines for its class at the version it declares, each in the shape
// the schema gives it.
//
// The adapter passes the lake's metadata through, so the version is the one
// the lake's source wrote. An event with none fails.
func assertOCSF(t *testing.T, events []capability.OcsfPayload) {
	t.Helper()
	data, err := json.Marshal(result(sourceLake, events))
	if err != nil {
		t.Fatal(err)
	}
	var wire invokeResult
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	for i, e := range wire.Events {
		if got, _ := e.Raw["class_uid"].(float64); int(got) != e.ClassUID {
			t.Errorf("event %d: sent as class %d, and the event says %v", i, e.ClassUID, e.Raw["class_uid"])
		}
		for _, p := range ocsfschema.ValidateEvent(e.Raw) {
			t.Errorf("event %d, class %d: %s", i, e.ClassUID, p)
		}
	}
}

// TestEveryEventIsOCSF runs every recorded response through the row decoding
// and checks each event against the OCSF schema of the version the lake
// declared (adr/0356): the pages recorded for the scenario, the page
// recorded with the table's profile, and the configuration snapshot the
// adapter shapes from AWS Config.
func TestEveryEventIsOCSF(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "raw_response", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("raw responses: %v %v", files, err)
	}
	for _, f := range files {
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			events := lakeEvents(t, name)
			if len(events) == 0 {
				t.Fatal("no events")
			}
			assertOCSF(t, events)
		})
	}
	// The configuration snapshot the adapter shapes itself from AWS Config
	// (class 5023), with tags, times and the supplementary configuration.
	t.Run("config snapshot", func(t *testing.T) {
		c := bucketConfig()
		c.history.ConfigurationItems[0].SupplementaryConfiguration = map[string]string{
			"PublicAccessBlockConfiguration": `{"blockPublicAcls":false}`,
		}
		events, err := resourceContext(context.Background(), c, bucketParams())
		if err != nil || len(events) != 1 {
			t.Fatalf("snapshot: %v, %d events", err, len(events))
		}
		if got := events[0].Raw["metadata"].(map[string]any)["version"]; got != inventoryVersion {
			t.Errorf("metadata.version = %v; want %s", got, inventoryVersion)
		}
		assertOCSF(t, events)
	})
	t.Run("profile page", func(t *testing.T) {
		cols, row := recordedPage(t, nil)
		e, err := rowToEvent(cols, row, testTable)
		if err != nil {
			t.Fatal(err)
		}
		assertOCSF(t, []capability.OcsfPayload{e})
	})
}
