package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/sd-strax/reckon/capability"
)

// jsonColumns are the projected structs, cast to JSON in the query and
// decoded back into the OCSF objects the lake stored.
var jsonColumns = map[string]bool{
	"actor": true, "api": true, "cloud": true, "resources": true,
	"src_endpoint": true, "http_request": true, "metadata": true,
}

// intColumns are the OCSF integer scalars of the projection.
var intColumns = map[string]bool{
	"activity_id": true, "status_id": true, "severity_id": true,
	"class_uid": true, "category_uid": true, "type_uid": true,
}

// athenaTimeLayouts are the forms Athena renders a timestamp column in.
var athenaTimeLayouts = []string{
	"2006-01-02 15:04:05.000",
	"2006-01-02 15:04:05.000 UTC",
	"2006-01-02 15:04:05",
	time.RFC3339Nano,
}

// classAPIActivity is the class the queries select (class_uid = 6003); a row
// carries its own class_uid, which the event keeps.
const classAPIActivity = 6003

// classNames are the OCSF captions of the classes the CloudTrail table holds.
var classNames = map[int]string{6003: "API Activity", 3001: "Account Change", 3002: "Authentication"}

// rowsToEvents reassembles each result row into its OCSF event.
func rowsToEvents(rs resultSet, table string) ([]capability.OcsfPayload, error) {
	out := make([]capability.OcsfPayload, 0, len(rs.rows))
	for _, r := range rs.rows {
		evt, err := rowToEvent(rs.columns, r, table)
		if err != nil {
			return nil, err
		}
		out = append(out, evt)
	}
	return out, nil
}

// rowToEvent rebuilds the OCSF event a row was projected from, its class the
// row's own class_uid (API Activity when the row has none):
// each JSON column decoded into its object (a NULL field inside it dropped,
// as the lake stores absence), the scalars typed as OCSF has them, and
// metadata passed through untouched so the source's product and schema
// version reach the engine. The only addition is metadata.log_name, set to
// the table the row came from when the lake left it empty.
func rowToEvent(cols []string, row []*string, table string) (capability.OcsfPayload, error) {
	raw := map[string]any{}
	for i, col := range cols {
		if i >= len(row) || row[i] == nil {
			continue
		}
		v := *row[i]
		switch {
		case jsonColumns[col]:
			var decoded any
			if err := json.Unmarshal([]byte(v), &decoded); err != nil {
				return capability.OcsfPayload{}, fatal("column " + col + " is not JSON: " + err.Error())
			}
			if decoded = pruneNulls(decoded); decoded != nil {
				raw[col] = decoded
			}
		case intColumns[col]:
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return capability.OcsfPayload{}, fatal("column " + col + " is not an integer: " + strconv.Quote(v))
			}
			raw[col] = n
		case col == "time":
			raw[col] = epochMillis(v)
		case col == "time_dt":
			raw[col] = rfc3339(v)
		default:
			raw[col] = v
		}
	}
	classUID := classAPIActivity
	if n, ok := raw["class_uid"].(int64); ok {
		classUID = int(n)
	} else {
		raw["class_uid"] = int64(classAPIActivity)
	}
	meta, _ := raw["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		raw["metadata"] = meta
	}
	if s, _ := meta["log_name"].(string); s == "" {
		meta["log_name"] = table
	}
	return capability.OcsfPayload{
		ClassUID: classUID, ClassName: classNames[classUID],
		Time: eventTime(raw), Raw: raw,
	}, nil
}

// pruneNulls drops null members from decoded JSON, recursively; an object or
// array left empty is kept (it was present in the source).
func pruneNulls(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if val == nil {
				delete(x, k)
				continue
			}
			x[k] = pruneNulls(val)
		}
		return x
	case []any:
		out := x[:0]
		for _, val := range x {
			if val != nil {
				out = append(out, pruneNulls(val))
			}
		}
		return out
	}
	return v
}

// epochMillis reads OCSF time: an integer of epoch milliseconds as the lake
// stores it, or a rendered timestamp converted to one. Anything else is kept
// as the string it was.
func epochMillis(v string) any {
	s := strings.TrimSpace(v)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	if t, ok := parseAthenaTime(s); ok {
		return t.UnixMilli()
	}
	return s
}

// rfc3339 renders an Athena timestamp as OCSF's datetime (RFC 3339), keeping
// a value it cannot parse as given.
func rfc3339(v string) string {
	if t, ok := parseAthenaTime(strings.TrimSpace(v)); ok {
		return t.Format(time.RFC3339Nano)
	}
	return v
}

func parseAthenaTime(s string) (time.Time, bool) {
	for _, layout := range athenaTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// eventTime is the event's time from OCSF time (epoch ms), else time_dt.
func eventTime(raw map[string]any) time.Time {
	if ms, ok := raw["time"].(int64); ok {
		return time.UnixMilli(ms).UTC()
	}
	if s, ok := raw["time_dt"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
