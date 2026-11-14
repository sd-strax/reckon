package ocsf

import (
	"strings"

	"github.com/sd-strax/reckon/identity"
)

// NamesUser reports whether an OCSF user object carries a real name, uid or
// uid_alt: one that is not empty and not a placeholder
// (identity.PlaceholderLogin, identity.PlaceholderUserID). A name of the form
// "domain\sam" counts by its sam. It does not decide whether the account
// rule (03 §7.2) can mint an account from them, which depends on the account
// type. The normalizer and the event view both use it to decide whether a
// group change's users[] names anyone. When no entry does, they read the
// older single user field instead (adr/0359).
//
// field reads one key of the object. The caller passes its own recorded
// reader, so the schema check sees which fields were read.
func NamesUser(user map[string]any, field func(obj map[string]any, key string) any) bool {
	if user == nil {
		return false
	}
	name, _ := field(user, "name").(string)
	if _, sam, ok := strings.Cut(name, `\`); ok {
		name = sam
	}
	if strings.TrimSpace(name) != "" && !identity.PlaceholderLogin(name) {
		return true
	}
	for _, key := range []string{"uid", "uid_alt"} {
		if id, _ := field(user, key).(string); strings.TrimSpace(id) != "" && !identity.PlaceholderUserID(strings.TrimSpace(id)) {
			return true
		}
	}
	return false
}
