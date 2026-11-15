package server

import "testing"

// TestStixLabel derives readable labels from persisted STIX payloads (the raw
// UUID a ticket reader can't use → the identifying value they can).
func TestStixLabel(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		payload string
		want    string
	}{
		{"host by hostname", "reckon-host", `{"domain":"CONTOSO","hostname":"WIN-FILE01"}`, "WIN-FILE01"},
		{"host falls back to name", "reckon-host", `{"name":"srv-01"}`, "srv-01"},
		{"ipv4 value", "ipv4-addr", `{"value":"10.0.0.5"}`, "10.0.0.5"},
		{"user by display name", "user-account", `{"display_name":"Mike Torres","user_id":"mtorres"}`, "Mike Torres"},
		{"user falls back to user_id", "user-account", `{"user_id":"svc_backup"}`, "svc_backup"},
		{"file by name", "file", `{"name":"evil.exe"}`, "evil.exe"},
		{"registry key with a value", "windows-registry-key", `{"key":"HKEY_LOCAL_MACHINE\\Run","values":[{"name":"Updater","data":"u.exe"}]}`, `HKEY_LOCAL_MACHINE\Run (Updater)`},
		{"registry key's default value", "windows-registry-key", `{"key":"HKEY_LOCAL_MACHINE\\Run","values":[{"name":""}]}`, `HKEY_LOCAL_MACHINE\Run (default)`},
		{"process command line collapses whitespace", "process", "{\"command_line\":\"powershell   -enc\\n  AAA\"}", "powershell -enc AAA"},
		{"process numeric pid formatted", "process", `{"pid":4321}`, "pid 4321"},
		{"unknown type, no identifier", "reckon-mystery", `{"foo":"bar"}`, ""},
		{"malformed payload", "reckon-host", `not json`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stixLabel(c.typ, []byte(c.payload)); got != c.want {
				t.Errorf("stixLabel(%s) = %q; want %q", c.typ, got, c.want)
			}
		})
	}
}
