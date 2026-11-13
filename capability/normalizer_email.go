package capability

import (
	"strings"

	"github.com/sd-strax/reckon/identity"
)

// emailNormalizer handles OCSF email_activity (class_uid 4009) → STIX (§4.10):
// the email-message, every address (from/to/cc/bcc), attachment files, and body
// URLs, with a contains relationship from the message to each attachment and URL.
type emailNormalizer struct{ r *identity.Resolver }

func (*emailNormalizer) ClassUID() int { return 4009 }

// 2: placeholder file hashes, adr/0211
// 3: OCSF classes and fields, adr/0356
func (*emailNormalizer) Version() int { return 3 }

func (n *emailNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "email_activity", n.Version())

	from := pathStr(p, "email.from")
	fromRef := b.emailAddr(from)
	toRefs := b.emailAddrs(p, "email.to")
	ccRefs := b.emailAddrs(p, "email.cc")
	// Blind copy is email.bcc[] from OCSF 1.9.0. Below that OCSF has no
	// field for it, so a source that reports one puts it in unmapped.bcc[].
	bccRefs := b.emailAddrs(p, "email.bcc")
	if len(bccRefs) == 0 {
		bccRefs = b.emailAddrs(p, "unmapped.bcc")
	}

	subject := pathStr(p, "email.subject")
	msgUID := strings.TrimSpace(pathStr(p, "email.message_uid"))
	props := map[string]any{
		"message_id": msgUID,
		"subject":    subject,
		"from_ref":   string(fromRef),
		"to_refs":    stringifyRefs(toRefs),
	}
	if len(ccRefs) > 0 {
		props["cc_refs"] = stringifyRefs(ccRefs)
	}
	if len(bccRefs) > 0 {
		props["bcc_refs"] = stringifyRefs(bccRefs)
	}
	msgID := b.emailMessage(msgUID, strings.TrimSpace(pathStr(p, "email.uid")), from, subject, props)

	// Attachments and body links, with a contains edge each. Both lists are
	// OCSF 1.4.0 and read when present. Below that an attachment is its own
	// email_file_activity event (§4.11a).
	for _, f := range objectsAt(p, "email.files") {
		if fileID := b.fileFrom(f); fileID != "" {
			b.addRel(RelContains, msgID, fileID)
		}
	}
	for _, u := range objectsAt(p, "email.urls") {
		if urlID := b.url(toString(fieldOf(u, "url_string"))); urlID != "" {
			b.addRel(RelContains, msgID, urlID)
		}
	}

	return b.finish(nil), nil
}

// emailMessage adds the email-message an email activity event names and
// returns its id. The identity follows the rule hosts and accounts follow
// (adr/0211, adr/0324, adr/0356):
//
//   - A Message-ID (email.message_uid) mints the strong message.
//   - With no Message-ID, the source's own id (email.uid) mints the weak
//     message on (source tool, uid), marked weak. It is the event's message.
//   - With both, the strong message is the event's, and the weak one is added
//     outside object_refs with an aliases edge to the strong one. That edge is
//     how a click, which names the message by the source's id alone, reaches
//     the delivered message.
//   - With neither, the message is keyed on (from, subject, date).
func (b *resultBuilder) emailMessage(msgUID, uid, from, subject string, props map[string]any) identity.STIXID {
	weak, weakProps := b.weakEmailMessage(uid)
	if msgUID == "" && weak != "" {
		for k, v := range weakProps {
			props[k] = v
		}
		return b.addSCO(weak, identity.TypeEmailMessage, props)
	}
	strong := b.addSCO(b.r.EmailMessage(msgUID, from, subject, b.evt.Time), identity.TypeEmailMessage, props)
	if msgUID != "" && weak != "" {
		b.addLinkedSCO(weak, identity.TypeEmailMessage, weakProps)
		b.addRel(RelAliases, weak, strong)
	}
	return strong
}

// weakEmailMessage mints the weak email-message the source's own message id
// names, on (source tool, uid), with the properties a weak message carries.
// It returns "" for an empty uid.
func (b *resultBuilder) weakEmailMessage(uid string) (identity.STIXID, map[string]any) {
	id := b.r.WeakEmailMessage(b.evt.SourceTool, uid)
	if id == "" {
		return "", nil
	}
	return id, map[string]any{"source_tool": b.evt.SourceTool, "email_uid": strings.TrimSpace(uid), "weak": true}
}

// eventEmailMessage adds the weak email-message a click or an attachment
// event names by its email_uid, in object_refs: the event names no other.
func (b *resultBuilder) eventEmailMessage() identity.STIXID {
	id, props := b.weakEmailMessage(pathStr(b.evt.Payload, "email_uid"))
	return b.addSCO(id, identity.TypeEmailMessage, props)
}

// emailURLNormalizer handles OCSF email_url_activity (class_uid 4012) → STIX
// (§4.11): the clicked URL, the clicker, the weak email-message the click
// names by email_uid, and the host, with a clicked edge (user → url) and a
// contains edge (message → url). 4011 is email_file_activity, not this class
// (adr/0356).
type emailURLNormalizer struct{ r *identity.Resolver }

func (*emailURLNormalizer) ClassUID() int { return 4012 }

// 2: host identity, adr/0211; 3: account identity, adr/0324
// 4: OCSF classes and fields, adr/0356
func (*emailURLNormalizer) Version() int { return 4 }

func (n *emailURLNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "email_url_activity", n.Version())
	b.deviceObjects("device")

	urlID := b.url(pathStr(p, "url.url_string"))
	host := b.host("device")
	user := b.userAccount("actor.user", "device", host)
	msgID := b.eventEmailMessage()

	if user != "" && urlID != "" {
		b.addRel(RelClicked, user, urlID)
	}
	if msgID != "" && urlID != "" {
		b.addRel(RelContains, msgID, urlID)
	}

	return b.finish(nil), nil
}

// emailFileNormalizer handles OCSF email_file_activity (class_uid 4011) → STIX
// (§4.11a): the attachment file, the weak email-message the event names by
// email_uid, the actor and the host, with a contains edge (message → file).
// Below OCSF 1.4.0 this event is the only place an attachment appears.
type emailFileNormalizer struct{ r *identity.Resolver }

func (*emailFileNormalizer) ClassUID() int { return 4011 }

// Version starts at 4. Versions 1 to 3 of class 4011 were the link-click
// normalizer registered under this number before adr/0356, and the
// ObservedData id carries the version but not the normalizer's name.
func (*emailFileNormalizer) Version() int { return 4 }

func (n *emailFileNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "email_file_activity", n.Version())
	b.deviceObjects("device")

	fileID := b.file("file")
	host := b.host("device")
	b.userAccount("actor.user", "device", host)
	msgID := b.eventEmailMessage()

	if msgID != "" && fileID != "" {
		b.addRel(RelContains, msgID, fileID)
	}

	return b.finish(activityExt(p)), nil
}

// stringifyRefs renders a ref slice for storage in SCO properties.
func stringifyRefs(refs []identity.STIXID) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, string(r))
	}
	return out
}
