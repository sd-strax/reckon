package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
)

// equivalenceNS is the fixed namespace both sides of the equivalence test
// mint under.
var equivalenceNS = uuid.MustParse("6f1d2c3b-4a59-4e8f-9b7c-0d1e2f3a4b5c")

func TestRowToEvent_Reassembles(t *testing.T) {
	events := lakeEvents(t, "put-bucket-policy.json")
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 (header dropped)", len(events))
	}
	e := events[0]
	if e.ClassUID != 6003 || e.ClassName != "API Activity" {
		t.Errorf("class = %d %q", e.ClassUID, e.ClassName)
	}
	if want := time.Date(2026, 7, 14, 9, 28, 0, 0, time.UTC); !e.Time.Equal(want) {
		t.Errorf("time = %s, want %s", e.Time, want)
	}
	raw := e.Raw
	if raw["time"] != int64(1784021280000) || raw["time_dt"] != "2026-07-14T09:28:00Z" {
		t.Errorf("time = %v, time_dt = %v", raw["time"], raw["time_dt"])
	}
	if raw["activity_id"] != int64(3) || raw["status_id"] != int64(1) || raw["class_uid"] != int64(6003) || raw["type_uid"] != int64(600303) {
		t.Errorf("scalars = %v %v %v %v", raw["activity_id"], raw["status_id"], raw["class_uid"], raw["type_uid"])
	}
	meta := raw["metadata"].(map[string]any)
	product := meta["product"].(map[string]any)
	if product["vendor_name"] != "AWS" || product["name"] != "Security Lake" || meta["version"] != "1.1.0" {
		t.Errorf("metadata = %v", meta)
	}
	if meta["log_name"] != testTable {
		t.Errorf("log_name = %v, want the table", meta["log_name"])
	}
	user := raw["actor"].(map[string]any)["user"].(map[string]any)
	if user["uid"] != "arn:aws:iam::111122223333:user/deploy-ci" || user["credential_uid"] != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("actor.user = %v", user)
	}
	if _, ok := user["email_addr"]; ok {
		t.Error("a NULL struct field must be dropped")
	}
	if _, ok := raw["actor"].(map[string]any)["session"]; ok {
		t.Error("a NULL session must be dropped")
	}
	res := raw["resources"].([]any)
	if len(res) != 1 || res[0].(map[string]any)["uid"] != "arn:aws:s3:::acme-internal-exports" {
		t.Errorf("resources = %v", res)
	}
}

func TestRowToEvent_NullColumnOmitted(t *testing.T) {
	events := lakeEvents(t, "list-users-denied.json")
	if _, ok := events[0].Raw["resources"]; ok {
		t.Error("a NULL resources column must be absent")
	}
	resp := events[0].Raw["api"].(map[string]any)["response"].(map[string]any)
	if resp["error"] != "AccessDenied" {
		t.Errorf("api.response = %v", resp)
	}
}

func TestRowToEvent_KeepsLakeLogName(t *testing.T) {
	logName := `{"log_name":"lake-said","version":"1.1.0"}`
	e, err := rowToEvent([]string{"metadata"}, []*string{&logName}, testTable)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Raw["metadata"].(map[string]any)["log_name"]; got != "lake-said" {
		t.Errorf("log_name = %v, want the lake's own", got)
	}
	if e.Raw["class_uid"] != int64(6003) {
		t.Errorf("class_uid = %v, want 6003 when the row carries none", e.Raw["class_uid"])
	}
}

func TestRowToEvent_BadColumns(t *testing.T) {
	bad := "{not json"
	_, err := rowToEvent([]string{"actor"}, []*string{&bad}, testTable)
	isClass(t, err, capability.ErrFatal, "actor")
	notInt := "x"
	_, err = rowToEvent([]string{"status_id"}, []*string{&notInt}, testTable)
	isClass(t, err, capability.ErrFatal, "status_id")
}

func TestRowToEvent_RenderedTime(t *testing.T) {
	ts := "2026-07-14 09:28:00.000"
	e, err := rowToEvent([]string{"time"}, []*string{&ts}, testTable)
	if err != nil {
		t.Fatal(err)
	}
	if e.Raw["time"] != int64(1784021280000) {
		t.Errorf("time = %v", e.Raw["time"])
	}
}

// --- equivalence (adr/0322) ---

// normalizeWire normalizes events as the engine receives them: through the
// invoke wire (JSON), then the registry.
func normalizeWire(t *testing.T, source string, events []capability.OcsfPayload) []capability.NormalizationResult {
	t.Helper()
	data, err := json.Marshal(result(source, events))
	if err != nil {
		t.Fatal(err)
	}
	var wire invokeResult
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	reg := capability.NewRegistry(identity.NewResolver(equivalenceNS))
	var out []capability.NormalizationResult
	for _, e := range wire.Events {
		res, err := reg.Normalize(capability.OcsfEvent{
			ID: uuid.New(), ClassUID: e.ClassUID, ClassName: e.ClassName, Time: e.Time,
			SourceTool: wire.SourceTool, Payload: e.Raw,
		})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, res)
	}
	return out
}

// normalizeFixture normalizes a fixture scenario file's ocsf object.
func normalizeFixture(t *testing.T, name string) capability.NormalizationResult {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "exposed-bucket-via-access-key", name))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		OCSF map[string]any `json:"ocsf"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	classUID, _ := f.OCSF["class_uid"].(float64)
	reg := capability.NewRegistry(identity.NewResolver(equivalenceNS))
	res, err := reg.Normalize(capability.OcsfEvent{
		ID: uuid.New(), ClassUID: int(classUID), SourceTool: "fixture:exposed-bucket-via-access-key", Payload: f.OCSF,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// relsOf is the sorted (type, source, target) of each relationship.
func relsOf(res capability.NormalizationResult) []string {
	var out []string
	for _, r := range res.Relationships {
		out = append(out, r.Type+" "+string(r.SourceRef)+" "+string(r.TargetRef))
	}
	sort.Strings(out)
	return out
}

// projectionColumns are the column names the adapter's projection yields:
// the alias of a cast, the bare name otherwise.
func projectionColumns() []string {
	var out []string
	for _, p := range projection {
		if _, alias, ok := strings.Cut(p, " AS JSON) AS "); ok {
			p = alias
		}
		out = append(out, strings.Trim(p, `"`))
	}
	return out
}

// TestRawResponses_MatchProjection pins every recorded page to the query the
// adapter sends: its header row and column metadata are the projection's
// columns, in order, so a column dropped from or added to the projection
// fails here until the pages are recaptured.
func TestRawResponses_MatchProjection(t *testing.T) {
	want := projectionColumns()
	if len(want) != len(projection) || want[0] != "time" || want[6] != "actor" {
		t.Fatalf("projection columns = %v", want)
	}
	files, err := filepath.Glob(filepath.Join("testdata", "raw_response", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("raw responses: %v %v", files, err)
	}
	for _, f := range files {
		rr := loadRawResponse(t, filepath.Base(f))
		first := rr.Pages[0].ResultSet
		if got := rowValues(first.Rows[0]); !reflect.DeepEqual(got, want) {
			t.Errorf("%s header = %v\nwant %v", f, got, want)
		}
		if got := columnNames(first); !reflect.DeepEqual(got, want) {
			t.Errorf("%s column metadata = %v", f, got)
		}
	}
}

// idsOf is the sorted ids of the SCOs of each identity type.
func idsOf(res capability.NormalizationResult) map[string][]string {
	out := map[string][]string{}
	for _, s := range res.SCOs {
		switch s.Type {
		case identity.TypeUserAccount, identity.TypeCloudAccount, identity.TypeCloudResource:
			out[s.Type] = append(out[s.Type], string(s.ID))
		}
	}
	for _, ids := range out {
		sort.Strings(ids)
	}
	return out
}

// TestEquivalence_FixtureVsLake pins that the lake and the fixture scenario
// agree on identity: the same call, read from Security Lake through the
// adapter's row decoding and normalized by the engine, mints the principal,
// account and resource ids the fixture's event mints (adr/0322).
func TestEquivalence_FixtureVsLake(t *testing.T) {
	for _, tc := range []struct {
		raw, fixture string
		resources    int
	}{
		{"put-bucket-policy.json", "api-bucket-2.json", 1},
		{"create-access-key.json", "api-persist-1.json", 1},
		{"attach-user-policy.json", "api-escalate-1.json", 2},
		{"list-users-denied.json", "api-denied-1.json", 0},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if rr := loadRawResponse(t, tc.raw); rr.Fixture == nil || *rr.Fixture != tc.fixture {
				t.Fatalf("%s is tagged for fixture %v", tc.raw, rr.Fixture)
			}
			lake := normalizeWire(t, sourceLake, lakeEvents(t, tc.raw))
			if len(lake) != 1 {
				t.Fatalf("lake events = %d", len(lake))
			}
			got, want := idsOf(lake[0]), idsOf(normalizeFixture(t, tc.fixture))
			if len(want[identity.TypeUserAccount]) == 0 || len(want[identity.TypeCloudAccount]) == 0 {
				t.Fatalf("fixture minted no principal or account: %v", want)
			}
			if n := len(want[identity.TypeCloudResource]); n != tc.resources {
				t.Fatalf("fixture minted %d resources, want %d", n, tc.resources)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("lake ids = %v\nfixture ids = %v", got, want)
			}
			if gr, wr := relsOf(lake[0]), relsOf(normalizeFixture(t, tc.fixture)); !reflect.DeepEqual(gr, wr) {
				t.Errorf("lake relationships = %v\nfixture relationships = %v", gr, wr)
			}
		})
	}
}

// TestEquivalence_AssumedRolePrincipalIsIssuer pins the session-scoped
// actor: an assumed-role call is attributed to the role that issued the
// session, not the session ARN.
func TestEquivalence_AssumedRolePrincipalIsIssuer(t *testing.T) {
	lake := normalizeWire(t, sourceLake, lakeEvents(t, "get-bucket-policy-assumed-role.json"))
	r := identity.NewResolver(equivalenceNS)
	issuer := r.UserAccount("", "arn:aws:iam::111122223333:role/deploy-role", "aws")
	session := r.UserAccount("", "arn:aws:sts::111122223333:assumed-role/deploy-role/ci-session", "aws")
	ids := idsOf(lake[0])[identity.TypeUserAccount]
	if !reflect.DeepEqual(ids, []string{string(issuer)}) {
		t.Errorf("principal ids = %v, want the issuer %s (not the session %s)", ids, issuer, session)
	}
	// The bucket it read is the scenario's bucket.
	bucket := idsOf(normalizeFixture(t, "api-bucket-2.json"))[identity.TypeCloudResource]
	if got := idsOf(lake[0])[identity.TypeCloudResource]; !reflect.DeepEqual(got, bucket) {
		t.Errorf("resource ids = %v, want %v", got, bucket)
	}
}

// TestEquivalence_NamedResource: a management event whose resource carries
// no ARN — only its name, type and region — mints the name-scoped
// x-cloud-resource the 6003 normalizer mints for the same resource in a
// fixture-shaped event, belonging to the same account.
func TestEquivalence_NamedResource(t *testing.T) {
	lake := normalizeWire(t, sourceLake, lakeEvents(t, "put-bucket-versioning-by-name.json"))
	if len(lake) != 1 {
		t.Fatalf("lake events = %d", len(lake))
	}

	data, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "exposed-bucket-via-access-key", "api-bucket-2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		OCSF map[string]any `json:"ocsf"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	f.OCSF["resources"] = []any{map[string]any{"name": "acme-internal-exports", "type": "AWS::S3::Bucket", "region": "us-east-1"}}
	reg := capability.NewRegistry(identity.NewResolver(equivalenceNS))
	want, err := reg.Normalize(capability.OcsfEvent{ID: uuid.New(), ClassUID: 6003, SourceTool: "fixture", Payload: f.OCSF})
	if err != nil {
		t.Fatal(err)
	}

	got := idsOf(lake[0])[identity.TypeCloudResource]
	if len(got) != 1 {
		t.Fatalf("lake resources = %v", got)
	}
	if !reflect.DeepEqual(got, idsOf(want)[identity.TypeCloudResource]) {
		t.Errorf("lake resource = %v, fixture-shaped = %v", got, idsOf(want)[identity.TypeCloudResource])
	}
	uidID := idsOf(normalizeFixture(t, "api-bucket-2.json"))[identity.TypeCloudResource]
	if reflect.DeepEqual(got, uidID) {
		t.Error("a name-scoped resource must not mint the uid-keyed id")
	}
	if gr, wr := relsOf(lake[0]), relsOf(want); !reflect.DeepEqual(gr, wr) || len(gr) == 0 {
		t.Errorf("lake relationships = %v\nfixture-shaped = %v (want the belongs-to)", gr, wr)
	}
}

// TestRowToEvent_KeepsRowClass: the event takes the row's own class_uid.
func TestRowToEvent_KeepsRowClass(t *testing.T) {
	class := "3001"
	e, err := rowToEvent([]string{"class_uid"}, []*string{&class}, testTable)
	if err != nil {
		t.Fatal(err)
	}
	if e.ClassUID != 3001 || e.ClassName != "Account Change" || e.Raw["class_uid"] != int64(3001) {
		t.Errorf("class = %d %q %v", e.ClassUID, e.ClassName, e.Raw["class_uid"])
	}
}
