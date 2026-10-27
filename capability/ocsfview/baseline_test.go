package ocsfview

import (
	"testing"
	"time"
)

// adr/0095: the fold is a counting fact — three nightly logons on three days
// are a pattern; the one interactive logon from elsewhere is a deviation
// naming the fields that differ; two logons on two days are nothing.
func TestBaseline(t *testing.T) {
	day := func(d int, hour int) time.Time { return time.Date(2026, 4, 12+d, hour, 0, 10, 0, time.UTC) }
	auth := func(id string, at time.Time, logon, src, dst string) BaselineEvent {
		return BaselineEvent{ID: id, ClassUID: 3002, ClassName: "Authentication", Time: at, Key: keyOf("logon_type", logon, "source", src, "destination", dst)}
	}
	events := []BaselineEvent{
		auth("n1", day(1, 2), "Network", "10.0.9.10", "WIN-FILE01"),
		auth("n2", day(2, 2), "Network", "10.0.9.10", "WIN-FILE01"),
		auth("n3", day(3, 2), "Network", "10.0.9.10", "WIN-FILE01"),
		auth("n4", day(3, 3), "Network", "10.0.9.10", "WIN-FILE01"), // same day, another hour: still the pattern
		auth("x1", day(8, 14), "RemoteInteractive", "10.0.4.20", "WIN-FILE01"),
		{ID: "p1", ClassUID: 1007, ClassName: "Process Activity", Time: day(8, 14), Key: keyOf("image", "powershell.exe", "parent", "mstsc.exe")}, // no process pattern: not a deviation
		{ID: "u1", ClassUID: 5001, ClassName: "Device Inventory Info", Time: day(0, 1)},                                                           // no key at all
	}
	v := Baseline(events)
	if v.EventsConsidered != 7 || !v.From.Equal(day(0, 1)) || !v.To.Equal(day(8, 14)) {
		t.Errorf("window = %v → %v over %d", v.From, v.To, v.EventsConsidered)
	}
	if len(v.Patterns) != 1 {
		t.Fatalf("patterns = %+v; want one", v.Patterns)
	}
	p := v.Patterns[0]
	if p.Count != 4 || p.Days != 3 || p.TypicalHour != 2 || p.Key["logon_type"] != "Network" || len(p.EventIDs) != 4 {
		t.Errorf("pattern = %+v", p)
	}
	if len(v.Deviations) != 1 || v.Deviations[0].EventID != "x1" {
		t.Fatalf("deviations = %+v; want x1 only", v.Deviations)
	}
	if got := v.Deviations[0].Differs; len(got) != 2 || got[0] != "logon_type" || got[1] != "source" {
		t.Errorf("differs = %v; want logon_type, source", got)
	}

	// Below the thresholds nothing repeats, and nothing deviates from nothing.
	short := Baseline(events[:2])
	if len(short.Patterns) != 0 || len(short.Deviations) != 0 {
		t.Errorf("two logons on two days: %+v", short)
	}
	if e := Baseline(nil); e.Patterns == nil || e.Deviations == nil {
		t.Error("empty fold must serialize lists, not null")
	}
}

func TestPatternKey(t *testing.T) {
	auth := map[string]any{"logon_type": "Network", "src_endpoint": map[string]any{"ip": "10.0.9.10"}, "dst_endpoint": map[string]any{"hostname": "WIN-FILE01"}}
	if k := PatternKey(3002, auth); k["logon_type"] != "Network" || k["source"] != "10.0.9.10" || k["destination"] != "WIN-FILE01" {
		t.Errorf("auth key = %v", k)
	}
	net := map[string]any{"dst_endpoint": map[string]any{"domain": "exfil.example.net", "port": float64(443)}}
	if k := PatternKey(4001, net); k["destination"] != "exfil.example.net" || k["port"] != "443" {
		t.Errorf("network key = %v", k)
	}
	proc := map[string]any{"process": map[string]any{"name": "powershell.exe", "parent_process": map[string]any{"name": "explorer.exe"}}}
	if k := PatternKey(1007, proc); k["image"] != "powershell.exe" || k["parent"] != "explorer.exe" {
		t.Errorf("process key = %v", k)
	}
	if PatternKey(2004, map[string]any{}) != nil {
		t.Error("a detection has no pattern key")
	}
}
