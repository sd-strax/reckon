package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sd-strax/reckon/capability"
)

func eventWith(resources ...map[string]any) capability.OcsfPayload {
	list := make([]any, len(resources))
	for i, r := range resources {
		list[i] = r
	}
	return capability.OcsfPayload{ClassUID: 6003, Raw: map[string]any{"resources": list}}
}

func TestFilterScope(t *testing.T) {
	scoped := map[string]any{"uid": "u1", "name": "n1", "type": "T", "zone": "z1", "namespace": "ns1", "group": map[string]any{"name": "g1", "uid": "gid1"}}
	bare := map[string]any{"uid": "u1", "name": "n1", "type": "T"}
	cases := []struct {
		name  string
		scope resourceScope
		event capability.OcsfPayload
		keep  bool
	}{
		{"no post scope keeps all", resourceScope{name: "n1"}, eventWith(bare), true},
		{"a uid is not narrowed by zone", resourceScope{uid: "u1", zone: "z1"}, eventWith(bare), true},
		{"a uid is not narrowed by group, namespace or type", resourceScope{uid: "u1", group: "g9", namespace: "ns9", typ: "Other"}, eventWith(bare), true},
		{"zone absent from event drops", resourceScope{name: "n1", zone: "z1"}, eventWith(bare), false},
		{"group absent drops", resourceScope{name: "n1", group: "g1"}, eventWith(bare), false},
		{"namespace absent drops", resourceScope{name: "n1", namespace: "ns1"}, eventWith(bare), false},
		{"all equal keeps", resourceScope{name: "n1", zone: "z1", group: "g1", namespace: "ns1"}, eventWith(scoped), true},
		{"group by uid keeps", resourceScope{name: "n1", group: "gid1"}, eventWith(scoped), true},
		{"zone differs drops", resourceScope{name: "n1", zone: "z2"}, eventWith(scoped), false},
		{"namespace case preserved", resourceScope{name: "n1", namespace: "NS1"}, eventWith(scoped), false},
		{"scope must sit on the named resource", resourceScope{name: "n1", zone: "z1"},
			eventWith(bare, map[string]any{"name": "other", "zone": "z1"}), false},
		{"type checked when neither uid nor name", resourceScope{typ: "T", zone: "z1"}, eventWith(scoped), true},
		{"type mismatch drops when neither uid nor name", resourceScope{typ: "Other", zone: "z1"}, eventWith(scoped), false},
		{"type alone with no uid or name is checked", resourceScope{typ: "Other"}, eventWith(scoped), false},
		{"a named resource's type is checked", resourceScope{name: "n1", typ: "Other"}, eventWith(bare), false},
		{"role named deploy is not the user named deploy", resourceScope{name: "deploy", typ: "AWS::IAM::Role"},
			eventWith(map[string]any{"name": "deploy", "type": "AWS::IAM::User"}), false},
		{"role named deploy is the role named deploy", resourceScope{name: "deploy", typ: "AWS::IAM::Role"},
			eventWith(map[string]any{"name": "deploy", "type": "AWS::IAM::User"}, map[string]any{"name": "deploy", "type": "AWS::IAM::Role"}), true},
		{"element with no region does not match a given region", resourceScope{name: "n1", region: "us-east-1"}, eventWith(bare), false},
		{"element's own region is the one checked", resourceScope{name: "n1", region: "eu-west-1"},
			capability.OcsfPayload{Raw: map[string]any{"cloud": map[string]any{"region": "us-east-1"}, "resources": []any{map[string]any{"name": "n1", "region": "eu-west-1"}}}}, true},
		{"event region does not stand in for the element's", resourceScope{name: "n1", region: "us-east-1"},
			capability.OcsfPayload{Raw: map[string]any{"cloud": map[string]any{"region": "us-east-1"}, "resources": []any{map[string]any{"name": "n1", "region": "eu-west-1"}}}}, false},
		{"name on one element and namespace on another drops", resourceScope{name: "n1", namespace: "ns1"},
			eventWith(map[string]any{"name": "n1"}, map[string]any{"name": "other", "namespace": "ns1"}), false},
		{"name and region split across elements drops", resourceScope{name: "n1", region: "us-east-1"},
			eventWith(map[string]any{"name": "n1"}, map[string]any{"name": "other", "region": "us-east-1"}), false},
		{"a region with no name filters the event, not an element", resourceScope{region: "us-east-1"}, eventWith(bare), true},
		{"no resources drops", resourceScope{name: "n1", zone: "z1"}, capability.OcsfPayload{Raw: map[string]any{}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterScope([]capability.OcsfPayload{tc.event}, tc.scope)
			if (len(got) == 1) != tc.keep {
				t.Errorf("kept = %v, want %v", len(got) == 1, tc.keep)
			}
		})
	}
}

// TestInvoke_ScopeFilteredAfterFetch runs a namespaced question through the
// bridge: a resource named (not identified) with a namespace the lake row
// lacks is unanswered, not broadened; the same resource by uid is answered,
// since a uid is not narrowed.
func TestInvoke_ScopeFilteredAfterFetch(t *testing.T) {
	rr := loadRawResponse(t, "put-bucket-policy.json")
	b := testBridge(t, nil, &fakeAthena{pages: rr.Pages}, &fakeGlue{}, &fakeConfig{})
	params := map[string]any{"resource": map[string]any{"name": "acme-internal-exports", "namespace": "team-a"}, "window": window()}
	if out := invokeOK(t, b, opActivity, params); len(out.Events) != 0 {
		t.Errorf("events = %d, want the namespaced question unanswered", len(out.Events))
	}
	params["resource"] = map[string]any{"uid": "arn:aws:s3:::acme-internal-exports", "namespace": "team-a"}
	b = testBridge(t, nil, &fakeAthena{pages: rr.Pages}, &fakeGlue{}, &fakeConfig{})
	if out := invokeOK(t, b, opActivity, params); len(out.Events) != 1 {
		t.Errorf("events = %d, want 1: a uid is not narrowed by namespace", len(out.Events))
	}
}

func TestInvoke_ForeignProviderFallsThrough(t *testing.T) {
	for _, tc := range []struct {
		op     string
		params map[string]any
	}{
		{opActivity, map[string]any{"resource": map[string]any{"name": "b", "provider": "gcp"}, "window": window()}},
		{opActivity, map[string]any{"account": map[string]any{"uid": "123", "provider": "azure"}, "window": window()}},
		{opHistory, map[string]any{"resource": map[string]any{"uid": "x", "provider": "gcp"}, "window": window()}},
		{opContext, map[string]any{"resource": map[string]any{"uid": "x", "type": "t", "provider": "gcp"}}},
	} {
		f, c := &fakeAthena{}, &fakeConfig{}
		b := testBridge(t, nil, f, &fakeGlue{}, c)
		raw, _ := json.Marshal(map[string]any{"operation": tc.op, "params": tc.params})
		_, werr := b.invoke(context.Background(), raw)
		if got := classOf(t, werr); got != string(capability.ErrFallthrough) {
			t.Errorf("%s %v: class = %s, want FALLTHROUGH", tc.op, tc.params, got)
		}
		if len(f.started) != 0 || len(c.historyIn) != 0 || len(c.listIn) != 0 {
			t.Error("a foreign provider must not reach AWS")
		}
	}
}

func invokeOK(t *testing.T, b *bridge, op string, params map[string]any) invokeResult {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"operation": op, "params": params})
	res, werr := b.invoke(context.Background(), raw)
	if werr != nil {
		t.Fatalf("invoke: %+v (%s)", werr, werr.Data)
	}
	return res.(invokeResult)
}
