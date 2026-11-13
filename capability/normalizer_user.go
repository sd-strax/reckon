package capability

import (
	"strings"

	"github.com/sd-strax/reckon/identity"
)

// Account shaping (03 §7.2, §7.4, adr/0324): which account_type an OCSF user
// object names, and which of its identifiers is the key. The resolver keys
// each type (identity.Resolver.Account); deciding the type and composing the
// login out of the vendor fields is the normalizer's, here, once for every
// class.

// userObject is what an OCSF user object says about the account, placeholders
// already read as absent (identity.Placeholder*).
type userObject struct {
	name, domain    string // name with any "domain\" prefix split into domain
	uid, uidAlt     string // the source's identifiers, as given
	sid             string // uid when it is a SID that names a principal
	email, fullName string
	accountTypeID   int    // user.account.type_id
	accountType     string // user.account.type, lowercased
}

// readUser reads an OCSF user object. A name of the form "domain\sam" with no
// domain field is split, so "CONTOSO\jdoe" and {name: jdoe, domain: CONTOSO}
// are one login.
func readUser(m map[string]any) userObject {
	str := func(path string) string { return strings.TrimSpace(pathStr(m, path)) }
	u := userObject{
		name: str("name"), domain: str("domain"), uid: str("uid"), uidAlt: str("uid_alt"),
		email: str("email_addr"), fullName: str("full_name"),
		accountTypeID: pathInt(m, "account.type_id"),
		accountType:   strings.ToLower(str("account.type")),
	}
	if u.domain == "" {
		if d, n, ok := strings.Cut(u.name, `\`); ok {
			u.domain, u.name = strings.TrimSpace(d), strings.TrimSpace(n)
		}
	}
	if identity.PlaceholderLogin(u.name) {
		u.name = ""
	}
	if identity.PlaceholderDomain(u.domain) {
		u.domain = ""
	}
	if identity.PlaceholderUserID(u.uid) {
		u.uid = ""
	}
	if identity.PlaceholderUserID(u.uidAlt) {
		u.uidAlt = ""
	}
	if identity.IsSID(u.uid) {
		u.sid = u.uid
	}
	return u
}

// principalName is the user's principal name (user@domain) when the record
// carries one: the name when it has an "@", else uid_alt when it does.
func (u userObject) principalName() string {
	switch {
	case strings.Contains(u.name, "@"):
		return u.name
	case strings.Contains(u.uidAlt, "@"):
		return u.uidAlt
	}
	return ""
}

// OCSF account.type_id values the shaping reads (the OCSF Account object).
const (
	ocsfAccountLDAP      = 1
	ocsfAccountWindows   = 2
	ocsfAccountAzureAD   = 6
	ocsfAccountMacOS     = 7
	ocsfAccountLinux     = 9
	ocsfAccountGoogleWks = 15
)

// machineDomains are the "domains" a Windows source names for principals that
// are the machine's own: the account is windows-local, scoped by the host.
var machineDomains = []string{
	"nt authority", "builtin", "nt service", "window manager", "font driver host", "iis apppool",
	"application package authority", "mandatory label",
}

// osFamily reads the OCSF device object the caller names: "windows", "unix"
// or "". The type_id is the OCSF OS enum (100 Windows, 200 Linux, 300 macOS,
// 400-402 the commercial Unixes); the type caption is read when the id is
// absent.
func osFamily(loc at) string {
	p, device := loc.obj, loc.path
	if device == "" {
		return ""
	}
	switch id := pathInt(p, device+".os.type_id"); {
	case id == 100:
		return "windows"
	case id == 200 || id == 300 || (id >= 400 && id <= 402):
		return "unix"
	}
	switch t := strings.ToLower(strings.TrimSpace(pathStr(p, device+".os.type"))); {
	case strings.HasPrefix(t, "windows"):
		return "windows"
	case t == "linux" || t == "macos" || t == "solaris" || t == "aix" || t == "hp-ux":
		return "unix"
	}
	return ""
}

// accountType decides the account_type of the user object u, named by an
// event whose device object (the machine the account acted on) is at device
// and minted as host. The record's other device objects, where an OS may be
// named, are the builder's (deviceObjects). The order is the decision:
//
//  1. cloud: the source is an identity provider, which OCSF says through the
//     account object, account.type_id 6 (Azure AD Account) or 15 (Google
//     Workspace) or the same captions in account.type; or, when the record
//     carries no SID and no domain\sam (a principal name beside a DNS
//     domain is none), the event comes from an identity provider, its
//     product or vendor named exactly (identityProviderSource).
//     Nothing else makes a login cloud: an "@" alone is a principal
//     name (adr/0324).
//  2. ldap: account.type_id 1 (LDAP Account).
//  3. unix: account.type_id 7 or 9 (macOS, Linux Account), or a Unix-family
//     device OS, on a record with no SID: scoped by the host, whatever
//     domain the record names. An explicit Unix signal outweighs every
//     Windows heuristic below but a SID.
//  4. windows-local: a machine's own principal, a well-known SID
//     (identity.WellKnownLocalSID), a machine pseudo-domain (NT AUTHORITY,
//     BUILTIN, NT SERVICE, IIS APPPOOL, ...), or a domain that is the machine
//     itself (a dotless domain that mints the event's host).
//  5. windows-domain: a Windows source, which is a record whose uid is a SID,
//     whose account.type_id is 2 (Windows Account), that names a domain, or
//     whose device OS is Windows, naming a domain, a SID or a principal name.
//  6. windows: a Windows source naming a bare login, no domain and no SID:
//     the weak account.
//  7. windows-domain again for a principal name from a source that names no
//     OS: the reading the seed gives "user@domain" (server seed resolution),
//     so the two meet.
//  8. local: a bare login from a source that names no OS, on a host: scoped
//     by the host as unix is, since keying it globally would make one account
//     of every machine's same-named local account, a merge no edge undoes.
//  9. login: a bare login from a source that names no OS and no host: the
//     weak account on the login alone, which joins nothing by itself.
func (b *resultBuilder) accountType(u userObject, device at, host identity.STIXID) string {
	switch {
	case u.accountTypeID == ocsfAccountAzureAD || u.accountTypeID == ocsfAccountGoogleWks ||
		u.accountType == "azure ad account" || u.accountType == "google workspace" ||
		(u.sid == "" && (u.domain == "" || u.name == "" || strings.Contains(u.name, "@")) && identityProviderSource(b.evt.Payload, b.devices)):
		return identity.AccountCloud
	case u.accountTypeID == ocsfAccountLDAP:
		return identity.AccountLDAP
	}
	os := osFamily(device)
	// An explicit Unix signal, the account type or the device's OS family,
	// outweighs the domain heuristic: a Linux host's account that names a
	// domain is still that host's account. A SID outranks it: a Windows
	// principal on a mis-typed record, or a well-known one, stays Windows.
	if u.sid == "" && (u.accountTypeID == ocsfAccountMacOS || u.accountTypeID == ocsfAccountLinux || os == "unix") {
		return identity.AccountUnix
	}
	if b.machineLocal(u, host) {
		return identity.AccountWindowsLocal
	}
	windows := u.sid != "" || u.accountTypeID == ocsfAccountWindows || u.domain != "" || os == "windows"
	switch {
	case windows && (u.domain != "" || u.sid != "" || u.principalName() != ""):
		return identity.AccountWindowsDomain
	case windows:
		return identity.AccountWindows
	case u.principalName() != "":
		return identity.AccountWindowsDomain
	case host == "":
		return identity.AccountLogin
	}
	return identity.AccountLocal
}

// knownDirectory is what the directories table says of a known directory: its
// slug (directorySlug) and whether it is an identity provider, whose sign-in
// records name cloud accounts (identityProviderSource).
type knownDirectory struct {
	slug string
	idp  bool
}

// directories maps known directory products and vendors, lowercased and
// named whole, to what they are: the identity providers' sign-in products
// and vendors (entra, okta, google), an on-premises Active Directory (ad),
// a personnel system (workday). Named whole, so a managed domain service or
// a SIEM that merely names a provider is not one.
var directories = map[string]knownDirectory{
	"microsoft entra id": {"entra", true}, "entra id": {"entra", true},
	"azure active directory": {"entra", true}, "azure ad": {"entra", true},
	"okta": {"okta", true}, "okta identity engine": {"okta", true}, "okta workforce identity cloud": {"okta", true},
	"okta system log":  {"okta", true},
	"google workspace": {"google", true}, "google cloud identity": {"google", true},
	"active directory": {"ad", false}, "active directory domain services": {"ad", false},
	"workday": {"workday", false},
}

// knownDirectoryOf looks the event's metadata.product up in directories: the
// product name, matched whole, else with no product name the vendor name
// (byVendor). A product name the table does not hold is its own directory:
// the vendor is not consulted (directorySlug slugifies the product).
func knownDirectoryOf(p map[string]any) (d knownDirectory, byVendor, ok bool) {
	product := strings.ToLower(strings.TrimSpace(pathStr(p, "metadata.product.name")))
	if product != "" {
		d, ok = directories[product]
		return d, false, ok
	}
	d, ok = directories[strings.ToLower(strings.TrimSpace(pathStr(p, "metadata.product.vendor_name")))]
	return d, true, ok
}

// namesDeviceOS reports whether any of the device objects the caller names
// says what OS it runs.
func namesDeviceOS(devices []at) bool {
	for _, dev := range devices {
		if dev.path == "" {
			continue
		}
		if pathInt(dev.obj, dev.path+".os.type_id") != 0 || strings.TrimSpace(pathStr(dev.obj, dev.path+".os.type")) != "" || strings.TrimSpace(pathStr(dev.obj, dev.path+".os.name")) != "" {
			return true
		}
	}
	return false
}

// identityProviderSource reports whether the event comes from an identity
// provider (directories, knownDirectoryOf): "Okta System Log" is one,
// "Microsoft Entra Domain Services" is not. A vendor name alone marks one
// only when none of the class's device objects names an OS: a vendor's agent
// on a machine is no sign-in. devices are the device objects the event's
// class carries, which the calling normalizer names (adr/0356).
func identityProviderSource(p map[string]any, devices []at) bool {
	d, byVendor, ok := knownDirectoryOf(p)
	if !ok || !d.idp {
		return false
	}
	return !byVendor || !namesDeviceOS(devices)
}

// machineLocal reports whether u names one of the machine's own principals:
// a well-known SID, a machine pseudo-domain, or a domain that is the host.
func (b *resultBuilder) machineLocal(u userObject, host identity.STIXID) bool {
	if identity.WellKnownLocalSID(u.sid) {
		return true
	}
	d := strings.ToLower(u.domain)
	for _, m := range machineDomains {
		if d == m {
			return true
		}
	}
	return host != "" && d != "" && !strings.Contains(d, ".") && b.r.Host(identity.HostIdentity{Hostname: d}) == host
}

// userAccount extracts the user-account the OCSF user object at prefix of
// the event (e.g. "actor.user") names; device is the prefix of the device
// object the account acted on. See userAccountAt.
func (b *resultBuilder) userAccount(prefix, device string, host identity.STIXID) identity.STIXID {
	return b.userAccountAt(b.top(prefix), b.top(device), host)
}

// userAccountAt extracts the user-account an OCSF user object names, shaped
// per adr/0324 (03 §7.2, §7.4). device is the device object the account acted
// on and host the x-host the normalizer minted from it ("" for none): a
// host-scoped account (windows-local, unix, local) is keyed under it, and the
// device's OS is one of the signals the type is read from (accountType).
//
// The record's strongest identifier mints the account, in object_refs: for a
// domain account the SID, else the principal name, else "domain\sam"; for a
// cloud or LDAP account the provider's id, else the sign-in name. Each lesser
// identifier the same record carries (the "domain\sam" and the principal name
// beside a SID, the sam beside a principal name, the sign-in name beside a
// provider id, a well-known principal's name beside its SID) mints the
// account it alone would have, outside object_refs, with an aliases edge
// from it to the strong account, DIRECT, the record's provenance: the source
// said in this record that the login is that account, as the host rule does
// for an agent id (adr/0211). It returns "" when the object names nothing.
func (b *resultBuilder) userAccountAt(user, device at, host identity.STIXID) identity.STIXID {
	m := objectAt(user.obj, user.path)
	if m == nil {
		return ""
	}
	u := readUser(m)
	ids := b.account(u, b.accountType(u, device, host), host, true)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// accountKey is one identifier of a user object as the account it mints, with
// the login property that account carries.
type accountKey struct {
	id    identity.STIXID
	login string
}

// account mints the accounts u names under typ and returns their ids,
// strongest first. inRefs puts the strong account in object_refs (the event
// named it); the lesser ones are always outside them, aliased to it.
func (b *resultBuilder) account(u userObject, typ string, host identity.STIXID, inRefs bool) []identity.STIXID {
	keys := b.accountKeys(u, typ, host)
	if len(keys) == 0 {
		return nil
	}
	ids := make([]identity.STIXID, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.id)
	}
	strong := keys[0]
	props := b.accountProps(u, typ, strong.login)
	if typ == identity.AccountWindows || typ == identity.AccountLogin {
		props["weak"] = true
	}
	if inRefs {
		b.addSCO(strong.id, identity.TypeUserAccount, props)
	} else {
		b.addLinkedSCO(strong.id, identity.TypeUserAccount, props)
	}
	for _, k := range keys[1:] {
		lesser := map[string]any{"account_login": k.login, "account_type": typ}
		if u.domain != "" {
			lesser["domain"] = u.domain
		}
		b.addLinkedSCO(k.id, identity.TypeUserAccount, lesser)
		b.addRel(RelAliases, k.id, strong.id)
	}
	return ids
}

// accountKeys lists the accounts u's identifiers mint under typ, strongest
// first, each minted once (an identifier that resolves to an id already
// listed is not repeated).
func (b *resultBuilder) accountKeys(u userObject, typ string, host identity.STIXID) []accountKey {
	var out []accountKey
	add := func(a identity.AccountIdentity, login string) {
		a.Type, a.HostRef = typ, host
		id := b.r.Account(a)
		if id == "" {
			return
		}
		for _, k := range out {
			if k.id == id {
				return
			}
		}
		out = append(out, accountKey{id: id, login: login})
	}
	sam := ""
	if u.domain != "" && u.name != "" && !strings.Contains(u.name, "@") {
		sam = u.domain + `\` + u.name
	}
	login := firstNonEmpty(sam, u.principalName(), u.name)
	switch typ {
	case identity.AccountWindowsDomain:
		if u.sid != "" {
			add(identity.AccountIdentity{UserID: u.sid}, login)
		}
		if pn := u.principalName(); pn != "" {
			add(identity.AccountIdentity{Login: pn}, pn)
		}
		if sam != "" {
			add(identity.AccountIdentity{Login: sam}, sam)
		}
	case identity.AccountWindowsLocal:
		if identity.WellKnownLocalSID(u.sid) {
			add(identity.AccountIdentity{UserID: u.sid}, login)
		}
		if u.name != "" {
			add(identity.AccountIdentity{Login: u.name}, login)
		}
	case identity.AccountCloud, identity.AccountLDAP:
		if u.uid != "" {
			add(identity.AccountIdentity{UserID: u.uid}, u.name)
		}
		if u.name != "" {
			add(identity.AccountIdentity{Login: u.name}, u.name)
		}
	default: // windows, unix, local, login: the bare login
		if u.name != "" {
			add(identity.AccountIdentity{Login: u.name}, u.name)
		}
	}
	return out
}

// accountProps are the strong account's properties: the login it is known
// by, the source's identifier keyed by the tool that gave it, the type, and
// what else the object says (principal name, mail, display name, domain,
// uid_alt). A cloud account also carries its provider, the directory slug
// (directorySlug).
func (b *resultBuilder) accountProps(u userObject, typ, login string) map[string]any {
	// The identifier the record gives, or the SID the account is keyed on when
	// the record carries it apart from uid (a directory's on-premises SID), so
	// a lookup or a seen-as match by the key finds the account.
	uid := firstNonEmpty(u.uid, u.sid)
	props := map[string]any{"account_login": login, "user_id": uid, "account_type": typ}
	put := func(k, v string) {
		if v != "" {
			props[k] = v
		}
	}
	put("principal_name", u.principalName())
	put("email", u.email)
	put("display_name", u.fullName)
	put("domain", u.domain)
	put("uid_alt", u.uidAlt)
	if uid != "" && b.evt.SourceTool != "" {
		props["source_ids"] = map[string]any{b.evt.SourceTool: uid}
	}
	if typ == identity.AccountCloud {
		put("provider", directorySlug(b.evt.Payload, b.evt.SourceTool))
	}
	return props
}

// directorySlug names the directory or identity provider an event comes
// from, the scope a person's id is unique in (identity.Resolver.Person): a
// known product or vendor (directories, the product name looked up first,
// then the vendor name) by its slug: "entra", "okta", "google", "ad",
// "workday"; any other source by its full product name, so "Microsoft Entra
// Domain Services" is its own directory and never "entra"; with no product
// name, the vendor name, else the event's source tool; lowercased with
// spaces as dashes. The slug depends on the record alone, so two installs
// of one source scope its persons alike.
func directorySlug(p map[string]any, sourceTool string) string {
	if d, _, ok := knownDirectoryOf(p); ok {
		return d.slug
	}
	product := strings.ToLower(strings.TrimSpace(pathStr(p, "metadata.product.name")))
	s := strings.ToLower(firstNonEmpty(product, strings.TrimSpace(pathStr(p, "metadata.product.vendor_name")), strings.TrimSpace(sourceTool)))
	return strings.Join(strings.Fields(s), "-")
}
