package main

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/smithy-go"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/internal/adapterplugin"
)

func TestConfigure_Defaults(t *testing.T) {
	s, err := parseSettings(baseConfig())
	if err != nil {
		t.Fatal(err)
	}
	if s.database != testDB || s.table != testTable {
		t.Errorf("defaults = %q.%q", s.database, s.table)
	}
	if s.queryTimeout != 60*time.Second || !s.configEnabled || s.accountUIDs != nil {
		t.Errorf("defaults = %+v", s)
	}
}

func TestConfigure_Values(t *testing.T) {
	cfg := baseConfig()
	cfg["region"] = "eu-west-2"
	cfg["account_uids"] = []any{"111122223333", "444455556666"}
	cfg["query_timeout"] = "PT2M30S"
	cfg["config_enabled"] = false
	cfg["role_arn"] = "arn:aws:iam::111122223333:role/reckon-reader"
	cfg["session_token"] = "token"
	s, err := parseSettings(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s.database != "amazon_security_lake_glue_db_eu_west_2" || s.table != "amazon_security_lake_table_eu_west_2_cloud_trail_mgmt_2_0" {
		t.Errorf("region defaults = %q.%q", s.database, s.table)
	}
	if !reflect.DeepEqual(s.accountUIDs, []string{"111122223333", "444455556666"}) || s.queryTimeout != 150*time.Second || s.configEnabled || s.sessionToken != "token" {
		t.Errorf("settings = %+v", s)
	}
}

func TestConfigure_Rejects(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"missing region":        func(c map[string]any) { delete(c, "region") },
		"missing workgroup":     func(c map[string]any) { delete(c, "athena_workgroup") },
		"missing output":        func(c map[string]any) { delete(c, "output_location") },
		"missing key id":        func(c map[string]any) { delete(c, "access_key_id") },
		"missing secret":        func(c map[string]any) { c["secret_access_key"] = "" },
		"bad region":            func(c map[string]any) { c["region"] = "US East" },
		"output not s3":         func(c map[string]any) { c["output_location"] = "https://bucket/" },
		"unsafe table":          func(c map[string]any) { c["cloudtrail_table"] = `t"; DROP TABLE x` },
		"unsafe database":       func(c map[string]any) { c["glue_database"] = "Db-Name" },
		"account not 12 digits": func(c map[string]any) { c["account_uids"] = []any{"1234"} },
		"accounts not a list":   func(c map[string]any) { c["account_uids"] = "111122223333" },
		"timeout not ISO":       func(c map[string]any) { c["query_timeout"] = "60s" },
		"timeout zero":          func(c map[string]any) { c["query_timeout"] = "PT0S" },
		"config_enabled string": func(c map[string]any) { c["config_enabled"] = "yes" },
		"role not an ARN":       func(c map[string]any) { c["role_arn"] = "reckon-reader" },
		"region not a string":   func(c map[string]any) { c["region"] = float64(1) },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := baseConfig()
			mutate(cfg)
			b := newBridge()
			raw, _ := json.Marshal(map[string]any{"config": cfg})
			_, werr := b.configure(raw)
			if werr == nil || werr.Code != -32602 {
				t.Fatalf("configure = %+v, want -32602", werr)
			}
			if s, _ := b.snapshot(); s != nil {
				t.Error("a rejected config must not be stored")
			}
		})
	}
}

func TestParseISODuration(t *testing.T) {
	for in, want := range map[string]time.Duration{"PT60S": time.Minute, "PT1.5S": 1500 * time.Millisecond, "PT1H": time.Hour, "P1DT2M": 24*time.Hour + 2*time.Minute} {
		got, err := parseISODuration(in)
		if err != nil || got != want {
			t.Errorf("parseISODuration(%q) = %s, %v; want %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "P", "PT", "60", "PT-1S", "1H"} {
		if _, err := parseISODuration(bad); err == nil {
			t.Errorf("parseISODuration(%q) accepted", bad)
		}
	}
}

// TestAWSConfig_OnlyConfiguredCredentials pins that credentials come from
// configure alone: an ambient key in the environment is not used.
func TestAWSConfig_OnlyConfiguredCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAAMBIENT")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ambient")
	t.Setenv("AWS_PROFILE", "ambient")
	s, err := parseSettings(baseConfig())
	if err != nil {
		t.Fatal(err)
	}
	cfg := awsConfig(s)
	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "AKIDEXAMPLE" || creds.SecretAccessKey != "secret" {
		t.Errorf("credentials = %s, want the configured key", creds.AccessKeyID)
	}
	if cfg.Region != "us-east-1" || cfg.Retryer == nil {
		t.Errorf("config = region %q retryer %v", cfg.Region, cfg.Retryer != nil)
	}

	s.roleARN = "arn:aws:iam::111122223333:role/reckon-reader"
	cache, ok := awsConfig(s).Credentials.(*aws.CredentialsCache)
	if !ok || !cache.IsCredentialsProvider(&stscreds.AssumeRoleProvider{}) {
		t.Errorf("role_arn credentials = %T, want a cached assume-role provider", awsConfig(s).Credentials)
	}
}

func TestDescribe(t *testing.T) {
	d := describe()
	var verbs []string
	for _, v := range d.Verbs {
		verbs = append(verbs, v.Verb)
	}
	if !reflect.DeepEqual(verbs, []string{verbActivity, verbHistory, verbContext}) {
		t.Errorf("verbs = %v", verbs)
	}
	catalog := capability.DefaultCatalog()
	for _, v := range d.Verbs {
		if want, _ := catalog.Descriptor(v.Verb); !reflect.DeepEqual(v, want) {
			t.Errorf("%s descriptor drifts from the catalog", v.Verb)
		}
	}
	var ops []string
	for _, o := range d.Operations {
		ops = append(ops, o.Name)
		if o.Params["type"] != "object" {
			t.Errorf("%s params schema = %v", o.Name, o.Params)
		}
	}
	if !reflect.DeepEqual(ops, []string{opActivity, opHistory, opContext}) {
		t.Errorf("operations = %v", ops)
	}
	if len(d.ActionTypes) != 0 || len(d.DefaultWriteBindings) != 0 {
		t.Error("the adapter is read-only")
	}

	props := d.ConfigSchema["properties"].(map[string]any)
	var secrets []string
	for k, v := range props {
		if v.(map[string]any)["x-secret"] == true {
			secrets = append(secrets, k)
		}
	}
	if len(secrets) != 3 || !contains(secrets, "access_key_id") || !contains(secrets, "secret_access_key") || !contains(secrets, "session_token") {
		t.Errorf("x-secret fields = %v", secrets)
	}
	if req := d.ConfigSchema["required"]; !reflect.DeepEqual(req, []any{"region", "athena_workgroup", "output_location", "access_key_id", "secret_access_key"}) {
		t.Errorf("required = %v", req)
	}
}

// TestDescribe_BindingsMatchExample pins the default bindings to the example
// tenant config, which carries the cloud scenario's full-scope shape.
func TestDescribe_BindingsMatchExample(t *testing.T) {
	cfg, err := capability.LoadTenantConfig("../../examples/adapters/aws-security-lake/tenant-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := cfg.Adapters[adapterName]
	if !ok || spec.Class != "native_api" || spec.Scope == "" {
		t.Fatalf("example instance = %+v", spec)
	}
	if _, err := parseSettings(spec.Config); err != nil {
		t.Errorf("example config does not configure: %v", err)
	}
	if !reflect.DeepEqual(spec.Reads, []string{opActivity, opHistory, opContext}) {
		t.Errorf("reads = %v", spec.Reads)
	}
	for _, rb := range describe().DefaultReadBindings {
		bs := cfg.Bindings[rb.Verb]
		if len(bs) != 1 {
			t.Fatalf("%s: example has %d bindings", rb.Verb, len(bs))
		}
		if bs[0].Adapter != rb.Adapter || bs[0].Operation != rb.Operation || bs[0].Priority != rb.Priority {
			t.Errorf("%s: example binding = %+v, describe = %+v", rb.Verb, bs[0], rb)
		}
		if !reflect.DeepEqual(bs[0].Params, rb.Params) {
			t.Errorf("%s: params differ\nexample  %v\ndescribe %v", rb.Verb, bs[0].Params, rb.Params)
		}
	}
	// The resource carries every scope key, the provider lowercased.
	res := describe().DefaultReadBindings[0].Params["resource"].(map[string]any)
	if len(res) != 9 || res["provider"] != "${entity.cloud_resource.provider? | lower}" {
		t.Errorf("resource binding = %v", res)
	}
}

func TestHealth(t *testing.T) {
	if h := newBridge().health(context.Background()); h.Healthy || h.Message != "not configured" {
		t.Errorf("unconfigured health = %+v", h)
	}

	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true, "1")}}
	g := &fakeGlue{}
	if h := testBridge(t, nil, f, g, &fakeConfig{}).health(context.Background()); !h.Healthy {
		t.Errorf("health = %+v", h)
	}
	if aws.ToString(f.started[0].QueryString) != wantProbe || aws.ToString(f.started[0].WorkGroup) != "reckon" {
		t.Errorf("health query = %+v", f.started[0])
	}
	if aws.ToString(g.calls[0].DatabaseName) != testDB || aws.ToString(g.calls[0].Name) != testTable {
		t.Errorf("GetTable = %+v", g.calls[0])
	}

	denied := &fakeAthena{startErr: apiErr("AccessDeniedException")}
	h := testBridge(t, nil, denied, &fakeGlue{}, &fakeConfig{}).health(context.Background())
	if h.Healthy || !strings.Contains(h.Message, "AccessDeniedException: ") {
		t.Errorf("denied health = %+v", h)
	}

	missing := &fakeGlue{err: &smithy.GenericAPIError{Code: "EntityNotFoundException", Message: "Table not found"}}
	h = testBridge(t, nil, &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true, "1")}}, missing, &fakeConfig{}).health(context.Background())
	if h.Healthy || !strings.HasPrefix(h.Message, "glue: EntityNotFoundException") {
		t.Errorf("missing-table health = %+v", h)
	}
}

func TestInvoke_NotConfiguredAndUnknown(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"operation": opActivity, "params": map[string]any{}})
	_, werr := newBridge().invoke(context.Background(), raw)
	if got := classOf(t, werr); got != string(capability.ErrUnhealthy) {
		t.Errorf("unconfigured class = %s", got)
	}
	b := testBridge(t, nil, &fakeAthena{}, &fakeGlue{}, &fakeConfig{})
	raw, _ = json.Marshal(map[string]any{"operation": "cloudtrail.drop_tables", "params": map[string]any{}})
	_, werr = b.invoke(context.Background(), raw)
	if got := classOf(t, werr); got != string(capability.ErrFallthrough) {
		t.Errorf("unknown operation class = %s", got)
	}
}

func TestInvoke_WindowlessIsFatal(t *testing.T) {
	f := &fakeAthena{}
	b := testBridge(t, nil, f, &fakeGlue{}, &fakeConfig{})
	raw, _ := json.Marshal(map[string]any{"operation": opActivity, "params": map[string]any{
		"operation": "PutBucketPolicy", "window": map[string]any{"from": "0001-01-01T00:00:00Z", "to": "0001-01-01T00:00:00Z"},
	}})
	_, werr := b.invoke(context.Background(), raw)
	if got := classOf(t, werr); got != string(capability.ErrFatal) || !strings.Contains(werr.Message, "needs a window") {
		t.Errorf("windowless = %s %q", got, werr.Message)
	}
	if len(f.started) != 0 {
		t.Error("a windowless call must not reach Athena")
	}
}

// TestHandle_ActivityEndToEnd drives the protocol method with params in the
// shape the binding renders (strings, the injected verb, an empty principal
// object) and checks the statement Athena receives and the events returned.
func TestHandle_ActivityEndToEnd(t *testing.T) {
	rr := loadRawResponse(t, "put-bucket-policy.json")
	f := &fakeAthena{
		states: []types.QueryExecutionState{types.QueryExecutionStateRunning, types.QueryExecutionStateSucceeded},
		pages:  rr.Pages,
	}
	b := testBridge(t, map[string]any{"account_uids": []any{"111122223333"}}, f, &fakeGlue{}, &fakeConfig{})
	params, _ := json.Marshal(adapterplugin.InvokeParams{Operation: opActivity, Params: map[string]any{
		"verb":      verbActivity,
		"principal": map[string]any{},
		"resource":  map[string]any{"uid": "arn:aws:s3:::acme-internal-exports", "provider": "aws"},
		"account":   map[string]any{"provider": "aws"},
		"operation": "PutBucketPolicy",
		"outcome":   "ALL",
		"limit":     "200",
		"window":    window(),
	}})
	res, werr := b.handle(context.Background(), "invoke", params)
	if werr != nil {
		t.Fatalf("invoke: %+v", werr)
	}
	out := res.(invokeResult)
	if out.SourceTool != "aws-security-lake" || len(out.Events) != 1 || out.Events[0].ClassUID != 6003 {
		t.Errorf("result = %s %d", out.SourceTool, len(out.Events))
	}
	in := f.started[0]
	sql := aws.ToString(in.QueryString)
	for _, want := range []string{"accountid IN (?)", "api.operation = ?", "any_match(resources, r -> r.uid = ?)", "ORDER BY \"time\" DESC"} {
		if !strings.Contains(sql, want) {
			t.Errorf("sql lacks %q:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "status_id =") {
		t.Error("outcome ALL must not filter on status")
	}
	wantArgs := []string{"'2026-07-14T00:00:00Z'", "'2026-07-15T00:00:00Z'", "'111122223333'", "'PutBucketPolicy'", "'arn:aws:s3:::acme-internal-exports'", "200"}
	if !reflect.DeepEqual(in.ExecutionParameters, wantArgs) {
		t.Errorf("params = %q", in.ExecutionParameters)
	}
}

func TestHandle_HistoryWithoutResourceIsFatal(t *testing.T) {
	b := testBridge(t, nil, &fakeAthena{}, &fakeGlue{}, &fakeConfig{})
	params, _ := json.Marshal(adapterplugin.InvokeParams{Operation: opHistory, Params: map[string]any{"resource": map[string]any{}, "window": window()}})
	_, werr := b.handle(context.Background(), "invoke", params)
	if got := classOf(t, werr); got != string(capability.ErrFatal) {
		t.Errorf("class = %s", got)
	}
}

func TestHandle_Initialize(t *testing.T) {
	res, werr := newBridge().handle(context.Background(), "initialize", []byte(`{}`))
	if werr != nil {
		t.Fatal(werr)
	}
	if r := res.(adapterplugin.InitializeResult); r.Concurrency != 1 || r.ProtocolVersion != adapterplugin.ProtocolVersion {
		t.Errorf("initialize = %+v", r)
	}
	if _, werr := newBridge().handle(context.Background(), "dispatch", nil); werr == nil || werr.Code != -32601 {
		t.Errorf("dispatch = %+v, want method not found", werr)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestHandle_HistoryOldestFirst: history selects the newest rows and returns
// them oldest first.
func TestHandle_HistoryOldestFirst(t *testing.T) {
	newer := loadRawResponse(t, "put-bucket-policy.json").Pages[0]             // 09:28
	older := loadRawResponse(t, "put-bucket-versioning-by-name.json").Pages[0] // 09:30
	newer.ResultSet.Rows = append(newer.ResultSet.Rows, older.ResultSet.Rows[1])
	// The page carries the rows as Athena returns them for DESC: 09:30 first.
	newer.ResultSet.Rows[1], newer.ResultSet.Rows[2] = newer.ResultSet.Rows[2], newer.ResultSet.Rows[1]
	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{newer}}
	b := testBridge(t, nil, f, &fakeGlue{}, &fakeConfig{})
	out := invokeOK(t, b, opHistory, map[string]any{"resource": map[string]any{"name": "acme-internal-exports"}, "window": window()})
	if len(out.Events) != 2 || !out.Events[0].Time.Before(out.Events[1].Time) {
		t.Fatalf("events = %+v, want two oldest first", out.Events)
	}
	if !strings.Contains(aws.ToString(f.started[0].QueryString), `ORDER BY "time" DESC`) {
		t.Error("history must select the newest rows")
	}
}

// TestHandle_AccountRowWithoutCloudAccount: a lake row that carries its
// account only in the partition (no cloud.account) answers an account
// question: the query filters the accountid partition, nothing after the
// fetch reads cloud.account, and the event's account is the partition's.
func TestHandle_AccountRowWithoutCloudAccount(t *testing.T) {
	pg := loadRawResponse(t, "put-bucket-policy.json").Pages[0]
	cols := rowValues(pg.ResultSet.Rows[0])
	cloud := slices.Index(cols, "cloud")
	pg.ResultSet.Rows[1].Data[cloud] = datum(`{"provider":"AWS","region":"us-east-1","account":null}`)
	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{pg}}
	b := testBridge(t, nil, f, &fakeGlue{}, &fakeConfig{})
	out := invokeOK(t, b, opActivity, map[string]any{"account": map[string]any{"uid": "111122223333", "provider": "aws"}, "window": window()})
	if len(out.Events) != 1 {
		t.Fatalf("events = %d, want the row", len(out.Events))
	}
	// The row carried no cloud.account; the partition supplied it.
	if got := out.Events[0].Raw["cloud"].(map[string]any)["account"].(map[string]any)["uid"]; got != "111122223333" {
		t.Errorf("cloud.account.uid = %v, want the partition's", got)
	}
	sql := aws.ToString(f.started[0].QueryString)
	if !strings.Contains(sql, "accountid = ?") || strings.Contains(sql, "cloud.account") {
		t.Errorf("sql =\n%s", sql)
	}
}

// TestHealth_TableSchema: a table missing a core field is unhealthy, the
// field named; one missing a column only some reads use is healthy, the
// column left out of the projection; the complete schema is healthy with
// nothing degraded.
func TestHealth_TableSchema(t *testing.T) {
	ok := testBridge(t, nil, &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true, "1")}}, &fakeGlue{}, &fakeConfig{})
	if h := ok.health(context.Background()); !h.Healthy || len(h.Degraded) != 0 {
		t.Errorf("complete schema: %+v", h)
	}
	without := func(drop string) []string {
		var cols []string
		for _, c := range fullColumns {
			if c.Name != drop {
				cols = append(cols, c.Name)
			}
		}
		return cols
	}
	b := testBridge(t, nil, &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true, "1")}}, &fakeGlue{columns: without("class_uid")}, &fakeConfig{})
	if h := b.health(context.Background()); h.Healthy || !strings.Contains(h.Message, "lacks the core field(s) every query reads: class_uid") {
		t.Errorf("missing class_uid: %+v", h)
	}
	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true, "1")}}
	b = testBridge(t, nil, f, &fakeGlue{columns: without("unmapped")}, &fakeConfig{})
	if h := b.health(context.Background()); !h.Healthy || len(h.Degraded) != 0 {
		t.Errorf("missing unmapped: %+v", h)
	}
	if sql := aws.ToString(f.started[0].QueryString); strings.Contains(sql, "unmapped") {
		t.Errorf("probe projects a column the table lacks:\n%s", sql)
	}
}

// TestHealth_ResourceLink: the configured table is a Lake Formation resource
// link; its target's schema is the one read. An unreadable schema is the
// recorded profile for the table's suffix, said in the message, with the
// profile's degradations; with no matching profile it is unhealthy.
func TestHealth_ResourceLink(t *testing.T) {
	target := &gluetypes.TableIdentifier{CatalogId: aws.String("999988887777"), DatabaseName: aws.String("amazon_security_lake_glue_db_us_east_1"), Name: aws.String(testTable)}
	health := func(g *fakeGlue, extra map[string]any) adapterplugin.HealthResult {
		b := testBridge(t, extra, &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true, "1")}}, g, &fakeConfig{})
		return b.health(context.Background())
	}

	g := &fakeGlue{link: target}
	if h := health(g, nil); !h.Healthy || strings.Contains(h.Message, "assumed") || len(h.Degraded) != 0 {
		t.Errorf("link to a full schema: %+v", h)
	}
	if len(g.calls) != 2 || aws.ToString(g.calls[1].CatalogId) != "999988887777" ||
		aws.ToString(g.calls[1].DatabaseName) != "amazon_security_lake_glue_db_us_east_1" || aws.ToString(g.calls[1].Name) != testTable {
		t.Errorf("GetTable calls = %+v, want the link then its target", g.calls)
	}

	var cols []string
	for _, c := range fullColumns {
		if c.Name != "time" {
			cols = append(cols, c.Name)
		}
	}
	if h := health(&fakeGlue{link: target, columns: cols}, nil); h.Healthy || !strings.Contains(h.Message, "core field(s) every query reads: time") {
		t.Errorf("link to a schema without time: %+v", h)
	}

	denied := &fakeGlue{link: target, targetErr: &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "no access to the producer's catalog"}}
	h := health(denied, nil)
	if !h.Healthy || !strings.Contains(h.Message, "schema not readable (resource link target") || !strings.Contains(h.Message, "AccessDeniedException") ||
		!strings.Contains(h.Message, "assumed recorded profile "+profileName) {
		t.Errorf("link with a denied target: %+v", h)
	}
	if !reflect.DeepEqual(h.Degraded, degraded(profile20(t))) || len(h.Degraded) == 0 {
		t.Errorf("assumed profile degraded = %+v, want the profile's", h.Degraded)
	}
	if h := health(&fakeGlue{noSchema: true}, nil); !h.Healthy || !strings.Contains(h.Message, "schema not readable (the table reports no schema); assumed recorded profile "+profileName) {
		t.Errorf("no schema: %+v", h)
	}
	if h := health(&fakeGlue{noSchema: true}, map[string]any{"cloudtrail_table": "custom_cloud_trail_3_0"}); h.Healthy || !strings.Contains(h.Message, "no recorded profile matches table custom_cloud_trail_3_0") {
		t.Errorf("no schema, no profile: %+v", h)
	}
}

// wantProbe is health's probe against the full schema: the projection and
// the predicates of every query shape — activity with every filter and a
// named, fully scoped resource, event region, an unnamed scoped resource,
// activity by uid, history by uid and by name — behind false.
const wantProbe = selectLine + `
FROM "amazon_security_lake_glue_db_us_east_1"."amazon_security_lake_table_us_east_1_cloud_trail_mgmt_2_0"
WHERE false
  AND (time_dt BETWEEN from_iso8601_timestamp(?) AND from_iso8601_timestamp(?)
  AND class_uid = 6003
  AND accountid IN (?)
  AND (actor.user.uid = ? OR actor.session.issuer = ? OR actor.user.name = ?)
  AND (actor.user.credential_uid = ? OR actor.session.credential_uid = ?)
  AND accountid = ?
  AND api.operation = ?
  AND api.service.name = ?
  AND status_id = 1
  AND any_match(resources, r -> r.name = ? AND r.type = ? AND r.region = ? AND r.zone = ? AND (r."group".name = ? OR r."group".uid = ?) AND r.namespace = ?)
  AND accountid = ?
  AND region = ?)
  AND (time_dt BETWEEN from_iso8601_timestamp(?) AND from_iso8601_timestamp(?)
  AND class_uid = 6003
  AND accountid IN (?)
  AND region = ?)
  AND (time_dt BETWEEN from_iso8601_timestamp(?) AND from_iso8601_timestamp(?)
  AND class_uid = 6003
  AND accountid IN (?)
  AND any_match(resources, r -> r.type = ? AND r.zone = ? AND (r."group".name = ? OR r."group".uid = ?) AND r.namespace = ?))
  AND (time_dt BETWEEN from_iso8601_timestamp(?) AND from_iso8601_timestamp(?)
  AND class_uid = 6003
  AND accountid IN (?)
  AND status_id = 2
  AND any_match(resources, r -> r.uid = ?))
  AND (time_dt BETWEEN from_iso8601_timestamp(?) AND from_iso8601_timestamp(?)
  AND class_uid = 6003
  AND accountid IN (?)
  AND any_match(resources, r -> r.uid = ?)
  AND activity_id IN (1, 3, 4))
  AND (time_dt BETWEEN from_iso8601_timestamp(?) AND from_iso8601_timestamp(?)
  AND class_uid = 6003
  AND accountid IN (?)
  AND any_match(resources, r -> r.name = ? AND r.type = ? AND r.region = ? AND r.zone = ? AND (r."group".name = ? OR r."group".uid = ?) AND r.namespace = ?)
  AND accountid = ?
  AND region = ?
  AND activity_id IN (1, 3, 4))
LIMIT 0`

// TestHealth_ProbeSQL: health sends exactly the probe, one literal per
// placeholder.
func TestHealth_ProbeSQL(t *testing.T) {
	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true)}}
	b := testBridge(t, nil, f, &fakeGlue{}, &fakeConfig{})
	if h := b.health(context.Background()); !h.Healthy || strings.Contains(h.Message, "skipped") {
		t.Errorf("probe ok, schema complete: %+v", h)
	}
	if len(f.started) != 1 {
		t.Fatalf("queries = %d, want one probe", len(f.started))
	}
	in := f.started[0]
	if got := aws.ToString(in.QueryString); got != wantProbe {
		t.Errorf("probe =\n%s\nwant\n%s", got, wantProbe)
	}
	epoch, acct, v := "'1970-01-01T00:00:00Z'", "'000000000000'", "'probe'"
	vs := func(n int) []string { return slices.Repeat([]string{v}, n) }
	window := []string{epoch, epoch, acct}
	want := slices.Concat(
		window, vs(17), // every filter; the named resource with all its scopes, account and region
		window, vs(1), // event region
		window, vs(5), // unnamed resource scoped by type, zone, group (name or uid) and namespace
		window, vs(1), // activity by uid
		window, vs(1), // history by uid
		window, vs(9), // history by name with every scope, account and region
	)
	if !reflect.DeepEqual(in.ExecutionParameters, want) {
		t.Errorf("probe params = %q\nwant %q", in.ExecutionParameters, want)
	}
	if n := strings.Count(wantProbe, "?"); n != len(want) {
		t.Errorf("%d placeholders, %d params", n, len(want))
	}
}

// columnPathRE finds the column paths in a predicate: dotted identifiers
// and bare names.
var columnPathRE = regexp.MustCompile(`[a-z_][a-z0-9_]*(?:\.[a-z_][a-z0-9_]*)*`)

// predicateWords are the SQL words and functions in predicates, not columns.
var predicateWords = map[string]bool{
	"and": true, "or": true, "in": true, "between": true, "r": true,
	"from_iso8601_timestamp": true, "any_match": true,
}

// TestProbe_PlansEveryPredicateColumn: every column path any activity or
// history query shape filters on appears in the probe, so health plans every
// column a call can read.
func TestProbe_PlansEveryPredicateColumn(t *testing.T) {
	probe, err := probeQuery(fullSchema, testDB, testTable)
	if err != nil {
		t.Fatal(err)
	}
	shapes := []map[string]any{
		{"principal": map[string]any{"name": "p", "credential": "k"}, "account": map[string]any{"uid": "a"},
			"operation": "o", "service": "s", "outcome": "FAILURE"},
		{"outcome": "SUCCESS"},
		{"resource": map[string]any{"uid": "u", "region": "r", "zone": "z"}},
		{"resource": map[string]any{"name": "n"}},
		{"resource": map[string]any{"name": "n", "type": "t", "region": "r", "account_uid": "a"}},
		{"resource": map[string]any{"region": "r", "type": "t"}},
	}
	seen := map[string]bool{}
	for _, params := range shapes {
		c := mustCall(t, params)
		for _, history := range []bool{false, true} {
			if history && !c.resource.given() {
				continue
			}
			q, err := activityWhere(fullSchema, []string{"1", "2"}, c, history)
			if err != nil {
				t.Fatal(err)
			}
			for _, pred := range q.where {
				for _, path := range columnPathRE.FindAllString(pred, -1) {
					if predicateWords[path] || seen[path] {
						continue
					}
					seen[path] = true
					if !strings.Contains(probe.sql, path) {
						t.Errorf("column %q (from %q) is not in the probe", path, pred)
					}
				}
			}
		}
	}
	for _, want := range []string{"region", "actor.session.credential_uid", "r.uid", "r.type", "activity_id", "status_id"} {
		if !seen[want] {
			t.Errorf("shapes never produced %q; the test does not cover it", want)
		}
	}
}

// TestHealth_ProbeCatchesColumnBehindUnreadableLink: the link's target
// schema cannot be read, so the recorded profile is assumed — but the probe
// built from it fails on a column the table lacks, and health is unhealthy.
func TestHealth_ProbeCatchesColumnBehindUnreadableLink(t *testing.T) {
	st := types.QueryExecutionStatus{StateChangeReason: aws.String("COLUMN_NOT_FOUND: line 1:250: Column 'unmapped' cannot be resolved")}
	f := &fakeAthena{states: []types.QueryExecutionState{types.QueryExecutionStateFailed}, status: &st}
	g := &fakeGlue{
		link:      &gluetypes.TableIdentifier{CatalogId: aws.String("999988887777"), DatabaseName: aws.String(testDB), Name: aws.String(testTable)},
		targetErr: &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "no access"},
	}
	h := testBridge(t, nil, f, g, &fakeConfig{}).health(context.Background())
	if h.Healthy || !strings.Contains(h.Message, "COLUMN_NOT_FOUND") || !strings.Contains(h.Message, "unmapped") {
		t.Errorf("health = %+v, want unhealthy naming the missing column", h)
	}
}

// TestHealth_ProbeFailuresAreUnhealthy: denial and throttling on the probe
// are unhealthy with their reason; health has no retry.
func TestHealth_ProbeFailuresAreUnhealthy(t *testing.T) {
	for code, f := range map[string]*fakeAthena{
		"AccessDeniedException": {startErr: apiErr("AccessDeniedException")},
		"ThrottlingException":   {startErr: apiErr("ThrottlingException")},
		"query_timeout":         {states: []types.QueryExecutionState{types.QueryExecutionStateRunning}},
	} {
		b := testBridge(t, map[string]any{"query_timeout": "PT0.05S"}, f, &fakeGlue{}, &fakeConfig{})
		if h := b.health(context.Background()); h.Healthy || !strings.Contains(h.Message, code) {
			t.Errorf("%s: health = %+v", code, h)
		}
	}
}
