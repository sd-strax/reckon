package capability

import (
	"strings"

	"github.com/sd-strax/reckon/identity"
)

// emailNormalizer handles OCSF email_activity (class_uid 4009) → STIX (§4.10):
// the email-message, every address (from/to/cc/bcc), attachment files, and body
// URLs, with a contains relationship from the message to each attachment and URL.
//
// The message is keyed on its Message-ID (email.message_uid), else on (from,
// subject, date, recipients, thread_uid). email.uid is the id of the email
// thread, not of one message, so it is kept on the message as thread_uid.
// It mints no weak message and no aliases edge: two messages of one thread
// would then read as one (adr/0359).
type emailNormalizer struct{ r *identity.Resolver }

func (*emailNormalizer) ClassUID() int { return 4009 }

// 2: placeholder file hashes, adr/0211
// 3: OCSF classes and fields, adr/0356
// 4: OCSF read gaps, adr/0359
func (*emailNormalizer) Version() int { return 4 }

func (n *emailNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "email_activity", n.Version())

	from := pathStr(p, "email.from")
	fromRef := b.emailAddr(from)
	toRefs, to := b.emailAddrs(p, "email.to")
	ccRefs, cc := b.emailAddrs(p, "email.cc")
	// Blind copy is email.bcc[] from OCSF 1.9.0. Below that OCSF has no
	// field for it, so a source that reports one puts it in unmapped.bcc[].
	bccRefs, bcc := b.emailAddrs(p, "email.bcc")
	if len(bccRefs) == 0 {
		bccRefs, bcc = b.emailAddrs(p, "unmapped.bcc")
	}
	// The recipients are every address the message was sent to: the header
	// to, cc and blind copy, and the envelope. With no Message-ID they are
	// part of its identity, so two copies of one run sent to different
	// mailboxes stay two messages (adr/0359).
	recipients := append(append(append([]string{}, to...), cc...), bcc...)
	recipients = append(recipients, envelopeRecipients(p)...)

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
	// From OCSF 1.4.0, email.uid is the id of the email thread. It is read as
	// one at every version and kept as a property. It is never the
	// message's identity on its own: with no Message-ID it only keeps apart
	// messages of different threads (adr/0359).
	thread := strings.TrimSpace(pathStr(p, "email.uid"))
	if thread != "" {
		props["thread_uid"] = thread
	}
	msgID := b.addSCO(b.r.EmailMessage(msgUID, from, subject, b.evt.Time, recipients, thread), identity.TypeEmailMessage, props)

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

// envelopeRecipients reads the envelope addresses of an email: smtp_to[]
// (the SMTP RCPT TO values), and the Delivered-To values, delivered_to_list[]
// from OCSF 1.4.0, else the deprecated single delivered_to (03 §4.16). They
// count toward the message's identity only; they mint no email-addr.
func envelopeRecipients(p map[string]any) []string {
	out := stringsAt(p, "email.smtp_to")
	if delivered := stringsAt(p, "email.delivered_to_list"); len(delivered) > 0 {
		return append(out, delivered...)
	}
	if d := pathStr(p, "email.delivered_to"); d != "" {
		out = append(out, d)
	}
	return out
}

// stringsAt reads the strings of the list at path of p.
func stringsAt(p map[string]any, path string) []string {
	v, ok := lookupPath(p, path)
	if !ok {
		return nil
	}
	return stringSlice(v)
}

// threadExt adds the event's email_uid to ext as thread_uid, trimmed, and
// returns ext. A click or an attachment event names its email only by
// email_uid, which OCSF matches to email.uid from 1.4.0: a thread id. So it
// is kept on the observation and mints no email-message (adr/0359). An empty
// email_uid adds nothing.
func threadExt(p, ext map[string]any) map[string]any {
	t := strings.TrimSpace(pathStr(p, "email_uid"))
	if t == "" {
		return ext
	}
	if ext == nil {
		ext = map[string]any{}
	}
	ext["thread_uid"] = t
	return ext
}

// emailURLNormalizer handles OCSF email_url_activity (class_uid 4012) → STIX
// (§4.11): the clicked URL, the clicker and the host, with a clicked edge
// (user → url). The click's email_uid names a thread, not one message, so
// it mints no email-message and rides the observation as thread_uid
// (adr/0359). 4011 is email_file_activity, not this class (adr/0356).
type emailURLNormalizer struct{ r *identity.Resolver }

func (*emailURLNormalizer) ClassUID() int { return 4012 }

// 2: host identity, adr/0211; 3: account identity, adr/0324
// 4: OCSF classes and fields, adr/0356
// 5: OCSF read gaps, adr/0359
func (*emailURLNormalizer) Version() int { return 5 }

func (n *emailURLNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "email_url_activity", n.Version())
	b.deviceObjects("device")

	urlID := b.url(pathStr(p, "url.url_string"))
	host := b.host("device")
	user := b.userAccount("actor.user", "device", host)

	if user != "" && urlID != "" {
		b.addRel(RelClicked, user, urlID)
	}

	return b.finish(threadExt(p, nil)), nil
}

// emailFileNormalizer handles OCSF email_file_activity (class_uid 4011) → STIX
// (§4.11a): the attachment file, the actor and the host. Like a click, the
// event's email_uid names a thread, so it mints no email-message and rides
// the observation as thread_uid (adr/0359). Below OCSF 1.4.0 this event is
// the only place an attachment appears.
type emailFileNormalizer struct{ r *identity.Resolver }

func (*emailFileNormalizer) ClassUID() int { return 4011 }

// Version starts at 4. Versions 1 to 3 of class 4011 were the link-click
// normalizer registered under this number before adr/0356, and the
// ObservedData id carries the version but not the normalizer's name.
// 5: OCSF read gaps, adr/0359
func (*emailFileNormalizer) Version() int { return 5 }

func (n *emailFileNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "email_file_activity", n.Version())
	b.deviceObjects("device")

	b.file("file")
	host := b.host("device")
	b.userAccount("actor.user", "device", host)

	return b.finish(threadExt(p, activityExt(p))), nil
}

// stringifyRefs renders a ref slice for storage in SCO properties.
func stringifyRefs(refs []identity.STIXID) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, string(r))
	}
	return out
}
