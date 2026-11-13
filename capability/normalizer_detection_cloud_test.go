package capability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// cloudDetection is a cloud detection finding at the OCSF floor: the account
// and the bucket are on the finding, and the one evidence artifact carries
// the caller and its address.
func cloudDetection() map[string]any {
	return versioned(2004, "1.1.0", map[string]any{
		"finding_info": map[string]any{"uid": "det-c1", "title": "Bucket made public"},
		"cloud": map[string]any{
			"provider": "AWS",
			"account":  map[string]any{"uid": "111122223333", "name": "acme-prod", "type_id": 10},
		},
		"resources": []any{map[string]any{"uid": "arn:aws:s3:::acme-data", "name": "acme-data", "type": "AWS::S3::Bucket"}},
		"evidences": []any{map[string]any{
			"actor": map[string]any{"user": map[string]any{
				"uid": deployARN, "name": "deploy-ci", "type_id": 1,
				"account":        map[string]any{"uid": "111122223333"},
				"credential_uid": "AKIAEXAMPLE",
			}},
			"src_endpoint": map[string]any{"ip": "203.0.113.77"},
		}},
	})
}

// refsOf is the ids of one type in a result's object_refs, sorted.
func refsOf(res NormalizationResult, typ string) []identity.STIXID {
	var out []identity.STIXID
	for _, od := range res.ObservedData {
		for _, r := range od.ObjectRefs {
			if strings.HasPrefix(string(r), typ+"--") {
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// TestDetectionCloud (§4.12, adr/0356): a finding that names a cloud provider
// yields the account and the resource from the finding itself and the cloud
// principal from the artifact's actor.user, by the API-activity rules. All
// three are in the observation the sighting names.
func TestDetectionCloud(t *testing.T) {
	res := normalizeOne(t, cloudDetection())
	ids := identity.NewResolver(testNS)

	account := ids.CloudAccount("aws", "111122223333")
	bucket, _ := ids.CloudResource(identity.CloudResourceIdentity{Provider: "aws", UID: "arn:aws:s3:::acme-data"})
	principal := ids.Account(identity.AccountIdentity{Type: "aws", UserID: deployARN})
	od := res.ObservedData[0]
	for name, id := range map[string]identity.STIXID{"account": account, "bucket": bucket, "principal": principal} {
		if !refsContain(od.ObjectRefs, id) {
			t.Errorf("object_refs lack the %s %s: %v", name, id, od.ObjectRefs)
		}
	}

	// The caller is the provider's principal and nothing else: no directory
	// login is minted from its name.
	users := scosOf(res, identity.TypeUserAccount)
	if len(users) != 1 || users[0].ID != principal {
		t.Fatalf("user-accounts = %+v; want only the cloud principal", users)
	}
	props := users[0].Properties
	if props["account_type"] != "aws" || props["account_login"] != "" || props["display_name"] != "deploy-ci" ||
		props["credential_uid"] != "AKIAEXAMPLE" || props["user_type"] != "User" {
		t.Errorf("principal properties = %v", props)
	}
	if got := scosOf(res, identity.TypeCloudAccount); len(got) != 1 || got[0].Properties["name"] != "acme-prod" || got[0].Properties["account_type"] != "AWS Account" {
		t.Errorf("cloud accounts = %+v", got)
	}
	// The principal belongs to its own account. The bucket is named by its
	// uid, which carries its own owner, so it is not asserted to belong.
	if !hasEdge(res, RelBelongsTo, principal, account) {
		t.Error("the principal does not belong to its account")
	}
	if hasEdge(res, RelBelongsTo, bucket, account) {
		t.Error("a resource named by uid is asserted to belong to the finding's account")
	}
	if len(scosOf(res, identity.TypeIPv4Addr)) != 1 {
		t.Errorf("addresses = %+v; want the caller's", scosOf(res, identity.TypeIPv4Addr))
	}
}

// TestDetectionCloudMeetsAPIActivity (adr/0356): an alert and an API call
// about the same account, resource and caller name the same three objects.
func TestDetectionCloudMeetsAPIActivity(t *testing.T) {
	call := versioned(6003, "1.1.0", map[string]any{
		"cloud": map[string]any{"provider": "AWS", "account": map[string]any{"uid": "111122223333"}},
		"actor": map[string]any{"user": map[string]any{"uid": deployARN, "name": "deploy-ci"}},
		"api":   map[string]any{"operation": "PutBucketPolicy"},
		"resources": []any{map[string]any{
			"uid": "arn:aws:s3:::acme-data", "name": "acme-data", "type": "AWS::S3::Bucket",
		}},
	})
	alert, api := normalizeOne(t, cloudDetection()), normalizeOne(t, call)
	for _, typ := range []string{identity.TypeCloudAccount, identity.TypeCloudResource, identity.TypeUserAccount} {
		a, b := refsOf(alert, typ), refsOf(api, typ)
		if len(a) != 1 || !slices.Equal(a, b) {
			t.Errorf("%s: the alert names %v, the API call %v; want one shared id", typ, a, b)
		}
	}
}

// TestDetectionCloudBothEnds (03 §4.16): a ceiling-shaped cloud finding of
// the same things names the same objects as the floor-shaped one. It carries
// fields that arrive above the floor: the credential comes from the list,
// and the cloud partition and the account's type caption are there.
func TestDetectionCloudBothEnds(t *testing.T) {
	ceiling := versioned(2004, "1.9.0", map[string]any{
		"finding_info": map[string]any{"uid": "det-c1", "title": "Bucket made public"},
		"cloud": map[string]any{
			"provider":        "AWS",
			"cloud_partition": "aws",
			"account":         map[string]any{"uid": "111122223333", "name": "acme-prod", "type": "AWS Account", "type_id": 10},
			"org":             map[string]any{"uid": "o-example"},
		},
		"resources": []any{map[string]any{
			"uid": "arn:aws:s3:::acme-data", "name": "acme-data", "type": "AWS::S3::Bucket", "cloud_partition": "aws",
		}},
		"evidences": []any{map[string]any{
			"actor": map[string]any{"user": map[string]any{
				"uid": deployARN, "name": "deploy-ci", "type_id": 1, "type": "User",
				"account":                  map[string]any{"uid": "111122223333"},
				"programmatic_credentials": []any{map[string]any{"uid": "AKIAEXAMPLE", "type": "Access Key"}},
			}},
			"src_endpoint": map[string]any{"ip": "203.0.113.77"},
		}},
	})
	floor := cloudDetection()
	for _, e := range []map[string]any{floor, ceiling} {
		if bad := ocsfschema.ValidateEvent(e); len(bad) != 0 {
			t.Errorf("OCSF %s: %v", pathStr(e, "metadata.version"), bad)
		}
	}
	floorRefs, floorRels := entityIDs(t, floor)
	ceilRefs, ceilRels := entityIDs(t, ceiling)
	if !slices.Equal(floorRefs, ceilRefs) || !slices.Equal(floorRels, ceilRels) {
		t.Errorf("floor names %v %v\nceiling names %v %v", floorRefs, floorRels, ceilRefs, ceilRels)
	}
	res := normalizeOne(t, ceiling)
	if got := scosOf(res, identity.TypeUserAccount)[0].Properties["credential_uid"]; got != "AKIAEXAMPLE" {
		t.Errorf("credential = %v; want the one in programmatic_credentials", got)
	}
	if got := scosOf(res, identity.TypeCloudAccount)[0].Properties["org_uid"]; got != "o-example" {
		t.Errorf("org_uid = %v; want cloud.org.uid", got)
	}
}

// TestDetectionCloudFallbacks (§4.15's rules on a finding): the role behind an
// assumed-role session is the principal, uid_alt stands in for a missing uid,
// a caller from another account brings its own account, the deprecated
// cloud.project_uid names the account when cloud.account does not, and a
// resource with no uid is identified under the finding's account and belongs
// to it.
func TestDetectionCloudFallbacks(t *testing.T) {
	ids := identity.NewResolver(testNS)
	role := "arn:aws:iam::999900001111:role/ops"
	p := versioned(2004, "1.1.0", map[string]any{
		"finding_info": map[string]any{"uid": "det-c2"},
		"cloud":        map[string]any{"provider": "AWS", "project_uid": "111122223333"},
		"resources": []any{map[string]any{
			"name": "queue-1", "type": "AWS::SQS::Queue", "region": "us-east-1",
			"group": map[string]any{"name": "rg"}, "namespace": "ns",
		}},
		"evidences": []any{
			map[string]any{"actor": map[string]any{
				"user": map[string]any{
					"uid": "AROAEXAMPLE:session-1", "name": "ops",
					"account": map[string]any{"uid": "999900001111", "name": "acme-ops", "type": "AWS Account"},
				},
				"session": map[string]any{"issuer": role, "credential_uid": "ASIAEXAMPLE"},
			}},
			map[string]any{"actor": map[string]any{"user": map[string]any{"uid_alt": "AIDAALT"}}},
			// A name alone is no identifier: no principal.
			map[string]any{"actor": map[string]any{"user": map[string]any{"name": "nameless"}}},
		},
	})
	res := normalizeOne(t, p)

	account := ids.CloudAccount("aws", "111122223333")
	home := ids.CloudAccount("aws", "999900001111")
	principal := ids.Account(identity.AccountIdentity{Type: "aws", UserID: role})
	alt := ids.Account(identity.AccountIdentity{Type: "aws", UserID: "AIDAALT"})
	if got := refsOf(res, identity.TypeCloudAccount); !slices.Contains(got, account) || !slices.Contains(got, home) || len(got) != 2 {
		t.Errorf("accounts = %v; want the finding's %s and the caller's %s", got, account, home)
	}
	if got := refsOf(res, identity.TypeUserAccount); !slices.Contains(got, principal) || !slices.Contains(got, alt) || len(got) != 2 {
		t.Errorf("principals = %v; want the role %s and the uid_alt one %s", got, principal, alt)
	}
	if !hasEdge(res, RelBelongsTo, principal, home) || hasEdge(res, RelBelongsTo, principal, account) {
		t.Error("the principal belongs to its own account, not the finding's")
	}
	for _, s := range scosOf(res, identity.TypeUserAccount) {
		if s.ID == principal && (s.Properties["is_service_account"] != true || s.Properties["credential_uid"] != "ASIAEXAMPLE") {
			t.Errorf("role properties = %v", s.Properties)
		}
	}
	for _, s := range scosOf(res, identity.TypeCloudAccount) {
		if s.ID == home && (s.Properties["name"] != "acme-ops" || s.Properties["account_type"] != "AWS Account" || s.Properties["org_uid"] != nil) {
			t.Errorf("caller account properties = %v", s.Properties)
		}
	}
	queues := scosOf(res, identity.TypeCloudResource)
	if len(queues) != 1 || queues[0].Properties["account_ref"] != string(account) || !hasEdge(res, RelBelongsTo, queues[0].ID, account) {
		t.Errorf("resources = %+v; want one, under the finding's account", queues)
	}
}

// TestDetectionNoProviderKeepsDirectoryLogin (adr/0356): a finding that names
// no cloud provider reads actor.user as a directory login, and mints nothing
// cloud-shaped even when it carries resources.
func TestDetectionNoProviderKeepsDirectoryLogin(t *testing.T) {
	p := detectionPayload()
	p["resources"] = []any{map[string]any{"uid": "res-1", "name": "thing"}}
	p["cloud"] = map[string]any{"account": map[string]any{"uid": "111122223333"}}
	res := normalizeOne(t, p)
	ids := identity.NewResolver(testNS)
	if want := ids.Account(identity.AccountIdentity{Type: identity.AccountWindowsDomain, UserID: "S-1-5-21-7-1101"}); !slices.Equal(accountsIn(res), []identity.STIXID{want}) {
		t.Errorf("accounts in object_refs = %v; want the domain account %s", accountsIn(res), want)
	}
	if n := len(scosOf(res, identity.TypeCloudAccount)) + len(scosOf(res, identity.TypeCloudResource)); n != 0 {
		t.Errorf("%d cloud objects minted with no provider", n)
	}
}

// TestScenario_ExposedBucket_AlertMeetsAPICalls (adr/0356): the scenario's
// alert names the bucket, the account and the principal by the ids its API
// calls mint for them.
func TestScenario_ExposedBucket_AlertMeetsAPICalls(t *testing.T) {
	files, err := filepath.Glob("../fixtures/exposed-bucket-via-access-key/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("fixtures: %v (%d files)", err, len(files))
	}
	reg := testRegistry()
	types := []string{identity.TypeCloudAccount, identity.TypeCloudResource, identity.TypeUserAccount}
	fromAPI := map[identity.STIXID]bool{}
	var alert *NormalizationResult
	for _, f := range files {
		raw, err := os.ReadFile(f) // #nosec G304 -- a fixture path from Glob
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Ocsf map[string]any `json:"ocsf"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		class := toInt(doc.Ocsf["class_uid"])
		if class != 6003 && class != 2004 {
			continue
		}
		res, err := reg.Normalize(makeEvent(doc.Ocsf))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if class == 2004 {
			if alert != nil {
				t.Fatalf("%s: a second alert; the scenario has one", f)
			}
			alert = &res
			continue
		}
		for _, typ := range types {
			for _, id := range refsOf(res, typ) {
				fromAPI[id] = true
			}
		}
	}
	if alert == nil {
		t.Fatal("the scenario has no alert")
	}
	ids := identity.NewResolver(testNS)
	bucket, _ := ids.CloudResource(identity.CloudResourceIdentity{Provider: "aws", UID: "arn:aws:s3:::acme-internal-exports"})
	want := map[string]identity.STIXID{
		identity.TypeCloudAccount:  ids.CloudAccount("aws", "111122223333"),
		identity.TypeCloudResource: bucket,
		identity.TypeUserAccount:   ids.Account(identity.AccountIdentity{Type: "aws", UserID: deployARN}),
	}
	for _, typ := range types {
		got := refsOf(*alert, typ)
		if !slices.Equal(got, []identity.STIXID{want[typ]}) {
			t.Errorf("the alert's %s = %v; want %s", typ, got, want[typ])
			continue
		}
		if !fromAPI[got[0]] {
			t.Errorf("the alert's %s %s is not one the API calls mint", typ, got[0])
		}
	}
}
