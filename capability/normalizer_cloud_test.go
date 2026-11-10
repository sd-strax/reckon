package capability

import (
	"slices"
	"testing"

	"github.com/sd-strax/reckon/identity"
)

// apiPayload is an IAM-user call on one bucket, the shape the fixture uses.
func apiPayload() map[string]any {
	return map[string]any{
		"class_uid":     6003,
		"time":          "2026-07-14T09:28:00Z",
		"activity_id":   3,
		"activity_name": "Update",
		"actor": map[string]any{
			"user": map[string]any{
				"uid":            "arn:aws:iam::111122223333:user/deploy-ci",
				"name":           "deploy-ci",
				"type_id":        1,
				"account":        map[string]any{"uid": "111122223333", "name": "acme-prod", "type_id": 10},
				"credential_uid": "AKIAIOSFODNN7EXAMPLE",
			},
		},
		"api": map[string]any{
			"operation": "PutBucketPolicy",
			"service":   map[string]any{"name": "s3.amazonaws.com"},
			"request":   map[string]any{"uid": "req-1"},
		},
		"cloud": map[string]any{
			"provider": "AWS",
			"account":  map[string]any{"uid": "111122223333", "name": "acme-prod", "type_id": 10},
			"region":   "us-east-1",
		},
		"resources": []any{
			map[string]any{"uid": "arn:aws:s3:::acme-internal-exports", "name": "acme-internal-exports", "type": "AWS::S3::Bucket"},
		},
		"src_endpoint": map[string]any{"ip": "203.0.113.77"},
		"http_request": map[string]any{"user_agent": "aws-cli/2.15.0"},
		"status":       "Success",
		"status_id":    1,
	}
}

func normalizeOne(t *testing.T, p map[string]any) NormalizationResult {
	t.Helper()
	res, err := testRegistry().Normalize(makeEvent(p))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(res.ObservedData) != 1 {
		t.Fatalf("observed data = %d; want 1", len(res.ObservedData))
	}
	return res
}

func scosOf(res NormalizationResult, typ string) []SCO {
	var out []SCO
	for _, s := range res.SCOs {
		if s.Type == typ {
			out = append(out, s)
		}
	}
	return out
}

func hasEdge(res NormalizationResult, typ string, src, tgt identity.STIXID) bool {
	for _, r := range res.Relationships {
		if r.Type == typ && r.SourceRef == src && r.TargetRef == tgt {
			return true
		}
	}
	return false
}

const deployARN = "arn:aws:iam::111122223333:user/deploy-ci"

// TestAPIActivity_IAMUser covers §4.15 end to end on an IAM-user call: the
// principal keyed on its ARN in the provider namespace with the name as its
// display name, the account, the resource by its uid (no belongs-to: the uid
// carries its own owner), the source address, the principal belongs-to its own
// account, and the call on one ObservedData.
func TestAPIActivity_IAMUser(t *testing.T) {
	ids := identity.NewResolver(testNS)
	res := normalizeOne(t, apiPayload())

	users := scosOf(res, identity.TypeUserAccount)
	if len(users) != 1 {
		t.Fatalf("user-accounts = %d; want 1", len(users))
	}
	u := users[0]
	if want := ids.UserAccount("", deployARN, "aws"); u.ID != want {
		t.Errorf("principal id = %s; want %s", u.ID, want)
	}
	for k, want := range map[string]any{
		"account_login": "", "user_id": deployARN, "account_type": "aws", "user_type": "User",
		"is_service_account": false, "credential_uid": "AKIAIOSFODNN7EXAMPLE",
		"cloud_account_uid": "111122223333", "display_name": "deploy-ci",
	} {
		if u.Properties[k] != want {
			t.Errorf("principal %s = %v; want %v", k, u.Properties[k], want)
		}
	}

	accts := scosOf(res, identity.TypeCloudAccount)
	if len(accts) != 1 || accts[0].ID != ids.CloudAccount("aws", "111122223333") {
		t.Fatalf("accounts = %+v", accts)
	}
	a := accts[0]
	if a.Properties["account_type"] != "AWS Account" || a.Properties["name"] != "acme-prod" || a.Properties["provider"] != "aws" {
		t.Errorf("account props = %v", a.Properties)
	}

	rs := scosOf(res, identity.TypeCloudResource)
	if len(rs) != 1 {
		t.Fatalf("resources = %d; want 1", len(rs))
	}
	wantRes, _ := ids.CloudResource(identity.CloudResourceIdentity{Provider: "aws", UID: "arn:aws:s3:::acme-internal-exports"})
	if rs[0].ID != wantRes || rs[0].Properties["type"] != "AWS::S3::Bucket" {
		t.Errorf("resource = %+v", rs[0])
	}
	if _, ok := rs[0].Properties["account_ref"]; ok {
		t.Errorf("uid resource carries account_ref: %v", rs[0].Properties)
	}
	if len(scosOf(res, identity.TypeIPv4Addr)) != 1 {
		t.Error("no source address")
	}

	if !hasEdge(res, RelBelongsTo, u.ID, a.ID) || hasEdge(res, RelBelongsTo, rs[0].ID, a.ID) || len(res.Relationships) != 1 {
		t.Errorf("relationships = %+v; want only the principal belongs-to its account", res.Relationships)
	}

	od := res.ObservedData[0]
	if od.Provenance.Normalizer != "api_activity" || od.Provenance.NormalizerVersion != 1 || od.Provenance.DerivationMode != DerivationDirect {
		t.Errorf("provenance = %+v", od.Provenance)
	}
	if len(od.ObjectRefs) != 4 {
		t.Errorf("object_refs = %v; want principal, account, resource, address", od.ObjectRefs)
	}
	for k, want := range map[string]any{
		"operation": "PutBucketPolicy", "service": "s3.amazonaws.com", "activity_id": 3, "activity": "Update",
		"status_id": 1, "status": "Success", "request_uid": "req-1", "region": "us-east-1", "principal_name": "deploy-ci",
		"credential_uid": "AKIAIOSFODNN7EXAMPLE", "user_agent": "aws-cli/2.15.0", "resource_count": 1,
	} {
		if od.Extensions[k] != want {
			t.Errorf("ext %s = %v; want %v", k, od.Extensions[k], want)
		}
	}
	for _, k := range []string{"error", "error_message", "response_code", "session", "app_name"} {
		if _, ok := od.Extensions[k]; ok {
			t.Errorf("ext carries absent key %s", k)
		}
	}
}

// TestAPIActivity_AssumedRole: on AWS the session issuer is the durable
// principal, so two sessions of one role are one user-account; the session
// rides the ObservedData.
func TestAPIActivity_AssumedRole(t *testing.T) {
	role := func(session string) map[string]any {
		p := apiPayload()
		p["actor"] = map[string]any{
			"user": map[string]any{
				"uid": "arn:aws:sts::111122223333:assumed-role/Deploy/" + session, "name": "Deploy", "type_id": 1,
			},
			"session": map[string]any{"issuer": "arn:aws:iam::111122223333:role/Deploy", "uid": session, "is_mfa": false},
		}
		return p
	}
	r1, r2 := normalizeOne(t, role("i-0abc")), normalizeOne(t, role("i-0def"))
	u1, u2 := scosOf(r1, identity.TypeUserAccount), scosOf(r2, identity.TypeUserAccount)
	if len(u1) != 1 || len(u2) != 1 || u1[0].ID != u2[0].ID {
		t.Fatalf("two sessions of one role: %v vs %v; want one principal", u1, u2)
	}
	want := identity.NewResolver(testNS).UserAccount("", "arn:aws:iam::111122223333:role/Deploy", "aws")
	if u1[0].ID != want || u1[0].Properties["is_service_account"] != true || u1[0].Properties["account_login"] != "" ||
		u1[0].Properties["user_id"] != "arn:aws:iam::111122223333:role/Deploy" || u1[0].Properties["display_name"] != "Deploy" {
		t.Errorf("role principal = %+v", u1[0])
	}
	// No actor.user.account: no principal edge, by inference or otherwise.
	if len(r1.Relationships) != 0 {
		t.Errorf("relationships = %+v; want none", r1.Relationships)
	}
	s, _ := r1.ObservedData[0].Extensions["session"].(map[string]any)
	if s["uid"] != "i-0abc" || s["issuer"] != "arn:aws:iam::111122223333:role/Deploy" || s["is_mfa"] != false {
		t.Errorf("session ext = %v", r1.ObservedData[0].Extensions["session"])
	}
}

// TestAPIActivity_AzureIssuerIgnored: outside the AWS row the session issuer
// is not the principal (on Azure it names the token service), so the user's
// uid is, and two users under one issuer are two principals.
func TestAPIActivity_AzureIssuerIgnored(t *testing.T) {
	azure := func(guid string) map[string]any {
		p := apiPayload()
		p["cloud"] = map[string]any{"provider": "Azure", "account": map[string]any{"uid": "0f8fad5b-d9cb-469f-a165-70867728950e", "type_id": 13}}
		p["actor"] = map[string]any{
			"user":    map[string]any{"uid": guid, "name": "deploy-ci"},
			"session": map[string]any{"issuer": "https://sts.windows.net/5f1e2d3c-0000-4000-8000-00000000aaaa/"},
		}
		return p
	}
	const g1, g2 = "6a4b2c1d-1111-4111-8111-111111111111", "6a4b2c1d-2222-4222-8222-222222222222"
	u1 := scosOf(normalizeOne(t, azure(g1)), identity.TypeUserAccount)
	u2 := scosOf(normalizeOne(t, azure(g2)), identity.TypeUserAccount)
	if len(u1) != 1 || len(u2) != 1 {
		t.Fatalf("principals = %v, %v", u1, u2)
	}
	if u1[0].Properties["user_id"] != g1 || u1[0].ID != identity.NewResolver(testNS).UserAccount("", g1, "azure") {
		t.Errorf("azure principal = %+v; want user_id %s", u1[0], g1)
	}
	if u1[0].ID == u2[0].ID {
		t.Error("two Azure users under one issuer merged")
	}
	if u1[0].Properties["is_service_account"] != false {
		t.Error("an ignored issuer marked the principal a service account")
	}
}

// TestAPIActivity_Root: the root principal is shaped like any other.
func TestAPIActivity_Root(t *testing.T) {
	p := apiPayload()
	p["actor"] = map[string]any{"user": map[string]any{"uid": "arn:aws:iam::111122223333:root", "name": "Root"}}
	u := scosOf(normalizeOne(t, p), identity.TypeUserAccount)
	if len(u) != 1 || u[0].ID != identity.NewResolver(testNS).UserAccount("", "arn:aws:iam::111122223333:root", "aws") ||
		u[0].Properties["account_login"] != "" || u[0].Properties["user_id"] != "arn:aws:iam::111122223333:root" || u[0].Properties["display_name"] != "Root" {
		t.Errorf("root principal = %+v", u)
	}
}

// TestAPIActivity_NoDurableID: a call naming only an app, or a principal by
// name alone, mints no user-account; the name and app ride the ObservedData,
// so two accounts' same-named users can never merge.
func TestAPIActivity_NoDurableID(t *testing.T) {
	p := apiPayload()
	p["actor"] = map[string]any{"app_name": "backup.example.internal"}
	res := normalizeOne(t, p)
	if u := scosOf(res, identity.TypeUserAccount); len(u) != 0 {
		t.Errorf("app-only call minted a principal: %+v", u)
	}
	if res.ObservedData[0].Extensions["app_name"] != "backup.example.internal" {
		t.Errorf("app_name ext = %v", res.ObservedData[0].Extensions["app_name"])
	}

	nameOnly := func(acct string) NormalizationResult {
		p := apiPayload()
		p["actor"] = map[string]any{"user": map[string]any{"name": "deploy-ci"}}
		p["cloud"].(map[string]any)["account"] = map[string]any{"uid": acct}
		return normalizeOne(t, p)
	}
	a, b := nameOnly("111122223333"), nameOnly("444455556666")
	for _, r := range []NormalizationResult{a, b} {
		if u := scosOf(r, identity.TypeUserAccount); len(u) != 0 {
			t.Errorf("name-only call minted a principal: %+v", u)
		}
		if r.ObservedData[0].Extensions["principal_name"] != "deploy-ci" {
			t.Errorf("principal_name ext = %v", r.ObservedData[0].Extensions["principal_name"])
		}
	}
}

// TestAPIActivity_Resources: a resource by uid (no belongs-to), one by name
// under the account (belongs-to it), one with neither (not minted), a repeat
// (once); without an account the uid resource is still minted and the named
// one is not.
func TestAPIActivity_Resources(t *testing.T) {
	ids := identity.NewResolver(testNS)
	p := apiPayload()
	p["resources"] = []any{
		map[string]any{"uid": "arn:aws:s3:::acme-release-artifacts"},
		map[string]any{"name": "Acme-Logs", "type": "AWS::S3::Bucket"},
		map[string]any{"type": "AWS::S3::Bucket"},
		"not an object",
		map[string]any{"name": "Acme-Logs", "type": "aws::s3::bucket"},
	}
	res := normalizeOne(t, p)
	acct := ids.CloudAccount("aws", "111122223333")
	byUID, _ := ids.CloudResource(identity.CloudResourceIdentity{Provider: "aws", UID: "arn:aws:s3:::acme-release-artifacts"})
	byName, _ := ids.CloudResource(identity.CloudResourceIdentity{AccountRef: acct, Type: "AWS::S3::Bucket", Name: "Acme-Logs"})
	rs := scosOf(res, identity.TypeCloudResource)
	if len(rs) != 2 || rs[0].ID != byUID || rs[1].ID != byName {
		t.Fatalf("resources = %+v; want the uid one and the named one, once each", rs)
	}
	if hasEdge(res, RelBelongsTo, byUID, acct) {
		t.Error("a uid-identified resource belongs-to the event's account")
	}
	if !hasEdge(res, RelBelongsTo, byName, acct) || rs[1].Properties["account_ref"] != string(acct) {
		t.Errorf("named resource: edge %v, props %v; want belongs-to and account_ref", hasEdge(res, RelBelongsTo, byName, acct), rs[1].Properties)
	}
	n := 0
	for _, r := range res.Relationships {
		if r.SourceRef == byName {
			n++
		}
	}
	if n != 1 {
		t.Errorf("named resource has %d belongs-to edges; want 1 despite the repeat", n)
	}
	if res.ObservedData[0].Extensions["resource_count"] != 5 {
		t.Errorf("resource_count = %v; want the payload's 5", res.ObservedData[0].Extensions["resource_count"])
	}

	// No account anywhere: the uid resource stands alone, the named one is not minted.
	p["cloud"] = map[string]any{"provider": "AWS"}
	delete(p["actor"].(map[string]any)["user"].(map[string]any), "account")
	res = normalizeOne(t, p)
	if n := len(scosOf(res, identity.TypeCloudAccount)); n != 0 {
		t.Errorf("accounts = %d; want none", n)
	}
	if rs := scosOf(res, identity.TypeCloudResource); len(rs) != 1 || rs[0].Properties["uid"] != "arn:aws:s3:::acme-release-artifacts" {
		t.Errorf("resources without an account = %+v; want only the uid one", rs)
	}
	if len(res.Relationships) != 0 {
		t.Errorf("relationships = %+v; want none without an account", res.Relationships)
	}
}

// TestAPIActivity_ResourceGroups: two same-named VMs in one region and two
// resource groups, with no uid, are two resources under the account.
func TestAPIActivity_ResourceGroups(t *testing.T) {
	ids := identity.NewResolver(testNS)
	p := apiPayload()
	p["cloud"] = map[string]any{"provider": "Azure", "account": map[string]any{"uid": "0f8fad5b-d9cb-469f-a165-70867728950e", "type_id": 13}}
	p["resources"] = []any{
		map[string]any{"name": "api", "type": "Microsoft.Compute/virtualMachines", "region": "eastus", "group": map[string]any{"name": "rg-a"}},
		map[string]any{"name": "api", "type": "Microsoft.Compute/virtualMachines", "region": "eastus", "group": map[string]any{"uid": "rg-b"}},
		map[string]any{"name": "api", "type": "Microsoft.Compute/virtualMachines", "region": "eastus", "namespace": float64(12)},
	}
	res := normalizeOne(t, p)
	acct := ids.CloudAccount("azure", "0f8fad5b-d9cb-469f-a165-70867728950e")
	rs := scosOf(res, identity.TypeCloudResource)
	if len(rs) != 3 || rs[0].ID == rs[1].ID {
		t.Fatalf("resources = %+v; want three", rs)
	}
	numeric, _ := ids.CloudResource(identity.CloudResourceIdentity{AccountRef: acct, Type: "Microsoft.Compute/virtualMachines", Region: "eastus", Namespace: "12", Name: "api"})
	if rs[2].ID != numeric || rs[2].Properties["namespace"] != "12" {
		t.Errorf("numeric namespace resource = %+v; want namespace \"12\"", rs[2])
	}
	for i, g := range []string{"rg-a", "rg-b"} {
		want, _ := ids.CloudResource(identity.CloudResourceIdentity{AccountRef: acct, Type: "Microsoft.Compute/virtualMachines", Region: "eastus", Group: g, Name: "api"})
		if rs[i].ID != want || rs[i].Properties["group"] != g || rs[i].Properties["account_ref"] != string(acct) || !hasEdge(res, RelBelongsTo, rs[i].ID, acct) {
			t.Errorf("resource %d = %+v; want group %s, account_ref and belongs-to", i, rs[i], g)
		}
	}
}

// TestAPIActivity_NoProvider: a 6003 event with no cloud.provider (a
// non-cloud application API) mints no cloud identity, whatever ids it
// carries; the call is still one ObservedData with its fields and source.
func TestAPIActivity_NoProvider(t *testing.T) {
	appCall := func() map[string]any {
		p := apiPayload()
		delete(p, "cloud")
		return p
	}
	a, b := normalizeOne(t, appCall()), normalizeOne(t, appCall())
	for _, typ := range []string{identity.TypeUserAccount, identity.TypeCloudAccount, identity.TypeCloudResource} {
		if n := len(scosOf(a, typ)); n != 0 {
			t.Errorf("%s minted without a provider: %d", typ, n)
		}
	}
	if len(a.Relationships) != 0 {
		t.Errorf("relationships = %+v; want none", a.Relationships)
	}
	ext := a.ObservedData[0].Extensions
	if ext["operation"] != "PutBucketPolicy" || ext["principal_name"] != "deploy-ci" {
		t.Errorf("ext = %v; want the operation and principal name", ext)
	}
	if len(scosOf(a, identity.TypeIPv4Addr)) != 1 || len(a.SCOs) != 1 {
		t.Errorf("SCOs = %+v; want only the source address", a.SCOs)
	}
	// Coincident uids in two events share nothing cloud-shaped; the address
	// is the only SCO either holds.
	for _, s := range b.SCOs {
		if s.Type != identity.TypeIPv4Addr {
			t.Errorf("second event minted %s", s.ID)
		}
	}
}

// TestAPIActivity_CallerAccount: the principal belongs-to the account its own
// actor.user.account names: the event's account when they agree, a second
// account on a cross-account call, and none when the payload does not say.
func TestAPIActivity_CallerAccount(t *testing.T) {
	ids := identity.NewResolver(testNS)
	home, target := ids.CloudAccount("aws", "111122223333"), ids.CloudAccount("aws", "444455556666")
	principal := ids.UserAccount("", deployARN, "aws")

	res := normalizeOne(t, apiPayload())
	if n := len(scosOf(res, identity.TypeCloudAccount)); n != 1 || !hasEdge(res, RelBelongsTo, principal, home) {
		t.Errorf("same account: %d accounts, edge %v; want one and the edge", n, hasEdge(res, RelBelongsTo, principal, home))
	}

	p := apiPayload()
	p["cloud"].(map[string]any)["account"] = map[string]any{"uid": "444455556666", "name": "acme-shared"}
	p["cloud"].(map[string]any)["org"] = map[string]any{"uid": "o-exampleorg1"}
	res = normalizeOne(t, p)
	accts := map[identity.STIXID]SCO{}
	for _, a := range scosOf(res, identity.TypeCloudAccount) {
		accts[a.ID] = a
	}
	if len(accts) != 2 || accts[home].Properties["name"] != "acme-prod" || accts[target].Properties["name"] != "acme-shared" {
		t.Errorf("cross-account accounts = %+v; want the target and the caller's", accts)
	}
	if accts[target].Properties["org_uid"] != "o-exampleorg1" {
		t.Errorf("event account org_uid = %v; want the event's org", accts[target].Properties["org_uid"])
	}
	if _, ok := accts[home].Properties["org_uid"]; ok {
		t.Errorf("caller's account carries the event's org: %v", accts[home].Properties)
	}
	if !hasEdge(res, RelBelongsTo, principal, home) || hasEdge(res, RelBelongsTo, principal, target) {
		t.Errorf("relationships = %+v; want the principal belongs-to its own account only", res.Relationships)
	}
	if len(scosOf(res, identity.TypeCloudResource)) != 1 {
		t.Error("cross-account call lost its resource")
	}

	p = apiPayload()
	delete(p["actor"].(map[string]any)["user"].(map[string]any), "account")
	if res := normalizeOne(t, p); len(res.Relationships) != 0 {
		t.Errorf("no caller account: relationships = %+v; want none", res.Relationships)
	}
}

// TestAPIActivity_NumericIDs: an account or resource uid given as a JSON
// number is read, formatted without an exponent.
func TestAPIActivity_NumericIDs(t *testing.T) {
	ids := identity.NewResolver(testNS)
	p := apiPayload()
	p["cloud"].(map[string]any)["account"] = map[string]any{"uid": float64(111122223333)}
	p["actor"].(map[string]any)["user"].(map[string]any)["account"] = map[string]any{"uid": float64(111122223333)}
	p["resources"] = []any{map[string]any{"uid": float64(987654321012)}}
	res := normalizeOne(t, p)
	accts := scosOf(res, identity.TypeCloudAccount)
	if len(accts) != 1 || accts[0].ID != ids.CloudAccount("aws", "111122223333") || accts[0].Properties["account_uid"] != "111122223333" {
		t.Errorf("numeric account = %+v", accts)
	}
	if rs := scosOf(res, identity.TypeCloudResource); len(rs) != 1 || rs[0].Properties["uid"] != "987654321012" {
		t.Errorf("numeric resource = %+v", rs)
	}
}

// TestAPIActivity_Failure: a denied call carries the error, the response code
// and the status caption when the payload gives only the id.
func TestAPIActivity_Failure(t *testing.T) {
	p := apiPayload()
	delete(p, "status")
	delete(p, "activity_name")
	p["status_id"] = 2
	p["activity_id"] = 2
	p["api"].(map[string]any)["response"] = map[string]any{"code": float64(403), "error": "AccessDenied", "message": "User deploy-ci is not authorized to perform iam:ListUsers."}
	ext := normalizeOne(t, p).ObservedData[0].Extensions
	for k, want := range map[string]any{
		"status": "Failure", "status_id": 2, "error": "AccessDenied", "activity": "Read", "response_code": 403,
		"error_message": "User deploy-ci is not authorized to perform iam:ListUsers.",
	} {
		if ext[k] != want {
			t.Errorf("ext %s = %v; want %v", k, ext[k], want)
		}
	}
}

// TestAPIActivity_Fallbacks: the deprecated and alternate fields are read when
// the preferred ones are absent, and the 1.7.0 credential list is preferred
// when present.
func TestAPIActivity_Fallbacks(t *testing.T) {
	ids := identity.NewResolver(testNS)
	p := apiPayload()
	p["cloud"] = map[string]any{"provider": "Google Cloud", "project_uid": "acme-prod-1234"}
	p["actor"] = map[string]any{
		"user": map[string]any{
			"uid_alt":                  "1234567890",
			"name":                     "Deployer@acme-prod-1234.iam.gserviceaccount.com",
			"type_id":                  4,
			"programmatic_credentials": []any{map[string]any{"uid": "key-new"}},
			"credential_uid":           "key-old",
		},
		"session": map[string]any{"credential_uid": "key-session"},
	}
	res := normalizeOne(t, p)
	accts := scosOf(res, identity.TypeCloudAccount)
	if len(accts) != 1 || accts[0].ID != ids.CloudAccount("gcp", "acme-prod-1234") {
		t.Errorf("project_uid fallback: %+v", accts)
	}
	u := scosOf(res, identity.TypeUserAccount)
	if len(u) != 1 || u[0].ID != ids.UserAccount("", "1234567890", "gcp") {
		t.Fatalf("uid_alt principal = %+v", u)
	}
	if u[0].Properties["credential_uid"] != "key-new" || u[0].Properties["is_service_account"] != true || u[0].Properties["user_type"] != "Service" {
		t.Errorf("principal props = %v", u[0].Properties)
	}

	// Without the list, the user's key beats the session's; without both, the session's.
	actor := p["actor"].(map[string]any)["user"].(map[string]any)
	delete(actor, "programmatic_credentials")
	if got := credentialUID(p); got != "key-old" {
		t.Errorf("credential = %q; want key-old", got)
	}
	delete(actor, "credential_uid")
	if got := credentialUID(p); got != "key-session" {
		t.Errorf("credential = %q; want key-session", got)
	}

	// cloud.account beats the caller's account, which beats project_uid.
	p = apiPayload()
	p["cloud"] = map[string]any{"provider": "AWS", "project_uid": "ignored"}
	accts = scosOf(normalizeOne(t, p), identity.TypeCloudAccount)
	if len(accts) != 1 || accts[0].ID != ids.CloudAccount("aws", "111122223333") {
		t.Errorf("actor account precedence: %+v", accts)
	}
	p["cloud"] = map[string]any{"provider": "AWS", "project_uid": "ignored", "account": map[string]any{"uid": "444455556666"}}
	accts = scosOf(normalizeOne(t, p), identity.TypeCloudAccount)
	if len(accts) != 2 || accts[0].ID != ids.CloudAccount("aws", "444455556666") {
		t.Errorf("cloud.account precedence: %+v; want the event's account first, then the caller's", accts)
	}
}

// TestAPIActivity_NumericPrecision: a numeric id is read only when float64
// holds it exactly; a 21-digit principal id (already rounded by decoding) or a
// fraction mints nothing, a 12-digit account id still mints.
func TestAPIActivity_NumericPrecision(t *testing.T) {
	p := apiPayload()
	p["actor"] = map[string]any{"user": map[string]any{"uid": float64(123456789012345678901), "name": "deploy-ci"}}
	p["cloud"].(map[string]any)["account"] = map[string]any{"uid": float64(111122223333)}
	res := normalizeOne(t, p)
	if u := scosOf(res, identity.TypeUserAccount); len(u) != 0 {
		t.Errorf("21-digit numeric uid minted a principal: %+v", u)
	}
	if a := scosOf(res, identity.TypeCloudAccount); len(a) != 1 || a[0].Properties["account_uid"] != "111122223333" {
		t.Errorf("12-digit numeric account = %+v", a)
	}
	for _, v := range []any{1.5, float64(1 << 53), -float64(1 << 53)} {
		if got := scalarString(v); got != "" {
			t.Errorf("scalarString(%v) = %q; want empty", v, got)
		}
	}
	if got := scalarString(float64(1<<53 - 1)); got != "9007199254740991" {
		t.Errorf("scalarString(2^53-1) = %q", got)
	}
}

// TestAPIActivity_UIDBeatsUIDAlt: outside the AWS row the user's uid is the
// durable id when both it and the alternate uid are present.
func TestAPIActivity_UIDBeatsUIDAlt(t *testing.T) {
	p := apiPayload()
	p["cloud"] = map[string]any{"provider": "GCP", "account": map[string]any{"uid": "acme-prod-1234"}}
	p["actor"] = map[string]any{"user": map[string]any{
		"uid": "deployer@acme-prod-1234.iam.gserviceaccount.com", "uid_alt": "1234567890",
	}}
	u := scosOf(normalizeOne(t, p), identity.TypeUserAccount)
	if len(u) != 1 || u[0].Properties["user_id"] != "deployer@acme-prod-1234.iam.gserviceaccount.com" {
		t.Errorf("principal = %+v; want user_id the uid", u)
	}
}

func TestProviderSlug(t *testing.T) {
	for in, want := range map[string]string{
		"AWS": "aws", "Azure": "azure", "Microsoft Azure": "azure", "GCP": "gcp", "Google Cloud": "gcp",
		" Oracle Cloud ": "oracle cloud", "": "",
	} {
		if got := providerSlug(in); got != want {
			t.Errorf("providerSlug(%q) = %q; want %q", in, got, want)
		}
	}
}

// TestAPIActivity_RegisteredAndStable: 6003 has a dedicated normalizer, and
// the same event normalizes to the same ids every time.
func TestAPIActivity_RegisteredAndStable(t *testing.T) {
	if !slices.Contains(testRegistry().HandledClasses(), 6003) {
		t.Fatal("6003 not in HandledClasses")
	}
	evt := makeEvent(apiPayload())
	a, _ := testRegistry().Normalize(evt)
	b, _ := testRegistry().Normalize(evt)
	collect := func(r NormalizationResult) []string {
		var out []string
		for _, s := range r.SCOs {
			out = append(out, string(s.ID))
		}
		for _, rel := range r.Relationships {
			out = append(out, string(rel.ID))
		}
		return append(out, string(r.ObservedData[0].ID))
	}
	if !slices.Equal(collect(a), collect(b)) {
		t.Errorf("ids differ across runs:\n%v\n%v", collect(a), collect(b))
	}
}
