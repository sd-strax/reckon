package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/adapterplugin"
)

func fieldList(s *tableSchema) []string {
	var out []string
	for f := range s.fields {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// TestSchemaFromColumns: a struct yields its member paths at every depth, an
// array of structs its element members under `[]`, a map and an array of
// scalars are leaves, names are lowercased and backticks dropped, and a
// comma inside a decimal's precision does not split members.
func TestSchemaFromColumns(t *testing.T) {
	s := schemaFromColumns([]glueColumn{
		{Name: "Actor", Type: "struct<user:struct<uid:string,`Name`:string,account:struct<uid:string>>,session:struct<is_mfa:boolean>>"},
		{Name: "resources", Type: "array<struct<uid:string,owner:struct<account:struct<uid:string>>,type:string>>"},
		{Name: "unmapped", Type: "map<string,struct<x:int>>"},
		{Name: "profiles", Type: "array<string>"},
		{Name: "amount", Type: "struct<value:decimal(10,2),unit:string>"},
		{Name: "region", Type: "string"},
		{Name: "untyped", Type: ""},
	})
	want := []string{
		"actor", "actor.session", "actor.session.is_mfa", "actor.user", "actor.user.account", "actor.user.account.uid", "actor.user.name", "actor.user.uid",
		"amount", "amount.unit", "amount.value",
		"profiles", "region",
		"resources", "resources[].owner", "resources[].owner.account", "resources[].owner.account.uid", "resources[].type", "resources[].uid",
		"unmapped", "untyped",
	}
	if got := fieldList(s); !slices.Equal(got, want) {
		t.Errorf("fields =\n%q\nwant\n%q", got, want)
	}
}

// TestRecordedProfile_Fields: the recorded 2.0 table carries the fields the
// live run found (adr/0335) and lacks the ones it did not.
func TestRecordedProfile_Fields(t *testing.T) {
	s := profile20(t)
	for _, f := range []string{"time", "time_dt", "class_uid", "accountid", "region", "status", "activity_id",
		"actor.user.uid", "actor.user.name", "actor.user.credential_uid", "actor.session.issuer",
		"api.operation", "api.service.name", "resources[].uid", "resources[].type", "cloud.region", "unmapped"} {
		if !s.has(f) {
			t.Errorf("profile lacks %q", f)
		}
	}
	for _, f := range []string{"status_id", "actor.session.credential_uid", "actor.session.uid", "resources[].name", "resources[].region", "cloud.account", "unmapped.x"} {
		if s.has(f) {
			t.Errorf("profile carries %q, which the 2.0 table lacks", f)
		}
	}
	// The embedded copy the runtime falls back to is the same recording.
	emb, name, ok := profileSchema(testTable)
	if !ok || name != profileName || !reflect.DeepEqual(emb.fields, s.fields) {
		t.Errorf("profileSchema(%s) = %v %q %v", testTable, emb, name, ok)
	}
	if _, _, ok := profileSchema("custom_table"); ok {
		t.Error("a table without a known suffix matched a profile")
	}
}

// TestDegraded_Profile: against the recorded 2.0 table exactly resource by
// name and resource scope by zone, group or namespace are degraded, on both
// CloudTrail operations; the credential filter
// stays available through actor.user.credential_uid, the outcome through
// status.
func TestDegraded_Profile(t *testing.T) {
	scope := "resources[].zone, resources[].group, resources[].namespace absent"
	want := []adapterplugin.DegradedCapability{
		{Operation: opActivity, Capability: capResourceName, Reason: "resources[].name absent"},
		{Operation: opActivity, Capability: capResourceScope, Reason: scope},
		{Operation: opHistory, Capability: capResourceName, Reason: "resources[].name absent"},
		{Operation: opHistory, Capability: capResourceScope, Reason: scope},
	}
	if got := degraded(profile20(t)); !reflect.DeepEqual(got, want) {
		t.Errorf("degraded = %+v\nwant %+v", got, want)
	}
	if got := degraded(fullSchema); len(got) != 0 {
		t.Errorf("full schema degraded = %+v", got)
	}
}

// TestDegraded_Each: each capability with every candidate gone is reported
// on the operations it belongs to, the absent fields named; one candidate
// left keeps it.
func TestDegraded_Each(t *testing.T) {
	without := func(paths ...string) *tableSchema {
		s := &tableSchema{fields: map[string]bool{}}
		for f := range fullSchema.fields {
			if !slices.Contains(paths, f) {
				s.fields[f] = true
			}
		}
		return s
	}
	cases := []struct {
		drop []string
		want []adapterplugin.DegradedCapability
	}{
		{[]string{"actor.user.uid", "actor.user.name"}, nil},
		{principalFields, []adapterplugin.DegradedCapability{{Operation: opActivity, Capability: capPrincipal, Reason: "actor.user.uid, actor.session.issuer, actor.user.name absent"}}},
		{credentialFields, []adapterplugin.DegradedCapability{{Operation: opActivity, Capability: capCredential, Reason: "actor.user.credential_uid, actor.session.credential_uid absent"}}},
		{[]string{"status_id"}, nil},
		{outcomeFields, []adapterplugin.DegradedCapability{{Operation: opActivity, Capability: capOutcome, Reason: "status_id, status absent"}}},
		{eventRegionFields, []adapterplugin.DegradedCapability{{Operation: opActivity, Capability: capEventRegion, Reason: "region, cloud.region absent"}}},
		{historyFields, []adapterplugin.DegradedCapability{{Operation: opHistory, Capability: capHistory, Reason: "activity_id absent"}}},
		{[]string{resourceRegionField}, []adapterplugin.DegradedCapability{
			{Operation: opActivity, Capability: capResourceRegion, Reason: "resources[].region absent"},
			{Operation: opHistory, Capability: capResourceRegion, Reason: "resources[].region absent"},
		}},
		{[]string{"resources[].uid"}, []adapterplugin.DegradedCapability{
			{Operation: opActivity, Capability: capResourceUID, Reason: "resources[].uid absent"},
			{Operation: opHistory, Capability: capResourceUID, Reason: "resources[].uid absent"},
		}},
	}
	for _, tc := range cases {
		if got := degraded(without(tc.drop...)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("without %q: degraded = %+v\nwant %+v", tc.drop, got, tc.want)
		}
	}
}

// profileCalls are activity and history calls covering every filter the
// profile can express.
func profileCalls() []probeCall {
	from, to := time.Unix(0, 0).UTC(), time.Unix(3600, 0).UTC()
	return []probeCall{
		{cloudCall{principalName: "p", credential: "k", accountUID: "a", operation: "o", service: "s", outcome: "SUCCESS",
			resource: resourceScope{accountUID: "a"}, limit: 1, from: from, to: to}, false},
		{cloudCall{outcome: "FAILURE", resource: resourceScope{region: "r"}, limit: 1, from: from, to: to}, false},
		{cloudCall{resource: resourceScope{uid: "u", region: "r"}, limit: 1, from: from, to: to}, false},
		{cloudCall{resource: resourceScope{uid: "u"}, limit: 1, from: from, to: to}, true},
	}
}

// TestProfile_SQLNamesOnlyPresentFields: every column and nested path the
// activity, history and probe statements name against the recorded profile
// is a field of the recorded schema.
func TestProfile_SQLNamesOnlyPresentFields(t *testing.T) {
	s := profile20(t)
	var stmts []string
	for _, c := range profileCalls() {
		q, err := activityQuery(s, testDB, testTable, []string{"111122223333"}, c.call, c.history)
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		stmts = append(stmts, q.sql)
	}
	probe, err := probeQuery(s, testDB, testTable)
	if err != nil {
		t.Fatal(err)
	}
	stmts = append(stmts, probe.sql)
	named := 0
	for _, sql := range stmts {
		paths := sqlFieldPaths(sql)
		named += len(paths)
		for _, p := range paths {
			if !s.has(p) {
				t.Errorf("statement names %q, absent from the profile:\n%s", p, sql)
			}
		}
		for _, c := range projection {
			projected := strings.Contains(sql, c.expr+",") || strings.Contains(sql, c.expr+"\n")
			if projected && !s.has(c.source) {
				t.Errorf("statement projects %q, absent from the profile", c.source)
			}
		}
		for _, col := range []string{"status_id", "region"} {
			for _, w := range strings.Fields(strings.NewReplacer("(", " ", ")", " ", ",", " ").Replace(sql)) {
				if w == col && !s.has(col) {
					t.Errorf("statement names column %q, absent from the profile", col)
				}
			}
		}
	}
	if named == 0 {
		t.Fatal("no nested paths read from any statement; the test checks nothing")
	}
	if strings.Contains(probe.sql, "status_id") || strings.Contains(probe.sql, "r -> r.name") || !strings.Contains(probe.sql, "lower(status) = 'success'") {
		t.Errorf("probe against the profile:\n%s", probe.sql)
	}
}

// TestProfile_OutcomeUsesStatus: without status_id the outcome is the status
// string compared without case.
func TestProfile_OutcomeUsesStatus(t *testing.T) {
	s := profile20(t)
	for outcome, pred := range map[string]string{"SUCCESS": "lower(status) = 'success'", "FAILURE": "lower(status) = 'failure'"} {
		q, err := activityQuery(s, testDB, testTable, nil, mustCall(t, map[string]any{"outcome": outcome}), false)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(q.sql, "\n  AND "+pred+"\n") || strings.Contains(q.sql, "status_id") {
			t.Errorf("%s:\n%s", outcome, q.sql)
		}
	}
}

// TestProfile_ByNameDeclined: a resource named without its uid, on either
// operation, is declined with FALLTHROUGH naming the absent fields; nothing
// reaches Athena.
func TestProfile_ByNameDeclined(t *testing.T) {
	s := profile20(t)
	for _, tc := range []struct {
		res  map[string]any
		want string
	}{
		{map[string]any{"name": "example-bucket"}, "resource by name: resources[].name absent"},
		{map[string]any{"name": "example-bucket", "type": "AWS::S3::Bucket", "region": "us-east-1"}, "resource by name: resources[].name, resources[].region absent"},
	} {
		for _, history := range []bool{false, true} {
			_, err := activityQuery(s, testDB, testTable, nil, mustCall(t, map[string]any{"resource": tc.res}), history)
			isClass(t, err, capability.ErrFallthrough, tc.want)
		}
	}
	// A type or region scope the table cannot express declines even when
	// the name is expressible: dropping it would widen the match.
	noRegion := schemaWithout()
	delete(noRegion.fields, resourceRegionField)
	_, err := activityQuery(noRegion, testDB, testTable, nil, mustCall(t, map[string]any{"resource": map[string]any{"name": "b", "region": "us-east-1"}}), false)
	isClass(t, err, capability.ErrFallthrough, "resource region scope: resources[].region absent")
}

// TestDeclines_EachFilter: a call needing a filter whose candidates are all
// absent is declined with FALLTHROUGH naming them.
func TestDeclines_EachFilter(t *testing.T) {
	cases := []struct {
		drop    []string
		params  map[string]any
		history bool
		want    string
	}{
		{principalFields, map[string]any{"principal": map[string]any{"name": "p"}}, false, "principal filter: actor.user.uid, actor.session.issuer, actor.user.name absent"},
		{credentialFields, map[string]any{"principal": map[string]any{"credential": "k"}}, false, "credential filter"},
		{operationFields, map[string]any{"operation": "o"}, false, "operation filter: api.operation absent"},
		{serviceFields, map[string]any{"service": "s"}, false, "service filter: api.service.name absent"},
		{outcomeFields, map[string]any{"outcome": "FAILURE"}, false, "outcome filter: status_id, status absent"},
		{eventRegionFields, map[string]any{"resource": map[string]any{"region": "r"}}, false, "region filter: region, cloud.region absent"},
		{resourceUIDFields, map[string]any{"resource": map[string]any{"uid": "u"}}, true, "resource by uid: resources[].uid absent"},
		{historyFields, map[string]any{"resource": map[string]any{"uid": "u"}}, true, "resource history: activity_id absent"},
	}
	for _, tc := range cases {
		s := &tableSchema{fields: map[string]bool{}}
		for f := range fullSchema.fields {
			if !slices.Contains(tc.drop, f) {
				s.fields[f] = true
			}
		}
		_, err := activityQuery(s, testDB, testTable, nil, mustCall(t, tc.params), tc.history)
		isClass(t, err, capability.ErrFallthrough, tc.want)
		// The probe built from the same schema still renders: it plans only
		// what the table can express.
		if _, err := probeQuery(s, testDB, testTable); err != nil {
			t.Errorf("without %q: probe: %v", tc.drop, err)
		}
	}
	// ALL needs no outcome field.
	s := schemaWithout("status", "status_id")
	if _, err := activityQuery(s, testDB, testTable, nil, mustCall(t, map[string]any{"outcome": "ALL"}), false); err != nil {
		t.Errorf("outcome ALL without status: %v", err)
	}
}

// TestInvoke_ByNameDeclinedAgainstProfile: an instance whose schema is not
// readable runs on the recorded profile, and a by-name call is declined
// FALLTHROUGH before any query.
func TestInvoke_ByNameDeclinedAgainstProfile(t *testing.T) {
	f := &fakeAthena{}
	g := &fakeGlue{noSchema: true}
	b := testBridge(t, nil, f, g, &fakeConfig{})
	params, _ := json.Marshal(adapterplugin.InvokeParams{Operation: opActivity, Params: map[string]any{
		"resource": map[string]any{"name": "example-bucket"}, "window": window(),
	}})
	_, werr := b.handle(context.Background(), "invoke", params)
	if got := classOf(t, werr); got != string(capability.ErrFallthrough) || !strings.Contains(werr.Message, "resources[].name absent") {
		t.Errorf("by-name invoke = %s %q", got, werr.Message)
	}
	if len(f.started) != 0 {
		t.Error("a declined call must not reach Athena")
	}
	if len(g.calls) != 1 {
		t.Errorf("GetTable calls = %d, want one lazy discovery", len(g.calls))
	}
}

// TestInvoke_DiscoversLazilyThenCaches: an invoke before any health reads
// the schema; the next invoke reuses it; health reads it again.
func TestInvoke_DiscoversLazilyThenCaches(t *testing.T) {
	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true)}}
	g := &fakeGlue{columns: []string{"time", "time_dt", "class_uid", "accountid", "status", "api"}}
	b := testBridge(t, nil, f, g, &fakeConfig{})
	params, _ := json.Marshal(adapterplugin.InvokeParams{Operation: opActivity, Params: map[string]any{
		"operation": "PutBucketPolicy", "outcome": "FAILURE", "window": window(),
	}})
	for range 2 {
		if _, werr := b.handle(context.Background(), "invoke", params); werr != nil {
			t.Fatalf("invoke: %+v", werr)
		}
	}
	if len(g.calls) != 1 {
		t.Errorf("GetTable calls = %d, want one", len(g.calls))
	}
	sql := aws.ToString(f.started[0].QueryString)
	if !strings.HasPrefix(sql, `SELECT "time", time_dt, status, CAST(api AS JSON) AS api, class_uid, accountid AS lake_accountid`+"\n") ||
		!strings.Contains(sql, "lower(status) = 'failure'") {
		t.Errorf("sql =\n%s", sql)
	}
	b.health(context.Background())
	if len(g.calls) != 2 {
		t.Errorf("GetTable calls after health = %d, want two", len(g.calls))
	}
	// A call needing a capability the discovered table lacks is declined.
	params, _ = json.Marshal(adapterplugin.InvokeParams{Operation: opActivity, Params: map[string]any{
		"principal": map[string]any{"name": "p"}, "window": window(),
	}})
	if _, werr := b.handle(context.Background(), "invoke", params); classOf(t, werr) != string(capability.ErrFallthrough) {
		t.Errorf("principal without actor = %+v", werr)
	}
}

// TestInvoke_DiscoveryFailures: a table Glue cannot find is UNHEALTHY at
// invoke, as is an unreadable schema with no matching profile.
func TestInvoke_DiscoveryFailures(t *testing.T) {
	params, _ := json.Marshal(adapterplugin.InvokeParams{Operation: opActivity, Params: map[string]any{"window": window()}})
	missing := &fakeGlue{err: &smithy.GenericAPIError{Code: "EntityNotFoundException", Message: "Table not found"}}
	_, werr := testBridge(t, nil, &fakeAthena{}, missing, &fakeConfig{}).handle(context.Background(), "invoke", params)
	if got := classOf(t, werr); got != string(capability.ErrUnhealthy) {
		t.Errorf("missing table = %s", got)
	}
	link := &fakeGlue{
		link:      &gluetypes.TableIdentifier{CatalogId: aws.String("999988887777"), DatabaseName: aws.String(testDB), Name: aws.String("custom")},
		targetErr: &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "no access"},
	}
	_, werr = testBridge(t, map[string]any{"cloudtrail_table": "custom"}, &fakeAthena{}, link, &fakeConfig{}).handle(context.Background(), "invoke", params)
	if got := classOf(t, werr); got != string(capability.ErrUnhealthy) || !strings.Contains(werr.Message, "no recorded profile") {
		t.Errorf("unreadable, no profile = %s %q", got, werr.Message)
	}
}

// TestHealth_AgainstProfile: a lake whose schema is the recorded table is
// healthy, with resource by name degraded on both operations.
func TestHealth_AgainstProfile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "profiles", profileName, "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		StorageDescriptor struct{ Columns []glueColumn }
		PartitionKeys     []glueColumn
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	g := &fakeGlue{typed: append(rec.StorageDescriptor.Columns, rec.PartitionKeys...)}
	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true)}}
	res := testBridge(t, nil, f, g, &fakeConfig{}).health(context.Background())
	if !res.Healthy || strings.Contains(res.Message, "assumed") || !reflect.DeepEqual(res.Degraded, degraded(profile20(t))) {
		t.Errorf("health = %+v", res)
	}
	for _, p := range sqlFieldPaths(aws.ToString(f.started[0].QueryString)) {
		if !profile20(t).has(p) {
			t.Errorf("probe names %q", p)
		}
	}
}

// recordedPage is the recorded result row: each column as Athena returns it
// under CAST(… AS JSON), structs and arrays as JSON objects and lists.
func recordedPage(t *testing.T, extra map[string]any) (cols []string, row []*string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "profiles", profileName, "pages", "1.json"))
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var page map[string]any
	if err := dec.Decode(&page); err != nil {
		t.Fatal(err)
	}
	for k, v := range extra {
		page[k] = v
	}
	for name := range page {
		cols = append(cols, name)
	}
	sort.Strings(cols)
	for _, name := range cols {
		switch v := page[name].(type) {
		case nil:
			row = append(row, nil)
		case string:
			row = append(row, &v)
		case json.Number:
			s := v.String()
			row = append(row, &s)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			s := string(b)
			row = append(row, &s)
		}
	}
	return cols, row
}

// resultSource maps a result column to the table column it is projected
// from (lake_accountid is accountid); any other name is its own.
func resultSource(name string) string {
	for _, c := range projection {
		if c.name == name {
			return c.source
		}
	}
	return name
}

// TestProfile_PageDecodesAndNormalizes: the recorded row names only fields
// of the recorded schema (each result column mapped to its source column),
// decodes through rowToEvent and normalizes as API Activity: the assumed
// role's issuer as the principal, the bucket by its ARN, and the outcome
// from the status string. A page captured with the runbook's full SELECT,
// carrying time and lake_accountid, passes the same way.
func TestProfile_PageDecodesAndNormalizes(t *testing.T) {
	t.Run("recorded", func(t *testing.T) { checkProfilePage(t, nil) })
	t.Run("with time and lake_accountid", func(t *testing.T) {
		checkProfilePage(t, map[string]any{"time": json.Number("1705302805000"), lakeAccountColumn: "123456789012"})
	})
}

func checkProfilePage(t *testing.T, extra map[string]any) {
	s := profile20(t)
	cols, row := recordedPage(t, extra)
	for _, c := range cols {
		if !s.has(resultSource(c)) {
			t.Errorf("page column %q (from %q) is not in schema.json", c, resultSource(c))
		}
	}
	e, err := rowToEvent(cols, row, testTable)
	if err != nil {
		t.Fatal(err)
	}
	if e.ClassUID != 6003 || e.Time.IsZero() || e.Raw["status"] != "Success" {
		t.Errorf("event = %d %s %v", e.ClassUID, e.Time, e.Raw["status"])
	}
	if extra != nil {
		cloud, _ := e.Raw["cloud"].(map[string]any)
		account, _ := cloud["account"].(map[string]any)
		if e.Raw["time"] != int64(1705302805000) || account["uid"] != "123456789012" {
			t.Errorf("time = %v, cloud.account = %v", e.Raw["time"], cloud["account"])
		}
	}
	res := normalizeWire(t, sourceLake, []capability.OcsfPayload{e})
	if len(res) != 1 || len(res[0].ObservedData) != 1 {
		t.Fatalf("normalized = %+v", res)
	}
	ids := idsOf(res[0])
	r := identity.NewResolver(equivalenceNS)
	issuer := r.Account(identity.AccountIdentity{Type: "aws", UserID: "arn:aws:iam::123456789012:role/aws-reserved/sso.amazonaws.com/ap-south-1/AWSReservedSSO_AdministratorAccess_0123456789abcdef"})
	if !reflect.DeepEqual(ids[identity.TypeUserAccount], []string{string(issuer)}) {
		t.Errorf("principal = %v, want the issuing role %s", ids[identity.TypeUserAccount], issuer)
	}
	if len(ids[identity.TypeCloudResource]) != 1 || len(ids[identity.TypeCloudAccount]) == 0 {
		t.Errorf("ids = %v, want the bucket and its account", ids)
	}
	ext, err := json.Marshal(res[0].ObservedData[0].Extensions)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"status":"Success"`, `"operation":"PutBucketPolicy"`} {
		if !strings.Contains(string(ext), want) {
			t.Errorf("observed-data extensions lack %s: %s", want, ext)
		}
	}
}

// TestDiscover_ProfileNamedByLinkTarget: the fallback profile is named by
// the table the resource link points to, not the link's own name: a
// custom-named link to a CloudTrail 2.0 table assumes the profile, one to
// another source's table has none and is unhealthy.
func TestDiscover_ProfileNamedByLinkTarget(t *testing.T) {
	health := func(target string) adapterplugin.HealthResult {
		g := &fakeGlue{
			link:      &gluetypes.TableIdentifier{CatalogId: aws.String("999988887777"), DatabaseName: aws.String(testDB), Name: aws.String(target)},
			targetErr: &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "no access"},
		}
		b := testBridge(t, map[string]any{"cloudtrail_table": "my_trail_link"}, &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true)}}, g, &fakeConfig{})
		return b.health(context.Background())
	}
	if h := health("amazon_security_lake_table_eu_west_1_cloud_trail_mgmt_2_0"); !h.Healthy || !strings.Contains(h.Message, "assumed recorded profile "+profileName) {
		t.Errorf("link to a CloudTrail 2.0 table: %+v", h)
	}
	if h := health("amazon_security_lake_table_eu_west_1_s3_data_2_0"); h.Healthy || !strings.Contains(h.Message, "no recorded profile matches table amazon_security_lake_table_eu_west_1_s3_data_2_0") {
		t.Errorf("link to an S3 data-events table: %+v", h)
	}
	if _, _, ok := profileSchema("amazon_security_lake_table_eu_west_1_s3_data_2_0"); ok {
		t.Error("a _2_0 table of another source matched the CloudTrail profile")
	}
}

// TestPostScope_Declined: a zone, group or namespace — or a type given
// without a name — that the table does not carry declines the call
// FALLTHROUGH naming the field, rather than widening the query and
// answering empty after the fetch; a uid-identified resource ignores them.
func TestPostScope_Declined(t *testing.T) {
	s := profile20(t)
	for _, tc := range []struct {
		res          map[string]any
		want, byName string
	}{
		{map[string]any{"zone": "z"}, "resource scope by zone, group or namespace: resources[].zone absent",
			"resource by name: resources[].name, resources[].zone absent"},
		{map[string]any{"group": "g", "namespace": "n"}, "resource scope by zone, group or namespace: resources[].group, resources[].namespace absent",
			"resource by name: resources[].name, resources[].group, resources[].namespace absent"},
	} {
		_, err := activityQuery(s, testDB, testTable, nil, mustCall(t, map[string]any{"resource": tc.res}), false)
		isClass(t, err, capability.ErrFallthrough, tc.want)
		// History needs an identifier: by name, every absent field is named.
		tc.res["name"] = "b"
		_, err = activityQuery(s, testDB, testTable, nil, mustCall(t, map[string]any{"resource": tc.res}), true)
		isClass(t, err, capability.ErrFallthrough, tc.byName)
	}
	if _, err := activityQuery(s, testDB, testTable, nil, mustCall(t, map[string]any{"resource": map[string]any{"uid": "u", "zone": "z"}}), false); err != nil {
		t.Errorf("uid with a zone: %v", err)
	}
	if _, err := activityQuery(fullSchema, testDB, testTable, nil, mustCall(t, map[string]any{"resource": map[string]any{"zone": "z", "group": "g", "namespace": "n"}}), false); err != nil {
		t.Errorf("full schema with every scope: %v", err)
	}
	noType := schemaWithout()
	delete(noType.fields, resourceTypeField)
	_, err := activityQuery(noType, testDB, testTable, nil, mustCall(t, map[string]any{"resource": map[string]any{"type": "AWS::S3::Bucket"}}), false)
	isClass(t, err, capability.ErrFallthrough, "resource type scope: resources[].type absent")
}

// TestTypeScope_Declined: a resource named with a type on a table without
// resources[].type is declined, and health reports the type scope degraded
// on both CloudTrail operations.
func TestTypeScope_Declined(t *testing.T) {
	noType := schemaWithout()
	delete(noType.fields, resourceTypeField)
	for _, history := range []bool{false, true} {
		_, err := activityQuery(noType, testDB, testTable, nil, mustCall(t, map[string]any{"resource": map[string]any{"name": "b", "type": "AWS::S3::Bucket"}}), history)
		isClass(t, err, capability.ErrFallthrough, "resource type scope: resources[].type absent")
	}
	want := []adapterplugin.DegradedCapability{
		{Operation: opActivity, Capability: capResourceType, Reason: "resources[].type absent"},
		{Operation: opHistory, Capability: capResourceType, Reason: "resources[].type absent"},
	}
	if got := degraded(noType); !reflect.DeepEqual(got, want) {
		t.Errorf("degraded = %+v\nwant %+v", got, want)
	}
}

// predicateAtoms splits a WHERE predicate into the conditions it is made of:
// the any_match wrapper and grouping parentheses dropped, split on AND/OR.
func predicateAtoms(pred string) []string {
	p := strings.TrimPrefix(pred, "any_match(resources, r -> ")
	if p != pred {
		p = strings.TrimSuffix(p, ")")
	}
	if strings.HasPrefix(p, "(") && strings.HasSuffix(p, ")") && strings.Contains(p, " OR ") {
		p = p[1 : len(p)-1]
	}
	var out []string
	for _, a := range strings.Split(p, " AND ") {
		out = append(out, strings.Split(a, " OR ")...)
	}
	return out
}

// TestProbe_PlansEveryExpressiblePredicate: for the recorded profile and the
// full schema, every condition the activity and history builders emit for a
// call the table can express appears in the probe, so health plans every
// predicate a call can send.
func TestProbe_PlansEveryExpressiblePredicate(t *testing.T) {
	shapes := []map[string]any{
		{"principal": map[string]any{"name": "p", "credential": "k"}, "account": map[string]any{"uid": "a"},
			"operation": "o", "service": "s", "outcome": "FAILURE"},
		{"outcome": "SUCCESS"},
		{"resource": map[string]any{"uid": "u", "region": "r"}},
		{"resource": map[string]any{"name": "n"}},
		{"resource": map[string]any{"name": "n", "type": "t"}},
		{"resource": map[string]any{"name": "n", "region": "r"}},
		{"resource": map[string]any{"name": "n", "type": "t", "region": "r", "account_uid": "a"}},
		{"resource": map[string]any{"region": "r", "type": "t"}},
		{"resource": map[string]any{"account_uid": "a"}},
	}
	for name, s := range map[string]*tableSchema{"profile": profile20(t), "full": fullSchema} {
		probe, err := probeQuery(s, testDB, testTable)
		if err != nil {
			t.Fatal(err)
		}
		expressed := 0
		for _, params := range shapes {
			c := mustCall(t, params)
			for _, history := range []bool{false, true} {
				if history && !c.resource.given() {
					continue
				}
				q, err := activityWhere(s, probeAccounts, c, history)
				if classify(err) == capability.ErrFallthrough {
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				expressed++
				for _, pred := range q.where {
					for _, atom := range predicateAtoms(pred) {
						if !strings.Contains(probe.sql, atom) {
							t.Errorf("%s: %q (from %q, history=%v) is not in the probe:\n%s", name, atom, pred, history, probe.sql)
						}
					}
				}
			}
		}
		if expressed < 5 {
			t.Errorf("%s: only %d shapes expressible; the test covers too little", name, expressed)
		}
	}
}

// TestProfile_EventRegionIsOnePredicate: a region given without a resource
// name filters the region column the 2.0 table carries, and only it; a table
// without the column filters cloud.region instead.
func TestProfile_EventRegionIsOnePredicate(t *testing.T) {
	c := mustCall(t, map[string]any{"resource": map[string]any{"region": "ap-south-1"}})
	q, err := activityWhere(profile20(t), nil, c, false)
	if err != nil {
		t.Fatal(err)
	}
	if last := q.where[len(q.where)-1]; last != "region = ?" || strings.Contains(q.clause(), "cloud.region") {
		t.Errorf("where = %q", q.where)
	}
	q, err = activityWhere(schemaWithout("region"), nil, c, false)
	if err != nil {
		t.Fatal(err)
	}
	if last := q.where[len(q.where)-1]; last != "cloud.region = ?" || strings.Contains(q.clause(), "AND region") {
		t.Errorf("without region, where = %q", q.where)
	}
}

// TestConfigure_ClearsWithoutReading: configure reads nothing; it drops the
// schema a previous configuration held, and the next call reads the new
// table's schema.
func TestConfigure_ClearsWithoutReading(t *testing.T) {
	full := &fakeGlue{}
	statusOnly := &fakeGlue{columns: []string{"time", "time_dt", "class_uid", "accountid", "status", "api"}}
	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true)}}
	b := newBridge()
	b.pollMin, b.pollMax = time.Millisecond, 2*time.Millisecond
	b.newClients = func(s settings) clients {
		g := map[string]*fakeGlue{"table_a": full, "table_b": statusOnly}[s.table]
		return clients{athena: f, glue: g, config: &fakeConfig{}}
	}
	configure := func(table string) {
		t.Helper()
		cfg := baseConfig()
		cfg["cloudtrail_table"] = table
		raw, _ := json.Marshal(map[string]any{"config": cfg})
		if _, werr := b.configure(raw); werr != nil {
			t.Fatalf("configure %s: %+v", table, werr)
		}
	}
	configure("table_a")
	if len(full.calls) != 0 || b.schema != nil {
		t.Fatalf("configure read the schema: calls %d, schema %v", len(full.calls), b.schema)
	}
	if h := b.health(context.Background()); !h.Healthy || b.schema == nil {
		t.Fatalf("health: %+v", h)
	}
	configure("table_b")
	if b.schema != nil || len(statusOnly.calls) != 0 {
		t.Fatal("reconfigure kept the previous schema or read the new one")
	}
	params, _ := json.Marshal(adapterplugin.InvokeParams{Operation: opActivity, Params: map[string]any{"outcome": "FAILURE", "window": window()}})
	if _, werr := b.handle(context.Background(), "invoke", params); werr != nil {
		t.Fatal(werr)
	}
	if len(statusOnly.calls) != 1 || len(full.calls) != 1 {
		t.Errorf("GetTable calls a=%d b=%d, want one each", len(full.calls), len(statusOnly.calls))
	}
	if sql := aws.ToString(f.started[len(f.started)-1].QueryString); !strings.Contains(sql, "lower(status) = 'failure'") || strings.Contains(sql, "status_id") {
		t.Errorf("after reconfigure, sql =\n%s", sql)
	}
}

// TestHealth_AnyFailureDropsTheSchema: after a good schema, every way health
// can fail drops it — a throttled read, the table not found or denied, a
// core field missing, a schema unreadable with no profile, a probe the store
// rejects — and the next invoke reads the schema again, classifying its own
// read's failure (RETRY when transient, UNHEALTHY otherwise); after a
// rejected probe the read succeeds and the call's own rejected query drops
// the schema again.
func TestHealth_AnyFailureDropsTheSchema(t *testing.T) {
	link := &gluetypes.TableIdentifier{CatalogId: aws.String("999988887777"), DatabaseName: aws.String(testDB), Name: aws.String("custom_table")}
	cases := []struct {
		name      string
		breakIt   func(*fakeGlue, *fakeAthena)
		wantClass capability.ErrorClass // the invoke's own read; "" when it succeeds
	}{
		{"throttled read", func(g *fakeGlue, _ *fakeAthena) {
			g.err = &smithy.GenericAPIError{Code: "ThrottlingException", Message: "slow down"}
		}, capability.ErrRetry},
		{"not found", func(g *fakeGlue, _ *fakeAthena) {
			g.err = &smithy.GenericAPIError{Code: "EntityNotFoundException", Message: "Table not found"}
		}, capability.ErrUnhealthy},
		{"configured table denied", func(g *fakeGlue, _ *fakeAthena) {
			g.err = &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "denied"}
		}, capability.ErrUnhealthy},
		{"core missing", func(g *fakeGlue, _ *fakeAthena) {
			g.columns = []string{"time", "time_dt", "accountid", "status", "api"}
		}, capability.ErrUnhealthy},
		{"unreadable, no profile", func(g *fakeGlue, _ *fakeAthena) {
			g.link, g.targetErr = link, &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "no access"}
		}, capability.ErrUnhealthy},
		{"probe rejected", func(_ *fakeGlue, f *fakeAthena) {
			st := types.QueryExecutionStatus{StateChangeReason: aws.String("COLUMN_NOT_FOUND: Column 'unmapped' cannot be resolved")}
			f.states, f.status = []types.QueryExecutionState{types.QueryExecutionStateFailed}, &st
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &fakeGlue{}
			f := &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true)}}
			b := testBridge(t, map[string]any{"cloudtrail_table": "custom_table"}, f, g, &fakeConfig{})
			if h := b.health(context.Background()); !h.Healthy || b.schema == nil {
				t.Fatalf("first health: %+v", h)
			}
			tc.breakIt(g, f)
			if h := b.health(context.Background()); h.Healthy {
				t.Errorf("health = %+v", h)
			}
			if b.schema != nil {
				t.Fatal("the failure kept the schema")
			}
			reads := len(g.calls)
			params, _ := json.Marshal(adapterplugin.InvokeParams{Operation: opActivity, Params: map[string]any{"window": window()}})
			_, werr := b.handle(context.Background(), "invoke", params)
			if len(g.calls) == reads {
				t.Error("the invoke did not read the schema again")
			}
			if tc.wantClass == "" {
				// The read succeeds, but the store rejects the call's
				// query too, which drops the schema again.
				if werr == nil || b.schema != nil {
					t.Errorf("invoke after a rejected probe = %+v, schema %v", werr, b.schema)
				}
				return
			}
			if got := classOf(t, werr); got != string(tc.wantClass) {
				t.Errorf("invoke = %s %q, want %s", got, werr.Message, tc.wantClass)
			}
			if b.schema != nil {
				t.Error("a failed invoke read held a schema")
			}
		})
	}
}

// TestDiscover_Unreadable: a target read denied, not found, or answered 401
// or 403 with no error code (or the SDK's UnknownError) is an unreadable
// schema (the profile); a 403
// carrying a code, or a 500, is a failed read (unhealthy). A table listing
// no columns is unreadable whatever partition keys it lists.
func TestDiscover_Unreadable(t *testing.T) {
	link := &gluetypes.TableIdentifier{CatalogId: aws.String("999988887777"), DatabaseName: aws.String(testDB), Name: aws.String(testTable)}
	coded403 := &smithy.OperationError{ServiceID: "Glue", OperationName: "GetTable",
		Err: &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 403}},
			Err: &smithy.GenericAPIError{Code: "UnrecognizedClientException", Message: "bad token"}}}
	health := func(g *fakeGlue) adapterplugin.HealthResult {
		return testBridge(t, nil, &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true)}}, g, &fakeConfig{}).health(context.Background())
	}
	// A code-less JSON error body, as the SDK's awsjson deserializer
	// renders it: code UnknownError inside the 403 response error.
	unknown403 := &smithy.OperationError{ServiceID: "Glue", OperationName: "GetTable",
		Err: &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 403}},
			Err: &smithy.GenericAPIError{Code: "UnknownError", Message: "UnknownError"}}}
	for name, err := range map[string]error{
		"denied":           &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "no"},
		"not found":        &smithy.GenericAPIError{Code: "EntityNotFoundException", Message: "no"},
		"401":              statusErr(401),
		"403":              statusErr(403),
		"403 UnknownError": unknown403,
	} {
		if h := health(&fakeGlue{link: link, targetErr: err}); !h.Healthy || !strings.Contains(h.Message, "assumed recorded profile "+profileName) {
			t.Errorf("%s: %+v", name, h)
		}
	}
	for name, err := range map[string]error{"403 with a code": coded403, "500": statusErr(500)} {
		if h := health(&fakeGlue{link: link, targetErr: err}); h.Healthy || strings.Contains(h.Message, "assumed") {
			t.Errorf("%s: %+v", name, h)
		}
	}
	for name, g := range map[string]*fakeGlue{
		"no columns":                 {emptySchema: true},
		"no columns, partition keys": {emptySchema: true, partitionOnly: []string{"accountid", "region"}},
	} {
		if h := health(g); !h.Healthy || !strings.Contains(h.Message, "schema not readable (the table reports no schema); assumed recorded profile "+profileName) {
			t.Errorf("%s: %+v", name, h)
		}
	}
}

// TestSchema_IcebergNotCurrentIgnored: a column Iceberg marks no longer
// current is not a field the table carries: with status_id so marked, the
// outcome filters the status string and the projection leaves status_id
// out.
func TestSchema_IcebergNotCurrentIgnored(t *testing.T) {
	var cols []glueColumn
	for _, c := range fullColumns {
		switch c.Name {
		case "status_id":
			c.Parameters = map[string]string{"iceberg.field.current": "false"}
		case "status":
			c.Parameters = map[string]string{"iceberg.field.current": "true"}
		}
		cols = append(cols, c)
	}
	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true)}}
	b := testBridge(t, nil, f, &fakeGlue{typed: cols}, &fakeConfig{})
	params, _ := json.Marshal(adapterplugin.InvokeParams{Operation: opActivity, Params: map[string]any{"outcome": "SUCCESS", "window": window()}})
	if _, werr := b.handle(context.Background(), "invoke", params); werr != nil {
		t.Fatal(werr)
	}
	sql := aws.ToString(f.started[0].QueryString)
	if strings.Contains(sql, "status_id") || !strings.Contains(sql, "lower(status) = 'success'") {
		t.Errorf("sql =\n%s", sql)
	}
	if schemaFromColumns(cols).has("status_id") || !schemaFromColumns(cols).has("status") {
		t.Error("schemaFromColumns counted a not-current column, or dropped a current one")
	}
}

// TestInvoke_FailedQueryDropsTheSchema: a call whose query the store
// rejects drops the schema it was built from, keeping its own error class,
// and the next call reads Glue again.
func TestInvoke_FailedQueryDropsTheSchema(t *testing.T) {
	st := types.QueryExecutionStatus{StateChangeReason: aws.String("COLUMN_NOT_FOUND: line 1:8: Column 'unmapped' cannot be resolved")}
	f := &fakeAthena{states: []types.QueryExecutionState{types.QueryExecutionStateFailed}, status: &st}
	g := &fakeGlue{}
	b := testBridge(t, nil, f, g, &fakeConfig{})
	params, _ := json.Marshal(adapterplugin.InvokeParams{Operation: opActivity, Params: map[string]any{"window": window()}})
	_, werr := b.handle(context.Background(), "invoke", params)
	// The class is the query outcome's, as before: a missing column is
	// FALLTHROUGH.
	if got := classOf(t, werr); got != string(capability.ErrFallthrough) || !strings.Contains(werr.Message, "COLUMN_NOT_FOUND") {
		t.Fatalf("invoke = %s %+v", got, werr)
	}
	if b.schema != nil {
		t.Fatal("a rejected query kept the schema")
	}
	reads := len(g.calls)
	_, _ = b.handle(context.Background(), "invoke", params)
	if len(g.calls) != reads+1 {
		t.Errorf("GetTable calls = %d, want %d: the next call reads the schema again", len(g.calls), reads+1)
	}
}

// TestResourceScopes_InTheQuery: where the table carries the field, a type
// given without a name, and a zone, group or namespace with or without one,
// are conditions on the same resources[] element in the query, so its limit
// applies to matching rows; a uid ignores them.
func TestResourceScopes_InTheQuery(t *testing.T) {
	groupNameOnly := schemaWithout()
	delete(groupNameOnly.fields, "resources[].group.uid")
	cases := []struct {
		s       *tableSchema
		res     map[string]any
		history bool
		want    string
		args    []string
	}{
		{fullSchema, map[string]any{"type": "AWS::S3::Bucket"}, false, "any_match(resources, r -> r.type = ?)", []string{"'AWS::S3::Bucket'"}},
		{fullSchema, map[string]any{"zone": "z"}, false, "any_match(resources, r -> r.zone = ?)", []string{"'z'"}},
		{fullSchema, map[string]any{"group": "g"}, false, `any_match(resources, r -> (r."group".name = ? OR r."group".uid = ?))`, []string{"'g'", "'g'"}},
		{groupNameOnly, map[string]any{"group": "g"}, false, `any_match(resources, r -> r."group".name = ?)`, []string{"'g'"}},
		{fullSchema, map[string]any{"namespace": "ns"}, false, "any_match(resources, r -> r.namespace = ?)", []string{"'ns'"}},
		{fullSchema, map[string]any{"type": "t", "zone": "z", "namespace": "ns"}, false, "any_match(resources, r -> r.type = ? AND r.zone = ? AND r.namespace = ?)", []string{"'t'", "'z'", "'ns'"}},
		{fullSchema, map[string]any{"name": "n", "zone": "z", "group": "g", "namespace": "ns"}, true,
			`any_match(resources, r -> r.name = ? AND r.zone = ? AND (r."group".name = ? OR r."group".uid = ?) AND r.namespace = ?)`, []string{"'n'", "'z'", "'g'", "'g'", "'ns'"}},
		{fullSchema, map[string]any{"uid": "u", "type": "t", "zone": "z", "group": "g", "namespace": "ns"}, false, "any_match(resources, r -> r.uid = ?)", []string{"'u'"}},
	}
	for _, tc := range cases {
		q, err := activityWhere(tc.s, nil, mustCall(t, map[string]any{"resource": tc.res}), tc.history)
		if err != nil {
			t.Fatalf("%v: %v", tc.res, err)
		}
		found := false
		for i, pred := range q.where {
			if pred != tc.want {
				continue
			}
			found = true
			// The predicate's parameters follow those of the ones before it.
			n := 0
			for _, p := range q.where[:i] {
				n += strings.Count(p, "?")
			}
			if got := q.params[n : n+len(tc.args)]; !slices.Equal(got, tc.args) {
				t.Errorf("%v: args = %q, want %q", tc.res, got, tc.args)
			}
		}
		if !found {
			t.Errorf("%v: where = %q, want %q", tc.res, q.where, tc.want)
		}
	}
}

// TestResource_UIDAbsentDeclines: a resource given by its identifier on a
// table without resources[].uid is declined, on both operations, even with
// a name beside it: a name is no substitute for the identifier. The uid wins
// wherever the table carries it.
func TestResource_UIDAbsentDeclines(t *testing.T) {
	noUID := schemaWithout()
	delete(noUID.fields, "resources[].uid")
	c := mustCall(t, map[string]any{"resource": map[string]any{"uid": "arn:aws:s3:::b", "name": "b", "type": "AWS::S3::Bucket"}})
	for _, history := range []bool{false, true} {
		_, err := activityQuery(noUID, testDB, testTable, nil, c, history)
		isClass(t, err, capability.ErrFallthrough, "resource by uid: resources[].uid absent")
	}
	q, err := activityQuery(fullSchema, testDB, testTable, nil, c, false)
	if err != nil || !strings.Contains(q.sql, "any_match(resources, r -> r.uid = ?)") || strings.Contains(q.sql, "r.name") {
		t.Errorf("full schema: %v\n%s", err, q.sql)
	}
}

// TestGroupScope_ExpressibleAgrees: a group the table carries without a
// name or uid of its own (a string, or a struct of other members) cannot be
// matched: health reports the scope degraded, the probe leaves it out, and
// a call giving a group is declined, all naming the same fields.
func TestGroupScope_ExpressibleAgrees(t *testing.T) {
	for _, groupType := range []string{"string", "struct<type:string>"} {
		var cols []glueColumn
		for _, c := range fullColumns {
			if c.Name == "resources" {
				c.Type = "array<struct<uid:string,name:string,type:string,region:string,zone:string,group:" + groupType + ",namespace:string>>"
			}
			cols = append(cols, c)
		}
		s := schemaFromColumns(cols)
		if !s.has("resources[].group") || groupMissing(s) == nil {
			t.Fatalf("%s: group fields = %v", groupType, groupMissing(s))
		}
		reason := "resources[].group.name, resources[].group.uid absent"
		want := []adapterplugin.DegradedCapability{
			{Operation: opActivity, Capability: capResourceScope, Reason: reason},
			{Operation: opHistory, Capability: capResourceScope, Reason: reason},
		}
		if got := degraded(s); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: degraded = %+v\nwant %+v", groupType, got, want)
		}
		_, err := activityQuery(s, testDB, testTable, nil, mustCall(t, map[string]any{"resource": map[string]any{"name": "b", "group": "g"}}), false)
		isClass(t, err, capability.ErrFallthrough, capResourceScope+": "+reason)
		probe, err := probeQuery(s, testDB, testTable)
		if err != nil || strings.Contains(probe.sql, "group") {
			t.Errorf("%s: probe %v names the group:\n%s", groupType, err, probe.sql)
		}
		// Zone and namespace remain expressible.
		if _, err := activityQuery(s, testDB, testTable, nil, mustCall(t, map[string]any{"resource": map[string]any{"zone": "z", "namespace": "n"}}), false); err != nil {
			t.Errorf("%s: zone and namespace: %v", groupType, err)
		}
	}
}

// TestDiscover_LinkTargetRegion: a resource link whose target is in another
// Region is read in that Region; one in the adapter's own Region, or naming
// none, is read with no override.
func TestDiscover_LinkTargetRegion(t *testing.T) {
	for region, want := range map[string]string{"eu-west-1": "eu-west-1", "us-east-1": "", "": ""} {
		link := &gluetypes.TableIdentifier{CatalogId: aws.String("999988887777"), DatabaseName: aws.String(testDB), Name: aws.String(testTable)}
		if region != "" {
			link.Region = aws.String(region)
		}
		g := &fakeGlue{link: link}
		b := testBridge(t, nil, &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true)}}, g, &fakeConfig{})
		if h := b.health(context.Background()); !h.Healthy {
			t.Fatalf("%q: %+v", region, h)
		}
		if len(g.regions) != 2 || g.regions[0] != "" || g.regions[1] != want {
			t.Errorf("target Region %q: per-call regions = %q, want the target read in %q", region, g.regions, want)
		}
	}
}

// TestProfileSchema_ParsedOnceFreshEachTime: the profile is parsed once and
// each lookup returns its own tableSchema over the shared fields, so one
// lookup's assumed text never reaches another.
func TestProfileSchema_ParsedOnceFreshEachTime(t *testing.T) {
	a, _, _ := profileSchema(testTable)
	b, _, _ := profileSchema(testTable)
	if a == b {
		t.Fatal("profileSchema returned the same tableSchema twice")
	}
	a.assumed = "x"
	if b.assumed != "" {
		t.Error("one lookup's assumed text reached another")
	}
	if reflect.ValueOf(a.fields).UnsafePointer() != reflect.ValueOf(b.fields).UnsafePointer() {
		t.Error("the profile was parsed again")
	}
}
