package capability

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/stix"
)

// Class rule R1 (adr/0357): every id is minted from the values the object
// stores, never from raw input. remintProblems re-mints each object's id from
// its stored STIX JSON through the resolver and reports every object whose id
// differs. TestMain runs it over every result the package's tests produced,
// and TestBundledFixturesSerialize over every bundled fixture.
//
// Exceptions, each a value the id uses that STIX has no property for on the
// object, and so is not stored on it:
//
//   - The host scope of a process, a windows-registry-key, a
//     reckon-scheduled-task and a host-local user-account (03 §7.2). The host
//     is a reckon-host of the same result, and the check tries each one.
//   - A process with no pid or no start time has a random id: STIX gives a
//     process no id contributing properties (03 §7.2).
//   - An email-message with no Message-ID is keyed on its sender, subject,
//     recipients, thread and the record's time. The record's time is the
//     observation's first_observed, not a property of the message.
//   - A sighting is keyed on the vendor that saw it, whose name is stored on
//     the identity its where_sighted_refs names.
//
// observed-data is minted from the stored record (its class, time, source and
// payload hash), which the observation does not repeat, and is not checked.
func remintProblems(ids *identity.Resolver, res NormalizationResult) []string {
	stored := map[string]map[string]any{}
	var order []string
	for _, o := range stixObjects(res) {
		raw, err := stix.Marshal(o)
		if err != nil {
			continue // checkProduced reports it
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		stored[o.ID] = m
		order = append(order, o.ID)
	}
	var hosts []identity.STIXID
	for _, id := range order {
		if strings.HasPrefix(id, identity.TypeHost+"--") {
			hosts = append(hosts, identity.STIXID(id))
		}
	}
	var eventTime time.Time
	if len(res.ObservedData) > 0 {
		eventTime = res.ObservedData[0].FirstObserved
	}

	var out []string
	for _, id := range order {
		m := stored[id]
		typ, _ := m["type"].(string)
		candidates, skip := remintCandidates(ids, typ, m, stored, hosts, eventTime)
		if skip {
			continue
		}
		found := false
		for _, c := range candidates {
			if string(c) == id {
				found = true
				break
			}
		}
		if !found {
			out = append(out, fmt.Sprintf("%s: the id re-minted from the stored object is %v", id, candidates))
		}
	}
	return out
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func intPtr(m map[string]any, k string) *int {
	f, ok := m[k].(float64)
	if !ok {
		return nil
	}
	n := int(f)
	return &n
}

// remintCandidates returns the ids the stored object may have, more than one
// only where an exception leaves a scope to try. skip is true for a type the
// check does not cover.
func remintCandidates(ids *identity.Resolver, typ string, m map[string]any, stored map[string]map[string]any, hosts []identity.STIXID, eventTime time.Time) (out []identity.STIXID, skip bool) {
	perHost := func(f func(identity.STIXID) identity.STIXID) []identity.STIXID {
		out := []identity.STIXID{f("")}
		for _, h := range hosts {
			out = append(out, f(h))
		}
		return out
	}
	switch typ {
	case identity.TypeIPv4Addr:
		return []identity.STIXID{ids.IPv4Addr(str(m, "value"))}, false
	case identity.TypeIPv6Addr:
		return []identity.STIXID{ids.IPv6Addr(str(m, "value"))}, false
	case identity.TypeDomainName:
		return []identity.STIXID{ids.DomainName(str(m, "value"))}, false
	case identity.TypeURL:
		return []identity.STIXID{ids.URL(str(m, "value"))}, false
	case identity.TypeEmailAddr:
		return []identity.STIXID{ids.EmailAddr(str(m, "value"))}, false
	case identity.TypeDirectory:
		return []identity.STIXID{ids.Directory(str(m, "path"))}, false
	case identity.TypeArtifact:
		h, _ := m["hashes"].(map[string]any)
		sha, _ := h["SHA-256"].(string)
		if str(m, "url") != RecordURLPrefix+sha {
			return nil, false // the url must name the same hash
		}
		return []identity.STIXID{ids.ArtifactByHash(sha)}, false
	case identity.TypeFile:
		hashes := map[string]string{}
		if h, ok := m["hashes"].(map[string]any); ok {
			for k, v := range h {
				hashes[k], _ = v.(string)
			}
		}
		var size int64
		if p := intPtr(m, "size"); p != nil {
			size = int64(*p)
		}
		return []identity.STIXID{ids.File(identity.FileIdentity{
			Hashes: hashes, Name: str(m, "name"), ParentDirectory: parentDir(stix.FilePath.String(m)), Size: size,
		})}, false
	case identity.TypeNetworkTraffic:
		var protocols []string
		if list, ok := m["protocols"].([]any); ok {
			for _, p := range list {
				s, _ := p.(string)
				protocols = append(protocols, s)
			}
		}
		return []identity.STIXID{ids.NetworkTraffic(identity.STIXID(str(m, "src_ref")), identity.STIXID(str(m, "dst_ref")),
			intPtr(m, "src_port"), intPtr(m, "dst_port"), protocols)}, false
	case identity.TypeProcess:
		pid := intPtr(m, "pid")
		started, _ := time.Parse(time.RFC3339Nano, str(m, "created_time"))
		if pid == nil || started.IsZero() || len(hosts) == 0 {
			return nil, true // a random id: the exception above
		}
		return perHost(func(h identity.STIXID) identity.STIXID { id, _ := ids.Process(h, *pid, started); return id }), false
	case identity.TypeWindowsRegistryKey:
		var name *string
		if vals, ok := m["values"].([]any); ok && len(vals) > 0 {
			v, _ := vals[0].(map[string]any)
			s, _ := v["name"].(string)
			name = &s
		}
		return perHost(func(h identity.STIXID) identity.STIXID { return ids.RegistryKey(h, str(m, "key"), name) }), false
	case identity.TypeScheduledTask:
		return perHost(func(h identity.STIXID) identity.STIXID { return ids.ScheduledTask(h, str(m, "name"), str(m, "path")) }), false
	case identity.TypeUserAccount:
		return perHost(func(h identity.STIXID) identity.STIXID {
			return ids.Account(identity.AccountIdentity{Type: str(m, "account_type"), Login: str(m, "account_login"), UserID: str(m, "user_id"), HostRef: h})
		}), false
	case identity.TypeEmailMessage:
		value := func(ref any) string {
			r, _ := ref.(string)
			return str(stored[r], "value")
		}
		from := ""
		if ref := str(m, "from_ref"); ref != "" {
			from = value(ref)
		}
		var recipients []string
		refs := func(v any) {
			arr, _ := v.([]any)
			for _, r := range arr {
				recipients = append(recipients, value(r))
			}
		}
		refs(m["to_refs"])
		refs(m["cc_refs"])
		refs(m["bcc_refs"])
		ext := stix.ExtensionFrom(m, stix.ExtEmailMessage)
		refs(ext["envelope_to_refs"])
		thread, _ := ext["thread_uid"].(string)
		return []identity.STIXID{ids.EmailMessage(str(m, "message_id"), from, str(m, "subject"), eventTime, recipients, thread)}, false
	case identity.TypeHost:
		return []identity.STIXID{ids.Host(identity.HostIdentity{
			Hostname: str(m, "hostname"), MAC: str(m, "mac"), SourceTool: str(m, "source_tool"), SourceUID: str(m, "device_uid"),
		})}, false
	case identity.TypeGroup:
		return []identity.STIXID{ids.Group(str(m, "directory"), str(m, "group_name"))}, false
	case identity.TypeCloudAccount:
		return []identity.STIXID{ids.CloudAccount(str(m, "provider"), str(m, "account_uid"))}, false
	case identity.TypeCloudResource:
		id, _ := ids.CloudResource(identity.CloudResourceIdentity{
			Provider: str(m, "provider"), UID: str(m, "uid"), AccountRef: identity.STIXID(str(m, "account_ref")),
			Type: str(m, "resource_type"), Region: str(m, "region"), Zone: str(m, "zone"), Group: str(m, "group"),
			Namespace: str(m, "namespace"), Name: str(m, "name"),
		})
		return []identity.STIXID{id}, false
	case identity.TypeIdentity:
		if str(m, "identity_class") == "individual" {
			ext := stix.ExtensionFrom(m, stix.ExtPerson)
			d, _ := ext["directory"].(string)
			i, _ := ext["directory_id"].(string)
			return []identity.STIXID{ids.Person(d, i)}, false
		}
		return []identity.STIXID{ids.Identity(str(m, "name"))}, false
	case "indicator":
		// The finding uid is the external reference under the vendor's
		// name, the name stored on the identity created_by_ref names.
		vendor := str(stored[str(m, "created_by_ref")], "name")
		uid := ""
		if refs, ok := m["external_references"].([]any); ok {
			for _, r := range refs {
				ref, _ := r.(map[string]any)
				if ref["source_name"] == vendor {
					uid, _ = ref["external_id"].(string)
				}
			}
		}
		return []identity.STIXID{ids.Indicator(identity.STIXID(str(m, "created_by_ref")), str(m, "pattern"), str(m, "pattern_type"), uid)}, false
	case "sighting":
		seen, _ := time.Parse(time.RFC3339Nano, str(m, "first_seen"))
		var vendor string
		if refs, ok := m["where_sighted_refs"].([]any); ok && len(refs) > 0 {
			ref, _ := refs[0].(string)
			vendor = str(stored[ref], "name")
		}
		var od []identity.STIXID
		if refs, ok := m["observed_data_refs"].([]any); ok {
			for _, r := range refs {
				s, _ := r.(string)
				od = append(od, identity.STIXID(s))
			}
		}
		return []identity.STIXID{ids.Sighting(identity.STIXID(str(m, "sighting_of_ref")), seen, vendor, od)}, false
	case "relationship":
		return []identity.STIXID{ids.Relationship(str(m, "relationship_type"), identity.STIXID(str(m, "source_ref")), identity.STIXID(str(m, "target_ref")))}, false
	}
	return nil, true
}
