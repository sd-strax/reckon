package capability

import (
	"math"
	"strconv"
	"strings"

	"github.com/sd-strax/reckon/identity"
)

// apiActivityNormalizer handles OCSF api_activity (class_uid 6003) → STIX
// (§4.15): a cloud control-plane call. The principal is a user-account keyed
// on the provider's durable principal id, the account the call ran in an
// x-cloud-account, each resource it named an x-cloud-resource, the caller's
// address an ipv4/ipv6-addr. A resource named under the account belongs-to it;
// the principal belongs-to its own account. The operation, its outcome and the
// session ride on the one ObservedData, which is the observation: there is no
// edge from principal to resource. An event with no cloud.provider mints no
// cloud identities at all (see Normalize).
type apiActivityNormalizer struct{ r *identity.Resolver }

func (*apiActivityNormalizer) ClassUID() int { return 6003 }

// Version starts at 2: a 6003 event normalized before this normalizer
// existed went through the opaque default at version 1, and the
// ObservedData id carries the version but not the normalizer's name, so a
// version 1 here would mint the same id and keep the entity-less observation.
func (*apiActivityNormalizer) Version() int { return 2 }

func (n *apiActivityNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "api_activity", n.Version())
	provider := providerSlug(pathStr(p, "cloud.provider"))
	if provider == "" {
		// OCSF API Activity also describes non-cloud application APIs. With
		// no provider there is no namespace for a cloud identity, so nothing
		// cloud-shaped is minted: the call is still observed, with its source.
		b.ipAddr("src_endpoint.ip")
		return b.finish(apiExt(p)), nil
	}

	principal := b.cloudPrincipal(provider)
	account := b.cloudAccount(provider)
	for _, res := range b.cloudResources(provider, account) {
		// A uid carries its own owner, which may not be the account the call
		// ran in (cross-account access); only a resource scoped under the
		// account is asserted to belong to it.
		if res.rule == identity.CloudResourceByName {
			b.addRel(RelBelongsTo, res.id, account)
		}
	}
	if principal != "" {
		if home := b.callerAccount(provider, account); home != "" {
			b.addRel(RelBelongsTo, principal, home)
		}
	}
	b.ipAddr("src_endpoint.ip")

	return b.finish(apiExt(p)), nil
}

// providerSlug maps an OCSF cloud.provider to the slug a cloud principal's
// account_type and a cloud account's provider carry (03 §7.2). The provider is
// the principal's namespace, deliberately not "cloud" (the IdP sign-in
// account): an ARN and an IdP user id never share one. A provider the table
// does not know is kept, lowercased.
func providerSlug(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "aws", "amazon web services":
		return "aws"
	case "azure", "microsoft azure":
		return "azure"
	case "gcp", "google cloud", "google cloud platform":
		return "gcp"
	}
	return p
}

// sessionScopedActors names, per provider slug, the field holding the durable
// principal behind a session-scoped actor id. The rule is "the provider's
// durable principal identifier"; a row exists where a provider's actor id is
// session-scoped (an AWS assumed-role ARN names the session, the session's
// issuer names the role), so another such provider is a row, not a branch.
var sessionScopedActors = map[string]string{
	"aws": "actor.session.issuer",
}

// durablePrincipalID returns the provider's durable id for the caller and
// whether it came from the session-scoped row: that row's field when the
// provider has one and it is present, else actor.user.uid, else uid_alt.
func durablePrincipalID(slug string, p map[string]any) (id string, fromSession bool) {
	if path, ok := sessionScopedActors[slug]; ok {
		if v := pathScalar(p, path); v != "" {
			return v, true
		}
	}
	return firstNonEmpty(pathScalar(p, "actor.user.uid"), pathScalar(p, "actor.user.uid_alt")), false
}

// userTypes are the OCSF user.type_id captions.
var userTypes = map[int]string{1: "User", 2: "Admin", 3: "System", 4: "Service", 99: "Other"}

// cloudPrincipal extracts the caller as a user-account (03 §4.15, §7.4
// shaping): identity is the provider's durable principal id with the provider
// slug as account_type, and no login, since a principal name is neither
// unique nor stable across accounts. The name is the display name. With no
// durable id no principal is minted; the name still rides the ObservedData.
func (b *resultBuilder) cloudPrincipal(provider string) identity.STIXID {
	p := b.evt.Payload
	userID, fromSession := durablePrincipalID(provider, p)
	user, _ := lookupPath(p, "actor.user")
	m, _ := user.(map[string]any)
	return b.cloudUser(provider, userID, m, fromSession, credentialUID(p))
}

// cloudUser adds the user-account for a cloud principal from its OCSF User
// object: keyed on userID in the provider namespace, with an empty login, the
// full name or name as the display name, and the type, credential and home
// account when given. The caller supplies the durable id, since which field
// holds it depends on where the object sits (the 6003 actor, a 5023 resource
// owner). An empty userID mints nothing.
func (b *resultBuilder) cloudUser(provider, userID string, user map[string]any, service bool, credential string) identity.STIXID {
	if userID == "" {
		return ""
	}
	if user == nil {
		user = map[string]any{}
	}
	typeID := pathInt(user, "type_id")
	props := map[string]any{
		"account_login":      "",
		"user_id":            userID,
		"account_type":       provider,
		"is_service_account": service || typeID == 3 || typeID == 4,
	}
	if d := firstNonEmpty(pathStr(user, "full_name"), strings.TrimSpace(pathStr(user, "name"))); d != "" {
		props["display_name"] = d
	}
	if t := firstNonEmpty(userTypes[typeID], pathStr(user, "type")); t != "" {
		props["user_type"] = t
	}
	if credential != "" {
		props["credential_uid"] = credential
	}
	if a := pathScalar(user, "account.uid"); a != "" {
		props["cloud_account_uid"] = a
	}
	return b.addSCO(b.r.Account(identity.AccountIdentity{Type: provider, UserID: userID}), identity.TypeUserAccount, props)
}

// credentialUID is the access key the call was made with: the
// programmatic_credentials list (1.6.0 and later) first, then the deprecated
// user and session fields.
func credentialUID(p map[string]any) string {
	if v, ok := lookupPath(p, "actor.user.programmatic_credentials"); ok {
		if list, ok := v.([]any); ok && len(list) > 0 {
			if m, ok := list[0].(map[string]any); ok {
				if c := toString(fieldOf(m, "uid")); c != "" {
					return c
				}
			}
		}
	}
	return firstNonEmpty(pathStr(p, "actor.user.credential_uid"), pathStr(p, "actor.session.credential_uid"))
}

// accountTypes are the OCSF account.type_id captions for the cloud account
// kinds; any other id reads account.type as given.
var accountTypes = map[int]string{
	5: "GCP Account", 6: "Azure AD Account", 10: "AWS Account",
	11: "GCP Project", 13: "Azure Subscription", 17: "M365 Tenant",
}

// cloudAccount extracts the x-cloud-account the call ran in: cloud.account
// first, then the caller's own account, then the deprecated cloud.project_uid
// as a last fallback. Returns "" when the event names no account.
func (b *resultBuilder) cloudAccount(provider string) identity.STIXID {
	p := b.evt.Payload
	for _, pre := range []string{"cloud.account", "actor.user.account"} {
		if uid := pathScalar(p, pre+".uid"); uid != "" {
			return b.addAccount(provider, uid, pre)
		}
	}
	if uid := pathScalar(p, "cloud.project_uid"); uid != "" {
		return b.addAccount(provider, uid, "")
	}
	return ""
}

// callerAccount returns the principal's own account from actor.user.account:
// the event's account when it is the same one, else a second x-cloud-account
// (a cross-account call). Returns "" when the payload does not name it; the
// event's account is never assumed to be the caller's.
func (b *resultBuilder) callerAccount(provider string, eventAccount identity.STIXID) identity.STIXID {
	uid := pathScalar(b.evt.Payload, "actor.user.account.uid")
	if uid == "" {
		return ""
	}
	if id := b.r.CloudAccount(provider, uid); id == eventAccount {
		return id
	}
	return b.addAccount(provider, uid, "actor.user.account")
}

// addAccount adds an x-cloud-account, its descriptive properties read from the
// OCSF account object at prefix (none when prefix is empty). cloud.org
// describes the account the call ran in, so org_uid attaches only to an
// account read from cloud.account or cloud.project_uid, never to one read
// from the caller's actor.user.account.
func (b *resultBuilder) addAccount(provider, uid, prefix string) identity.STIXID {
	p := b.evt.Payload
	props := map[string]any{"provider": provider, "account_uid": uid}
	if prefix != "" {
		if name := pathStr(p, prefix+".name"); name != "" {
			props["name"] = name
		}
		if t := firstNonEmpty(accountTypes[pathInt(p, prefix+".type_id")], pathStr(p, prefix+".type")); t != "" {
			props["account_type"] = t
		}
	}
	if prefix != "actor.user.account" {
		if org := pathScalar(p, "cloud.org.uid"); org != "" {
			props["org_uid"] = org
		}
	}
	return b.addSCO(b.r.CloudAccount(provider, uid), identity.TypeCloudAccount, props)
}

// cloudResource is a resource the event named and the rule that identified it.
type cloudResource struct {
	id   identity.STIXID
	rule identity.CloudResourceRule
}

// cloudResources extracts an x-cloud-resource for each resources[] entry the
// resolver can identify (by the provider's unique id, else by name under the
// event's account), each once; an entry with neither stays in the raw payload.
func (b *resultBuilder) cloudResources(provider string, account identity.STIXID) []cloudResource {
	v, ok := lookupPath(b.evt.Payload, "resources")
	if !ok {
		return nil
	}
	list, _ := v.([]any)
	var out []cloudResource
	seen := map[identity.STIXID]bool{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		uid, name, typ, region := scalarString(fieldOf(m, "uid")), toString(fieldOf(m, "name")), toString(fieldOf(m, "type")), scalarString(fieldOf(m, "region"))
		zone, group, namespace := scalarString(fieldOf(m, "zone")), resourceGroup(m), scalarString(fieldOf(m, "namespace"))
		id, rule := b.r.CloudResource(identity.CloudResourceIdentity{
			Provider: provider, UID: uid, AccountRef: account, Type: typ,
			Region: region, Zone: zone, Group: group, Namespace: namespace, Name: name,
		})
		if rule == identity.CloudResourceNone || seen[id] {
			continue
		}
		seen[id] = true
		props := map[string]any{"provider": provider}
		fields := map[string]string{
			"uid": uid, "name": name, "type": typ,
			"region": region, "zone": zone, "group": group, "namespace": namespace,
			"cloud_partition": toString(fieldOf(m, "cloud_partition")),
		}
		// Only a scoped resource names the account it lives in; a uid
		// carries its own owner.
		if rule == identity.CloudResourceByName {
			fields["account_ref"] = string(account)
		}
		for k, val := range fields {
			if val != "" {
				props[k] = val
			}
		}
		out = append(out, cloudResource{id: b.addSCO(id, identity.TypeCloudResource, props), rule: rule})
	}
	return out
}

// resourceGroup reads an OCSF resource's group (a Group object): its name,
// else its uid.
func resourceGroup(m map[string]any) string {
	g, _ := fieldOf(m, "group").(map[string]any)
	return firstNonEmpty(toString(fieldOf(g, "name")), scalarString(fieldOf(g, "uid")))
}

// pathScalar reads a scalar at path as a string, an identifier given as a
// JSON number included (an account id), formatted without an exponent.
func pathScalar(p map[string]any, path string) string {
	v, _ := lookupPath(p, path)
	return scalarString(v)
}

// scalarString is the trimmed string form of a string or numeric value; ""
// for anything else. A JSON number decodes as float64, which holds an integer
// exactly only below 2^53: a longer id (a 21-digit numeric principal id)
// arrives already rounded, and two distinct ids would mint one entity. So a
// number is an identifier only when it is an exactly representable integer;
// a fraction or a rounded integer reads as absent and mints nothing.
func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x != math.Trunc(x) || math.Abs(x) >= 1<<53 {
			return ""
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return ""
}

// apiActivities and apiStatuses are the OCSF captions for a payload that
// carries the id without the name.
var (
	apiActivities = map[int]string{1: "Create", 2: "Read", 3: "Update", 4: "Delete", 99: "Other"}
	apiStatuses   = map[int]string{1: "Success", 2: "Failure"}
)

// apiExt is the ObservedData's extensions for an API call: the operation, its
// outcome and the call's context, flat, absent keys omitted (§4.15). These are
// the fields the agent reasons on and a verb filters by.
func apiExt(p map[string]any) map[string]any {
	ext := map[string]any{}
	put := func(k, v string) {
		if v != "" {
			ext[k] = v
		}
	}
	put("operation", pathStr(p, "api.operation"))
	put("service", pathStr(p, "api.service.name"))
	put("principal_name", strings.TrimSpace(pathStr(p, "actor.user.name")))
	activityID := pathInt(p, "activity_id")
	if activityID != 0 {
		ext["activity_id"] = activityID
	}
	put("activity", firstNonEmpty(pathStr(p, "activity_name"), apiActivities[activityID]))
	statusID := pathInt(p, "status_id")
	if statusID != 0 {
		ext["status_id"] = statusID
	}
	put("status", firstNonEmpty(pathStr(p, "status"), apiStatuses[statusID]))
	put("error", pathStr(p, "api.response.error"))
	put("error_message", firstNonEmpty(pathStr(p, "api.response.error_message"), pathStr(p, "api.response.message")))
	if code := pathInt(p, "api.response.code"); code != 0 {
		ext["response_code"] = code
	}
	put("request_uid", pathStr(p, "api.request.uid"))
	put("region", pathStr(p, "cloud.region"))
	session := map[string]any{}
	for _, k := range []string{"uid", "issuer", "credential_uid", "is_mfa"} {
		if v, ok := lookupPath(p, "actor.session."+k); ok && v != nil && v != "" {
			session[k] = v
		}
	}
	if len(session) > 0 {
		ext["session"] = session
	}
	put("credential_uid", credentialUID(p))
	put("user_agent", pathStr(p, "http_request.user_agent"))
	put("app_name", pathStr(p, "actor.app_name"))
	if v, ok := lookupPath(p, "resources"); ok {
		list, _ := v.([]any)
		ext["resource_count"] = len(list)
	}
	return ext
}
