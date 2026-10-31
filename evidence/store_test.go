package evidence

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/sd-strax/reckon/aggregate"
	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/pgmigrate"
	"github.com/sd-strax/reckon/internal/testpg"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "evidence-test-pg-*")
	if err != nil {
		log.Fatalf("temp dir: %v", err)
	}
	cleanupDir := func() { _ = os.RemoveAll(dir) }
	port, err := testpg.FreePort()
	if err != nil {
		cleanupDir()
		log.Fatalf("%v", err)
	}
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Port(port).
		RuntimePath(filepath.Join(dir, "runtime")).
		DataPath(filepath.Join(dir, "data")).
		Username("test").
		Password("test").
		Database("reckon_test"))
	if err := pg.Start(); err != nil {
		cleanupDir()
		log.Fatalf("embedded postgres: %v", err)
	}
	dsn := testpg.DSN(port, "reckon_test")
	if err := pgmigrate.Run(dsn, aggregate.Migrations(), "reckon_main"); err != nil {
		_ = pg.Stop()
		cleanupDir()
		log.Fatalf("migrate: %v", err)
	}
	if testDB, err = sql.Open("postgres", dsn); err != nil {
		_ = pg.Stop()
		cleanupDir()
		log.Fatalf("sql.Open: %v", err)
	}
	code := m.Run()
	_ = testDB.Close()
	_ = pg.Stop()
	cleanupDir()
	os.Exit(code)
}

func newStore(t *testing.T) *Store {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	if _, err := testDB.Exec(`TRUNCATE ocsf_events, stix_edges, stix_objects`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return NewStore(testDB)
}

func stixID(typ string) identity.STIXID {
	return identity.STIXID(typ + "--" + uuid.NewString())
}

func telemetry(id uuid.UUID) capability.OcsfEvent {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return capability.OcsfEvent{
		ID: id, ClassUID: 3002, ClassName: "Authentication", Time: at, RecordedAt: at,
		SourceTool: "fixture:test", Payload: map[string]any{"class_uid": 3002},
	}
}

func rowCount(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := testDB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestPersist_OpensWhatItWrote: every ref a result carries opens afterwards.
func TestPersist_OpensWhatItWrote(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	eventID := uuid.New()
	od := stixID("observed-data")
	src, tgt := stixID("user-account"), stixID("x-host")
	err := s.Persist(ctx, capability.CapabilityResult{
		Events: []capability.OcsfEvent{telemetry(eventID)},
		Normalized: []capability.NormalizationResult{{
			ObservedData:  []capability.ObservedData{{ID: od, FirstObserved: time.Now()}},
			Relationships: []capability.Relationship{{ID: stixID("relationship"), Type: "x-authenticated-to", SourceRef: src, TargetRef: tgt}},
		}},
	})
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	e, err := s.Event(ctx, eventID)
	if err != nil || e.ClassUID != 3002 || e.ClassName != "Authentication" || e.SourceTool != "fixture:test" {
		t.Errorf("Event = %+v, %v", e, err)
	}
	o, err := s.Object(ctx, string(od))
	if err != nil || o.Type != "observed-data" || o.Ref != string(od) {
		t.Errorf("Object = %+v, %v", o, err)
	}
	if n := rowCount(t, "stix_edges"); n != 1 {
		t.Errorf("edges = %d; want 1", n)
	}
}

// TestPersist_IsAtomic: a malformed id fails the write and leaves nothing, so
// no ref from the failed invocation can be cited.
func TestPersist_IsAtomic(t *testing.T) {
	s := newStore(t)
	err := s.Persist(context.Background(), capability.CapabilityResult{
		Events: []capability.OcsfEvent{telemetry(uuid.New())},
		Normalized: []capability.NormalizationResult{{
			ObservedData: []capability.ObservedData{{ID: "no-separator"}},
		}},
	})
	if err == nil {
		t.Fatal("Persist accepted a malformed STIX id")
	}
	if n := rowCount(t, "ocsf_events"); n != 0 {
		t.Errorf("ocsf_events = %d after a failed write; want 0", n)
	}
}

// TestPersist_ReSeeingIsANoOp: ids are deterministic, so a second sighting of
// the same entity keeps the first write.
func TestPersist_ReSeeingIsANoOp(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	id := stixID("observed-data")
	eventID := uuid.New()
	write := func(n int) {
		t.Helper()
		err := s.Persist(ctx, capability.CapabilityResult{
			Events:     []capability.OcsfEvent{telemetry(eventID)},
			Normalized: []capability.NormalizationResult{{ObservedData: []capability.ObservedData{{ID: id, NumberObserved: n}}}},
		})
		if err != nil {
			t.Fatalf("Persist: %v", err)
		}
	}
	write(1)
	first, err := s.Object(ctx, string(id))
	if err != nil {
		t.Fatal(err)
	}
	write(2)
	second, err := s.Object(ctx, string(id))
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Payload) != string(second.Payload) {
		t.Errorf("re-seeing rewrote the object: %s -> %s", first.Payload, second.Payload)
	}
	if rowCount(t, "stix_objects") != 1 || rowCount(t, "ocsf_events") != 1 {
		t.Error("re-seeing added a row")
	}
}

func TestReads_NotFoundAndMalformed(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Object(ctx, string(stixID("indicator"))); !errors.Is(err, ErrNotFound) {
		t.Errorf("absent object: %v", err)
	}
	if _, err := s.Object(ctx, "indicator--not-a-uuid"); !errors.Is(err, ErrMalformedRef) {
		t.Errorf("malformed ref: %v", err)
	}
	if _, err := s.Event(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("absent event: %v", err)
	}
}

// TestIndicatorNeighbourhood: the sightings, vendor identity and cited evidence
// come back; a cited object that was never persisted is a gap, not an error.
func TestIndicatorNeighbourhood(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	indID, vendor, od := stixID("indicator"), stixID("identity"), stixID("observed-data")
	missing := stixID("observed-data")
	err := s.Persist(ctx, capability.CapabilityResult{
		Normalized: []capability.NormalizationResult{{
			ObservedData: []capability.ObservedData{{ID: od}},
			Indicators:   []capability.Indicator{{ID: indID, CreatedByRef: vendor, ValidFrom: time.Now()}},
			Sightings: []capability.Sighting{{
				ID: stixID("sighting"), SightingOfRef: indID,
				ObservedDataRefs: []identity.STIXID{od, missing, od},
			}},
			Identities: []capability.VendorIdentity{{ID: vendor, Name: "EDR"}},
		}},
	})
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	ind, err := s.Object(ctx, string(indID))
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.IndicatorNeighbourhood(ctx, ind)
	if err != nil {
		t.Fatalf("IndicatorNeighbourhood: %v", err)
	}
	if len(n.Sightings) != 1 || n.Sightings[0].Type != "sighting" {
		t.Errorf("sightings = %+v", n.Sightings)
	}
	if n.Identity == nil || n.Identity.Ref != string(vendor) {
		t.Errorf("identity = %+v", n.Identity)
	}
	if len(n.Evidence) != 1 || n.Evidence[0].Ref != string(od) {
		t.Errorf("evidence = %+v; want the one persisted object, once", n.Evidence)
	}
}
