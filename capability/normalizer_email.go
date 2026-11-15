package capability

import (
	"bufio"
	"mime"
	"net/textproto"
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
// 4: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; is_multipart from
// the raw header, addresses that are addr-specs, URLs that are URIs, file
// hashes and names STIX can hold, adr/0357
func (*emailNormalizer) Version() int { return 4 }

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
	// A property the record does not give is left out, not written empty:
	// STIX reads an absent property as unknown (adr/0357).
	props := map[string]any{"is_multipart": isMultipart(pathStr(p, "email.raw_header"))}
	if msgUID != "" {
		props["message_id"] = msgUID
	}
	if subject != "" {
		props["subject"] = subject
	}
	if fromRef != "" {
		props["from_ref"] = string(fromRef)
	}
	if len(toRefs) > 0 {
		props["to_refs"] = stringifyRefs(toRefs)
	}
	if len(ccRefs) > 0 {
		props["cc_refs"] = stringifyRefs(ccRefs)
	}
	if len(bccRefs) > 0 {
		props["bcc_refs"] = stringifyRefs(bccRefs)
	}
	// The message is keyed on the sender it stores: none when the address
	// was left out (adr/0357).
	if fromRef == "" {
		from = ""
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
		// The event's own properties stand; the weak message adds its mark.
		for k, v := range weakProps {
			if _, ok := props[k]; !ok {
				props[k] = v
			}
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
	// STIX requires is_multipart. A record that names the message only by
	// the source's id says nothing about its parts, so it is false.
	return id, map[string]any{"is_multipart": false, "source_tool": b.evt.SourceTool, "email_uid": strings.TrimSpace(uid), "weak": true}
}

// isMultipart reports whether an email's raw header says the message has
// parts: its Content-Type header's media type is multipart/... (adr/0357).
// OCSF has no field for it, so the raw header is the only place a source can
// say it. The header is read as RFC 5322 defines it (net/textproto): folded
// lines are unfolded first (§2.2.3), and the header name is matched without
// case. A header that names no Content-Type, or no raw header at all, is
// false.
func isMultipart(rawHeader string) bool {
	if strings.TrimSpace(rawHeader) == "" {
		return false
	}
	// The header block ends at a blank line; a raw header may lack one. A
	// line the reader cannot parse ends the block, and what came before it
	// is kept.
	r := textproto.NewReader(bufio.NewReader(strings.NewReader(strings.TrimRight(rawHeader, "\r\n") + "\r\n\r\n")))
	header, _ := r.ReadMIMEHeader()
	value := strings.TrimSpace(header.Get("Content-Type"))
	if value == "" {
		return false
	}
	if mediaType, _, err := mime.ParseMediaType(value); err == nil {
		return strings.HasPrefix(mediaType, "multipart/")
	}
	return strings.HasPrefix(strings.ToLower(value), "multipart/")
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
// 5: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; is_multipart,
// URLs that are URIs, adr/0357
func (*emailURLNormalizer) Version() int { return 5 }

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
// 5: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; is_multipart,
// file hashes and names STIX can hold, adr/0357
func (*emailFileNormalizer) Version() int { return 5 }

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
