package action

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
)

// TestBuildRequestCommand_TargetTypes: an action type that declares target
// types refuses a target of any other type, or with no typed entity_ref, and
// names the declared types; a destination action does not read entity_ref
// (adr/0354).
func TestBuildRequestCommand_TargetTypes(t *testing.T) {
	catalog := defaultActionCatalog(t)
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	req := func(actionType, ref string) ActionRequest {
		return ActionRequest{
			ActionType:   actionType,
			Targets:      []aggregate.TargetSpec{{EntityRef: ref, ResolvedIdentifier: "x"}},
			EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000354"},
			Rationale:    "r",
		}
	}

	accepted := map[string][]string{
		"host.isolate":     {"reckon-host--1"},
		"host.unisolate":   {"reckon-host--1"},
		"account.disable":  {"user-account--1"},
		"account.enable":   {"user-account--1"},
		"email.quarantine": {"email-message--1"},
		"email.release":    {"email-message--1"},
		"email.purge":      {"email-message--1"},
		"ioc.block":        {"ipv4-addr--1", "ipv6-addr--1", "domain-name--1", "url--1", "file--1"},
		"ioc.unblock":      {"ipv4-addr--1", "ipv6-addr--1", "domain-name--1", "url--1", "file--1"},
	}
	for at, refs := range accepted {
		for _, ref := range refs {
			if _, err := BuildRequestCommand(catalog, req(at, ref), now); err != nil {
				t.Errorf("%s on %s: unexpected refusal: %v", at, ref, err)
			}
		}
	}

	refused := []struct {
		actionType, ref, wantTypes string
	}{
		{"host.isolate", "ipv4-addr--1", "reckon-host"}, // an IP is not a host
		{"host.isolate", "user-account--1", "reckon-host"},
		{"host.isolate", "", "reckon-host"},
		{"host.isolate", "reckon-host", "reckon-host"}, // no "--": no type
		{"account.disable", "reckon-cloud-account--1", "user-account"},
		{"email.purge", "email-addr--1", "email-message"},
		{"ioc.block", "reckon-host--1", "ipv4-addr, ipv6-addr, domain-name, url, file"},
	}
	for _, c := range refused {
		_, err := BuildRequestCommand(catalog, req(c.actionType, c.ref), now)
		if err == nil {
			t.Errorf("%s on %q: expected refusal", c.actionType, c.ref)
			continue
		}
		if !strings.Contains(err.Error(), c.wantTypes) {
			t.Errorf("%s on %q: error should name the declared types %q; got %v", c.actionType, c.ref, c.wantTypes, err)
		}
	}

	// Every target is checked, not only the first.
	mixed := req("host.isolate", "reckon-host--1")
	mixed.Targets = append(mixed.Targets, aggregate.TargetSpec{EntityRef: "ipv4-addr--2", ResolvedIdentifier: "10.0.0.2"})
	if _, err := BuildRequestCommand(catalog, mixed, now); err == nil || !strings.Contains(err.Error(), "target 1") {
		t.Errorf("a wrong-type second target should be refused naming it; got %v", err)
	}

	// A destination action does not read entity_ref.
	for _, r := range []ActionRequest{
		{ActionType: "ticket.comment", Targets: []aggregate.TargetSpec{{ResolvedIdentifier: "INC-1"}},
			Parameters: []byte(`{"body":"b"}`), EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000354"}, Rationale: "r"},
		{ActionType: "notify.slack", Targets: []aggregate.TargetSpec{{ResolvedIdentifier: "#soc"}},
			Parameters: []byte(`{"message":"m"}`), EvidenceRefs: []string{"observed-data--0d000000-0000-4000-8000-000000000354"}, Rationale: "r"},
	} {
		if _, err := BuildRequestCommand(catalog, r, now); err != nil {
			t.Errorf("%s with no entity_ref: unexpected refusal: %v", r.ActionType, err)
		}
	}

	// A reversal is checked like any other request.
	rev := ActionRequest{
		ActionType:    "host.unisolate",
		Targets:       []aggregate.TargetSpec{{EntityRef: "user-account--1", ResolvedIdentifier: "x"}},
		Rationale:     "undo",
		ReversalOfRef: uuid.New(),
	}
	if _, err := BuildRequestCommand(catalog, rev, now); err == nil {
		t.Error("a reversal with a wrong-type target should be refused")
	}
}

// TestRegister_RefusesAnUnknownTargetType (adr/0354): a target type that is
// not an entity type reckon mints could never be carried by a target, so the
// descriptor is refused and not registered. Every type in the default catalog
// is accepted.
func TestRegister_RefusesAnUnknownTargetType(t *testing.T) {
	for _, tt := range []string{"host", "ip-addr", "observed-data", "artifact", ""} {
		c := NewActionCatalog()
		err := c.Register(ActionDescriptor{ActionType: "x.act", TargetTypes: []string{tt}})
		if err == nil || !strings.Contains(err.Error(), "not an entity type") {
			t.Errorf("target type %q: Register = %v; want a refusal naming it", tt, err)
		}
		if _, ok := c.Descriptor("x.act"); ok {
			t.Errorf("target type %q: descriptor was registered", tt)
		}
	}
	c := NewActionCatalog()
	if err := c.Register(ActionDescriptor{ActionType: "x.act", TargetTypes: []string{"reckon-host", "ipv4-addr"}}); err != nil {
		t.Errorf("entity target types refused: %v", err)
	}
	defaultActionCatalog(t) // fails the test if any default descriptor is refused
}
