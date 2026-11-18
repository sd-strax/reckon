package capability

import "github.com/sd-strax/reckon/identity"

// fileActivityNormalizer handles OCSF file_activity (class_uid 1001) → STIX
// (§4.5): the file (with its parent directory), the actor process, the host, and
// the executing user, with activity_id preserved on the ObservedData.
type fileActivityNormalizer struct{ r *identity.Resolver }

func (*fileActivityNormalizer) ClassUID() int { return 1001 }

// 2: host identity, adr/0211; 3: account identity, adr/0324
// 4: OCSF classes and fields, adr/0356
// 5: OCSF read gaps, adr/0359
// 6: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; file hashes,
// names and sizes STIX can hold, adr/0357
// 7: an account typed Windows only on a Windows signal, account type 19
// read as one, adr/0366
// 8: the account type read by the one rule, a caption for every type and
// the id over it, adr/0367
// 9: the account minted for each user object recorded in user_accounts,
// adr/0398
func (*fileActivityNormalizer) Version() int { return 9 }

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
