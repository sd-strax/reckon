package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"

	"github.com/sd-strax/reckon/capability"
)

// athenaAPI is the slice of the Athena client the adapter calls, so tests
// inject a fake.
type athenaAPI interface {
	StartQueryExecution(ctx context.Context, in *athena.StartQueryExecutionInput, optFns ...func(*athena.Options)) (*athena.StartQueryExecutionOutput, error)
	GetQueryExecution(ctx context.Context, in *athena.GetQueryExecutionInput, optFns ...func(*athena.Options)) (*athena.GetQueryExecutionOutput, error)
	GetQueryResults(ctx context.Context, in *athena.GetQueryResultsInput, optFns ...func(*athena.Options)) (*athena.GetQueryResultsOutput, error)
	StopQueryExecution(ctx context.Context, in *athena.StopQueryExecutionInput, optFns ...func(*athena.Options)) (*athena.StopQueryExecutionOutput, error)
}

const (
	defaultPollMin = 500 * time.Millisecond
	defaultPollMax = 2 * time.Second
	pageSize       = 1000 // GetQueryResults' MaxResults ceiling
	stopTimeout    = 10 * time.Second
)

// athenaRunner runs one statement through a workgroup: start, poll until a
// terminal state or the query timeout, then page the results.
type athenaRunner struct {
	client    athenaAPI
	workgroup string
	output    string
	timeout   time.Duration
	pollMin   time.Duration
	pollMax   time.Duration
}

// resultSet is a query's column names and its data rows, each datum the
// VarCharValue Athena returned (nil for SQL NULL).
type resultSet struct {
	columns []string
	rows    [][]*string
}

// run executes q and returns at most limit data rows. A failure while the
// query may still be running — the timeout, a transport error while
// polling — stops it, so nothing keeps scanning on the caller's bill; a
// query that ended FAILED or CANCELLED, or SUCCEEDED before a fetch error,
// is not running and is not stopped. A query
// still running at the timeout is reported as RETRY.
func (a *athenaRunner) run(ctx context.Context, q sqlQuery, limit int) (resultSet, error) {
	qctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	in := &athena.StartQueryExecutionInput{
		QueryString: aws.String(q.sql),
		WorkGroup:   aws.String(a.workgroup),
	}
	if len(q.params) > 0 {
		in.ExecutionParameters = q.params
	}
	if a.output != "" {
		in.ResultConfiguration = &types.ResultConfiguration{OutputLocation: aws.String(a.output)}
	}
	start, err := a.client.StartQueryExecution(qctx, in)
	if err != nil {
		return resultSet{}, err
	}
	id := aws.ToString(start.QueryExecutionId)
	if terminal, err := a.wait(qctx, id); err != nil {
		if !terminal {
			a.stop(ctx, id)
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return resultSet{}, withClass(capability.ErrRetry, "athena query "+id+" did not finish within query_timeout; stopped", err)
		}
		return resultSet{}, err
	}
	// The query has SUCCEEDED by now: a fetch error leaves nothing running.
	return a.fetch(qctx, id, limit)
}

// wait polls the execution every pollMin, backing off to pollMax, until it
// reaches a terminal state or ctx ends. terminal reports that the query
// ended on its own (FAILED or CANCELLED) when err is non-nil.
func (a *athenaRunner) wait(ctx context.Context, id string) (terminal bool, err error) {
	interval := a.pollMin
	for {
		out, err := a.client.GetQueryExecution(ctx, &athena.GetQueryExecutionInput{QueryExecutionId: aws.String(id)})
		if err != nil {
			return false, err
		}
		var status *types.QueryExecutionStatus
		if out.QueryExecution != nil {
			status = out.QueryExecution.Status
		}
		if status != nil {
			switch status.State {
			case types.QueryExecutionStateSucceeded:
				return false, nil
			case types.QueryExecutionStateFailed:
				return true, failedQuery(id, status)
			case types.QueryExecutionStateCancelled:
				return true, withClass(capability.ErrRetry, "athena query "+id+" was cancelled", nil)
			}
		}
		t := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return false, ctx.Err()
		case <-t.C:
		}
		interval = min(interval*2, a.pollMax)
	}
}

// stop cancels a query that outlived its timeout, on a context of its own so
// the cancellation is sent even though the call's context has ended.
func (a *athenaRunner) stop(ctx context.Context, id string) {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	_, _ = a.client.StopQueryExecution(sctx, &athena.StopQueryExecutionInput{QueryExecutionId: aws.String(id)})
}

// fetch pages GetQueryResults until limit data rows or the last page. The
// first page's first row is the header Athena returns for a SELECT; it is
// dropped.
func (a *athenaRunner) fetch(ctx context.Context, id string, limit int) (resultSet, error) {
	var rs resultSet
	var token *string
	first := true
	for {
		out, err := a.client.GetQueryResults(ctx, &athena.GetQueryResultsInput{
			QueryExecutionId: aws.String(id),
			MaxResults:       aws.Int32(pageSize),
			NextToken:        token,
		})
		if err != nil {
			return resultSet{}, err
		}
		var rows []types.Row
		if out.ResultSet != nil {
			rows = out.ResultSet.Rows
			if first {
				rs.columns = columnNames(out.ResultSet)
				if len(rows) > 0 && (rs.columns == nil || isHeader(rows[0], rs.columns)) {
					if rs.columns == nil {
						rs.columns = rowValues(rows[0])
					}
					rows = rows[1:]
				}
			}
		}
		first = false
		for _, r := range rows {
			if len(rs.rows) >= limit {
				return rs, nil
			}
			rs.rows = append(rs.rows, rowPointers(r))
		}
		token = out.NextToken
		if token == nil || aws.ToString(token) == "" || len(rs.rows) >= limit {
			return rs, nil
		}
	}
}

// failedQuery classifies a FAILED execution from Athena's own error detail:
// a missing table, database or permission is the instance's configuration
// (UNHEALTHY); a column the table lacks or a type or schema it does not
// match is a schema-version mismatch between the lake and the adapter
// (FALLTHROUGH, 03 §6.2: another binding may read this version); a system or
// retryable error is RETRY; anything else — a syntax error in a statement
// the adapter built — is FATAL.
func failedQuery(id string, st *types.QueryExecutionStatus) error {
	reason := aws.ToString(st.StateChangeReason)
	var category int32
	retryable := false
	if e := st.AthenaError; e != nil {
		category = aws.ToInt32(e.ErrorCategory)
		retryable = e.Retryable
		if reason == "" {
			reason = aws.ToString(e.ErrorMessage)
		}
	}
	msg := "athena query " + id + " failed: " + reason
	upper := strings.ToUpper(reason)
	for _, s := range []string{"TABLE_NOT_FOUND", "SCHEMA_NOT_FOUND", "CATALOG_NOT_FOUND", "PERMISSION_DENIED", "ACCESS DENIED", "ACCESSDENIED", "INSUFFICIENT PERMISSIONS", "INSUFFICIENT LAKE FORMATION PERMISSION"} {
		if strings.Contains(upper, s) {
			return withClass(capability.ErrUnhealthy, msg, nil)
		}
	}
	for _, s := range []string{"COLUMN_NOT_FOUND", "TYPE_MISMATCH", "SCHEMA_MISMATCH", "CANNOT BE RESOLVED"} {
		if strings.Contains(upper, s) {
			return withClass(capability.ErrFallthrough, msg, nil)
		}
	}
	if retryable || category == 1 { // 1 = SYSTEM
		return withClass(capability.ErrRetry, msg, nil)
	}
	return withClass(capability.ErrFatal, msg, nil)
}

func columnNames(rs *types.ResultSet) []string {
	if rs.ResultSetMetadata == nil || len(rs.ResultSetMetadata.ColumnInfo) == 0 {
		return nil
	}
	out := make([]string, len(rs.ResultSetMetadata.ColumnInfo))
	for i, c := range rs.ResultSetMetadata.ColumnInfo {
		out[i] = firstNonEmpty(aws.ToString(c.Label), aws.ToString(c.Name))
	}
	return out
}

// isHeader reports whether r carries exactly the column names.
func isHeader(r types.Row, cols []string) bool {
	if len(r.Data) != len(cols) {
		return false
	}
	for i, d := range r.Data {
		if aws.ToString(d.VarCharValue) != cols[i] {
			return false
		}
	}
	return true
}

func rowValues(r types.Row) []string {
	out := make([]string, len(r.Data))
	for i, d := range r.Data {
		out[i] = aws.ToString(d.VarCharValue)
	}
	return out
}

func rowPointers(r types.Row) []*string {
	out := make([]*string, len(r.Data))
	for i, d := range r.Data {
		out[i] = d.VarCharValue
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
