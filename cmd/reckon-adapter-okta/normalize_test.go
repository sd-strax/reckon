package main

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability"
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// mapAt navigates nested map[string]any, failing the test on a miss.
func mapAt(t *testing.T, m map[string]any, keys ...string) map[string]any {
	t.Helper()
	cur := m
	for _, k := range keys {
		next, ok := cur[k].(map[string]any)
		if !ok {
			t.Fatalf("expected map at %v, missing %q in %v", keys, k, cur)
		}
		cur = next
	}
	return cur
}

func TestNormalizeAuthEvent(t *testing.T) {
	// A genuine authentication event → OCSF 3002 with the engine's paths.
	in := `{"items":[{"eventType":"user.session.start","published":"2026-08-04T10:00:00Z",` +
		`"actor":{"id":"00uABC","alternateId":"alice@acme.com","type":"User"},` +
		`"client":{"ipAddress":"1.2.3.4"},"outcome":{"result":"SUCCESS"}}]}`
	evs, err := normalize("get_logs", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].ClassUID != 3002 {
		t.Fatalf("want one class-3002 event, got %+v", evs)
	}
	// The event's own time is OCSF's epoch milliseconds, with the same
	// instant as an RFC 3339 string beside it (adr/0356).
	if evs[0].Raw["time"] != int64(1785837600000) || evs[0].Raw["time_dt"] != "2026-08-04T10:00:00Z" {
		t.Errorf("time = %v, time_dt = %v; want 1785837600000 and 2026-08-04T10:00:00Z", evs[0].Raw["time"], evs[0].Raw["time_dt"])
	}
	if got := mapAt(t, evs[0].Raw, "actor", "user")["name"]; got != "alice@acme.com" {
		t.Errorf("actor.user.name = %v", got)
	}
	if got := mapAt(t, evs[0].Raw, "src_endpoint")["ip"]; got != "1.2.3.4" {
		t.Errorf("src_endpoint.ip = %v", got)
	}
	if got := evs[0].Raw["status"]; got != "SUCCESS" {
		t.Errorf("status = %v", got)
	}
	if _, has := mapAt(t, evs[0].Raw, "src_endpoint")["location"]; has {
		t.Errorf("a client with no geographicalContext carried a location: %v", evs[0].Raw["src_endpoint"])
	}
	// The product marks the record as an identity provider's (03 §7.4).
	if got := mapAt(t, evs[0].Raw, "metadata", "product")["name"]; got != "Okta System Log" {
		t.Errorf("metadata.product.name = %v", got)
	}
	assertOCSF(t, evs)
}

// TestNormalizeAuthEvent_Geolocation: Okta's client.geographicalContext (the
// Toronto example from Okta's own sample payload, country as the full name
// Okta sends) reaches the event as an OCSF Geo Location under src_endpoint,
// the country name carried verbatim as desc, attributed to Okta (adr/0208).
func TestNormalizeAuthEvent_Geolocation(t *testing.T) {
	in := `{"items":[{"eventType":"user.session.start","published":"2026-08-04T10:00:00Z",` +
		`"actor":{"id":"00uABC","alternateId":"alice@acme.com","type":"User"},` +
		`"client":{"ipAddress":"203.0.113.5","geographicalContext":{"city":"Toronto","state":"Ontario",` +
		`"country":"Canada","postalCode":"M4M","geolocation":{"lat":43.6529,"lon":-79.3849}}},` +
		`"outcome":{"result":"SUCCESS"}}]}`
	evs, err := normalize("get_logs", in)
	if err != nil {
		t.Fatal(err)
	}
	loc := mapAt(t, evs[0].Raw, "src_endpoint", "location")
	for k, want := range map[string]any{
		"city": "Toronto", "region": "Ontario", "desc": "Canada", "postal_code": "M4M",
		"lat": 43.6529, "long": -79.3849, "provider": "okta",
	} {
		if got := loc[k]; got != want {
			t.Errorf("location.%s = %v (%T); want %v", k, got, got, want)
		}
	}
	for _, k := range []string{"coordinates", "country"} {
		if _, has := loc[k]; has {
			t.Errorf("location.%s was emitted; the name is carried as desc, no code is guessed", k)
		}
	}
	if got := mapAt(t, evs[0].Raw, "src_endpoint")["ip"]; got != "203.0.113.5" {
		t.Errorf("src_endpoint.ip = %v", got)
	}
}

// TestGeoLocation_Edges: a numeric postal code is carried as text; a context
// with only coordinates names no place, so none is emitted; a country name
// alone stands, as desc; no context, no location.
func TestGeoLocation_Edges(t *testing.T) {
	ev := func(geo string) map[string]any {
		var e map[string]any
		if err := json.Unmarshal([]byte(`{"client":{"ipAddress":"1.2.3.4","geographicalContext":`+geo+`}}`), &e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	loc := geoLocation(ev(`{"city":"New York","state":"New York","country":"United States","postalCode":10013}`))
	if loc["postal_code"] != "10013" || loc["desc"] != "United States" {
		t.Errorf("Okta's New York example → %v", loc)
	}
	if loc := geoLocation(ev(`{"geolocation":{"lat":1,"lon":2}}`)); loc != nil {
		t.Errorf("coordinates alone emitted a location: %v", loc)
	}
	loc = geoLocation(ev(`{"country":" Türkiye "}`))
	if loc["desc"] != "Türkiye" || loc["provider"] != "okta" || len(loc) != 2 {
		t.Errorf("a country alone → %v", loc)
	}
	if geoLocation(ev(`null`)) != nil || geoLocation(map[string]any{}) != nil {
		t.Error("no context emitted a location")
	}
}

// assertOCSF is check 3 of adr/0356 for this adapter: every event it emits
// holds only fields OCSF defines for its class at the version it declares,
// each in the shape and scalar type the schema gives it, and names the class
// it is sent as.
func assertOCSF(t *testing.T, evs []ocsfEvent) {
	t.Helper()
	for i, e := range evs {
		if got := e.Raw["class_uid"]; got != e.ClassUID {
			t.Errorf("event %d: sent as class %d, and the event says %v", i, e.ClassUID, got)
		}
		if got := mapAt(t, e.Raw, "metadata")["version"]; got != ocsfVersion {
			t.Errorf("event %d: metadata.version = %v; want %s", i, got, ocsfVersion)
		}
		for _, p := range ocsfschema.ValidateEvent(e.Raw) {
			t.Errorf("event %d, class %d: %s", i, e.ClassUID, p)
		}
	}
}

// TestEveryEventIsOCSF runs one record of each kind the adapter handles
// through normalize and checks every event against the OCSF schema
// (adr/0356). The records carry every Okta field the mapping reads.
func TestEveryEventIsOCSF(t *testing.T) {
	logs := `{"items":[` +
		`{"eventType":"user.session.start","published":"2026-08-04T10:00:00Z","displayMessage":"User login to Okta",` +
		`"actor":{"id":"00uABC","alternateId":"alice@acme.com","type":"User"},` +
		`"client":{"ipAddress":"203.0.113.5","geographicalContext":{"city":"Toronto","state":"Ontario",` +
		`"country":"Canada","postalCode":"M4M","geolocation":{"lat":43.6529,"lon":-79.3849}}},"outcome":{"result":"SUCCESS"}},` +
		`{"eventType":"user.lifecycle.create","published":"2026-08-03T16:29:52Z","displayMessage":"Create okta user",` +
		`"actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},` +
		`"target":[{"id":"00uNEW","type":"User","alternateId":"bob@acme.com","displayName":"Bob"}],"outcome":{"result":"SUCCESS"}},` +
		`{"eventType":"group.user_membership.add","published":"2026-08-03T16:30:00Z","displayMessage":"Add user to group membership",` +
		`"actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},` +
		`"target":[{"id":"00uNEW","type":"User","alternateId":"bob@acme.com"},{"id":"00gADM","type":"UserGroup","displayName":"Admins"}],"outcome":{"result":"SUCCESS"}},` +
		`{"eventType":"user.account.privilege.grant","published":"2026-08-03T16:31:00Z","displayMessage":"Grant user privilege",` +
		`"actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},"debugContext":{"debugData":{"privilegeGranted":"Super administrator"}},` +
		`"target":[{"id":"00uT","type":"User","alternateId":"eve@acme.com"}],"outcome":{"result":"SUCCESS"}},` +
		`{"eventType":"system.brand.create","published":"2026-08-03T16:29:52Z","displayMessage":"Create brand",` +
		`"actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},"outcome":{"result":"SUCCESS"}}]}`
	evs, err := normalize("get_logs", logs)
	if err != nil {
		t.Fatal(err)
	}
	var classes []int
	for _, e := range evs {
		classes = append(classes, e.ClassUID)
	}
	if want := []int{3002, 3001, 3006, 3005, 0}; !slices.Equal(classes, want) {
		t.Fatalf("classes = %v; want %v", classes, want)
	}
	assertOCSF(t, evs)

	for op, in := range map[string]string{
		"get_user": `[{"id":"00uX","status":"ACTIVE","created":"2026-01-02T03:04:05Z","profile":{"login":"carol@acme.com","email":"carol@acme.com",` +
			`"firstName":"Carol","lastName":"Diaz","department":"Finance","organization":"Acme","title":"Analyst","employeeNumber":"E-7","managerId":"someone"}}]`,
		"list_groups": `[{"id":"00gADM","profile":{"name":"Admins"}}]`,
	} {
		evs, err := normalize(op, in)
		if err != nil || len(evs) != 1 {
			t.Fatalf("%s: %v, %d events", op, err, len(evs))
		}
		assertOCSF(t, evs)
	}
}

func TestNormalizeAccountLifecycleEvent(t *testing.T) {
	// user.lifecycle.create → OCSF 3001 Account Change with target user + initiator.
	in := `{"items":[{"eventType":"user.lifecycle.create","published":"2026-08-03T16:29:52Z",` +
		`"actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},` +
		`"target":[{"id":"00uNEW","type":"User","alternateId":"bob@acme.com","displayName":"Bob"}],` +
		`"outcome":{"result":"SUCCESS"}}]}`
	evs, err := normalize("get_logs", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].ClassUID != 3001 {
		t.Fatalf("want one class-3001 event, got %+v", evs)
	}
	if got := mapAt(t, evs[0].Raw, "user")["name"]; got != "bob@acme.com" {
		t.Errorf("target user.name = %v", got)
	}
	if got := mapAt(t, evs[0].Raw, "user")["uid"]; got != "00uNEW" {
		t.Errorf("target user.uid = %v", got)
	}
	if got := mapAt(t, evs[0].Raw, "actor", "user")["name"]; got != "admin@acme.com" {
		t.Errorf("initiator = %v", got)
	}
	for _, k := range []string{"group", "privileges", "okta"} {
		if _, has := evs[0].Raw[k]; has {
			t.Errorf("an account change carries %s", k)
		}
	}
	// The Okta record is kept where OCSF puts a source's own fields.
	if got := mapAt(t, evs[0].Raw, "unmapped", "okta")["eventType"]; got != "user.lifecycle.create" {
		t.Errorf("unmapped.okta.eventType = %v", got)
	}
}

// TestNormalizeGroupMembership: group.user_membership.* is Group Management
// (3006), with the group by its name and id and the member as the user.
func TestNormalizeGroupMembership(t *testing.T) {
	for _, eventType := range []string{"group.user_membership.add", "group.user_membership.remove"} {
		in := `{"items":[{"eventType":"` + eventType + `","published":"2026-08-03T16:30:00Z",` +
			`"actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},` +
			`"target":[{"id":"00uNEW","type":"User","alternateId":"bob@acme.com"},{"id":"00gADM","type":"UserGroup","displayName":"Admins"}],` +
			`"outcome":{"result":"SUCCESS"}}]}`
		evs, err := normalize("get_logs", in)
		if err != nil {
			t.Fatal(err)
		}
		if len(evs) != 1 || evs[0].ClassUID != 3006 {
			t.Fatalf("%s: want one class-3006 event, got %+v", eventType, evs)
		}
		group := mapAt(t, evs[0].Raw, "group")
		if group["name"] != "Admins" || group["uid"] != "00gADM" {
			t.Errorf("%s: group = %v", eventType, group)
		}
		if got := mapAt(t, evs[0].Raw, "user")["name"]; got != "bob@acme.com" {
			t.Errorf("%s: member = %v", eventType, got)
		}
		assertOCSF(t, evs)
	}
	// An app assignment is no group membership: it stays an account change.
	evs, _ := normalize("get_logs", `{"items":[{"eventType":"application.user_membership.add","actor":{"id":"a"},"target":[{"id":"00uT","type":"User","alternateId":"eve@acme.com"}]}]}`)
	if len(evs) != 1 || evs[0].ClassUID != 3001 {
		t.Fatalf("application.user_membership.add: want class 3001, got %+v", evs)
	}
}

// TestNormalizePrivilegeGrant: a privilege grant or revocation is User Access
// Management (3005). privileges is a list of the roles Okta names, never the
// display message.
func TestNormalizePrivilegeGrant(t *testing.T) {
	for eventType, debug := range map[string]string{
		"user.account.privilege.grant":  `{"privilegeGranted":"Organization administrator, Application administrator (all)"}`,
		"user.account.privilege.revoke": `{"privilegeRevoked":"Organization administrator, Application administrator (all)"}`,
	} {
		in := `{"items":[{"eventType":"` + eventType + `","published":"2026-08-03T16:00:00Z",` +
			`"actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},"displayMessage":"Grant user privilege",` +
			`"debugContext":{"debugData":` + debug + `},` +
			`"target":[{"id":"00uT","type":"User","alternateId":"eve@acme.com"}],"outcome":{"result":"SUCCESS"}}]}`
		evs, _ := normalize("get_logs", in)
		if len(evs) != 1 || evs[0].ClassUID != 3005 {
			t.Fatalf("%s: want class-3005, got %+v", eventType, evs)
		}
		want := []any{"Organization administrator", "Application administrator (all)"}
		if got, _ := evs[0].Raw["privileges"].([]any); !slices.Equal(got, want) {
			t.Errorf("%s: privileges = %v; want %v", eventType, evs[0].Raw["privileges"], want)
		}
		if got := evs[0].Raw["message"]; got != "Grant user privilege" {
			t.Errorf("%s: message = %v", eventType, got)
		}
		assertOCSF(t, evs)
	}
	// An event that names no role carries no privileges.
	evs, _ := normalize("get_logs", `{"items":[{"eventType":"user.account.privilege.grant","displayMessage":"Grant user privilege","actor":{"id":"a"}}]}`)
	if _, has := evs[0].Raw["privileges"]; has || evs[0].ClassUID != 3005 {
		t.Errorf("no role named: class %d, privileges %v", evs[0].ClassUID, evs[0].Raw["privileges"])
	}
}

func TestNormalizeAdminEventStaysOpaque(t *testing.T) {
	// A config/admin event is NOT an authentication — must not be force-fit to 3002.
	in := `{"items":[{"eventType":"system.brand.create","published":"2026-08-03T16:29:52Z",` +
		`"actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},"outcome":{"result":"SUCCESS"}}]}`
	evs, _ := normalize("get_logs", in)
	if len(evs) != 1 || evs[0].ClassUID != 0 {
		t.Fatalf("admin event should be opaque (class 0), got %+v", evs)
	}
	if got := mapAt(t, evs[0].Raw, "unmapped", "okta")["eventType"]; got != "system.brand.create" {
		t.Errorf("the Okta record is not kept: %v", evs[0].Raw)
	}
	assertOCSF(t, evs)
}

// TestNormalizeUserLookup: a user record is User Inventory Info (5003), the
// input the engine's user-inventory normalizer reads (03 §4.19).
func TestNormalizeUserLookup(t *testing.T) {
	in := `[{"id":"00uX","status":"ACTIVE","profile":{"login":"carol@acme.com","email":"carol.diaz@acme.com",` +
		`"firstName":"Carol","lastName":"Diaz","department":"Finance","title":"Analyst","employeeNumber":"E-7"}}]`
	evs, err := normalize("get_user", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].ClassUID != 5003 {
		t.Fatalf("want class-5003 user, got %+v", evs)
	}
	user := mapAt(t, evs[0].Raw, "user")
	for k, want := range map[string]any{"uid": "00uX", "name": "carol@acme.com", "email_addr": "carol.diaz@acme.com", "full_name": "Carol Diaz"} {
		if user[k] != want {
			t.Errorf("user.%s = %v; want %v", k, user[k], want)
		}
	}
	if got := mapAt(t, evs[0].Raw, "user", "org")["ou_name"]; got != "Finance" {
		t.Errorf("user.org.ou_name = %v", got)
	}
	person := mapAt(t, evs[0].Raw, "user", "ldap_person")
	if person["job_title"] != "Analyst" || person["employee_uid"] != "E-7" {
		t.Errorf("user.ldap_person = %v", person)
	}
	// The product names the directory the person's id is unique in.
	if got := mapAt(t, evs[0].Raw, "metadata", "product")["name"]; got != "Okta" {
		t.Errorf("metadata.product.name = %v", got)
	}
	assertOCSF(t, evs)

	// A profile with only a login leaves the empty fields out.
	evs, _ = normalize("get_user", `[{"id":"00uY","profile":{"login":"dan@acme.com"}}]`)
	if user := mapAt(t, evs[0].Raw, "user"); len(user) != 2 {
		t.Errorf("user = %v; want only uid and name", user)
	}
}

// engineNormalize sends the adapter's events through the engine's
// normalizers, as JSON, the way they cross the plugin boundary.
func engineNormalize(t *testing.T, evs []ocsfEvent) []capability.NormalizationResult {
	t.Helper()
	reg := capability.NewRegistry(identity.NewResolver(uuid.MustParse("33333333-3333-4333-8333-333333333333")))
	var out []capability.NormalizationResult
	for _, e := range evs {
		wire, err := json.Marshal(e.Raw)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(wire, &payload); err != nil {
			t.Fatal(err)
		}
		res, err := reg.Normalize(capability.OcsfEvent{ID: uuid.Must(uuid.NewV7()), ClassUID: e.ClassUID, Time: e.Time, SourceTool: "okta:test", Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, res)
	}
	return out
}

// TestEngineReadsWhatTheAdapterWrites (adr/0356): the engine's normalizers
// read each class the adapter emits. A user record names a person and the
// Okta sign-in account, a group event names the group and the membership,
// and a privilege event keeps its privileges as a list.
func TestEngineReadsWhatTheAdapterWrites(t *testing.T) {
	count := func(res capability.NormalizationResult, typ string) int {
		n := 0
		for _, s := range res.SCOs {
			if s.Type == typ {
				n++
			}
		}
		return n
	}

	users, _ := normalize("get_user", `[{"id":"00uX","profile":{"login":"carol@acme.com","email":"carol@acme.com","firstName":"Carol","lastName":"Diaz"}}]`)
	res := engineNormalize(t, users)[0]
	if got := res.ObservedData[0].Provenance.Normalizer; got != "user_inventory" {
		t.Fatalf("a user record went to %s", got)
	}
	if count(res, identity.TypeIdentity) != 1 || count(res, identity.TypeUserAccount) == 0 {
		t.Errorf("a user record names %d persons and %d accounts; want one person and its accounts", count(res, identity.TypeIdentity), count(res, identity.TypeUserAccount))
	}
	for _, s := range res.SCOs {
		if s.Type == identity.TypeIdentity && (s.Properties["directory"] != "okta" || s.Properties["directory_id"] != "00uX" || s.Properties["name"] != "Carol Diaz") {
			t.Errorf("person = %v", s.Properties)
		}
	}

	logs, _ := normalize("get_logs", `{"items":[`+
		`{"eventType":"group.user_membership.add","published":"2026-08-03T16:30:00Z","actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},`+
		`"target":[{"id":"00uNEW","type":"User","alternateId":"bob@acme.com"},{"id":"00gADM","type":"UserGroup","displayName":"Admins"}]},`+
		`{"eventType":"user.account.privilege.grant","published":"2026-08-03T16:31:00Z","actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},`+
		`"debugContext":{"debugData":{"privilegeGranted":"Super administrator"}},"target":[{"id":"00uT","type":"User","alternateId":"eve@acme.com"}]}]}`)
	got := engineNormalize(t, logs)
	group, access := got[0], got[1]
	if group.ObservedData[0].Provenance.Normalizer != "group_management" || count(group, identity.TypeGroup) != 1 {
		t.Errorf("group event: normalizer %s, %d groups", group.ObservedData[0].Provenance.Normalizer, count(group, identity.TypeGroup))
	}
	member := false
	for _, r := range group.Relationships {
		member = member || r.Type == capability.RelMemberOfGroup
	}
	if !member {
		t.Error("group event: no member-of-group relationship")
	}
	if access.ObservedData[0].Provenance.Normalizer != "user_access" {
		t.Errorf("privilege event went to %s", access.ObservedData[0].Provenance.Normalizer)
	}
	if privs, _ := access.ObservedData[0].Extensions["privileges"].([]string); !slices.Equal(privs, []string{"Super administrator"}) {
		t.Errorf("privileges = %v", access.ObservedData[0].Extensions["privileges"])
	}
}
