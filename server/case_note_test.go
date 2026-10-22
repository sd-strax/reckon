package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/config"
)

// The note is an ordinary ticket.comment request (adr/0032): one target (the
// case), grounded on the promoted case read, inside the investigation.
func TestBuildCaseNoteRequest(t *testing.T) {
	id := uuid.New()
	ic := &aggregate.InvestigationCurrent{
		AggregateID: id,
		Title:       "Suspicious RDP logon",
		Seed:        &aggregate.Seed{Type: aggregate.SeedCase, CaseID: "INC0010042", Source: "servicenow", CaseRef: "observed-data--abc"},
	}
	body, err := buildCaseNoteRequest(ic, "opened in reckon")
	if err != nil {
		t.Fatal(err)
	}
	if body.ActionType != "ticket.comment" || body.InvestigationRef != id.String() {
		t.Errorf("request = %+v; want a ticket.comment inside %s", body, id)
	}
	if len(body.Targets) != 1 || body.Targets[0].ResolvedIdentifier != "INC0010042" || body.Targets[0].EntityRef != "observed-data--abc" {
		t.Errorf("targets = %+v; want the case as the single target", body.Targets)
	}
	if len(body.EvidenceRefs) != 1 || body.EvidenceRefs[0] != "observed-data--abc" {
		t.Errorf("evidence = %v; want the case_ref as grounding", body.EvidenceRefs)
	}
	var params map[string]string
	if err := json.Unmarshal(body.Parameters, &params); err != nil || params["body"] != "opened in reckon" {
		t.Errorf("parameters = %s; want {body: opened in reckon}", body.Parameters)
	}

	// Not a case seed, or a case seed with nothing to ground on: refused,
	// never an unsourced write.
	if _, err := buildCaseNoteRequest(&aggregate.InvestigationCurrent{AggregateID: id, Seed: &aggregate.Seed{Type: aggregate.SeedEntity}}, "x"); err == nil {
		t.Error("entity seed: want an error")
	}
	if _, err := buildCaseNoteRequest(&aggregate.InvestigationCurrent{AggregateID: id, Seed: &aggregate.Seed{Type: aggregate.SeedCase, CaseID: "INC1"}}, "x"); err == nil {
		t.Error("case seed without case_ref: want an error")
	}
}

// The offer follows the posture: present for a case seed unless off; absent
// for any other seed. The default text names who, when, and the investigation.
func TestCaseNoteOffer(t *testing.T) {
	now := time.Date(2026, 10, 22, 18, 0, 0, 0, time.UTC)
	seed := &aggregate.Seed{Type: aggregate.SeedCase, CaseID: "INC0010042", CaseRef: "observed-data--abc", Source: "servicenow"}

	b := &Backend{cfg: BackendConfig{CaseNoteOnSeed: config.CaseNoteConfirm}}
	offer := b.caseNoteOffer(context.Background(), "inv-1", "Suspicious RDP logon", seed, "sdas", now)
	if offer == nil || offer.CaseID != "INC0010042" {
		t.Fatalf("confirm posture: offer = %+v; want one for INC0010042", offer)
	}
	if offer.SystemName != "ServiceNow" {
		t.Errorf("system name = %q; want ServiceNow from the seed source", offer.SystemName)
	}
	// An unknown source with no store to consult: no name, no guess.
	if o := b.caseNoteOffer(context.Background(), "inv-1", "t", &aggregate.Seed{Type: aggregate.SeedCase, CaseID: "X", CaseRef: "observed-data--abc", Source: "fixture:s"}, "", now); o == nil || o.SystemName != "" {
		t.Errorf("fixture source without a store: offer = %+v; want an offer with no system name", o)
	}
	for _, want := range []string{"sdas", "2026-10-22T18:00:00Z", "Suspicious RDP logon", "reckon://investigation/inv-1"} {
		if !strings.Contains(offer.Body, want) {
			t.Errorf("default note lacks %q: %s", want, offer.Body)
		}
	}
	if o := (&Backend{cfg: BackendConfig{CaseNoteOnSeed: config.CaseNoteAuto}}).caseNoteOffer(context.Background(), "inv-1", "t", seed, "", now); o == nil {
		t.Error("auto posture: want an offer")
	}
	if o := (&Backend{cfg: BackendConfig{CaseNoteOnSeed: config.CaseNoteOff}}).caseNoteOffer(context.Background(), "inv-1", "t", seed, "", now); o != nil {
		t.Error("off posture: want no offer")
	}
	if o := b.caseNoteOffer(context.Background(), "inv-1", "t", &aggregate.Seed{Type: aggregate.SeedEntity}, "", now); o != nil {
		t.Error("entity seed: want no offer")
	}
}
