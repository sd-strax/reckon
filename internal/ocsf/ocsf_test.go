package ocsf

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestEvent(t *testing.T) {
	at := time.Date(2026, 7, 14, 9, 28, 0, 123456789, time.FixedZone("x", 3600))
	got := Event(3002, "1.1.0", Product("Acme", "Log"), at)
	want := map[string]any{
		"class_uid": 3002,
		"metadata": map[string]any{
			"version": "1.1.0",
			"product": map[string]any{"vendor_name": "Acme", "name": "Log"},
		},
		"time":    at.UnixMilli(),
		"time_dt": "2026-07-14T08:28:00.123Z",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Event = %v\nwant %v", got, want)
	}

	got = Event(0, "1.1.0", Product("Acme", "Log"), time.Time{})
	if _, ok := got["time"]; ok {
		t.Error("an event with no time carries time")
	}
	if _, ok := got["time_dt"]; ok {
		t.Error("an event with no time carries time_dt")
	}
}

// The epoch itself is a time, not "no time".
func TestSetTimeEpoch(t *testing.T) {
	raw := map[string]any{}
	SetTime(raw, time.Unix(0, 0).UTC())
	if raw["time"] != int64(0) || raw["time_dt"] != "1970-01-01T00:00:00Z" {
		t.Errorf("SetTime(epoch) = %v", raw)
	}
}

func TestEpochMillis(t *testing.T) {
	want := time.UnixMilli(1784021280000).UTC()
	for _, v := range []any{float64(1784021280000), int(1784021280000), int64(1784021280000), json.Number("1784021280000")} {
		if got, ok := EpochMillis(v); !ok || !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("EpochMillis(%T %v) = %v, %v; want %v", v, v, got, ok, want)
		}
	}
	for _, v := range []any{1.5, float64(1 << 53), -float64(1 << 53), math.Inf(1), math.NaN(), "1784021280000", json.Number("1.5"), nil, true} {
		if got, ok := EpochMillis(v); ok {
			t.Errorf("EpochMillis(%T %v) = %v; want not a timestamp", v, v, got)
		}
	}
}

func TestWholeNumber(t *testing.T) {
	tests := []struct {
		in   any
		want int
		ok   bool
	}{
		{float64(85), 85, true},
		{85, 85, true},
		{int64(85), 85, true},
		{json.Number("85"), 85, true},
		{float64(-3), -3, true},
		{1e18, 1e9, true},
		{-1e18, -1e9, true},
		{int64(math.MaxInt64), 1e9, true},
		{85.5, 0, false},
		{math.Inf(1), 0, false},
		{math.NaN(), 0, false},
		{json.Number("8.5"), 0, false},
		{"85", 0, false},
		{nil, 0, false},
	}
	for _, tt := range tests {
		if got, ok := WholeNumber(tt.in); got != tt.want || ok != tt.ok {
			t.Errorf("WholeNumber(%T %v) = %d, %v; want %d, %v", tt.in, tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
