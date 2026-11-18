package ocsf

import (
	"strings"

	"github.com/sd-strax/reckon/identity"
)

// UserName reads the name and domain of an OCSF user object, placeholders
// read as absent. Both are trimmed. A placeholder domain
// (identity.PlaceholderDomain) is blanked first. A name of the form
// "domain\sam" with no domain left is then split, so "CONTOSO\jdoe",
// {name: "CONTOSO\jdoe", domain: "-"} and {name: jdoe, domain: CONTOSO} are
// one login. A name whose part before the backslash holds "=" or "," is a
// distinguished name and is not split. After the split, a placeholder name (identity.PlaceholderLogin)
// or domain is "". ReadUser reads a user object's name and domain by it.
//
// field reads one key of the object. Callers pass their own reader, as they
// do to NamesUser. A key the reader returns as a non-string gives no value.
func UserName(user map[string]any, field func(obj map[string]any, key string) any) (name, domain string) {
	name, _ = field(user, "name").(string)
	domain, _ = field(user, "domain").(string)
	name, domain = strings.TrimSpace(name), strings.TrimSpace(domain)
	if identity.PlaceholderDomain(domain) {
		domain = ""
	}
	if domain == "" {
		// A distinguished name (CN=Doe\, John,CN=Users,...), as Windows
		// 4728/4732 report a member, escapes with a backslash. The part
		// before it holds "=" or ",", which a NetBIOS domain never does, so
		// it is not split.
		if d, n, ok := strings.Cut(name, `\`); ok && !strings.ContainsAny(d, "=,") {
			domain, name = strings.TrimSpace(d), strings.TrimSpace(n)
		}
	}
	if identity.PlaceholderLogin(name) {
		name = ""
	}
	if identity.PlaceholderDomain(domain) {
		domain = ""
	}
	return name, domain
}

// User is what reckon reads from an OCSF user object (adr/0367). The
// normalizer, the event view and an account's "seen as" line all read it
// through ReadUser, so they agree on who the object names.
type User struct {
	Name, Domain string // as UserName reads them
	// UID and UIDAlt are the ids, trimmed. An id that is not a string, or
	// is a placeholder (identity.PlaceholderUserID), is "".
	UID, UIDAlt string
	// Login is the login the account rule stores (UserLogin).
	Login string
	// PrincipalName is the user@domain the object carries, or ""
	// (UserPrincipalName).
	PrincipalName string
	// AccountTypeID is the account type, read by AccountTypeID.
	AccountTypeID int
}

// ReadUser reads an OCSF user object. field reads one key of an object, as
// it does for UserName. The account type is read from the user's account
// object, which field reads too.
func ReadUser(user map[string]any, field func(obj map[string]any, key string) any) User {
	if user == nil {
		return User{}
	}
	u := User{UID: userID(field(user, "uid")), UIDAlt: userID(field(user, "uid_alt"))}
	u.Name, u.Domain = UserName(user, field)
	u.Login = UserLogin(u.Name, u.Domain)
	u.PrincipalName = UserPrincipalName(u.Name, u.UIDAlt)
	if account, _ := field(user, "account").(map[string]any); account != nil {
		caption, _ := field(account, "type").(string)
		u.AccountTypeID = AccountTypeID(field(account, "type_id"), caption)
	}
	return u
}

// userID is an id of a user object: a string, trimmed, with a placeholder
// read as "".
func userID(v any) string {
	s, _ := v.(string)
	if s = strings.TrimSpace(s); identity.PlaceholderUserID(s) {
		return ""
	}
	return s
}

// UserLogin is the login the account rule stores for a name and domain:
// "domain\name" when both are present and the name holds no "@", else the
// name. An "@" name is a principal name, which carries its own domain.
func UserLogin(name, domain string) string {
	if domain != "" && name != "" && !strings.Contains(name, "@") {
		return domain + `\` + name
	}
	return name
}

// UserPrincipalName is the principal name (user@domain) a user object
// carries: the name when it holds an "@", else the uid_alt when it does,
// else "".
func UserPrincipalName(name, uidAlt string) string {
	switch {
	case strings.Contains(name, "@"):
		return name
	case strings.Contains(uidAlt, "@"):
		return uidAlt
	}
	return ""
}

// NamesUser reports whether an OCSF user object carries a real name, uid or
// uid_alt: one that is not empty and not a placeholder. The name is read as
// UserName reads it, so a "domain\sam" name counts by its sam. An id counts
// when it is not an identity.PlaceholderUserID. It does not decide whether
// the account rule (03 §7.2) can mint an account from them, which depends on
// the account type. The normalizer and the event view both use it to decide
// whether a group change's users[] names anyone. When no entry does, they
// read the older single user field instead (adr/0359).
//
// field reads one key of the object. The caller passes its own recorded
// reader, so the schema check sees which fields were read.
func NamesUser(user map[string]any, field func(obj map[string]any, key string) any) bool {
	if user == nil {
		return false
	}
	if name, _ := UserName(user, field); name != "" {
		return true
	}
	for _, key := range []string{"uid", "uid_alt"} {
		if userID(field(user, key)) != "" {
			return true
		}
	}
	return false
}
