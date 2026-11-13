package capability

import (
	"strings"

	"github.com/sd-strax/reckon/identity"
)

// The two OCSF Windows registry classes one normalizer handles (§4.6,
// adr/0356). 1003 is OCSF's Kernel Activity and is not registered.
const (
	classRegistryKey   = 201001 // Registry Key Activity: reg_key.path
	classRegistryValue = 201002 // Registry Value Activity: reg_value.*
)

// registryNormalizer handles OCSF registry_key_activity (class_uid 201001)
// and registry_value_activity (201002) → STIX (§4.6): the host-scoped
// registry key, the actor process, and the host, with activity_id preserved
// on the ObservedData. It is registered once per class and reads only that
// class's fields: a key event names the key in reg_key.path, a value event in
// reg_value.path, so both yield the same key object.
type registryNormalizer struct {
	r     *identity.Resolver
	class int
}

func (n *registryNormalizer) ClassUID() int { return n.class }

// 2: host identity, adr/0211
// 3: OCSF classes and fields, adr/0356
func (*registryNormalizer) Version() int { return 3 }

func (n *registryNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	name := "registry_key_activity"
	if n.class == classRegistryValue {
		name = "registry_value_activity"
	}
	b := newBuilder(n.r, evt, name, n.Version())

	host := b.host("device")
	var fullPath, valueName, valueData string
	props := map[string]any{}
	if n.class == classRegistryValue {
		fullPath = pathStr(p, "reg_value.path")
		valueName = pathStr(p, "reg_value.name")
		if v, ok := lookupPath(p, "reg_value.data"); ok {
			valueData = scalarString(v)
		}
		// The value's type: the caption, else the id.
		if t := pathStr(p, "reg_value.type"); t != "" {
			props["value_type"] = t
		} else if id := pathInt(p, "reg_value.type_id"); id != 0 {
			props["value_type_id"] = id
		}
	} else {
		fullPath = pathStr(p, "reg_key.path")
	}
	if hive, keyPath := splitRegistryPath(fullPath); hive != "" || keyPath != "" {
		props["hive"] = hive
		props["key_path"] = keyPath
		props["value_name"] = valueName
		props["value_data"] = valueData
		b.addSCO(n.r.RegistryKey(host, hive, keyPath, valueName), identity.TypeRegistryKey, props)
	}
	b.actorProcess(p, host)

	return b.finish(activityExt(p)), nil
}

// splitRegistryPath splits an OCSF registry path, which is the full key path
// with the hive first, into the hive (the first segment) and the rest.
func splitRegistryPath(full string) (hive, keyPath string) {
	full = strings.Trim(strings.TrimSpace(full), `\`)
	hive, keyPath, _ = strings.Cut(full, `\`)
	return hive, strings.TrimLeft(keyPath, `\`)
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
