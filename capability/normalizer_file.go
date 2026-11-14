package capability

import "github.com/sd-strax/reckon/identity"

// fileActivityNormalizer handles OCSF file_activity (class_uid 1001) → STIX
// (§4.5): the file (with its parent directory), the actor process, the host, and
// the executing user, with activity_id preserved on the ObservedData.
type fileActivityNormalizer struct{ r *identity.Resolver }

func (*fileActivityNormalizer) ClassUID() int { return 1001 }

// 2: host identity, adr/0211; 3: account identity, adr/0324
// 4: OCSF classes and fields, adr/0356
func (*fileActivityNormalizer) Version() int { return 4 }

func (n *fileActivityNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "file_activity", n.Version())
	b.deviceObjects("device")

	fileID := b.file("file")
	if dir := parentDir(pathStr(p, "file.path")); dir != "" {
		dirID := b.addSCO(n.r.Directory(dir), identity.TypeDirectory, map[string]any{"path": dir})
		b.addRel(RelParentDirectoryOf, dirID, fileID)
	}

	host := b.host("device")
	b.actorProcess(p, host)
	b.userAccount("actor.user", "device", host)

	return b.finish(activityExt(p)), nil
}
