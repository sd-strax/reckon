package capability

import (
	"strings"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/stix"
)

// userInventoryNormalizer handles OCSF user_inventory (class_uid 5003) →
// STIX (03 §4.19, adr/0324): a directory's user object, the one record that
// names a person. It mints the person, a STIX identity with identity_class
// individual keyed by (directory, the directory's id), the accounts the
// record names, each by the account rule with the aliases one record
// justifies, a belongs-to from each account to the person, and the account's
// group memberships. It is the only normalizer that mints a person; every
// other class names accounts only.
type userInventoryNormalizer struct{ r *identity.Resolver }

func (*userInventoryNormalizer) ClassUID() int { return 5003 }

// Version starts at 2: a 5003 record normalized before this normalizer
// existed went through the opaque default at version 1, and the ObservedData
// id carries the version but not the normalizer's name (§4.13), as for 5001.
//
// 3: OCSF read gaps, adr/0359
// 4: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; the person out of
// object_refs and named in person_ref, and only with a name, adr/0357
func (*userInventoryNormalizer) Version() int { return 4 }

// accountlessDirectories describe people, not sign-ins: a personnel system's
// worker id names a person and no account.
var accountlessDirectories = []string{"workday"}

// onPremisesDirectories key their own accounts as domain accounts: an
// on-premises directory's user is the windows-domain account, not a cloud
// sign-in.
var onPremisesDirectories = []string{"ad"}

func (n *userInventoryNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "user_inventory", n.Version())
	raw, _ := lookupPath(p, "user")
	m, ok := raw.(map[string]any)
	if !ok {
		return b.finish(activityExt(p)), nil
	}
	u := readUser(m)
	directory := directorySlug(p, evt.SourceTool)
	directoryID := u.uid
	str := func(path string) string { return strings.TrimSpace(pathStr(p, path)) }

	// The person, from the directory's own id. An identity is an SDO, and
	// observed-data's object_refs may list only SCOs and relationships, so
	// the person stays out of them. The observation lists the belongs-to
	// relationships instead, and the person is their target (adr/0357).
	// STIX requires an identity to have a name. A record that gives the
	// person no name gets no person, and so no belongs-to to one. Its
	// accounts are still written (adr/0357).
	person := n.r.Person(directory, directoryID)
	if person != "" {
		props := n.personProps(p, u, directory, directoryID)
		if props["name"] == nil {
			person = ""
		} else {
			b.addLinkedSCO(person, identity.TypeIdentity, props)
		}
	}

	// The domain family's SID: user.uid when it is one (an on-premises
	// directory keyed by SID), else the on-premises SID a cloud directory
	// carries for a synced user (uid_alt when it has the SID form, else the
	// unmapped on_premises_security_identifier). The sAMAccountName and its
	// NetBIOS domain are the unmapped on_premises_sam_account_name and
	// on_premises_domain_name, a placeholder domain read as none and the
	// domain falling back to user.domain; an on-premises directory's own
	// record carries the sam as user.name.
	sid := firstNonEmpty(u.sid, sidOf(u.uidAlt), sidOf(str("unmapped.on_premises_security_identifier")))
	sam := str("unmapped.on_premises_sam_account_name")
	onPremDomain := str("unmapped.on_premises_domain_name")
	if identity.PlaceholderDomain(onPremDomain) {
		onPremDomain = ""
	}
	domain := firstNonEmpty(onPremDomain, u.domain)
	onPrem := containsFold(onPremisesDirectories, directory)
	if onPrem && sam == "" && !strings.Contains(u.name, "@") {
		sam = u.name
	}
	principal := u.principalName()

	var accounts []identity.STIXID
	var member identity.STIXID // the account groups[] are memberships of
	if !containsFold(accountlessDirectories, directory) {
		// The domain account family: the SID, the principal name and
		// domain\sam, strongest first, the lesser aliased to the strongest.
		dom := userObject{name: firstNonEmpty(sam, principal), sid: sid, uidAlt: principal, email: u.email, fullName: u.fullName}
		if sam != "" {
			dom.domain = domain
		}
		if identity.PlaceholderLogin(dom.name) {
			dom.name = ""
		}
		domIDs := b.account(dom, identity.AccountWindowsDomain, "", true)
		accounts = append(accounts, domIDs...)
		// The directory's own sign-in account, keyed on its id, with the
		// sign-in name aliased to it; an on-premises directory has none
		// beyond the domain account.
		if !onPrem && directoryID != "" {
			cloud := userObject{name: principal, uid: directoryID, email: u.email, fullName: u.fullName}
			cloudIDs := b.account(cloud, identity.AccountCloud, "", true)
			accounts = append(accounts, cloudIDs...)
			if len(cloudIDs) > 0 {
				member = cloudIDs[0]
			}
		}
		if member == "" && len(domIDs) > 0 {
			member = domIDs[0]
		}
	}
	for _, a := range accounts {
		b.addRefRel(RelBelongsTo, a, person)
	}

	// Group memberships ride the account, as account_change records them
	// (§4.9); a person's groups are the union over its accounts at read time.
	if member != "" {
		for _, g := range groupList(m) {
			gid := b.addSCO(n.r.Group(firstNonEmpty(g.domain, directory), g.name), identity.TypeGroup, map[string]any{
				"group_name": g.name, "directory": firstNonEmpty(g.domain, directory),
			})
			b.addRel(RelMemberOfGroup, member, gid)
		}
	}
	// The observation names the person it describes in person_ref. An
	// identity is an SDO, which object_refs cannot list, and a directory with
	// no accounts, such as an HR system, has no belongs-to to reach the person
	// through. Readers that ask which records are about a person read it
	// beside object_refs (adr/0324, adr/0357).
	ext := activityExt(p)
	if person != "" {
		if ext == nil {
			ext = map[string]any{}
		}
		ext[stix.ObservationPersonRef.Name()] = string(person)
	}
	return b.finish(ext), nil
}

// personProps is what the directory record says about the person: the
// display name (also the STIX identity's name), mail, principal name,
// department (org.ou_name), title (ldap_person.job_title), the manager as a
// person ref when the record names the manager by directory id
// (ldap_person.manager.uid), whether the account is enabled (unmapped
// account_enabled), the employee id (ldap_person.employee_uid), and the
// directory and its id.
func (n *userInventoryNormalizer) personProps(p map[string]any, u userObject, directory, directoryID string) map[string]any {
	str := func(path string) string { return strings.TrimSpace(pathStr(p, path)) }
	props := map[string]any{"identity_class": "individual", "directory": directory, "directory_id": directoryID}
	put := func(k, v string) {
		if v != "" {
			props[k] = v
		}
	}
	display := firstNonEmpty(u.fullName, u.principalName(), u.name)
	put("name", display)
	put("display_name", display)
	mail := u.email
	if mail == "" {
		if v, ok := lookupPath(p, "user.ldap_person.email_addrs"); ok {
			if list, ok := v.([]any); ok && len(list) > 0 {
				mail = strings.TrimSpace(toString(list[0]))
			}
		}
	}
	put("mail", mail)
	put("principal_name", u.principalName())
	put("department", str("user.org.ou_name"))
	put("organization", str("user.org.name"))
	put("title", str("user.ldap_person.job_title"))
	put("employee_id", str("user.ldap_person.employee_uid"))
	if mgr := n.r.Person(directory, str("user.ldap_person.manager.uid")); mgr != "" {
		props["manager_ref"] = string(mgr)
	}
	if v, ok := lookupPath(p, "unmapped.account_enabled"); ok {
		if enabled, ok := v.(bool); ok {
			props["enabled"] = enabled
		}
	}
	return props
}

// sidOf answers v when it is a SID that names a principal, else "".
func sidOf(v string) string {
	v = strings.TrimSpace(v)
	if identity.IsSID(v) && !identity.PlaceholderSID(v) {
		return v
	}
	return ""
}

func containsFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

// inventoryGroup is one entry of an OCSF user's groups[].
type inventoryGroup struct{ name, domain string }

// groupList reads user.groups[] as named groups: an entry with neither a
// name nor a uid is dropped, and a uid stands in for a missing name.
func groupList(user map[string]any) []inventoryGroup {
	v, ok := fieldOf(user, "groups").([]any)
	if !ok {
		return nil
	}
	var out []inventoryGroup
	for _, item := range v {
		g, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := firstNonEmpty(strings.TrimSpace(toString(fieldOf(g, "name"))), strings.TrimSpace(toString(fieldOf(g, "uid"))))
		if name == "" {
			continue
		}
		out = append(out, inventoryGroup{name: name, domain: strings.TrimSpace(toString(fieldOf(g, "domain")))})
	}
	return out
}
