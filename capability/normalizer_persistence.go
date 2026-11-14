package capability

import (
	"encoding/json"
	"fmt"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/internal/ocsf"
)

// The two OCSF Windows registry classes one normalizer handles (§4.6,
// adr/0356). 1003 is OCSF's Kernel Activity and is not registered.
const (
	classRegistryKey   = 201001 // Registry Key Activity: reg_key.path
	classRegistryValue = 201002 // Registry Value Activity: reg_value.*
)

// registryNormalizer handles OCSF registry_key_activity (class_uid 201001)
// and registry_value_activity (201002) → STIX (§4.6, adr/0341): the
// host-scoped windows-registry-key, the actor process, and the host, with
// activity_id preserved on the ObservedData. It is registered once per class
// and reads only that class's fields: a key event names the key in
// reg_key.path, a value event in reg_value.path, so both yield the same type
// of object. A value event's object holds that one value in values, and its
// id carries the value's name.
type registryNormalizer struct {
	r     *identity.Resolver
	class int
}

func (n *registryNormalizer) ClassUID() int { return n.class }

// 2: host identity, adr/0211
// 3: OCSF classes and fields, adr/0356
// 4: STIX windows-registry-key, adr/0341
func (*registryNormalizer) Version() int { return 4 }

func (n *registryNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	name := "registry_key_activity"
	if n.class == classRegistryValue {
		name = "registry_value_activity"
	}
	b := newBuilder(n.r, evt, name, n.Version())

	host := b.host("device")
	var path string
	var valueName *string
	var value map[string]any
	if n.class == classRegistryValue {
		path = pathStr(p, "reg_value.path")
		// A string name wins. No name with is_default true is the default
		// value, named "". Neither names no value: the key alone.
		regValue, _ := p["reg_value"].(map[string]any)
		if s, ok := ocsf.RegistryValueName(regValue); ok {
			valueName = &s
			value = registryValue(p, s)
		}
	} else {
		path = pathStr(p, "reg_key.path")
	}
	if key := identity.RegistryKeyPath(path); key != "" {
		props := map[string]any{"key": key}
		if value != nil {
			props["values"] = []any{value}
		}
		b.addSCO(n.r.RegistryKey(host, key, valueName), identity.TypeWindowsRegistryKey, props)
	}
	b.actorProcess(p, host)

	return b.finish(activityExt(p)), nil
}

// registryValue is the windows-registry-value-type of a value event: the name
// as written ("" for the default value), the data as a string, and the STIX
// data_type when the source's type is one (adr/0341).
func registryValue(p map[string]any, name string) map[string]any {
	value := map[string]any{"name": name}
	if v, ok := lookupPath(p, "reg_value.data"); ok && v != nil {
		if data, ok := registryData(v); ok {
			value["data"] = data
		}
	}
	dataType := pathStr(p, "reg_value.type")
	if !stixRegistryDataTypes[dataType] {
		dataType = ocsf.RegistryValueTypes[pathInt(p, "reg_value.type_id")]
	}
	if stixRegistryDataTypes[dataType] {
		value["data_type"] = dataType
	}
	return value
}

// registryData is a value's data as a string. OCSF types it as JSON, so a
// value that is not a string is written as its JSON text. A number that
// float64 cannot state exactly (a REG_QWORD FILETIME at or above 2^53, which
// arrives rounded) reports false, and the value leaves data out as it does
// when the source gives none.
func registryData(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		if !exactNumber(x) {
			return "", false
		}
	}
	text, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v), true
	}
	return string(text), true
}

// stixRegistryDataTypes are the data_type names STIX 2.1 allows on a
// windows-registry-value-type (windows-registry-datatype-enum).
var stixRegistryDataTypes = map[string]bool{
	"REG_NONE": true, "REG_SZ": true, "REG_EXPAND_SZ": true, "REG_BINARY": true,
	"REG_DWORD": true, "REG_DWORD_BIG_ENDIAN": true, "REG_DWORD_LITTLE_ENDIAN": true,
	"REG_LINK": true, "REG_MULTI_SZ": true, "REG_RESOURCE_LIST": true,
	"REG_FULL_RESOURCE_DESCRIPTION": true, "REG_RESOURCE_REQUIREMENTS_LIST": true,
	"REG_QWORD": true, "REG_INVALID_TYPE": true,
}

// scheduledJobNormalizer handles OCSF scheduled_job_activity (class_uid 1006) →
// STIX (§4.7): the host-scoped scheduled task, its principal user, and the host.
type scheduledJobNormalizer struct{ r *identity.Resolver }

func (*scheduledJobNormalizer) ClassUID() int { return 1006 }

// 2: host identity, adr/0211; 3: account identity, adr/0324
// 4: OCSF classes and fields, adr/0356
func (*scheduledJobNormalizer) Version() int { return 4 }

func (n *scheduledJobNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "scheduled_job_activity", n.Version())
	b.deviceObjects("device")

	host := b.host("device")
	principal := b.userAccount("job.user", "device", host)
	name := pathStr(p, "job.name")
	path := pathStr(p, "job.file.path")
	if name != "" || path != "" {
		cmds := jobCommandLines(p)
		props := map[string]any{
			"name":               name,
			"path":               path,
			"command_line":       "",
			"principal_user_ref": string(principal),
		}
		if len(cmds) > 0 {
			props["command_line"] = cmds[0]
		}
		if len(cmds) > 1 {
			props["command_lines"] = cmds
		}
		b.addSCO(n.r.ScheduledTask(host, name, path), identity.TypeScheduledTask, props)
	}

	return b.finish(activityExt(p)), nil
}

// jobCommandLines reads a job's command lines: one per action in
// job.job_actions[] (OCSF 1.9.0), else the deprecated job.cmd_line (03 §4.16:
// the replacement first, the old field as the fallback).
func jobCommandLines(p map[string]any) []string {
	var out []string
	for _, action := range objectsAt(p, "job.job_actions") {
		if c := toString(fieldOf(action, "cmd_line")); c != "" {
			out = append(out, c)
		}
	}
	if len(out) > 0 {
		return out
	}
	if c := pathStr(p, "job.cmd_line"); c != "" {
		return []string{c}
	}
	return nil
}

// moduleNormalizer handles OCSF module_activity (class_uid 1005) → STIX (§4.8):
// the loaded module file, the loading process, and the host, with a loads edge.
// 1009 is OCSF's Script Activity and is not this class (adr/0356).
type moduleNormalizer struct{ r *identity.Resolver }

func (*moduleNormalizer) ClassUID() int { return 1005 }

// 2: host identity, adr/0211
// 3: OCSF classes and fields, adr/0356
func (*moduleNormalizer) Version() int { return 3 }

func (n *moduleNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	b := newBuilder(n.r, evt, "module_activity", n.Version())

	host := b.host("device")
	fileID := b.file("module.file")
	procID := b.actorProcess(evt.Payload, host)
	if procID != "" && fileID != "" {
		b.addRel(RelLoads, procID, fileID)
	}

	return b.finish(nil), nil
}
