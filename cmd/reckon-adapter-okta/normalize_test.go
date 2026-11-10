package main

import (
	"encoding/json"
	"testing"
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

func TestNormalizeAccountLifecycleEvent(t *testing.T) {
	// user.lifecycle.create → OCSF 3005 with target user + initiator.
	in := `{"items":[{"eventType":"user.lifecycle.create","published":"2026-08-03T16:29:52Z",` +
		`"actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},` +
		`"target":[{"id":"00uNEW","type":"User","alternateId":"bob@acme.com","displayName":"Bob"}],` +
		`"outcome":{"result":"SUCCESS"}}]}`
	evs, err := normalize("get_logs", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].ClassUID != 3005 {
		t.Fatalf("want one class-3005 event, got %+v", evs)
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
}

func TestNormalizePrivilegeGrant(t *testing.T) {
	in := `{"items":[{"eventType":"user.account.privilege.grant","published":"2026-08-03T16:00:00Z",` +
		`"actor":{"id":"00uADMIN","alternateId":"admin@acme.com"},"displayMessage":"Grant user privilege",` +
		`"target":[{"id":"00uT","type":"User","alternateId":"eve@acme.com"}],"outcome":{"result":"SUCCESS"}}]}`
	evs, _ := normalize("get_logs", in)
	if len(evs) != 1 || evs[0].ClassUID != 3005 {
		t.Fatalf("want class-3005, got %+v", evs)
	}
	if got := evs[0].Raw["privileges"]; got != "Grant user privilege" {
		t.Errorf("privileges = %v", got)
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
}

func TestNormalizeUserLookup(t *testing.T) {
	in := `[{"id":"00uX","status":"ACTIVE","profile":{"login":"carol@acme.com","email":"carol@acme.com"}}]`
	evs, err := normalize("get_user", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].ClassUID != 3005 {
		t.Fatalf("want class-3005 user, got %+v", evs)
	}
	if got := mapAt(t, evs[0].Raw, "user")["name"]; got != "carol@acme.com" {
		t.Errorf("user.name = %v", got)
	}
}
