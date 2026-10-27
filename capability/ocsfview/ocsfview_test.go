package ocsfview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refs answers every slot with a synthetic id so the test can see which
// slots were looked up and with what value.
func refs(seen map[string]string) RefLookup {
	return func(typ, value string) string {
		seen[typ+"="+value] = "x"
		return typ + "--" + value
	}
}

func TestAuthentication(t *testing.T) {
	p := map[string]any{
		"class_uid": float64(3002), "class_name": "Authentication",
		"actor":        map[string]any{"user": map[string]any{"name": "svc_backup", "domain": "CONTOSO"}},
		"dst_endpoint": map[string]any{"hostname": "WIN-FILE01"},
		"src_endpoint": map[string]any{"ip": "10.0.4.20"},
		"logon_type":   "RemoteInteractive", "logon_type_id": float64(10), "status": "SUCCESS",
	}
	seen := map[string]string{}
	v := Build(3002, p, refs(seen))
	if got := v.Text(); got != `CONTOSO\svc_backup RemoteInteractive logon to WIN-FILE01 from 10.0.4.20` {
		t.Errorf("sentence = %q", got)
	}
	if v.Category != "auth" {
		t.Errorf("category = %q", v.Category)
	}
	// The user slot is looked up by the shaped login the normalizer records.
	if seen[`user-account=CONTOSO\svc_backup`] == "" || seen["x-host=WIN-FILE01"] == "" || seen["ipv4-addr=10.0.4.20"] == "" {
		t.Errorf("lookups = %v", seen)
	}
	var result, logon Field
	for _, f := range v.Fields {
		switch f.Label {
		case "Result":
			result = f
		case "Logon type":
			logon = f
		}
	}
	if result.Value != "SUCCESS" || result.Tone != "ok" || logon.Value != "RemoteInteractive (type 10)" {
		t.Errorf("fields = %+v", v.Fields)
	}

	// A failed logon says so in the sentence.
	p["status"] = "FAILURE"
	if got := Build(3002, p, nil).Text(); !strings.HasSuffix(got, " — failure") {
		t.Errorf("failed sentence = %q", got)
	}
}

func TestProcess_DecodesEncodedCommand(t *testing.T) {
	p := map[string]any{
		"process": map[string]any{
			"pid": float64(6120), "name": "powershell.exe",
			"cmd_line":       "powershell.exe -NoProfile -EncodedCommand ZQBjAGgAbwA=",
			"file":           map[string]any{"path": `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, "hashes": map[string]any{"SHA-256": "9f1c"}},
			"parent_process": map[string]any{"pid": float64(5000), "name": "mstsc-hijack.exe"},
		},
		"device": map[string]any{"hostname": "WIN-FILE01"},
		"actor":  map[string]any{"user": map[string]any{"name": "svc_backup", "domain": "CONTOSO"}},
	}
	seen := map[string]string{}
	v := Build(1007, p, refs(seen))
	if got := v.Text(); got != `CONTOSO\svc_backup ran powershell.exe (pid 6120) on WIN-FILE01, parent mstsc-hijack.exe` {
		t.Errorf("sentence = %q", got)
	}
	// A process slot reads as its name and is looked up by its pid.
	if seen["process=6120"] == "" || seen["process=5000"] == "" {
		t.Errorf("process lookups = %v", seen)
	}
	if v.Decoded == nil || v.Decoded.Encoding != "base64-utf16le" || v.Decoded.Text != "echo" || v.Decoded.Field != "process.cmd_line" {
		t.Errorf("decoded = %+v", v.Decoded)
	}
	// The original command line is still a field beside the decoded form.
	found := false
	for _, f := range v.Fields {
		if f.Label == "Command line" && strings.Contains(f.Value, "-EncodedCommand") {
			found = true
		}
	}
	if !found {
		t.Errorf("command line field missing: %+v", v.Fields)
	}
	// Garbage after the flag does not decode: no decoded block, no panic.
	p["process"].(map[string]any)["cmd_line"] = "powershell -enc ////////"
	if d := Build(1007, p, nil).Decoded; d != nil {
		t.Errorf("garbage decoded to %+v", d)
	}
}

func TestNetworkAndDNS(t *testing.T) {
	net := map[string]any{
		"src_endpoint":    map[string]any{"ip": "10.0.4.20", "port": float64(49999)},
		"dst_endpoint":    map[string]any{"ip": "185.220.101.5", "port": float64(443), "domain": "exfil.example.net"},
		"connection_info": map[string]any{"protocol_name": "tcp"},
		"device":          map[string]any{"hostname": "WIN-FILE01"},
	}
	if got := Build(4001, net, nil).Text(); got != "WIN-FILE01 connected tcp to exfil.example.net:443 from 10.0.4.20:49999" {
		t.Errorf("network = %q", got)
	}
	dns := map[string]any{
		"device":  map[string]any{"hostname": "WIN-FILE01"},
		"query":   map[string]any{"hostname": "exfil.example.net", "type": "A"},
		"answers": []any{map[string]any{"rdata": "185.220.101.5"}},
		"rcode":   "NOERROR",
	}
	v := Build(4003, dns, nil)
	if got := v.Text(); got != "WIN-FILE01 queried exfil.example.net → 185.220.101.5" {
		t.Errorf("dns = %q", got)
	}
	if v.Category != "network" {
		t.Errorf("category = %q", v.Category)
	}
}

func TestDetectionAndCase(t *testing.T) {
	det := map[string]any{
		"metadata": map[string]any{"product": map[string]any{"vendor_name": "VendorX"}},
		"finding":  map[string]any{"uid": "det-1", "title": "Encoded command after remote logon", "severity": "High", "confidence": "High", "types": []any{"T1059.001"}},
		"evidence": map[string]any{"class_name": "Process Activity", "device": map[string]any{"hostname": "WIN-FILE01"}, "actor": map[string]any{"user": map[string]any{"name": "svc_backup"}}},
	}
	v := Build(2004, det, nil)
	if got := v.Text(); got != "VendorX detected “Encoded command after remote logon” (high) on WIN-FILE01 as svc_backup" {
		t.Errorf("detection = %q", got)
	}
	if v.Category != "detection" {
		t.Errorf("category = %q", v.Category)
	}
	inc := map[string]any{"finding_info": map[string]any{"uid": "INC0010042", "title": "Suspicious logon"}, "status": "New", "priority": "2"}
	if got := Build(2005, inc, nil).Text(); got != "case INC0010042 “Suspicious logon” (New, priority 2)" {
		t.Errorf("case = %q", got)
	}
}

func TestUnknownClassHasFieldsNoSentence(t *testing.T) {
	v := Build(9999, map[string]any{"class_uid": float64(9999), "message": "hello", "count": float64(3), "nested": map[string]any{"x": 1}}, nil)
	if len(v.Sentence) != 0 || v.Category != "other" {
		t.Errorf("unknown class: %+v", v)
	}
	if len(v.Fields) != 2 || v.Fields[0].Label != "Count" || v.Fields[1].Value != "hello" {
		t.Errorf("fields = %+v", v.Fields)
	}
	if b, _ := json.Marshal(Build(9999, map[string]any{}, nil)); !strings.Contains(string(b), `"fields":[]`) {
		t.Errorf("fields must serialize as a list: %s", b)
	}
}

// Every fixture event the scenarios ship renders without panicking, and every
// class the normalizers handle yields a sentence.
func TestFixtures(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "fixtures", "*", "*.json"))
	if len(files) == 0 {
		t.Skip("no fixtures beside the module")
	}
	handled := map[int]bool{1001: true, 1003: true, 1006: true, 1007: true, 1009: true, 2004: true, 2005: true, 3002: true, 3005: true, 4001: true, 4003: true, 4009: true, 4011: true, 5001: true}
	n := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Ocsf map[string]any `json:"ocsf"`
		}
		if json.Unmarshal(raw, &doc) != nil || doc.Ocsf == nil {
			continue
		}
		class, _ := doc.Ocsf["class_uid"].(float64)
		v := Build(int(class), doc.Ocsf, nil)
		n++
		if handled[int(class)] && len(v.Sentence) == 0 {
			t.Errorf("%s: class %d rendered no sentence", filepath.Base(f), int(class))
		}
	}
	if n == 0 {
		t.Fatal("no OCSF fixtures read")
	}
}
