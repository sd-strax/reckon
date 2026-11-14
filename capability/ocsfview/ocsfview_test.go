package ocsfview

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sd-strax/reckon/internal/ocsf"
)

func sentenceText(v View) string {
	var b strings.Builder
	for _, t := range v.Sentence {
		b.WriteString(t.Text)
	}
	return b.String()
}

func fieldValue(v View, label string) (Field, bool) {
	for _, f := range v.Fields {
		if f.Label == label {
			return f, true
		}
	}
	return Field{}, false
}

func payload(js string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		panic(err)
	}
	return m
}

func ent(ref string, props map[string]any) Entity {
	typ, _, _ := strings.Cut(ref, "--")
	return Entity{Ref: ref, Type: typ, Props: props}
}

// TestRender_Authentication: the design's sentence, with the three entities as
// chips bound to the record's refs, and the curated fields with the result
// toned.
// TestRender_Authentication_Location: a source with a reported place reads
// "from <ip> in <city>, <country>" and the Location field names the place and
// who reported it (adr/0208); a place with no source address is not said in
// the sentence, since it would read as the host's.
func TestRender_Authentication_Location(t *testing.T) {
	p := payload(`{"class_uid":3002,"user":{"name":"m.reyes"},"dst_endpoint":{"hostname":"corp-sso"},
		"src_endpoint":{"ip":"203.0.113.5","location":{"city":"Toronto","region":"Ontario","country":"CA","provider":"okta"}},
		"logon_type":"Interactive","status":"SUCCESS"}`)
	v := Render(Event{ClassUID: 3002, Payload: p}, nil)
	if got := sentenceText(v); got != "m.reyes interactive logon to corp-sso from 203.0.113.5 in Toronto, CA (located by okta)" {
		t.Errorf("sentence = %q", got)
	}
	if f, ok := fieldValue(v, "Location"); !ok || f.Value != "Toronto, Ontario, CA · reported by okta" {
		t.Errorf("Location field = %+v", f)
	}

	// The fixture's shape: city and country, no provider.
	p = payload(`{"class_uid":3002,"user":{"name":"m.reyes"},"dst_endpoint":{"hostname":"corp-sso"},
		"src_endpoint":{"ip":"10.0.12.40","location":{"country":"CA","city":"Toronto"}},"status":"FAILURE"}`)
	v = Render(Event{ClassUID: 3002, Payload: p}, nil)
	if got := sentenceText(v); got != "m.reyes logon to corp-sso from 10.0.12.40 in Toronto, CA failed" {
		t.Errorf("sentence = %q", got)
	}
	if f, _ := fieldValue(v, "Location"); f.Value != "Toronto, CA" {
		t.Errorf("Location field = %+v", f)
	}

	// A region stands in for a missing city; a country alone stands.
	p = payload(`{"class_uid":3002,"user":{"name":"m.reyes"},"src_endpoint":{"ip":"10.0.12.40","location":{"region":"Ontario","country":"CA"}}}`)
	if got := sentenceText(Render(Event{ClassUID: 3002, Payload: p}, nil)); got != "m.reyes logon from 10.0.12.40 in Ontario, CA" {
		t.Errorf("sentence = %q", got)
	}
	p = payload(`{"class_uid":3002,"user":{"name":"m.reyes"},"src_endpoint":{"ip":"10.0.12.40","location":{"country":"CA"}}}`)
	if got := sentenceText(Render(Event{ClassUID: 3002, Payload: p}, nil)); got != "m.reyes logon from 10.0.12.40 in CA" {
		t.Errorf("sentence = %q", got)
	}

	// The location's description is shown as what it is, never as the country:
	// a country name the adapter could not code (adr/0208) and a producer's
	// own description read the same way, after the place.
	p = payload(`{"class_uid":3002,"user":{"name":"m.reyes"},"src_endpoint":{"ip":"10.0.12.40","location":{"city":"Atlantis City","desc":"Atlantis","provider":"okta"}}}`)
	v = Render(Event{ClassUID: 3002, Payload: p}, nil)
	if got := sentenceText(v); got != "m.reyes logon from 10.0.12.40 in Atlantis City (located by okta)" {
		t.Errorf("sentence = %q", got)
	}
	if f, _ := fieldValue(v, "Location"); f.Value != "Atlantis City · Atlantis · reported by okta" {
		t.Errorf("Location field = %+v", f)
	}
	// A country-only location from a source that names countries (the Okta
	// adapter's shape): the sentence reads the description, so the timeline
	// and the export carry the place and who located it.
	p = payload(`{"class_uid":3002,"user":{"name":"m.reyes"},"src_endpoint":{"ip":"198.51.100.7","location":{"desc":"Türkiye","provider":"okta"}}}`)
	v = Render(Event{ClassUID: 3002, Payload: p}, nil)
	if got := sentenceText(v); got != "m.reyes logon from 198.51.100.7 in Türkiye (located by okta)" {
		t.Errorf("sentence = %q", got)
	}
	if f, _ := fieldValue(v, "Location"); f.Value != "Türkiye · reported by okta" {
		t.Errorf("Location field = %+v", f)
	}
	p = payload(`{"class_uid":3002,"user":{"name":"m.reyes"},"src_endpoint":{"ip":"10.0.12.40","location":{"city":"Toronto","country":"CA","desc":"corporate office"}}}`)
	v = Render(Event{ClassUID: 3002, Payload: p}, nil)
	if got := sentenceText(v); got != "m.reyes logon from 10.0.12.40 in Toronto, CA" {
		t.Errorf("sentence = %q", got)
	}
	if f, _ := fieldValue(v, "Location"); f.Value != "Toronto, CA · corporate office" {
		t.Errorf("Location field = %+v", f)
	}

	// No source address: the place is not in the sentence, but the field has it.
	p = payload(`{"class_uid":3002,"user":{"name":"m.reyes"},"dst_endpoint":{"hostname":"corp-sso"},
		"src_endpoint":{"location":{"city":"Toronto","country":"CA"}}}`)
	v = Render(Event{ClassUID: 3002, Payload: p}, nil)
	if got := sentenceText(v); got != "m.reyes logon to corp-sso" {
		t.Errorf("sentence = %q", got)
	}
	if f, ok := fieldValue(v, "Location"); !ok || f.Value != "Toronto, CA" {
		t.Errorf("Location field = %+v", f)
	}
}

// TestRender_AuthenticationInitiator (adr/0356): the account that logged on
// is the event's user. actor.user is whoever started the logon, shown as its
// own field when it is another account, and never as the one that logged on.
func TestRender_AuthenticationInitiator(t *testing.T) {
	ents := []Entity{
		ent("user-account--u1", map[string]any{"account_login": `CONTOSO\jdoe`, "account_type": "windows-domain"}),
		ent("user-account--sys", map[string]any{"account_login": `NT AUTHORITY\SYSTEM`, "account_type": "windows-local"}),
	}
	p := payload(`{"class_uid":3002,"user":{"name":"jdoe","domain":"CONTOSO"},"actor":{"user":{"name":"SYSTEM","domain":"NT AUTHORITY"}},
		"dst_endpoint":{"hostname":"WIN-FILE01"},"logon_type":"Interactive","status":"Success"}`)
	v := Render(Event{ClassUID: 3002, Payload: p}, ents)
	if got := sentenceText(v); got != "jdoe interactive logon to WIN-FILE01" {
		t.Errorf("sentence = %q", got)
	}
	if f, _ := fieldValue(v, "User"); f.Value != `CONTOSO\jdoe` || f.Ref != "user-account--u1" {
		t.Errorf("User field = %+v; want the account that logged on", f)
	}
	if f, ok := fieldValue(v, "Started by"); !ok || f.Value != `NT AUTHORITY\SYSTEM` || f.Ref != "user-account--sys" {
		t.Errorf("Started by field = %+v; want the initiator", f)
	}

	// The same account in both places, or no initiator, shows no extra field.
	// It is the same account when the names differ only in letter case, with
	// or without an entity to bind to, and when two spellings bind to one
	// entity.
	for _, tc := range []struct {
		js   string
		ents []Entity
	}{
		{`{"class_uid":3002,"user":{"name":"jdoe","domain":"CONTOSO"},"actor":{"user":{"name":"jdoe","domain":"CONTOSO"}}}`, ents},
		{`{"class_uid":3002,"user":{"name":"jdoe","domain":"CONTOSO"}}`, ents},
		{`{"class_uid":3002,"user":{"name":"jdoe","domain":"CONTOSO"},"actor":{"user":{"name":"JDoe","domain":"contoso"}}}`, ents},
		{`{"class_uid":3002,"user":{"name":"jdoe","domain":"CONTOSO"},"actor":{"user":{"name":"JDoe","domain":"contoso"}}}`, nil},
		{`{"class_uid":3002,"user":{"name":"jdoe"},"actor":{"user":{"name":"jdoe","domain":"CONTOSO"}}}`, ents},
	} {
		if f, ok := fieldValue(Render(Event{ClassUID: 3002, Payload: payload(tc.js)}, tc.ents), "Started by"); ok {
			t.Errorf("%s: Started by field = %+v; want none", tc.js, f)
		}
	}

	// An event that names only an initiator does not say who logged on.
	v = Render(Event{ClassUID: 3002, Payload: payload(`{"class_uid":3002,"actor":{"user":{"name":"SYSTEM","domain":"NT AUTHORITY"}},"dst_endpoint":{"hostname":"WIN-FILE01"}}`)}, ents)
	if got := sentenceText(v); got != "Logon to WIN-FILE01" {
		t.Errorf("sentence with only an initiator = %q", got)
	}
	if f, ok := fieldValue(v, "User"); ok {
		t.Errorf("User field = %+v; want none", f)
	}
}

func TestRender_Authentication(t *testing.T) {
	p := payload(`{"class_uid":3002,"user":{"name":"svc_backup","domain":"CONTOSO","uid":"S-1"},
		"dst_endpoint":{"hostname":"WIN-FILE01","domain":"CONTOSO"},"src_endpoint":{"ip":"10.0.4.20"},
		"logon_type":"RemoteInteractive","status":"SUCCESS"}`)
	ents := []Entity{
		ent("user-account--u1", map[string]any{"account_login": `CONTOSO\svc_backup`, "account_type": "windows-domain"}),
		ent("x-host--h1", map[string]any{"hostname": "WIN-FILE01", "domain": "CONTOSO"}),
		ent("ipv4-addr--i1", map[string]any{"value": "10.0.4.20"}),
	}
	v := Render(Event{ClassUID: 3002, Payload: p}, ents)
	if got := sentenceText(v); got != "svc_backup RDP logon to WIN-FILE01 from 10.0.4.20" {
		t.Errorf("sentence = %q", got)
	}
	want := []Token{
		{T: "entity", Ref: "user-account--u1", Text: "svc_backup"},
		{T: "text", Text: " RDP logon to "},
		{T: "entity", Ref: "x-host--h1", Text: "WIN-FILE01"},
		{T: "text", Text: " from "},
		{T: "entity", Ref: "ipv4-addr--i1", Text: "10.0.4.20"},
	}
	if !slices.Equal(v.Sentence, want) {
		t.Errorf("tokens = %+v", v.Sentence)
	}
	if v.Category != CategoryAuth {
		t.Errorf("category = %s", v.Category)
	}
	if f, ok := fieldValue(v, "User"); !ok || f.Value != `CONTOSO\svc_backup` || f.Ref != "user-account--u1" || !f.Mono {
		t.Errorf("User field = %+v", f)
	}
	if f, _ := fieldValue(v, "Result"); f.Value != "SUCCESS" || f.Tone != "ok" {
		t.Errorf("Result field = %+v", f)
	}
	if f, _ := fieldValue(v, "Logon type"); f.Value != "RemoteInteractive" {
		t.Errorf("Logon type field = %+v", f)
	}

	p["status"] = "FAILURE"
	v = Render(Event{ClassUID: 3002, Payload: p}, nil)
	if got := sentenceText(v); got != "svc_backup RDP logon to WIN-FILE01 from 10.0.4.20 failed" {
		t.Errorf("failed sentence = %q", got)
	}
	// Without entities every slot is text: the sentence reads the same.
	for _, tok := range v.Sentence {
		if tok.T != "text" {
			t.Errorf("token with no record = %+v", tok)
		}
	}
	if f, _ := fieldValue(v, "Result"); f.Tone != "bad" {
		t.Errorf("Result tone = %q", f.Tone)
	}
}

// TestRender_Sentences pins one sentence per class with a template.
func TestRender_Sentences(t *testing.T) {
	cases := []struct {
		name     string
		classUID int
		payload  string
		want     string
		category string
	}{
		{"process", 1007, `{"process":{"pid":6120,"name":"powershell.exe","cmd_line":"powershell.exe -enc ZQ==","parent_process":{"pid":5000,"name":"mstsc-hijack.exe"}},
			"device":{"hostname":"WIN-FILE01"},"actor":{"user":{"name":"svc_backup","domain":"CONTOSO"}}}`,
			"svc_backup ran powershell.exe on WIN-FILE01, child of mstsc-hijack.exe", CategoryProcess},
		{"process launched, no user", 1007, `{"activity_name":"Launch","process":{"pid":1,"name":"a.exe"},"device":{"hostname":"H"}}`,
			"a.exe was launched on H", CategoryProcess},
		{"network", 4001, `{"src_endpoint":{"ip":"10.0.4.20","port":49999},"dst_endpoint":{"ip":"185.220.101.5","port":443,"domain":"exfil.example.net"},
			"connection_info":{"protocol_name":"tcp"},"device":{"hostname":"WIN-FILE01"}}`,
			"WIN-FILE01 connected to 185.220.101.5 port 443 (exfil.example.net) over tcp from 10.0.4.20", CategoryNetwork},
		{"network without host", 4001, `{"src_endpoint":{"ip":"10.0.4.20"},"dst_endpoint":{"ip":"1.2.3.4","port":22}}`,
			"10.0.4.20 connected to 1.2.3.4 port 22", CategoryNetwork},
		{"network failed by activity id", 4001, `{"activity_id":4,"src_endpoint":{"ip":"10.0.4.20"},"dst_endpoint":{"ip":"1.2.3.4","port":22}}`,
			"10.0.4.20 failed to connect to 1.2.3.4 port 22", CategoryNetwork},
		{"network refused", 4001, `{"activity_name":"Refuse","device":{"hostname":"WS1"},"dst_endpoint":{"ip":"1.2.3.4","port":445}}`,
			"WS1 was refused a connection to 1.2.3.4 port 445", CategoryNetwork},
		{"network open with failed status", 4001, `{"activity_name":"Open","status":"Failure","src_endpoint":{"ip":"10.0.4.20"},"dst_endpoint":{"ip":"1.2.3.4"}}`,
			"10.0.4.20 failed to connect to 1.2.3.4", CategoryNetwork},
		{"network unknown activity with failed status", 4001, `{"activity_id":0,"activity_name":"Unknown","status_id":2,"src_endpoint":{"ip":"10.0.0.1"},"dst_endpoint":{"ip":"10.0.0.2"}}`,
			"10.0.0.1 failed to connect to 10.0.0.2", CategoryNetwork},
		{"network closed, no endpoints known", 4001, `{"activity_id":2,"connection_info":{"protocol_name":"tcp"}}`,
			"Connection closed over tcp", CategoryNetwork},
		{"dns", 4003, `{"query":{"hostname":"exfil.example.net","type":"A"},"answers":[{"rdata":"185.220.101.5","type":"A"}],
			"src_endpoint":{"ip":"10.0.4.20"},"device":{"hostname":"WIN-FILE01"},"rcode":"NoError"}`,
			"WIN-FILE01 queried exfil.example.net → 185.220.101.5", CategoryNetwork},
		{"file", 1001, `{"activity_name":"Create","file":{"name":"payload.dll","path":"C:\\Temp\\payload.dll"},"actor":{"user":{"name":"jdoe"},"process":{"pid":4,"name":"explorer.exe"}},"device":{"hostname":"WS1"}}`,
			"jdoe created payload.dll on WS1 via explorer.exe", CategoryProcess},
		{"registry key", 201001, `{"activity_name":"Modify","reg_key":{"path":"HKLM\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Run"},
			"actor":{"process":{"pid":9,"name":"reg.exe"}},"device":{"hostname":"WS1"}}`,
			`reg.exe modified HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Run on WS1`, CategoryProcess},
		{"registry key without actor", 201001, `{"activity_name":"Delete","reg_key":{"path":"HKLM\\SOFTWARE\\Run"},"device":{"hostname":"WS1"}}`,
			`HKEY_LOCAL_MACHINE\SOFTWARE\Run was deleted on WS1`, CategoryProcess},
		{"registry value", 201002, `{"activity_name":"Set","reg_value":{"path":"HKLM\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Run","name":"Updater","data":"C:\\u.exe","type":"REG_SZ"},
			"actor":{"process":{"pid":9,"name":"reg.exe"}},"device":{"hostname":"WS1"}}`,
			`reg.exe set value Updater of HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Run on WS1`, CategoryProcess},
		{"registry value by user", 201002, `{"activity_name":"Delete","reg_value":{"path":"HKLM\\SOFTWARE\\Run","name":"Updater"},"actor":{"user":{"name":"jdoe"}}}`,
			`jdoe deleted value Updater of HKEY_LOCAL_MACHINE\SOFTWARE\Run`, CategoryProcess},
		{"scheduled job", 1006, `{"activity_name":"Create","job":{"name":"Updater","file":{"path":"\\Microsoft\\Updater"},"user":{"name":"SYSTEM"}},"actor":{"user":{"name":"jdoe"}},"device":{"hostname":"WS1"}}`,
			"jdoe created scheduled job Updater on WS1 running as SYSTEM", CategoryProcess},
		{"scheduled job named by its path", 1006, `{"activity_name":"Create","job":{"file":{"path":"\\Microsoft\\Updater"},"job_actions":[{"cmd_line":"u.exe"}]},"device":{"hostname":"WS1"}}`,
			`Scheduled job \Microsoft\Updater was created on WS1`, CategoryProcess},
		{"module", 1005, `{"module":{"file":{"name":"evil.dll","path":"C:\\Temp\\evil.dll"}},"actor":{"process":{"pid":7,"name":"svchost.exe"}},"device":{"hostname":"WS1"}}`,
			"svchost.exe loaded evil.dll on WS1", CategoryProcess},
		{"account change", 3001, `{"activity_name":"Password Reset","user":{"name":"jdoe","domain":"CONTOSO"},"actor":{"user":{"name":"admin","domain":"CONTOSO"}},"device":{"hostname":"DC1"}}`,
			"admin reset the password of jdoe on DC1", CategoryAuth},
		{"account change without actor", 3001, `{"activity_name":"Create","user":{"name":"jdoe"}}`,
			"Account jdoe was created", CategoryAuth},
		{"user access", 3005, `{"activity_name":"Assign Privileges","user":{"name":"jdoe","domain":"CONTOSO"},"actor":{"user":{"name":"admin","domain":"CONTOSO"}},"privileges":["SeDebugPrivilege"],"device":{"hostname":"DC1"}}`,
			"admin assigned privileges to jdoe on DC1", CategoryAuth},
		{"group management", 3006, `{"activity_name":"Add User","user":{"name":"jdoe"},"actor":{"user":{"name":"admin"}},"group":{"name":"Domain Admins"},"device":{"hostname":"DC1"}}`,
			"admin added jdoe to Domain Admins on DC1", CategoryAuth},
		{"group management users[]", 3006, `{"activity_name":"Add User","users":[{"name":"jdoe"},{"name":"asmith"}],"actor":{"user":{"name":"admin"}},"group":{"name":"Domain Admins"},"device":{"hostname":"DC1"}}`,
			"admin added jdoe, asmith to Domain Admins on DC1", CategoryAuth},
		{"group management remove", 3006, `{"activity_name":"Remove User","user":{"name":"jdoe"},"group":{"uid":"g-512"}}`,
			"Account jdoe was removed from g-512", CategoryAuth},
		{"group management privileges", 3006, `{"activity_name":"Assign Privileges","user":{"name":"jdoe"},"actor":{"user":{"name":"admin"}},"group":{"name":"Domain Admins"}}`,
			"admin assigned privileges to jdoe in Domain Admins", CategoryAuth},
		{"email", 4009, `{"email":{"from":"billing@evil.example","to":["ap@corp.example","cfo@corp.example"],"subject":"Overdue invoice 44817","message_uid":"<m1>"}}`,
			"billing@evil.example sent “Overdue invoice 44817” to ap@corp.example, cfo@corp.example", CategoryNetwork},
		{"email url", 4012, `{"activity_name":"Receive","email_uid":"gw-1","url":{"url_string":"https://login.evil.example/x"},"actor":{"user":{"name":"jdoe"}},"device":{"hostname":"WS1"}}`,
			"jdoe received https://login.evil.example/x on WS1", CategoryNetwork},
		{"email url without actor", 4012, `{"url":{"url_string":"https://login.evil.example/x"}}`,
			"Link https://login.evil.example/x was opened", CategoryNetwork},
		{"email file", 4011, `{"activity_name":"Receive","email_uid":"gw-1","file":{"name":"invoice.pdf.exe"},"actor":{"user":{"name":"jdoe"}},"device":{"hostname":"WS1"}}`,
			"jdoe received attachment invoice.pdf.exe on WS1", CategoryNetwork},
		{"email file without actor", 4011, `{"activity_name":"Scan","email_uid":"gw-1","file":{"name":"invoice.pdf.exe"}}`,
			"Attachment invoice.pdf.exe was scanned", CategoryNetwork},
		{"detection", 2004, `{"metadata":{"product":{"vendor_name":"CrowdStrike"}},"finding_info":{"uid":"det-1","title":"Suspicious lateral movement","types":["T1021.001"]},"severity_id":4,
			"device":{"hostname":"WIN-FILE01"},"evidences":[{"actor":{"user":{"name":"svc_backup"}}}]}`,
			"CrowdStrike detected “Suspicious lateral movement” (high) on WIN-FILE01", CategoryDetection},
		{"detection on the artifact's device", 2004, `{"finding_info":{"uid":"det-1","title":"Beacon"},"severity_id":99,"severity":"Urgent",
			"device":{"hostname":"SENSOR"},"evidences":[{"process":{"pid":1}},{"device":{"hostname":"WS7"},"user":{"name":"jdoe"}}]}`,
			"Detected “Beacon” (urgent) on WS7", CategoryDetection},
		{"case", 2005, `{"category_uid":2,"finding_info_list":[{"uid":"INC0010039","title":"Phishing report"}],"status":"New","priority":"3 - Moderate","assignee_group":{"name":"SOC Tier 2"}}`,
			"Case INC0010039 “Phishing report”, New, priority 3 - Moderate, assigned to SOC Tier 2", CategoryCase},
		{"case assigned by group id", 2005, `{"finding_info_list":[{"uid":"INC1"},{"uid":"det-1","title":"second"}],"priority":2,"assignee_group":{"uid":"grp-7"}}`,
			"Case INC1, priority 2, assigned to grp-7", CategoryCase},
		{"device inventory", 5001, `{"device":{"hostname":"WIN-FILE01","os":{"name":"Windows Server 2022"},"owner":{"name":"File Services"}},"unmapped":{"criticality":"high"}}`,
			"WIN-FILE01 runs Windows Server 2022, owned by File Services, criticality high", CategoryAsset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := Render(Event{ClassUID: tc.classUID, Payload: payload(tc.payload)}, nil)
			if got := sentenceText(v); got != tc.want {
				t.Errorf("sentence = %q\n            want %q", got, tc.want)
			}
			if v.Category != tc.category {
				t.Errorf("category = %s; want %s", v.Category, tc.category)
			}
			if len(v.Fields) == 0 {
				t.Error("no fields")
			}
		})
	}
}

// TestRender_HashByAlgorithmID: a fingerprint that carries OCSF's required
// algorithm_id and no caption still shows its hash, labelled with the
// caption OCSF gives that id (adr/0356).
func TestRender_HashByAlgorithmID(t *testing.T) {
	p := payload(`{"process":{"pid":6120,"name":"tool.exe","file":{"hashes":[{"algorithm_id":3,"value":"9f1c"}]}},"device":{"hostname":"WIN-FILE01"}}`)
	v := Render(Event{ClassUID: 1007, Payload: p}, []Entity{ent("file--f1", map[string]any{"hashes": map[string]any{"SHA-256": "9f1c"}})})
	var hash *Field
	for i := range v.Fields {
		if v.Fields[i].Label == "SHA-256" {
			hash = &v.Fields[i]
		}
	}
	if hash == nil || hash.Value != "9f1c" {
		t.Errorf("fields = %+v; want the SHA-256 shown", v.Fields)
	}
}

// TestRender_ProcessFields: the curated process fields, the command line shown
// as it is, the parent with its pid, the image bound to the file object.
func TestRender_ProcessFields(t *testing.T) {
	p := payload(`{"process":{"pid":6120,"name":"powershell.exe","cmd_line":"powershell.exe -NoProfile -EncodedCommand ZQBjAGgAbwA=",
		"file":{"path":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","name":"powershell.exe","hashes":[{"algorithm":"SHA-256","value":"9f1c"}]},
		"parent_process":{"pid":5000,"name":"mstsc-hijack.exe"}},"device":{"hostname":"WIN-FILE01","domain":"CONTOSO"},"actor":{"user":{"name":"svc_backup","domain":"CONTOSO"}}}`)
	ents := []Entity{
		ent("process--p1", map[string]any{"pid": float64(6120), "name": "powershell.exe"}),
		ent("process--p0", map[string]any{"pid": float64(5000), "name": "mstsc-hijack.exe"}),
		ent("file--f1", map[string]any{"name": "powershell.exe", "hashes": map[string]any{"SHA-256": "9f1c"}}),
	}
	v := Render(Event{ClassUID: 1007, Payload: p}, ents)
	// The user and host have no object in this record, so they read as text
	// and merge with the words around them; the process is the one chip.
	if got := sentenceText(v); got != "svc_backup ran powershell.exe on WIN-FILE01, child of mstsc-hijack.exe" {
		t.Errorf("sentence = %q", got)
	}
	var refs []string
	for _, tok := range v.Sentence {
		if tok.T == "entity" {
			refs = append(refs, tok.Ref)
		}
	}
	if !slices.Equal(refs, []string{"process--p1", "process--p0"}) {
		t.Errorf("entity slots = %v; want the process and its parent", refs)
	}
	for label, want := range map[string]Field{
		"Command line": {Value: "powershell.exe -NoProfile -EncodedCommand ZQBjAGgAbwA=", Mono: true},
		"Parent":       {Value: "mstsc-hijack.exe (pid 5000)", Ref: "process--p0", Mono: true},
		"Image":        {Value: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, Ref: "file--f1", Mono: true},
		"SHA-256":      {Value: "9f1c", Mono: true},
		"PID":          {Value: "6120", Mono: true},
	} {
		got, ok := fieldValue(v, label)
		want.Label = label
		if !ok || got != want {
			t.Errorf("%s = %+v; want %+v", label, got, want)
		}
	}
}

// TestRender_BindsByValue: a slot binds to the object of its type that carries
// its value, never to the type alone — two users on an account change, two
// addresses on a connection.
func TestRender_BindsByValue(t *testing.T) {
	v := Render(Event{ClassUID: 3001, Payload: payload(`{"activity_name":"Disable","user":{"name":"jdoe","domain":"CONTOSO"},"actor":{"user":{"name":"admin","domain":"CONTOSO"}}}`)}, []Entity{
		ent("user-account--target", map[string]any{"account_login": `CONTOSO\jdoe`}),
		ent("user-account--actor", map[string]any{"account_login": `CONTOSO\admin`}),
	})
	if v.Sentence[0].Ref != "user-account--actor" || v.Sentence[2].Ref != "user-account--target" {
		t.Errorf("account change slots = %+v", v.Sentence)
	}
	v = Render(Event{ClassUID: 4001, Payload: payload(`{"src_endpoint":{"ip":"10.0.0.1"},"dst_endpoint":{"ip":"10.0.0.2"}}`)}, []Entity{
		ent("ipv4-addr--dst", map[string]any{"value": "10.0.0.2"}),
	})
	if v.Sentence[0].T != "text" || v.Sentence[0].Text != "10.0.0.1 connected to " || v.Sentence[1].Ref != "ipv4-addr--dst" {
		t.Errorf("network slots = %+v", v.Sentence)
	}
	// A user whose record holds the bare login still binds.
	v = Render(Event{ClassUID: 3002, Payload: payload(`{"user":{"name":"root"},"dst_endpoint":{"hostname":"srv"}}`)}, []Entity{
		ent("user-account--r", map[string]any{"account_login": "root", "account_type": "local"}),
	})
	if v.Sentence[0].Ref != "user-account--r" {
		t.Errorf("bare login did not bind: %+v", v.Sentence[0])
	}
}

// TestRender_BindsByDomain: two same-named accounts of different domains bind
// apart, whatever order the record holds them in; a payload naming no domain
// binds a qualified login only when the record holds exactly one ending in it.
func TestRender_BindsByDomain(t *testing.T) {
	v := Render(Event{ClassUID: 3001, Payload: payload(`{"activity_name":"Disable","user":{"name":"admin","domain":"B"},"actor":{"user":{"name":"admin","domain":"A"}}}`)}, []Entity{
		ent("user-account--target", map[string]any{"account_login": `B\admin`}),
		ent("user-account--actor", map[string]any{"account_login": `A\admin`}),
	})
	if refs := entityRefs(v); !slices.Equal(refs, []string{"user-account--actor", "user-account--target"}) {
		t.Errorf("account change slots = %v", refs)
	}
	// No domain on the payload, two qualified candidates: neither is chosen.
	v = Render(Event{ClassUID: 3002, Payload: payload(`{"user":{"name":"admin"},"dst_endpoint":{"hostname":"srv"}}`)}, []Entity{
		ent("user-account--1", map[string]any{"account_login": `A\admin`}),
		ent("user-account--2", map[string]any{"account_login": `B\admin`}),
	})
	if refs := entityRefs(v); len(refs) != 0 {
		t.Errorf("ambiguous login bound: %v", refs)
	}
	// One qualified candidate for a bare payload name still binds.
	v = Render(Event{ClassUID: 3002, Payload: payload(`{"user":{"name":"admin"},"dst_endpoint":{"hostname":"srv"}}`)}, []Entity{
		ent("user-account--1", map[string]any{"account_login": `A\admin`}),
	})
	if refs := entityRefs(v); !slices.Equal(refs, []string{"user-account--1"}) {
		t.Errorf("single qualified login did not bind: %v", refs)
	}
}

// TestRender_BindsStoredShapes: slots bind to the property shapes the
// normalizers persist — a process held by pid alone, a file held by its path,
// a group held as group_name and directory.
func TestRender_BindsStoredShapes(t *testing.T) {
	v := Render(Event{ClassUID: 4001, Payload: payload(`{"actor":{"process":{"pid":77,"name":"curl.exe"}},"src_endpoint":{"ip":"10.0.0.1"},"dst_endpoint":{"ip":"10.0.0.2"}}`)}, []Entity{
		ent("process--p", map[string]any{"pid": float64(77)}),
	})
	if f, _ := fieldValue(v, "Process"); f.Ref != "process--p" {
		t.Errorf("pid-only process did not bind: %+v", f)
	}
	v = Render(Event{ClassUID: 4001, Payload: payload(`{"actor":{"process":{"pid":77,"name":"curl.exe"}},"src_endpoint":{"ip":"10.0.0.1"}}`)}, []Entity{
		ent("process--q", map[string]any{"pid": float64(77), "name": "other.exe"}),
	})
	if f, _ := fieldValue(v, "Process"); f.Ref != "" {
		t.Errorf("process with a conflicting stored name bound: %+v", f)
	}

	v = Render(Event{ClassUID: 1001, Payload: payload(`{"activity_name":"Create","file":{"path":"C:\\Temp\\payload.dll"},"device":{"hostname":"WS1"}}`)}, []Entity{
		ent("file--f", map[string]any{"name": "", "path": `C:\Temp\payload.dll`}),
	})
	if refs := entityRefs(v); !slices.Equal(refs, []string{"file--f"}) {
		t.Errorf("path-only file did not bind: %v", refs)
	}

	v = Render(Event{ClassUID: 3006, Payload: payload(`{"activity_name":"Add User","user":{"name":"jdoe"},"group":{"name":"Domain Admins","domain":"CONTOSO"}}`)}, []Entity{
		ent("x-group--other", map[string]any{"group_name": "Domain Admins", "directory": "FABRIKAM"}),
		ent("x-group--g", map[string]any{"group_name": "Domain Admins", "directory": "CONTOSO"}),
	})
	if refs := entityRefs(v); !slices.Equal(refs, []string{"x-group--g"}) {
		t.Errorf("group slots = %v; want the CONTOSO group", refs)
	}
	if f, _ := fieldValue(v, "Group"); f.Ref != "x-group--g" {
		t.Errorf("Group field = %+v", f)
	}
}

// TestRender_BindsHostByShortName: an x-host is identified by its short
// hostname (adr/0211), so a record naming the FQDN binds the host whose
// hostname is its first label, in either case, and a host stored before the
// split with the full name still binds; the slot shows the name as reported.
func TestRender_BindsHostByShortName(t *testing.T) {
	for _, stored := range []string{"WS1", "ws1.corp.example"} {
		v := Render(Event{ClassUID: 1001, Payload: payload(`{"activity_name":"Create","file":{"path":"C:\\Temp\\a.dll"},"device":{"hostname":"ws1.Corp.Example."}}`)}, []Entity{
			ent("x-host--h", map[string]any{"hostname": stored}),
		})
		var bound bool
		for _, tok := range v.Sentence {
			if tok.T == "entity" && tok.Ref == "x-host--h" {
				bound = tok.Text == "ws1.Corp.Example."
			}
		}
		if !bound {
			t.Errorf("stored hostname %q: FQDN record did not bind with its reported text: %+v", stored, v.Sentence)
		}
	}
	v := Render(Event{ClassUID: 1001, Payload: payload(`{"activity_name":"Create","file":{"path":"C:\\Temp\\a.dll"},"device":{"hostname":"ws2.corp.example"}}`)}, []Entity{
		ent("x-host--h", map[string]any{"hostname": "ws1"}),
	})
	if refs := entityRefs(v); slices.Contains(refs, "x-host--h") {
		t.Errorf("another short name bound: %v", refs)
	}
}

// TestRender_EmailAttachments counts the attachments and links the normalizer
// reads: email.files[] and email.urls[] (OCSF 1.4.0). The pre-OCSF
// email.attachments is not read (adr/0356).
func TestRender_EmailAttachments(t *testing.T) {
	v := Render(Event{ClassUID: 4009, Payload: payload(`{"email":{"from":"a@x.example","subject":"s","files":[{"name":"invoice.zip"},{"name":"a.pdf"}],"urls":[{"url_string":"https://x.example/"}]}}`)}, nil)
	if f, ok := fieldValue(v, "Attachments"); !ok || f.Value != "2" {
		t.Errorf("Attachments = %+v, %v", f, ok)
	}
	if f, ok := fieldValue(v, "Links"); !ok || f.Value != "1" {
		t.Errorf("Links = %+v, %v", f, ok)
	}
	v = Render(Event{ClassUID: 4009, Payload: payload(`{"email":{"from":"a@x.example","subject":"s","attachments":[{"name":"invoice.zip"}]}}`)}, nil)
	if f, ok := fieldValue(v, "Attachments"); ok {
		t.Errorf("email.attachments was read: %+v", f)
	}
}

func entityRefs(v View) []string {
	var refs []string
	for _, tok := range v.Sentence {
		if tok.T == "entity" {
			refs = append(refs, tok.Ref)
		}
	}
	return refs
}

// TestRender_UnknownClass: the top-level scalars as fields, no sentence, the
// category from category_uid. The clock keys are left out, and a timestamp
// with its _dt twin is shown once, as the twin's text.
func TestRender_UnknownClass(t *testing.T) {
	v := Render(Event{ClassUID: 8001, Payload: payload(`{"class_uid":8001,"category_uid":8,"time":1767225600000,"time_dt":"2026-01-01T00:00:00Z","start_time":1767225600000,"start_time_dt":"2026-01-01T00:00:00Z","end_time":1767225660000,"api":{"operation":"GetObject"},"severity":"Informational","count":2,"ok":true}`)}, nil)
	if v.Sentence != nil {
		t.Errorf("sentence for an unknown class: %+v", v.Sentence)
	}
	labels := make([]string, 0, len(v.Fields))
	for _, f := range v.Fields {
		labels = append(labels, f.Label+"="+f.Value)
	}
	if want := []string{"count=2", "end_time=1767225660000", "ok=true", "severity=Informational", "start_time=2026-01-01T00:00:00Z"}; !slices.Equal(labels, want) {
		t.Errorf("fields = %v; want %v", labels, want)
	}
	if v.Category != CategoryOther {
		t.Errorf("category = %s", v.Category)
	}
	v = Render(Event{ClassUID: 5002, Payload: nil}, nil)
	if v.Fields == nil || v.Category != CategoryAsset {
		t.Errorf("nil payload: fields %v category %s", v.Fields, v.Category)
	}
	// Serialized: fields is a list even when empty, sentence absent.
	js, _ := json.Marshal(v)
	if string(js) != `{"fields":[],"category":"asset"}` {
		t.Errorf("json = %s", js)
	}
}

// TestRender_BaseEvent (adr/0356): an event with no OCSF class is a Base
// Event (class 0), and everything its source said is under unmapped. The view
// shows the scalars there, and the scalars of each object directly under it
// (the vendor's record), each under its own key. A key is shown once, from
// the top of the event first, then unmapped, then the records in name order.
func TestRender_BaseEvent(t *testing.T) {
	fields := func(js string) []string {
		v := Render(Event{ClassUID: 0, Payload: payload(js)}, nil)
		if v.Sentence != nil {
			t.Errorf("sentence for a base event: %+v", v.Sentence)
		}
		out := make([]string, 0, len(v.Fields))
		for _, f := range v.Fields {
			out = append(out, f.Label+"="+f.Value)
		}
		return out
	}
	for name, tc := range map[string]struct {
		js   string
		want []string
	}{
		"a reputation lookup": {
			`{"class_uid":0,"time":1767225600000,"metadata":{"version":"1.1.0","product":{"name":"GreyNoise"}},
			  "unmapped":{"ip":"198.51.100.7","reputation":"benign","greynoise":{"ip":"198.51.100.7","business_service_intelligence":{"found":true,"name":"Example CDN"}}}}`,
			[]string{"ip=198.51.100.7", "reputation=benign"},
		},
		"a vendor record's own scalars": {
			`{"class_uid":0,"unmapped":{"indicator":"evil.example","falcon":{"indicator":"evil.example","type":"domain","malicious_confidence":"high","labels":[{"name":"x"}],"published_date":1767225600}}}`,
			[]string{"indicator=evil.example", "malicious_confidence=high", "published_date=1767225600", "type=domain"},
		},
		"the first place wins": {
			`{"class_uid":0,"severity":"Low","unmapped":{"severity":"vendor-high","b":{"severity":"b","name":"from b"},"a":{"name":"from a"}}}`,
			[]string{"name=from a", "severity=Low"},
		},
		// The class and clock keys are left out at the top of the event only.
		"a vendor's own time and class keys": {
			`{"class_uid":0,"class_name":"Base Event","time":1767225600000,"unmapped":{"type_name":"lookup","falcon":{"time":"2026-01-01T00:00:00Z","class_name":"intel"}}}`,
			[]string{"class_name=intel", "time=2026-01-01T00:00:00Z", "type_name=lookup"},
		},
		"unmapped that is not an object": {`{"class_uid":0,"count":1,"unmapped":"text"}`, []string{"count=1", "unmapped=text"}},
	} {
		if got := fields(tc.js); !slices.Equal(got, tc.want) {
			t.Errorf("%s: fields = %v; want %v", name, got, tc.want)
		}
	}
}

// TestRender_BaseEventFromWrapper: a Base Event as an adapter writes it
// (ocsf.Event) shows what its source said and none of the fields the wrapper
// adds. The activity, severity and status numbers are OCSF's classification,
// so they are not shown, and a source's own key of the same name is.
func TestRender_BaseEventFromWrapper(t *testing.T) {
	raw := ocsf.Event(0, "1.1.0", ocsf.Product("Okta", "Okta Directory"), time.UnixMilli(1767225600000))
	raw["status_id"] = 1
	raw["unmapped"] = map[string]any{"okta": map[string]any{"name": "Everyone", "severity_id": "sev-2"}}
	js, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	v := Render(Event{ClassUID: 0, Payload: payload(string(js))}, nil)
	got := make([]string, 0, len(v.Fields))
	for _, f := range v.Fields {
		got = append(got, f.Label+"="+f.Value)
	}
	if want := []string{"name=Everyone", "severity_id=sev-2"}; !slices.Equal(got, want) {
		t.Errorf("fields = %v; want %v", got, want)
	}
}

// TestRender_Fixtures renders every event of every fixture scenario: a class
// with a template yields a sentence, every event yields a category, and
// nothing panics on real payload shapes.
func TestRender_Fixtures(t *testing.T) {
	files, err := filepath.Glob("../../fixtures/*/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("fixtures: %v (%d files)", err, len(files))
	}
	templated := Classes()
	rendered := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Ocsf map[string]any `json:"ocsf"`
		}
		if json.Unmarshal(raw, &doc) != nil || doc.Ocsf == nil {
			continue // action fixtures and lookup tables
		}
		classUID := toInt(doc.Ocsf["class_uid"])
		v := Render(Event{ClassUID: classUID, ClassName: toString(doc.Ocsf["class_name"]), Payload: doc.Ocsf}, nil)
		rendered++
		if v.Category == "" {
			t.Errorf("%s: no category", f)
		}
		if slices.Contains(templated, classUID) && len(v.Sentence) == 0 {
			t.Errorf("%s: class %d has a template but rendered no sentence", f, classUID)
		}
		if v.Fields == nil {
			t.Errorf("%s: nil fields", f)
		}
	}
	if rendered < 20 {
		t.Errorf("rendered only %d fixture events", rendered)
	}
}

func TestEntityFromObject(t *testing.T) {
	e := EntityFromObject("x-host--1", "x-host", []byte(`{"id":"x-host--1","type":"x-host","properties":{"hostname":"H"},"provenance":{}}`))
	if e.Props["hostname"] != "H" {
		t.Errorf("properties not read: %+v", e)
	}
	e = EntityFromObject("x-host--1", "x-host", []byte(`{"hostname":"flat"}`))
	if e.Props["hostname"] != "flat" {
		t.Errorf("flat object not read: %+v", e)
	}
	if e := EntityFromObject("x", "x", []byte(`nope`)); e.Props != nil {
		t.Errorf("malformed payload: %+v", e)
	}
}

// apiCall is a cloud API call on one bucket, the account named by
// cloud.account.
const apiCall = `{"class_uid":6003,"category_uid":6,"activity_name":"Update",
	"actor":{"user":{"uid":"arn:aws:iam::111122223333:user/deploy-ci","name":"deploy-ci","credential_uid":"AKIAIOSFODNN7EXAMPLE","account":{"uid":"111122223333"}},"session":{"uid":"sess-1"}},
	"api":{"operation":"PutBucketPolicy","service":{"name":"s3.amazonaws.com"}},
	"cloud":{"provider":"AWS","account":{"uid":"111122223333","name":"acme-prod"},"region":"us-east-1"},
	"resources":[{"uid":"arn:aws:s3:::acme-internal-exports","name":"acme-internal-exports","type":"AWS::S3::Bucket"}],
	"src_endpoint":{"ip":"203.0.113.77"},"http_request":{"user_agent":"aws-cli/2.15.0"},"status":"Success","status_id":1}`

func apiEntities() []Entity {
	return []Entity{
		ent("user-account--p", map[string]any{"account_login": "", "user_id": "arn:aws:iam::111122223333:user/deploy-ci", "account_type": "aws", "display_name": "deploy-ci"}),
		ent("x-cloud-account--a", map[string]any{"provider": "aws", "account_uid": "111122223333"}),
		ent("x-cloud-resource--r", map[string]any{"uid": "arn:aws:s3:::acme-internal-exports", "name": "acme-internal-exports"}),
		ent("ipv4-addr--s", map[string]any{"value": "203.0.113.77"}),
	}
}

// TestRender_APIActivity: the sentence with the principal, resource, account
// and source as chips bound to the record, the curated fields, the outcome
// toned, the application category.
func TestRender_APIActivity(t *testing.T) {
	v := Render(Event{ClassUID: 6003, Payload: payload(apiCall)}, apiEntities())
	if got := sentenceText(v); got != "deploy-ci PutBucketPolicy acme-internal-exports in acme-prod · Success" {
		t.Errorf("sentence = %q", got)
	}
	if refs := entityRefs(v); !slices.Equal(refs, []string{"user-account--p", "x-cloud-resource--r", "x-cloud-account--a"}) {
		t.Errorf("sentence refs = %v", refs)
	}
	if v.Category != CategoryApplication {
		t.Errorf("category = %s", v.Category)
	}
	for label, want := range map[string]Field{
		"Principal":  {Label: "Principal", Value: "deploy-ci", Ref: "user-account--p", Mono: true},
		"Operation":  {Label: "Operation", Value: "PutBucketPolicy", Mono: true},
		"Service":    {Label: "Service", Value: "s3.amazonaws.com"},
		"Resource":   {Label: "Resource", Value: "acme-internal-exports", Ref: "x-cloud-resource--r", Mono: true},
		"Account":    {Label: "Account", Value: "acme-prod", Ref: "x-cloud-account--a", Mono: true},
		"Region":     {Label: "Region", Value: "us-east-1"},
		"Outcome":    {Label: "Outcome", Value: "Success", Tone: "ok"},
		"Source":     {Label: "Source", Value: "203.0.113.77", Ref: "ipv4-addr--s", Mono: true},
		"Credential": {Label: "Credential", Value: "AKIAIOSFODNN7EXAMPLE", Mono: true},
		"Session":    {Label: "Session", Value: "sess-1", Mono: true},
		"User agent": {Label: "User agent", Value: "aws-cli/2.15.0"},
	} {
		if f, ok := fieldValue(v, label); !ok || f != want {
			t.Errorf("%s field = %+v; want %+v", label, f, want)
		}
	}
	slots := map[string]SlotValue{}
	for _, s := range v.Slots {
		slots[s.Name] = s
	}
	if slots["actor"].Ref != "user-account--p" || slots["account"].Ref != "x-cloud-account--a" || slots["source"].Ref != "ipv4-addr--s" ||
		slots["operation"].Text != "PutBucketPolicy" || slots["resource"].Text != "acme-internal-exports" || slots["outcome"].Text != "Success" {
		t.Errorf("slots = %+v", v.Slots)
	}
}

// TestRender_APIActivityShapes: without a resource the service reads in
// parentheses; a failure tones bad and names the error; many resources fold to
// the first and a count; an app-only call reads as the app; a resource bound
// by type and name; an account named only by the caller's account.
func TestRender_APIActivityShapes(t *testing.T) {
	v := Render(Event{ClassUID: 6003, Payload: payload(`{"actor":{"user":{"name":"deploy-ci"}},"api":{"operation":"ListUsers","service":{"name":"iam.amazonaws.com"},"response":{"error":"AccessDenied"}},
		"cloud":{"account":{"uid":"111122223333"}},"status_id":2}`)}, apiEntities())
	if got := sentenceText(v); got != "deploy-ci ListUsers (iam.amazonaws.com) in 111122223333 · Failure: AccessDenied" {
		t.Errorf("no-resource failure sentence = %q", got)
	}
	if f, _ := fieldValue(v, "Outcome"); f.Value != "Failure: AccessDenied" || f.Tone != "bad" {
		t.Errorf("Outcome = %+v", f)
	}

	v = Render(Event{ClassUID: 6003, Payload: payload(`{"actor":{"user":{"name":"deploy-ci"}},"api":{"operation":"DeleteObjects"},
		"resources":[{"uid":"arn:aws:s3:::acme-internal-exports"},{"uid":"arn:aws:s3:::b"},{"uid":"arn:aws:s3:::c"}]}`)}, apiEntities())
	if got := sentenceText(v); got != "deploy-ci DeleteObjects arn:aws:s3:::acme-internal-exports, +2 more" {
		t.Errorf("many-resources sentence = %q", got)
	}
	n := 0
	for _, f := range v.Fields {
		if f.Label == "Resource" {
			n++
		}
	}
	if n != 3 {
		t.Errorf("Resource fields = %d; want 3", n)
	}
	for _, s := range v.Slots {
		if s.Name == "resource" && s.Text != "arn:aws:s3:::acme-internal-exports, arn:aws:s3:::b, arn:aws:s3:::c" {
			t.Errorf("resource slot = %+v", s)
		}
	}

	v = Render(Event{ClassUID: 6003, Payload: payload(`{"actor":{"app_name":"backup.example.internal"},"api":{"operation":"GetObject"}}`)}, nil)
	if got := sentenceText(v); got != "backup.example.internal called GetObject" {
		t.Errorf("app-only sentence = %q", got)
	}
	if got := sentenceText(Render(Event{ClassUID: 6003, Payload: map[string]any{}}, nil)); got != "API call" {
		t.Errorf("empty sentence = %q", got)
	}

	v = Render(Event{ClassUID: 6003, Payload: payload(`{"actor":{"user":{"name":"deploy-ci","account":{"uid":"111122223333"}}},"api":{"operation":"PutObject"},
		"resources":[{"name":"acme-logs","type":"AWS::S3::Bucket"}]}`)}, []Entity{
		ent("x-cloud-resource--other", map[string]any{"name": "acme-logs", "type": "AWS::DynamoDB::Table"}),
		ent("x-cloud-resource--n", map[string]any{"name": "acme-logs", "type": "AWS::S3::Bucket"}),
		ent("x-cloud-account--a", map[string]any{"account_uid": "111122223333"}),
	})
	if refs := entityRefs(v); !slices.Equal(refs, []string{"x-cloud-resource--n", "x-cloud-account--a"}) {
		t.Errorf("name-bound refs = %v", refs)
	}
}

// TestRender_APIActivityPrincipal: the principal binds by the durable id the
// record keyed it on: the user's uid with a name, the uid alone (the chip
// reads the uid), the session issuer for a role.
func TestRender_APIActivityPrincipal(t *testing.T) {
	const arn = "arn:aws:iam::111122223333:user/deploy-ci"
	byID := []Entity{
		ent("user-account--other", map[string]any{"account_login": "", "user_id": "arn:aws:iam::444455556666:user/deploy-ci"}),
		ent("user-account--p", map[string]any{"account_login": "", "user_id": arn}),
	}
	v := Render(Event{ClassUID: 6003, Payload: payload(`{"actor":{"user":{"uid":"` + arn + `","name":"deploy-ci"}},"api":{"operation":"PutObject"}}`)}, byID)
	if v.Sentence[0] != (Token{T: "entity", Ref: "user-account--p", Text: "deploy-ci"}) {
		t.Errorf("name+uid principal = %+v", v.Sentence[0])
	}
	v = Render(Event{ClassUID: 6003, Payload: payload(`{"actor":{"user":{"uid":"` + arn + `"}},"api":{"operation":"PutObject"}}`)}, byID)
	if v.Sentence[0] != (Token{T: "entity", Ref: "user-account--p", Text: arn}) || sentenceText(v) != arn+" PutObject" {
		t.Errorf("uid-only principal = %+v (%q)", v.Sentence, sentenceText(v))
	}
	role := []Entity{ent("user-account--role", map[string]any{"account_login": "", "user_id": "arn:aws:iam::111122223333:role/Deploy"})}
	v = Render(Event{ClassUID: 6003, Payload: payload(`{"actor":{"user":{"uid":"arn:aws:sts::111122223333:assumed-role/Deploy/i-0abc","name":"Deploy"},
		"session":{"issuer":"arn:aws:iam::111122223333:role/Deploy","uid":"i-0abc"}},"api":{"operation":"PutObject"}}`)}, role)
	if v.Sentence[0] != (Token{T: "entity", Ref: "user-account--role", Text: "Deploy"}) {
		t.Errorf("issuer-bound role = %+v", v.Sentence[0])
	}
}

// TestRender_APIActivityOutcomeAndCaller: an error tones the outcome bad with
// no status given; a cross-account call shows the caller's account beside the
// account the call ran in, and a same-account call does not.
func TestRender_APIActivityOutcomeAndCaller(t *testing.T) {
	v := Render(Event{ClassUID: 6003, Payload: payload(`{"actor":{"user":{"name":"deploy-ci"}},"api":{"operation":"ListUsers","response":{"error":"AccessDenied"}}}`)}, nil)
	if f, _ := fieldValue(v, "Outcome"); f.Value != "Failure: AccessDenied" || f.Tone != "bad" {
		t.Errorf("Outcome without status = %+v", f)
	}
	ents := []Entity{
		ent("x-cloud-account--target", map[string]any{"account_uid": "444455556666"}),
		ent("x-cloud-account--home", map[string]any{"account_uid": "111122223333"}),
	}
	v = Render(Event{ClassUID: 6003, Payload: payload(`{"actor":{"user":{"name":"deploy-ci","account":{"uid":"111122223333","name":"acme-prod"}}},
		"api":{"operation":"GetObject"},"cloud":{"account":{"uid":"444455556666","name":"acme-shared"}}}`)}, ents)
	if f, _ := fieldValue(v, "Account"); f.Value != "acme-shared" || f.Ref != "x-cloud-account--target" {
		t.Errorf("Account = %+v", f)
	}
	if f, ok := fieldValue(v, "Caller account"); !ok || f.Value != "acme-prod" || f.Ref != "x-cloud-account--home" {
		t.Errorf("Caller account = %+v, %v", f, ok)
	}
	v = Render(Event{ClassUID: 6003, Payload: payload(apiCall)}, apiEntities())
	if _, ok := fieldValue(v, "Caller account"); ok {
		t.Error("same-account call shows a caller account")
	}
}

// TestRender_APIActivityResourceKey: the resource slot keys on the bound
// object, so two resources with one display name stay apart, one resource
// seen twice folds, and an unbound resource keys by its exact text.
func TestRender_APIActivityResourceKey(t *testing.T) {
	key := func(uid string, ents []Entity) SlotValue {
		v := Render(Event{ClassUID: 6003, Payload: payload(`{"api":{"operation":"GetObject"},"resources":[{"uid":"` + uid + `","name":"exports"}]}`)}, ents)
		for _, s := range v.Slots {
			if s.Name == "resource" {
				return s
			}
		}
		t.Fatal("no resource slot")
		return SlotValue{}
	}
	a := key("arn:aws:s3:::exports", []Entity{ent("x-cloud-resource--a", map[string]any{"uid": "arn:aws:s3:::exports"})})
	b := key("arn:aws:s3:::other/exports", []Entity{ent("x-cloud-resource--b", map[string]any{"uid": "arn:aws:s3:::other/exports"})})
	again := key("arn:aws:s3:::exports", []Entity{ent("x-cloud-resource--a", map[string]any{"uid": "arn:aws:s3:::exports"})})
	if a.Text != "exports" || b.Text != "exports" {
		t.Errorf("texts = %q, %q; want the display name", a.Text, b.Text)
	}
	if a.Identity == b.Identity {
		t.Error("two bound resources with one display name fold")
	}
	if a.Identity != again.Identity {
		t.Error("one bound resource seen twice does not fold")
	}
	u1, u2 := key("arn:aws:s3:::x", nil), key("arn:aws:s3:::y", nil)
	if u1.Identity != u2.Identity || u1.Identity == a.Identity {
		t.Errorf("unbound keys = %q, %q; want the exact text, apart from the bound one", u1.Identity, u2.Identity)
	}
	upper := Render(Event{ClassUID: 6003, Payload: payload(`{"resources":[{"uid":"arn:aws:s3:::x","name":"Exports"}]}`)}, nil)
	for _, s := range upper.Slots {
		if s.Name == "resource" && s.Identity == u1.Identity {
			t.Error("unbound resource text folded for case")
		}
	}
}

// TestRender_APIActivityResourceByName: a by-name resource binds on the
// identity tuple: the name exactly, the region without case, so three
// resources that differ only by name case or region each bind their own.
func TestRender_APIActivityResourceByName(t *testing.T) {
	v := Render(Event{ClassUID: 6003, Payload: payload(`{"api":{"operation":"DescribeTable"},"resources":[
		{"name":"Orders","type":"AWS::DynamoDB::Table","region":"us-east-1"},
		{"name":"orders","type":"AWS::DynamoDB::Table","region":"US-EAST-1"},
		{"name":"Orders","type":"AWS::DynamoDB::Table","region":"us-west-2"}]}`)}, []Entity{
		ent("x-cloud-resource--east", map[string]any{"name": "Orders", "type": "AWS::DynamoDB::Table", "region": "us-east-1"}),
		ent("x-cloud-resource--lower", map[string]any{"name": "orders", "type": "AWS::DynamoDB::Table", "region": "us-east-1"}),
		ent("x-cloud-resource--west", map[string]any{"name": "Orders", "type": "AWS::DynamoDB::Table", "region": "us-west-2"}),
	})
	var refs []string
	for _, f := range v.Fields {
		if f.Label == "Resource" {
			refs = append(refs, f.Ref)
		}
	}
	if want := []string{"x-cloud-resource--east", "x-cloud-resource--lower", "x-cloud-resource--west"}; !slices.Equal(refs, want) {
		t.Errorf("resource refs = %v; want %v", refs, want)
	}
}

// TestRender_APIActivityResourceGroups: two same-named VMs in one region and
// two resource groups each bind their own chip.
func TestRender_APIActivityResourceGroups(t *testing.T) {
	v := Render(Event{ClassUID: 6003, Payload: payload(`{"api":{"operation":"Restart"},"resources":[
		{"name":"api","type":"Microsoft.Compute/virtualMachines","region":"eastus","group":{"name":"rg-a"}},
		{"name":"api","type":"Microsoft.Compute/virtualMachines","region":"eastus","group":{"name":"RG-B"}}]}`)}, []Entity{
		ent("x-cloud-resource--a", map[string]any{"name": "api", "type": "Microsoft.Compute/virtualMachines", "region": "eastus", "group": "rg-a"}),
		ent("x-cloud-resource--b", map[string]any{"name": "api", "type": "Microsoft.Compute/virtualMachines", "region": "eastus", "group": "rg-b"}),
	})
	var refs []string
	for _, f := range v.Fields {
		if f.Label == "Resource" {
			refs = append(refs, f.Ref)
		}
	}
	if want := []string{"x-cloud-resource--a", "x-cloud-resource--b"}; !slices.Equal(refs, want) {
		t.Errorf("resource refs = %v; want %v", refs, want)
	}
}

// TestRender_APIActivityResourceZones: one name in two zones binds to two
// resources, the zone matched without case like the region.
func TestRender_APIActivityResourceZones(t *testing.T) {
	v := Render(Event{ClassUID: 6003, Payload: payload(`{"api":{"operation":"Stop"},"resources":[
		{"name":"api","type":"compute.googleapis.com/Instance","region":"us-central1","zone":"us-central1-a"},
		{"name":"api","type":"compute.googleapis.com/Instance","region":"us-central1","zone":"US-CENTRAL1-B"}]}`)}, []Entity{
		ent("x-cloud-resource--a", map[string]any{"name": "api", "type": "compute.googleapis.com/Instance", "region": "us-central1", "zone": "us-central1-a"}),
		ent("x-cloud-resource--b", map[string]any{"name": "api", "type": "compute.googleapis.com/Instance", "region": "us-central1", "zone": "us-central1-b"}),
	})
	if refs, want := resourceRefs(v), []string{"x-cloud-resource--a", "x-cloud-resource--b"}; !slices.Equal(refs, want) {
		t.Errorf("resource refs = %v; want %v", refs, want)
	}
}

// TestRender_APIActivityResourceNamespaceCase: namespaces differing only in
// case bind to two resources, the namespace matched exactly like the name.
func TestRender_APIActivityResourceNamespaceCase(t *testing.T) {
	v := Render(Event{ClassUID: 6003, Payload: payload(`{"api":{"operation":"GetTable"},"resources":[
		{"name":"orders","type":"AWS::Glue::Table","namespace":"sourcea"},
		{"name":"orders","type":"AWS::Glue::Table","namespace":"SourceA"}]}`)}, []Entity{
		ent("x-cloud-resource--upper", map[string]any{"name": "orders", "type": "AWS::Glue::Table", "namespace": "SourceA"}),
		ent("x-cloud-resource--lower", map[string]any{"name": "orders", "type": "AWS::Glue::Table", "namespace": "sourcea"}),
	})
	if refs, want := resourceRefs(v), []string{"x-cloud-resource--lower", "x-cloud-resource--upper"}; !slices.Equal(refs, want) {
		t.Errorf("resource refs = %v; want %v", refs, want)
	}
}

func resourceRefs(v View) []string {
	var refs []string
	for _, f := range v.Fields {
		if f.Label == "Resource" {
			refs = append(refs, f.Ref)
		}
	}
	return refs
}

// inventorySnapshot is a cloud resource configuration snapshot of one bucket.
const inventorySnapshot = `{"class_uid":5023,"category_uid":5,"activity_id":2,"activity_name":"Collect",
	"cloud":{"provider":"AWS","account":{"uid":"111122223333","name":"acme-prod"},"region":"us-east-1"},
	"resources":[{"uid":"arn:aws:s3:::acme-internal-exports","name":"acme-internal-exports","type":"AWS::S3::Bucket","region":"us-east-1",
		"data":{"versioning":"Suspended","policy":{"Version":"2012-10-17"},"encryption":"AES256"},
		"labels":["env:prod","data-class:internal"],"criticality":"High","owner":{"name":"data-platform"},
		"created_time":1740916800000,"created_time_dt":"2025-03-02T12:00:00Z","modified_time":1784021280000,"modified_time_dt":"2026-07-14T09:28:00Z"}]}`

// TestRender_CloudInventory: the resource and its account as chips bound to
// the record, the type and region in the sentence, the configuration as its
// top-level keys, the asset category; with no provider (so nothing minted)
// the same sentence reads with unbound text.
func TestRender_CloudInventory(t *testing.T) {
	ents := []Entity{
		ent("x-cloud-account--a", map[string]any{"provider": "aws", "account_uid": "111122223333"}),
		ent("x-cloud-resource--r", map[string]any{"uid": "arn:aws:s3:::acme-internal-exports", "name": "acme-internal-exports"}),
	}
	v := Render(Event{ClassUID: 5023, Payload: payload(inventorySnapshot)}, ents)
	const want = "acme-internal-exports (AWS::S3::Bucket) in acme-prod, us-east-1"
	if got := sentenceText(v); got != want {
		t.Errorf("sentence = %q", got)
	}
	if refs := entityRefs(v); !slices.Equal(refs, []string{"x-cloud-resource--r", "x-cloud-account--a"}) {
		t.Errorf("sentence refs = %v", refs)
	}
	if v.Category != CategoryAsset {
		t.Errorf("category = %s", v.Category)
	}
	for label, want := range map[string]Field{
		"Resource":      {Label: "Resource", Value: "acme-internal-exports", Ref: "x-cloud-resource--r", Mono: true},
		"Type":          {Label: "Type", Value: "AWS::S3::Bucket", Mono: true},
		"Account":       {Label: "Account", Value: "acme-prod", Ref: "x-cloud-account--a", Mono: true},
		"Region":        {Label: "Region", Value: "us-east-1"},
		"Owner":         {Label: "Owner", Value: "data-platform"},
		"Created":       {Label: "Created", Value: "2025-03-02T12:00:00Z"},
		"Modified":      {Label: "Modified", Value: "2026-07-14T09:28:00Z"},
		"Criticality":   {Label: "Criticality", Value: "High", Tone: severityTone("High")},
		"Labels":        {Label: "Labels", Value: "env:prod, data-class:internal"},
		"Configuration": {Label: "Configuration", Value: "encryption, policy, versioning"},
	} {
		if f, ok := fieldValue(v, label); !ok || f != want {
			t.Errorf("%s field = %+v; want %+v", label, f, want)
		}
	}
	slots := map[string]SlotValue{}
	for _, s := range v.Slots {
		slots[s.Name] = s
	}
	if slots["resource"].Ref != "x-cloud-resource--r" || slots["account"].Ref != "x-cloud-account--a" ||
		slots["type"].Text != "AWS::S3::Bucket" || slots["region"].Text != "us-east-1" || slots["owner"].Text != "data-platform" {
		t.Errorf("slots = %+v", v.Slots)
	}

	// The times are read from the epoch milliseconds (OCSF timestamp_t)
	// first, and from the _dt twin when a source writes only that.
	for name, edit := range map[string]func(first map[string]any){
		"the number alone": func(first map[string]any) {
			delete(first, "created_time_dt")
			delete(first, "modified_time_dt")
		},
		"the _dt twin alone": func(first map[string]any) {
			delete(first, "created_time")
			delete(first, "modified_time")
		},
	} {
		ms := payload(inventorySnapshot)
		edit(ms["resources"].([]any)[0].(map[string]any))
		ev := Render(Event{ClassUID: 5023, Payload: ms}, ents)
		for label, want := range map[string]string{"Created": "2025-03-02T12:00:00Z", "Modified": "2026-07-14T09:28:00Z"} {
			if f, ok := fieldValue(ev, label); !ok || f.Value != want {
				t.Errorf("%s: %s field = %+v; want %s", name, label, f, want)
			}
		}
	}

	p := payload(inventorySnapshot)
	delete(p["cloud"].(map[string]any), "provider")
	v = Render(Event{ClassUID: 5023, Payload: p}, nil)
	if got := sentenceText(v); got != want {
		t.Errorf("no provider: sentence = %q; want %q", got, want)
	}
	if refs := entityRefs(v); len(refs) != 0 {
		t.Errorf("no provider: sentence refs = %v; want none", refs)
	}
}

// TestOcsfTimestamp repeats capability's TestOcsfTimestamp table: the view
// and the normalizers read a timestamp by one rule. The field holds epoch
// milliseconds, and its _dt twin an RFC 3339 string. The number is read
// first, and a string in the number's field is not read (adr/0356).
func TestOcsfTimestamp(t *testing.T) {
	for _, tc := range []struct {
		in, dt any
		want   string
	}{
		{float64(1784021280000), nil, "2026-07-14T09:28:00Z"},
		{int64(1784021280000), nil, "2026-07-14T09:28:00Z"},
		{1784021280123, nil, "2026-07-14T09:28:00.123Z"},
		{json.Number("1784021280000"), nil, "2026-07-14T09:28:00Z"},
		{nil, " 2025-03-02T12:00:00Z ", "2025-03-02T12:00:00Z"},
		{nil, "2025-03-02T13:00:00+01:00", "2025-03-02T12:00:00Z"},
		{float64(1784021280000), "2025-03-02T12:00:00Z", "2026-07-14T09:28:00Z"}, // the number first
		{float64(1.5), "2025-03-02T12:00:00Z", "2025-03-02T12:00:00Z"},           // not a timestamp: the twin
		{"2025-03-02T12:00:00Z", nil, ""},                                        // a string is not a timestamp_t
		{nil, "yesterday", ""},
		{float64(1.5), nil, ""}, {float64(1 << 53), nil, ""}, {true, nil, ""}, {nil, nil, ""},
	} {
		obj := map[string]any{}
		if tc.in != nil {
			obj["created_time"] = tc.in
		}
		if tc.dt != nil {
			obj["created_time_dt"] = tc.dt
		}
		if got := ocsfTimestamp(obj, "created_time"); got != tc.want {
			t.Errorf("ocsfTimestamp(%v, %v) = %q, want %q", tc.in, tc.dt, got, tc.want)
		}
	}
}

// TestRender_OCSFFields (adr/0356): the curated fields of the classes whose
// paths moved to OCSF's, read from the OCSF field and from each fallback.
func TestRender_OCSFFields(t *testing.T) {
	for _, tc := range []struct {
		name    string
		class   int
		payload string
		want    map[string]string // label → value; "" means the field is absent
	}{
		{"registry value", 201002, `{"reg_value":{"path":"HKLM\\SOFTWARE\\Run","name":"Updater","data":"C:\\u.exe","type":"REG_SZ"}}`,
			map[string]string{"Key": `HKEY_LOCAL_MACHINE\SOFTWARE\Run`, "Value": "Updater", "Data": `C:\u.exe`, "Type": "REG_SZ"}},
		{"registry value, typed data only", 201002, `{"reg_value":{"path":"HKLM\\SOFTWARE\\Run","name":"Updater","reg_string_data":"C:\\u.exe","type":"REG_SZ"}}`,
			map[string]string{"Data": `C:\u.exe`}},
		{"registry value, integer data", 201002, `{"reg_value":{"path":"HKLM\\SOFTWARE\\Run","name":"Start","reg_string_data":"","reg_integer_data":4}}`,
			map[string]string{"Data": "4"}},
		{"registry value, a typed list as JSON", 201002, `{"reg_value":{"path":"HKLM\\SOFTWARE\\Run","name":"Libs","reg_string_list_data":["a.dll","b.dll"]}}`,
			map[string]string{"Data": `["a.dll","b.dll"]`}},
		{"registry key reads no value", 201001, `{"reg_key":{"path":"HKLM\\SOFTWARE\\Run"},"reg_value":{"name":"Updater","data":"x"}}`,
			map[string]string{"Key": `HKEY_LOCAL_MACHINE\SOFTWARE\Run`, "Value": "", "Data": ""}},
		{"job at the floor", 1006, `{"job":{"name":"Updater","file":{"path":"\\Microsoft\\Updater"},"cmd_line":"u.exe -q"}}`,
			map[string]string{"Job": "Updater", "Path": `\Microsoft\Updater`, "Command line": "u.exe -q"}},
		{"job at the ceiling", 1006, `{"job":{"name":"Updater","job_actions":[{"cmd_line":"u.exe -q"}],"cmd_line":"old.exe"}}`,
			map[string]string{"Command line": "u.exe -q"}},
		{"job action without a command", 1006, `{"job":{"name":"Updater","job_actions":[{"type_id":2}],"cmd_line":"old.exe"}}`,
			map[string]string{"Command line": "old.exe"}},
		{"account change reads no group or privileges", 3001, `{"user":{"name":"jdoe"},"group":{"name":"Domain Admins"},"privileges":["SeDebugPrivilege"]}`,
			map[string]string{"Account": "jdoe", "Group": "", "Privileges": ""}},
		{"user access", 3005, `{"user":{"name":"jdoe"},"group":{"name":"Domain Admins"},"privileges":["SeDebugPrivilege","SeBackupPrivilege"]}`,
			map[string]string{"Group": "", "Privileges": "SeDebugPrivilege, SeBackupPrivilege"}},
		{"group management", 3006, `{"user":{"name":"jdoe"},"group":{"name":"Domain Admins"},"privileges":["SeDebugPrivilege"]}`,
			map[string]string{"Group": "Domain Admins", "Privileges": "SeDebugPrivilege"}},
		{"group management users[]", 3006, `{"users":[{"name":"jdoe","domain":"CONTOSO"},{"name":"asmith"}],"user":{"name":"old"},"group":{"name":"Domain Admins"}}`,
			map[string]string{"Account": `CONTOSO\jdoe`}},
		{"group management empty users[]", 3006, `{"users":[],"user":{"name":"jdoe"},"group":{"name":"Domain Admins"}}`,
			map[string]string{"Account": "jdoe"}},
		{"email click", 4012, `{"email_uid":"gw-1","url":{"url_string":"https://x.example/"}}`,
			map[string]string{"URL": "https://x.example/", "Thread id": "gw-1", "Email id": ""}},
		{"email attachment", 4011, `{"email_uid":"gw-1","file":{"name":"a.exe","path":"C:\\a.exe","size":12,"hashes":[{"algorithm":"SHA-256","value":"beef"}]}}`,
			map[string]string{"File": `C:\a.exe`, "SHA-256": "beef", "Size": "12 bytes", "Thread id": "gw-1", "Email id": ""}},
		{"hashes in the object form are not read", 1001, `{"file":{"name":"a.exe","hashes":{"SHA-256":"beef"}}}`,
			map[string]string{"File": "a.exe", "SHA-256": ""}},
		{"detection by ids", 2004, `{"finding_info":{"uid":"det-1","title":"Beacon","types":["T1071","T1105"]},"severity_id":5,"severity":"ignored","confidence_id":2,"confidence":"ignored",
			"evidences":[{"actor":{"user":{"name":"jdoe","domain":"CONTOSO"}}}]}`,
			map[string]string{"Finding": "Beacon", "Finding id": "det-1", "Severity": "Critical", "Confidence": "Medium", "Techniques": "T1071, T1105", "User": `CONTOSO\jdoe`}},
		{"detection by score and captions", 2004, `{"finding_info":{"uid":"det-1"},"severity":"Elevated","confidence_score":73,"confidence_id":1,"evidences":[{"user":{"name":"target"}}]}`,
			map[string]string{"Severity": "Elevated", "Confidence": "73", "User": "target"}},
		{"detection with a score of 0", 2004, `{"finding_info":{"uid":"det-1"},"confidence_score":0,"confidence_id":3}`,
			map[string]string{"Confidence": "0"}},
		{"detection with a score above 100", 2004, `{"finding_info":{"uid":"det-1"},"confidence_score":250}`,
			map[string]string{"Confidence": "100"}},
		{"device last seen, the number", 5001, `{"device":{"hostname":"H","last_seen_time":1784021280000,"last_seen_time_dt":"2001-01-01T00:00:00Z"}}`,
			map[string]string{"Last seen": "2026-07-14T09:28:00Z"}},
		{"device last seen, the _dt twin", 5001, `{"device":{"hostname":"H","last_seen_time_dt":"2026-07-14T09:28:00Z"}}`,
			map[string]string{"Last seen": "2026-07-14T09:28:00Z"}},
		{"detection by confidence caption", 2004, `{"finding_info":{"uid":"det-1"},"confidence_id":99,"confidence":"Certain"}`,
			map[string]string{"Confidence": "Certain", "Severity": "", "User": ""}},
		{"case", 2005, `{"finding_info_list":[{"uid":"INC1","title":"T"}],"assignee_group":{"name":"SOC","uid":"g"},"src_url":"https://sor.example/INC1","unmapped":{"sor":"servicenow","link":"https://old.example"}}`,
			map[string]string{"Case": "INC1", "Title": "T", "Assigned to": "SOC", "System": "servicenow", "Link": "https://sor.example/INC1"}},
		{"device inventory", 5001, `{"device":{"hostname":"H","criticality":"low"},"unmapped":{"criticality":"high"}}`,
			map[string]string{"Criticality": "high"}},
		{"network process", 4001, `{"actor":{"process":{"pid":7,"name":"curl.exe"}},"process":{"pid":9,"name":"old.exe"},"dst_endpoint":{"ip":"10.0.0.2"}}`,
			map[string]string{"Process": "curl.exe"}},
		{"api application at the ceiling", 6003, `{"cloud":{"provider":"AWS"},"actor":{"application":{"name":"backup"},"app_name":"old"},"api":{"operation":"Get"}}`,
			map[string]string{"Principal": "backup"}},
		{"api application at the floor", 6003, `{"cloud":{"provider":"AWS"},"actor":{"app_name":"old"},"api":{"operation":"Get"}}`,
			map[string]string{"Principal": "old"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := Render(Event{ClassUID: tc.class, Payload: payload(tc.payload)}, nil)
			for label, want := range tc.want {
				f, ok := fieldValue(v, label)
				if want == "" {
					if ok {
						t.Errorf("%s = %q; want the field absent", label, f.Value)
					}
					continue
				}
				if !ok || f.Value != want {
					t.Errorf("%s = %q (present %v); want %q", label, f.Value, ok, want)
				}
			}
		})
	}

	// Every command line of a job with several actions is shown.
	v := Render(Event{ClassUID: 1006, Payload: payload(`{"job":{"name":"Updater","job_actions":[{"cmd_line":"a.exe"},{"cmd_line":"b.exe"}]}}`)}, nil)
	var cmds []string
	for _, f := range v.Fields {
		if f.Label == "Command line" {
			cmds = append(cmds, f.Value)
		}
	}
	if !slices.Equal(cmds, []string{"a.exe", "b.exe"}) {
		t.Errorf("command lines = %v; want both", cmds)
	}
}

// TestRender_RegistryBindsTheKey (adr/0341): both registry classes give the
// full path, and bind the windows-registry-key whose expanded key and value
// name match, letter case aside. A key event binds the key with no value.
func TestRender_RegistryBindsTheKey(t *testing.T) {
	ents := []Entity{
		ent("windows-registry-key--run", map[string]any{"key": `HKEY_LOCAL_MACHINE\SOFTWARE\Run`}),
		ent("windows-registry-key--updater", map[string]any{"key": `HKEY_LOCAL_MACHINE\SOFTWARE\Run`, "values": []any{map[string]any{"name": "Updater"}}}),
		ent("windows-registry-key--other", map[string]any{"key": `HKEY_CURRENT_USER\SOFTWARE\Run`}),
		ent("windows-registry-key--default", map[string]any{"key": `HKEY_LOCAL_MACHINE\SOFTWARE\Run`, "values": []any{map[string]any{"name": ""}}}),
	}
	for js, want := range map[string]struct {
		class int
		ref   string
	}{
		`{"reg_key":{"path":"hklm\\software\\run"}}`:                                   {201001, "windows-registry-key--run"},
		`{"reg_value":{"path":"HKLM\\SOFTWARE\\Run","name":"updater"}}`:                {201002, "windows-registry-key--updater"},
		`{"reg_value":{"path":"\\REGISTRY\\MACHINE\\SOFTWARE\\Run","name":"Updater"}}`: {201002, "windows-registry-key--updater"},
		`{"reg_value":{"path":"HKLM\\SOFTWARE\\Run","is_default":true}}`:               {201002, "windows-registry-key--default"},
	} {
		v := Render(Event{ClassUID: want.class, Payload: payload(js)}, ents)
		if f, _ := fieldValue(v, "Key"); f.Ref != want.ref {
			t.Errorf("%s: Key = %+v; want it bound to %s", js, f, want.ref)
		}
	}
}

// TestCategory_ExtensionClass: a Windows-extension class number carries the
// extension above the category, so 201001 is a category-1 class.
func TestCategory_ExtensionClass(t *testing.T) {
	for class, want := range map[int]string{201001: CategoryProcess, 201002: CategoryProcess, 1005: CategoryProcess, 4012: CategoryNetwork, 2005: CategoryCase, 3006: CategoryAuth} {
		if got := Category(class, nil); got != want {
			t.Errorf("Category(%d) = %s; want %s", class, got, want)
		}
	}
}

// TestRender_CloudDetection (03 §4.12, adr/0356): a cloud finding reads on
// its resource and in its account, bound as on an API call, and the
// artifact's actor.user binds to the provider's principal by its id.
func TestRender_CloudDetection(t *testing.T) {
	const finding = `{"class_uid":2004,"metadata":{"product":{"vendor_name":"cspm"}},"finding_info":{"uid":"det-1","title":"Bucket made public"},"severity_id":4,
		"cloud":{"provider":"AWS","account":{"uid":"111122223333","name":"acme-prod"}},
		"resources":[{"uid":"arn:aws:s3:::acme-internal-exports","name":"acme-internal-exports","type":"AWS::S3::Bucket"},{"name":"second"}],
		"evidences":[{"src_endpoint":{"ip":"203.0.113.77"}},{"actor":{"user":{"uid":"arn:aws:iam::111122223333:user/deploy-ci","name":"deploy-ci"}}}]}`
	v := Render(Event{ClassUID: 2004, Payload: payload(finding)}, apiEntities())
	if got := sentenceText(v); got != "cspm detected “Bucket made public” (high) on acme-internal-exports, +1 more in acme-prod" {
		t.Errorf("sentence = %q", got)
	}
	for label, want := range map[string]Field{
		"Resource": {Label: "Resource", Value: "acme-internal-exports", Ref: "x-cloud-resource--r", Mono: true},
		"Account":  {Label: "Account", Value: "acme-prod", Ref: "x-cloud-account--a", Mono: true},
		"User":     {Label: "User", Value: "deploy-ci", Ref: "user-account--p", Mono: true},
	} {
		if f, ok := fieldValue(v, label); !ok || f != want {
			t.Errorf("%s field = %+v; want %+v", label, f, want)
		}
	}
	slots := map[string]SlotValue{}
	for _, s := range v.Slots {
		slots[s.Name] = s
	}
	if slots["user"].Ref != "user-account--p" || slots["account"].Ref != "x-cloud-account--a" || slots["resource"].Text != "acme-internal-exports, second" {
		t.Errorf("slots = %+v", v.Slots)
	}

	// The account by the deprecated project_uid; a host still leads the
	// sentence when the finding has one.
	v = Render(Event{ClassUID: 2004, Payload: payload(`{"finding_info":{"title":"T"},"device":{"hostname":"WS1"},
		"cloud":{"provider":"AWS","project_uid":"111122223333"},"resources":[{"name":"r1"}]}`)}, apiEntities())
	if got := sentenceText(v); got != "Detected “T” on WS1 in 111122223333" {
		t.Errorf("sentence = %q", got)
	}
	if f, _ := fieldValue(v, "Account"); f.Ref != "x-cloud-account--a" {
		t.Errorf("Account = %+v; want it bound by project_uid", f)
	}

	// With no cloud provider, actor.user is a directory login, bound by its
	// login, never by an id.
	ents := []Entity{
		ent("user-account--login", map[string]any{"account_login": `CONTOSO\jdoe`}),
		ent("user-account--id", map[string]any{"account_login": "", "user_id": "u-1"}),
	}
	v = Render(Event{ClassUID: 2004, Payload: payload(`{"finding_info":{"title":"T"},"evidences":[{"actor":{"user":{"uid":"u-1","name":"jdoe","domain":"CONTOSO"}}}]}`)}, ents)
	if f, _ := fieldValue(v, "User"); f.Ref != "user-account--login" || f.Value != `CONTOSO\jdoe` {
		t.Errorf("User = %+v; want the directory login", f)
	}
	// With one, an actor.user the normalizer read as a login on the host
	// (no provider signal, adr/0359) binds by its login.
	v = Render(Event{ClassUID: 2004, Payload: payload(`{"finding_info":{"title":"T"},"cloud":{"provider":"AWS"},"evidences":[{"actor":{"user":{"name":"jdoe","domain":"CONTOSO"}}}]}`)}, ents)
	if f, _ := fieldValue(v, "User"); f.Ref != "user-account--login" || f.Value != `CONTOSO\jdoe` {
		t.Errorf("User = %+v; want the host login", f)
	}
	// A Windows account type on a cloud finding is a login on the host,
	// shown qualified and bound by its login, never by its id.
	sidEnts := []Entity{
		ent("user-account--login", map[string]any{"account_login": `CONTOSO\jdoe`}),
		ent("user-account--id", map[string]any{"account_login": "", "user_id": "S-1-5-21-7-1101"}),
	}
	v = Render(Event{ClassUID: 2004, Payload: payload(`{"finding_info":{"title":"T"},"cloud":{"provider":"AWS"},
		"evidences":[{"actor":{"user":{"name":"jdoe","domain":"CONTOSO","uid":"S-1-5-21-7-1101","account":{"type_id":2}}}}]}`)}, sidEnts)
	if f, _ := fieldValue(v, "User"); f.Ref != "user-account--login" || f.Value != `CONTOSO\jdoe` {
		t.Errorf("User = %+v; want the Windows login bound by its login", f)
	}
	// With one, the artifact's own user is still a directory login.
	v = Render(Event{ClassUID: 2004, Payload: payload(`{"finding_info":{"title":"T"},"cloud":{"provider":"AWS"},"evidences":[{"user":{"name":"jdoe","domain":"CONTOSO"}}]}`)}, ents)
	if f, _ := fieldValue(v, "User"); f.Ref != "user-account--login" {
		t.Errorf("User = %+v; want the directory login", f)
	}
}

// TestRender_FixtureActivities: the shipped events whose activity is an OCSF
// caption read as that activity, not as the caption in lower case. A fixture
// that goes back to a source's own word ("GROUP_ADD") fails here.
func TestRender_FixtureActivities(t *testing.T) {
	for file, want := range map[string]string{
		"privilege-01.json":          "svc_backup added svc_backup to Administrators on WIN-FILE01",
		"persistence-runkey-01.json": `powershell.exe set value SyncHost of HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Run on WIN-FILE01`,
		"logon-1.json":               "jdoe RDP logon to WIN-FILE01 from 10.0.4.55",
	} {
		raw, err := os.ReadFile(filepath.Join("../../fixtures/lateral-movement-via-rdp", file))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Ocsf map[string]any `json:"ocsf"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		v := Render(Event{ClassUID: toInt(doc.Ocsf["class_uid"]), Payload: doc.Ocsf}, nil)
		if got := sentenceText(v); got != want {
			t.Errorf("%s reads %q; want %q", file, got, want)
		}
	}
}

// TestRender_GroupMembers (03 §4.9, adr/0359): each entry of a group
// change's users[] binds to its own account, and the target slot holds them
// all, so two changes of different members never fold together.
func TestRender_GroupMembers(t *testing.T) {
	ents := []Entity{
		ent("user-account--jdoe", map[string]any{"account_login": `CONTOSO\jdoe`}),
		ent("user-account--asmith", map[string]any{"account_login": `CONTOSO\asmith`}),
	}
	render := func(users string) View {
		return Render(Event{ClassUID: 3006, Payload: payload(`{"activity_name":"Add User","users":` + users + `,"group":{"name":"Domain Admins"}}`)}, ents)
	}
	v := render(`[{"name":"jdoe","domain":"CONTOSO"},{"name":"asmith","domain":"CONTOSO"}]`)
	var refs []string
	for _, tok := range v.Sentence {
		if tok.T == "entity" {
			refs = append(refs, tok.Ref)
		}
	}
	if !slices.Equal(refs, []string{"user-account--jdoe", "user-account--asmith"}) {
		t.Errorf("sentence entities = %v; want both members bound", refs)
	}
	// Each member has its own Account row, linked to its account.
	var rows []Field
	for _, f := range v.Fields {
		if f.Label == "Account" {
			rows = append(rows, f)
		}
	}
	if len(rows) != 2 || rows[0].Value != `CONTOSO\jdoe` || rows[0].Ref != "user-account--jdoe" ||
		rows[1].Value != `CONTOSO\asmith` || rows[1].Ref != "user-account--asmith" {
		t.Errorf("Account rows = %+v; want one linked row per member", rows)
	}
	target := func(v View) SlotValue {
		for _, s := range v.Slots {
			if s.Name == "target" {
				return s
			}
		}
		return SlotValue{}
	}
	one := render(`[{"name":"jdoe","domain":"CONTOSO"}]`)
	if target(v).Identity == "" || target(v).Identity == target(one).Identity {
		t.Errorf("target slots %+v and %+v; want two members apart from one", target(v), target(one))
	}
}

// TestRender_GroupMembersFoldByAccount (adr/0359): with several members the
// target slot keys on each member's account, so the same names in two
// domains never fold, whether or not the record bound them.
func TestRender_GroupMembersFoldByAccount(t *testing.T) {
	ents := []Entity{
		ent("user-account--corp-alice", map[string]any{"account_login": `CORP\alice`}),
		ent("user-account--corp-bob", map[string]any{"account_login": `CORP\bob`}),
		ent("user-account--lab-alice", map[string]any{"account_login": `LAB\alice`}),
		ent("user-account--lab-bob", map[string]any{"account_login": `LAB\bob`}),
	}
	target := func(domain string, ents []Entity) SlotValue {
		js := `{"activity_name":"Add User","actor":{"user":{"name":"admin"}},"group":{"name":"Domain Admins"},` +
			`"users":[{"name":"alice","domain":"` + domain + `"},{"name":"bob","domain":"` + domain + `"}]}`
		for _, s := range Render(Event{ClassUID: 3006, Payload: payload(js)}, ents).Slots {
			if s.Name == "target" {
				return s
			}
		}
		return SlotValue{}
	}
	for name, e := range map[string][]Entity{"bound": ents, "unbound": nil} {
		corp, lab := target("CORP", e), target("LAB", e)
		if corp.Identity == "" || corp.Identity == lab.Identity {
			t.Errorf("%s: target slots %+v and %+v fold; want them apart", name, corp, lab)
		}
	}
	if got := target("CORP", ents).Identity; !strings.Contains(got, "ref:user-account--corp-alice") || !strings.Contains(got, "ref:user-account--corp-bob") {
		t.Errorf("bound target identity = %s; want each member's ref", got)
	}
}

// TestRender_GroupMembersNotTheDeprecatedUser (adr/0359): with users[]
// present, its members are the accounts shown, and the deprecated user is
// not shown.
func TestRender_GroupMembersNotTheDeprecatedUser(t *testing.T) {
	v := Render(Event{ClassUID: 3006, Payload: payload(`{"users":[{"name":"jdoe","domain":"CONTOSO"},{"name":"asmith"}],"user":{"name":"old"},"group":{"name":"Domain Admins"}}`)}, nil)
	var accounts []string
	for _, f := range v.Fields {
		if f.Label == "Account" {
			accounts = append(accounts, f.Value)
		}
	}
	if !slices.Equal(accounts, []string{`CONTOSO\jdoe`, "asmith"}) {
		t.Errorf("Account rows = %v; want the users[] members", accounts)
	}
	if strings.Contains(sentenceText(v), "old") {
		t.Errorf("sentence %q names the deprecated user", sentenceText(v))
	}
}

// TestRender_GroupMembersNameNobody (adr/0359): when no entry of users[]
// names a user, the deprecated user is the target, as the normalizer reads
// it.
func TestRender_GroupMembersNameNobody(t *testing.T) {
	v := Render(Event{ClassUID: 3006, Payload: payload(`{"activity_name":"Add User","users":[{"uid":""}],"user":{"name":"jdoe"},"group":{"name":"Domain Admins"}}`)}, nil)
	if f, ok := fieldValue(v, "Account"); !ok || f.Value != "jdoe" {
		t.Errorf("Account = %+v (present %v); want jdoe from user", f, ok)
	}
	if got := sentenceText(v); got != "Account jdoe was added to Domain Admins" {
		t.Errorf("sentence = %q", got)
	}
}

// TestRender_GroupTargetFoldsAlike (adr/0359): one member and many fold by
// the same rule: the ref, else the qualified login folded for case.
func TestRender_GroupTargetFoldsAlike(t *testing.T) {
	target := func(users string) string {
		js := `{"activity_name":"Add User","group":{"name":"Domain Admins"},"users":` + users + `}`
		for _, s := range Render(Event{ClassUID: 3006, Payload: payload(js)}, nil).Slots {
			if s.Name == "target" {
				return s.Identity
			}
		}
		return ""
	}
	corp, lab := target(`[{"name":"alice","domain":"CORP"}]`), target(`[{"name":"alice","domain":"LAB"}]`)
	if corp == "" || corp == lab {
		t.Errorf("CORP and LAB alice fold: %q, %q", corp, lab)
	}
	if a, b := target(`[{"name":"Alice","domain":"CORP"}]`), target(`[{"name":"alice","domain":"corp"}]`); a == "" || a != b {
		t.Errorf("CORP\\Alice and corp\\alice do not fold: %q, %q", a, b)
	}
	if a, b := target(`[{"name":"Alice","domain":"CORP"},{"name":"bob","domain":"CORP"}]`), target(`[{"name":"bob","domain":"corp"},{"name":"alice","domain":"corp"}]`); a == "" || a != b {
		t.Errorf("two members in another case and order do not fold: %q, %q", a, b)
	}
}

// TestRender_GroupMembersCapped (adr/0359): a long member list reads as a
// message's recipients do: two named, then "+N more", in the sentence and in
// the field rows.
func TestRender_GroupMembersCapped(t *testing.T) {
	var members []string
	for i := range 300 {
		members = append(members, fmt.Sprintf(`{"name":"user%03d"}`, i))
	}
	v := Render(Event{ClassUID: 3006, Payload: payload(`{"activity_name":"Add User","actor":{"user":{"name":"admin"}},"group":{"name":"G"},"users":[` + strings.Join(members, ",") + `]}`)}, nil)
	if got := sentenceText(v); got != "admin added user000, user001 +298 more to G" {
		t.Errorf("sentence = %q", got)
	}
	var rows []string
	for _, f := range v.Fields {
		if f.Label == "Account" {
			rows = append(rows, f.Value)
		}
	}
	if !slices.Equal(rows, []string{"user000", "user001", "+298 more"}) {
		t.Errorf("Account rows = %v", rows)
	}
}

// TestRender_GroupMembersDeduplicated (adr/0359): a member listed twice is
// one account, as the normalizer mints it once. The change reads and folds
// as the same change sent without the repeat.
func TestRender_GroupMembersDeduplicated(t *testing.T) {
	render := func(users string) View {
		return Render(Event{ClassUID: 3006, Payload: payload(`{"activity_name":"Add User","actor":{"user":{"name":"admin"}},"group":{"name":"G"},"users":` + users + `}`)}, nil)
	}
	twice := render(`[{"name":"jdoe"},{"name":"JDOE"},{"name":"asmith"}]`)
	once := render(`[{"name":"jdoe"},{"name":"asmith"}]`)
	if got := sentenceText(twice); got != "admin added jdoe, asmith to G" {
		t.Errorf("sentence = %q", got)
	}
	var rows []string
	for _, f := range twice.Fields {
		if f.Label == "Account" {
			rows = append(rows, f.Value)
		}
	}
	if !slices.Equal(rows, []string{"jdoe", "asmith"}) {
		t.Errorf("Account rows = %v; want two", rows)
	}
	target := func(v View) string {
		for _, s := range v.Slots {
			if s.Name == "target" {
				return s.Identity
			}
		}
		return ""
	}
	if target(twice) == "" || target(twice) != target(once) {
		t.Errorf("target slots %q and %q; want them to fold", target(twice), target(once))
	}
}

// TestRender_GroupMemberByID: a users[] entry that names an account by its
// uid alone shows the uid as the Account, and binds the user-account whose
// user_id it is when the record holds one.
func TestRender_GroupMemberByID(t *testing.T) {
	const sid = "S-1-5-21-7-1101"
	js := `{"activity_name":"Add User","actor":{"user":{"name":"admin"}},"group":{"name":"G"},"users":[{"uid":"` + sid + `"}]}`
	for name, tc := range map[string]struct {
		ents []Entity
		ref  string
	}{
		"bound":   {[]Entity{ent("user-account--sid", map[string]any{"user_id": sid})}, "user-account--sid"},
		"unbound": {nil, ""},
	} {
		v := Render(Event{ClassUID: 3006, Payload: payload(js)}, tc.ents)
		if f, ok := fieldValue(v, "Account"); !ok || f.Value != sid || f.Ref != tc.ref {
			t.Errorf("%s: Account = %+v (present %v); want %s linked to %q", name, f, ok, sid, tc.ref)
		}
		if got := sentenceText(v); got != "admin added "+sid+" to G" {
			t.Errorf("%s: sentence = %q", name, got)
		}
	}
}

// TestRender_GroupMemberByIDKeys: a member named by id alone keys the target
// slot, so two changes of different such members, or of Alice with and
// without one, never fold; one listed twice is one account.
func TestRender_GroupMemberByIDKeys(t *testing.T) {
	render := func(users string) View {
		return Render(Event{ClassUID: 3006, Payload: payload(`{"activity_name":"Add User","actor":{"user":{"name":"admin"}},"group":{"name":"G"},"users":` + users + `}`)}, nil)
	}
	target := func(v View) string {
		for _, s := range v.Slots {
			if s.Name == "target" {
				return s.Identity
			}
		}
		return ""
	}
	a, b := target(render(`[{"uid":"S-1-5-21-7-1101"}]`)), target(render(`[{"uid":"S-1-5-21-7-1102"}]`))
	if a == "" || a == b {
		t.Errorf("two id-only members fold: %q, %q", a, b)
	}
	alone, with := target(render(`[{"name":"alice"}]`)), target(render(`[{"name":"alice"},{"uid":"S-1-5-21-7-1101"}]`))
	if alone == "" || alone == with {
		t.Errorf("alice alone and alice with an id-only member fold: %q, %q", alone, with)
	}
	twice := render(`[{"uid":"S-1-5-21-7-1101"},{"uid":" S-1-5-21-7-1101 "}]`)
	var rows []string
	for _, f := range twice.Fields {
		if f.Label == "Account" {
			rows = append(rows, f.Value)
		}
	}
	if !slices.Equal(rows, []string{"S-1-5-21-7-1101"}) {
		t.Errorf("Account rows = %v; want one", rows)
	}
	if target(twice) != a {
		t.Errorf("target slots %q and %q; want the repeat to fold", target(twice), a)
	}
}

// TestRender_AccountChangeUserByID: an account change whose user carries
// only a uid shows the uid as the Account, read by the same rule as a group
// member.
func TestRender_AccountChangeUserByID(t *testing.T) {
	v := Render(Event{ClassUID: 3001, Payload: payload(`{"activity_name":"Enable","user":{"uid":"S-1-5-21-7-1101"}}`)}, nil)
	if f, ok := fieldValue(v, "Account"); !ok || f.Value != "S-1-5-21-7-1101" {
		t.Errorf("Account = %+v (present %v); want the uid", f, ok)
	}
}

// TestRender_RegistryTypedDataSlot (adr/0359): the data slot reads the typed
// field, so two different writes to one value do not fold together.
func TestRender_RegistryTypedDataSlot(t *testing.T) {
	data := func(js string) string {
		v := Render(Event{ClassUID: 201002, Payload: payload(js)}, nil)
		for _, s := range v.Slots {
			if s.Name == "data" {
				return s.Identity
			}
		}
		return ""
	}
	a := data(`{"reg_value":{"path":"HKLM\\SOFTWARE\\Run","name":"Updater","reg_string_data":"C:\\a.exe"}}`)
	b := data(`{"reg_value":{"path":"HKLM\\SOFTWARE\\Run","name":"Updater","reg_string_data":"C:\\b.exe"}}`)
	if a == "" || a == b {
		t.Errorf("data slots %q and %q; want two values", a, b)
	}
}
