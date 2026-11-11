package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"
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
	cfg, err := capability.LoadTenantConfig("../../examples/adapters/security-lake/tenant-config.yaml")
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
	if aws.ToString(f.started[0].QueryString) != "SELECT 1" || aws.ToString(f.started[0].WorkGroup) != "reckon" {
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
	if out.SourceTool != "security-lake" || len(out.Events) != 1 || out.Events[0].ClassUID != 6003 {
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
