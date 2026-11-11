package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/configservice"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/internal/adapterplugin"
)

const (
	version = "0.1.0"

	sourceLake   = "security-lake"
	sourceConfig = "aws-config"

	defaultQueryTimeout = 60 * time.Second
	// credentialExpiryWindow refreshes an assumed-role session this long
	// before it expires.
	credentialExpiryWindow = 5 * time.Minute
	roleSessionName        = "reckon-security-lake"
)

var (
	regionRE  = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`)
	accountRE = regexp.MustCompile(`^[0-9]{12}$`)
)

// glueAPI is the slice of the Glue client health uses.
type glueAPI interface {
	GetTable(ctx context.Context, in *glue.GetTableInput, optFns ...func(*glue.Options)) (*glue.GetTableOutput, error)
}

// clients are the AWS service clients one configured instance uses.
type clients struct {
	athena athenaAPI
	glue   glueAPI
	config configAPI
}

// settings is the validated instance configuration.
type settings struct {
	region          string
	workgroup       string
	outputLocation  string
	accessKeyID     string
	secretAccessKey string
	sessionToken    string
	roleARN         string
	database        string
	table           string
	accountUIDs     []string
	queryTimeout    time.Duration
	configEnabled   bool
}

// bridge holds the configured instance and its clients.
type bridge struct {
	mu         sync.Mutex
	set        *settings
	cl         clients
	newClients func(settings) clients
	pollMin    time.Duration
	pollMax    time.Duration
}

func newBridge() *bridge {
	return &bridge{newClients: awsClients, pollMin: defaultPollMin, pollMax: defaultPollMax}
}

// handle dispatches one reckon plugin protocol method.
func (b *bridge) handle(ctx context.Context, method string, params []byte) (any, *wireError) {
	switch method {
	case "initialize":
		return adapterplugin.InitializeResult{ProtocolVersion: adapterplugin.ProtocolVersion, AdapterVersion: version, Concurrency: 1}, nil
	case "describe":
		return describe(), nil
	case "configure":
		return b.configure(params)
	case "invoke":
		return b.invoke(ctx, params)
	case "health":
		return b.health(ctx), nil
	default:
		return nil, &wireError{Code: -32601, Message: "unknown method " + method}
	}
}

// configure validates and stores the instance config (11 §4.3). The x-secret
// fields arrive resolved to plaintext and live only in this process's memory;
// they are the only credentials the adapter uses.
func (b *bridge) configure(params []byte) (any, *wireError) {
	var p adapterplugin.ConfigureParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &wireError{Code: -32602, Message: "configure params: " + err.Error()}
	}
	s, err := parseSettings(p.Config)
	if err != nil {
		return nil, &wireError{Code: -32602, Message: "configure: " + err.Error()}
	}
	cl := b.newClients(s)
	b.mu.Lock()
	b.set, b.cl = &s, cl
	b.mu.Unlock()
	return map[string]any{"ok": true}, nil
}

func parseSettings(cfg map[string]any) (settings, error) {
	s := settings{configEnabled: true, queryTimeout: defaultQueryTimeout}
	strs := map[string]*string{
		"region": &s.region, "athena_workgroup": &s.workgroup, "output_location": &s.outputLocation,
		"access_key_id": &s.accessKeyID, "secret_access_key": &s.secretAccessKey, "session_token": &s.sessionToken,
		"role_arn": &s.roleARN, "glue_database": &s.database, "cloudtrail_table": &s.table,
	}
	for k, dst := range strs {
		v, ok := cfg[k]
		if !ok || v == nil {
			continue
		}
		sv, ok := v.(string)
		if !ok {
			return s, fmt.Errorf("%s must be a string", k)
		}
		*dst = strings.TrimSpace(sv)
	}
	var missing []string
	for _, k := range []string{"region", "athena_workgroup", "output_location", "access_key_id", "secret_access_key"} {
		if *strs[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return s, fmt.Errorf("requires %s", strings.Join(missing, ", "))
	}
	if !regionRE.MatchString(s.region) {
		return s, fmt.Errorf("region %q is not an AWS region", s.region)
	}
	if !strings.HasPrefix(s.outputLocation, "s3://") {
		return s, fmt.Errorf("output_location must be an s3:// URI")
	}
	if s.roleARN != "" && !strings.HasPrefix(s.roleARN, "arn:") {
		return s, fmt.Errorf("role_arn must be an ARN")
	}
	regionID := strings.ReplaceAll(s.region, "-", "_")
	if s.database == "" {
		s.database = "amazon_security_lake_glue_db_" + regionID
	}
	if s.table == "" {
		s.table = "amazon_security_lake_table_" + regionID + "_cloud_trail_mgmt_2_0"
	}
	if !identRE.MatchString(s.database) {
		return s, fmt.Errorf("glue_database %q must match [a-z0-9_]+", s.database)
	}
	if !identRE.MatchString(s.table) {
		return s, fmt.Errorf("cloudtrail_table %q must match [a-z0-9_]+", s.table)
	}
	if v, ok := cfg["account_uids"]; ok && v != nil {
		list, ok := v.([]any)
		if !ok {
			return s, fmt.Errorf("account_uids must be a list of account ids")
		}
		for _, item := range list {
			a, _ := item.(string)
			if !accountRE.MatchString(a) {
				return s, fmt.Errorf("account_uids entry %v is not a 12-digit account id", item)
			}
			s.accountUIDs = append(s.accountUIDs, a)
		}
	}
	if v, ok := cfg["query_timeout"]; ok && v != nil {
		sv, _ := v.(string)
		d, err := parseISODuration(sv)
		if err != nil {
			return s, fmt.Errorf("query_timeout: %w", err)
		}
		s.queryTimeout = d
	}
	if v, ok := cfg["config_enabled"]; ok && v != nil {
		bv, ok := v.(bool)
		if !ok {
			return s, fmt.Errorf("config_enabled must be a boolean")
		}
		s.configEnabled = bv
	}
	return s, nil
}

// isoDurationRE is the subset of ISO 8601 durations a timeout needs: days,
// hours, minutes and (fractional) seconds.
var isoDurationRE = regexp.MustCompile(`^P(?:([0-9]+)D)?(?:T(?:([0-9]+)H)?(?:([0-9]+)M)?(?:([0-9]+(?:\.[0-9]+)?)S)?)?$`)

func parseISODuration(s string) (time.Duration, error) {
	m := isoDurationRE.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if m == nil {
		return 0, fmt.Errorf("%q is not an ISO 8601 duration such as PT60S", s)
	}
	var d time.Duration
	for i, unit := range []time.Duration{24 * time.Hour, time.Hour, time.Minute} {
		if m[i+1] != "" {
			n, err := strconv.Atoi(m[i+1])
			if err != nil {
				return 0, err
			}
			d += time.Duration(n) * unit
		}
	}
	if m[4] != "" {
		f, err := strconv.ParseFloat(m[4], 64)
		if err != nil {
			return 0, err
		}
		d += time.Duration(f * float64(time.Second))
	}
	if d <= 0 {
		return 0, fmt.Errorf("%q must be longer than zero", s)
	}
	return d, nil
}

// awsConfig builds the SDK configuration from the configured values alone:
// static credentials from configure, optionally exchanged for an assumed
// role's session (cached, refreshed before expiry), and the SDK's standard
// retryer. The default credential chain, shared config files, environment
// and instance metadata are never read: the config package that would read
// them is not imported.
func awsConfig(s settings) aws.Config {
	cfg := aws.Config{
		Region:      s.region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(s.accessKeyID, s.secretAccessKey, s.sessionToken)),
		Retryer:     func() aws.Retryer { return retry.NewStandard() },
	}
	if s.roleARN != "" {
		provider := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), s.roleARN, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = roleSessionName
		})
		cfg.Credentials = aws.NewCredentialsCache(provider, func(o *aws.CredentialsCacheOptions) {
			o.ExpiryWindow = credentialExpiryWindow
		})
	}
	return cfg
}

func awsClients(s settings) clients {
	cfg := awsConfig(s)
	return clients{
		athena: athena.NewFromConfig(cfg),
		glue:   glue.NewFromConfig(cfg),
		config: configservice.NewFromConfig(cfg),
	}
}

// snapshot returns the configured settings and clients, nil before configure.
func (b *bridge) snapshot() (*settings, clients) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.set, b.cl
}

func (b *bridge) runner(s *settings, cl clients) *athenaRunner {
	return &athenaRunner{
		client: cl.athena, workgroup: s.workgroup, output: s.outputLocation,
		timeout: s.queryTimeout, pollMin: b.pollMin, pollMax: b.pollMax,
	}
}

// invoke performs one read operation (11 §4). Failures carry their 03 §6.2
// class.
func (b *bridge) invoke(ctx context.Context, params []byte) (any, *wireError) {
	var p adapterplugin.InvokeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, classErr(-32602, "invoke params: "+err.Error(), string(capability.ErrFallthrough))
	}
	s, cl := b.snapshot()
	if s == nil {
		return nil, classErr(-32000, "adapter not configured", string(capability.ErrUnhealthy))
	}
	switch p.Operation {
	case opActivity, opHistory:
		events, err := b.cloudtrail(ctx, s, cl, p.Operation == opHistory, p.Params)
		if err != nil {
			return nil, wireErr(err)
		}
		return result(sourceLake, events), nil
	case opContext:
		if !s.configEnabled {
			return nil, classErr(-32602, "AWS Config is disabled for this instance (config_enabled: false)", string(capability.ErrFallthrough))
		}
		events, err := resourceContext(ctx, cl.config, p.Params)
		if err != nil {
			return nil, wireErr(err)
		}
		return result(sourceConfig, events), nil
	default:
		return nil, classErr(-32602, "unknown read operation "+p.Operation, string(capability.ErrFallthrough))
	}
}

// cloudtrail answers cloudtrail.api_activity and cloudtrail.resource_history:
// one bounded, parameterized Athena query, the rows reassembled into their
// events, then the scope the table cannot express checked on each. History
// is returned oldest first.
func (b *bridge) cloudtrail(ctx context.Context, s *settings, cl clients, history bool, params map[string]any) ([]capability.OcsfPayload, error) {
	c, err := parseCall(params)
	if err != nil {
		return nil, err
	}
	if c.foreignProvider() {
		return nil, errForeignProvider()
	}
	q, err := activityQuery(s.database, s.table, s.accountUIDs, c, history)
	if err != nil {
		return nil, err
	}
	rs, err := b.runner(s, cl).run(ctx, q, c.limit)
	if err != nil {
		return nil, err
	}
	events, err := rowsToEvents(rs, s.table)
	if err != nil {
		return nil, err
	}
	events = filterScope(events, c.resource)
	if history {
		slices.Reverse(events)
	}
	return events, nil
}

// errForeignProvider declines a call naming a provider other than the lake's:
// every row here is AWS, so another binding must answer it.
func errForeignProvider() error {
	return withClass(capability.ErrFallthrough, "the lake holds AWS only; the call names another provider", nil)
}

type healthResult struct {
	Healthy bool   `json:"healthy"`
	Message string `json:"message"`
}

// health proves the credentials, workgroup and output location with a
// trivial query, then that the CloudTrail table exists.
func (b *bridge) health(ctx context.Context) healthResult {
	s, cl := b.snapshot()
	if s == nil {
		return healthResult{Healthy: false, Message: "not configured"}
	}
	if _, err := b.runner(s, cl).run(ctx, sqlQuery{sql: "SELECT 1"}, 1); err != nil {
		return unhealthy("athena", err)
	}
	gctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	if _, err := cl.glue.GetTable(gctx, &glue.GetTableInput{DatabaseName: aws.String(s.database), Name: aws.String(s.table)}); err != nil {
		return unhealthy("glue", err)
	}
	return healthResult{Healthy: true, Message: fmt.Sprintf("Athena workgroup %s, table %s.%s (%s)", s.workgroup, s.database, s.table, s.region)}
}

func unhealthy(service string, err error) healthResult {
	msg := err.Error()
	if code := errorCode(err); code != "" {
		msg = code + ": " + msg
	}
	return healthResult{Healthy: false, Message: service + ": " + msg}
}

// invokeResult is the reckon-plugin invoke result wire shape (matches the
// host's decoder in internal/adapterplugin). capability.OcsfPayload has no
// JSON tags of its own, so the events are re-shaped onto ocsfEvent.
type invokeResult struct {
	SourceTool string      `json:"source_tool"`
	Events     []ocsfEvent `json:"events"`
}

type ocsfEvent struct {
	ClassUID  int            `json:"class_uid"`
	ClassName string         `json:"class_name"`
	Time      time.Time      `json:"time"`
	Raw       map[string]any `json:"raw"`
}

func result(source string, events []capability.OcsfPayload) invokeResult {
	out := invokeResult{SourceTool: source, Events: make([]ocsfEvent, 0, len(events))}
	for _, e := range events {
		out.Events = append(out.Events, ocsfEvent{ClassUID: e.ClassUID, ClassName: e.ClassName, Time: e.Time, Raw: e.Raw})
	}
	return out
}
