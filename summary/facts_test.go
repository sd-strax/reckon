package summary

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/aggregate"
)

// adr/0103: the facts are counts and words from the record; the still-to-do
// list is ordered; the authorship line reads the lineage; the handoff note is
// the facts plus the narrative.
func TestBuildFactsAndHandoff(t *testing.T) {
	inv := uuid.New()
	verdictID := uuid.New()
	ic := aggregate.InvestigationCurrent{AggregateID: inv, Title: "Workstation logon chain", Status: "ACTIVE", LastEventSequence: 42,
		VerdictDisposition: "true_positive", VerdictRationale: "a stolen account was used", VerdictAt: sql.NullTime{Time: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC), Valid: true}, VerdictInterpretationID: verdictID,
		Seed: &aggregate.Seed{Type: aggregate.SeedEntity, EntityIdentifier: "host-a"}}
	h1 := aggregate.HypothesisCurrent{ID: "x-hypothesis--1", Status: aggregate.HypothesisSupported, Handle: "not-owner", Labels: []string{"T1078", "lateral"}}
	h2 := aggregate.HypothesisCurrent{ID: "x-hypothesis--2", Status: aggregate.HypothesisProposed}
	preds := []aggregate.PredictionCurrent{
		{ID: "x-prediction--1", HypothesisRef: h1.ID, Statement: "no prior interactive logons", Title: "no prior interactive logons", Status: aggregate.PredictionConfirmed},
		{ID: "x-prediction--2", HypothesisRef: h2.ID, Statement: "a change ticket covers the window, filed before it", Status: aggregate.PredictionUntested},
	}
	exp := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	actions := []aggregate.ActionCurrent{
		{ActionType: "host.isolate", Status: aggregate.ActionStatusSucceeded, Targets: []aggregate.TargetSpec{{ResolvedIdentifier: "host-a"}}},
		{ActionType: "account.disable", Status: aggregate.ActionStatusRequested, Targets: []aggregate.TargetSpec{{ResolvedIdentifier: "user-a"}}, ExpiresAt: exp},
		{ActionType: "notify.email", Status: aggregate.ActionStatusRequested, Targets: []aggregate.TargetSpec{{ResolvedIdentifier: "ops@example.test"}}},
	}
	f := BuildFacts(Inputs{
		Investigation: ic, Hypotheses: []aggregate.HypothesisCurrent{h1, h2}, Predictions: preds, Actions: actions,
		Thread:          []ThreadFact{{InterpretationID: verdictID.String(), ActorName: "Analyst One", ConsultedSOPs: []aggregate.ConsultedSOP{{SOPID: "sop-1", Title: "Runbook", Used: true}, {SOPID: "sop-2", Title: "Other", Used: false}}}},
		OwnerItems:      []OwnerFact{{Title: "Rotate the credential", Owner: "Identity team", Status: "OPEN"}, {Title: "Re-image", Owner: "Desktop", Status: "CONFIRMED"}},
		AwaitingReplies: 1, CreatedAt: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
	})
	if f.AsOfSequence != 42 || f.Seed == nil || f.Seed.Value != "host-a" || f.Verdict == nil || f.Verdict.AuthorName != "Analyst One" {
		t.Fatalf("facts head = %+v", f)
	}
	if f.Techniques[0] != "T1078" || len(f.Techniques) != 1 {
		t.Errorf("techniques = %v", f.Techniques)
	}
	if f.Containment.Total != 2 || f.Containment.Approved != 1 || f.Containment.Terminal != 1 || f.Containment.Pending != 1 || f.Containment.NearestExpiry == nil || !f.Containment.NearestExpiry.Equal(exp) || f.Containment.Done[0] != "host.isolate on host-a" {
		t.Errorf("containment = %+v", f.Containment)
	}
	if f.Notification.Status != "awaiting_approval" || f.Notification.Requested != 1 || f.Notification.AwaitingReplies != 1 {
		t.Errorf("notification = %+v", f.Notification)
	}
	if len(f.OpenQuestions) != 1 || f.OpenQuestions[0].Ordinal != 2 || f.OpenQuestions[0].Hypothesis != "H2" || f.OpenQuestions[0].Title != "a change ticket covers the window" {
		t.Errorf("open questions = %+v", f.OpenQuestions)
	}
	if f.OwnerItems.Open != 1 || f.OwnerItems.Confirmed != 1 || len(f.OwnerItems.Waiting) != 1 {
		t.Errorf("owner items = %+v", f.OwnerItems)
	}
	if len(f.ProceduresFollowed) != 1 || f.ProceduresFollowed[0].Title != "Runbook" {
		t.Errorf("procedures = %+v", f.ProceduresFollowed)
	}
	want := []string{"Approve account.disable on user-a", "Approve the notification to ops@example.test", "Await the replies still outstanding", "Test P2 under H2: a change ticket covers the window", "Ask Identity team: Rotate the credential"}
	if strings.Join(f.StillToDo, "|") != strings.Join(want, "|") {
		t.Errorf("still to do = %v\nwant %v", f.StillToDo, want)
	}

	revs := []Narrative{
		{Revision: 1, AuthorKind: AuthorAI, AuthorID: "reckon", GeneratorModel: "model-x", AsOfSequence: 30},
		{Revision: 2, AuthorKind: AuthorHuman, AuthorID: "a1", AuthorName: "Analyst One", AsOfSequence: 42},
	}
	a := AuthorshipOf(revs, 42)
	if a.DraftedBy != "reckon" || a.DraftedModel != "model-x" || a.EditedBy != "Analyst One" || a.Revisions != 2 || !a.LatestIsCurrent {
		t.Errorf("authorship = %+v", a)
	}
	if a2 := AuthorshipOf(revs[:1], 42); a2.EditedBy != "" || a2.LatestIsCurrent {
		t.Errorf("draft-only authorship = %+v", a2)
	}
	note := HandoffNote(f, "The account was used from a workstation it never uses.")
	if !strings.HasPrefix(note, "Workstation logon chain\n\nThe account was used") || !strings.Contains(note, "Verdict: true_positive — a stolen account was used (Analyst One)") || !strings.Contains(note, "- Ask Identity team: Rotate the credential") {
		t.Errorf("handoff note = %q", note)
	}
}
