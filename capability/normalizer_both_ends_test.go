package capability

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsfschema"
)

// This file is "Test both ends" (03 §4.16, adr/0356) for the normalizers
// adr/0356 changed: an event shaped to the OCSF floor (1.1.0) and one shaped
// to the ceiling (1.9.0), naming the same entities, mint the same ids. Each
// event's fields are first checked against the schema of the version it
// declares (ocsfschema.ValidateFields), so a test event cannot name a field
// or a type OCSF does not have. ValidateFields does not check that the
// fields OCSF requires are there: these events carry only what the
// normalizers read.

// entityIDs is what a set of events names: the ids of the SCOs in their
// observations' object_refs, and their relationships as "type src tgt", both
// sorted. The observed-data ids are left out: they carry the payload.
func entityIDs(t *testing.T, events ...map[string]any) (refs, rels []string) {
	t.Helper()
	reg := testRegistry()
	seen := map[string]bool{}
	for _, e := range events {
		res, err := reg.Normalize(makeEvent(e))
		if err != nil {
			t.Fatal(err)
		}
		for _, od := range res.ObservedData {
			for _, r := range od.ObjectRefs {
				if !seen[string(r)] {
					seen[string(r)] = true
					refs = append(refs, string(r))
				}
			}
		}
		for _, r := range res.Relationships {
			k := r.Type + " " + string(r.SourceRef) + " " + string(r.TargetRef)
			if !seen[k] {
				seen[k] = true
				rels = append(rels, k)
			}
		}
	}
	sort.Strings(refs)
	sort.Strings(rels)
	return refs, rels
}

// scoProps is the value of each named property on every SCO the events
// give, as "id key=value", sorted. An SCO without the property is left out.
func scoProps(t *testing.T, keys []string, events ...map[string]any) []string {
	t.Helper()
	if len(keys) == 0 {
		return nil
	}
	reg := testRegistry()
	var out []string
	for _, e := range events {
		res, err := reg.Normalize(makeEvent(e))
		if err != nil {
			t.Fatal(err)
		}
		for _, sco := range res.SCOs {
			for _, k := range keys {
				if v, ok := sco.Properties[k]; ok {
					out = append(out, fmt.Sprintf("%s %s=%v", sco.ID, k, v))
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// versioned is an OCSF event of class at version: fields plus class_uid, time
// and metadata.version.
func versioned(class int, version string, fields map[string]any) map[string]any {
	e := map[string]any{"class_uid": class, "time": ms("2026-04-20T14:32:11Z"), "metadata": map[string]any{"version": version}}
	for k, v := range fields {
		e[k] = v
	}
	return e
}

func TestBothEnds(t *testing.T) {
	when := ms("2026-04-20T14:32:11Z")
	sha := func(v string) []any {
		return []any{map[string]any{"algorithm": "SHA-256", "algorithm_id": 3, "value": v}}
	}
	device := map[string]any{"hostname": "WS1", "domain": "CONTOSO", "uid": "dev-1", "type_id": 2}
	// The ceiling's device adds what OCSF added after the floor.
	deviceCeiling := map[string]any{
		"hostname": "WS1", "domain": "CONTOSO", "uid": "dev-1", "type_id": 2,
		"agent_list": []any{map[string]any{"uid": "agent-1", "name": "edr-sensor", "type_id": 1}},
		"owner":      map[string]any{"name": "File Services"},
	}
	user := map[string]any{"name": "jdoe", "domain": "CONTOSO", "uid": "S-1-5-21-7-1101"}
	admin := map[string]any{"name": "admin", "domain": "CONTOSO", "uid": "S-1-5-21-7-500"}
	process := map[string]any{
		"pid": 4242, "name": "powershell.exe", "cmd_line": "powershell -enc ZQBjAGgAbwA=", "created_time": when,
		"file":           map[string]any{"name": "powershell.exe", "path": `C:\Windows\System32\powershell.exe`, "type_id": 1, "hashes": sha("abc123")},
		"parent_process": map[string]any{"pid": 1000, "name": "explorer.exe", "created_time": when},
	}
	actorProcess := map[string]any{"pid": 4242, "name": "powershell.exe", "created_time": when}
	attachment := map[string]any{"name": "invoice.pdf.exe", "type_id": 1, "hashes": sha("beef")}
	mail := map[string]any{
		"from": "attacker@evil.example", "to": []any{"alice@corp.example"}, "cc": []any{"carol@corp.example"},
		"subject": "Invoice", "message_uid": "<abc123@evil.example>", "uid": "gw-0001",
	}
	with := func(base map[string]any, extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	runKey := `HKLM\Software\Microsoft\Windows\CurrentVersion\Run`
	finding := map[string]any{"uid": "det-123", "title": "Suspicious PowerShell", "types": []any{"T1059.001"}}

	for _, tc := range []struct {
		name string
		// floor and ceiling are the events at each end. Several events at one
		// end are read together: the floor needs two where the ceiling needs
		// one.
		floor, ceiling []map[string]any
		// entities is how many objects the events name, so that two empty
		// results cannot pass as equal.
		entities int
		// props are SCO properties both ends must give the same value.
		props []string
	}{
		{
			name: "process_activity", entities: 5,
			floor:   []map[string]any{versioned(1007, "1.1.0", map[string]any{"process": process, "device": device, "actor": map[string]any{"user": user}})},
			ceiling: []map[string]any{versioned(1007, "1.9.0", map[string]any{"process": process, "device": deviceCeiling, "actor": map[string]any{"user": user}})},
		},
		{
			name: "authentication", entities: 3,
			floor: []map[string]any{versioned(3002, "1.1.0", map[string]any{
				"user": user, "dst_endpoint": map[string]any{"hostname": "WS1", "domain": "CONTOSO", "uid": "dev-1"},
				"src_endpoint": map[string]any{"ip": "10.0.4.55"}, "logon_type": "Remote Interactive", "status": "Success",
			})},
			ceiling: []map[string]any{versioned(3002, "1.9.0", map[string]any{
				"user": user,
				"dst_endpoint": map[string]any{
					"hostname": "WS1", "domain": "CONTOSO", "uid": "dev-1",
					"agent_list": []any{map[string]any{"uid": "agent-1", "name": "edr-sensor", "type_id": 1}},
				},
				"src_endpoint": map[string]any{"ip": "10.0.4.55"}, "logon_type": "Remote Interactive", "status": "Success",
			})},
		},
		{
			name: "file_activity", entities: 5,
			floor: []map[string]any{versioned(1001, "1.1.0", map[string]any{
				"file":  map[string]any{"name": "evil.dll", "path": `C:\Temp\evil.dll`, "type_id": 1, "hashes": sha("dead")},
				"actor": map[string]any{"process": actorProcess, "user": user}, "device": device,
			})},
			ceiling: []map[string]any{versioned(1001, "1.9.0", map[string]any{
				"file":  map[string]any{"name": "evil.dll", "path": `C:\Temp\evil.dll`, "type_id": 1, "hashes": sha("dead")},
				"actor": map[string]any{"process": actorProcess, "user": user}, "device": deviceCeiling,
			})},
		},
		{
			name: "registry_key_activity", entities: 3,
			floor:   []map[string]any{versioned(201001, "1.1.0", map[string]any{"reg_key": map[string]any{"path": runKey}, "actor": map[string]any{"process": actorProcess}, "device": device})},
			ceiling: []map[string]any{versioned(201001, "1.9.0", map[string]any{"reg_key": map[string]any{"path": runKey}, "actor": map[string]any{"process": actorProcess}, "device": deviceCeiling})},
		},
		{
			name: "registry_value_activity", entities: 3, props: []string{"values"},
			floor: []map[string]any{versioned(201002, "1.1.0", map[string]any{
				"reg_value": map[string]any{"path": runKey, "name": "Updater", "data": `C:\Temp\evil.exe`, "type": "REG_SZ", "type_id": 10},
				"actor":     map[string]any{"process": actorProcess}, "device": device,
			})},
			ceiling: []map[string]any{versioned(201002, "1.9.0", map[string]any{
				"reg_value": map[string]any{"path": runKey, "name": "Updater", "data": `C:\Temp\evil.exe`, "reg_string_data": `C:\Temp\evil.exe`, "type": "REG_SZ", "type_id": 10},
				"actor":     map[string]any{"process": actorProcess}, "device": deviceCeiling,
			})},
		},
		{
			// From 1.6.0 a source may send the data only in the field for its
			// type (adr/0359).
			name: "registry_value_activity typed data only", entities: 3, props: []string{"values"},
			floor: []map[string]any{versioned(201002, "1.1.0", map[string]any{
				"reg_value": map[string]any{"path": runKey, "name": "Updater", "data": `C:\Temp\evil.exe`, "type": "REG_SZ", "type_id": 10},
				"actor":     map[string]any{"process": actorProcess}, "device": device,
			})},
			ceiling: []map[string]any{versioned(201002, "1.9.0", map[string]any{
				"reg_value": map[string]any{"path": runKey, "name": "Updater", "reg_string_data": `C:\Temp\evil.exe`, "type": "REG_SZ", "type_id": 10},
				"actor":     map[string]any{"process": actorProcess}, "device": deviceCeiling,
			})},
		},
		{
			// The floor's one command line is job.cmd_line. The ceiling's is
			// the job's action.
			name: "scheduled_job_activity", entities: 3,
			floor: []map[string]any{versioned(1006, "1.1.0", map[string]any{
				"job":    map[string]any{"name": "EvilTask", "file": map[string]any{"name": "EvilTask", "path": `\Microsoft\Windows\EvilTask`, "type_id": 1}, "cmd_line": "evil.exe /q", "user": user},
				"device": device,
			})},
			ceiling: []map[string]any{versioned(1006, "1.9.0", map[string]any{
				"job": map[string]any{
					"name": "EvilTask", "file": map[string]any{"name": "EvilTask", "path": `\Microsoft\Windows\EvilTask`, "type_id": 1},
					"job_actions": []any{map[string]any{"type_id": 1, "cmd_line": "evil.exe /q"}}, "user": user,
				},
				"device": deviceCeiling,
			})},
		},
		{
			name: "module_activity", entities: 3,
			floor: []map[string]any{versioned(1005, "1.1.0", map[string]any{
				"module": map[string]any{"load_type_id": 1, "file": map[string]any{"name": "evil.dll", "path": `C:\Temp\evil.dll`, "type_id": 1, "hashes": sha("abcd")}},
				"actor":  map[string]any{"process": actorProcess}, "device": device,
			})},
			ceiling: []map[string]any{versioned(1005, "1.9.0", map[string]any{
				"module": map[string]any{"load_type_id": 1, "file": map[string]any{"name": "evil.dll", "path": `C:\Temp\evil.dll`, "type_id": 1, "hashes": sha("abcd")}},
				"actor":  map[string]any{"process": actorProcess}, "device": deviceCeiling,
			})},
		},
		{
			name: "account_change", entities: 3,
			floor:   []map[string]any{versioned(3001, "1.1.0", map[string]any{"activity_id": 1, "user": user, "actor": map[string]any{"user": admin}, "device": device})},
			ceiling: []map[string]any{versioned(3001, "1.9.0", map[string]any{"activity_id": 1, "user": user, "actor": map[string]any{"user": admin}, "device": deviceCeiling})},
		},
		{
			name: "user_access", entities: 3,
			floor:   []map[string]any{versioned(3005, "1.1.0", map[string]any{"activity_id": 1, "user": user, "actor": map[string]any{"user": admin}, "privileges": []any{"SeDebugPrivilege"}, "device": device})},
			ceiling: []map[string]any{versioned(3005, "1.9.0", map[string]any{"activity_id": 1, "user": user, "actor": map[string]any{"user": admin}, "privileges": []any{"SeDebugPrivilege"}, "device": deviceCeiling})},
		},
		{
			name: "group_management", entities: 4,
			floor: []map[string]any{versioned(3006, "1.1.0", map[string]any{
				"activity_id": 3, "user": user, "actor": map[string]any{"user": admin},
				"group": map[string]any{"name": "Domain Admins", "domain": "contoso.com", "uid": "g-512"}, "device": device,
			})},
			ceiling: []map[string]any{versioned(3006, "1.9.0", map[string]any{
				"activity_id": 3, "user": user, "actor": map[string]any{"user": admin},
				"group": map[string]any{"name": "Domain Admins", "domain": "contoso.com", "uid": "g-512"}, "device": deviceCeiling,
			})},
		},
		{
			// From 1.9.0 the members are users[], and user is deprecated. A
			// source may send users[] alone (adr/0359).
			name: "group_management users[] only", entities: 4,
			floor: []map[string]any{versioned(3006, "1.1.0", map[string]any{
				"activity_id": 3, "user": user, "actor": map[string]any{"user": admin},
				"group": map[string]any{"name": "Domain Admins", "domain": "contoso.com", "uid": "g-512"}, "device": device,
			})},
			ceiling: []map[string]any{versioned(3006, "1.9.0", map[string]any{
				"activity_id": 3, "users": []any{user}, "actor": map[string]any{"user": admin},
				"group": map[string]any{"name": "Domain Admins", "domain": "contoso.com", "uid": "g-512"}, "device": deviceCeiling,
			})},
		},
		{
			name: "network_activity", entities: 6,
			floor: []map[string]any{versioned(4001, "1.1.0", map[string]any{
				"src_endpoint": map[string]any{"ip": "10.0.4.20", "port": 49152}, "dst_endpoint": map[string]any{"ip": "203.0.113.9", "port": 443, "domain": "c2.example"},
				"connection_info": map[string]any{"protocol_name": "tcp"}, "actor": map[string]any{"process": actorProcess}, "device": device,
			})},
			ceiling: []map[string]any{versioned(4001, "1.9.0", map[string]any{
				"src_endpoint": map[string]any{"ip": "10.0.4.20", "port": 49152}, "dst_endpoint": map[string]any{"ip": "203.0.113.9", "port": 443, "domain": "c2.example"},
				"connection_info": map[string]any{"protocol_name": "tcp"}, "actor": map[string]any{"process": actorProcess}, "device": deviceCeiling,
			})},
		},
		{
			// The floor has no email.files[], email.urls[] or email.bcc[]: the
			// attachment is its own email_file_activity event, and the blind
			// copy is the source's own field. The ceiling carries all three on
			// the email. Both name the same message, addresses and file. The
			// link is left to the click below: the floor's email has no place
			// for it.
			name: "email_activity", entities: 5,
			floor: []map[string]any{
				versioned(4009, "1.1.0", map[string]any{"email": mail, "unmapped": map[string]any{"bcc": []any{"dave@corp.example"}}}),
			},
			ceiling: []map[string]any{
				versioned(4009, "1.9.0", map[string]any{"email": with(mail, map[string]any{"bcc": []any{"dave@corp.example"}})}),
			},
		},
		{
			// The email_uid is a thread id: it mints no message (adr/0359).
			name: "email_file_activity", entities: 3,
			floor:   []map[string]any{versioned(4011, "1.1.0", map[string]any{"activity_id": 2, "email_uid": "gw-0001", "file": attachment, "actor": map[string]any{"user": user}, "device": device})},
			ceiling: []map[string]any{versioned(4011, "1.9.0", map[string]any{"activity_id": 2, "email_uid": "gw-0001", "file": attachment, "actor": map[string]any{"user": user}, "device": deviceCeiling})},
		},
		{
			name: "email_url_activity", entities: 3,
			floor: []map[string]any{versioned(4012, "1.1.0", map[string]any{
				"email_uid": "gw-0001", "url": map[string]any{"url_string": "https://evil.example/pay"}, "actor": map[string]any{"user": user}, "device": device,
			})},
			ceiling: []map[string]any{versioned(4012, "1.9.0", map[string]any{
				"email_uid": "gw-0001", "url": map[string]any{"url_string": "https://evil.example/pay"}, "actor": map[string]any{"user": user}, "device": deviceCeiling,
			})},
		},
		{
			// At the floor an artifact has no device or user, so the host is
			// the finding's own device and the account is the artifact's
			// actor.user. At the ceiling the artifact carries its device, and
			// the finding states a confidence score beside the id.
			name: "detection_finding", entities: 6,
			floor: []map[string]any{versioned(2004, "1.1.0", map[string]any{
				"finding_info": finding, "severity_id": 4, "confidence_id": 3, "device": device,
				"evidences": []any{map[string]any{"process": process, "actor": map[string]any{"user": user}, "src_endpoint": map[string]any{"ip": "10.0.4.55"}}},
			})},
			ceiling: []map[string]any{versioned(2004, "1.9.0", map[string]any{
				"finding_info": finding, "severity_id": 4, "confidence_id": 3, "confidence_score": 85, "device": deviceCeiling,
				"evidences": []any{map[string]any{"process": process, "actor": map[string]any{"user": user}, "src_endpoint": map[string]any{"ip": "10.0.4.55"}, "device": deviceCeiling}},
			})},
		},
		{
			name: "inventory_info", entities: 2,
			floor: []map[string]any{versioned(5001, "1.1.0", map[string]any{
				"device": with(device, map[string]any{"ip": "10.0.4.20", "os": map[string]any{"name": "Windows Server 2022", "type_id": 100}}), "unmapped": map[string]any{"criticality": "high"},
			})},
			ceiling: []map[string]any{versioned(5001, "1.9.0", map[string]any{
				"device": with(deviceCeiling, map[string]any{"ip": "10.0.4.20", "os": map[string]any{"name": "Windows Server 2022", "type_id": 100}}), "unmapped": map[string]any{"criticality": "high"},
			})},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, e := range append(append([]map[string]any{}, tc.floor...), tc.ceiling...) {
				if bad := ocsfschema.ValidateFields(e); len(bad) > 0 {
					t.Errorf("OCSF %s class %d: %v", pathStr(e, "metadata.version"), toInt(e["class_uid"]), bad)
				}
			}
			floorRefs, floorRels := entityIDs(t, tc.floor...)
			ceilRefs, ceilRels := entityIDs(t, tc.ceiling...)
			if len(floorRefs) != tc.entities {
				t.Errorf("the floor names %d entities; want %d", len(floorRefs), tc.entities)
			}
			if strings.Join(floorRefs, "\n") != strings.Join(ceilRefs, "\n") {
				t.Errorf("entities differ:\nfloor   %v\nceiling %v", floorRefs, ceilRefs)
			}
			if strings.Join(floorRels, "\n") != strings.Join(ceilRels, "\n") {
				t.Errorf("relationships differ:\nfloor   %v\nceiling %v", floorRels, ceilRels)
			}
			if f, c := scoProps(t, tc.props, tc.floor...), scoProps(t, tc.props, tc.ceiling...); strings.Join(f, "\n") != strings.Join(c, "\n") {
				t.Errorf("properties differ:\nfloor   %v\nceiling %v", f, c)
			}
		})
	}
}

// TestBothEndsEmailAttachment (§4.10, §4.11a, adr/0356, adr/0359): below
// OCSF 1.4.0 an attachment and a link are not on the email. The floor's
// email and its attachment event name the same message and file the
// ceiling's one email names. The attachment event names no message: its
// email_uid is a thread id, kept as thread_uid. So at the floor nothing
// records which email held the file, and no aliases edge joins anything.
func TestBothEndsEmailAttachment(t *testing.T) {
	sha := []any{map[string]any{"algorithm": "SHA-256", "algorithm_id": 3, "value": "beef"}}
	mail := func() map[string]any {
		return map[string]any{
			"from": "attacker@evil.example", "to": []any{"alice@corp.example"},
			"subject": "Invoice", "message_uid": "<abc123@evil.example>", "uid": "gw-0001",
		}
	}
	attachment := map[string]any{"name": "invoice.pdf.exe", "type_id": 1, "hashes": sha}
	floorMail := versioned(4009, "1.1.0", map[string]any{"email": mail()})
	floorFile := versioned(4011, "1.1.0", map[string]any{"activity_id": 2, "email_uid": "gw-0001", "file": attachment})
	ceilingMail := mail()
	ceilingMail["files"] = []any{attachment}
	ceilingMail["urls"] = []any{map[string]any{"url_string": "https://evil.example/pay"}}
	ceiling := versioned(4009, "1.9.0", map[string]any{"email": ceilingMail})
	for _, e := range []map[string]any{floorMail, floorFile, ceiling} {
		if bad := ocsfschema.ValidateFields(e); len(bad) > 0 {
			t.Fatalf("OCSF %s class %d: %v", pathStr(e, "metadata.version"), toInt(e["class_uid"]), bad)
		}
	}

	reg := testRegistry()
	norm := func(e map[string]any) NormalizationResult {
		t.Helper()
		res, err := reg.Normalize(makeEvent(e))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	fm, ff, c := norm(floorMail), norm(floorFile), norm(ceiling)
	ids := identity.NewResolver(testNS)
	strong := ids.EmailMessage("<abc123@evil.example>", "", "", time.Time{}, nil, "")
	file := scoOfType(c.SCOs, identity.TypeFile)

	if scoOfType(ff.SCOs, identity.TypeFile) != file || file == "" {
		t.Errorf("the attachment event's file is not the ceiling email's: %s vs %s", scoOfType(ff.SCOs, identity.TypeFile), file)
	}
	if !refsContain(fm.ObservedData[0].ObjectRefs, strong) || !refsContain(c.ObservedData[0].ObjectRefs, strong) {
		t.Error("both ends must name the message by its Message-ID")
	}
	// Ceiling: the message contains the file directly.
	if !hasEdge(c, RelContains, strong, file) {
		t.Errorf("ceiling: no contains %s → %s", strong, file)
	}
	// Floor: the attachment event names no message and keeps the thread id.
	if n := len(scosOf(ff, identity.TypeEmailMessage)); n != 0 || hasRel(ff.Relationships, RelContains) {
		t.Errorf("floor: the attachment event minted %d messages, relationships %v; want none", n, ff.Relationships)
	}
	if got := ff.ObservedData[0].Extensions["thread_uid"]; got != "gw-0001" {
		t.Errorf("floor: thread_uid = %v; want gw-0001", got)
	}
	for name, res := range map[string]NormalizationResult{"floor": fm, "attachment": ff, "ceiling": c} {
		if hasRel(res.Relationships, RelAliases) {
			t.Errorf("%s: aliases %v; want nothing", name, relsOf(res, RelAliases))
		}
	}
}
