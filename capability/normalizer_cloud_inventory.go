package capability

import (
	"strings"
	"time"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsf"
	"github.com/sd-strax/reckon/stix"
)

// cloudInventoryNormalizer handles OCSF Cloud Resources Inventory Info
// (class_uid 5023) → STIX (§4.18): a configuration snapshot of cloud
// resources. Identity follows api_activity exactly, so a snapshot of a
// resource mints the id the control-plane calls on it mint: the account is an
// reckon-cloud-account from cloud.account, each resources[] entry an
// reckon-cloud-resource by the same rules, a resource named under the account
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
// strings; a shaping change is a re-normalization (§4.13).
// Version 4: they are read as OCSF writes them, epoch milliseconds, else the
// _dt twin (adr/0356).
// 5: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; resource_type,
// configuration as any JSON, an out-of-range resource time left out,
// is_service_account only when known, adr/0357
// 6: a cloud account's type named by OCSF's caption, an unknown one
// Other, adr/0367
// 7: the account minted for each user object recorded in user_accounts,
// adr/0398
func (*cloudInventoryNormalizer) Version() int { return 7 }

func (n *cloudInventoryNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "cloud_resources_inventory_info", n.Version())
	provider := providerSlug(pathStr(p, "cloud.provider"))
	if provider == "" {
		return b.finish(inventoryExt(p)), nil
	}

	var account identity.STIXID
	if uid := pathScalar(p, "cloud.account.uid"); uid != "" {
		account = b.addAccount(provider, uid, b.top("cloud.account"), true)
	} else if uid := pathScalar(p, "cloud.project_uid"); uid != "" {
		account = b.addAccount(provider, uid, at{}, true)
	}
	b.cloudResourcesOf(provider, account)
	for _, item := range objectEntries(p, "resources") {
		owner, _ := fieldOf(item.obj, "owner").(map[string]any)
		if id := ownerID(owner); id != "" {
			b.recordAccount(ocsf.PlaceIn(item.place, "owner"), b.cloudUser(provider, id, owner, false, ""))
		}
	}
	return b.finish(inventoryExt(p)), nil
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
// modified_time arrive at 1.5.0 and are read when present, as RFC 3339; the
// schema version itself is never copied (§4.16).
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
	resources := objectsAt(p, "resources")
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
	put("resource_type", toString(fieldOf(first, "type")))
	putAny("configuration", fieldOf(first, "data"))
	putAny("labels", fieldOf(first, "labels"))
	putAny("tags", fieldOf(first, "tags"))
	put("criticality", toString(fieldOf(first, "criticality")))
	put("created_time", ocsfTimestamp(first, "created_time"))
	put("modified_time", ocsfTimestamp(first, "modified_time"))
	if owner, _ := fieldOf(first, "owner").(map[string]any); owner != nil {
		put("owner_name", firstNonEmpty(strings.TrimSpace(toString(fieldOf(owner, "name"))), strings.TrimSpace(toString(fieldOf(owner, "full_name")))))
	}
	return ext
}

// ocsfTimestamp renders the OCSF timestamp at key of obj as RFC 3339 UTC, or
// "" when there is none. The field holds epoch milliseconds, and its _dt twin
// an RFC 3339 string; the number is read first (see ocsfTime).
// capability/ocsfview holds a copy of this rule for the event view (it cannot
// import this package).
func ocsfTimestamp(obj map[string]any, key string) string {
	t := ocsfTime(obj, key)
	// An optional time whose year a STIX timestamp cannot write, outside
	// 0000 to 9999, is left out, and the object is kept. Only the event's own
	// time refuses the record (adr/0357).
	if t.IsZero() || !stix.InTimestampRange(t) {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}
