package capability

import (
	"math"
	"strings"
	"time"

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

// Version starts at 2: a 5023 snapshot normalized before this normalizer
// existed went through the opaque default at version 1, and the
// ObservedData id carries the version but not the normalizer's name, so a
// version 1 here would mint the same id and keep the entity-less observation.
// Version 3: created_time and modified_time reach the extensions as RFC 3339
// strings whether the payload carries epoch milliseconds (OCSF timestamp_t,
// what a live source emits) or a string; a shaping change is a
// re-normalization (§4.13).
func (*cloudInventoryNormalizer) Version() int { return 3 }

func (n *cloudInventoryNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "cloud_resources_inventory_info", n.Version())
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
		owner, _ := fieldOf(item, "owner").(map[string]any)
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
	return firstNonEmpty(scalarString(fieldOf(owner, "uid")), scalarString(fieldOf(owner, "uid_alt")))
}

// inventoryActivities are the OCSF 5023 activity captions.
var inventoryActivities = map[int]string{1: "Log", 2: "Collect"}

// inventoryExt is the ObservedData's extensions for a snapshot: the activity,
// region and resource count, and the first resource's configuration as the
// snapshot reports it, absent keys omitted (§4.18). created_time and
// modified_time arrive at 1.7.0 and are read when present, as RFC 3339; the schema version
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
	put("resource_uid", scalarString(fieldOf(first, "uid")))
	put("resource_name", strings.TrimSpace(toString(fieldOf(first, "name"))))
	put("type", toString(fieldOf(first, "type")))
	putAny("configuration", fieldOf(first, "data"))
	putAny("labels", fieldOf(first, "labels"))
	putAny("tags", fieldOf(first, "tags"))
	put("criticality", toString(fieldOf(first, "criticality")))
	put("created_time", ocsfTimestamp(fieldOf(first, "created_time")))
	put("modified_time", ocsfTimestamp(fieldOf(first, "modified_time")))
	if owner, _ := fieldOf(first, "owner").(map[string]any); owner != nil {
		put("owner_name", firstNonEmpty(strings.TrimSpace(toString(fieldOf(owner, "name"))), strings.TrimSpace(toString(fieldOf(owner, "full_name")))))
	}
	return ext
}

// ocsfTimestamp renders an OCSF timestamp as RFC 3339 UTC: a number is epoch
// milliseconds (timestamp_t), converted when it is an exact integer below
// 2^53 (beyond that a JSON number has already been rounded); a string is
// kept as given; anything else is "". capability/ocsfview holds a copy of
// this rule for the event view (it cannot import this package).
func ocsfTimestamp(v any) string {
	var f float64
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		f = x
	case int64:
		f = float64(x)
	case int:
		f = float64(x)
	default:
		return ""
	}
	if f != math.Trunc(f) || math.Abs(f) >= 1<<53 {
		return ""
	}
	return time.UnixMilli(int64(f)).UTC().Format(time.RFC3339Nano)
}
