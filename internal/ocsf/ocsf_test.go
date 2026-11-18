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
		"class_uid": 3002, "category_uid": 3, "activity_id": 0, "type_uid": 300200, "severity_id": 0,
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

// RegistryValueData reads the first typed field that holds a value, then
// data, writes it by adr/0341's rule, and reads every field through the
// caller's reader (adr/0359).
func TestRegistryValueData(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value map[string]any
		want  string
		ok    bool
	}{
		{"none", nil, "", false},
		{"no data", map[string]any{"name": "x"}, "", false},
		{"string", map[string]any{"reg_string_data": "x", "reg_integer_data": float64(1), "data": "d"}, "x", true},
		{"integer", map[string]any{"reg_integer_data": float64(4294967295), "data": "d"}, "4294967295", true},
		{"binary", map[string]any{"reg_binary_data": "AAE=", "data": "d"}, "AAE=", true},
		{"string list as JSON", map[string]any{"reg_string_list_data": []any{"a", "b"}, "data": "d"}, `["a","b"]`, true},
		{"data string", map[string]any{"data": "d"}, "d", true},
		{"data string as it arrives", map[string]any{"data": " C:\\a.exe "}, " C:\\a.exe ", true},
		{"data number", map[string]any{"data": float64(4)}, "4", true},
		{"data list as JSON", map[string]any{"data": []any{"a", "b"}}, `["a","b"]`, true},
		{"data object as JSON", map[string]any{"data": map[string]any{"a": "b"}}, `{"a":"b"}`, true},
		{"an empty data string is data", map[string]any{"data": ""}, "", true},
		// An empty typed value never hides the next field.
		{"empty string before an integer", map[string]any{"type_id": float64(4), "reg_string_data": "", "reg_integer_data": float64(4)}, "4", true},
		{"empty list before data", map[string]any{"reg_string_list_data": []any{}, "data": "x"}, "x", true},
		// A number float64 cannot state exactly is left out.
		{"integer past 2^53", map[string]any{"reg_integer_data": float64(9007199254740993), "data": "x"}, "x", true},
		{"integer past 2^53 alone", map[string]any{"reg_integer_data": float64(9007199254740993)}, "", false},
		{"integer past 2^53 in data", map[string]any{"data": float64(9007199254740993)}, "", false},
		{"largest exact integer", map[string]any{"reg_integer_data": float64(9007199254740991)}, "9007199254740991", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[string]bool{}
			got, ok := RegistryValueData(tc.value, func(m map[string]any, k string) any {
				seen[k] = true
				return m[k]
			})
			if got != tc.want || ok != tc.ok {
				t.Errorf("RegistryValueData = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
			if tc.value != nil && len(seen) == 0 {
				t.Error("no field was read through the caller's reader")
			}
		})
	}
}

// NamesUser counts a login, a uid or a uid_alt that is not a placeholder,
// and reads each field through the caller's reader (adr/0359).
func TestNamesUser(t *testing.T) {
	for _, tc := range []struct {
		user map[string]any
		want bool
	}{
		{nil, false},
		{map[string]any{}, false},
		{map[string]any{"uid": ""}, false},
		{map[string]any{"name": " ", "uid": "-", "uid_alt": "0"}, false},
		{map[string]any{"name": "anonymous logon"}, false},
		{map[string]any{"name": `CONTOSO\`}, false},
		{map[string]any{"name": "jdoe"}, true},
		{map[string]any{"name": `CONTOSO\jdoe`}, true},
		{map[string]any{"uid": "S-1-5-21-1-2-3-1101"}, true},
		{map[string]any{"uid_alt": "jdoe@contoso.example"}, true},
		{map[string]any{"name": `X\-`, "domain": "D"}, true},
		{map[string]any{"name": `CONTOSO\-`, "domain": "-"}, false},
		{map[string]any{"name": `CONTOSO\jdoe`, "domain": "-"}, true},
	} {
		read := 0
		got := NamesUser(tc.user, func(m map[string]any, k string) any { read++; return m[k] })
		if got != tc.want {
			t.Errorf("NamesUser(%v) = %v; want %v", tc.user, got, tc.want)
		}
		if tc.user != nil && read == 0 {
			t.Errorf("NamesUser(%v) read nothing through the caller's reader", tc.user)
		}
	}
}

// UserName reads the name and domain as the normalizer's account rule does:
// trimmed, a placeholder domain blanked, "domain\sam" split when no domain is
// left, placeholders absent.
func TestUserName(t *testing.T) {
	for _, tc := range []struct {
		user         map[string]any
		name, domain string
	}{
		{map[string]any{}, "", ""},
		{map[string]any{"name": " - "}, "", ""},
		{map[string]any{"name": "n/a", "domain": "CONTOSO"}, "", "CONTOSO"},
		{map[string]any{"name": " jdoe ", "domain": " CONTOSO "}, "jdoe", "CONTOSO"},
		{map[string]any{"name": ` CONTOSO \ jdoe `}, "jdoe", "CONTOSO"},
		{map[string]any{"name": `CONTOSO\-`}, "", "CONTOSO"},
		{map[string]any{"name": `-\jdoe`}, "jdoe", ""},
		{map[string]any{"name": "jdoe", "domain": "-"}, "jdoe", ""},
		{map[string]any{"name": 7}, "", ""},
		{map[string]any{"name": `X\-`, "domain": "D"}, `X\-`, "D"},
		{map[string]any{"name": `CONTOSO\-`, "domain": "-"}, "", "CONTOSO"},
		{map[string]any{"name": `CONTOSO\jdoe`, "domain": "-"}, "jdoe", "CONTOSO"},
		{map[string]any{"name": `CN=Doe\, John,CN=Users,DC=contoso,DC=com`, "domain": "-"}, `CN=Doe\, John,CN=Users,DC=contoso,DC=com`, ""},
		{map[string]any{"name": `CN=Doe\, John,CN=Users,DC=contoso,DC=com`}, `CN=Doe\, John,CN=Users,DC=contoso,DC=com`, ""},
	} {
		name, domain := UserName(tc.user, func(m map[string]any, k string) any { return m[k] })
		if name != tc.name || domain != tc.domain {
			t.Errorf("UserName(%v) = %q, %q; want %q, %q", tc.user, name, domain, tc.name, tc.domain)
		}
	}
}

// ReadUser reads a user object by the one rule (adr/0367): the name and
// domain by UserName, the domain\sam login only for a name with no "@", the
// principal name from the name or the uid_alt, the ids trimmed with a
// placeholder read as none, and the account type by AccountTypeID through
// the caller's reader.
func TestReadUser(t *testing.T) {
	for _, tc := range []struct {
		user map[string]any
		want User
	}{
		{map[string]any{"name": `CONTOSO\jdoe`, "uid": " S-1-5-21-7-1101 ", "uid_alt": "jdoe@contoso.com"},
			User{Name: "jdoe", Domain: "CONTOSO", UID: "S-1-5-21-7-1101", UIDAlt: "jdoe@contoso.com", Login: `CONTOSO\jdoe`, PrincipalName: "jdoe@contoso.com"}},
		{map[string]any{"name": "jdoe@contoso.com", "domain": "contoso.com", "uid": "-"},
			User{Name: "jdoe@contoso.com", Domain: "contoso.com", Login: "jdoe@contoso.com", PrincipalName: "jdoe@contoso.com"}},
		{map[string]any{"name": "root", "account": map[string]any{"type": "linux account"}},
			User{Name: "root", Login: "root", AccountTypeID: 9}},
		{map[string]any{"name": "jdoe", "account": map[string]any{"type_id": float64(6), "type": "Windows Account"}},
			User{Name: "jdoe", Login: "jdoe", AccountTypeID: 6}},
	} {
		read := map[string]bool{}
		got := ReadUser(tc.user, func(m map[string]any, k string) any { read[k] = true; return m[k] })
		if got != tc.want {
			t.Errorf("ReadUser(%v) = %+v; want %+v", tc.user, got, tc.want)
		}
		if _, ok := tc.user["account"]; ok && !read["type_id"] {
			t.Errorf("ReadUser(%v) read the account type outside the caller's reader", tc.user)
		}
	}
}
