package capability

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sd-strax/reckon/identity"
)

// The tests in this file cover the four inputs the STIX validation test found
// in the normalizers' own tests (adr/0357).

// A hash that does not fit its algorithm is left out of the stored file and
// of the inputs to its id. A file it leaves with nothing to identify it is
// not written, as for a file with no hashes and no name.
func TestFileHashThatDoesNotFitIsLeftOut(t *testing.T) {
	ids := identity.NewResolver(testNS)
	normalize := func(file map[string]any) NormalizationResult {
		t.Helper()
		res, err := testRegistry().Normalize(makeEvent(map[string]any{
			"class_uid": 1001, "time": ms("2026-04-20T14:32:11Z"),
			"file": file, "device": map[string]any{"hostname": "WS1"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	fp := func(algo, value string) map[string]any { return map[string]any{"algorithm": algo, "value": value} }

	res := normalize(map[string]any{"name": "evil.dll", "hashes": []any{fp("SHA-256", "deadbeef"), fp("MD5", testMD5)}})
	f := scosOf(res, identity.TypeFile)
	if len(f) != 1 || !reflect.DeepEqual(f[0].Properties["hashes"], map[string]string{"MD5": testMD5}) {
		t.Fatalf("broken SHA-256 beside a good MD5: files = %+v; want the MD5 alone", f)
	}
	if want := ids.File(identity.FileIdentity{Hashes: map[string]string{"MD5": testMD5}, Name: "evil.dll"}); f[0].ID != want {
		t.Errorf("file id = %s; want %s, minted from the MD5 alone", f[0].ID, want)
	}

	res = normalize(map[string]any{"name": "evil.dll", "hashes": []any{fp("SHA-256", "deadbeef")}})
	f = scosOf(res, identity.TypeFile)
	if len(f) != 1 || f[0].Properties["hashes"] != nil || f[0].ID != ids.File(identity.FileIdentity{Name: "evil.dll"}) {
		t.Errorf("broken hash with a name: files = %+v; want the file by its name, with no hashes", f)
	}

	// A versioned TLSH loses its "T1", and a name STIX does not allow as a
	// key is written with "-" (OCSF's captions for algorithm_id 11 and 16).
	// The stored hashes are the ones the id is minted from.
	tlsh := strings.Repeat("A1", 35)
	hashes := []any{
		fp("TLSH", "T1"+tlsh),
		map[string]any{"algorithm_id": 11, "value": testSHA256},
		map[string]any{"algorithm_id": 16, "value": "0123456789abcdef"},
	}
	res = normalize(map[string]any{"name": "evil.dll", "hashes": hashes})
	want := map[string]string{"TLSH": tlsh, "SHA-512-256": testSHA256, "xxHash-H3-64-bit": "0123456789abcdef"}
	f = scosOf(res, identity.TypeFile)
	if len(f) != 1 || !reflect.DeepEqual(f[0].Properties["hashes"], want) {
		t.Errorf("TLSH and key names: files = %+v; want hashes %v", f, want)
	} else if f[0].ID != ids.File(identity.FileIdentity{Hashes: want, Name: "evil.dll"}) {
		t.Errorf("file id %s is not minted from the stored hashes", f[0].ID)
	}

	res = normalize(map[string]any{"hashes": []any{fp("SHA-256", "deadbeef")}})
	if f := scosOf(res, identity.TypeFile); len(f) != 0 {
		t.Errorf("a broken hash and nothing else wrote a file: %+v", f)
	}
}

// STIX requires protocols. The name is OCSF's protocol_name, else
// protocol_num's name, else the address family. Only telemetry values are
// used.
func TestNetworkTrafficProtocols(t *testing.T) {
	for _, c := range []struct {
		name     string
		conn     map[string]any
		src, dst string
		want     []string
	}{
		{"the name, lowercased", map[string]any{"protocol_name": "TCP", "protocol_num": 17}, "10.0.0.1", "10.0.0.2", []string{"tcp"}},
		{"tcp from the number", map[string]any{"protocol_num": 6}, "10.0.0.1", "10.0.0.2", []string{"tcp"}},
		{"udp from the number", map[string]any{"protocol_num": 17}, "10.0.0.1", "10.0.0.2", []string{"udp"}},
		{"icmp from the number", map[string]any{"protocol_num": 1}, "10.0.0.1", "10.0.0.2", []string{"icmp"}},
		{"ipv4 from the addresses", map[string]any{"protocol_num": 47}, "10.0.0.1", "10.0.0.2", []string{"ipv4"}},
		{"ipv6 from the addresses", nil, "fe80::1", "fe80::2", []string{"ipv6"}},
		{"ipv4 from the destination alone", nil, "", "10.0.0.2", []string{"ipv4"}},
	} {
		p := map[string]any{"class_uid": 4001, "time": ms("2026-04-20T14:32:11Z"), "dst_endpoint": map[string]any{"ip": c.dst, "port": 443}}
		if c.src != "" {
			p["src_endpoint"] = map[string]any{"ip": c.src, "port": 50000}
		}
		if c.conn != nil {
			p["connection_info"] = c.conn
		}
		res, err := testRegistry().Normalize(makeEvent(p))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		nt := scosOf(res, identity.TypeNetworkTraffic)
		if len(nt) != 1 || !reflect.DeepEqual(nt[0].Properties["protocols"], c.want) {
			t.Errorf("%s: network-traffic = %+v; want protocols %v", c.name, nt, c.want)
		}
	}
}

// An event with no time, and a detection with no pattern, no uid and no
// title, are broken records. Normalizing either is an error. The resolver
// keeps the raw event and notes the error, as for any normalize error.
func TestBrokenRecordsAreErrorsAndKeptRaw(t *testing.T) {
	if _, err := testRegistry().Normalize(makeEvent(map[string]any{"class_uid": 1007, "device": map[string]any{"hostname": "WS1"}})); !errors.Is(err, ErrNoEventTime) {
		t.Errorf("no time: err = %v; want ErrNoEventTime", err)
	}
	noRef := detectionPayload()
	delete(noRef, "finding_info")
	if _, err := testRegistry().Normalize(makeEvent(noRef)); !errors.Is(err, ErrNoRuleReference) {
		t.Errorf("no pattern, uid or title: err = %v; want ErrNoRuleReference", err)
	}
	withPattern := detectionPayload()
	delete(withPattern, "finding_info")
	withPattern["unmapped"] = map[string]any{"pattern": "rule-1234"}
	if _, err := testRegistry().Normalize(makeEvent(withPattern)); err != nil {
		t.Errorf("a vendor pattern alone: %v", err)
	}
	for _, keep := range []map[string]any{{"uid": "f-1"}, {"title": "Suspicious sign-in"}} {
		p := detectionPayload()
		p["finding_info"] = keep
		if _, err := testRegistry().Normalize(makeEvent(p)); err != nil {
			t.Errorf("finding_info %v: %v; a uid or a title is a rule reference", keep, err)
		}
	}

	at := time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC)
	siem := &stubAdapter{name: "siem", healthy: true, resp: AdapterResponse{SourceTool: "siem", Events: []OcsfPayload{
		{ClassUID: 1007, ClassName: "Process Activity", Raw: map[string]any{"class_uid": 1007, "device": map[string]any{"hostname": "WS1"}}},
		{ClassUID: 2004, ClassName: "Detection Finding", Time: at, Raw: map[string]any{"class_uid": 2004, "time": at.UnixMilli()}},
		{ClassUID: 1007, ClassName: "Process Activity", Time: at, Raw: map[string]any{"class_uid": 1007, "time": at.UnixMilli(), "device": map[string]any{"hostname": "WS1"}}},
	}}}
	res := newResolver(map[string][]Binding{"v": {{Adapter: "siem", Operation: "op", Priority: 1}}}, map[string]Adapter{"siem": siem})
	out, err := res.Resolve(context.Background(), "v", CallInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 3 || len(out.OcsfEventRefs) != 3 {
		t.Errorf("events = %d, refs = %d; want all three raw events kept", len(out.Events), len(out.OcsfEventRefs))
	}
	if len(out.Normalized) != 1 {
		t.Errorf("normalized = %d; want only the whole record", len(out.Normalized))
	}
	notes := strings.Join(out.DegradationNotes, "\n")
	for _, want := range []string{"normalize class 1007: " + ErrNoEventTime.Error(), "normalize class 2004: " + ErrNoRuleReference.Error()} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes = %q; want one saying %q", out.DegradationNotes, want)
		}
	}
}
