package main

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"

	"github.com/sd-strax/reckon/capability"
)

func datum(v string) types.Datum { return types.Datum{VarCharValue: aws.String(v)} }

// page builds a GetQueryResults page of single-column rows, with the header
// row when header is true.
func page(header bool, vals ...string) athena.GetQueryResultsOutput {
	var rows []types.Row
	if header {
		rows = append(rows, types.Row{Data: []types.Datum{datum("n")}})
	}
	for _, v := range vals {
		rows = append(rows, types.Row{Data: []types.Datum{datum(v)}})
	}
	return athena.GetQueryResultsOutput{ResultSet: &types.ResultSet{
		Rows:              rows,
		ResultSetMetadata: &types.ResultSetMetadata{ColumnInfo: []types.ColumnInfo{{Name: aws.String("n"), Label: aws.String("n"), Type: aws.String("varchar")}}},
	}}
}

func values(rs resultSet) []string {
	var out []string
	for _, r := range rs.rows {
		out = append(out, aws.ToString(r[0]))
	}
	return out
}

func TestRun_PollsUntilSucceeded(t *testing.T) {
	f := &fakeAthena{
		states: []types.QueryExecutionState{types.QueryExecutionStateQueued, types.QueryExecutionStateRunning, types.QueryExecutionStateSucceeded},
		pages:  []athena.GetQueryResultsOutput{page(true, "a", "b")},
	}
	q := sqlQuery{sql: "SELECT ? AS n", params: []string{"'x'"}}
	rs, err := testRunner(f, time.Second).run(context.Background(), q, 10)
	if err != nil {
		t.Fatal(err)
	}
	if f.polls != 3 {
		t.Errorf("polls = %d, want 3", f.polls)
	}
	in := f.started[0]
	if aws.ToString(in.QueryString) != q.sql || !reflect.DeepEqual(in.ExecutionParameters, q.params) ||
		aws.ToString(in.WorkGroup) != "reckon" || aws.ToString(in.ResultConfiguration.OutputLocation) != "s3://results/" {
		t.Errorf("start input = %+v", in)
	}
	if got := values(rs); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("rows = %q, want the header dropped", got)
	}
	if !reflect.DeepEqual(rs.columns, []string{"n"}) {
		t.Errorf("columns = %q", rs.columns)
	}
}

func TestRun_NoParamsSendsNone(t *testing.T) {
	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true)}}
	if _, err := testRunner(f, time.Second).run(context.Background(), sqlQuery{sql: "SELECT 1"}, 1); err != nil {
		t.Fatal(err)
	}
	if f.started[0].ExecutionParameters != nil {
		t.Errorf("ExecutionParameters = %q, want none", f.started[0].ExecutionParameters)
	}
}

func TestRun_FailedQuery(t *testing.T) {
	cases := []struct {
		name   string
		status types.QueryExecutionStatus
		want   capability.ErrorClass
	}{
		{"syntax error is FATAL", types.QueryExecutionStatus{
			StateChangeReason: aws.String("SYNTAX_ERROR: line 3:7: mismatched input 'FORM'. Expecting: 'FROM'"),
			AthenaError:       &types.AthenaError{ErrorCategory: aws.Int32(2), ErrorType: aws.Int32(1301)},
		}, capability.ErrFatal},
		{"missing column is FALLTHROUGH", types.QueryExecutionStatus{
			StateChangeReason: aws.String("COLUMN_NOT_FOUND: line 1:8: Column 'actor' cannot be resolved"),
			AthenaError:       &types.AthenaError{ErrorCategory: aws.Int32(2)},
		}, capability.ErrFallthrough},
		{"type mismatch is FALLTHROUGH", types.QueryExecutionStatus{
			StateChangeReason: aws.String("TYPE_MISMATCH: line 4:5: Cannot apply operator: timestamp(3) = varchar"),
		}, capability.ErrFallthrough},
		{"schema mismatch is FALLTHROUGH", types.QueryExecutionStatus{
			StateChangeReason: aws.String("HIVE_PARTITION_SCHEMA_MISMATCH: There is a mismatch between the table and partition schemas"),
		}, capability.ErrFallthrough},
		{"missing table is UNHEALTHY", types.QueryExecutionStatus{
			StateChangeReason: aws.String("TABLE_NOT_FOUND: line 2:6: Table 'awsdatacatalog.db.t' does not exist"),
			AthenaError:       &types.AthenaError{ErrorCategory: aws.Int32(2)},
		}, capability.ErrUnhealthy},
		{"lake formation denial is UNHEALTHY", types.QueryExecutionStatus{
			StateChangeReason: aws.String("Insufficient Lake Formation permission(s) on amazon_security_lake_table"),
		}, capability.ErrUnhealthy},
		{"system error is RETRY", types.QueryExecutionStatus{
			StateChangeReason: aws.String("GENERIC_INTERNAL_ERROR: internal"),
			AthenaError:       &types.AthenaError{ErrorCategory: aws.Int32(1)},
		}, capability.ErrRetry},
		{"retryable is RETRY", types.QueryExecutionStatus{
			StateChangeReason: aws.String("Query exhausted resources at this scale factor"),
			AthenaError:       &types.AthenaError{ErrorCategory: aws.Int32(2), Retryable: true},
		}, capability.ErrRetry},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.status
			f := &fakeAthena{states: []types.QueryExecutionState{types.QueryExecutionStateRunning, types.QueryExecutionStateFailed}, status: &st}
			_, err := testRunner(f, time.Second).run(context.Background(), sqlQuery{sql: "SELECT 1"}, 1)
			isClass(t, err, tc.want, aws.ToString(tc.status.StateChangeReason))
			if len(f.fetches) != 0 {
				t.Error("a failed query must not be fetched")
			}
			if len(f.stopped) != 0 {
				t.Errorf("stopped = %q, want a FAILED query left alone", f.stopped)
			}
		})
	}
}

func TestRun_CancelledIsRetry(t *testing.T) {
	f := &fakeAthena{states: []types.QueryExecutionState{types.QueryExecutionStateCancelled}}
	_, err := testRunner(f, time.Second).run(context.Background(), sqlQuery{sql: "SELECT 1"}, 1)
	isClass(t, err, capability.ErrRetry, "cancelled")
	if len(f.stopped) != 0 {
		t.Errorf("stopped = %q, want a CANCELLED query left alone", f.stopped)
	}
}

// TestRun_PollErrorStopsQuery: a transport error while polling leaves the
// query's state unknown, so it is stopped.
func TestRun_PollErrorStopsQuery(t *testing.T) {
	f := &fakeAthena{pollErr: statusErr(503), executeID: "q-poll"}
	_, err := testRunner(f, time.Second).run(context.Background(), sqlQuery{sql: "SELECT 1"}, 1)
	isClass(t, err, capability.ErrRetry, "")
	if !reflect.DeepEqual(f.stopped, []string{"q-poll"}) {
		t.Errorf("stopped = %q", f.stopped)
	}
}

func TestRun_TimeoutStopsQuery(t *testing.T) {
	f := &fakeAthena{states: []types.QueryExecutionState{types.QueryExecutionStateRunning}, executeID: "slow-1"}
	start := time.Now()
	_, err := testRunner(f, 30*time.Millisecond).run(context.Background(), sqlQuery{sql: "SELECT 1"}, 1)
	isClass(t, err, capability.ErrRetry, "query_timeout")
	if !reflect.DeepEqual(f.stopped, []string{"slow-1"}) {
		t.Errorf("stopped = %q, want the query stopped", f.stopped)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("the timeout did not bound the poll")
	}
}

func TestRun_PagingStopsAtLimit(t *testing.T) {
	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true, "1", "2"), page(false, "3", "4"), page(false, "5", "6")}}
	rs, err := testRunner(f, time.Second).run(context.Background(), sqlQuery{sql: "SELECT 1"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := values(rs); !reflect.DeepEqual(got, []string{"1", "2", "3"}) {
		t.Errorf("rows = %q", got)
	}
	if len(f.fetches) != 2 {
		t.Errorf("fetched %d pages, want 2", len(f.fetches))
	}
	for i, in := range f.fetches {
		if aws.ToInt32(in.MaxResults) != 1000 {
			t.Errorf("page %d MaxResults = %d", i, aws.ToInt32(in.MaxResults))
		}
	}
	if f.fetches[0].NextToken != nil || aws.ToString(f.fetches[1].NextToken) != "1" {
		t.Errorf("tokens = %v, %v", f.fetches[0].NextToken, aws.ToString(f.fetches[1].NextToken))
	}
}

func TestRun_PagesUntilLastPage(t *testing.T) {
	f := &fakeAthena{pages: []athena.GetQueryResultsOutput{page(true, "1"), page(false, "2"), page(false, "3")}}
	rs, err := testRunner(f, time.Second).run(context.Background(), sqlQuery{sql: "SELECT 1"}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(values(rs)); got != "[1 2 3]" || len(f.fetches) != 3 {
		t.Errorf("rows = %s over %d fetches", got, len(f.fetches))
	}
}

// TestRun_FetchErrorLeavesQueryAlone: a fetch fails only after the query
// SUCCEEDED, so there is nothing running to stop.
func TestRun_FetchErrorLeavesQueryAlone(t *testing.T) {
	f := &fakeAthena{fetchErr: apiErr("ThrottlingException"), executeID: "q-fetch"}
	_, err := testRunner(f, time.Second).run(context.Background(), sqlQuery{sql: "SELECT 1"}, 1)
	isClass(t, err, capability.ErrRetry, "ThrottlingException")
	if len(f.stopped) != 0 {
		t.Errorf("stopped = %q, want none after a SUCCEEDED query", f.stopped)
	}
}
