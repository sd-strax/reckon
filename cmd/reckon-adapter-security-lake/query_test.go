package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sd-strax/reckon/capability"
)

const selectLine = `SELECT "time", time_dt, activity_id, activity_name, status, status_id, CAST(actor AS JSON) AS actor, CAST(api AS JSON) AS api, CAST(cloud AS JSON) AS cloud, CAST(resources AS JSON) AS resources, CAST(src_endpoint AS JSON) AS src_endpoint, CAST(http_request AS JSON) AS http_request, CAST(metadata AS JSON) AS metadata, CAST(unmapped AS JSON) AS unmapped, severity_id, class_uid, category_uid, type_uid, accountid AS lake_accountid`

const (
	windowPred = "time_dt BETWEEN from_iso8601_timestamp(?) AND from_iso8601_timestamp(?)"
	classPred  = "class_uid = 6003"
)

var windowParams = []string{"'2026-07-14T00:00:00Z'", "'2026-07-15T00:00:00Z'"}

func window() map[string]any {
	return map[string]any{"from": "2026-07-14T00:00:00Z", "to": "2026-07-15T00:00:00Z"}
}

func mustCall(t *testing.T, params map[string]any) cloudCall {
	t.Helper()
	if _, ok := params["window"]; !ok {
		params["window"] = window()
	}
	c, err := parseCall(params)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func wantSQL(where []string) string {
	return selectLine + "\nFROM \"" + testDB + "\".\"" + testTable + "\"\nWHERE " +
		strings.Join(where, "\n  AND ") + "\nORDER BY \"time\" DESC\nLIMIT ?"
}

func isClass(t *testing.T, err error, want capability.ErrorClass, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want %s error, got nil", want)
	}
	if got := classify(err); got != want {
		t.Fatalf("class = %s, want %s (%v)", got, want, err)
	}
	if msg != "" && !strings.Contains(err.Error(), msg) {
		t.Fatalf("error %q does not mention %q", err, msg)
	}
}

func TestActivityQuery_EachFilter(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		where  []string
		args   []string
	}{
		{"window only", map[string]any{}, nil, nil},
		{"principal", map[string]any{"principal": map[string]any{"name": "arn:aws:iam::111122223333:user/deploy-ci"}},
			[]string{"(actor.user.uid = ? OR actor.session.issuer = ? OR actor.user.name = ?)"},
			[]string{"'arn:aws:iam::111122223333:user/deploy-ci'", "'arn:aws:iam::111122223333:user/deploy-ci'", "'arn:aws:iam::111122223333:user/deploy-ci'"}},
		{"credential", map[string]any{"principal": map[string]any{"credential": "AKIAIOSFODNN7EXAMPLE"}},
			[]string{"(actor.user.credential_uid = ? OR actor.session.credential_uid = ?)"}, []string{"'AKIAIOSFODNN7EXAMPLE'", "'AKIAIOSFODNN7EXAMPLE'"}},
		{"account filters the accountid partition only", map[string]any{"account": map[string]any{"uid": "111122223333", "provider": "aws"}},
			[]string{"accountid = ?"}, []string{"'111122223333'"}},
		{"operation", map[string]any{"operation": "PutBucketPolicy"}, []string{"api.operation = ?"}, []string{"'PutBucketPolicy'"}},
		{"service", map[string]any{"service": "s3.amazonaws.com"}, []string{"api.service.name = ?"}, []string{"'s3.amazonaws.com'"}},
		{"outcome success", map[string]any{"outcome": "SUCCESS"}, []string{"status_id = 1"}, nil},
		{"outcome failure lowercase", map[string]any{"outcome": "failure"}, []string{"status_id = 2"}, nil},
		{"outcome all", map[string]any{"outcome": "ALL"}, nil, nil},
		{"resource uid", map[string]any{"resource": map[string]any{"uid": "arn:aws:s3:::acme-internal-exports"}},
			[]string{"any_match(resources, r -> r.uid = ?)"}, []string{"'arn:aws:s3:::acme-internal-exports'"}},
		{"resource name", map[string]any{"resource": map[string]any{"name": "acme-internal-exports"}},
			[]string{"any_match(resources, r -> r.name = ?)"}, []string{"'acme-internal-exports'"}},
		{"a uid is matched on the uid alone", map[string]any{"resource": map[string]any{
			"uid": "arn:aws:s3:::b", "name": "b", "account_uid": "111122223333", "region": "us-east-1", "zone": "z", "group": "g", "namespace": "ns"}},
			[]string{"any_match(resources, r -> r.uid = ?)"}, []string{"'arn:aws:s3:::b'"}},
		{"named resource under its account and region", map[string]any{"resource": map[string]any{"name": "b", "account_uid": "111122223333", "region": "us-east-1"}},
			[]string{"any_match(resources, r -> r.name = ? AND r.region = ?)", "accountid = ?", "region = ?"},
			[]string{"'b'", "'us-east-1'", "'111122223333'", "'us-east-1'"}},
		{"named resource's type on the same element", map[string]any{"resource": map[string]any{"name": "deploy", "type": "AWS::IAM::Role"}},
			[]string{"any_match(resources, r -> r.name = ? AND r.type = ?)"}, []string{"'deploy'", "'AWS::IAM::Role'"}},
		{"region without a resource", map[string]any{"operation": "RunInstances", "resource": map[string]any{"region": "eu-west-1"}},
			[]string{"api.operation = ?", "cloud.region = ?", "region = ?"}, []string{"'RunInstances'", "'eu-west-1'", "'eu-west-1'"}},
		{"quote escaped", map[string]any{"operation": "x' OR '1'='1"}, []string{"api.operation = ?"}, []string{"'x'' OR ''1''=''1'"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := activityQuery(testDB, testTable, nil, mustCall(t, tc.params), false)
			if err != nil {
				t.Fatal(err)
			}
			if want := wantSQL(append([]string{windowPred, classPred}, tc.where...)); q.sql != want {
				t.Errorf("sql =\n%s\nwant\n%s", q.sql, want)
			}
			wantArgs := append(append(append([]string{}, windowParams...), tc.args...), "200")
			if !reflect.DeepEqual(q.params, wantArgs) {
				t.Errorf("params = %q, want %q", q.params, wantArgs)
			}
			if n := strings.Count(q.sql, "?"); n != len(q.params) {
				t.Errorf("%d placeholders, %d params", n, len(q.params))
			}
		})
	}
}

// TestActivityQuery_Combined is the golden statement for a call carrying
// every filter, on an instance narrowed to two accounts.
func TestActivityQuery_Combined(t *testing.T) {
	c := mustCall(t, map[string]any{
		"verb":      "get_cloud_api_activity",
		"principal": map[string]any{"name": "deploy-ci", "credential": "AKIAIOSFODNN7EXAMPLE"},
		"resource":  map[string]any{"name": "acme-internal-exports", "provider": "aws", "type": "AWS::S3::Bucket", "account_uid": "111122223333", "region": "us-east-1"},
		"account":   map[string]any{"uid": "111122223333", "provider": "aws"},
		"operation": "PutBucketPolicy",
		"service":   "s3.amazonaws.com",
		"outcome":   "SUCCESS",
		"limit":     "50",
	})
	q, err := activityQuery(testDB, testTable, []string{"111122223333", "444455556666"}, c, false)
	if err != nil {
		t.Fatal(err)
	}
	const want = selectLine + `
FROM "amazon_security_lake_glue_db_us_east_1"."amazon_security_lake_table_us_east_1_cloud_trail_mgmt_2_0"
WHERE time_dt BETWEEN from_iso8601_timestamp(?) AND from_iso8601_timestamp(?)
  AND class_uid = 6003
  AND accountid IN (?, ?)
  AND (actor.user.uid = ? OR actor.session.issuer = ? OR actor.user.name = ?)
  AND (actor.user.credential_uid = ? OR actor.session.credential_uid = ?)
  AND accountid = ?
  AND api.operation = ?
  AND api.service.name = ?
  AND status_id = 1
  AND any_match(resources, r -> r.name = ? AND r.type = ? AND r.region = ?)
  AND accountid = ?
  AND region = ?
ORDER BY "time" DESC
LIMIT ?`
	if q.sql != want {
		t.Errorf("sql =\n%s\nwant\n%s", q.sql, want)
	}
	wantArgs := []string{
		"'2026-07-14T00:00:00Z'", "'2026-07-15T00:00:00Z'",
		"'111122223333'", "'444455556666'",
		"'deploy-ci'", "'deploy-ci'", "'deploy-ci'",
		"'AKIAIOSFODNN7EXAMPLE'", "'AKIAIOSFODNN7EXAMPLE'",
		"'111122223333'", "'PutBucketPolicy'", "'s3.amazonaws.com'",
		"'acme-internal-exports'", "'AWS::S3::Bucket'", "'us-east-1'", "'111122223333'", "'us-east-1'",
		"50",
	}
	if !reflect.DeepEqual(q.params, wantArgs) {
		t.Errorf("params = %q\nwant %q", q.params, wantArgs)
	}
}

// TestHistoryQuery is the golden resource-history statement: resource
// required, the mutating activities only, the newest limit rows (the bridge
// reverses them), and no activity filters even when given.
func TestHistoryQuery(t *testing.T) {
	c := mustCall(t, map[string]any{
		"resource":  map[string]any{"uid": "arn:aws:s3:::acme-internal-exports", "name": "acme-internal-exports"},
		"operation": "ignored",
		"limit":     float64(200),
	})
	q, err := activityQuery(testDB, testTable, []string{"111122223333"}, c, true)
	if err != nil {
		t.Fatal(err)
	}
	const want = selectLine + `
FROM "amazon_security_lake_glue_db_us_east_1"."amazon_security_lake_table_us_east_1_cloud_trail_mgmt_2_0"
WHERE time_dt BETWEEN from_iso8601_timestamp(?) AND from_iso8601_timestamp(?)
  AND class_uid = 6003
  AND accountid IN (?)
  AND any_match(resources, r -> r.uid = ?)
  AND activity_id IN (1, 3, 4)
ORDER BY "time" DESC
LIMIT ?`
	if q.sql != want {
		t.Errorf("sql =\n%s\nwant\n%s", q.sql, want)
	}
	wantArgs := []string{"'2026-07-14T00:00:00Z'", "'2026-07-15T00:00:00Z'", "'111122223333'", "'arn:aws:s3:::acme-internal-exports'", "200"}
	if !reflect.DeepEqual(q.params, wantArgs) {
		t.Errorf("params = %q, want %q", q.params, wantArgs)
	}
}

func TestHistoryQuery_RequiresResource(t *testing.T) {
	_, err := activityQuery(testDB, testTable, nil, mustCall(t, map[string]any{"resource": map[string]any{"type": "AWS::S3::Bucket"}}), true)
	isClass(t, err, capability.ErrFatal, "resource uid or name")
}

func TestParseCall_RefusesWindowless(t *testing.T) {
	for name, w := range map[string]any{
		"absent":    nil,
		"empty":     map[string]any{},
		"zero":      map[string]any{"from": "0001-01-01T00:00:00Z", "to": "0001-01-01T00:00:00Z"},
		"zero from": map[string]any{"from": "0001-01-01T00:00:00Z", "to": "2026-07-15T00:00:00Z"},
		"no from":   map[string]any{"to": "2026-07-15T00:00:00Z"},
		"garbage":   map[string]any{"from": "yesterday", "to": "2026-07-15T00:00:00Z"},
	} {
		t.Run(name, func(t *testing.T) {
			params := map[string]any{"operation": "PutBucketPolicy"}
			if w != nil {
				params["window"] = w
			}
			_, err := parseCall(params)
			isClass(t, err, capability.ErrFatal, "a control-plane query needs a window")
		})
	}
	_, err := parseCall(map[string]any{"window": map[string]any{"from": "2026-07-15T00:00:00Z", "to": "2026-07-14T00:00:00Z"}})
	isClass(t, err, capability.ErrFatal, "after")
}

// TestParseCall_HalfOpenWindow: a window with a start and no end, or the
// zero end, runs to now.
func TestParseCall_HalfOpenWindow(t *testing.T) {
	fixed := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	restore := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = restore })
	for name, w := range map[string]any{
		"no to":   map[string]any{"from": "2026-07-14T00:00:00Z"},
		"zero to": map[string]any{"from": "2026-07-14T00:00:00Z", "to": "0001-01-01T00:00:00Z"},
	} {
		c, err := parseCall(map[string]any{"window": w})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !c.to.Equal(fixed) || !c.from.Equal(time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("%s: window = %s..%s", name, c.from, c.to)
		}
	}
}

func TestParseCall_BadOutcome(t *testing.T) {
	_, err := parseCall(map[string]any{"outcome": "MAYBE", "window": window()})
	isClass(t, err, capability.ErrFatal, "outcome")
}

func TestTableRef_RejectsUnsafeNames(t *testing.T) {
	for _, tc := range [][2]string{
		{testDB, `t"; DROP TABLE x; --`},
		{testDB, "Upper_Case"},
		{testDB, "with-dash"},
		{"db.other", testTable},
		{"", testTable},
	} {
		_, err := activityQuery(tc[0], tc[1], nil, mustCall(t, map[string]any{}), false)
		if !errors.As(err, new(*classedError)) || classify(err) != capability.ErrFatal {
			t.Errorf("%q.%q: err = %v, want FATAL", tc[0], tc[1], err)
		}
	}
}

func TestParseLimit(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want int
	}{
		{nil, 200}, {"", 200}, {"200", 200}, {" 25 ", 25}, {float64(12), 12},
		{"0", 1}, {float64(-3), 1}, {"5000", 1000}, {float64(1e9), 1000},
	} {
		got, err := parseLimit(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("parseLimit(%v) = %d, %v; want %d", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []any{"lots", float64(1.5), true} {
		_, err := parseLimit(bad)
		isClass(t, err, capability.ErrFatal, "limit")
	}
}

func TestStrLit(t *testing.T) {
	if got := strLit("O'Brien"); got != "'O''Brien'" {
		t.Errorf("strLit = %s", got)
	}
}

// TestQuery_NeverFiltersCloudAccount: no account filter reads cloud.account,
// which a lake row may omit.
func TestQuery_NeverFiltersCloudAccount(t *testing.T) {
	c := mustCall(t, map[string]any{
		"account":  map[string]any{"uid": "111122223333"},
		"resource": map[string]any{"name": "b", "account_uid": "111122223333"},
	})
	for _, history := range []bool{false, true} {
		q, err := activityQuery(testDB, testTable, []string{"111122223333"}, c, history)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(q.sql, "cloud.account") {
			t.Errorf("history=%v sql filters cloud.account:\n%s", history, q.sql)
		}
	}
}

func TestRequiredColumns(t *testing.T) {
	want := []string{"time", "time_dt", "activity_id", "activity_name", "status", "status_id", "actor", "api", "cloud", "resources",
		"src_endpoint", "http_request", "metadata", "unmapped", "severity_id", "class_uid", "category_uid", "type_uid", "accountid", "region"}
	if got := requiredColumns(); !reflect.DeepEqual(got, want) {
		t.Errorf("requiredColumns = %v", got)
	}
}
