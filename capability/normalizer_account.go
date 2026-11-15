package capability

import (
	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsf"
)

// The three OCSF account classes one function shapes (§4.9, adr/0356).
const (
	classAccountChange   = 3001 // Account Change: user, user_result
	classUserAccess      = 3005 // User Access Management: user, privileges[]
	classGroupManagement = 3006 // Group Management: users[] or user, group, privileges[]
)

// accountNormalizer handles the OCSF account classes → STIX (§4.9):
// account_change (3001), user_access (3005) and group_management (3006). It is
// registered once per class and reads only that class's fields. Every class
// gives the target user, the initiator and the host. group_management can
// name several targets (targets), and also gives the x-group and a
// member-of-group relationship from each target. The activity, and the
// privileges of the two classes that carry them, ride on the ObservedData.
type accountNormalizer struct {
	r     *identity.Resolver
	class int
}

func (n *accountNormalizer) ClassUID() int { return n.class }

// 2: host identity, adr/0211; 3: account identity, adr/0324
// 4: OCSF classes and fields, adr/0356
// 5: OCSF read gaps, adr/0359
func (*accountNormalizer) Version() int { return 5 }

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
	targets := n.targets(b, host)
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
			for _, target := range targets {
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

// targets adds the accounts the event is about and returns their ids. On
// group_management every entry of users[] (OCSF 1.9.0) that names a user
// (ocsf.NamesUser) is a target. OCSF 1.9.0 deprecates user there, and it is
// read when no entry of users[] names a user (adr/0359). The event view
// decides by the same rule. The other two classes have no users[] and read
// user alone.
func (n *accountNormalizer) targets(b *resultBuilder, host identity.STIXID) []identity.STIXID {
	// The class is checked first: 3001 and 3005 have no users[] to read.
	if n.class != classGroupManagement {
		return b.oneAccount("user", host)
	}
	var members []map[string]any
	for _, u := range objectsAt(b.evt.Payload, "users") {
		if ocsf.NamesUser(u, fieldOf) {
			members = append(members, u)
		}
	}
	if len(members) == 0 {
		return b.oneAccount("user", host)
	}
	var out []identity.STIXID
	for _, u := range members {
		if id := b.userAccountOf(u, b.top("device"), host); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// oneAccount is the user-account the user object at prefix names, as a list
// of none or one.
func (b *resultBuilder) oneAccount(prefix string, host identity.STIXID) []identity.STIXID {
	if id := b.userAccount(prefix, "device", host); id != "" {
		return []identity.STIXID{id}
	}
	return nil
}
