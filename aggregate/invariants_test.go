package aggregate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// --- pure applyCommand tests (no DB, no -short skip) --------------------------

// pairedState returns a folded state an existing investigation would have,
// matching env's tenant so the tenant-immutability guard passes.
func pairedState(env Envelope, status string, seq int64) aggregateState {
	return aggregateState{Seq: seq, Exists: true, TenantID: env.TenantID, Status: status}
}

// TestApplyCommand_LifecyclePairing asserts every lifecycle transition builds
// exactly (domain event, InterpretationRecorded) with a shared correlation_id,
// the correct interpretation type, a payload back-reference, and the current
// schema version stamped — covering the three transitions the integration
// pairing test (activate) does not.
func TestApplyCommand_LifecyclePairing(t *testing.T) {
	cases := []struct {
		name       string
		cmd        Command
		fromStatus string
		domainType string
		interpType string
	}{
		{"conclude", ConcludeInvestigation{ReportRef: "report--a", Summary: "tp"}, StatusActive, EventTypeConcluded, InterpretationConclusion},
		{"reopen", ReopenInvestigation{Reason: "new evidence"}, StatusConcluded, EventTypeReopened, InterpretationLifecycle},
		{"archive", ArchiveInvestigation{}, StatusConcluded, EventTypeArchived, InterpretationLifecycle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnvelope("alice")
			st := pairedState(env, tc.fromStatus, 3)
			if tc.name == "conclude" {
				// The conclude gate requires a verdict of record (01).
				st.Verdicts = []verdictEntry{{InterpID: uuid.New(), Disposition: VerdictMalicious}}
			}
			events, err := applyCommand(env, tc.cmd, st, testIDs())
			if err != nil {
				t.Fatalf("applyCommand: %v", err)
			}
			if len(events) != 2 {
				t.Fatalf("produced %d events; want 2 (domain + interpretation)", len(events))
			}
			domain, interp := events[0], events[1]
			if domain.Type != tc.domainType {
				t.Errorf("domain type = %q; want %q", domain.Type, tc.domainType)
			}
			if interp.Type != EventTypeInterpretationRecorded {
				t.Errorf("paired type = %q; want %q", interp.Type, EventTypeInterpretationRecorded)
			}
			if domain.SequenceNo != 4 || interp.SequenceNo != 5 {
				t.Errorf("sequence nos = %d,%d; want 4,5", domain.SequenceNo, interp.SequenceNo)
			}
			if domain.CorrelationID != env.CorrelationID || interp.CorrelationID != env.CorrelationID {
				t.Errorf("correlation ids %s,%s; want command's %s",
					domain.CorrelationID, interp.CorrelationID, env.CorrelationID)
			}
			if domain.Version != schemaVersion || interp.Version != schemaVersion {
				t.Errorf("versions = %d,%d; want %d", domain.Version, interp.Version, schemaVersion)
			}

			var ir InterpretationRecorded
			if err := json.Unmarshal(interp.Payload, &ir); err != nil {
				t.Fatalf("unmarshal interpretation: %v", err)
			}
			if ir.InterpretationType != tc.interpType {
				t.Errorf("interpretation_type = %q; want %q", ir.InterpretationType, tc.interpType)
			}

			// The domain payload's LifecycleInterpretationRef must point at the
			// paired interpretation.
			var ref struct {
				LifecycleInterpretationRef uuid.UUID `json:"lifecycle_interpretation_ref"`
			}
			if err := json.Unmarshal(domain.Payload, &ref); err != nil {
				t.Fatalf("unmarshal domain payload: %v", err)
			}
			if ref.LifecycleInterpretationRef != ir.InterpretationID {
				t.Errorf("domain ref %s != interpretation id %s",
					ref.LifecycleInterpretationRef, ir.InterpretationID)
			}
		})
	}
}

// TestApplyCommand_Pure: the same envelope, command, state and id source
// yield the same events, interpretation ids included (adr/0217). The paired
// interpretation's id is the one the domain payload references.
func TestApplyCommand_Pure(t *testing.T) {
	env := newTestEnvelope("alice")
	id := uuid.New()
	st := activeStateWithAction(env, id, "")
	cmd := sampleRequest(id)
	first, err := applyCommand(env, cmd, st, testIDs())
	if err != nil {
		t.Fatalf("applyCommand: %v", err)
	}
	again, err := applyCommand(env, cmd, st, testIDs())
	if err != nil {
		t.Fatalf("applyCommand again: %v", err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Errorf("two runs over the same input differ:\n%s\n%s", a, b)
	}
	want := testIDs()()
	var req ActionRequested
	var interp InterpretationRecorded
	for _, e := range first {
		switch e.Type {
		case EventTypeActionRequested:
			_ = json.Unmarshal(e.Payload, &req)
		case EventTypeInterpretationRecorded:
			_ = json.Unmarshal(e.Payload, &interp)
		}
	}
	if interp.InterpretationID != want || req.RequestingInterpretationID != want {
		t.Errorf("interpretation id %s, referenced as %s; want the id source's first, %s",
			interp.InterpretationID, req.RequestingInterpretationID, want)
	}
}

// TestApplyCommand_TenantMismatchRejected: an aggregate belongs to the tenant
// that created it; a command arriving under any other tenant is refused.
func TestApplyCommand_TenantMismatchRejected(t *testing.T) {
	env := newTestEnvelope("alice")
	state := aggregateState{Seq: 1, Exists: true, TenantID: uuid.New(), Status: StatusDraft}
	if _, err := applyCommand(env, ActivateInvestigation{}, state, testIDs()); err == nil {
		t.Fatal("expected tenant-mismatch rejection; got nil")
	}
}

// TestFoldState_UnknownEventType: the write-side fold accepts the metadata-only
// types it carries nothing for, and refuses a type it does not know instead of
// dropping it silently (#214).
func TestFoldState_UnknownEventType(t *testing.T) {
	stream := []Event{{SequenceNo: 1, Type: EventTypeCreated}}
	for i, typ := range []string{EventTypeRenamed, EventTypeMemberAdded, EventTypeMemberRemoved, EventTypeEvidenceAttached} {
		stream = append(stream, Event{SequenceNo: int64(i + 2), Type: typ, Payload: json.RawMessage(`{}`)})
	}
	if _, err := foldState(stream); err != nil {
		t.Fatalf("metadata-only events should fold: %v", err)
	}

	stream = append(stream, Event{SequenceNo: int64(len(stream) + 1), Type: "investigation.unheard_of", Payload: json.RawMessage(`{}`)})
	if _, err := foldState(stream); err == nil {
		t.Fatal("an unknown event type should fail the fold")
	}
}

// --- transactional invariant tests (need Pg) -----------------------------------

// failingProjector fails Apply on one event type, letting tests prove the
// Handler's one-transaction invariant: if any projector fails on any event,
// nothing — not even the earlier events of the same command — persists.
type failingProjector struct{ failOn string }

func (failingProjector) Name() string { return "failing" }
func (f failingProjector) Apply(_ context.Context, _ *sql.Tx, evt Event) error {
	if evt.Type == f.failOn {
		return errors.New("injected projector failure")
	}
	return nil
}
func (failingProjector) Reset(_ context.Context, _ *sql.Tx) error { return nil }

// TestHandleRollbackOnProjectorFailure proves the atomicity invariant. The
// activate command emits (status_changed, interpretation.recorded); the
// injected projector fails on the second, so the first — already appended and
// projected inside the same tx — must vanish with the rollback.
func TestHandleRollbackOnProjectorFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	aggID := uuid.New()

	// Create with a healthy handler first.
	mustHandle(t, newTestHandler(), cmdEnv(aggID), CreateInvestigation{Title: "INV-ATOMIC"})

	// Then activate through a handler whose extra projector fails on the
	// paired interpretation event.
	broken := NewHandler(NewStore(testDB),
		InvestigationCurrentProjector{},
		failingProjector{failOn: EventTypeInterpretationRecorded},
	)
	if _, err := broken.Handle(ctx, cmdEnv(aggID), ActivateInvestigation{}); err == nil {
		t.Fatal("expected Handle to fail via injected projector")
	}

	// Nothing from the failed command persisted: stream still has only the
	// creation event, projection still draft.
	events, err := NewStore(testDB).LoadStream(ctx, aggID)
	if err != nil {
		t.Fatalf("LoadStream: %v", err)
	}
	if len(events) != 1 || events[0].Type != EventTypeCreated {
		t.Fatalf("stream after rollback = %d events (first %q); want just the creation event",
			len(events), events[0].Type)
	}
	assertProjection(t, aggID, StatusDraft, "")
}

// TestHandleConcurrentCommands races two Handle calls on the same aggregate.
// Exactly one may win; the loser fails one of two legitimate ways depending on
// timing: a DB-level ErrConcurrent when the fold snapshots overlap, or a clean
// state-transition rejection when they serialize and the second fold already
// sees the winner's new state. Either way the aggregate can never accept both.
// (The store-level ErrConcurrent path specifically is covered with forced
// concurrency by TestStoreConcurrentInsert.)
func TestHandleConcurrentCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	h := newTestHandler()
	aggID := uuid.New()
	mustHandle(t, h, cmdEnv(aggID), CreateInvestigation{Title: "INV-RACE"})

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.Handle(context.Background(), cmdEnv(aggID), ActivateInvestigation{})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	var okCount, failCount int
	for err := range errs {
		if err == nil {
			okCount++
		} else {
			// Either failure mode is acceptable; neither may be a surprise.
			failCount++
		}
	}
	if okCount != 1 || failCount != 1 {
		t.Errorf("got %d successes and %d failures; want exactly 1 and 1", okCount, failCount)
	}

	// The winner's transition fully applied exactly once.
	assertProjection(t, aggID, StatusActive, "")
}

// TestEnvelopeColumnsRoundTrip verifies the 0008 envelope columns: EventID is
// minted at append, Version is stamped, RecordedAt is DB-set, CausationID
// stays NULL for command-initiated events.
func TestEnvelopeColumnsRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	resetTables(t)
	h := newTestHandler()
	aggID := uuid.New()
	res := mustHandle(t, h, cmdEnv(aggID), CreateInvestigation{Title: "INV-ENV"})

	if res.AppliedEvents[0].EventID == (uuid.UUID{}) {
		t.Error("AppliedEvents[0].EventID is zero; want minted at append")
	}

	events, err := NewStore(testDB).LoadStream(context.Background(), aggID)
	if err != nil {
		t.Fatalf("LoadStream: %v", err)
	}
	e := events[0]
	if e.EventID == (uuid.UUID{}) {
		t.Error("stored EventID is zero")
	}
	if e.EventID != res.AppliedEvents[0].EventID {
		t.Errorf("stored EventID %s != applied EventID %s", e.EventID, res.AppliedEvents[0].EventID)
	}
	if e.Version != schemaVersion {
		t.Errorf("stored Version = %d; want %d", e.Version, schemaVersion)
	}
	if e.RecordedAt.IsZero() {
		t.Error("stored RecordedAt is zero; want DB-stamped write time")
	}
	if e.CausationID.Valid {
		t.Errorf("CausationID = %v; want NULL for command-initiated events", e.CausationID)
	}
}

// TestCheckEventVersion: only the version this binary reads passes.
func TestCheckEventVersion(t *testing.T) {
	if err := CheckEventVersion(EventTypeCreated, schemaVersion); err != nil {
		t.Errorf("current version: %v", err)
	}
	for _, v := range []int{0, schemaVersion + 1} {
		if err := CheckEventVersion(EventTypeCreated, v); !errors.Is(err, ErrUnknownEventVersion) {
			t.Errorf("version %d: %v; want ErrUnknownEventVersion", v, err)
		}
	}
}

// TestUnknownEventVersion_StopsTheRead: an event at a version this binary
// does not know stops the stream load and every command on that aggregate,
// instead of being folded as the current shape (adr/0217).
func TestUnknownEventVersion_StopsTheRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping aggregate integration test in short mode")
	}
	resetTables(t)
	ctx := context.Background()
	h := newTestHandler()
	env := newTestEnvelope("alice")
	if _, err := h.Handle(ctx, env, CreateInvestigation{Title: "x"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `UPDATE events SET event_version = $2 WHERE aggregate_id = $1`,
		env.AggregateID, schemaVersion+1); err != nil {
		t.Fatalf("bump version: %v", err)
	}
	if _, err := NewStore(testDB).LoadStream(ctx, env.AggregateID); !errors.Is(err, ErrUnknownEventVersion) {
		t.Errorf("LoadStream: %v; want ErrUnknownEventVersion", err)
	}
	next := env
	next.CorrelationID = uuid.New()
	if _, err := h.Handle(ctx, next, ActivateInvestigation{}); !errors.Is(err, ErrUnknownEventVersion) {
		t.Errorf("Handle: %v; want ErrUnknownEventVersion", err)
	}
}
