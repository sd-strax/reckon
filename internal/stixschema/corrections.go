package stixschema

import (
	"fmt"
	"maps"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"github.com/sd-strax/reckon/identity"
)

// Some OASIS patterns disagree with the STIX 2.1 spec text (adr/0357 rule 8).
// The copied schema files stay byte for byte as upstream has them. Each
// correction below checks the spec's own rule by hand. Where the schema would
// refuse a value the spec allows, the correction then hands the schema a copy
// of the object in which that one value is one the schema accepts. Nothing
// else in the copy changes, so every other rule still applies.
type correction struct {
	typ string
	// spec is the section of STIX 2.1 the correction follows.
	spec string
	// why says what the OASIS schema gets wrong.
	why string
	// apply checks the spec's rule on obj and edits the schema's copy.
	apply func(obj, schemaCopy map[string]any) []Problem
}

var corrections = []correction{
	{
		typ:  "ipv6-addr",
		spec: "§6.9.1",
		why:  "the value pattern begins with a literal s and writes d for \\d, so it refuses every address with an embedded dotted quad",
		apply: func(obj, cp map[string]any) []Problem {
			v, _ := obj["value"].(string)
			if !isIPv6(v) {
				return []Problem{{Message: fmt.Sprintf("value %q is not an IPv6 address in CIDR notation (STIX 2.1 §6.9.1)", v)}}
			}
			cp["value"] = "::1"
			return nil
		},
	},
	{
		typ:  "windows-registry-key",
		spec: "§6.17.1",
		why:  "the key's not pattern anchors only HKLM, so it refuses a key holding HKCU or HKU anywhere, and it does not check that the key starts with a full hive name",
		apply: func(obj, cp map[string]any) []Problem {
			key, ok := obj["key"].(string)
			if !ok {
				return nil
			}
			cp["key"] = "HKEY_LOCAL_MACHINE"
			if !identity.HasRegistryHive(key) {
				return []Problem{{Message: fmt.Sprintf("key %q does not start with a fully expanded hive name (STIX 2.1 §6.17.1)", key)}}
			}
			return nil
		},
	},
	{
		typ:  "windows-registry-key",
		spec: "§6.17.1",
		why:  "the anyOf names modified, the STIX 2.0 property, where 2.1 has modified_time",
		apply: func(obj, cp map[string]any) []Problem {
			return atLeastOne(obj, cp, "windows-registry-key", "§6.17.1",
				[]string{"key", "values", "modified_time", "creator_user_ref", "number_of_subkeys"},
				"modified_time", "modified")
		},
	},
	{
		typ:  "process",
		spec: "§6.13.1",
		why:  "the anyOf names created, name and arguments, STIX 2.0 properties, and leaves out created_time",
		apply: func(obj, cp map[string]any) []Problem {
			return atLeastOne(obj, cp, "process", "§6.13.1",
				[]string{"extensions", "is_hidden", "pid", "created_time", "cwd", "command_line",
					"environment_variables", "opened_connection_refs", "creator_user_ref", "image_ref",
					"parent_ref", "child_refs"},
				"created_time", "created")
		},
	},
}

// atLeastOne checks the spec's rule that an object holds at least one of
// props. When it does through prop21 alone, the schema's copy also gets the
// 2.0 name the schema's anyOf lists.
func atLeastOne(obj, cp map[string]any, typ, spec string, props []string, prop21, prop20 string) []Problem {
	if !slices.ContainsFunc(props, func(p string) bool { _, ok := obj[p]; return ok }) {
		return []Problem{{Message: fmt.Sprintf("a %s holds none of %s (STIX 2.1 %s)", typ, strings.Join(props, ", "), spec)}}
	}
	if v, ok := obj[prop21]; ok {
		cp[prop20] = v
	}
	return nil
}

func isIPv6(v string) bool {
	if p, err := netip.ParsePrefix(v); err == nil {
		return p.Addr().Is6() && p.Addr().Zone() == ""
	}
	a, err := netip.ParseAddr(v)
	return err == nil && a.Is6() && a.Zone() == ""
}

// applyCorrections returns the copy of obj the OASIS schema validates, and
// the problems the spec's own rules find.
func applyCorrections(typ string, obj map[string]any) (map[string]any, []Problem) {
	cp := maps.Clone(obj)
	var out []Problem
	for _, c := range corrections {
		if c.typ == typ {
			out = append(out, c.apply(obj, cp)...)
		}
	}
	return cp, out
}

// propertyName is the rule for a property name (STIX 2.1 §3.1, and §7.3.2.1
// for an extension's properties): lower case ASCII letters, digits and
// underscores, starting with a letter, 3 to 250 characters. "id" is the one
// shorter name the spec defines. The OASIS properties schema leaves its
// alternation unanchored at the end, so it admits a name with upper case
// letters after the third character.
var propertyName = regexp.MustCompile(`^[a-z][a-z0-9_]{2,249}$`)

// checkPropertyNames checks the names of the object's own properties and of
// each property-extension's properties.
func checkPropertyNames(obj map[string]any) []Problem {
	var out []Problem
	bad := func(where, name string) {
		out = append(out, Problem{Message: fmt.Sprintf("%s %q is not a valid property name (STIX 2.1 §3.1, §7.3.2.1)", where, name)})
	}
	for _, name := range sortedKeys(obj) {
		if name != "id" && !propertyName.MatchString(name) {
			bad("property", name)
		}
	}
	exts, _ := obj["extensions"].(map[string]any)
	for _, key := range sortedKeys(exts) {
		ext, _ := exts[key].(map[string]any)
		for _, name := range sortedKeys(ext) {
			if !propertyName.MatchString(name) {
				bad("extension property", name)
			}
		}
	}
	return out
}
