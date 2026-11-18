package stix

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Prop names one property inside one of reckon's property-extensions, such as
// the domain of reckon-account. It is the one place a reader finds the path to
// that property, in Go or in SQL, so no reader spells out a definition id.
type Prop struct {
	ext  string
	name string
}

// prop builds a Prop. It panics when the extension's schema does not list the
// property, so a path to a property that cannot exist fails at start-up.
func prop(ext, name string) Prop {
	if !slices.Contains(props[ext], name) {
		panic(fmt.Sprintf("stix: %s has no property %s", ext, name))
	}
	return Prop{ext: ext, name: name}
}

// The extension properties readers look up.
var (
	AccountPrincipalName = prop(ExtAccount, "principal_name")
	AccountUIDAlt        = prop(ExtAccount, "uid_alt")
	PersonPrincipalName  = prop(ExtPerson, "principal_name")
	PersonMail           = prop(ExtPerson, "mail")
	PersonDirectoryID    = prop(ExtPerson, "directory_id")
	ProcessName          = prop(ExtProcess, "name")
	FilePath             = prop(ExtFile, "path")
	ObservationSOR       = prop(ExtObservation, "sor")
	// ObservationPersonRef is the person a directory record describes
	// (adr/0324, adr/0357). Readers that ask which records are about an
	// entity read it beside object_refs.
	ObservationPersonRef = prop(ExtObservation, "person_ref")
	// ObservationUserAccounts maps each user object's place in the record to
	// the strong accounts the normalizer minted for it (adr/0398). Readers that link
	// a user object to its account read it.
	ObservationUserAccounts = prop(ExtObservation, "user_accounts")
	AssertionAssertedBy     = prop(ExtAssertion, "asserted_by")
	AssertionWithdrawnAt    = prop(ExtAssertion, "withdrawn_at")
)

// Extension is the name of the property-extension the property is in.
func (p Prop) Extension() string { return p.ext }

// Name is the property's name inside its extension.
func (p Prop) Name() string { return p.name }

// SQL is a Postgres expression for the property's value as text, read from a
// jsonb column that holds a STIX object, such as "payload". It is NULL when the
// object has no such property.
func (p Prop) SQL(column string) string {
	return ExtensionSQL(column, p.ext) + "->>'" + p.name + "'"
}

// JSONSQL is SQL with the property's value as jsonb, not as text. It is
// NULL when the object has no such property.
func (p Prop) JSONSQL(column string) string {
	return ExtensionSQL(column, p.ext) + "->'" + p.name + "'"
}

// From reads the property from a decoded STIX object. It is nil when the
// object has no such property.
func (p Prop) From(obj map[string]any) any {
	return ExtensionFrom(obj, p.ext)[p.name]
}

// String reads the property as a string. It is "" when the object has no such
// property, or when the value is not a string.
func (p Prop) String(obj map[string]any) string {
	s, _ := p.From(obj).(string)
	return s
}

// ExtensionSQL is a Postgres expression for a property-extension's jsonb
// object inside a jsonb column that holds a STIX object.
func ExtensionSQL(column, ext string) string {
	return column + "->'extensions'->'" + Key(ext) + "'"
}

// HasExtensionSQL is a Postgres condition that holds when the STIX object in a
// jsonb column carries the extension.
func HasExtensionSQL(column, ext string) string {
	return "(" + column + "->'extensions') ? '" + Key(ext) + "'"
}

// ExtensionFrom returns a property-extension's properties from a decoded STIX
// object, or nil when the object does not carry it.
func ExtensionFrom(obj map[string]any, ext string) map[string]any {
	exts, _ := obj["extensions"].(map[string]any)
	m, _ := exts[Key(ext)].(map[string]any)
	return m
}

// DecodeExtension decodes a property-extension of the STIX object in payload
// into v. found is false, and v is left alone, when the object does not carry
// the extension.
func DecodeExtension(payload []byte, ext string, v any) (found bool, err error) {
	var obj struct {
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	if err := json.Unmarshal(payload, &obj); err != nil {
		return false, fmt.Errorf("stix: decode object: %w", err)
	}
	raw, ok := obj.Extensions[Key(ext)]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return true, fmt.Errorf("stix: decode %s: %w", ext, err)
	}
	return true, nil
}
