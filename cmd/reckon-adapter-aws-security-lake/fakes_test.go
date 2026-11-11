package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/aws/aws-sdk-go-v2/service/configservice"
	cstypes "github.com/aws/aws-sdk-go-v2/service/configservice/types"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"

	"github.com/sd-strax/reckon/capability"
)

// fakeAthena replays a scripted query lifecycle: GetQueryExecution walks
// states (the last repeats), and GetQueryResults serves pages chained by
// NextToken "1", "2", ….
type fakeAthena struct {
	mu        sync.Mutex
	states    []types.QueryExecutionState
	status    *types.QueryExecutionStatus // returned with the terminal FAILED state
	pages     []athena.GetQueryResultsOutput
	startErr  error
	fetchErr  error
	pollErr   error
	started   []*athena.StartQueryExecutionInput
	polls     int
	stopped   []string
	fetches   []*athena.GetQueryResultsInput
	executeID string
}

func (f *fakeAthena) StartQueryExecution(_ context.Context, in *athena.StartQueryExecutionInput, _ ...func(*athena.Options)) (*athena.StartQueryExecutionOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, in)
	if f.startErr != nil {
		return nil, f.startErr
	}
	id := f.executeID
	if id == "" {
		id = "qid-1"
	}
	return &athena.StartQueryExecutionOutput{QueryExecutionId: aws.String(id)}, nil
}

func (f *fakeAthena) GetQueryExecution(_ context.Context, _ *athena.GetQueryExecutionInput, _ ...func(*athena.Options)) (*athena.GetQueryExecutionOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pollErr != nil {
		return nil, f.pollErr
	}
	state := types.QueryExecutionStateSucceeded
	if len(f.states) > 0 {
		state = f.states[min(f.polls, len(f.states)-1)]
	}
	f.polls++
	st := &types.QueryExecutionStatus{State: state}
	if state == types.QueryExecutionStateFailed && f.status != nil {
		st = f.status
		st.State = state
	}
	return &athena.GetQueryExecutionOutput{QueryExecution: &types.QueryExecution{Status: st}}, nil
}

func (f *fakeAthena) GetQueryResults(_ context.Context, in *athena.GetQueryResultsInput, _ ...func(*athena.Options)) (*athena.GetQueryResultsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches = append(f.fetches, in)
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	i := 0
	if in.NextToken != nil {
		_, _ = fmt.Sscanf(*in.NextToken, "%d", &i)
	}
	if i >= len(f.pages) {
		return &athena.GetQueryResultsOutput{ResultSet: &types.ResultSet{}}, nil
	}
	out := f.pages[i]
	if i+1 < len(f.pages) {
		out.NextToken = aws.String(fmt.Sprint(i + 1))
	}
	return &out, nil
}

func (f *fakeAthena) StopQueryExecution(_ context.Context, in *athena.StopQueryExecutionInput, _ ...func(*athena.Options)) (*athena.StopQueryExecutionOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, aws.ToString(in.QueryExecutionId))
	return &athena.StopQueryExecutionOutput{}, nil
}

type fakeGlue struct {
	err   error
	calls []*glue.GetTableInput
	// columns, when set, is the table's schema; otherwise every required
	// column, accountid and region as partition keys.
	columns []string
	// link, when set, makes the configured table a resource link to it: a
	// GetTable without a CatalogId returns the link (no schema), one with
	// the target's CatalogId returns the target, or targetErr.
	link      *gluetypes.TableIdentifier
	targetErr error
	// noSchema returns the table without a StorageDescriptor.
	noSchema bool
}

func (g *fakeGlue) GetTable(_ context.Context, in *glue.GetTableInput, _ ...func(*glue.Options)) (*glue.GetTableOutput, error) {
	g.calls = append(g.calls, in)
	if g.err != nil {
		return nil, g.err
	}
	if g.link != nil && in.CatalogId == nil {
		return &glue.GetTableOutput{Table: &gluetypes.Table{Name: in.Name, DatabaseName: in.DatabaseName, TargetTable: g.link}}, nil
	}
	if g.link != nil && g.targetErr != nil {
		return nil, g.targetErr
	}
	if g.noSchema {
		return &glue.GetTableOutput{Table: &gluetypes.Table{Name: in.Name}}, nil
	}
	cols := g.columns
	if cols == nil {
		cols = requiredColumns()
	}
	t := &gluetypes.Table{StorageDescriptor: &gluetypes.StorageDescriptor{}}
	for _, c := range cols {
		col := gluetypes.Column{Name: aws.String(c)}
		if c == "accountid" || c == "region" || c == "time_dt" {
			t.PartitionKeys = append(t.PartitionKeys, col)
		} else {
			t.StorageDescriptor.Columns = append(t.StorageDescriptor.Columns, col)
		}
	}
	return &glue.GetTableOutput{Table: t}, nil
}

type fakeConfig struct {
	history    *configservice.GetResourceConfigHistoryOutput
	historyErr error
	discovered *configservice.ListDiscoveredResourcesOutput            // answers a lookup by name
	byID       *configservice.ListDiscoveredResourcesOutput            // answers a lookup by resource id
	items      map[string]cstypes.ConfigurationItem                    // history by resource id, overriding history
	itemErrs   map[string]error                                        // history failure by resource id
	byIDs      map[string]*configservice.ListDiscoveredResourcesOutput // lookup by a specific resource id
	// exactStatus keeps each item\'s status as set, an empty one included;
	// otherwise an item with no status is reported OK.
	exactStatus bool
	historyIn   []*configservice.GetResourceConfigHistoryInput
	listIn      []*configservice.ListDiscoveredResourcesInput
}

func (c *fakeConfig) GetResourceConfigHistory(_ context.Context, in *configservice.GetResourceConfigHistoryInput, _ ...func(*configservice.Options)) (*configservice.GetResourceConfigHistoryOutput, error) {
	c.historyIn = append(c.historyIn, in)
	if c.historyErr != nil {
		return nil, c.historyErr
	}
	if err, ok := c.itemErrs[aws.ToString(in.ResourceId)]; ok {
		return nil, err
	}
	out := &configservice.GetResourceConfigHistoryOutput{}
	if item, ok := c.items[aws.ToString(in.ResourceId)]; ok {
		out.ConfigurationItems = []cstypes.ConfigurationItem{item}
	} else if c.history != nil {
		out.ConfigurationItems = append(out.ConfigurationItems, c.history.ConfigurationItems...)
	}
	// Config always reports a status; a fake item that sets none is a
	// recorded, current one.
	for i := range out.ConfigurationItems {
		if !c.exactStatus && out.ConfigurationItems[i].ConfigurationItemStatus == "" {
			out.ConfigurationItems[i].ConfigurationItemStatus = cstypes.ConfigurationItemStatusOk
		}
	}
	return out, nil
}

func (c *fakeConfig) ListDiscoveredResources(_ context.Context, in *configservice.ListDiscoveredResourcesInput, _ ...func(*configservice.Options)) (*configservice.ListDiscoveredResourcesOutput, error) {
	c.listIn = append(c.listIn, in)
	out := c.discovered
	if len(in.ResourceIds) > 0 {
		out = c.byID
		if o, ok := c.byIDs[in.ResourceIds[0]]; ok {
			out = o
		}
	}
	if out == nil {
		return &configservice.ListDiscoveredResourcesOutput{}, nil
	}
	return out, nil
}

// rawResponse is one testdata/raw_response file (03 §9.1).
type rawResponse struct {
	Scenario  string                         `json:"scenario"`
	Operation string                         `json:"operation"`
	Fixture   *string                        `json:"fixture"`
	Pages     []athena.GetQueryResultsOutput `json:"pages"`
}

func loadRawResponse(t *testing.T, name string) rawResponse {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "raw_response", name))
	if err != nil {
		t.Fatal(err)
	}
	var rr rawResponse
	if err := json.Unmarshal(data, &rr); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(rr.Pages) == 0 {
		t.Fatalf("%s: no pages", name)
	}
	return rr
}

// testRunner is an athenaRunner on f with millisecond polling.
func testRunner(f *fakeAthena, timeout time.Duration) *athenaRunner {
	return &athenaRunner{client: f, workgroup: "reckon", output: "s3://results/", timeout: timeout, pollMin: time.Millisecond, pollMax: 2 * time.Millisecond}
}

// lakeEvents runs a raw_response file's pages through the runner and the row
// decoding, as an invoke would.
func lakeEvents(t *testing.T, name string) []capability.OcsfPayload {
	t.Helper()
	rr := loadRawResponse(t, name)
	f := &fakeAthena{pages: rr.Pages}
	rs, err := testRunner(f, time.Second).run(context.Background(), sqlQuery{sql: "SELECT 1"}, maxLimit)
	if err != nil {
		t.Fatal(err)
	}
	events, err := rowsToEvents(rs, testTable)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

const (
	testDB    = "amazon_security_lake_glue_db_us_east_1"
	testTable = "amazon_security_lake_table_us_east_1_cloud_trail_mgmt_2_0"
)

// testBridge is a configured bridge on fakes.
func testBridge(t *testing.T, extra map[string]any, f *fakeAthena, g *fakeGlue, c *fakeConfig) *bridge {
	t.Helper()
	b := newBridge()
	b.pollMin, b.pollMax = time.Millisecond, 2*time.Millisecond
	b.newClients = func(settings) clients { return clients{athena: f, glue: g, config: c} }
	cfg := baseConfig()
	for k, v := range extra {
		cfg[k] = v
	}
	raw, _ := json.Marshal(map[string]any{"config": cfg})
	if _, werr := b.configure(raw); werr != nil {
		t.Fatalf("configure: %+v", werr)
	}
	return b
}

func baseConfig() map[string]any {
	return map[string]any{
		"region":            "us-east-1",
		"athena_workgroup":  "reckon",
		"output_location":   "s3://acme-reckon-athena/",
		"access_key_id":     "AKIDEXAMPLE",
		"secret_access_key": "secret",
	}
}

// classOf reads the 03 §6.2 class off a wire error.
func classOf(t *testing.T, werr *wireError) string {
	t.Helper()
	if werr == nil {
		t.Fatal("want an error, got none")
	}
	var d struct {
		Class string `json:"class"`
	}
	_ = json.Unmarshal(werr.Data, &d)
	return d.Class
}
