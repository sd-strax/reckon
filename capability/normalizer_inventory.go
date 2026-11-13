package capability

import "github.com/sd-strax/reckon/identity"

// deviceInventoryNormalizer handles OCSF inventory_info (class_uid 5001, Device
// Inventory Info) → STIX (§4.17): the x-host the inventory names, shaped by the shared host
// extractor, under one ObservedData that carries the os, owner and criticality
// in its extensions. An inventory record naming the hostname beside the
// source's device id is the record that aliases the source's weak host to the
// strong one (adr/0211).
type deviceInventoryNormalizer struct{ r *identity.Resolver }

func (*deviceInventoryNormalizer) ClassUID() int { return 5001 }

// Version starts at 2: a 5001 record normalized before this normalizer
// existed went through the opaque default at version 1, and the
// ObservedData id carries the version but not the normalizer's name, so a
// version 1 here would mint the same id and keep the entity-less observation.
// 3: OCSF classes and fields, adr/0356
func (*deviceInventoryNormalizer) Version() int { return 3 }

func (n *deviceInventoryNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "inventory_info", n.Version())
	b.host("device")
	b.ipAddr("device.ip")

	ext := map[string]any{}
	put := func(k, v string) {
		if v != "" {
			ext[k] = v
		}
	}
	put("os_name", pathStr(p, "device.os.name"))
	put("owner_name", pathStr(p, "device.owner.name"))
	// OCSF defines no criticality on a device, so it is the source's own
	// field (adr/0356).
	put("criticality", pathStr(p, "unmapped.criticality"))
	if len(ext) == 0 {
		ext = nil
	}
	return b.finish(ext), nil
}
