package main

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/configservice"
	cstypes "github.com/aws/aws-sdk-go-v2/service/configservice/types"

	"github.com/sd-strax/reckon/capability"
)

// configAPI is the slice of the AWS Config client the adapter calls.
type configAPI interface {
	GetResourceConfigHistory(ctx context.Context, in *configservice.GetResourceConfigHistoryInput, optFns ...func(*configservice.Options)) (*configservice.GetResourceConfigHistoryOutput, error)
	ListDiscoveredResources(ctx context.Context, in *configservice.ListDiscoveredResourcesInput, optFns ...func(*configservice.Options)) (*configservice.ListDiscoveredResourcesOutput, error)
}

const (
	classInventory     = 5023
	classNameInventory = "Cloud Resources Inventory Info"
	// inventoryVersion is the OCSF version this adapter writes class 5023 in
	// (03 §4.18). The class exists from 1.4.0, and a resource's created_time
	// and modified_time from 1.5.0.
	inventoryVersion = "1.7.0"
)

// fallthroughConfigCodes are AWS Config errors meaning Config cannot answer
// for this resource — not recorded, not discovered, a type it does not know —
// while the instance itself is fine, so another binding may answer.
var fallthroughConfigCodes = map[string]bool{
	"ResourceNotDiscoveredException":            true,
	"NoAvailableConfigurationRecorderException": true,
	"ValidationException":                       true,
	"InvalidParameterValueException":            true,
}

// resourceContext answers config.resource_context: the latest configuration
// item for the resource, shaped into one OCSF 5023 event. A resource Config
// cannot be asked about — another provider, no identifier, a type it cannot
// tell — falls through to another binding rather than failing the call.
func resourceContext(ctx context.Context, client configAPI, params map[string]any) ([]capability.OcsfPayload, error) {
	res := sub(params, "resource")
	r := resourceScope{
		uid: str(res, "uid"), name: str(res, "name"), typ: str(res, "type"), provider: str(res, "provider"),
		accountUID: str(res, "account_uid"), region: str(res, "region"),
		zone: str(res, "zone"), group: str(res, "group"), namespace: str(res, "namespace"),
	}
	if r.provider != "" && !strings.EqualFold(r.provider, "aws") {
		return nil, errForeignProvider()
	}
	if !r.given() {
		return nil, configErr(nil, "resource context needs a resource uid or name")
	}
	// A name-identified resource is narrowed by every scope given; a Config
	// item records no resource group or namespace, so a scope of either
	// cannot be confirmed and Config does not answer. A uid is not narrowed.
	if r.uid == "" {
		for _, k := range []struct{ key, val string }{{"group", r.group}, {"namespace", r.namespace}} {
			if k.val != "" {
				return nil, configErr(nil, k.key+" scope cannot be established from AWS Config")
			}
		}
	}
	if r.typ == "" {
		r.typ = configTypeFromARN(r.uid)
	}
	if r.typ == "" {
		return nil, configErr(nil, "AWS Config needs the resource type: give a type, or an ARN of a type the adapter maps")
	}
	return findItem(ctx, client, r)
}

// findItem resolves the resource to its latest configuration item. A uid
// that is not an ARN is Config's own id. Otherwise the lookups of
// configLookups run in turn, each yielding candidate ids:
//   - with a pinned ARN, a candidate Config cannot answer for (not
//     discovered, deleted, invalid) or whose ARN is not the one asked for is
//     skipped, and when a lookup's candidates are exhausted the next lookup
//     is tried; any other failure is returned;
//   - with a name only, two candidates are not an answer (a name is unique
//     per type only within an account and region), and a candidate in
//     another account or region is declined.
func findItem(ctx context.Context, client configAPI, r resourceScope) ([]capability.OcsfPayload, error) {
	notFound := configErr(nil, "AWS Config has not discovered "+r.typ+" "+firstNonEmpty(r.uid, r.name))
	if r.uid != "" && !strings.HasPrefix(r.uid, "arn:") {
		item, err := latestItem(ctx, client, r.typ, r.uid)
		if err != nil {
			return nil, err
		}
		if err := checkItem(item, r, r.uid); err != nil {
			return nil, err
		}
		return []capability.OcsfPayload{inventoryEvent(item)}, nil
	}
	pinned := strings.HasPrefix(r.uid, "arn:")
	miss := notFound
	for _, in := range configLookups(r) {
		ids, err := discover(ctx, client, in)
		if err != nil {
			return nil, configErr(err, "")
		}
		if len(ids) > 1 && !pinned {
			return nil, configErr(nil, "AWS Config: name "+r.name+" is not unique for this type ("+r.typ+")")
		}
		for _, id := range ids {
			item, err := latestItem(ctx, client, r.typ, id)
			if err != nil {
				if pinned && classify(err) == capability.ErrFallthrough {
					miss = err
					continue
				}
				return nil, err
			}
			if err := checkItem(item, r, id); err != nil {
				miss = err
				continue
			}
			return []capability.OcsfPayload{inventoryEvent(item)}, nil
		}
		if len(ids) > 0 && !pinned {
			return nil, miss
		}
	}
	return nil, miss
}

// latestItem is the resource's newest configuration item.
func latestItem(ctx context.Context, client configAPI, typ, id string) (cstypes.ConfigurationItem, error) {
	out, err := client.GetResourceConfigHistory(ctx, &configservice.GetResourceConfigHistoryInput{
		ResourceType:       cstypes.ResourceType(typ),
		ResourceId:         aws.String(id),
		Limit:              1,
		ChronologicalOrder: cstypes.ChronologicalOrderReverse,
	})
	if err != nil {
		return cstypes.ConfigurationItem{}, configErr(err, "")
	}
	if len(out.ConfigurationItems) == 0 {
		return cstypes.ConfigurationItem{}, configErr(nil, "AWS Config holds no configuration item for "+typ+" "+id)
	}
	item := out.ConfigurationItems[0]
	switch item.ConfigurationItemStatus {
	case cstypes.ConfigurationItemStatusOk, cstypes.ConfigurationItemStatusResourceDiscovered:
		return item, nil
	}
	// Deleted, not recorded, or a status this adapter does not know: the
	// newest item is not a configuration the resource has now.
	return cstypes.ConfigurationItem{}, configErr(nil, "AWS Config's newest item for "+typ+" "+id+" has status "+strconv.Quote(string(item.ConfigurationItemStatus)))
}

// checkItem confirms the item is the resource asked about: the same ARN for
// a resource given by ARN; for one given by name, the account, region and
// zone the call scoped it to, when given (a name is unique only within
// them; an item with no zone does not match a given one).
func checkItem(item cstypes.ConfigurationItem, r resourceScope, id string) error {
	arn := aws.ToString(item.Arn)
	if strings.HasPrefix(r.uid, "arn:") && arn != "" && arn != r.uid {
		return configErr(nil, "AWS Config's "+r.typ+" "+id+" is "+arn+", not "+r.uid)
	}
	if r.uid != "" {
		return nil
	}
	if a := aws.ToString(item.AccountId); r.accountUID != "" && a != r.accountUID {
		return configErr(nil, "AWS Config's "+r.typ+" "+id+" is in account "+a+", not "+r.accountUID)
	}
	if g := aws.ToString(item.AwsRegion); r.region != "" && g != r.region {
		return configErr(nil, "AWS Config's "+r.typ+" "+id+" is in region "+g+", not "+r.region)
	}
	if z := aws.ToString(item.AvailabilityZone); r.zone != "" && !strings.EqualFold(z, r.zone) {
		return configErr(nil, "AWS Config's "+r.typ+" "+id+" is in zone "+strconv.Quote(z)+", not "+r.zone)
	}
	return nil
}

// configLookups are the ListDiscoveredResources calls that find a resource
// given by ARN or by name, in order. Config keys a resource by its own id
// (the bucket name for S3, the unique id for IAM, the full ARN for Secrets
// Manager), not by its ARN, so the lookups are: by name (the name given,
// else the one the ARN carries), by the ARN's resource id, then by the full
// ARN as an id.
func configLookups(r resourceScope) []*configservice.ListDiscoveredResourcesInput {
	parts, _ := parseARN(r.uid)
	typ := cstypes.ResourceType(r.typ)
	var lookups []*configservice.ListDiscoveredResourcesInput
	if name := firstNonEmpty(r.name, parts.name); name != "" {
		lookups = append(lookups, &configservice.ListDiscoveredResourcesInput{ResourceType: typ, ResourceName: aws.String(name), Limit: discoverLimit})
	}
	if parts.id != "" {
		lookups = append(lookups, &configservice.ListDiscoveredResourcesInput{ResourceType: typ, ResourceIds: []string{parts.id}, Limit: discoverLimit})
	}
	if r.uid != "" && r.uid != parts.id {
		lookups = append(lookups, &configservice.ListDiscoveredResourcesInput{ResourceType: typ, ResourceIds: []string{r.uid}, Limit: discoverLimit})
	}
	return lookups
}

// discoverLimit asks for one more match than an answer needs, so a name that
// is not unique is seen as such.
const discoverLimit = 2

// discover is the resource ids a ListDiscoveredResources call returns.
func discover(ctx context.Context, client configAPI, in *configservice.ListDiscoveredResourcesInput) ([]string, error) {
	out, err := client.ListDiscoveredResources(ctx, in)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, ri := range out.ResourceIdentifiers {
		if id := aws.ToString(ri.ResourceId); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// arnKind is one resource kind an ARN can name: the service, the
// resource-type prefix of the ARN's resource part (with its separator; empty
// when the ARN names the resource directly, as for an S3 bucket), and the
// AWS Config resource type. The name the resource goes by follows the
// prefix, inner slashes kept (an ECR repository "team/app"), except where
// the kind says otherwise.
type arnKind struct {
	service, prefix, configType string
	// pathed: the part after the prefix is a path ending in the name (an IAM
	// role "service/app" is named "app").
	pathed bool
	// suffixed: the name carries a random "-XXXXXX" suffix the service adds
	// (a Secrets Manager secret "prod/db-AbCdEf" is named "prod/db").
	suffixed bool
	// qualified: a ":qualifier" (a Lambda version or alias) may follow the
	// name; it is dropped ("app:prod" is the function "app").
	qualified bool
}

// arnKinds is the one table of the ARN kinds the adapter maps, for a context
// call that names a resource by ARN without its type. An ARN it does not
// cover leaves the type unknown.
var arnKinds = []arnKind{ //nolint:gosec // G101: ARN prefixes mapped to Config resource type names (key/, secret:), not credentials
	{service: "s3", configType: "AWS::S3::Bucket"},
	{service: "iam", prefix: "user/", configType: "AWS::IAM::User", pathed: true},
	{service: "iam", prefix: "role/", configType: "AWS::IAM::Role", pathed: true},
	{service: "iam", prefix: "policy/", configType: "AWS::IAM::Policy", pathed: true},
	{service: "iam", prefix: "group/", configType: "AWS::IAM::Group", pathed: true},
	{service: "ec2", prefix: "instance/", configType: "AWS::EC2::Instance"},
	{service: "ec2", prefix: "security-group/", configType: "AWS::EC2::SecurityGroup"},
	{service: "ec2", prefix: "volume/", configType: "AWS::EC2::Volume"},
	{service: "ec2", prefix: "vpc/", configType: "AWS::EC2::VPC"},
	{service: "ec2", prefix: "subnet/", configType: "AWS::EC2::Subnet"},
	{service: "ec2", prefix: "network-interface/", configType: "AWS::EC2::NetworkInterface"},
	{service: "lambda", prefix: "function:", configType: "AWS::Lambda::Function", qualified: true},
	{service: "kms", prefix: "key/", configType: "AWS::KMS::Key"},
	{service: "sns", configType: "AWS::SNS::Topic"},
	{service: "sqs", configType: "AWS::SQS::Queue"},
	{service: "dynamodb", prefix: "table/", configType: "AWS::DynamoDB::Table"},
	{service: "rds", prefix: "db:", configType: "AWS::RDS::DBInstance"},
	{service: "secretsmanager", prefix: "secret:", configType: "AWS::SecretsManager::Secret", suffixed: true},
	{service: "cloudtrail", prefix: "trail/", configType: "AWS::CloudTrail::Trail"},
	{service: "eks", prefix: "cluster/", configType: "AWS::EKS::Cluster"},
	{service: "ecr", prefix: "repository/", configType: "AWS::ECR::Repository"},
}

// secretSuffix is the random suffix Secrets Manager appends to a secret's
// name in its ARN.
var secretSuffix = regexp.MustCompile(`-[A-Za-z0-9]{6}$`)

// arnParts is what an ARN of a mapped kind says about the resource: its
// Config type, the name it goes by, and the id its ARN carries (the resource
// part after the type prefix).
type arnParts struct {
	configType, name, id string
}

// parseARN reads an ARN against arnKinds; ok is false when it is not an ARN
// or names a kind the table does not cover. For an uncovered kind the parts
// still carry a generic name and id — the text after the last '/', else after
// the last ':' — with no type, so a caller who gives the type can still be
// looked up ("natgateway/nat-0abc" → "nat-0abc").
func parseARN(arn string) (arnParts, bool) {
	f := strings.SplitN(arn, ":", 6)
	if len(f) != 6 || f[0] != "arn" || f[5] == "" {
		return arnParts{}, false
	}
	service, resource := f[2], f[5]
	for _, k := range arnKinds {
		if k.service != service {
			continue
		}
		var rest string
		switch {
		case k.prefix == "" && !strings.ContainsAny(resource, "/:"):
			rest = resource
		case k.prefix != "" && strings.HasPrefix(resource, k.prefix):
			rest = strings.TrimPrefix(resource, k.prefix)
		}
		if rest == "" {
			continue
		}
		if k.qualified {
			rest, _, _ = strings.Cut(rest, ":")
		}
		name := rest
		if k.pathed {
			name = rest[strings.LastIndex(rest, "/")+1:]
		}
		if k.suffixed {
			name = secretSuffix.ReplaceAllString(name, "")
		}
		return arnParts{configType: k.configType, name: name, id: rest}, true
	}
	tail := resource
	if i := strings.LastIndex(resource, "/"); i >= 0 {
		tail = resource[i+1:]
	} else if i := strings.LastIndex(resource, ":"); i >= 0 {
		tail = resource[i+1:]
	}
	return arnParts{name: tail, id: tail}, false
}

// configTypeFromARN is the Config resource type an ARN names, "" when it is
// not an ARN or the table does not cover it.
func configTypeFromARN(arn string) string {
	p, _ := parseARN(arn)
	return p.configType
}

// configErr classifies a Config failure: a resource Config cannot answer for
// is FALLTHROUGH, anything else by the shared table.
func configErr(err error, msg string) error {
	if err == nil {
		return withClass(capability.ErrFallthrough, msg, nil)
	}
	if fallthroughConfigCodes[errorCode(err)] {
		return withClass(capability.ErrFallthrough, firstNonEmpty(msg, "AWS Config cannot answer for this resource"), err)
	}
	return err
}

// inventoryEvent shapes a configuration item into an OCSF 5023 event
// (adr/0322): one resource with its identifier, name, type, region,
// configuration, tags and times, under the account it belongs to.
func inventoryEvent(item cstypes.ConfigurationItem) capability.OcsfPayload {
	region := aws.ToString(item.AwsRegion)
	resource := map[string]any{}
	put := func(k, v string) {
		if v != "" {
			resource[k] = v
		}
	}
	put("uid", aws.ToString(item.Arn))
	put("name", aws.ToString(item.ResourceName))
	put("type", string(item.ResourceType))
	put("region", region)
	if data := configData(item); data != nil {
		resource["data"] = data
	}
	if len(item.Tags) > 0 {
		names := make([]string, 0, len(item.Tags))
		for k := range item.Tags {
			names = append(names, k)
		}
		sort.Strings(names)
		tags := make([]any, 0, len(names))
		for _, k := range names {
			tags = append(tags, map[string]any{"name": k, "value": item.Tags[k]})
		}
		resource["tags"] = tags
	}
	if t := item.ResourceCreationTime; t != nil {
		resource["created_time"] = t.UnixMilli()
	}
	captured := aws.ToTime(item.ConfigurationItemCaptureTime).UTC()
	if item.ConfigurationItemCaptureTime != nil {
		resource["modified_time"] = captured.UnixMilli()
	}

	cloud := map[string]any{"provider": "AWS"}
	if a := aws.ToString(item.AccountId); a != "" {
		cloud["account"] = map[string]any{"uid": a}
	}
	if region != "" {
		cloud["region"] = region
	}
	raw := map[string]any{
		"class_uid":     classInventory,
		"class_name":    classNameInventory,
		"category_uid":  5,
		"type_uid":      502302,
		"activity_id":   2,
		"activity_name": "Collect",
		"metadata": map[string]any{
			"product": map[string]any{"vendor_name": "AWS", "name": "AWS Config"},
			"version": inventoryVersion,
		},
		"cloud":     cloud,
		"resources": []any{resource},
	}
	if item.ConfigurationItemCaptureTime != nil {
		raw["time"] = captured.UnixMilli()
	}
	return capability.OcsfPayload{ClassUID: classInventory, ClassName: classNameInventory, Time: timeOrZero(item.ConfigurationItemCaptureTime), Raw: raw}
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

// configData is the resource's configuration as one object: the item's
// configuration decoded, with each supplementary configuration entry (for S3
// the bucket policy, ACL, public access block, encryption…) merged in at the
// top level under its Config key, its value decoded by supplementaryValue
// (wrapper layers and named embedded documents only). On a key clash the
// base value stays and the supplementary one is filed by mergeClashes. A
// configuration that is not JSON is kept as the string it is.
func configData(item cstypes.ConfigurationItem) any {
	var base any
	if c := aws.ToString(item.Configuration); c != "" {
		if err := json.Unmarshal([]byte(c), &base); err != nil {
			base = c
		}
	}
	if len(item.SupplementaryConfiguration) == 0 {
		return base
	}
	obj, ok := base.(map[string]any)
	switch {
	case ok:
	case base == nil:
		obj = map[string]any{}
	default:
		obj = map[string]any{"configuration": base}
	}
	keys := make([]string, 0, len(item.SupplementaryConfiguration))
	for k := range item.SupplementaryConfiguration {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	clashes := map[string]any{}
	for _, k := range keys {
		v := supplementaryValue(k, item.SupplementaryConfiguration[k])
		if _, taken := obj[k]; taken {
			clashes[k] = v
			continue
		}
		obj[k] = v
	}
	if len(clashes) > 0 {
		mergeClashes(obj, clashes)
	}
	return obj
}

// mergeClashes files supplementary entries whose key the base already has.
// They go under supplementary_configuration; if the base itself has that
// key, they merge into it when it is an object (its own keys win, and an
// entry that would replace one goes under supplementary_configuration_clashes)
// and otherwise go under supplementary_configuration_clashes.
func mergeClashes(obj, clashes map[string]any) {
	const key, fallback = "supplementary_configuration", "supplementary_configuration_clashes"
	existing, taken := obj[key]
	if !taken {
		obj[key] = clashes
		return
	}
	m, ok := existing.(map[string]any)
	if !ok {
		obj[fallback] = clashes
		return
	}
	rest := map[string]any{}
	for k, v := range clashes {
		if _, has := m[k]; has {
			rest[k] = v
			continue
		}
		m[k] = v
	}
	if len(rest) > 0 {
		obj[fallback] = rest
	}
}

// maxUnwrap bounds unwrapJSON on a pathological value.
const maxUnwrap = 8

// embeddedJSONFields names, per supplementary configuration key, the fields
// of its decoded document that hold a further JSON document as a string,
// which are decoded one level each. Every other string, at any depth, is
// kept verbatim: a policy's values (a condition on "s3:prefix" of "[1]")
// are data, not encodings.
//   - BucketPolicy: {"policyText": "<the bucket policy JSON>"}.
//   - AccessControlList, PublicAccessBlockConfiguration,
//     ServerSideEncryptionConfiguration and the other S3 entries are a JSON
//     string of an object with no further string-encoded field; unwrapJSON
//     alone decodes them.
var embeddedJSONFields = map[string][]string{
	"BucketPolicy": {"policyText"},
}

// supplementaryValue decodes one supplementary configuration entry: the
// wrapper layers Config adds (unwrapJSON), then the entry's named embedded
// documents (embeddedJSONFields), nothing else.
func supplementaryValue(key, raw string) any {
	v := unwrapJSON(raw)
	obj, ok := v.(map[string]any)
	if !ok {
		return v
	}
	for _, field := range embeddedJSONFields[key] {
		if s, ok := obj[field].(string); ok {
			obj[field] = unwrapJSON(s)
		}
	}
	return obj
}

// unwrapJSON removes JSON-string wrapping from s: while s parses as a JSON
// string it is unwrapped, and the first object or array it parses as is
// returned. A string that does not parse, or parses as a number, boolean or
// null, is returned as given. The returned document's own strings are never
// decoded.
func unwrapJSON(s string) any {
	cur := s
	for range maxUnwrap {
		var parsed any
		if err := json.Unmarshal([]byte(cur), &parsed); err != nil {
			return s
		}
		switch p := parsed.(type) {
		case map[string]any, []any:
			return p
		case string:
			cur = p
		default:
			return s
		}
	}
	return s
}
