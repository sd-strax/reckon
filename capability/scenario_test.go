package capability

import (
	"context"
	"testing"
	"time"

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
	for _, v := range []string{"enumerate_logons", "get_process_ancestry", "get_network_connections", "search_alerts", "query_external_cases", "get_external_case_details",
		"get_process_executions", "get_persistence_artifacts", "get_file_activity", "resolve_dns", "get_privilege_changes", "pivot_on_indicator"} {
		if !avail[v] {
			t.Errorf("verb %s not available in shipped scenario", v)
		}
	}
	// The escape hatch stays unbound on fixtures (a fixture cannot run a query),
	// so it is trimmed from the demo's tool set.
	if avail["query_logs"] {
		t.Error("query_logs is available in the shipped scenario; it must stay unbound")
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

	// The incident-day logons answer to the account and the window asked for
	// (#164): one account's logons exclude the other's, and a window after
	// both is a complete, empty answer — a citeable absence.
	user := func(name string, from time.Time) CallInput {
		return CallInput{Entity: map[string]any{"user": map[string]any{"name": name}}, Window: TimeWindow{From: from}}
	}
	incident := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	jdoe, err := res.Resolve(ctx, "enumerate_logons", user("jdoe", incident))
	if err != nil {
		t.Fatal(err)
	}
	if len(jdoe.ObservedDataRefs) != 1 {
		t.Errorf("jdoe's logons that day: refs=%d; want 1 (not svc_backup's)", len(jdoe.ObservedDataRefs))
	}
	after, err := res.Resolve(ctx, "enumerate_logons", user("svc_backup", time.Date(2026, 4, 20, 14, 33, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if after.Coverage != CoverageComplete || after.NullObservation == nil {
		t.Errorf("svc_backup after 14:33: coverage=%q null=%v refs=%d; want a COMPLETE empty answer with a null observation",
			after.Coverage, after.NullObservation != nil, len(after.ObservedDataRefs))
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
	// Evidence of absence is citeable (adr/0061): the complete, empty answer
	// mints an x-null-observation and its id is the one ref in the result.
	if change.Coverage != CoverageComplete || change.NullObservation == nil || len(change.ObservedDataRefs) != 1 || change.ObservedDataRefs[0] != change.NullObservation.ID {
		t.Errorf("query_external_cases(change record): coverage=%q null=%v refs=%v; want COMPLETE with the null observation as the only ref", change.Coverage, change.NullObservation != nil, change.ObservedDataRefs)
	}
	if change.NullObservation != nil && change.NullObservation.Parameters["filter"] != "WIN-FILE01 change window" {
		t.Errorf("null observation does not carry the question: %+v", change.NullObservation.Parameters)
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

// TestShippedLateralMovementHuntVerbs resolves the hunt verbs (#210) against
// the shipped scenario: the fleet-wide pair answers with no entity and narrows
// to a host, a window after the incident is a citeable absence, the file, DNS
// and privilege verbs answer on their entity, and a pivot on each planted
// indicator finds its sightings while an unknown one is a complete, empty
// answer.
func TestShippedLateralMovementHuntVerbs(t *testing.T) {
	cfg, err := LoadTenantConfig("../examples/capability/lateral-movement.yaml")
	if err != nil {
		t.Fatalf("load shipped config: %v", err)
	}
	res, _, err := BuildResolver(cfg, "../fixtures", uuid.New(), true)
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}
	ctx := context.Background()
	host := func(name string) map[string]any { return map[string]any{"host": map[string]any{"hostname": name}} }
	hostsOf := func(r CapabilityResult) map[string]int {
		out := map[string]int{}
		for _, e := range r.Events {
			out[pathStr(e.Payload, "device.hostname")]++
		}
		return out
	}
	complete := func(name string, r CapabilityResult, err error, wantEvents int) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.Coverage != CoverageComplete || len(r.Events) != wantEvents {
			t.Errorf("%s: coverage=%q events=%d; want COMPLETE/%d", name, r.Coverage, len(r.Events), wantEvents)
		}
	}
	absent := func(name string, r CapabilityResult, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.Coverage != CoverageComplete || r.NullObservation == nil || len(r.Events) != 0 {
			t.Errorf("%s: coverage=%q null=%v events=%d; want a COMPLETE empty answer with a null observation", name, r.Coverage, r.NullObservation != nil, len(r.Events))
		}
	}

	// "Where did this command line run?" — no entity: both hosts.
	fleet, err := res.Resolve(ctx, "get_process_executions", CallInput{Extra: map[string]any{"command_line_contains": "-EncodedCommand"}})
	complete("get_process_executions(fleet)", fleet, err, 2)
	if h := hostsOf(fleet); len(h) != 2 {
		t.Errorf("get_process_executions(fleet): hosts %v; want two", h)
	}
	one, err := res.Resolve(ctx, "get_process_executions", CallInput{Entity: host("WIN-APP02")})
	complete("get_process_executions(host)", one, err, 1)
	other, err := res.Resolve(ctx, "get_process_executions", CallInput{Entity: map[string]any{"user": map[string]any{"name": "jdoe"}}})
	absent("get_process_executions(other account)", other, err)
	const powershell = "9f1c2d3e4a5b6c7d8e9f0011223344556677889900aabbccddeeff0011223344"
	byImage, err := res.Resolve(ctx, "get_process_executions", CallInput{Extra: map[string]any{"image_hash": powershell}})
	complete("get_process_executions(hash)", byImage, err, 2)
	noImage, err := res.Resolve(ctx, "get_process_executions", CallInput{Extra: map[string]any{"image_hash": "0000000000000000000000000000000000000000000000000000000000000000"}})
	absent("get_process_executions(unknown hash)", noImage, err)

	// "Which hosts gained persistence this week?" — three artifacts on two hosts;
	// narrowed to the first host, two; after the incident, none.
	sweep, err := res.Resolve(ctx, "get_persistence_artifacts", CallInput{})
	complete("get_persistence_artifacts(fleet)", sweep, err, 3)
	if h := hostsOf(sweep); h["WIN-FILE01"] != 2 || h["WIN-APP02"] != 1 {
		t.Errorf("get_persistence_artifacts(fleet): hosts %v; want WIN-FILE01:2 WIN-APP02:1", h)
	}
	narrowed, err := res.Resolve(ctx, "get_persistence_artifacts", CallInput{Entity: host("WIN-FILE01")})
	complete("get_persistence_artifacts(host)", narrowed, err, 2)
	later, err := res.Resolve(ctx, "get_persistence_artifacts", CallInput{Window: TimeWindow{From: time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC)}})
	absent("get_persistence_artifacts(after)", later, err)

	// The dropped file by hash, the resolution by name, the group add by account.
	const payload = "3a7f9c0e1b2d4f6a8c9e0b1d2f3a4c5e6f708192a3b4c5d6e7f8091a2b3c4d5e"
	file, err := res.Resolve(ctx, "get_file_activity", CallInput{Entity: map[string]any{"file": map[string]any{"hash": payload}}})
	complete("get_file_activity(hash)", file, err, 1)
	noFile, err := res.Resolve(ctx, "get_file_activity", CallInput{Entity: map[string]any{"file": map[string]any{"hash": "0000000000000000000000000000000000000000000000000000000000000000"}}})
	absent("get_file_activity(unknown hash)", noFile, err)
	dns, err := res.Resolve(ctx, "resolve_dns", CallInput{Entity: map[string]any{"domain": "exfil.example.net"}})
	complete("resolve_dns", dns, err, 1)
	if len(dns.EntityRefs) < 2 {
		t.Errorf("resolve_dns: %d entities; want the name and its answer", len(dns.EntityRefs))
	}
	priv, err := res.Resolve(ctx, "get_privilege_changes", CallInput{Entity: map[string]any{"user": map[string]any{"name": "svc_backup"}}})
	complete("get_privilege_changes", priv, err, 1)

	// A pivot finds every sighting of the indicator across the scenario's
	// sources, whichever way it is named; an indicator the scenario never saw
	// is absence, not unavailability.
	byIP, err := res.Resolve(ctx, "pivot_on_indicator", CallInput{Entity: map[string]any{"ip": "185.220.101.5"}})
	complete("pivot_on_indicator(ip)", byIP, err, 2)
	byDomain, err := res.Resolve(ctx, "pivot_on_indicator", CallInput{Entity: map[string]any{"domain": "exfil.example.net"}})
	complete("pivot_on_indicator(domain)", byDomain, err, 2)
	byHash, err := res.Resolve(ctx, "pivot_on_indicator", CallInput{Entity: map[string]any{"file": map[string]any{"hash": payload}}})
	complete("pivot_on_indicator(hash)", byHash, err, 1)
	unknown, err := res.Resolve(ctx, "pivot_on_indicator", CallInput{Entity: map[string]any{"ip": "203.0.113.9"}})
	absent("pivot_on_indicator(unknown)", unknown, err)
	pivotLater, err := res.Resolve(ctx, "pivot_on_indicator", CallInput{Entity: map[string]any{"ip": "185.220.101.5"}, Window: TimeWindow{From: time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC)}})
	absent("pivot_on_indicator(after)", pivotLater, err)

	// The old verbs are unchanged by the shared fixtures: the connection still
	// answers only get_network_connections' question, once.
	net, err := res.Resolve(ctx, "get_network_connections", CallInput{Entity: host("WIN-FILE01")})
	complete("get_network_connections", net, err, 1)
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

// TestShippedImpossibleTravelScenario loads the second bundled scenario
// (adr/0058) — identity-rooted, no host — and resolves its bound verbs end to
// end. The discriminating axes the eval's second driver relies on are pinned
// here: `outcome` tells failed sign-ins from successful ones (any-of matcher,
// §9.1), the 30-day office baseline is time-scoped, the foreign address has
// indicator context, and a free-text case search is COMPLETE with zero cases.
func TestShippedImpossibleTravelScenario(t *testing.T) {
	cfg, err := LoadTenantConfig("../examples/capability/impossible-travel.yaml")
	if err != nil {
		t.Fatalf("load shipped config: %v", err)
	}
	res, catalog, err := BuildResolver(cfg, "../fixtures", uuid.New(), true)
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}
	avail := map[string]bool{}
	for _, v := range res.AvailableVerbs(catalog) {
		avail[v] = true
	}
	for _, v := range []string{"enumerate_logons", "search_alerts", "get_indicator_context", "query_external_cases", "get_external_case_details"} {
		if !avail[v] {
			t.Errorf("verb %s not available in shipped scenario", v)
		}
	}
	for _, v := range []string{"get_process_ancestry", "get_network_connections", "get_host_context"} {
		if avail[v] {
			t.Errorf("verb %s is available but the scenario has no host — it must be unbound", v)
		}
	}

	ctx := context.Background()
	user := map[string]any{"user": map[string]any{"name": "m.reyes"}}
	day := map[string]any{"from": "2026-06-11T00:00:00Z", "to": "2026-06-12T00:00:00Z"}

	// Successful sign-ins on the incident day: the office one and the foreign one.
	ok, err := res.Resolve(ctx, "enumerate_logons", CallInput{Entity: user, Extra: map[string]any{"window": day}})
	if err != nil {
		t.Fatal(err)
	}
	if ok.Coverage != CoverageComplete || len(ok.ObservedDataRefs) != 2 {
		t.Errorf("enumerate_logons(SUCCESS, incident day): coverage=%q refs=%d; want COMPLETE/2", ok.Coverage, len(ok.ObservedDataRefs))
	}
	// The failures before the success are a separate question.
	fail, err := res.Resolve(ctx, "enumerate_logons", CallInput{Entity: user, Extra: map[string]any{"outcome": "FAILURE", "window": day}})
	if err != nil {
		t.Fatal(err)
	}
	if fail.Coverage != CoverageComplete || len(fail.ObservedDataRefs) != 5 {
		t.Errorf("enumerate_logons(FAILURE): coverage=%q refs=%d; want COMPLETE/5", fail.Coverage, len(fail.ObservedDataRefs))
	}
	// ALL reaches both.
	all, err := res.Resolve(ctx, "enumerate_logons", CallInput{Entity: user, Extra: map[string]any{"outcome": "ALL", "window": day}})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.ObservedDataRefs) != 7 {
		t.Errorf("enumerate_logons(ALL): refs=%d; want 7", len(all.ObservedDataRefs))
	}
	// A 30-day window reaches the office baseline; the incident day does not.
	month := map[string]any{"from": "2026-05-12T00:00:00Z", "to": "2026-06-12T00:00:00Z"}
	base, err := res.Resolve(ctx, "enumerate_logons", CallInput{Entity: user, Extra: map[string]any{"window": month}})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.ObservedDataRefs) != 12 {
		t.Errorf("enumerate_logons(SUCCESS, 30 days): refs=%d; want 12 (10 baseline + 2 incident-day)", len(base.ObservedDataRefs))
	}

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

	ti, err := res.Resolve(ctx, "get_indicator_context", CallInput{Entity: map[string]any{"ip": "203.0.113.46"}})
	if err != nil {
		t.Fatal(err)
	}
	if ti.Coverage != CoverageComplete || len(ti.ObservedDataRefs) != 1 {
		t.Errorf("get_indicator_context: coverage=%q refs=%d; want COMPLETE/1", ti.Coverage, len(ti.ObservedDataRefs))
	}

	queue, err := res.Resolve(ctx, "query_external_cases", CallInput{Extra: map[string]any{"queue": res.PolicyString(PolicyCaseQueue)}})
	if err != nil {
		t.Fatal(err)
	}
	if queue.Coverage != CoverageComplete || !caseNumbers(queue)["INC0012010"] {
		t.Errorf("query_external_cases(queue): coverage=%q cases=%v; want COMPLETE with INC0012010", queue.Coverage, caseNumbers(queue))
	}
	travel, err := res.Resolve(ctx, "query_external_cases", CallInput{Extra: map[string]any{"filter": "m.reyes travel notice"}})
	if err != nil {
		t.Fatal(err)
	}
	if travel.Coverage != CoverageComplete || travel.NullObservation == nil || len(travel.ObservedDataRefs) != 1 {
		t.Errorf("query_external_cases(travel notice): coverage=%q null=%v refs=%d; want COMPLETE with a citeable null observation", travel.Coverage, travel.NullObservation != nil, len(travel.ObservedDataRefs))
	}
	detail, err := res.Resolve(ctx, "get_external_case_details", CallInput{Extra: map[string]any{"case_id": "INC0012010"}})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Coverage != CoverageComplete || len(detail.ObservedDataRefs) != 1 {
		t.Errorf("get_external_case_details: coverage=%q refs=%d; want COMPLETE/1", detail.Coverage, len(detail.ObservedDataRefs))
	}
}
