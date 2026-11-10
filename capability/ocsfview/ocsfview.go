// Package ocsfview renders an OCSF event for a reading surface: one sentence
// with the entities as typed slots, a curated field list, and the category word
// (implementation/workbench-react.md §8.1, adr/0090). The Story timeline, the
// Evidence inspector and the export all read the same view, so an event reads
// the same everywhere and the display knowledge for a class lives in one
// place.
//
// Pure functions, no I/O. The entity refs come from the record, never from a
// read-time identity computation: the caller passes the objects the event was
// normalized into, and each sentence slot binds to the object of its type whose
// properties carry the value the payload shows. An event ingested under a
// source scope keeps the ids its scope minted.
package ocsfview

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sd-strax/reckon/identity"
)

// Event is what the renderer reads: the class and the raw OCSF payload.
type Event struct {
	ClassUID  int
	ClassName string
	Payload   map[string]any
}

// Entity is one interpretation-layer object the event was normalized into, as
// the record holds it: its ref, its STIX type and its properties.
type Entity struct {
	Ref   string
	Type  string
	Props map[string]any
}

// EntityFromObject reads a persisted STIX object into an Entity. The object's
// properties sit under "properties" (capability.SCO); an object without that
// key is read as flat.
func EntityFromObject(ref, typ string, payload []byte) Entity {
	e := Entity{Ref: ref, Type: typ}
	var raw map[string]any
	if json.Unmarshal(payload, &raw) != nil {
		return e
	}
	if props, ok := raw["properties"].(map[string]any); ok {
		e.Props = props
	} else {
		e.Props = raw
	}
	return e
}

// Token is one piece of the sentence: text, or an entity the client renders as
// a chip. An entity the record did not mint renders as text (no ref).
type Token struct {
	T    string `json:"t"` // "entity" | "text"
	Ref  string `json:"ref,omitempty"`
	Text string `json:"text"`
}

// Field is one curated field: a label, the value as shown, the entity ref when
// the value names one, whether it is monospaced, and a tone for an outcome
// (ok | warn | bad).
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Ref   string `json:"ref,omitempty"`
	Mono  bool   `json:"mono,omitempty"`
	Tone  string `json:"tone,omitempty"`
}

// View is the rendering of one event. Sentence is absent for a class without a
// template; Fields is never nil. Decoded is the command line's encoded
// argument read back (adr/0300), present only when a known encoding decodes
// cleanly; the field it came from is always rendered as stored.
type View struct {
	Sentence []Token  `json:"sentence,omitempty"`
	Fields   []Field  `json:"fields"`
	Category string   `json:"category"`
	Decoded  *Decoded `json:"decoded,omitempty"`
	// Slots are the class's slot values the template registered, for the
	// group-by fold (group.go); not on the wire.
	Slots []SlotValue `json:"-"`
}

// Category words, from category_uid (the thousands digit of class_uid) with the
// case class named apart: the Story filters and the inspector read them.
const (
	CategoryProcess     = "process"
	CategoryDetection   = "detection"
	CategoryAuth        = "auth"
	CategoryNetwork     = "network"
	CategoryAsset       = "asset"
	CategoryCase        = "case"
	CategoryApplication = "application"
	CategoryOther       = "other"
)

// Category names an event's category.
func Category(classUID int, payload map[string]any) string {
	if classUID == classCase {
		return CategoryCase
	}
	cat := toInt(payload["category_uid"])
	if cat == 0 {
		cat = classUID / 1000
	}
	switch cat {
	case 1:
		return CategoryProcess
	case 2:
		return CategoryDetection
	case 3:
		return CategoryAuth
	case 4:
		return CategoryNetwork
	case 5:
		return CategoryAsset
	case 6:
		return CategoryApplication
	default:
		return CategoryOther
	}
}

// The classes with a template: every class a normalizer handles (03 §4.1–§4.12,
// §4.15) and device inventory.
const (
	classFileActivity    = 1001
	classRegistryKey     = 1003
	classScheduledJob    = 1006
	classProcess         = 1007
	classModule          = 1009
	classDetection       = 2004
	classCase            = 2005
	classAuthentication  = 3002
	classAccountChange   = 3005
	classNetwork         = 4001
	classDNS             = 4003
	classEmail           = 4009
	classEmailURL        = 4011
	classDeviceInventory = 5001
	classAPIActivity     = 6003
)

// Classes lists the class_uids with a sentence template, sorted.
func Classes() []int {
	return []int{
		classFileActivity, classRegistryKey, classScheduledJob, classProcess, classModule,
		classDetection, classCase, classAuthentication, classAccountChange,
		classNetwork, classDNS, classEmail, classEmailURL, classDeviceInventory,
		classAPIActivity,
	}
}

// Render renders one event with the entities the record derived from it.
func Render(e Event, entities []Entity) View {
	r := &renderer{p: e.Payload, ents: entities}
	if r.p == nil {
		r.p = map[string]any{}
	}
	switch e.ClassUID {
	case classAuthentication:
		r.authentication()
	case classProcess:
		r.process()
	case classNetwork:
		r.network()
	case classDNS:
		r.dns()
	case classFileActivity:
		r.fileActivity()
	case classRegistryKey:
		r.registryKey()
	case classScheduledJob:
		r.scheduledJob()
	case classModule:
		r.module()
	case classAccountChange:
		r.accountChange()
	case classEmail:
		r.email()
	case classEmailURL:
		r.emailURL()
	case classDetection:
		r.detection()
	case classCase:
		r.caseFinding()
	case classDeviceInventory:
		r.deviceInventory()
	case classAPIActivity:
		r.apiActivity()
	default:
		r.scalars()
	}
	if r.fields == nil {
		r.fields = []Field{}
	}
	return View{Sentence: r.sentence, Fields: r.fields, Category: Category(e.ClassUID, e.Payload), Decoded: r.decoded, Slots: r.slots}
}

// renderer accumulates one view. The sentence helpers add nothing for an
// empty value, so a template reads as a sequence of optional clauses.
type renderer struct {
	p        map[string]any
	ents     []Entity
	sentence []Token
	fields   []Field
	slots    []SlotValue
	decoded  *Decoded
}

// keySlot registers a slot's value for the fold: the ref when the record
// minted the entity, the text always. The text folds for case: names, hosts,
// accounts, paths and keys compare case-insensitively.
func (r *renderer) keySlot(name string, s slot) {
	r.slots = append(r.slots, SlotValue{Name: name, Text: s.text, Ref: s.ref, Identity: strings.ToLower(s.text)})
}

// keyText registers a slot by its text alone, folded for case: a scalar, or a
// per-occurrence object (the process, the flow) whose ref would differ on
// every event.
func (r *renderer) keyText(name, text string) {
	r.slots = append(r.slots, SlotValue{Name: name, Text: text, Identity: strings.ToLower(text)})
}

// keyExact registers a slot whose text compares exactly: an opaque value
// (a command line, registry data, a subject, a URL) where case carries
// meaning, so "-I" and "-i" are two observations.
func (r *renderer) keyExact(name string, s slot) {
	r.slots = append(r.slots, SlotValue{Name: name, Text: s.text, Ref: s.ref, Identity: s.text})
}

// keyList registers a multi-valued slot: the texts sorted, joined for reading,
// and as identity encoded with their boundaries intact, so one value "a,b"
// never equals two values "a" and "b". fold lowercases values that are
// case-insensitive (addresses); order never makes two observations differ.
func (r *renderer) keyList(name string, slots []slot, fold bool) {
	vals := make([]string, 0, len(slots))
	for _, s := range slots {
		if s.text == "" {
			continue
		}
		if fold {
			vals = append(vals, strings.ToLower(s.text))
		} else {
			vals = append(vals, s.text)
		}
	}
	sort.Strings(vals)
	var id string
	if len(vals) > 0 { // no values is no value, which never folds
		enc, _ := json.Marshal(vals) // strings always marshal
		id = string(enc)
	}
	r.slots = append(r.slots, SlotValue{Name: name, Text: strings.Join(vals, ", "), Identity: id})
}

// slot is a sentence slot: what the payload shows, and the record's ref when
// the record minted the entity.
type slot struct {
	text string
	ref  string
}

func (s slot) empty() bool { return s.text == "" }

func (r *renderer) text(s string) {
	if s == "" {
		return
	}
	if n := len(r.sentence); n > 0 && r.sentence[n-1].T == "text" {
		r.sentence[n-1].Text += s
		return
	}
	r.sentence = append(r.sentence, Token{T: "text", Text: s})
}

func (r *renderer) entity(s slot) {
	if s.empty() {
		return
	}
	if s.ref == "" {
		r.text(s.text)
		return
	}
	r.sentence = append(r.sentence, Token{T: "entity", Ref: s.ref, Text: s.text})
}

// clause adds lead + the slot when the slot has a value.
func (r *renderer) clause(lead string, s slot) {
	if s.empty() {
		return
	}
	r.text(lead)
	r.entity(s)
}

func (r *renderer) field(label, value string) *Field {
	if value == "" {
		return &Field{}
	}
	r.fields = append(r.fields, Field{Label: label, Value: value})
	return &r.fields[len(r.fields)-1]
}

func (r *renderer) mono(label, value string) *Field {
	f := r.field(label, value)
	f.Mono = true
	return f
}

func (r *renderer) entityField(label string, s slot) *Field {
	f := r.mono(label, s.text)
	f.Ref = s.ref
	return f
}

// --- payload access ------------------------------------------------------------------

func (r *renderer) str(path string) string { return pathStr(r.p, path) }
func (r *renderer) int(path string) int    { return toInt(pathAny(r.p, path)) }

// --- entity binding -------------------------------------------------------------------

// bind returns the ref of the entity of typ whose properties satisfy match, or
// "" when the record holds none or more than one. A slot never binds by type
// alone (two users on an account change, two addresses on a connection), and an
// ambiguous match renders as text rather than open the wrong object.
func (r *renderer) bind(typ string, match func(props map[string]any) bool) string {
	ref := ""
	for _, e := range r.ents {
		if e.Type != typ || !match(e.Props) {
			continue
		}
		if ref != "" {
			return ""
		}
		ref = e.Ref
	}
	return ref
}

func propEquals(props map[string]any, key, want string) bool {
	return want != "" && strings.EqualFold(toString(props[key]), want)
}

// host binds an OCSF endpoint/device object at prefix to its x-host.
func (r *renderer) host(prefix string) slot {
	name := r.str(prefix + ".hostname")
	if name == "" {
		return slot{}
	}
	return slot{text: name, ref: r.bind(identity.TypeHost, func(p map[string]any) bool { return propEquals(p, "hostname", name) })}
}

// user binds an OCSF user object at prefix to its user-account. The record's
// login is shaped (03 §7.4: "domain\user" when the payload names a domain, else
// the address or the bare name). With a domain only the qualified login
// matches, so two same-named accounts of different domains bind apart; without
// one the bare name matches, or a qualified login ending in it when the record
// holds exactly one such account.
func (r *renderer) user(prefix string) slot {
	name := r.str(prefix + ".name")
	if name == "" {
		return slot{}
	}
	domain := r.str(prefix + ".domain")
	ref := r.bind(identity.TypeUserAccount, func(p map[string]any) bool {
		login := toString(p["account_login"])
		if domain != "" {
			return strings.EqualFold(login, domain+`\`+name)
		}
		return strings.EqualFold(login, name) ||
			strings.HasSuffix(strings.ToLower(login), `\`+strings.ToLower(name))
	})
	return slot{text: name, ref: ref}
}

func (r *renderer) qualifiedUser(prefix string) string {
	name := r.str(prefix + ".name")
	if d := r.str(prefix + ".domain"); d != "" && name != "" {
		return d + `\` + name
	}
	return name
}

func (r *renderer) ip(path string) slot {
	v := r.str(path)
	if v == "" {
		return slot{}
	}
	match := func(p map[string]any) bool { return propEquals(p, "value", v) }
	ref := r.bind(identity.TypeIPv4Addr, match)
	if ref == "" {
		ref = r.bind(identity.TypeIPv6Addr, match)
	}
	return slot{text: v, ref: ref}
}

func (r *renderer) domain(v string) slot {
	if v == "" {
		return slot{}
	}
	return slot{text: v, ref: r.bind(identity.TypeDomainName, func(p map[string]any) bool { return propEquals(p, "value", v) })}
}

func (r *renderer) url(v string) slot {
	if v == "" {
		return slot{}
	}
	return slot{text: v, ref: r.bind(identity.TypeURL, func(p map[string]any) bool { return propEquals(p, "value", v) })}
}

func (r *renderer) emailAddr(v string) slot {
	if v == "" {
		return slot{}
	}
	return slot{text: v, ref: r.bind(identity.TypeEmailAddr, func(p map[string]any) bool { return propEquals(p, "value", v) })}
}

// file binds an OCSF file object at prefix to its file by name, hash or path.
func (r *renderer) file(prefix string) slot {
	name := r.str(prefix + ".name")
	path := r.str(prefix + ".path")
	text := name
	if text == "" {
		text = baseName(path)
	}
	if text == "" {
		return slot{}
	}
	hashes := hashesOf(pathAny(r.p, prefix+".hashes"))
	ref := r.bind(identity.TypeFile, func(p map[string]any) bool {
		if name != "" && propEquals(p, "name", name) {
			return true
		}
		if path != "" && propEquals(p, "path", path) {
			return true
		}
		for alg, want := range hashes {
			if got, ok := p["hashes"].(map[string]any); ok && strings.EqualFold(toString(got[alg]), want) {
				return true
			}
		}
		return false
	})
	return slot{text: text, ref: ref}
}

// proc binds an OCSF process object at prefix to its process by pid. A stored
// name must agree with the payload's; a process the record holds by pid alone
// (the network normalizer stores no name) still binds.
func (r *renderer) proc(prefix string) slot {
	name := r.str(prefix + ".name")
	pid := r.int(prefix + ".pid")
	if name == "" && pid == 0 {
		return slot{}
	}
	text := name
	if text == "" {
		text = "pid " + strconv.Itoa(pid)
	}
	ref := r.bind(identity.TypeProcess, func(p map[string]any) bool {
		if pid == 0 || toInt(p["pid"]) != pid {
			return false
		}
		stored := toString(p["name"])
		return name == "" || stored == "" || strings.EqualFold(stored, name)
	})
	return slot{text: text, ref: ref}
}

// --- templates ------------------------------------------------------------------------

// activity returns the OCSF activity as a verb phrase from the class's table,
// or fallback when the payload names none the table knows.
func (r *renderer) activity(table map[string]string, fallback string) string {
	name := r.str("activity_name")
	if v, ok := table[strings.ToLower(name)]; ok {
		return v
	}
	if name != "" && name != "Other" && name != "Unknown" {
		return strings.ToLower(name)
	}
	return fallback
}

func (r *renderer) activityField() {
	r.field("Activity", r.str("activity_name"))
}

func (r *renderer) hashField(prefix string) {
	hashes := hashesOf(pathAny(r.p, prefix+".hashes"))
	for _, alg := range []string{"SHA-256", "SHA-1", "MD5"} {
		if v := hashes[alg]; v != "" {
			r.mono(alg, v)
			return
		}
	}
}

var logonWords = map[string]string{
	"interactive":       "interactive",
	"network":           "network",
	"batch":             "batch",
	"service":           "service",
	"unlock":            "unlock",
	"networkcleartext":  "network cleartext",
	"newcredentials":    "new-credentials",
	"remoteinteractive": "RDP",
	"cachedinteractive": "cached interactive",
	"system":            "system",
}

func (r *renderer) authentication() {
	user := r.user("actor.user")
	host := r.host("dst_endpoint")
	src := r.ip("src_endpoint.ip")
	logon := r.str("logon_type")
	word := logonWords[strings.ToLower(logon)]
	if word == "" {
		word = strings.ToLower(logon)
	}
	status := r.str("status")
	failed := statusTone(status) == "bad"

	if user.empty() {
		r.text("Logon")
	} else {
		r.entity(user)
		if word != "" {
			r.text(" " + word)
		}
		r.text(" logon")
	}
	if user.empty() && word != "" {
		r.text(" (" + word + ")")
	}
	r.clause(" to ", host)
	r.clause(" from ", src)
	if failed {
		r.text(" failed")
	}

	r.entityField("User", slot{text: r.qualifiedUser("actor.user"), ref: user.ref})
	r.field("Logon type", logon)
	r.entityField("Source", src)
	r.entityField("Destination", host)
	r.field("Result", status).Tone = statusTone(status)
	r.field("Detail", r.str("status_detail"))
	r.field("Protocol", r.str("auth_protocol"))

	r.keySlot("user", user)
	r.keyText("logon_type", logon)
	r.keySlot("src", src)
	r.keySlot("dst", host)
	r.keyText("status", status)
}

var processVerbs = map[string]string{
	"launch": "launched", "terminate": "terminated", "open": "opened", "inject": "injected into", "set user id": "set the user id of",
}

func (r *renderer) process() {
	user := r.user("actor.user")
	host := r.host("device")
	proc := r.proc("process")
	parent := r.proc("process.parent_process")
	verb := r.activity(processVerbs, "ran")

	switch {
	case user.empty() && proc.empty():
		r.text("Process activity")
	case user.empty():
		r.entity(proc)
		r.text(" " + pastToPassive(verb))
	default:
		r.entity(user)
		r.text(" " + verb + " ")
		r.entity(proc)
	}
	r.clause(" on ", host)
	r.clause(", child of ", parent)

	r.entityField("Process", proc)
	if pid := r.int("process.pid"); pid != 0 {
		r.mono("PID", strconv.Itoa(pid))
	}
	r.mono("Command line", r.str("process.cmd_line"))
	r.decode("process.cmd_line")
	if !parent.empty() {
		text := parent.text
		if ppid := r.int("process.parent_process.pid"); ppid != 0 && r.str("process.parent_process.name") != "" {
			text += " (pid " + strconv.Itoa(ppid) + ")"
		}
		r.entityField("Parent", slot{text: text, ref: parent.ref})
	}
	r.entityField("User", slot{text: r.qualifiedUser("actor.user"), ref: user.ref})
	r.entityField("Host", host)
	image := r.file("process.file")
	r.entityField("Image", slot{text: r.str("process.file.path"), ref: image.ref})
	r.hashField("process.file")

	// The image keys by the file object (hash-first) when the record minted
	// one, else by the image name the process shows.
	r.keySlot("user", user)
	r.keySlot("host", host)
	r.keyText("process", proc.text)
	r.keyText("parent", parent.text)
	r.keySlot("image", slot{text: firstNonEmpty(image.text, proc.text), ref: image.ref})
	r.keyExact("cmd_line", slot{text: r.str("process.cmd_line")})
	r.keyText("activity", r.str("activity_name"))
}

// networkActivities are the OCSF network_activity activity ids, for a payload
// that carries the id without the name.
var networkActivities = map[int]string{
	1: "open", 2: "close", 3: "reset", 4: "fail", 5: "refuse", 6: "traffic", 7: "listen",
}

// networkVerbs reads the activity as what happened to the connection; an
// unsuccessful attempt stays unsuccessful. The subject form is used after a
// host or address, the bare form opens a subjectless sentence.
var networkVerbs = map[string][2]string{
	"open":    {" connected", "Connection"},
	"traffic": {" sent traffic", "Traffic"},
	"close":   {" closed a connection", "Connection closed"},
	"reset":   {" reset a connection", "Connection reset"},
	"fail":    {" failed to connect", "Failed connection"},
	"refuse":  {" was refused a connection", "Refused connection"},
	"listen":  {" listened for connections", "Listening"},
}

func (r *renderer) network() {
	host := r.host("device")
	src := r.ip("src_endpoint.ip")
	dst := r.ip("dst_endpoint.ip")
	domain := r.domain(r.str("dst_endpoint.domain"))
	proto := r.str("connection_info.protocol_name")
	proc := r.proc("process")
	dport := r.int("dst_endpoint.port")
	status := r.str("status")

	key := strings.ToLower(r.str("activity_name"))
	if key == "" {
		key = networkActivities[r.int("activity_id")]
	}
	// An activity the table does not know (absent, Unknown, Other) reads as a
	// connection, or as a failed one when the status says so.
	verb, ok := networkVerbs[key]
	if !ok {
		verb = networkVerbs["open"]
	}
	if (!ok || key == "open") && (statusTone(status) == "bad" || r.int("status_id") == 2) {
		verb = networkVerbs["fail"]
	}

	switch {
	case !host.empty():
		r.entity(host)
		r.text(verb[0])
	case !src.empty():
		r.entity(src)
		r.text(verb[0])
	default:
		r.text(verb[1])
	}
	r.clause(" to ", dst)
	if dst.empty() {
		r.clause(" to ", domain)
	}
	if dport != 0 && (!dst.empty() || !domain.empty()) {
		r.text(" port " + strconv.Itoa(dport))
	}
	if !dst.empty() && !domain.empty() {
		r.text(" (")
		r.entity(domain)
		r.text(")")
	}
	if proto != "" {
		r.text(" over " + strings.ToLower(proto))
	}
	if !host.empty() {
		r.clause(" from ", src)
	}

	r.entityField("Source", slot{text: endpoint(src.text, r.int("src_endpoint.port")), ref: src.ref})
	r.entityField("Destination", slot{text: endpoint(dst.text, dport), ref: dst.ref})
	r.entityField("Domain", domain)
	r.field("Protocol", proto)
	r.field("Direction", r.str("connection_info.direction"))
	r.field("Result", status).Tone = statusTone(status)
	r.entityField("Host", host)
	r.entityField("Process", proc)
	if b := r.int("traffic.bytes"); b != 0 {
		r.mono("Bytes", strconv.Itoa(b))
	}
	r.activityField()

	// A domain-only destination keys by the domain, so connections to
	// different names stay apart when no address is given.
	r.keySlot("host", host)
	r.keySlot("src", src)
	if dst.empty() {
		r.keySlot("dst", domain)
	} else {
		r.keySlot("dst", dst)
	}
	if dport != 0 {
		r.keyText("dst_port", strconv.Itoa(dport))
	} else {
		r.keyText("dst_port", "")
	}
	r.keySlot("domain", domain)
	r.keyText("protocol", strings.ToLower(proto))
	r.keyText("process", proc.text)
	r.keyText("activity", key)
}

func (r *renderer) dns() {
	host := r.host("device")
	src := r.ip("src_endpoint.ip")
	query := r.domain(r.str("query.hostname"))
	answers := r.dnsAnswers()

	switch {
	case !host.empty():
		r.entity(host)
		r.text(" queried ")
	case !src.empty():
		r.entity(src)
		r.text(" queried ")
	default:
		r.text("DNS query for ")
	}
	r.entity(query)
	if len(answers) > 0 {
		r.text(" → ")
		for i, a := range answers {
			if i == 3 {
				r.text(fmt.Sprintf(" +%d more", len(answers)-3))
				break
			}
			if i > 0 {
				r.text(", ")
			}
			r.entity(a)
		}
	}

	r.entityField("Query", query)
	r.field("Type", r.str("query.type"))
	if len(answers) > 0 {
		texts := make([]string, 0, len(answers))
		for _, a := range answers {
			texts = append(texts, a.text)
		}
		r.mono("Answers", strings.Join(texts, ", "))
	}
	r.field("Response", r.str("rcode"))
	r.entityField("Requestor", src)
	r.entityField("Host", host)

	r.keySlot("host", host)
	r.keySlot("src", src)
	r.keySlot("query", query)
	r.keyText("type", r.str("query.type"))
	r.keyText("rcode", r.str("rcode"))
	r.keyList("answers", answers, false) // TXT payloads compare exactly
}

// dnsAnswers reads answers[].rdata as address or domain slots.
func (r *renderer) dnsAnswers() []slot {
	var out []slot
	for _, a := range pathList(r.p, "answers") {
		m, _ := a.(map[string]any)
		rdata := toString(m["rdata"])
		if rdata == "" {
			continue
		}
		s := slot{text: rdata}
		s.ref = r.bind(identity.TypeIPv4Addr, func(p map[string]any) bool { return propEquals(p, "value", rdata) })
		if s.ref == "" {
			s.ref = r.bind(identity.TypeIPv6Addr, func(p map[string]any) bool { return propEquals(p, "value", rdata) })
		}
		if s.ref == "" {
			s.ref = r.domain(rdata).ref
		}
		out = append(out, s)
	}
	return out
}

var fileVerbs = map[string]string{
	"create": "created", "read": "read", "update": "modified", "delete": "deleted", "rename": "renamed",
	"set attributes": "set attributes on", "get attributes": "read attributes of", "set security": "set security on",
	"get security": "read security of", "encrypt": "encrypted", "decrypt": "decrypted", "mount": "mounted",
	"unmount": "unmounted", "open": "opened",
}

func (r *renderer) fileActivity() {
	user := r.user("actor.user")
	host := r.host("device")
	file := r.file("file")
	proc := r.proc("actor.process")
	verb := r.activity(fileVerbs, "accessed")

	switch {
	case !user.empty():
		r.entity(user)
		r.text(" " + verb + " ")
		r.entity(file)
	case !proc.empty():
		r.entity(proc)
		r.text(" " + verb + " ")
		r.entity(file)
	case file.empty():
		r.text("File activity")
	default:
		r.entity(file)
		r.text(" " + pastToPassive(verb))
	}
	r.clause(" on ", host)
	if !user.empty() {
		r.clause(" via ", proc)
	}

	r.entityField("File", slot{text: firstNonEmpty(r.str("file.path"), file.text), ref: file.ref})
	r.activityField()
	r.entityField("Process", proc)
	r.entityField("User", slot{text: r.qualifiedUser("actor.user"), ref: user.ref})
	r.entityField("Host", host)
	r.hashField("file")
	if size := r.int("file.size"); size != 0 {
		r.mono("Size", strconv.Itoa(size)+" bytes")
	}

	r.keySlot("user", user)
	r.keySlot("host", host)
	r.keySlot("file", file)
	r.keyText("process", proc.text)
	r.keyText("activity", r.str("activity_name"))
}

var registryVerbs = map[string]string{
	"create": "created", "read": "read", "modify": "modified", "delete": "deleted", "rename": "renamed",
	"set security": "set security on", "restore": "restored", "import": "imported", "export": "exported",
}

func (r *renderer) registryKey() {
	host := r.host("device")
	proc := r.proc("actor.process")
	user := r.user("actor.user")
	hive, keyPath := r.str("reg_key.hive"), r.str("reg_key.path")
	keyText := strings.Trim(hive+`\`+keyPath, `\`)
	key := slot{text: keyText, ref: r.bind(identity.TypeRegistryKey, func(p map[string]any) bool {
		return propEquals(p, "key_path", keyPath) && strings.EqualFold(toString(p["hive"]), hive)
	})}
	verb := r.activity(registryVerbs, "changed")

	switch {
	case !proc.empty():
		r.entity(proc)
		r.text(" " + verb + " ")
		r.entity(key)
	case !user.empty():
		r.entity(user)
		r.text(" " + verb + " ")
		r.entity(key)
	default:
		r.entity(key)
		r.text(" " + pastToPassive(verb))
	}
	r.clause(" on ", host)

	r.entityField("Key", key)
	r.mono("Value", r.str("reg_value.name"))
	r.mono("Data", r.str("reg_value.data"))
	r.activityField()
	r.entityField("Process", proc)
	r.entityField("User", slot{text: r.qualifiedUser("actor.user"), ref: user.ref})
	r.entityField("Host", host)

	r.keySlot("host", host)
	r.keyText("process", proc.text)
	r.keySlot("user", user)
	r.keySlot("key", key)
	r.keyText("value", r.str("reg_value.name"))
	r.keyExact("data", slot{text: r.str("reg_value.data")})
	r.keyText("activity", r.str("activity_name"))
}

var jobVerbs = map[string]string{
	"create": "created", "update": "updated", "delete": "deleted", "enable": "enabled", "disable": "disabled", "start": "started",
}

func (r *renderer) scheduledJob() {
	host := r.host("device")
	actor := r.user("actor.user")
	runAs := r.user("job.user")
	name, path := r.str("job.name"), r.str("job.path")
	job := slot{text: firstNonEmpty(name, path), ref: r.bind(identity.TypeScheduledTask, func(p map[string]any) bool {
		return (name != "" && propEquals(p, "name", name)) || (path != "" && propEquals(p, "path", path))
	})}
	verb := r.activity(jobVerbs, "changed")

	if actor.empty() {
		r.text("Scheduled job ")
		r.entity(job)
		r.text(" " + pastToPassive(verb))
	} else {
		r.entity(actor)
		r.text(" " + verb + " scheduled job ")
		r.entity(job)
	}
	r.clause(" on ", host)
	r.clause(" running as ", runAs)

	r.entityField("Job", job)
	r.mono("Path", path)
	r.mono("Command line", r.str("job.command_line"))
	r.decode("job.command_line")
	r.entityField("Runs as", slot{text: r.qualifiedUser("job.user"), ref: runAs.ref})
	r.activityField()
	r.entityField("Actor", slot{text: r.qualifiedUser("actor.user"), ref: actor.ref})
	r.entityField("Host", host)

	r.keySlot("actor", actor)
	r.keySlot("host", host)
	r.keySlot("job", job)
	r.keySlot("run_as", runAs)
	r.keyExact("cmd_line", slot{text: r.str("job.command_line")})
	r.keyText("activity", r.str("activity_name"))
}

func (r *renderer) module() {
	host := r.host("device")
	proc := r.proc("actor.process")
	mod := r.file("module.file")

	if proc.empty() {
		r.text("Module ")
		r.entity(mod)
		r.text(" loaded")
	} else {
		r.entity(proc)
		r.text(" loaded ")
		r.entity(mod)
	}
	r.clause(" on ", host)

	r.entityField("Module", slot{text: firstNonEmpty(r.str("module.file.path"), mod.text), ref: mod.ref})
	r.entityField("Process", proc)
	r.field("Load type", r.str("module.load_type"))
	r.entityField("Host", host)
	r.hashField("module.file")

	r.keySlot("host", host)
	r.keyText("process", proc.text)
	r.keySlot("module", mod)
}

var accountVerbs = map[string]string{
	"create": "created", "enable": "enabled", "password change": "changed the password of", "password reset": "reset the password of",
	"disable": "disabled", "delete": "deleted", "attach policy": "attached a policy to", "detach policy": "detached a policy from",
	"lock": "locked", "unlock": "unlocked", "mfa factor enable": "enabled an MFA factor for", "mfa factor disable": "disabled an MFA factor for",
}

func (r *renderer) accountChange() {
	target := r.user("user")
	actor := r.user("actor.user")
	host := r.host("device")
	// The account-change normalizer stores an x-group as group_name and directory.
	groupName, groupDir := r.str("group.name"), r.str("group.domain")
	group := slot{text: groupName, ref: r.bind(identity.TypeGroup, func(p map[string]any) bool {
		return propEquals(p, "group_name", groupName) && (groupDir == "" || propEquals(p, "directory", groupDir))
	})}
	verb := r.activity(accountVerbs, "changed")

	if actor.empty() {
		r.text("Account ")
		r.entity(target)
		r.text(" " + pastToPassive(verb))
	} else {
		r.entity(actor)
		r.text(" " + verb + " ")
		r.entity(target)
	}
	r.clause(" in ", group)
	r.clause(" on ", host)

	r.entityField("Account", slot{text: r.qualifiedUser("user"), ref: target.ref})
	r.entityField("By", slot{text: r.qualifiedUser("actor.user"), ref: actor.ref})
	r.activityField()
	r.entityField("Group", group)
	r.field("Privileges", r.str("privileges"))
	r.entityField("Host", host)

	r.keySlot("actor", actor)
	r.keySlot("target", target)
	r.keySlot("group", group)
	r.keySlot("host", host)
	r.keyText("privileges", r.str("privileges"))
	r.keyText("activity", r.str("activity_name"))
}

func (r *renderer) email() {
	from := r.emailAddr(r.str("email.from"))
	to := r.emailAddrs("email.to")
	subject := r.str("email.subject")

	if from.empty() {
		r.text("Email")
	} else {
		r.entity(from)
		r.text(" sent")
	}
	if subject != "" {
		r.text(" “" + subject + "”")
	}
	if len(to) > 0 {
		r.text(" to ")
		for i, a := range to {
			if i == 2 {
				r.text(fmt.Sprintf(" +%d more", len(to)-2))
				break
			}
			if i > 0 {
				r.text(", ")
			}
			r.entity(a)
		}
	}

	r.entityField("From", from)
	r.mono("To", joinSlots(to))
	r.mono("Cc", joinSlots(r.emailAddrs("email.cc")))
	r.field("Subject", subject)
	r.mono("Message id", r.str("email.message_uid"))
	r.activityField()
	r.field("Disposition", r.str("disposition"))
	// The email normalizer reads attachments from email.attachments.
	attachments := pathList(r.p, "email.attachments")
	if len(attachments) == 0 {
		attachments = pathList(r.p, "email.files")
	}
	if n := len(attachments); n > 0 {
		r.field("Attachments", strconv.Itoa(n))
	}
	if n := len(pathList(r.p, "email.urls")); n > 0 {
		r.field("Links", strconv.Itoa(n))
	}

	r.keySlot("from", from)
	r.keyList("to", to, true)
	r.keyExact("subject", slot{text: subject})
}

func (r *renderer) emailAddrs(path string) []slot {
	var out []slot
	for _, v := range pathList(r.p, path) {
		if s := r.emailAddr(toString(v)); !s.empty() {
			out = append(out, s)
		}
	}
	return out
}

var emailURLVerbs = map[string]string{"send": "sent", "receive": "received", "scan": "scanned"}

func (r *renderer) emailURL() {
	user := r.user("actor.user")
	host := r.host("device")
	link := r.url(r.str("url.url_string"))
	verb := r.activity(emailURLVerbs, "opened")

	if user.empty() {
		r.text("Link ")
		r.entity(link)
		r.text(" " + pastToPassive(verb))
	} else {
		r.entity(user)
		r.text(" " + verb + " ")
		r.entity(link)
	}
	r.clause(" on ", host)

	r.entityField("URL", link)
	r.activityField()
	r.entityField("User", slot{text: r.qualifiedUser("actor.user"), ref: user.ref})
	r.entityField("Host", host)
	r.mono("Message id", r.str("email.message_uid"))

	r.keySlot("user", user)
	r.keySlot("host", host)
	r.keyExact("url", link)
	r.keyText("activity", r.str("activity_name"))
}

func (r *renderer) detection() {
	title := firstNonEmpty(r.str("finding.title"), r.str("finding_info.title"))
	uid := firstNonEmpty(r.str("finding.uid"), r.str("finding_info.uid"))
	vendor := firstNonEmpty(r.str("metadata.product.vendor_name"), r.str("metadata.product.name"))
	severity := r.str("severity")
	if severity == "" {
		severity = r.str("finding.severity")
	}
	host := r.host("evidence.device")
	if host.empty() {
		host = r.host("device")
	}
	user := r.user("evidence.actor.user")

	if vendor == "" {
		r.text("Detected")
	} else {
		r.text(vendor + " detected")
	}
	if title != "" {
		r.text(" “" + title + "”")
	}
	if severity != "" {
		r.text(" (" + strings.ToLower(severity) + ")")
	}
	r.clause(" on ", host)

	r.field("Finding", title)
	r.field("Severity", severity).Tone = severityTone(severity)
	r.field("Confidence", firstNonEmpty(r.str("confidence"), r.str("finding.confidence")))
	r.mono("Techniques", strings.Join(stringList(pathList(r.p, "finding.types")), ", "))
	r.field("Vendor", vendor)
	r.mono("Finding id", uid)
	r.field("Status", r.str("status"))
	r.entityField("Host", host)
	r.entityField("User", slot{text: r.qualifiedUser("evidence.actor.user"), ref: user.ref})

	r.keyText("vendor", vendor)
	r.keyExact("title", slot{text: title})
	r.keyText("severity", severity)
	r.keySlot("host", host)
	r.keySlot("user", user)
}

func (r *renderer) caseFinding() {
	uid := r.str("finding_info.uid")
	title := r.str("finding_info.title")
	status, priority, group := r.str("status"), r.str("priority"), r.str("assignment_group")

	r.text("Case")
	if uid != "" {
		r.text(" " + uid)
	}
	if title != "" {
		r.text(" “" + title + "”")
	}
	if status != "" {
		r.text(", " + status)
	}
	if priority != "" {
		r.text(", priority " + priority)
	}
	if group != "" {
		r.text(", assigned to " + group)
	}

	r.mono("Case", uid)
	r.field("Title", title)
	r.field("Status", status)
	r.field("Priority", priority)
	r.field("Assigned to", group)
	r.field("System", firstNonEmpty(r.str("unmapped.sor"), r.str("metadata.product.vendor_name")))
	r.mono("Link", r.str("unmapped.link"))

	r.keyText("status", status)
	r.keyText("priority", priority)
	r.keyText("assigned", group)
}

func (r *renderer) deviceInventory() {
	host := r.host("device")
	os := r.str("device.os.name")
	owner := r.str("device.owner.name")
	crit := r.str("device.criticality")

	if host.empty() {
		r.text("Device")
	} else {
		r.entity(host)
	}
	if os != "" {
		r.text(" runs " + os)
	}
	if owner != "" {
		r.text(", owned by " + owner)
	}
	if crit != "" {
		r.text(", criticality " + strings.ToLower(crit))
	}

	r.entityField("Host", host)
	r.field("OS", os)
	r.entityField("IP", r.ip("device.ip"))
	r.field("Owner", owner)
	r.field("Criticality", crit).Tone = severityTone(crit)
	r.field("Last seen", r.str("device.last_seen_time"))
	var agents []string
	for _, a := range pathList(r.p, "device.agent_list") {
		m, _ := a.(map[string]any)
		if n := toString(m["name"]); n != "" {
			agents = append(agents, n)
		}
	}
	r.field("Agents", strings.Join(agents, ", "))

	r.keySlot("host", host)
	r.keyText("os", os)
	r.keyText("owner", owner)
	r.keyText("criticality", strings.ToLower(crit))
}

// apiStatuses are the OCSF status_id captions, for a payload that carries the
// id without the name.
var apiStatuses = map[int]string{1: "Success", 2: "Failure"}

// apiActivity renders a cloud control-plane call (03 §4.15): who called which
// operation on what, in which account, and how it ended. The principal binds
// to its user-account by the durable id the record keyed it on, the account
// to its x-cloud-account by uid, each resource to its x-cloud-resource by uid
// or by type and name.
func (r *renderer) apiActivity() {
	principal := r.cloudPrincipal()
	app := r.str("actor.app_name")
	op := r.str("api.operation")
	service := r.str("api.service.name")
	resources := r.cloudResources()
	account, accountUID := r.cloudAccount()
	src := r.ip("src_endpoint.ip")
	status := firstNonEmpty(r.str("status"), apiStatuses[r.int("status_id")])
	errCode := r.str("api.response.error")
	outcome, tone := status, statusTone(status)
	if errCode != "" {
		outcome, tone = firstNonEmpty(status, "Failure")+": "+errCode, "bad"
	}

	switch {
	case !principal.empty():
		r.entity(principal)
		if op == "" {
			r.text(" called")
		}
	case app != "":
		r.text(app + " called")
	default:
		r.text("API call")
	}
	if op != "" {
		r.text(" " + op)
	}
	if len(resources) > 0 {
		r.text(" ")
		r.entity(resources[0])
		if n := len(resources) - 1; n > 0 {
			r.text(fmt.Sprintf(", +%d more", n))
		}
	} else if service != "" {
		r.text(" (" + service + ")")
	}
	r.clause(" in ", account)
	if outcome != "" {
		r.text(" · " + outcome)
	}

	if principal.empty() {
		r.field("Principal", app)
	} else {
		r.entityField("Principal", principal)
	}
	r.mono("Operation", op)
	r.field("Service", service)
	for _, res := range resources {
		r.entityField("Resource", res)
	}
	r.entityField("Account", account)
	if caller := r.callerAccount(); !caller.empty() && !strings.EqualFold(r.str("actor.user.account.uid"), accountUID) {
		r.entityField("Caller account", caller)
	}
	r.field("Region", r.str("cloud.region"))
	r.field("Outcome", outcome).Tone = tone
	r.entityField("Source", src)
	r.mono("Credential", r.credential())
	r.mono("Session", r.str("actor.session.uid"))
	r.field("User agent", r.str("http_request.user_agent"))

	actor := principal
	if actor.empty() {
		actor = slot{text: app}
	}
	r.keySlot("actor", actor)
	r.keyText("operation", op)
	r.keyText("service", service)
	r.keyList("resource", resources, false) // resource ids compare exactly
	r.keySlot("account", account)
	r.keyText("outcome", outcome)
	r.keySlot("source", src)
}

// cloudPrincipal binds the caller to its user-account by the durable id the
// normalizer keyed it on: the session issuer, the user's uid or its alternate
// uid, tried in that order against user_id. The order is the union of the
// normalizer's provider rows, safe because the entities a view receives are
// the ones derived from that event. The chip reads the name, else the full
// name, else the id.
func (r *renderer) cloudPrincipal() slot {
	name := r.str("actor.user.name")
	ids := []string{r.str("actor.session.issuer"), r.str("actor.user.uid"), r.str("actor.user.uid_alt")}
	text := firstNonEmpty(name, r.str("actor.user.full_name"), ids[1], ids[2], ids[0])
	if text == "" {
		return slot{}
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if ref := r.bind(identity.TypeUserAccount, func(p map[string]any) bool { return propEquals(p, "user_id", id) }); ref != "" {
			return slot{text: text, ref: ref}
		}
	}
	return slot{text: text}
}

// cloudAccount binds the account the call ran in: cloud.account, else the
// caller's account, else the deprecated project_uid, as the normalizer reads
// it, and returns its uid. The account's name is shown when the payload gives
// one.
func (r *renderer) cloudAccount() (slot, string) {
	for _, pre := range []string{"cloud.account", "actor.user.account"} {
		if s := r.accountAt(pre); !s.empty() {
			return s, strings.TrimSpace(r.str(pre + ".uid"))
		}
	}
	uid := strings.TrimSpace(r.str("cloud.project_uid"))
	if uid == "" {
		return slot{}, ""
	}
	return slot{text: uid, ref: r.bindAccount(uid)}, uid
}

// callerAccount binds the principal's own account, actor.user.account.
func (r *renderer) callerAccount() slot { return r.accountAt("actor.user.account") }

// accountAt binds the OCSF account object at prefix to its x-cloud-account.
func (r *renderer) accountAt(prefix string) slot {
	uid := strings.TrimSpace(r.str(prefix + ".uid"))
	if uid == "" {
		return slot{}
	}
	return slot{text: firstNonEmpty(r.str(prefix+".name"), uid), ref: r.bindAccount(uid)}
}

func (r *renderer) bindAccount(uid string) string {
	return r.bind(identity.TypeCloudAccount, func(p map[string]any) bool { return propEquals(p, "account_uid", uid) })
}

// cloudResources binds each resources[] entry to its x-cloud-resource: by uid
// when the entry has one, else by type and name.
func (r *renderer) cloudResources() []slot {
	var out []slot
	for _, item := range pathList(r.p, "resources") {
		m, _ := item.(map[string]any)
		uid, name, typ := toString(m["uid"]), toString(m["name"]), toString(m["type"])
		text := firstNonEmpty(name, uid)
		if text == "" {
			continue
		}
		ref := r.bind(identity.TypeCloudResource, func(p map[string]any) bool {
			if uid != "" {
				return propEquals(p, "uid", uid)
			}
			return propEquals(p, "name", name) && strings.EqualFold(toString(p["type"]), typ)
		})
		out = append(out, slot{text: text, ref: ref})
	}
	return out
}

// credential is the access key the call was made with, in the normalizer's
// order: the 1.7.0 credential list, then the deprecated user and session
// fields.
func (r *renderer) credential() string {
	if list := pathList(r.p, "actor.user.programmatic_credentials"); len(list) > 0 {
		m, _ := list[0].(map[string]any)
		if c := toString(m["uid"]); c != "" {
			return c
		}
	}
	return firstNonEmpty(r.str("actor.user.credential_uid"), r.str("actor.session.credential_uid"))
}

// scalars is the view of a class without a template: the payload's top-level
// scalars as fields, the class and clock keys left out, no sentence.
func (r *renderer) scalars() {
	skip := map[string]bool{"class_uid": true, "class_name": true, "category_uid": true, "category_name": true, "type_uid": true, "type_name": true, "time": true}
	keys := make([]string, 0, len(r.p))
	for k, v := range r.p {
		if skip[k] {
			continue
		}
		switch v.(type) {
		case string, float64, bool, json.Number, int, int64:
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		r.field(k, toString(r.p[k]))
	}
}

// --- small helpers --------------------------------------------------------------------

func statusTone(status string) string {
	switch strings.ToLower(status) {
	case "success", "succeeded", "allowed", "delivered":
		return "ok"
	case "failure", "failed", "denied", "blocked":
		return "bad"
	}
	return ""
}

func severityTone(s string) string {
	switch strings.ToLower(s) {
	case "critical", "high", "fatal":
		return "bad"
	case "medium":
		return "warn"
	}
	return ""
}

// pastToPassive turns an active verb phrase into its passive for a sentence
// without an actor: "created" → "was created", "set security on" → "had its
// security set".
func pastToPassive(verb string) string {
	first, _, _ := strings.Cut(verb, " ")
	switch verb {
	case "read":
		return "was read"
	case "set attributes on", "set security on":
		return "had its " + strings.Fields(verb)[1] + " set"
	case "read attributes of", "read security of":
		return "had its " + strings.Fields(verb)[1] + " read"
	case "injected into":
		return "was injected into"
	}
	if strings.HasSuffix(first, "ed") || first == "read" {
		return "was " + first
	}
	return "was " + verb
}

func endpoint(ip string, port int) string {
	if ip == "" {
		return ""
	}
	if port == 0 {
		return ip
	}
	return ip + ":" + strconv.Itoa(port)
}

func joinSlots(ss []slot) string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.text)
	}
	return strings.Join(out, ", ")
}

func baseName(path string) string {
	if path == "" {
		return ""
	}
	i := strings.LastIndexAny(path, `\/`)
	return path[i+1:]
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func stringList(vs []any) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		if s := toString(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func hashesOf(v any) map[string]string {
	out := map[string]string{}
	switch hv := v.(type) {
	case map[string]any:
		for k, val := range hv {
			if s := toString(val); s != "" {
				out[k] = s
			}
		}
	case []any:
		// OCSF 1.x fingerprint list: [{algorithm, value}].
		for _, item := range hv {
			m, _ := item.(map[string]any)
			if alg, val := toString(m["algorithm"]), toString(m["value"]); alg != "" && val != "" {
				out[alg] = val
			}
		}
	}
	return out
}

// pathAny walks a dotted path through nested maps.
func pathAny(m map[string]any, path string) any {
	var cur any = m
	for _, seg := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = mm[seg]
		if !ok {
			return nil
		}
	}
	return cur
}

func pathStr(m map[string]any, path string) string { return toString(pathAny(m, path)) }

func pathList(m map[string]any, path string) []any {
	l, _ := pathAny(m, path).([]any)
	return l
}

func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		return x.String()
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return ""
}

func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}
