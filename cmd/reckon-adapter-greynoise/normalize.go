package main

import (
	"encoding/json"
	"maps"
	"strings"
	"time"

	"github.com/sd-strax/reckon/internal/ocsf"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// ocsfVersion is the OCSF release this adapter writes (adr/0356): the floor,
// which has every field it fills.
const ocsfVersion = "1.1.0"

// normalize shapes a greynoise-mcp tool's structured JSON output into OCSF
// events (11 §5.4). GreyNoise reports IP reputation, which is a threat-intel
// *claim* about an indicator, not passive telemetry — so a MALICIOUS
// classification becomes an OCSF Detection Finding (class 2004) that the
// engine's §4.12 normalizer turns into an Indicator + Sighting (INFERRED). A
// benign/unknown result — and every BSI (known business service, the RIOT
// successor) result — is emitted as a Base Event (class 0, opaque to the
// engine) rather than minting a false malicious Indicator: the reputation is
// preserved, no indicator is asserted. get_indicator_context's contract
// (03 §3) is exactly this: `reputation` plus `ti_matches` only when there is
// a match.
//
// Every event is OCSF (adr/0356). What OCSF has no field for (the reputation
// word, the GreyNoise record itself) rides under unmapped, which is where
// OCSF puts a source's own fields.
//
// Field paths follow the vendor's shipped v3 response schema (validated
// against the 0.5.4 source): top-level `ip`, with classification/actor/
// last_seen/tags nested under `internet_scanner_intelligence` and the
// known-service verdict under `business_service_intelligence`.
func normalize(operation, text string) ([]ocsfEvent, error) {
	objs, err := extractObjects(text)
	if err != nil {
		return nil, err
	}
	switch operation {
	case "lookup-ip-context", "gnql-query":
		return normalizeContext(objs), nil
	case "bsi-lookup":
		// A BSI match is a known business service — benign enrichment.
		return opaque("benign", objs), nil
	default:
		return opaque("", objs), nil
	}
}

// event wraps OCSF fields as one event of a class: it adds the class, the
// OCSF version, the product and the event's time. unmapped holds the source's
// own fields.
//
// The time is written as ocsf.SetTime describes.
func event(class int, name string, t time.Time, fields, unmapped map[string]any) ocsfEvent {
	raw := ocsf.Event(class, ocsfVersion, ocsf.Product("GreyNoise", "GreyNoise"), t)
	raw["unmapped"] = unmapped
	maps.Copy(raw, fields)
	return ocsfEvent{ClassUID: class, ClassName: name, Time: t, Raw: raw}
}

// normalizeContext classifies each GreyNoise v3 IP-context object.
func normalizeContext(items []map[string]any) []ocsfEvent {
	out := make([]ocsfEvent, 0, len(items))
	for _, e := range items {
		ip := str(e["ip"])
		if ip == "" {
			continue // not a context object (e.g. a stats wrapper) — skip
		}
		isi, _ := e["internet_scanner_intelligence"].(map[string]any)
		classification := strings.ToLower(str(isi["classification"]))
		t := eventTime(isi, "last_seen")
		if classification == "malicious" {
			out = append(out, findingEvent(ip, classification, t, isi, e))
		} else {
			out = append(out, enrichmentEvent(ip, classification, t, e))
		}
	}
	return out
}

// findingEvent builds an OCSF Detection Finding (2004) for an address
// GreyNoise classifies malicious (03 §4.12, adr/0356):
//
//   - finding_info names the finding: a uid from the address, a title from
//     the classification and the actor, and the tag names as types.
//   - evidences[] holds one artifact, the address as src_endpoint.ip, so the
//     engine mints its ipv4-addr or ipv6-addr.
//   - confidence is High: GreyNoise gives a classification and no score, and
//     only a malicious one becomes a finding.
//   - unmapped.pattern is the address as a STIX pattern. GreyNoise gives no
//     pattern of its own, and OCSF has no field for one.
//
// GreyNoise gives no severity and no device, so neither is written.
func findingEvent(ip, classification string, t time.Time, isi, raw map[string]any) ocsfEvent {
	title := "GreyNoise: " + classification
	if actor := str(isi["actor"]); actor != "" && actor != "unknown" {
		title += " (" + actor + ")"
	}
	finding := map[string]any{"uid": "greynoise:" + ip, "title": title}
	if types := tagNames(isi["tags"]); len(types) > 0 {
		finding["types"] = types
	}
	return event(2004, "Detection Finding", t, map[string]any{
		"finding_info":  finding,
		"confidence_id": 3,
		"confidence":    "High",
		"evidences":     []any{map[string]any{"src_endpoint": map[string]any{"ip": ip}}},
	}, map[string]any{
		"pattern":   "[" + addrType(ip) + ":value = '" + ip + "']",
		"greynoise": raw,
	})
}

// addrType is the STIX object type of an address: ipv6-addr when it has a
// colon, else ipv4-addr.
func addrType(ip string) string {
	if strings.Contains(ip, ":") {
		return "ipv6-addr"
	}
	return "ipv4-addr"
}

// enrichmentEvent emits a benign/unknown reputation as a Base Event (class 0
// → the engine's default normalizer, 03 §4.13): the reputation is preserved as
// provenance without asserting an indicator.
func enrichmentEvent(ip, classification string, t time.Time, raw map[string]any) ocsfEvent {
	return event(0, "Base Event", t, nil, map[string]any{
		"ip":         ip,
		"reputation": classification,
		"greynoise":  raw,
	})
}

func opaque(reputation string, objs []map[string]any) []ocsfEvent {
	out := make([]ocsfEvent, 0, len(objs))
	for _, o := range objs {
		unmapped := map[string]any{"greynoise": o}
		if reputation != "" {
			unmapped["reputation"] = reputation
		}
		if ip := str(o["ip"]); ip != "" {
			unmapped["ip"] = ip
		}
		out = append(out, event(0, "Base Event", time.Time{}, nil, unmapped))
	}
	return out
}

// tagNames extracts tag names from the v3 tag objects ({id, slug, name,
// category, …}); plain-string tags pass through for robustness.
func tagNames(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		switch t := e.(type) {
		case string:
			out = append(out, t)
		case map[string]any:
			if n := str(t["name"]); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

// extractObjects pulls the list of GreyNoise objects out of a tool result: a
// bare object (lookup-ip-context / bsi-lookup), or a wrapper with a well-known
// array field (gnql-query / multi-ip return {"data": [...]}).
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
	for _, key := range []string{"data", "results", "items"} {
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

// eventTime parses a timestamp at key — RFC3339 or GreyNoise's date-only
// last_seen ("2026-01-15") — else returns the zero time.
func eventTime(o map[string]any, key string) time.Time {
	s, ok := o[key].(string)
	if !ok {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
