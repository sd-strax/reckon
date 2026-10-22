package capability

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestShippedLateralMovementScenario loads the committed example config and the
// bundled fixture scenario and resolves every bound verb end to end — a
// regression guard that the shipped fixtures/config stay valid and wired.
func TestShippedLateralMovementScenario(t *testing.T) {
	cfg, err := LoadTenantConfig("../examples/capability/lateral-movement.yaml")
	if err != nil {
		t.Fatalf("load shipped config: %v", err)
	}
	res, catalog, err := BuildResolver(cfg, "../fixtures", uuid.New(), true)
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}

	// The bound verbs advertise as available.
	avail := map[string]bool{}
	for _, v := range res.AvailableVerbs(catalog) {
		avail[v] = true
	}
	for _, v := range []string{"enumerate_logons", "get_process_ancestry", "get_network_connections", "search_alerts", "query_external_cases", "get_external_case_details"} {
		if !avail[v] {
			t.Errorf("verb %s not available in shipped scenario", v)
		}
	}

	host := CallInput{Entity: map[string]any{"host": map[string]any{"hostname": "WIN-FILE01"}}}
	ctx := context.Background()

	// enumerate_logons → two logons on WIN-FILE01.
	logons, err := res.Resolve(ctx, "enumerate_logons", host)
	if err != nil {
		t.Fatal(err)
	}
	if logons.Coverage != CoverageComplete || len(logons.ObservedDataRefs) != 2 {
		t.Errorf("enumerate_logons: coverage=%q refs=%d; want COMPLETE/2", logons.Coverage, len(logons.ObservedDataRefs))
	}

	// get_process_ancestry → the powershell process + parent.
	proc, err := res.Resolve(ctx, "get_process_ancestry", host)
	if err != nil {
		t.Fatal(err)
	}
	if proc.Coverage != CoverageComplete || len(proc.EntityRefs) == 0 {
		t.Errorf("get_process_ancestry: coverage=%q entities=%d; want COMPLETE/>0", proc.Coverage, len(proc.EntityRefs))
	}

	// get_network_connections → the outbound exfil connection.
	net, err := res.Resolve(ctx, "get_network_connections", host)
	if err != nil {
		t.Fatal(err)
	}
	if net.Coverage != CoverageComplete {
		t.Errorf("get_network_connections: coverage=%q; want COMPLETE", net.Coverage)
	}

	// search_alerts → the detection, producing an INFERRED Indicator.
	alerts, err := res.Resolve(ctx, "search_alerts", CallInput{})
	if err != nil {
		t.Fatal(err)
	}
	var indicators int
	for _, nr := range alerts.Normalized {
		indicators += len(nr.Indicators)
	}
	if alerts.Coverage != CoverageComplete || indicators == 0 {
		t.Errorf("search_alerts: coverage=%q indicators=%d; want COMPLETE/>0", alerts.Coverage, indicators)
	}

	// The case system of record answers two questions differently (14 §4.2 vs
	// the Act 3 change-record check): the configured queue returns the three
	// open Tier 2 incidents; a free-text filter with no queue is COMPLETE with
	// zero cases — evidence of absence, not unavailability.
	if got := res.PolicyString(PolicyCaseQueue); got != "SOC Tier 2" {
		t.Errorf("policies.case_queue = %q; want %q", got, "SOC Tier 2")
	}
	queue, err := res.Resolve(ctx, "query_external_cases", CallInput{Extra: map[string]any{"queue": res.PolicyString(PolicyCaseQueue)}})
	if err != nil {
		t.Fatal(err)
	}
	if queue.Coverage != CoverageComplete || len(queue.ObservedDataRefs) != 3 {
		t.Errorf("query_external_cases(queue): coverage=%q refs=%d; want COMPLETE/3", queue.Coverage, len(queue.ObservedDataRefs))
	}
	if !caseNumbers(queue)["INC0010042"] {
		t.Errorf("query_external_cases(queue): demo case INC0010042 missing; got %v", caseNumbers(queue))
	}
	change, err := res.Resolve(ctx, "query_external_cases", CallInput{Extra: map[string]any{"filter": "WIN-FILE01 change window"}})
	if err != nil {
		t.Fatal(err)
	}
	if change.Coverage != CoverageComplete || len(change.ObservedDataRefs) != 0 {
		t.Errorf("query_external_cases(change record): coverage=%q refs=%d; want COMPLETE/0", change.Coverage, len(change.ObservedDataRefs))
	}

	// Every queued case resolves to exactly one detail read — the fail-closed
	// seed flow (14 §2) needs exactly one, whichever the analyst picks.
	for _, id := range []string{"INC0010039", "INC0010041", "INC0010042"} {
		detail, err := res.Resolve(ctx, "get_external_case_details", CallInput{Extra: map[string]any{"case_id": id}})
		if err != nil {
			t.Fatal(err)
		}
		if detail.Coverage != CoverageComplete || len(detail.ObservedDataRefs) != 1 {
			t.Errorf("get_external_case_details(%s): coverage=%q refs=%d; want COMPLETE/1", id, detail.Coverage, len(detail.ObservedDataRefs))
		}
		if nums := caseNumbers(detail); !nums[id] {
			t.Errorf("get_external_case_details(%s): got case numbers %v", id, nums)
		}
	}
}

// caseNumbers collects the case_number extension the case normalizer wrote on
// each ObservedData in a result (03 §2.9).
func caseNumbers(res CapabilityResult) map[string]bool {
	out := map[string]bool{}
	for _, n := range res.Normalized {
		for _, od := range n.ObservedData {
			if num, ok := od.Extensions["case_number"].(string); ok {
				out[num] = true
			}
		}
	}
	return out
}
