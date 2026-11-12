package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/internal/adapterplugin"
)

// tableSchema is the set of fields a table carries (11 §4.4, adr/0335): every
// top-level column and partition key, and every nested path its type names —
// `actor.user.credential_uid` for a struct member, `resources[].uid` for a
// member of an array of structs. A map, an array of scalars and a scalar are
// leaves. Names are lowercase, as Glue and Athena keep them.
type tableSchema struct {
	fields map[string]bool
	// assumed, when set, says the schema could not be read and why; the
	// fields are then the recorded profile's.
	assumed string
}

// has reports whether the table carries path.
func (s *tableSchema) has(path string) bool { return s.fields[path] }

// present is the candidates the table carries, in order.
func (s *tableSchema) present(candidates []string) []string {
	var out []string
	for _, c := range candidates {
		if s.has(c) {
			out = append(out, c)
		}
	}
	return out
}

// absent is the candidates the table does not carry, in order.
func (s *tableSchema) absent(candidates []string) []string {
	var out []string
	for _, c := range candidates {
		if !s.has(c) {
			out = append(out, c)
		}
	}
	return out
}

// glueColumn is a column as the Glue API (and a recorded schema.json) gives
// it: a name, a Hive type string and its parameters.
type glueColumn struct {
	Name       string            `json:"Name"`
	Type       string            `json:"Type"`
	Parameters map[string]string `json:"Parameters,omitempty"`
}

// schemaFromColumns parses columns (partition keys included) into the
// fields they carry, leaving out a column Iceberg marks no longer current
// (iceberg.field.current false; absent or true counts): an Iceberg table
// keeps a dropped column in Glue, and the table does not carry it.
func schemaFromColumns(cols []glueColumn) *tableSchema {
	s := &tableSchema{fields: map[string]bool{}}
	for _, c := range cols {
		if c.Parameters["iceberg.field.current"] == "false" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(c.Name))
		if name != "" {
			addHiveFields(s.fields, name, c.Type)
		}
	}
	return s
}

// schemaFromGlue parses a Glue table's columns and partition keys.
func schemaFromGlue(t *gluetypes.Table) *tableSchema {
	var cols []glueColumn
	add := func(cs []gluetypes.Column) {
		for _, c := range cs {
			cols = append(cols, glueColumn{Name: aws.ToString(c.Name), Type: aws.ToString(c.Type), Parameters: c.Parameters})
		}
	}
	if t.StorageDescriptor != nil {
		add(t.StorageDescriptor.Columns)
	}
	add(t.PartitionKeys)
	return schemaFromColumns(cols)
}

// addHiveFields records path and, for a struct or an array of structs, the
// paths of its members.
func addHiveFields(fields map[string]bool, path, typ string) {
	fields[path] = true
	if members, ok := hiveStruct(typ); ok {
		addMembers(fields, path, members)
		return
	}
	if elem, ok := hiveWrapped(typ, "array"); ok {
		if members, ok := hiveStruct(elem); ok {
			addMembers(fields, path+"[]", members)
		}
	}
}

func addMembers(fields map[string]bool, prefix string, members []string) {
	for _, m := range members {
		name, typ, ok := strings.Cut(m, ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.Trim(strings.TrimSpace(name), "`"))
		if name != "" {
			addHiveFields(fields, prefix+"."+name, typ)
		}
	}
}

// hiveStruct returns the `name:type` members of a struct<…> type.
func hiveStruct(typ string) ([]string, bool) {
	body, ok := hiveWrapped(typ, "struct")
	if !ok {
		return nil, false
	}
	return splitTopLevel(body), true
}

// hiveWrapped returns the body of a kind<…> type.
func hiveWrapped(typ, kind string) (string, bool) {
	t := strings.TrimSpace(typ)
	open := kind + "<"
	if len(t) < len(open)+1 || !strings.EqualFold(t[:len(open)], open) || !strings.HasSuffix(t, ">") {
		return "", false
	}
	return t[len(open) : len(t)-1], true
}

// splitTopLevel splits s on the commas outside any <…> or (…), so
// `a:struct<b:int,c:int>,d:decimal(10,2)` is two members.
func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '<', '(':
			depth++
		case '>', ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	if strings.TrimSpace(s[start:]) != "" {
		out = append(out, s[start:])
	}
	return out
}

// recordedProfile is a profile the adapter carries (testdata/profiles/<name>),
// used as the table's schema when the schema cannot be read. The lake's
// table name ends in its source and version (`_cloud_trail_mgmt_2_0`),
// which picks it. parsed is its schema.json, parsed once.
type recordedProfile struct {
	name, suffix string
	parsed       func() (*tableSchema, error)
}

// profile20Schema is the cloudtrail-mgmt-2.0 table as recorded from a live
// lake: the one recording the offline tests read and the runtime falls back
// to.
//
//go:embed testdata/profiles/cloudtrail-mgmt-2.0/schema.json
var profile20Schema []byte

var recordedProfiles = []recordedProfile{{
	name: profileName, suffix: "_cloud_trail_mgmt_2_0",
	parsed: sync.OnceValues(func() (*tableSchema, error) { return parseRecordedSchema(profile20Schema) }),
}}

// profileSchema is the recorded profile for table, by its suffix: a fresh
// tableSchema sharing the profile's (read-only) fields, so its assumed text
// is its own; ok is false when no profile matches.
func profileSchema(table string) (*tableSchema, string, bool) {
	for _, p := range recordedProfiles {
		if strings.HasSuffix(table, p.suffix) {
			parsed, err := p.parsed()
			if err != nil {
				panic("aws-security-lake: recorded profile " + p.name + " does not parse: " + err.Error())
			}
			return &tableSchema{fields: parsed.fields}, p.name, true
		}
	}
	return nil, "", false
}

// parseRecordedSchema reads a schema.json: a Glue table's Name,
// StorageDescriptor.Columns and PartitionKeys.
func parseRecordedSchema(data []byte) (*tableSchema, error) {
	var t struct {
		StorageDescriptor *struct {
			Columns []glueColumn `json:"Columns"`
		} `json:"StorageDescriptor"`
		PartitionKeys []glueColumn `json:"PartitionKeys"`
	}
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	if t.StorageDescriptor == nil || len(t.StorageDescriptor.Columns) == 0 {
		return nil, fmt.Errorf("no StorageDescriptor.Columns")
	}
	return schemaFromColumns(append(slices.Clone(t.StorageDescriptor.Columns), t.PartitionKeys...)), nil
}

// discoverSchema reads the configured table's schema from Glue, through a
// resource link when it is one (readableSchema). A failed read — the table
// not found, access denied on it, throttling, a timeout — is an error. A
// schema the subscriber may not read (a link whose target is denied or not
// found to it, a table with no StorageDescriptor) is the recorded profile
// named by the suffix of the table the link points to (the configured table
// when it is no link), marked assumed; with no such profile it is
// unhealthy.
func discoverSchema(ctx context.Context, s *settings, client glueAPI) (*tableSchema, error) {
	gctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	out, err := client.GetTable(gctx, &glue.GetTableInput{DatabaseName: aws.String(s.database), Name: aws.String(s.table)})
	if err != nil {
		return nil, err
	}
	t, skipped, err := readableSchema(gctx, client, out.Table, s.region)
	if err != nil {
		return nil, err
	}
	if skipped == "" {
		return schemaFromGlue(t), nil
	}
	source := s.table
	if out.Table != nil && out.Table.TargetTable != nil && aws.ToString(out.Table.TargetTable.Name) != "" {
		source = aws.ToString(out.Table.TargetTable.Name)
	}
	ps, name, ok := profileSchema(source)
	if !ok {
		return nil, withClass(capability.ErrUnhealthy, "the table's schema is not readable ("+skipped+") and no recorded profile matches table "+source, nil)
	}
	ps.assumed = "schema not readable (" + skipped + "); assumed recorded profile " + name
	return ps, nil
}

// Candidate fields per capability (adr/0335). A filter is expressed over the
// candidates the table carries, joined with OR; with none present the
// capability is degraded and a call that needs it is declined.
var (
	// coreFields are read by every query: a table without one is unhealthy.
	coreFields       = []string{"time", "time_dt", "class_uid", "accountid"}
	principalFields  = []string{"actor.user.uid", "actor.session.issuer", "actor.user.name"}
	credentialFields = []string{"actor.user.credential_uid", "actor.session.credential_uid"}
	operationFields  = []string{"api.operation"}
	serviceFields    = []string{"api.service.name"}
	// outcomeFields: status_id where the table has it, else the status
	// string compared without case.
	outcomeFields     = []string{"status_id", "status"}
	resourceUIDFields = []string{"resources[].uid"}
	// eventRegionFields filter a region given without a resource name: the
	// region column (the region the event was logged in, set on every row)
	// where the table has it, else cloud.region — one predicate, the first
	// present.
	eventRegionFields = []string{"region", "cloud.region"}
	historyFields     = []string{"activity_id"}
)

const (
	resourceNameField   = "resources[].name"
	resourceTypeField   = "resources[].type"
	resourceRegionField = "resources[].region"
)

// postScopeFields are the resources[] members a resource's zone, group and
// namespace are matched on, in the query (resourceElement) and again after
// the fetch (scope.go). A call giving one the table lacks is declined:
// without it the query would widen and the check after it would drop every
// row, answering a false empty.
var postScopeFields = []string{"resources[].zone", "resources[].group", "resources[].namespace"}

// groupFields are the group members a group scope matches, by name or uid.
var groupFields = []string{"resources[].group.name", "resources[].group.uid"}

// groupMissing is what the table lacks for a group scope: resources[].group
// itself, or, when the group carries neither a name nor a uid, both of
// those; nil when the scope is expressible. The query, the probe and health
// share it.
func groupMissing(s *tableSchema) []string {
	if !s.has(postScopeFields[1]) {
		return []string{postScopeFields[1]}
	}
	if len(s.present(groupFields)) == 0 {
		return slices.Clone(groupFields)
	}
	return nil
}

// scopeMissing is what the table lacks for the zone, group and namespace
// scopes, in that order.
func scopeMissing(s *tableSchema) []string {
	var out []string
	if !s.has(postScopeFields[0]) {
		out = append(out, postScopeFields[0])
	}
	out = append(out, groupMissing(s)...)
	if !s.has(postScopeFields[2]) {
		out = append(out, postScopeFields[2])
	}
	return out
}

// Capability names, as health and declines report them.
const (
	capPrincipal      = "principal filter"
	capCredential     = "credential filter" //nolint:gosec // G101: a capability name, not a credential
	capOperation      = "operation filter"
	capService        = "service filter"
	capOutcome        = "outcome filter"
	capResourceUID    = "resource by uid"
	capResourceName   = "resource by name"
	capResourceType   = "resource type scope"
	capResourceRegion = "resource region scope"
	capResourceScope  = "resource scope by zone, group or namespace"
	capEventRegion    = "region filter"
	capHistory        = "resource history"
)

// candidateFields is every field the adapter may read: the projection's
// columns, the predicate-only columns and every candidate path. describe
// lists it as the profile's fields.
func candidateFields() []string {
	var out []string
	add := func(fs ...string) {
		for _, f := range fs {
			if !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	for _, c := range projection {
		add(c.source)
	}
	add(predicateOnlyColumns...)
	add(coreFields...)
	add(principalFields...)
	add(credentialFields...)
	add(operationFields...)
	add(serviceFields...)
	add(outcomeFields...)
	add(resourceUIDFields...)
	add(resourceNameField, resourceTypeField, resourceRegionField)
	add(postScopeFields...)
	add(groupFields...)
	add(eventRegionFields...)
	add(historyFields...)
	return out
}

// declined is the FALLTHROUGH a call gets when it needs a capability the
// table cannot express: another binding may answer it.
func declined(capName string, absent []string) error {
	return withClass(capability.ErrFallthrough, fmt.Sprintf("the table cannot express the %s: %s absent", capName, strings.Join(absent, ", ")), nil)
}

// degraded is what the table cannot express, per operation (11 §4.4).
func degraded(s *tableSchema) []adapterplugin.DegradedCapability {
	var out []adapterplugin.DegradedCapability
	add := func(op, capName string, candidates []string) {
		if len(s.present(candidates)) == 0 {
			out = append(out, adapterplugin.DegradedCapability{Operation: op, Capability: capName, Reason: strings.Join(candidates, ", ") + " absent"})
		}
	}
	resource := func(op string) {
		add(op, capResourceUID, resourceUIDFields)
		add(op, capResourceName, []string{resourceNameField})
		// A type scopes the resource's element in the query whether or not
		// it is named, so it is reported either way; a region scopes the
		// element only beside a name.
		add(op, capResourceType, []string{resourceTypeField})
		if s.has(resourceNameField) {
			add(op, capResourceRegion, []string{resourceRegionField})
		}
		if missing := scopeMissing(s); len(missing) > 0 {
			out = append(out, adapterplugin.DegradedCapability{Operation: op, Capability: capResourceScope, Reason: strings.Join(missing, ", ") + " absent"})
		}
	}
	add(opActivity, capPrincipal, principalFields)
	add(opActivity, capCredential, credentialFields)
	add(opActivity, capOperation, operationFields)
	add(opActivity, capService, serviceFields)
	add(opActivity, capOutcome, outcomeFields)
	resource(opActivity)
	add(opActivity, capEventRegion, eventRegionFields)
	add(opHistory, capHistory, historyFields)
	resource(opHistory)
	return out
}
