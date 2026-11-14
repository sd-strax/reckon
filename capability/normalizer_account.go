package capability

import "github.com/sd-strax/reckon/identity"

// The three OCSF account classes one function shapes (§4.9, adr/0356).
const (
	classAccountChange   = 3001 // Account Change: user, user_result
	classUserAccess      = 3005 // User Access Management: user, privileges[]
	classGroupManagement = 3006 // Group Management: user, group, privileges[]
)

// accountNormalizer handles the OCSF account classes → STIX (§4.9):
// account_change (3001), user_access (3005) and group_management (3006). It is
// registered once per class and reads only that class's fields. Every class
// gives the target user, the initiator and the host. group_management also
// gives the x-group and a member-of-group relationship. The activity, and the
// privileges of the two classes that carry them, ride on the ObservedData.
type accountNormalizer struct {
	r     *identity.Resolver
	class int
}

func (n *accountNormalizer) ClassUID() int { return n.class }

// 2: host identity, adr/0211; 3: account identity, adr/0324
// 4: OCSF classes and fields, adr/0356
func (*accountNormalizer) Version() int { return 4 }

// name is the OCSF name of the class this instance is registered for.
func (n *accountNormalizer) name() string {
	switch n.class {
	case classUserAccess:
		return "user_access"
	case classGroupManagement:
		return "group_management"
	}
	return "account_change"
}

func (n *accountNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, n.name(), n.Version())
	b.deviceObjects("device", "src_endpoint")

	host := b.host("device")
	target := b.userAccount("user", "device", host)
	b.userAccount("actor.user", "device", host) // initiator

	if n.class == classGroupManagement {
		// A group with no name is named by its uid, as user.groups[] is
		// (§4.19).
		if group := firstNonEmpty(pathStr(p, "group.name"), pathStr(p, "group.uid")); group != "" {
			directory := pathStr(p, "group.domain")
			groupID := b.addSCO(n.r.Group(directory, group), identity.TypeGroup, map[string]any{
				"group_name": group,
				"directory":  directory,
			})
			if target != "" {
				b.addRel(RelMemberOfGroup, target, groupID)
			}
		}
	}

	ext := activityExt(p)
	if n.class == classUserAccess || n.class == classGroupManagement {
		if v, ok := lookupPath(p, "privileges"); ok {
			if privs := stringSlice(v); len(privs) > 0 {
				if ext == nil {
					ext = map[string]any{}
				}
				ext["privileges"] = privs
			}
		}
	}

	return b.finish(ext), nil
}
