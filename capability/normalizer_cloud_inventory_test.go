package capability

import (
	"slices"
	"testing"

	"github.com/sd-strax/reckon/identity"
)

const bucketARN = "arn:aws:s3:::acme-internal-exports"

// inventoryPayload is a 1.7.0 configuration snapshot of one bucket, the shape
// the fixture uses.
func inventoryPayload() map[string]any {
	return map[string]any{
		"class_uid":     5023,
		"category_uid":  5,
		"activity_id":   2,
		"activity_name": "Collect",
		"time":          "2026-07-14T10:00:00Z",
		"metadata":      map[string]any{"version": "1.7.0", "product": map[string]any{"name": "fixture-cloud-config"}},
		"cloud": map[string]any{
			"provider": "AWS",
			"account":  map[string]any{"uid": "111122223333", "name": "acme-prod", "type_id": 10},
			"region":   "us-east-1",
		},
		"resources": []any{
			map[string]any{
				"uid": bucketARN, "name": "acme-internal-exports", "type": "AWS::S3::Bucket", "region": "us-east-1",
				"data":          map[string]any{"versioning": "Suspended", "policy": map[string]any{"Version": "2012-10-17"}},
				"labels":        []any{"env:prod"},
				"tags":          []any{map[string]any{"name": "env", "value": "prod"}},
				"criticality":   "High",
				"created_time":  "2025-03-02T12:00:00Z",
				"modified_time": "2026-07-14T09:28:00Z",
			},
		},
	}
}

// TestCloudInventory_Snapshot covers §4.18 on a bucket snapshot: the account
// and the resource as typed objects, the resource keyed by its uid exactly as
// the 6003 call on it keys it (no belongs-to: the uid carries its owner), and
// the configuration on the one ObservedData.
func TestCloudInventory_Snapshot(t *testing.T) {
	ids := identity.NewResolver(testNS)
	res := normalizeOne(t, inventoryPayload())

	accts := scosOf(res, identity.TypeCloudAccount)
	if len(accts) != 1 || accts[0].ID != ids.CloudAccount("aws", "111122223333") {
		t.Fatalf("accounts = %+v", accts)
	}
	if accts[0].Properties["name"] != "acme-prod" || accts[0].Properties["account_type"] != "AWS Account" {
		t.Errorf("account props = %v", accts[0].Properties)
	}
	rs := scosOf(res, identity.TypeCloudResource)
	if len(rs) != 1 {
		t.Fatalf("resources = %+v; want one", rs)
	}
	api := scosOf(normalizeOne(t, apiPayload()), identity.TypeCloudResource)
	if len(api) != 1 || rs[0].ID != api[0].ID {
		t.Errorf("snapshot resource %s; the 6003 call on the same uid minted %+v", rs[0].ID, api)
	}
	if len(res.Relationships) != 0 {
		t.Errorf("relationships = %+v; want none for a uid-identified resource", res.Relationships)
	}
	if n := len(scosOf(res, identity.TypeUserAccount)); n != 0 {
		t.Errorf("user-accounts = %d; want none without an owner", n)
	}

	od := res.ObservedData[0]
	if od.Provenance.Normalizer != "cloud_inventory" || od.Provenance.NormalizerVersion != 2 {
		t.Errorf("provenance = %+v", od.Provenance)
	}
	ext := od.Extensions
	for k, want := range map[string]any{
		"activity": "Collect", "activity_id": 2, "region": "us-east-1", "resource_count": 1,
		"resource_uid": bucketARN, "resource_name": "acme-internal-exports", "type": "AWS::S3::Bucket",
		"criticality": "High", "created_time": "2025-03-02T12:00:00Z", "modified_time": "2026-07-14T09:28:00Z",
	} {
		if ext[k] != want {
			t.Errorf("ext %s = %v; want %v", k, ext[k], want)
		}
	}
	cfg, _ := ext["configuration"].(map[string]any)
	if cfg["versioning"] != "Suspended" || cfg["policy"] == nil {
		t.Errorf("configuration = %v; want the resource's data as decoded", ext["configuration"])
	}
	if l, _ := ext["labels"].([]any); len(l) != 1 {
		t.Errorf("labels = %v", ext["labels"])
	}
	if tg, _ := ext["tags"].([]any); len(tg) != 1 {
		t.Errorf("tags = %v", ext["tags"])
	}
	for _, k := range []string{"owner_name", "version", "schema_version", "metadata_version"} {
		if _, ok := ext[k]; ok {
			t.Errorf("ext carries %s: %v", k, ext)
		}
	}
}

// TestCloudInventory_ByName: a resource with no uid is identified under the
// account and belongs-to it, as on a 6003 call; the project_uid stands in
// for an absent cloud.account.
func TestCloudInventory_ByName(t *testing.T) {
	ids := identity.NewResolver(testNS)
	p := inventoryPayload()
	p["cloud"] = map[string]any{"provider": "GCP", "project_uid": "acme-prod-1234"}
	p["resources"] = []any{map[string]any{"name": "exports", "type": "storage.googleapis.com/Bucket", "region": "us-central1"}}
	res := normalizeOne(t, p)
	acct := ids.CloudAccount("gcp", "acme-prod-1234")
	want, rule := ids.CloudResource(identity.CloudResourceIdentity{
		Provider: "gcp", AccountRef: acct, Type: "storage.googleapis.com/Bucket", Region: "us-central1", Name: "exports",
	})
	if rule != identity.CloudResourceByName {
		t.Fatalf("rule = %v; want by name", rule)
	}
	rs := scosOf(res, identity.TypeCloudResource)
	if len(rs) != 1 || rs[0].ID != want {
		t.Fatalf("resources = %+v; want %s", rs, want)
	}
	if !hasEdge(res, RelBelongsTo, want, acct) || len(res.Relationships) != 1 {
		t.Errorf("relationships = %+v; want the resource belongs-to the account", res.Relationships)
	}
}

// TestCloudInventory_Owner: an owner with an identifier is a user-account in
// the provider namespace, shaped as the 6003 principal (no login, the id as
// user_id, the name as display name); an owner known only by name mints
// nothing and is named on the observation.
func TestCloudInventory_Owner(t *testing.T) {
	ids := identity.NewResolver(testNS)
	ownerARN := "arn:aws:iam::111122223333:user/data-platform"
	p := inventoryPayload()
	r := p["resources"].([]any)[0].(map[string]any)
	r["owner"] = map[string]any{"uid": ownerARN, "name": "data-platform"}
	res := normalizeOne(t, p)
	users := scosOf(res, identity.TypeUserAccount)
	if len(users) != 1 || users[0].ID != ids.UserAccount("", ownerARN, "aws") {
		t.Fatalf("owner = %+v; want the user-account keyed on its uid", users)
	}
	for k, want := range map[string]any{"account_login": "", "user_id": ownerARN, "account_type": "aws", "display_name": "data-platform"} {
		if users[0].Properties[k] != want {
			t.Errorf("owner %s = %v; want %v", k, users[0].Properties[k], want)
		}
	}
	if api := scosOf(normalizeOne(t, func() map[string]any {
		a := apiPayload()
		a["actor"] = map[string]any{"user": map[string]any{"uid": ownerARN, "name": "data-platform"}}
		return a
	}()), identity.TypeUserAccount); len(api) != 1 || api[0].ID != users[0].ID {
		t.Errorf("owner %s; the same principal on a 6003 call minted %+v", users[0].ID, api)
	}

	r["owner"] = map[string]any{"uid_alt": "AIDAEXAMPLEALT", "name": "data-platform"}
	if u := scosOf(normalizeOne(t, p), identity.TypeUserAccount); len(u) != 1 || u[0].Properties["user_id"] != "AIDAEXAMPLEALT" {
		t.Errorf("owner by uid_alt = %+v", u)
	}

	r["owner"] = map[string]any{"name": "data-platform"}
	res = normalizeOne(t, p)
	if n := len(scosOf(res, identity.TypeUserAccount)); n != 0 {
		t.Errorf("name-only owner minted %d user-accounts; want none", n)
	}
	if got := res.ObservedData[0].Extensions["owner_name"]; got != "data-platform" {
		t.Errorf("owner_name = %v; want the name", got)
	}
}

// TestCloudInventory_NoProvider: with no cloud.provider there is no namespace
// for a cloud identity; the snapshot is one ObservedData with its extensions.
func TestCloudInventory_NoProvider(t *testing.T) {
	p := inventoryPayload()
	delete(p, "cloud")
	p["resources"].([]any)[0].(map[string]any)["owner"] = map[string]any{"uid": "u-1", "name": "someone"}
	res := normalizeOne(t, p)
	if len(res.SCOs) != 0 || len(res.Relationships) != 0 {
		t.Errorf("SCOs %+v, relationships %+v; want none without a provider", res.SCOs, res.Relationships)
	}
	if ext := res.ObservedData[0].Extensions; ext["resource_uid"] != bucketARN || ext["configuration"] == nil {
		t.Errorf("ext = %v; want the snapshot's fields", ext)
	}
}

// TestCloudInventory_SchemaVersionRange (§4.16): the class first exists at
// 1.4.0, where the resource has no created_time/modified_time; a 1.4.0-shaped
// snapshot and a 1.7.0-shaped one mint the same ids, and only the latter
// carries the times.
func TestCloudInventory_SchemaVersionRange(t *testing.T) {
	floor := inventoryPayload()
	floor["metadata"] = map[string]any{"version": "1.4.0"}
	r := floor["resources"].([]any)[0].(map[string]any)
	delete(r, "created_time")
	delete(r, "modified_time")
	ceiling := inventoryPayload()
	ceiling["resources"].([]any)[0].(map[string]any)["uid_alt"] = "acme-internal-exports-alt"

	a, b := normalizeOne(t, floor), normalizeOne(t, ceiling)
	collect := func(res NormalizationResult) []identity.STIXID {
		var out []identity.STIXID
		for _, s := range res.SCOs {
			out = append(out, s.ID)
		}
		return out
	}
	if fa, ce := collect(a), collect(b); len(fa) != 2 || !slices.Equal(fa, ce) {
		t.Errorf("floor %v, ceiling %v; want the same account and resource ids", fa, ce)
	}
	if _, ok := a.ObservedData[0].Extensions["modified_time"]; ok {
		t.Errorf("1.4.0 snapshot ext carries modified_time: %v", a.ObservedData[0].Extensions)
	}
	if b.ObservedData[0].Extensions["modified_time"] != "2026-07-14T09:28:00Z" {
		t.Errorf("1.7.0 snapshot ext = %v; want modified_time", b.ObservedData[0].Extensions)
	}
	for i, res := range []NormalizationResult{a, b} {
		for _, k := range []string{"version", "schema_version", "metadata_version"} {
			if _, ok := res.ObservedData[0].Extensions[k]; ok {
				t.Errorf("snapshot %d ext carries %s", i, k)
			}
		}
	}
}

// TestCloudInventory_Registered: 5023 has a dedicated normalizer.
func TestCloudInventory_Registered(t *testing.T) {
	if !slices.Contains(testRegistry().HandledClasses(), 5023) {
		t.Fatal("5023 not in HandledClasses")
	}
}
