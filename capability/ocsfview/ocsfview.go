// Package ocsfview renders an OCSF event for a person: one sentence with the
// entities as typed slots, a curated field list, a category word, and a
// decoded form of an encoded field when a known encoding decodes cleanly
// (design/ui/00-backend-binding.md §6 gap 12; adr/0090). Display knowledge
// for the classes the normalizers handle (03 §4.1–§4.12) plus 2005 and 5001
// lives here, server-side, so the inspector, the Story timeline, the export
// and the baseline fold agree. Pure functions, no I/O: the caller supplies a
// RefLookup that maps an entity slot to the STIX id the normalizer minted, so
// the client renders chips, not text.
package ocsfview

import (
	"encoding/base64"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/sd-strax/reckon/identity"
)

// Span is one piece of the sentence: text, or an entity slot with the STIX
// type the normalizer would mint for it and its id when the lookup knows it.
type Span struct {
	T    string `json:"t"` // "text" | "entity"
	Text string `json:"text"`
	Ref  string `json:"ref,omitempty"`
	Type string `json:"type,omitempty"`
}

// Field is one curated fact: a label, the value verbatim, the entity id when
// the value names one, mono for data-like values, tone ok|bad|warn for a
// result word.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Ref   string `json:"ref,omitempty"`
	Mono  bool   `json:"mono,omitempty"`
	Tone  string `json:"tone,omitempty"`
}

// Decoded is the readable form of an encoded field. The original is always
// shown beside it; this is never a replacement.
type Decoded struct {
	Field    string `json:"field"`
	Encoding string `json:"encoding"`
	Text     string `json:"text"`
}

// EventView is the rendering of one event.
type EventView struct {
	// Sentence is empty for a class with no renderer (the fields still show).
	Sentence []Span  `json:"sentence,omitempty"`
	Fields   []Field `json:"fields"`
	// Category: auth | process | network | detection | asset | case | other.
	Category string   `json:"category"`
	Decoded  *Decoded `json:"decoded,omitempty"`
}

// RefLookup answers the STIX id of the entity the normalizer minted for a
// typed value in this event — "" when it minted none. The value is the
// type's salient property as the normalizer records it: hostname for x-host,
// the shaped login for user-account, the address or name for the value
// types, the pid for a process, the name for a file, the key path for a
// registry key, the name for a scheduled task.
type RefLookup func(stixType, value string) string

// Text renders the sentence as plain text (the export, a chip label).
func (v EventView) Text() string {
	var b strings.Builder
	for _, s := range v.Sentence {
		b.WriteString(s.Text)
	}
	return b.String()
}

// Category classifies a class_uid by its OCSF category: 1xxx system
// activity reads as process work, 2004 detection, 2005 case, 3xxx identity
// and access, 4xxx network, 5xxx discovery.
func Category(classUID int) string {
	switch {
	case classUID == 2004:
		return "detection"
	case classUID == 2005:
		return "case"
	case classUID >= 1000 && classUID < 2000:
		return "process"
	case classUID >= 3000 && classUID < 4000:
		return "auth"
	case classUID >= 4000 && classUID < 5000:
		return "network"
	case classUID >= 5000 && classUID < 6000:
		return "asset"
	default:
		return "other"
	}
}

// Build renders one event. A nil refs leaves every slot without an id.
func Build(classUID int, payload map[string]any, refs RefLookup) EventView {
	if refs == nil {
		refs = func(string, string) string { return "" }
	}
	b := &builder{p: payload, refs: refs}
	switch classUID {
	case 3002:
		b.authentication()
	case 1007:
		b.process()
	case 4001:
		b.network()
	case 4003:
		b.dns()
	case 1001:
		b.file()
	case 1003:
		b.registry()
	case 1006:
		b.scheduledJob()
	case 1009:
		b.module()
	case 3005:
		b.accountChange()
	case 4009:
		b.email()
	case 4011:
		b.emailURL()
	case 2004:
		b.detection()
	case 2005:
		b.incident()
	case 5001:
		b.deviceInventory()
	default:
		b.scalars()
	}
	v := EventView{Sentence: b.spans, Fields: b.fields, Category: Category(classUID), Decoded: b.decoded}
	if v.Fields == nil {
		v.Fields = []Field{}
	}
	return v
}

type builder struct {
	p       map[string]any
	refs    RefLookup
	spans   []Span
	fields  []Field
	decoded *Decoded
}

// --- the sentence ----------------------------------------------------------

func (b *builder) text(t string) *builder {
	if t == "" {
		return b
	}
	if n := len(b.spans); n > 0 && b.spans[n-1].T == "text" {
		b.spans[n-1].Text += t
		return b
	}
	b.spans = append(b.spans, Span{T: "text", Text: t})
	return b
}

func (b *builder) entity(typ, text string) *builder {
	return b.entityKeyed(typ, text, text)
}

// entityKeyed is an entity slot whose lookup value differs from its text: a
// process reads as its name but the normalizer keys it by pid.
func (b *builder) entityKeyed(typ, text, key string) *builder {
	if text == "" {
		return b
	}
	ref := ""
	if key != "" {
		ref = b.refs(typ, key)
	}
	b.spans = append(b.spans, Span{T: "entity", Text: text, Type: typ, Ref: ref})
	return b
}

// --- the fields ------------------------------------------------------------

func (b *builder) field(label, value string) *builder {
	if value != "" {
		b.fields = append(b.fields, Field{Label: label, Value: value})
	}
	return b
}

func (b *builder) mono(label, value string) *builder {
	if value != "" {
		b.fields = append(b.fields, Field{Label: label, Value: value, Mono: true})
	}
	return b
}

func (b *builder) entityField(label, typ, value string) *builder {
	if value != "" {
		b.fields = append(b.fields, Field{Label: label, Value: value, Ref: b.refs(typ, value), Mono: true})
	}
	return b
}

func (b *builder) toned(label, value, tone string) *builder {
	if value != "" {
		b.fields = append(b.fields, Field{Label: label, Value: value, Tone: tone})
	}
	return b
}

// --- payload access --------------------------------------------------------

func (b *builder) str(path string) string {
	v, ok := lookup(b.p, path)
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		return ""
	}
}

func (b *builder) first(paths ...string) string {
	for _, p := range paths {
		if v := b.str(p); v != "" {
			return v
		}
	}
	return ""
}

func (b *builder) list(path string) []string {
	v, ok := lookup(b.p, path)
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func lookup(m map[string]any, path string) (any, bool) {
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// login shapes an OCSF user object the way the normalizer does (03 §7.4):
// domain\user for a domain account, the name otherwise.
func (b *builder) login(prefix string) string {
	name := b.str(prefix + ".name")
	if name == "" {
		return ""
	}
	if d := b.str(prefix + ".domain"); d != "" {
		return d + "\\" + name
	}
	return name
}

// endpoint names an OCSF endpoint for the sentence: the hostname when it
// has one, else the address.
func (b *builder) endpoint(prefix string) (typ, text string) {
	if h := b.str(prefix + ".hostname"); h != "" {
		return identity.TypeHost, h
	}
	if ip := b.str(prefix + ".ip"); ip != "" {
		return ipType(ip), ip
	}
	if d := b.str(prefix + ".domain"); d != "" {
		return identity.TypeDomainName, d
	}
	return "", ""
}

func ipType(v string) string {
	if strings.Contains(v, ":") {
		return identity.TypeIPv6Addr
	}
	return identity.TypeIPv4Addr
}

// activity reads the activity word: activity_name verbatim, else the class's
// activity_id vocabulary, else "".
func (b *builder) activity(names map[int]string) string {
	if a := b.str("activity_name"); a != "" {
		return a
	}
	if id, err := strconv.Atoi(b.str("activity_id")); err == nil {
		if n, ok := names[id]; ok {
			return n
		}
	}
	return ""
}

func resultTone(status string) string {
	switch strings.ToLower(status) {
	case "success", "succeeded", "allowed", "ok":
		return "ok"
	case "failure", "failed", "denied", "blocked", "error":
		return "bad"
	default:
		return ""
	}
}

// --- per-class renderers ---------------------------------------------------

func (b *builder) authentication() {
	user := b.login("actor.user")
	if user == "" {
		user = b.login("user")
	}
	dstT, dst := b.endpoint("dst_endpoint")
	if dst == "" {
		dstT, dst = b.endpoint("device")
	}
	srcT, src := b.endpoint("src_endpoint")
	logon := b.str("logon_type")
	status := b.first("status", "status_detail")
	b.entity(identity.TypeUserAccount, user)
	if logon != "" {
		b.text(" " + logon)
	}
	if strings.HasPrefix(strings.ToLower(b.str("activity_name")), "logoff") {
		b.text(" logoff from ")
	} else {
		b.text(" logon to ")
	}
	b.entity(dstT, dst)
	if src != "" {
		b.text(" from ").entity(srcT, src)
	}
	if t := resultTone(status); t == "bad" {
		b.text(" — " + strings.ToLower(status))
	}
	b.entityField("User", identity.TypeUserAccount, user)
	lt := logon
	if id := b.str("logon_type_id"); id != "" && lt != "" {
		lt += " (type " + id + ")"
	}
	b.field("Logon type", lt)
	b.entityField("Destination", dstT, dst)
	b.entityField("Source", srcT, src)
	b.toned("Result", status, resultTone(status))
	b.field("Activity", b.str("activity_name"))
	b.field("Auth protocol", b.str("auth_protocol"))
	b.mono("Session", b.str("session.uid"))
}

var encodedCommand = regexp.MustCompile(`(?i)(?:^|\s)-(?:e|ec|enc|encodedcommand)\s+([A-Za-z0-9+/=]{8,})`)

// decodeCommand decodes a PowerShell -EncodedCommand argument (base64 of
// UTF-16LE) or a plain base64 UTF-8 payload when it decodes to printable text.
func decodeCommand(cmd string) *Decoded {
	m := encodedCommand.FindStringSubmatch(cmd)
	if m == nil {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil || len(raw) == 0 {
		return nil
	}
	if len(raw)%2 == 0 {
		u := make([]uint16, len(raw)/2)
		for i := range u {
			u[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
		}
		if text := string(utf16.Decode(u)); printable(text) {
			return &Decoded{Field: "process.cmd_line", Encoding: "base64-utf16le", Text: text}
		}
	}
	if text := string(raw); utf8.ValidString(text) && printable(text) {
		return &Decoded{Field: "process.cmd_line", Encoding: "base64-utf8", Text: text}
	}
	return nil
}

func printable(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == utf8.RuneError || (!unicode.IsPrint(r) && !unicode.IsSpace(r)) {
			return false
		}
	}
	return true
}

func (b *builder) process() {
	user := b.login("actor.user")
	hostT, host := b.endpoint("device")
	name := b.first("process.name", "process.file.name")
	pid := b.str("process.pid")
	parent := b.str("process.parent_process.name")
	cmd := b.str("process.cmd_line")
	activity := b.activity(processActivity)
	verb := "ran"
	if a := strings.ToLower(activity); a != "" && a != "launch" && a != "launched" {
		verb = strings.ToLower(activity)
	}
	if user != "" {
		b.entity(identity.TypeUserAccount, user).text(" " + verb + " ")
	}
	b.entityKeyed(identity.TypeProcess, name, pid)
	if pid != "" {
		b.text(" (pid " + pid + ")")
	}
	if host != "" {
		b.text(" on ").entity(hostT, host)
	}
	if parent != "" {
		b.text(", parent ").entityKeyed(identity.TypeProcess, parent, b.str("process.parent_process.pid"))
	}
	b.decoded = decodeCommand(cmd)
	if name != "" {
		b.fields = append(b.fields, Field{Label: "Process", Value: name, Ref: b.refs(identity.TypeProcess, pid), Mono: true})
	}
	b.mono("PID", pid)
	b.mono("Command line", cmd)
	b.field("Parent", parentLabel(parent, b.str("process.parent_process.pid")))
	b.entityField("User", identity.TypeUserAccount, user)
	b.entityField("Host", hostT, host)
	b.mono("Image path", b.str("process.file.path"))
	b.mono("SHA-256", b.str("process.file.hashes.SHA-256"))
	b.field("Activity", activity)
}

func parentLabel(name, pid string) string {
	if name == "" {
		return ""
	}
	if pid != "" {
		return name + " (pid " + pid + ")"
	}
	return name
}

var processActivity = map[int]string{1: "Launch", 2: "Terminate", 3: "Open", 4: "Inject", 5: "Set User ID"}

func (b *builder) network() {
	hostT, host := b.endpoint("device")
	src := b.str("src_endpoint.ip")
	dst := b.first("dst_endpoint.domain", "dst_endpoint.hostname", "dst_endpoint.ip")
	dstT := identity.TypeDomainName
	if dst == b.str("dst_endpoint.ip") {
		dstT = ipType(dst)
	} else if dst == b.str("dst_endpoint.hostname") {
		dstT = identity.TypeHost
	}
	proto := b.str("connection_info.protocol_name")
	activity := b.activity(networkActivity)
	if host != "" {
		b.entity(hostT, host).text(" ")
	}
	if a := strings.ToLower(activity); a != "" {
		b.text(a + " ")
	} else {
		b.text("connected ")
	}
	if proto != "" {
		b.text(proto + " ")
	}
	b.text("to ").entity(dstT, dst)
	if p := b.str("dst_endpoint.port"); p != "" {
		b.text(":" + p)
	}
	if src != "" {
		b.text(" from ").entity(ipType(src), src)
		if p := b.str("src_endpoint.port"); p != "" {
			b.text(":" + p)
		}
	}
	b.entityField("Source", ipType(src), src)
	b.mono("Source port", b.str("src_endpoint.port"))
	b.entityField("Destination", dstT, dst)
	if ip := b.str("dst_endpoint.ip"); ip != "" && ip != dst {
		b.entityField("Destination address", ipType(ip), ip)
	}
	b.mono("Destination port", b.str("dst_endpoint.port"))
	b.field("Protocol", proto)
	b.field("Direction", b.str("connection_info.direction"))
	b.field("Activity", activity)
	b.entityField("Host", hostT, host)
	b.mono("Bytes out", b.str("traffic.bytes_out"))
	b.mono("Bytes in", b.str("traffic.bytes_in"))
	b.toned("Disposition", b.str("disposition"), resultTone(b.str("disposition")))
}

var networkActivity = map[int]string{1: "Open", 2: "Close", 3: "Reset", 4: "Fail", 5: "Refuse", 6: "Traffic"}

func (b *builder) dns() {
	hostT, host := b.endpoint("device")
	if host == "" {
		hostT, host = b.endpoint("src_endpoint")
	}
	query := b.str("query.hostname")
	var answers []string
	if v, ok := lookup(b.p, "answers"); ok {
		if arr, ok := v.([]any); ok {
			for _, a := range arr {
				if m, ok := a.(map[string]any); ok {
					if r, ok := m["rdata"].(string); ok && r != "" {
						answers = append(answers, r)
					}
				}
			}
		}
	}
	if host != "" {
		b.entity(hostT, host).text(" queried ")
	} else {
		b.text("query for ")
	}
	b.entity(identity.TypeDomainName, query)
	if len(answers) > 0 {
		b.text(" → ")
		for i, a := range answers {
			if i > 0 {
				b.text(", ")
			}
			if ip := strings.TrimSpace(a); strings.Count(ip, ".") == 3 || strings.Contains(ip, ":") {
				b.entity(ipType(ip), ip)
			} else {
				b.entity(identity.TypeDomainName, a)
			}
		}
	}
	if rc := b.str("rcode"); rc != "" && strings.ToUpper(rc) != "NOERROR" {
		b.text(" — " + rc)
	}
	b.entityField("Query", identity.TypeDomainName, query)
	b.mono("Query type", b.str("query.type"))
	b.mono("Answers", strings.Join(answers, ", "))
	b.toned("Response code", b.str("rcode"), map[bool]string{true: "ok", false: "bad"}[strings.ToUpper(b.str("rcode")) == "NOERROR" || b.str("rcode") == ""])
	b.entityField("Host", hostT, host)
	b.entityField("Resolver", ipType(b.str("dst_endpoint.ip")), b.str("dst_endpoint.ip"))
}

// pastTense turns an OCSF activity word into the sentence's verb; empty
// reads as "touched".
func pastTense(verb string) string {
	switch {
	case verb == "":
		return "touched"
	case strings.HasSuffix(verb, "e"):
		return verb + "d"
	case strings.HasSuffix(verb, "d"):
		return verb
	default:
		return verb + "ed"
	}
}

var fileActivity = map[int]string{1: "Create", 2: "Read", 3: "Update", 4: "Delete", 5: "Rename", 6: "Set Attributes", 7: "Set Security", 8: "Get Attributes", 9: "Get Security", 10: "Encrypt", 11: "Decrypt", 12: "Mount", 13: "Unmount", 14: "Open"}

func (b *builder) file() {
	user := b.login("actor.user")
	hostT, host := b.endpoint("device")
	path := b.first("file.path", "file.name")
	name := b.first("file.name", "file.path")
	proc := b.str("actor.process.name")
	activity := b.activity(fileActivity)
	verb := pastTense(strings.ToLower(activity))
	switch {
	case proc != "":
		b.entityKeyed(identity.TypeProcess, proc, b.str("actor.process.pid"))
	case user != "":
		b.entity(identity.TypeUserAccount, user)
	default:
		b.text("a process")
	}
	b.text(" "+verb+" ").entity(identity.TypeFile, name)
	if host != "" {
		b.text(" on ").entity(hostT, host)
	}
	b.mono("File", path)
	b.field("Activity", activity)
	b.field("Process", parentLabel(proc, b.str("actor.process.pid")))
	b.entityField("User", identity.TypeUserAccount, user)
	b.entityField("Host", hostT, host)
	b.mono("SHA-256", b.str("file.hashes.SHA-256"))
	b.mono("Size", b.str("file.size"))
}

var registryActivity = map[int]string{1: "Create", 2: "Read", 3: "Modify", 4: "Delete", 5: "Rename", 6: "Set Security", 7: "Restore", 8: "Import", 9: "Export"}

func (b *builder) registry() {
	hostT, host := b.endpoint("device")
	hive := b.str("reg_key.hive")
	key := b.str("reg_key.path")
	full := key
	if hive != "" && key != "" {
		full = hive + "\\" + key
	} else if hive != "" {
		full = hive
	}
	proc := b.str("actor.process.name")
	activity := b.activity(registryActivity)
	if proc != "" {
		b.entityKeyed(identity.TypeProcess, proc, b.str("actor.process.pid")).text(" ")
	}
	if a := strings.ToLower(activity); a != "" {
		b.text(a + " ")
	} else {
		b.text("touched ")
	}
	b.text("registry key ").entity(identity.TypeRegistryKey, full)
	if v := b.str("reg_value.name"); v != "" {
		b.text(" value " + v)
	}
	if host != "" {
		b.text(" on ").entity(hostT, host)
	}
	b.mono("Key", full)
	b.mono("Value name", b.str("reg_value.name"))
	b.mono("Value data", b.str("reg_value.data"))
	b.field("Activity", activity)
	b.field("Process", parentLabel(proc, b.str("actor.process.pid")))
	b.entityField("Host", hostT, host)
}

var jobActivity = map[int]string{1: "Create", 2: "Update", 3: "Delete", 4: "Enable", 5: "Disable", 6: "Start"}

func (b *builder) scheduledJob() {
	hostT, host := b.endpoint("device")
	name := b.first("job.name", "job.path")
	user := b.login("job.user")
	activity := b.activity(jobActivity)
	b.text("scheduled task ").entity(identity.TypeScheduledTask, name)
	if a := strings.ToLower(activity); a != "" {
		b.text(" " + a + "d")
	}
	if host != "" {
		b.text(" on ").entity(hostT, host)
	}
	if user != "" {
		b.text(" as ").entity(identity.TypeUserAccount, user)
	}
	b.field("Task", name)
	b.mono("Path", b.str("job.path"))
	b.mono("Command line", b.str("job.command_line"))
	b.entityField("Runs as", identity.TypeUserAccount, user)
	b.field("Activity", activity)
	b.entityField("Host", hostT, host)
}

func (b *builder) module() {
	hostT, host := b.endpoint("device")
	proc := b.str("actor.process.name")
	mod := b.first("module.file.name", "module.file.path")
	if proc != "" {
		b.entityKeyed(identity.TypeProcess, proc, b.str("actor.process.pid"))
	} else {
		b.text("a process")
	}
	b.text(" loaded ").entity(identity.TypeFile, mod)
	if host != "" {
		b.text(" on ").entity(hostT, host)
	}
	b.mono("Module", b.first("module.file.path", "module.file.name"))
	b.field("Process", parentLabel(proc, b.str("actor.process.pid")))
	b.entityField("Host", hostT, host)
	b.mono("SHA-256", b.str("module.file.hashes.SHA-256"))
	b.field("Load type", b.str("module.load_type"))
}

var accountActivity = map[int]string{1: "Create", 2: "Enable", 3: "Password Change", 4: "Password Reset", 5: "Disable", 6: "Delete", 7: "Attach Policy", 8: "Detach Policy", 9: "Lock", 10: "MFA Factor Enable", 11: "MFA Factor Disable", 12: "Unlock"}

func (b *builder) accountChange() {
	actor := b.login("actor.user")
	target := b.login("user")
	group := b.str("group.name")
	activity := b.activity(accountActivity)
	hostT, host := b.endpoint("device")
	if actor != "" {
		b.entity(identity.TypeUserAccount, actor).text(" ")
	}
	if a := strings.ToLower(activity); a != "" {
		b.text(a + " ")
	} else {
		b.text("changed ")
	}
	b.text("account ").entity(identity.TypeUserAccount, target)
	if group != "" {
		b.text(" in group ").entity(identity.TypeGroup, group)
	}
	if host != "" {
		b.text(" on ").entity(hostT, host)
	}
	b.entityField("Actor", identity.TypeUserAccount, actor)
	b.entityField("Account", identity.TypeUserAccount, target)
	b.field("Activity", activity)
	b.entityField("Group", identity.TypeGroup, group)
	b.field("Privileges", strings.Join(b.list("privileges"), ", "))
	b.toned("Result", b.str("status"), resultTone(b.str("status")))
	b.entityField("Host", hostT, host)
}

func (b *builder) email() {
	from := b.str("email.from")
	to := b.list("email.to")
	subject := b.str("email.subject")
	b.text("email from ").entity(identity.TypeEmailAddr, from)
	if len(to) > 0 {
		b.text(" to ")
		for i, t := range to {
			if i > 0 {
				b.text(", ")
			}
			b.entity(identity.TypeEmailAddr, t)
		}
	}
	if subject != "" {
		b.text(" “" + subject + "”")
	}
	if d := b.str("disposition"); d != "" {
		b.text(" — " + strings.ToLower(d))
	}
	b.entityField("From", identity.TypeEmailAddr, from)
	b.field("To", strings.Join(to, ", "))
	b.field("Subject", subject)
	b.mono("Message id", b.str("email.message_uid"))
	b.field("Direction", b.str("direction"))
	b.field("Activity", b.str("activity_name"))
	b.toned("Disposition", b.str("disposition"), resultTone(b.str("disposition")))
	b.mono("Attachments", strings.Join(b.list("email.attachments"), ", "))
}

func (b *builder) emailURL() {
	u := b.str("url.url_string")
	b.text("URL ").entity(identity.TypeURL, u)
	if uid := b.str("email.message_uid"); uid != "" {
		b.text(" in message " + uid)
	}
	b.entityField("URL", identity.TypeURL, u)
	b.mono("Message id", b.str("email.message_uid"))
	b.field("Activity", b.str("activity_name"))
	b.toned("Disposition", b.str("disposition"), resultTone(b.str("disposition")))
}

func (b *builder) detection() {
	vendor := b.first("metadata.product.vendor_name", "metadata.product.name")
	title := b.first("finding.title", "finding_info.title")
	severity := b.str("severity")
	if severity == "" {
		severity = b.first("finding.severity", "finding_info.severity")
	}
	// The evidence the finding recursed names the host and the user.
	ev := &builder{p: map[string]any{}, refs: b.refs}
	if e, ok := lookup(b.p, "evidence"); ok {
		if m, ok := e.(map[string]any); ok {
			ev.p = m
		}
	}
	hostT, host := ev.endpoint("device")
	if host == "" {
		hostT, host = b.endpoint("device")
	}
	user := ev.login("actor.user")
	if vendor != "" {
		b.text(vendor + " detected ")
	} else {
		b.text("detected ")
	}
	if title != "" {
		b.text("“" + title + "”")
	} else {
		b.text("a finding")
	}
	if severity != "" {
		b.text(" (" + strings.ToLower(severity) + ")")
	}
	if host != "" {
		b.text(" on ").entity(hostT, host)
	}
	if user != "" {
		b.text(" as ").entity(identity.TypeUserAccount, user)
	}
	b.field("Finding", title)
	b.mono("Finding id", b.first("finding.uid", "finding_info.uid"))
	b.field("Vendor", vendor)
	b.toned("Severity", severity, map[string]string{"critical": "bad", "high": "bad", "medium": "warn"}[strings.ToLower(severity)])
	b.field("Confidence", b.first("finding.confidence", "confidence"))
	types := b.list("finding.types")
	if len(types) == 0 {
		types = b.list("finding_info.types")
	}
	b.mono("Techniques", strings.Join(types, ", "))
	b.entityField("Host", hostT, host)
	b.entityField("User", identity.TypeUserAccount, user)
	b.field("Evidence class", ev.str("class_name"))
	b.field("Disposition", b.str("disposition"))
}

func (b *builder) incident() {
	uid := b.str("finding_info.uid")
	title := b.str("finding_info.title")
	status := b.str("status")
	priority := b.str("priority")
	b.text("case ")
	if uid != "" {
		b.text(uid + " ")
	}
	if title != "" {
		b.text("“" + title + "”")
	}
	var parens []string
	if status != "" {
		parens = append(parens, status)
	}
	if priority != "" {
		parens = append(parens, "priority "+priority)
	}
	if len(parens) > 0 {
		b.text(" (" + strings.Join(parens, ", ") + ")")
	}
	b.mono("Case", uid)
	b.field("Title", title)
	b.field("Status", status)
	b.field("Priority", priority)
	b.field("Assignment group", b.str("assignment_group"))
	b.field("System", b.str("unmapped.sor"))
	b.mono("Link", b.str("unmapped.link"))
	b.field("Summary", b.str("finding_info.desc"))
}

func (b *builder) deviceInventory() {
	hostT, host := b.endpoint("device")
	osName := b.first("device.os.name", "device.os.type")
	owner := b.str("device.owner.name")
	crit := b.str("device.criticality")
	b.entity(hostT, host)
	if osName != "" {
		b.text(" · " + osName)
	}
	if owner != "" {
		b.text(" · owner " + owner)
	}
	if crit != "" {
		b.text(" · criticality " + crit)
	}
	b.entityField("Host", hostT, host)
	b.field("OS", osName)
	b.field("Owner", owner)
	b.toned("Criticality", crit, map[string]string{"critical": "bad", "high": "bad", "medium": "warn"}[strings.ToLower(crit)])
	b.mono("Last seen", b.str("device.last_seen_time"))
	b.field("Domain", b.str("device.domain"))
	var agents []string
	if v, ok := lookup(b.p, "device.agent_list"); ok {
		if arr, ok := v.([]any); ok {
			for _, a := range arr {
				if m, ok := a.(map[string]any); ok {
					if n, ok := m["name"].(string); ok && n != "" {
						agents = append(agents, n)
					}
				}
			}
		}
	}
	b.field("Agents", strings.Join(agents, ", "))
}

// scalars is the fallback for a class with no renderer: the top-level
// scalar fields in key order, no sentence.
func (b *builder) scalars() {
	keys := make([]string, 0, len(b.p))
	for k, v := range b.p {
		switch v.(type) {
		case string, float64, bool, int, int64:
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "class_uid" || k == "category_uid" || k == "type_uid" {
			continue
		}
		b.field(humanKey(k), b.str(k))
	}
}

func humanKey(k string) string {
	k = strings.ReplaceAll(k, "_", " ")
	if k == "" {
		return k
	}
	return strings.ToUpper(k[:1]) + k[1:]
}
