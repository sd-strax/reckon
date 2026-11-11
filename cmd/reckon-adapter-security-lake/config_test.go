package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/configservice"
	cstypes "github.com/aws/aws-sdk-go-v2/service/configservice/types"
	"github.com/aws/smithy-go"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
)

var (
	bucketCreated  = time.Date(2025, 3, 11, 16, 42, 0, 0, time.UTC)
	bucketCaptured = time.Date(2026, 7, 14, 9, 28, 0, 0, time.UTC)
)

// bucketConfig is a Config client that knows the scenario's bucket.
func bucketConfig() *fakeConfig {
	return &fakeConfig{
		discovered: &configservice.ListDiscoveredResourcesOutput{ResourceIdentifiers: []cstypes.ResourceIdentifier{
			{ResourceId: aws.String("acme-internal-exports"), ResourceName: aws.String("acme-internal-exports"), ResourceType: cstypes.ResourceTypeBucket},
		}},
		history: &configservice.GetResourceConfigHistoryOutput{ConfigurationItems: []cstypes.ConfigurationItem{{
			AccountId:                    aws.String("111122223333"),
			Arn:                          aws.String("arn:aws:s3:::acme-internal-exports"),
			AwsRegion:                    aws.String("us-east-1"),
			ResourceId:                   aws.String("acme-internal-exports"),
			ResourceName:                 aws.String("acme-internal-exports"),
			ResourceType:                 cstypes.ResourceTypeBucket,
			Configuration:                aws.String(`{"name":"acme-internal-exports","owner":{"id":"abc"},"creationDate":"2025-03-11T16:42:00.000Z"}`),
			Tags:                         map[string]string{"env": "prod", "data-class": "internal"},
			ResourceCreationTime:         aws.Time(bucketCreated),
			ConfigurationItemCaptureTime: aws.Time(bucketCaptured),
		}}},
	}
}

func bucketParams() map[string]any {
	return map[string]any{"verb": "get_cloud_resource_context", "resource": map[string]any{
		"uid": "arn:aws:s3:::acme-internal-exports", "name": "acme-internal-exports", "provider": "aws",
		"type": "AWS::S3::Bucket", "account_uid": "111122223333", "region": "us-east-1",
	}}
}

func TestResourceContext_ShapesInventory(t *testing.T) {
	c := bucketConfig()
	events, err := resourceContext(context.Background(), c, bucketParams())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.listIn) != 1 || aws.ToString(c.listIn[0].ResourceName) != "acme-internal-exports" || c.listIn[0].ResourceType != cstypes.ResourceTypeBucket {
		t.Errorf("discovery lookup = %+v", c.listIn)
	}
	in := c.historyIn[0]
	if aws.ToString(in.ResourceId) != "acme-internal-exports" || in.ResourceType != cstypes.ResourceTypeBucket || in.Limit != 1 || in.ChronologicalOrder != cstypes.ChronologicalOrderReverse {
		t.Errorf("history input = %+v", in)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	e := events[0]
	if e.ClassUID != 5023 || e.ClassName != "Cloud Resources Inventory Info" || !e.Time.Equal(bucketCaptured) {
		t.Errorf("event = %d %q %s", e.ClassUID, e.ClassName, e.Time)
	}
	want := map[string]any{
		"class_uid": 5023, "class_name": "Cloud Resources Inventory Info", "category_uid": 5, "type_uid": 502302,
		"activity_id": 2, "activity_name": "Collect", "time": bucketCaptured.UnixMilli(),
		"metadata": map[string]any{"product": map[string]any{"vendor_name": "AWS", "name": "AWS Config"}, "version": "1.7.0"},
		"cloud":    map[string]any{"provider": "AWS", "account": map[string]any{"uid": "111122223333"}, "region": "us-east-1"},
		"resources": []any{map[string]any{
			"uid": "arn:aws:s3:::acme-internal-exports", "name": "acme-internal-exports", "type": "AWS::S3::Bucket", "region": "us-east-1",
			"data":         map[string]any{"name": "acme-internal-exports", "owner": map[string]any{"id": "abc"}, "creationDate": "2025-03-11T16:42:00.000Z"},
			"tags":         []any{map[string]any{"name": "data-class", "value": "internal"}, map[string]any{"name": "env", "value": "prod"}},
			"created_time": bucketCreated.UnixMilli(), "modified_time": bucketCaptured.UnixMilli(),
		}},
	}
	if !reflect.DeepEqual(e.Raw, want) {
		got, _ := json.MarshalIndent(e.Raw, "", " ")
		t.Errorf("raw =\n%s", got)
	}
}

// TestResourceContext_MeetsTheCalls pins that the Config snapshot mints the
// resource id the fixture snapshot and the lake's calls on the bucket mint.
func TestResourceContext_MeetsTheCalls(t *testing.T) {
	events, err := resourceContext(context.Background(), bucketConfig(), bucketParams())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := idsOf(normalizeWire(t, sourceConfig, events)[0])
	fixture := idsOf(normalizeFixture(t, "inventory-bucket-1.json"))
	calls := idsOf(normalizeWire(t, sourceLake, lakeEvents(t, "put-bucket-policy.json"))[0])
	got := snapshot[identity.TypeCloudResource]
	if len(got) != 1 {
		t.Fatalf("snapshot resources = %v", got)
	}
	if !reflect.DeepEqual(got, fixture[identity.TypeCloudResource]) {
		t.Errorf("snapshot resource = %v, fixture snapshot = %v", got, fixture[identity.TypeCloudResource])
	}
	if !reflect.DeepEqual(got, calls[identity.TypeCloudResource]) {
		t.Errorf("snapshot resource = %v, lake PutBucketPolicy = %v", got, calls[identity.TypeCloudResource])
	}
	if !reflect.DeepEqual(snapshot[identity.TypeCloudAccount], fixture[identity.TypeCloudAccount]) {
		t.Errorf("snapshot account = %v, fixture = %v", snapshot[identity.TypeCloudAccount], fixture[identity.TypeCloudAccount])
	}
}

func TestResourceContext_ConfigIDUsedDirectly(t *testing.T) {
	c := bucketConfig()
	c.history.ConfigurationItems[0].Arn = aws.String("arn:aws:ec2:us-east-1:111122223333:instance/i-0abc")
	c.history.ConfigurationItems[0].Configuration = aws.String("not json")
	events, err := resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"uid": "i-0abc", "type": "AWS::EC2::Instance"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.listIn) != 0 || aws.ToString(c.historyIn[0].ResourceId) != "i-0abc" {
		t.Errorf("a non-ARN uid is Config's own id: list=%d history=%+v", len(c.listIn), c.historyIn[0])
	}
	if d := events[0].Raw["resources"].([]any)[0].(map[string]any)["data"]; d != "not json" {
		t.Errorf("data = %v, want the raw string", d)
	}
}

func TestResourceContext_Declines(t *testing.T) {
	for name, res := range map[string]map[string]any{
		"name with no type":         {"name": "acme-internal-exports"},
		"unmapped ARN with no type": {"uid": "arn:aws:s3:::acme-internal-exports/object.csv"},
		"not an ARN and no type":    {"uid": "i-0abc"},
		"no uid and no name":        {"type": "AWS::S3::Bucket"},
		"another provider":          {"uid": "x", "type": "t", "provider": "gcp"},
	} {
		t.Run(name, func(t *testing.T) {
			c := bucketConfig()
			_, err := resourceContext(context.Background(), c, map[string]any{"resource": res})
			isClass(t, err, capability.ErrFallthrough, "")
			if len(c.historyIn) != 0 {
				t.Error("Config must not be asked")
			}
		})
	}
}

// TestResourceContext_TypeFromARN: a resource given by ARN alone — the
// descriptor's {provider, uid} form — is asked about under the Config type
// its ARN names.
func TestResourceContext_TypeFromARN(t *testing.T) {
	c := bucketConfig()
	events, err := resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"uid": "arn:aws:s3:::acme-internal-exports", "provider": "aws"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || c.historyIn[0].ResourceType != cstypes.ResourceTypeBucket || aws.ToString(c.listIn[0].ResourceName) != "acme-internal-exports" {
		t.Errorf("history = %+v list = %+v", c.historyIn, c.listIn)
	}
	for arn, want := range map[string]string{
		"arn:aws:s3:::acme-internal-exports":                                  "AWS::S3::Bucket",
		"arn:aws:iam::111122223333:user/svc-backup":                           "AWS::IAM::User",
		"arn:aws:iam::111122223333:role/deploy-role":                          "AWS::IAM::Role",
		"arn:aws:iam::aws:policy/AdministratorAccess":                         "AWS::IAM::Policy",
		"arn:aws:iam::111122223333:group/admins":                              "AWS::IAM::Group",
		"arn:aws:ec2:us-east-1:111122223333:instance/i-0abc":                  "AWS::EC2::Instance",
		"arn:aws:ec2:us-east-1:111122223333:security-group/sg-1":              "AWS::EC2::SecurityGroup",
		"arn:aws:ec2:us-east-1:111122223333:volume/vol-1":                     "AWS::EC2::Volume",
		"arn:aws:ec2:us-east-1:111122223333:vpc/vpc-1":                        "AWS::EC2::VPC",
		"arn:aws:lambda:us-east-1:111122223333:function:export":               "AWS::Lambda::Function",
		"arn:aws:kms:us-east-1:111122223333:key/1234abcd-12ab-34cd-56ef-1234": "AWS::KMS::Key",
		"arn:aws:sns:us-east-1:111122223333:alerts":                           "AWS::SNS::Topic",
		"arn:aws:s3:::bucket/key.csv":                                         "",
		"arn:aws:ec2:us-east-1:111122223333:image/ami-1":                      "",
		"not-an-arn": "",
	} {
		if got := configTypeFromARN(arn); got != want {
			t.Errorf("configTypeFromARN(%q) = %q, want %q", arn, got, want)
		}
	}
}

// TestResourceContext_LookupByIDFallback: when the ARN's tail is not a
// resource name Config knows, it is tried as Config's resource id.
func TestResourceContext_LookupByIDFallback(t *testing.T) {
	c := bucketConfig()
	c.discovered = nil
	c.byID = &configservice.ListDiscoveredResourcesOutput{ResourceIdentifiers: []cstypes.ResourceIdentifier{{ResourceId: aws.String("acme-internal-exports")}}}
	if _, err := resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"uid": "arn:aws:s3:::acme-internal-exports"}}); err != nil {
		t.Fatal(err)
	}
	if len(c.listIn) != 2 || aws.ToString(c.listIn[0].ResourceName) != "acme-internal-exports" ||
		!reflect.DeepEqual(c.listIn[1].ResourceIds, []string{"acme-internal-exports"}) {
		t.Errorf("lookups = %+v", c.listIn)
	}
	if aws.ToString(c.historyIn[0].ResourceId) != "acme-internal-exports" {
		t.Errorf("history id = %s", aws.ToString(c.historyIn[0].ResourceId))
	}
}

// TestResourceContext_NameScopeChecked: a resource given by name is the
// item only in the account and region the call scoped it to.
func TestResourceContext_NameScopeChecked(t *testing.T) {
	base := map[string]any{"name": "acme-internal-exports", "type": "AWS::S3::Bucket"}
	for name, extra := range map[string]map[string]any{
		"other account": {"account_uid": "444455556666"},
		"other region":  {"region": "eu-west-1"},
	} {
		res := map[string]any{}
		for k, v := range base {
			res[k] = v
		}
		for k, v := range extra {
			res[k] = v
		}
		_, err := resourceContext(context.Background(), bucketConfig(), map[string]any{"resource": res})
		if classify(err) != capability.ErrFallthrough {
			t.Errorf("%s: err = %v, want FALLTHROUGH", name, err)
		}
	}
	base["account_uid"], base["region"] = "111122223333", "us-east-1"
	if _, err := resourceContext(context.Background(), bucketConfig(), map[string]any{"resource": base}); err != nil {
		t.Errorf("matching scope: %v", err)
	}
}

func TestResourceContext_ConfigCannotAnswer(t *testing.T) {
	undiscovered := &fakeConfig{}
	_, err := resourceContext(context.Background(), undiscovered, bucketParams())
	isClass(t, err, capability.ErrFallthrough, "has not discovered")

	c := bucketConfig()
	c.historyErr = &smithy.GenericAPIError{Code: "ResourceNotDiscoveredException", Message: "not discovered"}
	_, err = resourceContext(context.Background(), c, bucketParams())
	isClass(t, err, capability.ErrFallthrough, "")

	c = bucketConfig()
	c.historyErr = &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "no"}
	_, err = resourceContext(context.Background(), c, bucketParams())
	isClass(t, err, capability.ErrUnhealthy, "")

	c = bucketConfig()
	c.history.ConfigurationItems[0].Arn = aws.String("arn:aws:s3:::someone-else")
	_, err = resourceContext(context.Background(), c, bucketParams())
	isClass(t, err, capability.ErrFallthrough, "not arn:aws:s3:::acme-internal-exports")
}

func TestInvoke_ConfigDisabledFallsThrough(t *testing.T) {
	c := bucketConfig()
	b := testBridge(t, map[string]any{"config_enabled": false}, &fakeAthena{}, &fakeGlue{}, c)
	raw, _ := json.Marshal(map[string]any{"operation": opContext, "params": bucketParams()})
	_, werr := b.invoke(context.Background(), raw)
	if got := classOf(t, werr); got != string(capability.ErrFallthrough) {
		t.Errorf("class = %s, want FALLTHROUGH", got)
	}
	if len(c.historyIn) != 0 {
		t.Error("Config must not be called when disabled")
	}
}

// TestResourceContext_NameNotUnique: two resources of the type share the
// name and nothing pins which is meant, so Config does not answer.
func TestResourceContext_NameNotUnique(t *testing.T) {
	c := bucketConfig()
	c.discovered = &configservice.ListDiscoveredResourcesOutput{ResourceIdentifiers: []cstypes.ResourceIdentifier{
		{ResourceId: aws.String("AROAONE")}, {ResourceId: aws.String("AROATWO")},
	}}
	_, err := resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"name": "deploy", "type": "AWS::IAM::Role"}})
	isClass(t, err, capability.ErrFallthrough, "not unique")
	if c.listIn[0].Limit != 2 {
		t.Errorf("Limit = %d, want 2", c.listIn[0].Limit)
	}
	if len(c.historyIn) != 0 {
		t.Error("an ambiguous name must not be resolved by guessing")
	}
}

// TestResourceContext_ARNPinsAmongCandidates: with an ARN, the candidate
// whose item carries that ARN is the answer.
func TestResourceContext_ARNPinsAmongCandidates(t *testing.T) {
	const want = "arn:aws:iam::111122223333:role/service/app"
	c := &fakeConfig{
		discovered: &configservice.ListDiscoveredResourcesOutput{ResourceIdentifiers: []cstypes.ResourceIdentifier{
			{ResourceId: aws.String("AROAONE")}, {ResourceId: aws.String("AROATWO")},
		}},
		items: map[string]cstypes.ConfigurationItem{
			"AROAONE": {Arn: aws.String("arn:aws:iam::111122223333:role/other/app"), ResourceType: cstypes.ResourceTypeRole, ResourceName: aws.String("app")},
			"AROATWO": {Arn: aws.String(want), ResourceType: cstypes.ResourceTypeRole, ResourceName: aws.String("app")},
		},
	}
	events, err := resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"uid": want}})
	if err != nil {
		t.Fatal(err)
	}
	if got := events[0].Raw["resources"].([]any)[0].(map[string]any)["uid"]; got != want {
		t.Errorf("uid = %v", got)
	}
	if aws.ToString(c.listIn[0].ResourceName) != "app" || c.historyIn[0].ResourceType != cstypes.ResourceTypeRole {
		t.Errorf("lookup = %+v / %+v", c.listIn[0], c.historyIn[0])
	}
}

func TestParseARN(t *testing.T) {
	for arn, want := range map[string]arnParts{
		"arn:aws:ecr:us-east-1:111122223333:repository/team/app":              {"AWS::ECR::Repository", "team/app", "team/app"},
		"arn:aws:secretsmanager:us-east-1:111122223333:secret:prod/db-AbCdEf": {"AWS::SecretsManager::Secret", "prod/db", "prod/db-AbCdEf"},
		"arn:aws:iam::111122223333:role/service/app":                          {"AWS::IAM::Role", "app", "service/app"},
		"arn:aws:iam::111122223333:user/svc-backup":                           {"AWS::IAM::User", "svc-backup", "svc-backup"},
		"arn:aws:s3:::acme-internal-exports":                                  {"AWS::S3::Bucket", "acme-internal-exports", "acme-internal-exports"},
		"arn:aws:lambda:us-east-1:111122223333:function:export":               {"AWS::Lambda::Function", "export", "export"},
		"arn:aws:ec2:us-east-1:111122223333:instance/i-0abc":                  {"AWS::EC2::Instance", "i-0abc", "i-0abc"},
	} {
		got, ok := parseARN(arn)
		if !ok || got != want {
			t.Errorf("parseARN(%q) = %+v, %v; want %+v", arn, got, ok, want)
		}
	}
	for _, bad := range []string{"arn:aws:s3:::bucket/key", "arn:aws:iam::111122223333:role/", "arn:aws:ecr:us-east-1:1:image/x", "nope"} {
		if p, ok := parseARN(bad); ok {
			t.Errorf("parseARN(%q) = %+v, want unmapped", bad, p)
		}
	}
}

// TestResourceContext_LookupOrder: by name, then by the ARN's resource id,
// then by the full ARN (Secrets Manager keys secrets by ARN).
func TestResourceContext_LookupOrder(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:111122223333:secret:prod/db-AbCdEf"
	c := &fakeConfig{}
	_, err := resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"uid": arn}})
	isClass(t, err, capability.ErrFallthrough, "has not discovered")
	if len(c.listIn) != 3 {
		t.Fatalf("lookups = %d, want 3", len(c.listIn))
	}
	if aws.ToString(c.listIn[0].ResourceName) != "prod/db" || c.listIn[0].ResourceType != "AWS::SecretsManager::Secret" {
		t.Errorf("by name = %+v", c.listIn[0])
	}
	if !reflect.DeepEqual(c.listIn[1].ResourceIds, []string{"prod/db-AbCdEf"}) || !reflect.DeepEqual(c.listIn[2].ResourceIds, []string{arn}) {
		t.Errorf("by id = %v, then %v", c.listIn[1].ResourceIds, c.listIn[2].ResourceIds)
	}

	c = &fakeConfig{
		byID:  &configservice.ListDiscoveredResourcesOutput{ResourceIdentifiers: []cstypes.ResourceIdentifier{{ResourceId: aws.String(arn)}}},
		items: map[string]cstypes.ConfigurationItem{arn: {Arn: aws.String(arn), ResourceType: "AWS::SecretsManager::Secret"}},
	}
	if _, err := resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"uid": arn}}); err != nil {
		t.Fatal(err)
	}
	if aws.ToString(c.historyIn[0].ResourceId) != arn {
		t.Errorf("history id = %s", aws.ToString(c.historyIn[0].ResourceId))
	}
}

func ident(ids ...string) *configservice.ListDiscoveredResourcesOutput {
	out := &configservice.ListDiscoveredResourcesOutput{}
	for _, id := range ids {
		out.ResourceIdentifiers = append(out.ResourceIdentifiers, cstypes.ResourceIdentifier{ResourceId: aws.String(id)})
	}
	return out
}

// TestResourceContext_UnmappedKindWithType: an ARN of a kind the table does
// not cover is still looked up when the caller gives its type, by the text
// after its last '/'.
func TestResourceContext_UnmappedKindWithType(t *testing.T) {
	const arn = "arn:aws:ec2:us-east-1:111122223333:natgateway/nat-0abc"
	c := &fakeConfig{
		byIDs: map[string]*configservice.ListDiscoveredResourcesOutput{"nat-0abc": ident("nat-0abc")},
		items: map[string]cstypes.ConfigurationItem{"nat-0abc": {Arn: aws.String(arn), ResourceType: "AWS::EC2::NatGateway"}},
	}
	events, err := resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"uid": arn, "type": "AWS::EC2::NatGateway"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || aws.ToString(c.historyIn[0].ResourceId) != "nat-0abc" {
		t.Errorf("history = %+v", c.historyIn)
	}
	var byID bool
	for _, in := range c.listIn {
		byID = byID || reflect.DeepEqual(in.ResourceIds, []string{"nat-0abc"})
	}
	if !byID {
		t.Errorf("lookups = %+v, want one by ResourceIds [nat-0abc]", c.listIn)
	}

	c = &fakeConfig{}
	_, err = resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"uid": arn}})
	isClass(t, err, capability.ErrFallthrough, "needs the resource type")
	if len(c.listIn) != 0 {
		t.Error("an unmapped kind with no type must not be looked up")
	}
}

func TestParseARN_QualifiedLambda(t *testing.T) {
	p, ok := parseARN("arn:aws:lambda:us-east-1:111122223333:function:app:prod")
	if !ok || p != (arnParts{"AWS::Lambda::Function", "app", "app"}) {
		t.Errorf("parseARN = %+v, %v", p, ok)
	}
	if p, ok := parseARN("arn:aws:ec2:us-east-1:1:natgateway/nat-0abc"); ok || p != (arnParts{name: "nat-0abc", id: "nat-0abc"}) {
		t.Errorf("unmapped = %+v, %v", p, ok)
	}
	if p, _ := parseARN("arn:aws:foo:us-east-1:1:thing:item"); p.id != "item" {
		t.Errorf("unmapped ':' kind = %+v", p)
	}
}

// TestResourceContext_PinnedSkipsUnanswerableCandidate: a candidate Config
// cannot answer for is skipped and the next one tried; another error class
// is returned.
func TestResourceContext_PinnedSkipsUnanswerableCandidate(t *testing.T) {
	const arn = "arn:aws:iam::111122223333:role/service/app"
	gone := &smithy.GenericAPIError{Code: "ResourceNotDiscoveredException", Message: "deleted"}
	c := &fakeConfig{
		discovered: ident("AROAGONE", "AROALIVE"),
		itemErrs:   map[string]error{"AROAGONE": gone},
		items:      map[string]cstypes.ConfigurationItem{"AROALIVE": {Arn: aws.String(arn), ResourceType: cstypes.ResourceTypeRole}},
	}
	if _, err := resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"uid": arn}}); err != nil {
		t.Fatalf("want the live candidate, got %v", err)
	}

	c.itemErrs["AROAGONE"] = &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "no"}
	_, err := resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"uid": arn}})
	isClass(t, err, capability.ErrUnhealthy, "")
}

// TestResourceContext_PinnedContinuesPastNameMisses: when no name candidate
// carries the pinned ARN, the id and full-ARN lookups still run.
func TestResourceContext_PinnedContinuesPastNameMisses(t *testing.T) {
	const arn = "arn:aws:iam::111122223333:role/service/app"
	c := &fakeConfig{
		discovered: ident("AROAOTHER"),
		byIDs:      map[string]*configservice.ListDiscoveredResourcesOutput{"service/app": ident(), arn: ident("AROARIGHT")},
		items: map[string]cstypes.ConfigurationItem{
			"AROAOTHER": {Arn: aws.String("arn:aws:iam::111122223333:role/other/app"), ResourceType: cstypes.ResourceTypeRole},
			"AROARIGHT": {Arn: aws.String(arn), ResourceType: cstypes.ResourceTypeRole},
		},
	}
	events, err := resourceContext(context.Background(), c, map[string]any{"resource": map[string]any{"uid": arn}})
	if err != nil {
		t.Fatal(err)
	}
	if got := events[0].Raw["resources"].([]any)[0].(map[string]any)["uid"]; got != arn {
		t.Errorf("uid = %v", got)
	}
	if len(c.listIn) != 3 || !reflect.DeepEqual(c.listIn[2].ResourceIds, []string{arn}) {
		t.Errorf("lookups = %+v", c.listIn)
	}
}

// TestResourceContext_SupplementaryConfiguration: an S3 item's
// supplementary configuration (policy, public access block) joins the
// configuration, double-encoded JSON decoded, a clashing key kept aside.
func TestResourceContext_SupplementaryConfiguration(t *testing.T) {
	policy := `{"Version":"2012-10-17","Statement":[{"Sid":"PublicRead","Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::acme-internal-exports/*"}]}`
	bucketPolicy, _ := json.Marshal(map[string]string{"policyText": policy})
	c := bucketConfig()
	c.history.ConfigurationItems[0].SupplementaryConfiguration = map[string]string{
		"BucketPolicy":                   string(bucketPolicy),
		"PublicAccessBlockConfiguration": `{"blockPublicAcls":false,"ignorePublicAcls":false,"blockPublicPolicy":false,"restrictPublicBuckets":false}`,
		"name":                           `{"from":"supplementary"}`,
	}
	events, err := resourceContext(context.Background(), c, bucketParams())
	if err != nil {
		t.Fatal(err)
	}
	data := events[0].Raw["resources"].([]any)[0].(map[string]any)["data"].(map[string]any)
	if data["name"] != "acme-internal-exports" || data["creationDate"] == nil {
		t.Errorf("base configuration lost: %v", data)
	}
	text := data["BucketPolicy"].(map[string]any)["policyText"].(map[string]any)
	if text["Version"] != "2012-10-17" || len(text["Statement"].([]any)) != 1 {
		t.Errorf("policyText = %v, want the decoded policy", text)
	}
	if pab := data["PublicAccessBlockConfiguration"].(map[string]any); pab["blockPublicPolicy"] != false {
		t.Errorf("public access block = %v", pab)
	}
	if extra := data["supplementary_configuration"].(map[string]any); !reflect.DeepEqual(extra["name"], map[string]any{"from": "supplementary"}) {
		t.Errorf("clash = %v", extra)
	}
}

func TestDecodeNested(t *testing.T) {
	twice, _ := json.Marshal(`{"a":[1,"{\"b\":true}"]}`)
	got := decodeNested(string(twice), 0)
	want := map[string]any{"a": []any{float64(1), map[string]any{"b": true}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decodeNested = %#v", got)
	}
	for _, keep := range []string{"plain", "42", `"just a string"`} {
		if got := decodeNested(keep, 0); got != keep {
			t.Errorf("decodeNested(%q) = %#v, want it kept", keep, got)
		}
	}
}

// TestResourceContext_ItemStatus: only a recorded, current item answers.
func TestResourceContext_ItemStatus(t *testing.T) {
	for status, ok := range map[cstypes.ConfigurationItemStatus]bool{
		cstypes.ConfigurationItemStatusOk:                         true,
		cstypes.ConfigurationItemStatusResourceDiscovered:         true,
		cstypes.ConfigurationItemStatusResourceDeleted:            false,
		cstypes.ConfigurationItemStatusResourceDeletedNotRecorded: false,
		cstypes.ConfigurationItemStatusResourceNotRecorded:        false,
		"SomethingNew": false,
		"":             false,
	} {
		c := bucketConfig()
		c.exactStatus = true
		c.history.ConfigurationItems[0].ConfigurationItemStatus = status
		_, err := resourceContext(context.Background(), c, bucketParams())
		if ok && err != nil {
			t.Errorf("%s: %v", status, err)
		}
		if !ok {
			isClass(t, err, capability.ErrFallthrough, string(status))
		}
	}
}

// TestResourceContext_NameScopes: a named resource's zone is checked against
// the item's availability zone; a group or namespace cannot be confirmed
// from Config; a uid ignores all three.
func TestResourceContext_NameScopes(t *testing.T) {
	named := func(extra map[string]any) map[string]any {
		res := map[string]any{"name": "acme-internal-exports", "type": "AWS::S3::Bucket"}
		for k, v := range extra {
			res[k] = v
		}
		return map[string]any{"resource": res}
	}
	c := bucketConfig()
	_, err := resourceContext(context.Background(), c, named(map[string]any{"zone": "us-east-1a"}))
	isClass(t, err, capability.ErrFallthrough, "zone")

	c = bucketConfig()
	c.history.ConfigurationItems[0].AvailabilityZone = aws.String("US-EAST-1A")
	if _, err := resourceContext(context.Background(), c, named(map[string]any{"zone": "us-east-1a"})); err != nil {
		t.Errorf("matching zone: %v", err)
	}
	c = bucketConfig()
	c.history.ConfigurationItems[0].AvailabilityZone = aws.String("us-east-1b")
	_, err = resourceContext(context.Background(), c, named(map[string]any{"zone": "us-east-1a"}))
	isClass(t, err, capability.ErrFallthrough, "zone")

	for _, key := range []string{"group", "namespace"} {
		c = bucketConfig()
		_, err = resourceContext(context.Background(), c, named(map[string]any{key: "x"}))
		isClass(t, err, capability.ErrFallthrough, key+" scope cannot be established from AWS Config")
		if len(c.listIn) != 0 {
			t.Errorf("%s: Config must not be asked", key)
		}
	}

	c = bucketConfig()
	byUID := bucketParams()
	res := byUID["resource"].(map[string]any)
	res["zone"], res["group"], res["namespace"] = "us-east-1a", "g", "ns"
	if _, err := resourceContext(context.Background(), c, byUID); err != nil {
		t.Errorf("a uid must ignore zone, group and namespace: %v", err)
	}
}

// TestConfigData_ClashKeys: supplementary entries whose key the base has go
// under supplementary_configuration, merged into a base object of that name
// (its own keys kept), else under supplementary_configuration_clashes.
func TestConfigData_ClashKeys(t *testing.T) {
	item := func(base string) cstypes.ConfigurationItem {
		return cstypes.ConfigurationItem{Configuration: aws.String(base), SupplementaryConfiguration: map[string]string{"name": `{"s":1}`, "kept": `{"k":1}`}}
	}
	got := configData(item(`{"name":"b","supplementary_configuration":{"own":true}}`)).(map[string]any)
	if want := map[string]any{"own": true, "name": map[string]any{"s": float64(1)}}; !reflect.DeepEqual(got["supplementary_configuration"], want) {
		t.Errorf("object base: %v", got["supplementary_configuration"])
	}
	got = configData(item(`{"name":"b","supplementary_configuration":"scalar"}`)).(map[string]any)
	if got["supplementary_configuration"] != "scalar" || !reflect.DeepEqual(got["supplementary_configuration_clashes"], map[string]any{"name": map[string]any{"s": float64(1)}}) {
		t.Errorf("scalar base: %v", got)
	}
	got = configData(item(`{"name":"b","supplementary_configuration":{"name":"own"}}`)).(map[string]any)
	if sc := got["supplementary_configuration"].(map[string]any); sc["name"] != "own" ||
		!reflect.DeepEqual(got["supplementary_configuration_clashes"], map[string]any{"name": map[string]any{"s": float64(1)}}) {
		t.Errorf("clash inside the base object: %v", got)
	}
	if !reflect.DeepEqual(got["kept"], map[string]any{"k": float64(1)}) {
		t.Errorf("non-clashing entry = %v", got["kept"])
	}
}
