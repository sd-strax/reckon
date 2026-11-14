// Package ocsf holds the small pure rules about writing and reading an OCSF
// event that the adapters, the normalizers and the event view share
// (adr/0356): the fields every event starts with, how a time is written, and
// how a whole number is read. It does no I/O and knows no vendor.
package ocsf

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

// Event returns the fields every OCSF event of a class starts with: the
// class, the OCSF version it is written in, the product that reported it,
// and its time (see SetTime). The caller adds the class's own fields.
//
// It also writes the other fields OCSF requires of every event. The category
// is the class's own. The activity and the severity are 0, which is OCSF's
// Unknown, and the type, which OCSF derives as the class number times 100
// plus the activity, follows. No adapter in tree maps its vendor's events to
// OCSF's activities yet. One whose source says how severe the event is sets
// severity_id.
func Event(class int, version string, product map[string]any, t time.Time) map[string]any {
	raw := map[string]any{
		"class_uid": class,
		// A class number is its category times 1000 plus its own number. An
		// extension's class adds the extension's number times 100000 in
		// front (201001 is the Windows extension's 1001).
		"category_uid": class % 100000 / 1000,
		"activity_id":  0,
		"type_uid":     class * 100,
		"severity_id":  0,
		"metadata": map[string]any{
			"version": version,
			"product": product,
		},
	}
	SetTime(raw, t)
	return raw
}

// Product is OCSF's product object for a vendor's product.
func Product(vendor, name string) map[string]any {
	return map[string]any{"vendor_name": vendor, "name": name}
}

// SetTime writes an event's time. OCSF's time is epoch milliseconds, and
// time_dt, from the datetime profile, is the same instant as an RFC 3339
// string. An event with no time (the zero time) carries neither.
func SetTime(raw map[string]any, t time.Time) {
	if t.IsZero() {
		return
	}
	ms := t.UnixMilli()
	raw["time"] = ms
	raw["time_dt"] = time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
}

// EpochMillis reads a whole number of milliseconds since the epoch as a
// time. A number with a fraction, or one too large for a JSON number to hold
// exactly (2^53 and up), is not a timestamp.
func EpochMillis(v any) (time.Time, bool) {
	var ms int64
	switch n := v.(type) {
	case float64:
		if n != math.Trunc(n) || math.Abs(n) >= 1<<53 {
			return time.Time{}, false
		}
		ms = int64(n)
	case int:
		ms = int64(n)
	case int64:
		ms = n
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return time.Time{}, false
		}
		ms = i
	default:
		return time.Time{}, false
	}
	return time.UnixMilli(ms).UTC(), true
}

// WholeNumber reads a JSON or Go number that has no fraction. A value beyond
// ±1e9 is clamped to it.
func WholeNumber(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n != math.Trunc(n) || math.IsInf(n, 0) {
			return 0, false
		}
		// Clamp before converting: a float beyond int's range does not
		// convert to a defined value.
		return int(min(max(n, -1e9), 1e9)), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(min(max(i, -1e9), 1e9)), true
	case int:
		return n, true
	case int64:
		return int(min(max(n, -1e9), 1e9)), true
	}
	return 0, false
}

// RegistryValueName is the name of the value a reg_value object names, and
// whether it names one. A string name is the name as written. A value with no
// name but is_default true is the default value, whose name is "" (adr/0341).
// Anything else names no value.
func RegistryValueName(regValue map[string]any) (string, bool) {
	if s, ok := regValue["name"].(string); ok {
		return s, true
	}
	if isDefault, _ := regValue["is_default"].(bool); isDefault {
		return "", true
	}
	return "", false
}

// RegistryValueData is the data of an OCSF registry value object as a
// string, and whether the object gives one (03 §4.6). The normalizer's
// windows-registry-value-type data and the event view's Data field both come
// from it, so the two always agree.
//
// From 1.6.0 the data can arrive in a field for its type (adr/0359). The
// first of reg_string_data, reg_integer_data, reg_binary_data and
// reg_string_list_data that holds a value is read, else data. An empty
// string or an empty list in a typed field holds no value, so it never hides
// the next field.
//
// The value is written by adr/0341's rule. A string is kept as it arrives.
// Any other value is written as its JSON text, so a list reads as a JSON
// array. A number that float64 cannot state exactly (2^53 or more, which
// arrives rounded) is left out, as when the source gives no data.
//
// field reads one key of the object. The caller passes its own recorded
// reader, so the schema check sees which fields were read.
func RegistryValueData(regValue map[string]any, field func(obj map[string]any, key string) any) (string, bool) {
	if regValue == nil {
		return "", false
	}
	for _, key := range []string{"reg_string_data", "reg_integer_data", "reg_binary_data", "reg_string_list_data"} {
		v := field(regValue, key)
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		if l, ok := v.([]any); ok && len(l) == 0 {
			continue
		}
		if text, ok := registryText(v); ok {
			return text, true
		}
	}
	return registryText(field(regValue, "data"))
}

// registryText writes one registry data value by adr/0341's rule: a string
// as it arrives, anything else as its JSON text. A missing value, and a
// number float64 cannot state exactly, report false.
func registryText(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return x, true
	case float64:
		if !(math.Abs(x) < 1<<53) {
			return "", false
		}
	}
	text, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v), true
	}
	return string(text), true
}

// integerText writes a whole number in decimal, or "" when the value is not
// one that can be written exactly. A float64 is a decoded JSON number: one
// of 2^53 or more may already be rounded, so it reads as "". This is the
// rule scalarString in package capability follows for identifiers.
func integerText(v any) string {
	switch n := v.(type) {
	case float64:
		if n != math.Trunc(n) || math.Abs(n) >= 1<<53 {
			return ""
		}
		return strconv.FormatFloat(n, 'f', -1, 64)
	case json.Number:
		if _, err := n.Int64(); err != nil {
			return ""
		}
		return n.String()
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
	}
	return ""
}
