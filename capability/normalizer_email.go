package capability

import (
	"bufio"
	"mime"
	"net/textproto"
	"slices"
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
// 5: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; is_multipart from
// the raw header, addresses that are addr-specs, URLs that are URIs, file
// hashes and names STIX can hold, the thread and envelope recipients stored
// on the message, addresses cleaned up before they are checked, and a
// recipient that is not an address kept as text, adr/0357
func (*emailNormalizer) Version() int { return 5 }

func (n *emailNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	p := evt.Payload
	b := newBuilder(n.r, evt, "email_activity", n.Version())

	fromRef, from := b.emailAddr(pathStr(p, "email.from"))
	toRefs, to, unparsed := b.emailAddrs(p, "email.to")
	ccRefs, cc, ccUnparsed := b.emailAddrs(p, "email.cc")
	// Blind copy is email.bcc[] from OCSF 1.9.0. Below that OCSF has no
	// field for it, so a source that reports one puts it in unmapped.bcc[].
	bccRefs, bcc, bccUnparsed := b.emailAddrs(p, "email.bcc")
	if len(bccRefs) == 0 && len(bccUnparsed) == 0 {
		bccRefs, bcc, bccUnparsed = b.emailAddrs(p, "unmapped.bcc")
	}
	unparsed = append(append(unparsed, ccUnparsed...), bccUnparsed...)
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
	// From OCSF 1.4.0, email.uid is the id of the email thread. It is read as
	// one at every version and kept as a property. It is never the
	// message's identity on its own: with no Message-ID it only keeps apart
	// messages of different threads (adr/0359).
	thread := strings.TrimSpace(pathStr(p, "email.uid"))
	if thread != "" {
		props["thread_uid"] = thread
	}
	// The envelope recipients are email-addrs like the header ones, kept as
	// envelope_to_refs: STIX has no property for them (adr/0357).
	envRefs, env, envUnparsed := b.envelopeRecipients(p)
	if len(envRefs) > 0 {
		props["envelope_to_refs"] = stringifyRefs(envRefs)
	}
	// A recipient that is still not an address after cleanup has no
	// email-addr. It is kept as text, sorted and named once, so the message
	// id below is still minted from what the message stores (adr/0357).
	unparsed = sortedUnique(append(unparsed, envUnparsed...))
	if len(unparsed) > 0 {
		props["unparsed_recipients"] = unparsed
	}
	// The recipients are every address the message was sent to: the header
	// to, cc and blind copy, and the envelope. With no Message-ID they are
	// part of its identity, so two copies of one run sent to different
	// mailboxes stay two messages (adr/0359). The message is keyed on the
	// values it stores: the cleaned-up sender and recipients, and the text
	// of a recipient that is not an address (adr/0357).
	recipients := append(append(append(append(append([]string{}, to...), cc...), bcc...), env...), unparsed...)
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

// envelopeRecipients adds an email-addr for each envelope address of an
// email, by emailAddrList. The envelope is smtp_to[] (the SMTP RCPT TO
// values), and the Delivered-To values, delivered_to_list[] from OCSF 1.4.0,
// else the deprecated single delivered_to (03 §4.16). It returns the ids,
// the values they store, and the text of each entry that is not an address.
func (b *resultBuilder) envelopeRecipients(p map[string]any) ([]identity.STIXID, []string, []string) {
	addrs := stringsAt(p, "email.smtp_to")
	if delivered := stringsAt(p, "email.delivered_to_list"); len(delivered) > 0 {
		addrs = append(addrs, delivered...)
	} else if d := pathStr(p, "email.delivered_to"); d != "" {
		addrs = append(addrs, d)
	}
	return b.emailAddrList(addrs)
}

// sortedUnique returns ss sorted, each string once.
func sortedUnique(ss []string) []string {
	out := slices.Clone(ss)
	slices.Sort(out)
	return slices.Compact(out)
}

// stringsAt reads the strings of the list at path of p.
func stringsAt(p map[string]any, path string) []string {
	v, ok := lookupPath(p, path)
	if !ok {
		return nil
	}
	return stringSlice(v)
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
// 6: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; URLs that are
// URIs, adr/0357
// 7: an account typed Windows only on a Windows signal, account type 19
// read as one, adr/0366
// 8: the account type read by the one rule, a caption for every type and
// the id over it, adr/0367
func (*emailURLNormalizer) Version() int { return 8 }

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
// 6: STIX 2.1 output through the one serializer, an artifact for a record
// naming nothing, and a broken record is a normalize error; file hashes and
// names STIX can hold, adr/0357
// 7: an account typed Windows only on a Windows signal, account type 19
// read as one, adr/0366
// 8: the account type read by the one rule, a caption for every type and
// the id over it, adr/0367
func (*emailFileNormalizer) Version() int { return 8 }

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
