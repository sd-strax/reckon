// Package ocsf holds the small pure rules about writing and reading an OCSF
// event that the adapters, the normalizers and the event view share
// (adr/0356): the fields every event starts with, how a time is written, and
// how a whole number is read. It does no I/O and knows no vendor.
package ocsf

import (
	"encoding/json"
	"math"
	"strings"
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

// SplitRegistryPath splits an OCSF registry path, which is the full key path
// with the hive first, into the hive (the first segment) and the rest. The
// normalizer and the event view both split with it, so the view finds the
// x-registry-key the record holds.
func SplitRegistryPath(full string) (hive, keyPath string) {
	full = strings.Trim(strings.TrimSpace(full), `\`)
	hive, keyPath, _ = strings.Cut(full, `\`)
	return hive, strings.TrimLeft(keyPath, `\`)
}
