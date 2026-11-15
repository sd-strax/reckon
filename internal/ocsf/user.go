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
// or domain is "". The normalizer reads an account's login out of it. The
// event view uses it for the targets of an account event.
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
		if id, _ := field(user, key).(string); strings.TrimSpace(id) != "" && !identity.PlaceholderUserID(strings.TrimSpace(id)) {
			return true
		}
	}
	return false
}
