package aggregate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestRecordNarrative_Events: a narrative is one domain event with no paired
// interpretation, carrying the trimmed body and the model; the actor on the
// event is the authorship.
func TestRecordNarrative_Events(t *testing.T) {
	state := aggregateState{Exists: true, Status: StatusActive, Seq: 4, TenantID: newTestEnvelope("").TenantID}
	env := newTestEnvelope("")
	env.Actor = Actor{PrincipalID: "p-1", PrincipalName: "alice", Kind: ActorAIDelegated, Delegate: &AIDelegate{Vendor: "claude"}}
	events, err := applyCommand(env, RecordNarrative{Body: "  What happened so far.  ", Model: "m-1"}, state, uuid.New)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventTypeNarrativeRecorded || events[0].SequenceNo != 5 {
		t.Fatalf("events = %+v", events)
	}
	if !strings.Contains(string(events[0].Payload), `"body":"What happened so far."`) || !strings.Contains(string(events[0].Payload), `"model":"m-1"`) {
		t.Errorf("payload = %s", events[0].Payload)
	}
	if events[0].Actor.Kind != ActorAIDelegated || events[0].Actor.PrincipalName != "alice" {
		t.Errorf("actor = %+v; the authorship is the envelope's actor", events[0].Actor)
	}
}

// TestRecordNarrative_Guards: blank or oversized bodies are refused, a model
// on a human actor is refused, a concluded investigation still narrates, and
// an archived one does not.
func TestRecordNarrative_Guards(t *testing.T) {
	tenant := newTestEnvelope("").TenantID
	human := newTestEnvelope("")
	human.Actor = Actor{PrincipalID: "p-1", Kind: ActorHuman}
	active := aggregateState{Exists: true, Status: StatusActive, Seq: 1, TenantID: tenant}
	for name, c := range map[string]RecordNarrative{
		"blank":                  {Body: "   "},
		"oversized":              {Body: strings.Repeat("x", maxNarrativeRunes+1)},
		"model on a human actor": {Body: "ok", Model: "m"},
	} {
		if _, err := applyCommand(human, c, active, uuid.New); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := applyCommand(human, RecordNarrative{Body: strings.Repeat("y", maxNarrativeRunes)}, active, uuid.New); err != nil {
		t.Errorf("at the bound: %v", err)
	}
	concluded := aggregateState{Exists: true, Status: StatusConcluded, Seq: 9, TenantID: tenant}
	if _, err := applyCommand(human, RecordNarrative{Body: "the handoff"}, concluded, uuid.New); err != nil {
		t.Errorf("concluded investigation: %v; a closed case is still narrated", err)
	}
	archived := aggregateState{Exists: true, Status: StatusArchived, Seq: 9, TenantID: tenant}
	if _, err := applyCommand(human, RecordNarrative{Body: "x"}, archived, uuid.New); err == nil {
		t.Error("archived investigation accepted a narrative")
	}
}

// TestNarrative_Projection: revisions accumulate with their actors, the read
// answers the current one and who drafted the first, replay rebuilds it, and
// an investigation with none is ErrNoRecord.
func TestNarrative_Projection(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	tenantID := uuid.New()
	h := NewHandler(NewStore(testDB), InvestigationCurrentProjector{}, NarrativeProjector{})
	inv := activeInvestigation(t, h, tenantID)
	reads := h.Reads()
	if _, err := reads.Narrative(ctx, inv); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("no narrative yet: %v; want ErrNoRecord", err)
	}
	t0 := time.Now().UTC().Truncate(time.Microsecond)
	env := func(actor Actor, at time.Time) Envelope {
		return Envelope{AggregateID: inv, TenantID: tenantID, CorrelationID: uuid.New(), Actor: actor, OccurredAt: at}
	}
	draft := Actor{PrincipalID: "p-1", PrincipalName: "alice", Kind: ActorAIDelegated, Delegate: &AIDelegate{Vendor: "claude"}}
	edit := Actor{PrincipalID: "p-2", PrincipalName: "bob", Kind: ActorHuman}
	if _, err := h.Handle(ctx, env(draft, t0), RecordNarrative{Body: "The model's draft.", Model: "m-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(ctx, env(edit, t0.Add(time.Minute)), RecordNarrative{Body: "Bob's edit."}); err != nil {
		t.Fatal(err)
	}
	n, err := reads.Narrative(ctx, inv)
	if err != nil {
		t.Fatal(err)
	}
	if n.Current.Revision != 2 || n.Current.Body != "Bob's edit." || n.Current.Author.Kind != ActorHuman || n.Current.Author.Name != "bob" || n.Current.Author.Model != "" {
		t.Errorf("current = %+v", n.Current)
	}
	if n.DraftedBy.Kind != ActorAIDelegated || n.DraftedBy.Name != "alice" || n.DraftedBy.Model != "m-1" {
		t.Errorf("drafted by = %+v", n.DraftedBy)
	}
	if len(n.Revisions) != 2 || n.Revisions[0].Revision != 2 || n.Revisions[1].Body != "The model's draft." || n.Revisions[1].SequenceNo >= n.Revisions[0].SequenceNo {
		t.Errorf("revisions = %+v", n.Revisions)
	}
	// The cursor moved with the event, so a reader knows how far the record
	// had gone when the narrative was written.
	ic, err := reads.LoadInvestigationCurrent(ctx, inv)
	if err != nil || ic.LastEventSequence != n.Current.SequenceNo {
		t.Errorf("investigation_current cursor %d, narrative at %d (%v)", ic.LastEventSequence, n.Current.SequenceNo, err)
	}

	// Reset and replay rebuild the same rows.
	tx, err := testDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := (NarrativeProjector{}).Reset(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := reads.Narrative(ctx, inv); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("after Reset: %v; want ErrNoRecord", err)
	}
	if err := h.Replay(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := reads.Narrative(ctx, inv)
	if err != nil || again.Current.Revision != 2 || again.DraftedBy.Model != "m-1" {
		t.Errorf("after replay: %v %+v", err, again)
	}
}
