package main

import (
	"encoding/json"
	"strings"
	"time"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// ocsfVersion is the OCSF release this adapter writes (adr/0356): the floor,
// which has every field it fills.
const ocsfVersion = "1.1.0"

// normalize shapes a falcon-mcp tool's structured JSON output into OCSF events
// (11 §5.4). Every event is OCSF (adr/0356): what OCSF has no field for, the
// Falcon record itself included, rides under unmapped, which is where OCSF
// puts a source's own fields.
//
//   - detections → OCSF Detection Finding (class 2004): the engine's §4.12
//     normalizer turns each into an Indicator + Sighting (INFERRED). Field
//     paths are defensive first-guesses against the Falcon alerts shape
//     (composite_id, device.hostname, tactic/technique, severity_name) —
//     LIVE-TUNE against real output.
//   - hosts → OCSF Device Inventory Info (class 5001): the engine's §4.17
//     normalizer reads the device and mints its x-host. A host record with
//     no hostname has nothing OCSF's device can name, and is a Base Event.
//   - intel indicators → Base Events (class 0 → the engine's default
//     normalizer, 03 §4.13): the record is preserved whole with the
//     indicator surfaced.
func normalize(operation, text string) ([]ocsfEvent, error) {
	objs, err := extractObjects(text)
	if err != nil {
		return nil, err
	}
	switch operation {
	case "search_detections", "get_detection_details":
		return normalizeDetections(objs), nil
	case "search_hosts", "get_host_details":
		return normalizeHosts(objs), nil
	case "search_indicators":
		return opaqueEvents("indicator", objs), nil
	default:
		return opaqueEvents("", objs), nil
	}
}

// event wraps OCSF fields as one event of a class: it adds the class, the
// OCSF version, the product and the event's time. unmapped holds the source's
// own fields.
//
// OCSF's time is epoch milliseconds, and time_dt, from the datetime profile,
// is the same instant as an RFC 3339 string (adr/0356). An event with no
// time carries neither.
func event(class int, name string, t time.Time, fields, unmapped map[string]any) ocsfEvent {
	raw := map[string]any{
		"class_uid": class,
		"metadata": map[string]any{
			"version": ocsfVersion,
			"product": map[string]any{"vendor_name": "CrowdStrike", "name": "Falcon"},
		},
		"unmapped": unmapped,
	}
	if !t.IsZero() {
		ms := t.UnixMilli()
		raw["time"] = ms
		raw["time_dt"] = time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
	}
	for k, v := range fields {
		raw[k] = v
	}
	return ocsfEvent{ClassUID: class, ClassName: name, Time: t, Raw: raw}
}

// severityIDs are OCSF's severity_id values for Falcon's severity names.
var severityIDs = map[string]int{"informational": 1, "low": 2, "medium": 3, "high": 4, "critical": 5}

// normalizeDetections maps Falcon detections onto the OCSF Detection Finding
// fields the engine's §4.12 normalizer reads (adr/0356):
//
//   - finding_info names the finding: its uid, its title, and the tactic and
//     technique as types.
//   - severity is Falcon's severity name, and severity_id OCSF's number for
//     it. Falcon's severity is not a confidence, so none is written.
//   - device is the host the detection fired on, by its hostname.
//
// The adapter reads no process, file or user from a detection yet, so it
// writes no evidences[]. The engine reads the finding's own device as the
// host the detection fired on, so the finding still names its host.
func normalizeDetections(items []map[string]any) []ocsfEvent {
	out := make([]ocsfEvent, 0, len(items))
	for _, d := range items {
		uid := firstNonEmpty(str(d["composite_id"]), str(d["detection_id"]), str(d["id"]))
		title := firstNonEmpty(str(d["description"]), str(d["display_name"]), str(d["name"]))
		if title == "" {
			title = "Falcon detection"
		}
		finding := map[string]any{"uid": "falcon:" + uid, "title": title}
		var types []any
		for _, k := range []string{"tactic", "technique"} {
			if v := str(d[k]); v != "" {
				types = append(types, v)
			}
		}
		if len(types) > 0 {
			finding["types"] = types
		}
		fields := map[string]any{"finding_info": finding}
		if sev := str(d["severity_name"]); sev != "" {
			fields["severity"] = sev
			if id, ok := severityIDs[strings.ToLower(sev)]; ok {
				fields["severity_id"] = id
			}
		}
		if h := digStr(d, "device", "hostname"); h != "" {
			fields["device"] = map[string]any{"hostname": h}
		}
		out = append(out, event(2004, "Detection Finding", eventTime(d, "created_timestamp"), fields, map[string]any{"falcon": d}))
	}
	return out
}

// normalizeHosts maps Falcon host records onto OCSF Device Inventory Info
// (class 5001), which the engine's §4.17 normalizer reads (adr/0356). The
// adapter reads one field of a host record, its hostname, and that is the
// device. The record itself rides whole under unmapped.falcon.
//
// A record with no hostname gives OCSF's device nothing to name. It stays a
// Base Event, so the record is still kept.
func normalizeHosts(objs []map[string]any) []ocsfEvent {
	out := make([]ocsfEvent, 0, len(objs))
	for _, o := range objs {
		hostname := str(o["hostname"])
		if hostname == "" {
			out = append(out, opaqueEvents("", []map[string]any{o})...)
			continue
		}
		fields := map[string]any{"device": map[string]any{"hostname": hostname}}
		out = append(out, event(5001, "Device Inventory Info", time.Time{}, fields, map[string]any{"falcon": o}))
	}
	return out
}

// opaqueEvents preserves vendor objects whole as Base Events under the
// default normalizer (03 §4.13), surfacing one well-known key when present.
// Both ride under unmapped: a Base Event has no field for either.
func opaqueEvents(surfaceKey string, objs []map[string]any) []ocsfEvent {
	out := make([]ocsfEvent, 0, len(objs))
	for _, o := range objs {
		unmapped := map[string]any{"falcon": o}
		if surfaceKey != "" {
			if v := str(o[surfaceKey]); v != "" {
				unmapped[surfaceKey] = v
			}
		}
		out = append(out, event(0, "Base Event", time.Time{}, nil, unmapped))
	}
	return out
}

// extractObjects pulls the object list out of a tool result: a bare array, a
// bare object, or a wrapper with a well-known array field (the Falcon API
// convention is resources[]).
func extractObjects(text string) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var arr []map[string]any
		if err := json.Unmarshal([]byte(trimmed), &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return nil, err
	}
	for _, key := range []string{"resources", "detections", "hosts", "results", "data", "items"} {
		if arr, ok := obj[key].([]any); ok {
			out := make([]map[string]any, 0, len(arr))
			for _, e := range arr {
				if m, ok := e.(map[string]any); ok {
					out = append(out, m)
				}
			}
			return out, nil
		}
	}
	return []map[string]any{obj}, nil
}

// eventTime parses an RFC3339 timestamp at key, else returns the zero time.
func eventTime(o map[string]any, key string) time.Time {
	if s, ok := o[key].(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func digStr(m map[string]any, path ...string) string {
	cur := any(m)
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	return str(cur)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
