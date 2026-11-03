package ocsfview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
func TestRender_Authentication(t *testing.T) {
	p := payload(`{"class_uid":3002,"actor":{"user":{"name":"svc_backup","domain":"CONTOSO","uid":"S-1"}},
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
		{"dns", 4003, `{"query":{"hostname":"exfil.example.net","type":"A"},"answers":[{"rdata":"185.220.101.5","type":"A"}],
			"src_endpoint":{"ip":"10.0.4.20"},"device":{"hostname":"WIN-FILE01"},"rcode":"NoError"}`,
			"WIN-FILE01 queried exfil.example.net → 185.220.101.5", CategoryNetwork},
		{"file", 1001, `{"activity_name":"Create","file":{"name":"payload.dll","path":"C:\\Temp\\payload.dll"},"actor":{"user":{"name":"jdoe"},"process":{"pid":4,"name":"explorer.exe"}},"device":{"hostname":"WS1"}}`,
			"jdoe created payload.dll on WS1 via explorer.exe", CategoryProcess},
		{"registry", 1003, `{"activity_name":"Modify","reg_key":{"hive":"HKLM","path":"SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Run"},"reg_value":{"name":"Updater","data":"C:\\u.exe"},
			"actor":{"process":{"pid":9,"name":"reg.exe"}},"device":{"hostname":"WS1"}}`,
			`reg.exe modified HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Run on WS1`, CategoryProcess},
		{"scheduled job", 1006, `{"activity_name":"Create","job":{"name":"Updater","path":"\\Microsoft\\Updater","user":{"name":"SYSTEM"}},"actor":{"user":{"name":"jdoe"}},"device":{"hostname":"WS1"}}`,
			"jdoe created scheduled job Updater on WS1 running as SYSTEM", CategoryProcess},
		{"module", 1009, `{"module":{"file":{"name":"evil.dll","path":"C:\\Temp\\evil.dll"}},"actor":{"process":{"pid":7,"name":"svchost.exe"}},"device":{"hostname":"WS1"}}`,
			"svchost.exe loaded evil.dll on WS1", CategoryProcess},
		{"account change", 3005, `{"activity_name":"Password Reset","user":{"name":"jdoe","domain":"CONTOSO"},"actor":{"user":{"name":"admin","domain":"CONTOSO"}},"device":{"hostname":"DC1"}}`,
			"admin reset the password of jdoe on DC1", CategoryAuth},
		{"account change without actor", 3005, `{"activity_name":"Create","user":{"name":"jdoe"},"group":{"name":"Domain Admins"}}`,
			"Account jdoe was created in Domain Admins", CategoryAuth},
		{"email", 4009, `{"email":{"from":"billing@evil.example","to":["ap@corp.example","cfo@corp.example"],"subject":"Overdue invoice 44817","message_uid":"<m1>"}}`,
			"billing@evil.example sent “Overdue invoice 44817” to ap@corp.example, cfo@corp.example", CategoryNetwork},
		{"email url", 4011, `{"activity_name":"Receive","url":{"url_string":"https://login.evil.example/x"},"actor":{"user":{"name":"jdoe"}},"device":{"hostname":"WS1"}}`,
			"jdoe received https://login.evil.example/x on WS1", CategoryNetwork},
		{"detection", 2004, `{"metadata":{"product":{"vendor_name":"CrowdStrike"}},"finding":{"uid":"det-1","title":"Suspicious lateral movement","severity":"High","types":["T1021.001"]},
			"evidence":{"device":{"hostname":"WIN-FILE01"}}}`,
			"CrowdStrike detected “Suspicious lateral movement” (high) on WIN-FILE01", CategoryDetection},
		{"case", 2005, `{"category_uid":2,"finding_info":{"uid":"INC0010039","title":"Phishing report"},"status":"New","priority":"3 - Moderate","assignment_group":"SOC Tier 2"}`,
			"Case INC0010039 “Phishing report”, New, priority 3 - Moderate, assigned to SOC Tier 2", CategoryCase},
		{"device inventory", 5001, `{"device":{"hostname":"WIN-FILE01","os":{"name":"Windows Server 2022"},"owner":{"name":"File Services"},"criticality":"high"}}`,
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

// TestRender_ProcessFields: the curated process fields, the command line shown
// as it is, the parent with its pid, the image bound to the file object.
func TestRender_ProcessFields(t *testing.T) {
	p := payload(`{"process":{"pid":6120,"name":"powershell.exe","cmd_line":"powershell.exe -NoProfile -EncodedCommand ZQBjAGgAbwA=",
		"file":{"path":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","name":"powershell.exe","hashes":{"SHA-256":"9f1c"}},
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
	v := Render(Event{ClassUID: 3005, Payload: payload(`{"activity_name":"Disable","user":{"name":"jdoe","domain":"CONTOSO"},"actor":{"user":{"name":"admin","domain":"CONTOSO"}}}`)}, []Entity{
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
	v = Render(Event{ClassUID: 3002, Payload: payload(`{"actor":{"user":{"name":"root"}},"dst_endpoint":{"hostname":"srv"}}`)}, []Entity{
		ent("user-account--r", map[string]any{"account_login": "root", "account_type": "local"}),
	})
	if v.Sentence[0].Ref != "user-account--r" {
		t.Errorf("bare login did not bind: %+v", v.Sentence[0])
	}
}

// TestRender_UnknownClass: the top-level scalars as fields, no sentence, the
// category from category_uid.
func TestRender_UnknownClass(t *testing.T) {
	v := Render(Event{ClassUID: 6003, Payload: payload(`{"class_uid":6003,"category_uid":6,"time":"2026-01-01T00:00:00Z","api":{"operation":"GetObject"},"severity":"Informational","count":2,"ok":true}`)}, nil)
	if v.Sentence != nil {
		t.Errorf("sentence for an unknown class: %+v", v.Sentence)
	}
	labels := make([]string, 0, len(v.Fields))
	for _, f := range v.Fields {
		labels = append(labels, f.Label+"="+f.Value)
	}
	if want := []string{"count=2", "ok=true", "severity=Informational"}; !slices.Equal(labels, want) {
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
