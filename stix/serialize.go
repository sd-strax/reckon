package stix

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/sd-strax/reckon/identity"
)

// Kind says which family of STIX objects a type belongs to. It decides the
// common properties an object has.
type Kind int

// The three kinds of STIX object reckon writes.
const (
	// SDO is a STIX domain object, such as observed-data or identity.
	SDO Kind = iota + 1
	// SRO is a STIX relationship object: relationship or sighting.
	SRO
	// SCO is a STIX cyber-observable object, such as file or reckon-host.
	SCO
)

// Object is what a writer hands the serializer: one object, with its
// properties in a flat map.
//
// The serializer adds type, spec_version and id. It adds created and modified
// to an SDO or an SRO. It puts each property in Properties at the top level
// when STIX 2.1 defines it on the type, or when the type is a reckon type
// whose schema lists it. Otherwise it puts the property in the type's
// property-extension, when that extension's schema lists it. Any other
// property is an error.
type Object struct {
	Type string
	ID   string
	// Created and Modified are an SDO's or an SRO's times. Modified is
	// Created when it is zero. An SCO has neither.
	Created  time.Time
	Modified time.Time
	// Properties are the object's own properties, without the ones the
	// serializer adds.
	Properties map[string]any
	// Extensions are property-extensions the writer fills by name, such as
	// ExtProvenance or ExtObservation. Each property must be one the
	// extension's schema lists.
	Extensions map[string]map[string]any
}

// typeSpec is what the serializer knows about one type.
type typeSpec struct {
	kind Kind
	// own lists the properties the type itself defines, beyond the common
	// properties of its kind. For a reckon type it is the schema's list.
	own []string
	// ext is the property-extension that holds the type's custom
	// properties, or "" when the type has none.
	ext string
	// custom is true for a reckon type, which names its definition in
	// extensions.
	custom bool
	// required lists the properties the type's schema requires, beyond the
	// ones the serializer sets (adr/0357).
	required []string
	// oneOf lists groups of properties of which the type's schema requires
	// at least one, such as a file's hashes or name.
	oneOf [][]string
}

// commonSDO are the common properties of an SDO or an SRO (STIX 2.1 §3.2)
// that a writer may set. The serializer sets type, spec_version, id, created,
// modified and extensions itself.
var commonSDO = []string{
	"created_by_ref", "revoked", "labels", "confidence", "lang",
	"external_references", "object_marking_refs", "granular_markings",
}

// commonSCO are the common properties of an SCO (STIX 2.1 §3.2) that a writer
// may set.
var commonSCO = []string{"object_marking_refs", "granular_markings", "defanged"}

// serializerSet are the properties the serializer sets on every object.
var serializerSet = []string{"type", "spec_version", "id", "created", "modified", "extensions"}

// nativeTypes lists the STIX 2.1 types reckon writes, with the properties the
// specification defines on each (sections 4, 5 and 6), and the
// property-extension each type's custom properties go into.
var nativeTypes = map[string]typeSpec{
	identity.TypeObservedData: {kind: SDO, ext: ExtObservation, own: []string{
		"first_observed", "last_observed", "number_observed", "objects", "object_refs"}},
	"indicator": {kind: SDO, own: []string{
		"name", "description", "indicator_types", "pattern", "pattern_type", "pattern_version",
		"valid_from", "valid_until", "kill_chain_phases"}},
	identity.TypeIdentity: {kind: SDO, ext: ExtPerson, own: []string{
		"name", "description", "roles", "identity_class", "sectors", "contact_information"}},
	"note": {kind: SDO, ext: ExtAttestation, own: []string{"abstract", "content", "authors", "object_refs"}},

	"relationship": {kind: SRO, ext: ExtAssertion, own: []string{
		"relationship_type", "description", "source_ref", "target_ref", "start_time", "stop_time"}},
	"sighting": {kind: SRO, own: []string{
		"description", "first_seen", "last_seen", "count", "sighting_of_ref",
		"observed_data_refs", "where_sighted_refs", "summary"}},

	"artifact": {kind: SCO, own: []string{
		"mime_type", "payload_bin", "url", "hashes", "encryption_algorithm", "decryption_key"}},
	identity.TypeDirectory:  {kind: SCO, own: []string{"path", "path_enc", "ctime", "mtime", "atime", "contains_refs"}},
	identity.TypeDomainName: {kind: SCO, own: []string{"value", "resolves_to_refs"}},
	identity.TypeEmailAddr:  {kind: SCO, own: []string{"value", "display_name", "belongs_to_ref"}},
	identity.TypeEmailMessage: {kind: SCO, ext: ExtEmailMessage, own: []string{
		"is_multipart", "date", "content_type", "from_ref", "sender_ref", "to_refs", "cc_refs",
		"bcc_refs", "message_id", "subject", "received_lines", "additional_header_fields", "body",
		"body_multipart", "raw_email_ref"}},
	identity.TypeFile: {kind: SCO, ext: ExtFile, own: []string{
		"hashes", "size", "name", "name_enc", "magic_number_hex", "mime_type", "ctime", "mtime",
		"atime", "parent_directory_ref", "contains_refs", "content_ref"}},
	identity.TypeIPv4Addr: {kind: SCO, own: []string{"value", "resolves_to_refs", "belongs_to_refs"}},
	identity.TypeIPv6Addr: {kind: SCO, own: []string{"value", "resolves_to_refs", "belongs_to_refs"}},
	identity.TypeNetworkTraffic: {kind: SCO, own: []string{
		"start", "end", "is_active", "src_ref", "dst_ref", "src_port", "dst_port", "protocols",
		"src_byte_count", "dst_byte_count", "src_packets", "dst_packets", "ipfix",
		"src_payload_ref", "dst_payload_ref", "encapsulates_refs", "encapsulated_by_ref"}},
	identity.TypeProcess: {kind: SCO, ext: ExtProcess, own: []string{
		"is_hidden", "pid", "created_time", "cwd", "command_line", "environment_variables",
		"opened_connection_refs", "creator_user_ref", "image_ref", "parent_ref", "child_refs"}},
	identity.TypeURL: {kind: SCO, own: []string{"value"}},
	identity.TypeUserAccount: {kind: SCO, ext: ExtAccount, own: []string{
		"user_id", "credential", "account_login", "account_type", "display_name",
		"is_service_account", "is_privileged", "can_escalate_privs", "is_disabled",
		"account_created", "account_expires", "credential_last_changed",
		"account_first_login", "account_last_login"}},
	identity.TypeWindowsRegistryKey: {kind: SCO, own: []string{
		"key", "values", "modified_time", "creator_user_ref", "number_of_subkeys"}},
}

// nativeRequired is what STIX 2.1 requires of each native type reckon writes,
// beyond the properties the serializer sets: the property lists and the
// at-least-one groups of the OASIS schemas, read with the spec text
// (sections 4, 5 and 6).
var nativeRequired = map[string]struct {
	required []string
	oneOf    [][]string
}{
	identity.TypeObservedData:       {required: []string{"first_observed", "last_observed", "number_observed"}, oneOf: [][]string{{"objects", "object_refs"}}},
	"indicator":                     {required: []string{"pattern", "pattern_type", "valid_from"}},
	identity.TypeIdentity:           {required: []string{"name"}},
	"note":                          {required: []string{"content", "object_refs"}},
	"relationship":                  {required: []string{"relationship_type", "source_ref", "target_ref"}},
	"sighting":                      {required: []string{"sighting_of_ref"}},
	"artifact":                      {oneOf: [][]string{{"payload_bin", "url"}}},
	identity.TypeDirectory:          {required: []string{"path"}},
	identity.TypeDomainName:         {required: []string{"value"}},
	identity.TypeEmailAddr:          {required: []string{"value"}},
	identity.TypeEmailMessage:       {required: []string{"is_multipart"}},
	identity.TypeFile:               {oneOf: [][]string{{"hashes", "name"}}},
	identity.TypeIPv4Addr:           {required: []string{"value"}},
	identity.TypeIPv6Addr:           {required: []string{"value"}},
	identity.TypeNetworkTraffic:     {required: []string{"protocols"}, oneOf: [][]string{{"src_ref", "dst_ref"}}},
	identity.TypeURL:                {required: []string{"value"}},
	identity.TypeWindowsRegistryKey: {oneOf: [][]string{{"key", "values", "modified_time", "creator_user_ref", "number_of_subkeys"}}},
}

// specs is every type the serializer writes, native and reckon, and props is
// each property-extension's properties. Both are built once from the lists
// above and the embedded schemas.
var specs, props = buildSpecs()

func buildSpecs() (map[string]typeSpec, map[string][]string) {
	specs := make(map[string]typeSpec, len(nativeTypes)+len(definitions))
	for typ, s := range nativeTypes {
		r := nativeRequired[typ]
		s.required, s.oneOf = r.required, r.oneOf
		specs[typ] = s
	}
	props := map[string][]string{}
	for _, d := range definitions {
		names, required, err := schemaProperties(d)
		if err != nil {
			panic(err)
		}
		switch d.ExtensionTypes[0] {
		case PropertyExtension:
			props[d.Name] = slices.DeleteFunc(names, func(n string) bool { return n == "extension_type" })
			extRequired[d.Name] = slices.DeleteFunc(required, func(n string) bool { return n == "extension_type" })
		case NewSDO, NewSCO:
			kind := SDO
			if d.ExtensionTypes[0] == NewSCO {
				kind = SCO
			}
			own := slices.DeleteFunc(names, func(n string) bool { return slices.Contains(serializerSet, n) })
			req := slices.DeleteFunc(required, func(n string) bool { return slices.Contains(serializerSet, n) })
			specs[d.Name] = typeSpec{kind: kind, own: own, custom: true, required: req}
		}
	}
	return specs, props
}

// extRequired is what each property-extension's schema requires, beyond
// extension_type, which the serializer sets.
var extRequired = map[string][]string{}

// schemaProperties reads the names of the properties a definition's schema
// lists, and the ones it requires.
func schemaProperties(d Definition) (names, required []string, err error) {
	b, err := d.Schema()
	if err != nil {
		return nil, nil, fmt.Errorf("stix: read schema of %s: %w", d.Name, err)
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, nil, fmt.Errorf("stix: parse schema of %s: %w", d.Name, err)
	}
	names = make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, s.Required, nil
}

// KindOf returns the kind of a type the serializer writes. ok is false for a
// type it does not know.
func KindOf(typ string) (Kind, bool) {
	s, ok := specs[typ]
	return s.kind, ok
}

// Timestamped reports whether objects of the type carry created and
// modified: an SDO or an SRO does, an SCO does not.
func Timestamped(typ string) bool {
	k, ok := KindOf(typ)
	return ok && k != SCO
}

// Timestamp formats t as a STIX timestamp for created and modified: UTC, to
// the millisecond, with a Z.
func Timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// Truncate cuts t to the millisecond in UTC, the precision of created and
// modified. A writer stores the truncated time in a table column, so the
// column and the payload hold the same instant.
func Truncate(t time.Time) time.Time {
	return t.UTC().Truncate(time.Millisecond)
}

// Marshal builds an object's STIX JSON. See Object for how it places each
// property.
func Marshal(o Object) ([]byte, error) {
	m, err := Build(o)
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// Build is Marshal before encoding: the object as a map.
func Build(o Object) (map[string]any, error) {
	spec, ok := specs[o.Type]
	if !ok {
		return nil, fmt.Errorf("stix: %s is not a type the serializer writes", o.Type)
	}
	typ, _, _ := strings.Cut(o.ID, "--")
	if typ != o.Type {
		return nil, fmt.Errorf("stix: id %q is not of type %s", o.ID, o.Type)
	}
	out := map[string]any{"type": o.Type, "spec_version": SpecVersion, "id": o.ID}
	switch {
	case spec.kind == SCO && (!o.Created.IsZero() || !o.Modified.IsZero()):
		return nil, fmt.Errorf("stix: %s is an SCO, which has no created or modified", o.ID)
	case spec.kind != SCO && o.Created.IsZero():
		return nil, fmt.Errorf("stix: %s has no created time", o.ID)
	case spec.kind != SCO:
		modified := o.Modified
		if modified.IsZero() {
			modified = o.Created
		}
		out["created"], out["modified"] = Timestamp(o.Created), Timestamp(modified)
	}

	common := commonSDO
	if spec.kind == SCO {
		common = commonSCO
	}
	exts := map[string]map[string]any{}
	for name, v := range o.Properties {
		v, keep := value(v)
		switch {
		case slices.Contains(serializerSet, name):
			return nil, fmt.Errorf("stix: %s: the serializer sets %s itself", o.ID, name)
		case slices.Contains(spec.own, name) || slices.Contains(common, name):
			if keep {
				out[name] = v
			}
		case spec.ext != "" && slices.Contains(props[spec.ext], name):
			if keep {
				if exts[spec.ext] == nil {
					exts[spec.ext] = map[string]any{}
				}
				exts[spec.ext][name] = v
			}
		default:
			return nil, fmt.Errorf("stix: %s: property %q is neither a STIX property of %s nor in its property-extension", o.ID, name, o.Type)
		}
	}
	for ext, fields := range o.Extensions {
		if ext != ExtProvenance && ext != spec.ext {
			return nil, fmt.Errorf("stix: %s: %s does not take the property-extension %s", o.ID, o.Type, ext)
		}
		for name, v := range fields {
			if !slices.Contains(props[ext], name) {
				return nil, fmt.Errorf("stix: %s: property %q is not in %s", o.ID, name, ext)
			}
			v, keep := value(v)
			if !keep {
				continue
			}
			if exts[ext] == nil {
				exts[ext] = map[string]any{}
			}
			if _, dup := exts[ext][name]; dup {
				return nil, fmt.Errorf("stix: %s: property %q of %s is given twice", o.ID, name, ext)
			}
			exts[ext][name] = v
		}
	}

	extensions := map[string]any{}
	if spec.custom {
		d, _ := Lookup(o.Type)
		extensions[d.ID] = map[string]any{"extension_type": string(d.ExtensionTypes[0])}
	}
	for ext, fields := range exts {
		fields["extension_type"] = string(PropertyExtension)
		extensions[Key(ext)] = fields
	}
	if len(extensions) > 0 {
		out["extensions"] = extensions
	}
	// Every reference names a STIX object by its identifier (adr/0357).
	if err := checkRefs(o.ID, out); err != nil {
		return nil, err
	}
	for _, fields := range exts {
		if err := checkRefs(o.ID, fields); err != nil {
			return nil, err
		}
	}
	// An object missing a property its schema requires is not valid STIX,
	// so it is refused here, at runtime, not only in the test (adr/0357). A
	// property counts as missing when it is empty after omission.
	for _, name := range spec.required {
		if _, ok := out[name]; !ok {
			return nil, fmt.Errorf("stix: %s: %s requires %s, which is missing or empty", o.ID, o.Type, name)
		}
	}
	for _, group := range spec.oneOf {
		if !slices.ContainsFunc(group, func(n string) bool { _, ok := out[n]; return ok }) {
			return nil, fmt.Errorf("stix: %s: %s requires one of %s, and has none", o.ID, o.Type, strings.Join(group, ", "))
		}
	}
	for ext, fields := range exts {
		for _, name := range extRequired[ext] {
			if _, ok := fields[name]; !ok {
				return nil, fmt.Errorf("stix: %s: %s requires %s, which is missing or empty", o.ID, ext, name)
			}
		}
	}
	return out, nil
}

// value prepares one property value. keep is false for a value STIX would
// read as unknown: nil, an empty string, an empty list, or a zero time. A
// time becomes a STIX timestamp.
func value(v any) (out any, keep bool) {
	switch x := v.(type) {
	case nil:
		return nil, false
	case string:
		return x, x != ""
	case identity.STIXID:
		return string(x), x != ""
	case time.Time:
		if x.IsZero() {
			return nil, false
		}
		return x.UTC().Format(time.RFC3339Nano), true
	case *time.Time:
		if x == nil || x.IsZero() {
			return nil, false
		}
		return x.UTC().Format(time.RFC3339Nano), true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map:
		if rv.IsNil() || rv.Len() == 0 {
			return nil, false
		}
	case reflect.Pointer:
		if rv.IsNil() {
			return nil, false
		}
	}
	return v, true
}

// Key returns the extensions key of a reckon custom type or property-extension:
// its definition id. It panics on a name with no definition, which is a
// programming error.
func Key(name string) string {
	d, ok := Lookup(name)
	if !ok {
		panic("stix: no extension definition named " + name)
	}
	return d.ID
}

// ExtensionProperties returns the properties a property-extension's schema
// lists, without extension_type.
func ExtensionProperties(ext string) []string {
	return slices.Clone(props[ext])
}

// TypeProperties returns the properties the serializer puts at the top level
// of a type, beyond the ones it sets itself: the type's own and the common
// properties of its kind. ok is false for a type it does not write.
func TypeProperties(typ string) (out []string, ok bool) {
	s, ok := specs[typ]
	if !ok {
		return nil, false
	}
	common := commonSDO
	if s.kind == SCO {
		common = commonSCO
	}
	return append(slices.Clone(s.own), common...), true
}

// PropertyExtensionOf returns the property-extension that holds a type's
// custom properties, or "" when it has none.
func PropertyExtensionOf(typ string) string { return specs[typ].ext }

// Identifier is STIX 2.1's identifier form (§2.9), as the OASIS identifier
// schema checks it: a type, two hyphens and an RFC 4122 UUID.
var Identifier = regexp.MustCompile(`^[a-z][a-z0-9-]+[a-z0-9]--[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

// IsIdentifier reports whether ref is a STIX identifier.
func IsIdentifier(ref string) bool { return Identifier.MatchString(ref) }

// checkRefs refuses a _ref property or a _refs list whose value is not a STIX
// identifier, so no reference escapes the serializer in another form.
func checkRefs(id string, props map[string]any) error {
	for name, v := range props {
		switch {
		case strings.HasSuffix(name, "_ref"):
			if s, ok := v.(string); !ok || !IsIdentifier(s) {
				return fmt.Errorf("stix: %s: %s %v is not a STIX identifier", id, name, v)
			}
		case strings.HasSuffix(name, "_refs"):
			for _, ref := range refList(v) {
				if !IsIdentifier(ref) {
					return fmt.Errorf("stix: %s: %s holds %q, which is not a STIX identifier", id, name, ref)
				}
			}
		}
	}
	return nil
}

// refList reads a _refs value: a list of strings or of identity.STIXID.
func refList(v any) []string {
	var out []string
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return []string{fmt.Sprint(v)}
	}
	for i := 0; i < rv.Len(); i++ {
		out = append(out, fmt.Sprint(rv.Index(i).Interface()))
	}
	return out
}
