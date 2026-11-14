package capability

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sd-strax/reckon/identity"
)

// stubAdapter is a configurable Adapter for resolver tests.
type stubAdapter struct {
	name    string
	healthy bool
	resp    AdapterResponse
	err     error
	calls   int
}

func (s *stubAdapter) Name() string                  { return s.name }
func (s *stubAdapter) Class() AdapterClass           { return ClassCustom }
func (s *stubAdapter) SupportedOperations() []string { return []string{"op"} }
func (s *stubAdapter) Health() HealthStatus          { return HealthStatus{Healthy: s.healthy} }
func (s *stubAdapter) Invoke(_ context.Context, _ string, _ map[string]any) (AdapterResponse, error) {
	s.calls++
	return s.resp, s.err
}

func stubAuthResponse() AdapterResponse {
	return AdapterResponse{SourceTool: "stub", Events: []OcsfPayload{{
		ClassUID:  3002,
		ClassName: "Authentication",
		Time:      time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC),
		Raw: map[string]any{
			"class_uid":    3002,
			"user":         map[string]any{"name": "jdoe", "domain": "CONTOSO"},
			"dst_endpoint": map[string]any{"hostname": "H1", "domain": "CONTOSO"},
		},
	}}}
}

func healthyStub(name string) *stubAdapter {
	return &stubAdapter{name: name, healthy: true, resp: stubAuthResponse()}
}

func newResolver(bindings map[string][]Binding, adapters map[string]Adapter) *Resolver {
	return NewResolver(TenantContext{Name: "t"}, bindings, adapters, testRegistry())
}

// TestResolveEndToEndFixture: the fixture adapter through the resolver yields
// COMPLETE with normalized ObservedData and entities.
func TestResolveEndToEndFixture(t *testing.T) {
	a := newTestFixture(t, twoLogonsPlusOther)
	res := newResolver(
		map[string][]Binding{
			"enumerate_logons": {{
				Adapter: "fixture", Operation: "replay", Priority: 100,
				Params: map[string]any{
					"target":  map[string]any{"hostname": "${entity.host.hostname}"},
					"outcome": "SUCCESS",
				},
			}},
		},
		map[string]Adapter{"fixture": a},
	)
	out, err := res.Resolve(context.Background(), "enumerate_logons", CallInput{
		Entity: map[string]any{"host": map[string]any{"hostname": "WIN-FILE01"}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if out.Coverage != CoverageComplete {
		t.Errorf("coverage = %q; want COMPLETE", out.Coverage)
	}
	if len(out.ObservedDataRefs) != 2 {
		t.Errorf("observed_data_refs = %d; want 2", len(out.ObservedDataRefs))
	}
	if len(out.EntityRefs) == 0 {
		t.Error("no entity_refs produced")
	}
}

// TestResolvePriorityStopsAtFirstSuccess: the higher-priority binding is chosen
// and lower-priority bindings are not invoked (non-fan-out).
func TestResolvePriorityStopsAtFirstSuccess(t *testing.T) {
	high, low := healthyStub("high"), healthyStub("low")
	res := newResolver(
		map[string][]Binding{"v": {
			{Adapter: "low", Operation: "op", Priority: 10},
			{Adapter: "high", Operation: "op", Priority: 100},
		}},
		map[string]Adapter{"high": high, "low": low},
	)
	out, err := res.Resolve(context.Background(), "v", CallInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Coverage != CoverageComplete {
		t.Errorf("coverage = %q; want COMPLETE", out.Coverage)
	}
	if high.calls != 1 || low.calls != 0 {
		t.Errorf("calls: high=%d low=%d; want 1 and 0", high.calls, low.calls)
	}
}

// TestResolveCoverageClasses exercises the §6.1 classification.
func TestResolveCoverageClasses(t *testing.T) {
	t.Run("unavailable_tenant_no_binding", func(t *testing.T) {
		res := newResolver(map[string][]Binding{}, map[string]Adapter{})
		out, _ := res.Resolve(context.Background(), "missing", CallInput{})
		if out.Coverage != CoverageUnavailableTenant {
			t.Errorf("coverage = %q; want UNAVAILABLE_TENANT", out.Coverage)
		}
	})

	t.Run("unavailable_tenant_not_applicable", func(t *testing.T) {
		res := newResolver(
			map[string][]Binding{"v": {{Adapter: "s", Operation: "op", Priority: 1,
				Params: map[string]any{"id": "${entity.host.external_id}"}}}},
			map[string]Adapter{"s": healthyStub("s")},
		)
		out, _ := res.Resolve(context.Background(), "v", CallInput{Entity: map[string]any{"host": map[string]any{}}})
		if out.Coverage != CoverageUnavailableTenant {
			t.Errorf("coverage = %q; want UNAVAILABLE_TENANT (not applicable)", out.Coverage)
		}
	})

	t.Run("unavailable_transient_unhealthy", func(t *testing.T) {
		down := &stubAdapter{name: "s", healthy: false}
		res := newResolver(
			map[string][]Binding{"v": {{Adapter: "s", Operation: "op", Priority: 1}}},
			map[string]Adapter{"s": down},
		)
		out, _ := res.Resolve(context.Background(), "v", CallInput{})
		if out.Coverage != CoverageUnavailableTransient {
			t.Errorf("coverage = %q; want UNAVAILABLE_TRANSIENT", out.Coverage)
		}
	})

	t.Run("unavailable_transient_invoke_unhealthy", func(t *testing.T) {
		// Adapter passes the health pre-check but Invoke reveals it is
		// unhealthy: still TRANSIENT (recoverable), never UNAVAILABLE_TENANT
		// (structural) — the binding WAS applicable.
		flaky := &stubAdapter{name: "s", healthy: true, err: adapterErrorf(ErrUnhealthy, "auth expired")}
		res := newResolver(
			map[string][]Binding{"v": {{Adapter: "s", Operation: "op", Priority: 1}}},
			map[string]Adapter{"s": flaky},
		)
		out, _ := res.Resolve(context.Background(), "v", CallInput{})
		if out.Coverage != CoverageUnavailableTransient {
			t.Errorf("coverage = %q; want UNAVAILABLE_TRANSIENT", out.Coverage)
		}
	})

	t.Run("failed_call_errors", func(t *testing.T) {
		bad := &stubAdapter{name: "s", healthy: true, err: adapterErrorf(ErrFallthrough, "no data")}
		res := newResolver(
			map[string][]Binding{"v": {{Adapter: "s", Operation: "op", Priority: 1}}},
			map[string]Adapter{"s": bad},
		)
		out, _ := res.Resolve(context.Background(), "v", CallInput{})
		if out.Coverage != CoverageFailed {
			t.Errorf("coverage = %q; want FAILED", out.Coverage)
		}
	})
}

// TestResolveFanout: fan-out bindings all contribute; a mixed outcome is PARTIAL.
func TestResolveFanout(t *testing.T) {
	good := healthyStub("good")
	bad := &stubAdapter{name: "bad", healthy: true, err: adapterErrorf(ErrRetry, "rate limited")}
	res := newResolver(
		map[string][]Binding{"pivot": {
			{Adapter: "good", Operation: "op", Priority: 100, FanOut: true},
			{Adapter: "bad", Operation: "op", Priority: 90, FanOut: true},
		}},
		map[string]Adapter{"good": good, "bad": bad},
	)
	out, err := res.Resolve(context.Background(), "pivot", CallInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Coverage != CoveragePartial {
		t.Errorf("coverage = %q; want PARTIAL", out.Coverage)
	}
	if good.calls != 1 || bad.calls != 1 {
		t.Errorf("fan-out should invoke both: good=%d bad=%d", good.calls, bad.calls)
	}
}

// TestResolveFatalPropagates: a FATAL adapter error aborts the call.
func TestResolveFatalPropagates(t *testing.T) {
	boom := &stubAdapter{name: "s", healthy: true, err: adapterErrorf(ErrFatal, "invariant violated")}
	res := newResolver(
		map[string][]Binding{"v": {{Adapter: "s", Operation: "op", Priority: 1}}},
		map[string]Adapter{"s": boom},
	)
	if _, err := res.Resolve(context.Background(), "v", CallInput{}); err == nil {
		t.Error("FATAL error did not propagate")
	}
}

// TestValidateBindings rejects an unknown transform at load time.
func TestValidateBindings(t *testing.T) {
	err := ValidateBindings(map[string][]Binding{
		"v": {{Adapter: "s", Params: map[string]any{"x": "${entity.a | bogus}"}}},
	})
	if err == nil {
		t.Error("ValidateBindings accepted an unknown transform")
	}
}

// TestResolveNullObservation: a COMPLETE answer with no events mints an
// x-null-observation (adr/0061) whose id rides ObservedDataRefs; a complete
// answer WITH events, a PARTIAL empty answer, and an unavailable verb mint
// nothing. The id is deterministic over the question and its window.
func TestResolveNullObservation(t *testing.T) {
	empty := &stubAdapter{name: "s", healthy: true, resp: AdapterResponse{SourceTool: "stub:s"}}
	res := newResolver(
		map[string][]Binding{"v": {{Adapter: "s", Operation: "op", Priority: 1}}},
		map[string]Adapter{"s": empty},
	)
	in := CallInput{
		Entity: map[string]any{"host": map[string]any{"hostname": "h1"}},
		Window: TimeWindow{From: time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)},
		Extra:  map[string]any{"filter": "change window"},
	}
	out, err := res.Resolve(context.Background(), "v", in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Coverage != CoverageComplete || out.NullObservation == nil {
		t.Fatalf("coverage=%q null=%v; want COMPLETE with a null observation", out.Coverage, out.NullObservation)
	}
	n := out.NullObservation
	if n.Type != identity.TypeNullObservation || n.Verb != "v" || n.Coverage != string(CoverageComplete) || len(n.Sources) != 1 || n.Sources[0] != "stub:s" {
		t.Errorf("null observation = %+v", n)
	}
	if n.WindowFrom == nil || n.WindowTo == nil || n.Parameters["filter"] != "change window" {
		t.Errorf("window/parameters not carried: %+v", n)
	}
	if len(n.ObjectRefs) != 1 || !strings.HasPrefix(string(n.ObjectRefs[0]), identity.TypeHost+"--") {
		t.Errorf("object_refs = %v; want the subject host", n.ObjectRefs)
	}
	if len(out.ObservedDataRefs) != 1 || out.ObservedDataRefs[0] != n.ID {
		t.Errorf("ObservedDataRefs = %v; want the null observation's id so it is citeable", out.ObservedDataRefs)
	}
	if n.Provenance.DerivationMode != DerivationDirect {
		t.Errorf("derivation = %q; want DIRECT", n.Provenance.DerivationMode)
	}

	// Deterministic over the question; a different window is a different absence.
	again, _ := res.Resolve(context.Background(), "v", in)
	if again.NullObservation.ID != n.ID {
		t.Errorf("same question minted different ids: %s vs %s", again.NullObservation.ID, n.ID)
	}
	later := in
	later.Window.To = later.Window.To.Add(24 * time.Hour)
	other, _ := res.Resolve(context.Background(), "v", later)
	if other.NullObservation.ID == n.ID {
		t.Error("a different window must be a different null observation")
	}

	// Complete with events: no null observation.
	withEvents, _ := newResolver(
		map[string][]Binding{"v": {{Adapter: "s", Operation: "op", Priority: 1}}},
		map[string]Adapter{"s": healthyStub("s")},
	).Resolve(context.Background(), "v", CallInput{})
	if withEvents.NullObservation != nil {
		t.Error("an answer with events must not mint a null observation")
	}

	// Partial and empty: nothing citeable — an unvouched absence is not evidence.
	partial, _ := newResolver(
		map[string][]Binding{"v": {
			{Adapter: "ok", Operation: "op", Priority: 2, FanOut: true},
			{Adapter: "bad", Operation: "op", Priority: 1, FanOut: true},
		}},
		map[string]Adapter{
			"ok":  &stubAdapter{name: "ok", healthy: true, resp: AdapterResponse{SourceTool: "stub:ok"}},
			"bad": &stubAdapter{name: "bad", healthy: true, err: adapterErrorf(ErrFallthrough, "down")},
		},
	).Resolve(context.Background(), "v", CallInput{})
	if partial.Coverage != CoveragePartial || partial.NullObservation != nil {
		t.Errorf("partial empty: coverage=%q null=%v; want PARTIAL and no null observation", partial.Coverage, partial.NullObservation)
	}

	// Unavailable: nothing.
	unavailable, _ := newResolver(map[string][]Binding{}, map[string]Adapter{}).Resolve(context.Background(), "missing", CallInput{})
	if unavailable.NullObservation != nil {
		t.Error("an unavailable verb must not mint a null observation")
	}
}

// TestQueryLogsBestEffortNormalization (03 §2.7): the escape hatch's rows are
// normalized best-effort — a row of a known class becomes typed ObservedData
// with entities, a row of an unknown class is wrapped opaque — and the answer
// is COMPLETE with the query carried as the call's parameters.
func TestQueryLogsBestEffortNormalization(t *testing.T) {
	siem := &stubAdapter{name: "siem", healthy: true, resp: AdapterResponse{SourceTool: "siem", Events: []OcsfPayload{
		{
			ClassUID: 3002, ClassName: "Authentication", Time: time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC),
			Raw: map[string]any{
				"class_uid":    3002,
				"user":         map[string]any{"name": "jdoe", "domain": "CONTOSO"},
				"dst_endpoint": map[string]any{"hostname": "H1", "domain": "CONTOSO"},
			},
		},
		{
			ClassUID: 9901, ClassName: "Vendor Row", Time: time.Date(2026, 4, 20, 14, 1, 0, 0, time.UTC),
			Raw: map[string]any{"class_uid": 9901, "message": "something the normalizer has no class for"},
		},
	}}}
	res := newResolver(
		map[string][]Binding{"query_logs": {{
			Adapter: "siem", Operation: "op", Priority: 100,
			Params: map[string]any{"query": "${query_text}", "limit": "${limit ?? 1000}"},
		}}},
		map[string]Adapter{"siem": siem},
	)
	out, err := res.Resolve(context.Background(), "query_logs", CallInput{Extra: map[string]any{"query_text": "index=auth host=H1"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Coverage != CoverageComplete || len(out.Normalized) != 2 {
		t.Fatalf("coverage=%q normalized=%d; want COMPLETE/2", out.Coverage, len(out.Normalized))
	}
	if len(out.Normalized[0].SCOs) == 0 {
		t.Error("the authentication row produced no entities; want typed normalization")
	}
	if len(out.Normalized[1].SCOs) != 0 || len(out.Normalized[1].ObservedData) != 1 {
		t.Errorf("the unknown-class row: scos=%d observed=%d; want 0 entities and one opaque ObservedData",
			len(out.Normalized[1].SCOs), len(out.Normalized[1].ObservedData))
	}
	if siem.calls != 1 {
		t.Errorf("adapter called %d times; want 1", siem.calls)
	}
}

// TestSubjectRefs_Cloud: a cloud account and a cloud resource named as the
// subject resolve to the ids the api_activity normalizer mints for them, by
// uid or by name under the account; a subject without a provider, or a
// resource that nothing identifies, is left out.
func TestSubjectRefs_Cloud(t *testing.T) {
	ids := identity.NewResolver(testNS)
	acct := ids.CloudAccount("aws", "111122223333")
	byUID, _ := ids.CloudResource(identity.CloudResourceIdentity{Provider: "aws", UID: "arn:aws:s3:::acme-internal-exports"})
	byName, _ := ids.CloudResource(identity.CloudResourceIdentity{AccountRef: acct, Type: "AWS::S3::Bucket", Name: "acme-logs"})

	got := subjectRefs(ids, map[string]any{"cloud_account": map[string]any{"provider": "AWS", "uid": "111122223333"}})
	if len(got) != 1 || got[0] != acct {
		t.Errorf("cloud_account = %v; want %s", got, acct)
	}
	got = subjectRefs(ids, map[string]any{"cloud_resource": map[string]any{"provider": "AWS", "uid": "arn:aws:s3:::acme-internal-exports"}})
	if len(got) != 1 || got[0] != byUID {
		t.Errorf("cloud_resource by uid = %v; want %s", got, byUID)
	}
	got = subjectRefs(ids, map[string]any{"cloud_resource": map[string]any{"provider": "aws", "account_uid": "111122223333", "type": "AWS::S3::Bucket", "name": "acme-logs"}})
	if len(got) != 1 || got[0] != byName {
		t.Errorf("cloud_resource by name = %v; want %s", got, byName)
	}
	// A region scopes the name, as the normalizer passes resources[].region.
	p := apiPayload()
	p["resources"] = []any{map[string]any{"name": "Orders", "type": "AWS::DynamoDB::Table", "region": "us-east-1"}}
	nr, _ := NewRegistry(ids).Normalize(makeEvent(p))
	var regioned identity.STIXID
	for _, s := range nr.SCOs {
		if s.Type == identity.TypeCloudResource {
			regioned = s.ID
		}
	}
	got = subjectRefs(ids, map[string]any{"cloud_resource": map[string]any{"provider": "aws", "account_uid": "111122223333", "type": "AWS::DynamoDB::Table", "region": "us-east-1", "name": "Orders"}})
	if regioned == "" || len(got) != 1 || got[0] != regioned {
		t.Errorf("cloud_resource by name in a region = %v; want the normalizer's %s", got, regioned)
	}
	p["cloud"] = map[string]any{"provider": "Azure", "account": map[string]any{"uid": "0f8fad5b-d9cb-469f-a165-70867728950e"}}
	p["resources"] = []any{map[string]any{"name": "api", "type": "Microsoft.Compute/virtualMachines", "region": "eastus", "group": map[string]any{"name": "rg-a"}, "namespace": "team-a"}}
	nr, _ = NewRegistry(ids).Normalize(makeEvent(p))
	var grouped identity.STIXID
	for _, s := range nr.SCOs {
		if s.Type == identity.TypeCloudResource {
			grouped = s.ID
		}
	}
	got = subjectRefs(ids, map[string]any{"cloud_resource": map[string]any{"provider": "Azure", "account_uid": "0f8fad5b-d9cb-469f-a165-70867728950e", "type": "Microsoft.Compute/virtualMachines", "region": "eastus", "group": "rg-a", "namespace": "team-a", "name": "api"}})
	if grouped == "" || len(got) != 1 || got[0] != grouped {
		t.Errorf("cloud_resource by name in a group = %v; want the normalizer's %s", got, grouped)
	}
	// A zone and a mixed-case namespace resolve to the normalizer's id: the
	// zone folded like the region, the namespace with its case kept.
	p["cloud"] = map[string]any{"provider": "GCP", "account": map[string]any{"uid": "acme-prod-1234"}}
	p["resources"] = []any{map[string]any{"name": "api", "type": "compute.googleapis.com/Instance", "region": "us-central1", "zone": "us-central1-a", "namespace": "SourceA"}}
	nr, _ = NewRegistry(ids).Normalize(makeEvent(p))
	var zoned identity.STIXID
	for _, s := range nr.SCOs {
		if s.Type == identity.TypeCloudResource {
			zoned = s.ID
		}
	}
	zonedSubject := func(zone, namespace string) []identity.STIXID {
		return subjectRefs(ids, map[string]any{"cloud_resource": map[string]any{"provider": "GCP", "account_uid": "acme-prod-1234", "type": "compute.googleapis.com/Instance", "region": "us-central1", "zone": zone, "namespace": namespace, "name": "api"}})
	}
	if got = zonedSubject("US-CENTRAL1-A", "SourceA"); zoned == "" || len(got) != 1 || got[0] != zoned {
		t.Errorf("cloud_resource by name in a zone = %v; want the normalizer's %s", got, zoned)
	}
	if got = zonedSubject("us-central1-b", "SourceA"); len(got) != 1 || got[0] == zoned {
		t.Errorf("another zone = %v; want a different resource", got)
	}
	if got = zonedSubject("us-central1-a", "sourcea"); len(got) != 1 || got[0] == zoned {
		t.Errorf("namespace with another case = %v; want a different resource", got)
	}
	if other := subjectRefs(ids, map[string]any{"cloud_resource": map[string]any{"provider": "aws", "account_uid": "111122223333", "type": "AWS::DynamoDB::Table", "region": "us-west-2", "name": "Orders"}}); len(other) != 1 || other[0] == regioned {
		t.Errorf("another region = %v; want a different resource", other)
	}

	for _, e := range []map[string]any{
		{"cloud_account": map[string]any{"provider": "aws"}},
		{"cloud_account": map[string]any{"uid": "111122223333"}},
		{"cloud_account": map[string]any{"provider": " ", "uid": "111122223333"}},
		{"cloud_resource": map[string]any{"provider": "aws", "name": "acme-logs"}},
		{"cloud_resource": map[string]any{"uid": "arn:aws:s3:::acme-internal-exports"}},
		{"cloud_resource": map[string]any{"account_uid": "111122223333", "type": "AWS::S3::Bucket", "name": "acme-logs"}},
	} {
		if got := subjectRefs(ids, e); len(got) != 0 {
			t.Errorf("subjectRefs(%v) = %v; want none", e, got)
		}
	}

	// The normalizer agrees: the account and resource it mints are these ids.
	res, _ := NewRegistry(ids).Normalize(makeEvent(apiPayload()))
	minted := map[identity.STIXID]bool{}
	for _, s := range res.SCOs {
		minted[s.ID] = true
	}
	if !minted[acct] || !minted[byUID] {
		t.Errorf("normalizer minted %v; want the subject ids %s and %s", minted, acct, byUID)
	}
}
