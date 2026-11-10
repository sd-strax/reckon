package capability

import (
	"strings"

	"github.com/sd-strax/reckon/identity"
)

// cloudInventoryNormalizer handles OCSF Cloud Resources Inventory Info
// (class_uid 5023) → STIX (§4.18): a configuration snapshot of cloud
// resources. Identity follows api_activity exactly, so a snapshot of a
// resource mints the id the control-plane calls on it mint: the account is an
// x-cloud-account from cloud.account, each resources[] entry an
// x-cloud-resource by the same rules, a resource named under the account
// belongs-to it. A resource's owner (an OCSF User) is a user-account by the
// principal shaping when it carries an identifier. The configuration rides
// the one ObservedData. With no cloud.provider nothing cloud-shaped is
// minted, as for 6003.
type cloudInventoryNormalizer struct{ r *identity.Resolver }

func (*cloudInventoryNormalizer) ClassUID() int { return 5023 }
func (*cloudInventoryNormalizer) Version() int  { return 1 }

func (n *cloudInventoryNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "cloud_inventory", n.Version())
	provider := providerSlug(pathStr(p, "cloud.provider"))
	if provider == "" {
		return b.finish(inventoryExt(p)), nil
	}

	var account identity.STIXID
	if uid := pathScalar(p, "cloud.account.uid"); uid != "" {
		account = b.addAccount(provider, uid, "cloud.account")
	} else if uid := pathScalar(p, "cloud.project_uid"); uid != "" {
		account = b.addAccount(provider, uid, "")
	}
	for _, res := range b.cloudResources(provider, account) {
		if res.rule == identity.CloudResourceByName {
			b.addRel(RelBelongsTo, res.id, account)
		}
	}
	for _, item := range inventoryResources(p) {
		owner, _ := item["owner"].(map[string]any)
		if id := ownerID(owner); id != "" {
			b.cloudUser(provider, id, owner, false, "")
		}
	}
	return b.finish(inventoryExt(p)), nil
}

// inventoryResources is the payload's resources[] entries that are objects.
func inventoryResources(p map[string]any) []map[string]any {
	v, _ := lookupPath(p, "resources")
	list, _ := v.([]any)
	var out []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// ownerID is a resource owner's durable identifier: its uid, else uid_alt.
func ownerID(owner map[string]any) string {
	if owner == nil {
		return ""
	}
	return firstNonEmpty(scalarString(owner["uid"]), scalarString(owner["uid_alt"]))
}

// inventoryActivities are the OCSF 5023 activity captions.
var inventoryActivities = map[int]string{1: "Log", 2: "Collect"}

// inventoryExt is the ObservedData's extensions for a snapshot: the activity,
// region and resource count, and the first resource's configuration as the
// snapshot reports it, absent keys omitted (§4.18). created_time and
// modified_time arrive at 1.7.0 and are read when present; the schema version
// itself is never copied (§4.16).
func inventoryExt(p map[string]any) map[string]any {
	ext := map[string]any{}
	put := func(k, v string) {
		if v != "" {
			ext[k] = v
		}
	}
	putAny := func(k string, v any) {
		switch x := v.(type) {
		case nil:
			return
		case string:
			put(k, strings.TrimSpace(x))
			return
		case []any:
			if len(x) == 0 {
				return
			}
		case map[string]any:
			if len(x) == 0 {
				return
			}
		}
		ext[k] = v
	}
	activityID := pathInt(p, "activity_id")
	if activityID != 0 {
		ext["activity_id"] = activityID
	}
	put("activity", firstNonEmpty(pathStr(p, "activity_name"), inventoryActivities[activityID]))
	put("region", pathStr(p, "cloud.region"))
	resources := inventoryResources(p)
	if v, ok := lookupPath(p, "resources"); ok {
		list, _ := v.([]any)
		ext["resource_count"] = len(list)
	}
	if len(resources) == 0 {
		return ext
	}
	first := resources[0]
	put("resource_uid", scalarString(first["uid"]))
	put("resource_name", strings.TrimSpace(toString(first["name"])))
	put("type", toString(first["type"]))
	putAny("configuration", first["data"])
	putAny("labels", first["labels"])
	putAny("tags", first["tags"])
	put("criticality", toString(first["criticality"]))
	putAny("created_time", first["created_time"])
	putAny("modified_time", first["modified_time"])
	if owner, _ := first["owner"].(map[string]any); owner != nil {
		put("owner_name", firstNonEmpty(strings.TrimSpace(toString(owner["name"])), strings.TrimSpace(toString(owner["full_name"]))))
	}
	return ext
}
