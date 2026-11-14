package main

import (
	"encoding/json"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/sd-strax/reckon/internal/ocsf"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// ocsfVersion is the OCSF release this adapter writes (adr/0356). It is the
// oldest release that has every field the adapter fills: the latitude and
// longitude of a sign-in's place arrive in 1.2.0.
const ocsfVersion = "1.2.0"

// The OCSF classes this adapter writes (adr/0356).
const (
	classBase            = 0    // Base Event: a record no class describes
	classAuthentication  = 3002 // a sign-in
	classAccountChange   = 3001 // a lifecycle or account event
	classUserAccess      = 3005 // a privilege granted or revoked
	classGroupManagement = 3006 // a member added to or removed from a group
	classUserInventory   = 5003 // a directory's record of one user
)

// Okta's products, as metadata.product names them. The engine reads the
// product to tell an identity provider's records from a host's (03 §7.4).
const (
	productSystemLog = "Okta System Log"
	productDirectory = "Okta"
)

// normalize shapes an okta-mcp tool's JSON text output into OCSF events (11 §0
// — by the time data crosses the adapter boundary it is OCSF; the engine's
// normalizer then maps OCSF → STIX). It reshapes Okta's native fields into
// OCSF's own classes and fields (adr/0356), and keeps the original Okta
// object under unmapped.okta, which is where OCSF puts a source's own fields.
//
// Okta's System Log is heterogeneous (auth, account lifecycle, admin/config —
// all event types), so get_logs events are classified by eventType:
//
//   - a sign-in becomes Authentication (3002)
//   - a lifecycle or account event becomes Account Change (3001)
//   - a group.user_membership.* event becomes Group Management (3006)
//   - a privilege grant or revocation becomes User Access Management (3005),
//     with no privileges[] (see accessPayload)
//   - everything else is a Base Event (class 0 → the engine's default
//     normalizer, 03 §4.13) rather than force-fit: an admin config change is
//     not an authentication and must not be labeled one.
//
// A user record becomes User Inventory Info (5003, 03 §4.19).
func normalize(operation, text string) ([]ocsfEvent, error) {
	objs, err := extractObjects(text)
	if err != nil {
		return nil, err
	}
	switch operation {
	case "get_logs":
		return normalizeLogs(objs), nil
	case "get_user", "list_users", "list_group_users":
		return normalizeUsers(objs), nil
	default: // groups etc. — preserved opaque until a dedicated shape is needed
		return opaque(objs), nil
	}
}

// event wraps OCSF fields as one event of a class: it adds the class, the
// OCSF version, the product and the event's time, and keeps the Okta object
// the fields were read from under unmapped.okta.
//
// The time is written as ocsf.SetTime describes.
func event(class int, name, product string, t time.Time, fields, okta map[string]any) ocsfEvent {
	raw := ocsf.Event(class, ocsfVersion, ocsf.Product("Okta", product), t)
	raw["unmapped"] = map[string]any{"okta": okta}
	maps.Copy(raw, fields)
	return ocsfEvent{ClassUID: class, ClassName: name, Time: t, Raw: raw}
}

// normalizeLogs classifies each Okta System Log event and reshapes it.
func normalizeLogs(items []map[string]any) []ocsfEvent {
	out := make([]ocsfEvent, 0, len(items))
	for _, e := range items {
		t := eventTime(e, "published")
		switch classify(str(e["eventType"])) {
		case classAuthentication:
			out = append(out, event(classAuthentication, "Authentication", productSystemLog, t, authPayload(e), e))
		case classAccountChange:
			out = append(out, event(classAccountChange, "Account Change", productSystemLog, t, accountPayload(e), e))
		case classGroupManagement:
			out = append(out, event(classGroupManagement, "Group Management", productSystemLog, t, groupPayload(e), e))
		case classUserAccess:
			out = append(out, event(classUserAccess, "User Access Management", productSystemLog, t, accessPayload(e), e))
		default:
			out = append(out, event(classBase, "Base Event", productSystemLog, t, logFields(e), e))
		}
	}
	return out
}

// normalizeUsers reshapes each Okta user object into an OCSF User Inventory
// Info event (5003), the record the engine's user-inventory normalizer reads
// (03 §4.19): the directory's id for the user, the sign-in name, and what the
// profile says about the person.
func normalizeUsers(users []map[string]any) []ocsfEvent {
	out := make([]ocsfEvent, 0, len(users))
	for _, u := range users {
		p := map[string]any{"user": inventoryUser(u)}
		out = append(out, event(classUserInventory, "User Inventory Info", productDirectory, eventTime(u, "created"), p, u))
	}
	return out
}

// inventoryUser maps an Okta user object onto an OCSF User: id to uid, the
// profile's login to name, email to email_addr, the display name (else the
// first and last name) to full_name, department and organization to org, and
// title and employeeNumber to ldap_person. A field the profile leaves empty
// is left out. Okta's managerId is free text, not a user id, so no manager is
// named.
func inventoryUser(u map[string]any) map[string]any {
	profile := func(k string) string { return strings.TrimSpace(digStr(u, "profile", k)) }
	user := map[string]any{}
	put := func(m map[string]any, k, v string) {
		if v != "" {
			m[k] = v
		}
	}
	put(user, "uid", str(u["id"]))
	put(user, "name", firstNonEmpty(profile("login"), profile("email")))
	put(user, "email_addr", profile("email"))
	put(user, "full_name", firstNonEmpty(profile("displayName"), strings.TrimSpace(profile("firstName")+" "+profile("lastName"))))
	org := map[string]any{}
	put(org, "name", profile("organization"))
	put(org, "ou_name", profile("department"))
	if len(org) > 0 {
		user["org"] = org
	}
	person := map[string]any{}
	put(person, "job_title", profile("title"))
	put(person, "employee_uid", profile("employeeNumber"))
	if len(person) > 0 {
		user["ldap_person"] = person
	}
	return user
}

// opaque keeps an Okta object no OCSF class describes (a group) as a Base
// Event: the object rides under unmapped.okta.
func opaque(objs []map[string]any) []ocsfEvent {
	out := make([]ocsfEvent, 0, len(objs))
	for _, o := range objs {
		out = append(out, event(classBase, "Base Event", productDirectory, time.Time{}, nil, o))
	}
	return out
}

// logFields are the OCSF fields every System Log event fills, whatever its
// class: Okta's displayMessage as the message and outcome.result as the
// status. An empty one is left out. status_id is OCSF's number for the
// result, or Other (99) for a result OCSF does not have, such as SKIPPED.
func logFields(e map[string]any) map[string]any {
	p := map[string]any{}
	if m := str(e["displayMessage"]); m != "" {
		p["message"] = m
	}
	if s := digStr(e, "outcome", "result"); s != "" {
		p["status"] = s
		p["status_id"] = ocsf.CaptionID(ocsf.Statuses, s)
	}
	return p
}

// oktaUser maps an Okta actor or target onto an OCSF User: alternateId (the
// login), else displayName, as the name, and id as the uid. An empty one is left out.
func oktaUser(o map[string]any) map[string]any {
	user := map[string]any{}
	if name := firstNonEmpty(str(o["alternateId"]), str(o["displayName"])); name != "" {
		user["name"] = name
	}
	if uid := str(o["id"]); uid != "" {
		user["uid"] = uid
	}
	return user
}

// initiator is the System Log event's actor as an OCSF actor.
func initiator(e map[string]any) map[string]any {
	actor, _ := e["actor"].(map[string]any)
	return map[string]any{"user": oktaUser(actor)}
}

// authPayload maps an Okta auth LogEvent onto the OCSF 3002 paths the engine's
// auth normalizer reads: user.{name,uid}, src_endpoint.ip, status. In a
// sign-in event Okta's actor is the account signing in, which OCSF calls the
// event's user (adr/0356). The client's reported place rides
// src_endpoint.location (adr/0208).
//
// Some session events are done to another account: an admin clears a user's
// sessions (user.session.clear) or impersonates a user
// (user.session.impersonation.*). Okta's LogEvent schema says the actor is
// the entity "who performs an action on a target" and the target is "the
// entity that an actor performs an action on"
// (https://developer.okta.com/docs/api/openapi/okta-management/management/tag/SystemLog/).
// Okta's event-type catalog
// (https://developer.okta.com/docs/reference/api/event-types/) does not list
// the targets of each event type, so the rule goes by the event itself: when
// it has a User target that is not the actor, that target is the user and
// the actor is the initiator, as in accountPayload. Otherwise the actor is
// the user.
func authPayload(e map[string]any) map[string]any {
	src := map[string]any{"ip": digStr(e, "client", "ipAddress")}
	if loc := geoLocation(e); loc != nil {
		src["location"] = loc
	}
	p := logFields(e)
	actor, _ := e["actor"].(map[string]any)
	if tu := firstTarget(e, "User"); tu != nil && !sameOktaEntity(tu, actor) {
		p["user"] = oktaUser(tu)
		p["actor"] = initiator(e)
	} else {
		p["user"] = oktaUser(actor)
	}
	p["src_endpoint"] = src
	// Okta's event type is not one of OCSF's logon types, so the id is Other
	// and the event type is the caption.
	p["logon_type"] = str(e["eventType"])
	p["logon_type_id"] = ocsf.Other
	return p
}

// sameOktaEntity reports whether an Okta actor and target are one entity: by
// id when both carry one, else by alternateId, letter case aside.
func sameOktaEntity(a, b map[string]any) bool {
	if ai, bi := str(a["id"]), str(b["id"]); ai != "" && bi != "" {
		return ai == bi
	}
	return strings.EqualFold(str(a["alternateId"]), str(b["alternateId"]))
}

// geoProvider attributes the place: it is Okta's IP geolocation of the client,
// not where the person was. OCSF's location.provider is "the provider of the
// geographical location data".
const geoProvider = "okta"

// geoLocation reshapes Okta's client.geographicalContext into an OCSF Geo
// Location object (adr/0208): city; region from Okta's state, carried as the
// name Okta gives (OCSF names the field a subdivision code; Okta has no code);
// the country name Okta gives as desc, verbatim — OCSF's country is the ISO
// alpha-2 code, Okta sends a name, and reckon carries the name rather than
// guess the code; the postal code as a string (Okta's examples send both a
// number and a string; a number has lost any leading zero before it reaches
// here); lat and long from geolocation (OCSF deprecated coordinates in 1.2.0
// for these two); and the provider. nil when the context names no place at
// all (coordinates alone are not one).
func geoLocation(e map[string]any) map[string]any {
	geo, _ := digAny(e, "client", "geographicalContext").(map[string]any)
	if geo == nil {
		return nil
	}
	loc := map[string]any{}
	if v := str(geo["city"]); v != "" {
		loc["city"] = v
	}
	if v := str(geo["state"]); v != "" {
		loc["region"] = v
	}
	if v := strings.TrimSpace(str(geo["country"])); v != "" {
		loc["desc"] = v
	}
	if v := scalarString(geo["postalCode"]); v != "" {
		loc["postal_code"] = v
	}
	if len(loc) == 0 {
		return nil
	}
	if lat, ok := toFloat(digAny(geo, "geolocation", "lat")); ok {
		if lon, ok := toFloat(digAny(geo, "geolocation", "lon")); ok {
			loc["lat"] = lat
			loc["long"] = lon
		}
	}
	loc["provider"] = geoProvider
	return loc
}

// scalarString reads a string, or a JSON number as text (json.Unmarshal into
// any gives float64); anything else is "".
func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// toFloat reads a JSON number.
func toFloat(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// accountPayload maps an Okta lifecycle or account LogEvent onto OCSF Account
// Change (3001): user (the target), actor.user (the initiator), and the
// message and status every log event carries.
func accountPayload(e map[string]any) map[string]any {
	p := logFields(e)
	p["actor"] = initiator(e)
	if tu := firstTarget(e, "User"); tu != nil {
		p["user"] = oktaUser(tu)
	}
	return p
}

// groupPayload maps a group.user_membership.* LogEvent onto OCSF Group
// Management (3006): the account fields, plus the UserGroup target as the
// group, by its displayName and its id.
func groupPayload(e map[string]any) map[string]any {
	p := accountPayload(e)
	if tg := firstTarget(e, "UserGroup"); tg != nil {
		group := map[string]any{}
		if n := str(tg["displayName"]); n != "" {
			group["name"] = n
		}
		if id := str(tg["id"]); id != "" {
			group["uid"] = id
		}
		p["group"] = group
	}
	return p
}

// accessPayload maps a privilege grant or revocation onto OCSF User Access
// Management (3005): the account fields, with Okta's display message as the
// message. It writes no privileges[].
//
// Okta's System Log documentation names no field that holds the privilege
// granted or revoked:
//
//   - The event-type catalog describes user.account.privilege.grant as
//     carrying "the type of admin privileges the user currently has", roles
//     inherited through groups included, and names no field for them.
//     https://developer.okta.com/docs/reference/api/event-types/
//   - The System Log API reference lists no debugContext.debugData keys for
//     these events, and says of debugData: "Both key names and values may
//     change from release to release and aren't guaranteed to be stable."
//     https://developer.okta.com/docs/api/openapi/okta-management/management/tags/systemlog/other/listlogevents
//   - Okta's support article on the revoke event says its PrivilegeRevoked
//     field is "a general message" that does "not represent the actual
//     privileges that the user account had before revocation": it lists the
//     roles that remain, or every standard role when none remain.
//     https://support.okta.com/help/s/article/why-the-revoke-admin-right-system-logs-event-shows-more-admin-roles
//
// So debugData's privilegeGranted and privilegeRevoked are not read as the
// event's privileges: a list of the roles a user holds, or of every standard
// role, is not the privilege this event granted or revoked. The Okta record,
// debugData included, is still kept whole under unmapped.okta.
func accessPayload(e map[string]any) map[string]any {
	return accountPayload(e)
}

// classify maps an Okta eventType to the OCSF class it is written as.
// Prefixes follow Okta's System Log event-type taxonomy.
func classify(eventType string) int {
	switch {
	case hasAnyPrefix(eventType, "user.authentication", "user.session"),
		strings.Contains(eventType, ".signon"),
		strings.Contains(eventType, "mfa.factor.verify"):
		return classAuthentication
	case strings.HasPrefix(eventType, "group.user_membership"):
		return classGroupManagement
	case strings.HasPrefix(eventType, "user.account.privilege"):
		return classUserAccess
	case hasAnyPrefix(eventType, "user.lifecycle", "user.account"),
		strings.Contains(eventType, "user_membership"),
		strings.Contains(eventType, "mfa.factor.activate"),
		strings.Contains(eventType, "mfa.factor.reset"):
		return classAccountChange
	default:
		return classBase
	}
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// firstTarget returns the first target[] element of the given Okta type.
func firstTarget(e map[string]any, typ string) map[string]any {
	arr, ok := e["target"].([]any)
	if !ok {
		return nil
	}
	for _, el := range arr {
		if m, ok := el.(map[string]any); ok && str(m["type"]) == typ {
			return m
		}
	}
	return nil
}

// extractObjects pulls the list of Okta objects out of a tool result, whether a
// bare array, a single object, or a wrapper object with a well-known array field
// (get_logs returns {"items": [...]}).
func extractObjects(text string) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var arr []map[string]any
		if err := json.Unmarshal([]byte(trimmed), &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return nil, err
	}
	for _, key := range []string{"items", "logs", "events", "users", "groups", "data", "results"} {
		if arr, ok := obj[key].([]any); ok {
			out := make([]map[string]any, 0, len(arr))
			for _, e := range arr {
				if m, ok := e.(map[string]any); ok {
					out = append(out, m)
				}
			}
			return out, nil
		}
	}
	return []map[string]any{obj}, nil
}

// eventTime parses an RFC3339 timestamp at key, or returns the zero time.
func eventTime(o map[string]any, key string) time.Time {
	if s, ok := o[key].(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// digStr reads a nested string at a path of map keys.
func digStr(m map[string]any, path ...string) string { return str(digAny(m, path...)) }

// digAny reads a nested value at a path of map keys; nil when the path is not
// there.
func digAny(m map[string]any, path ...string) any {
	cur := any(m)
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
