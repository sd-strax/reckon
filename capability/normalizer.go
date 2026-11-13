package capability

import (
	"net"
	"strings"
	"time"

	"github.com/sd-strax/reckon/identity"
)

// Normalizer turns one OCSF event into interpretation-layer objects. It is a
// pure function of the event (given its resolver); normalizers are registered
// by class_uid (§4.13). Identity is never computed here directly — normalizers
// delegate to the identity resolver, the single arbiter (§7.4).
type Normalizer interface {
	ClassUID() int
	Version() int
	Normalize(evt OcsfEvent) (NormalizationResult, error)
}

// Registry dispatches OcsfEvents to the normalizer registered for their class,
// falling back to an opaque default for unrecognized classes (§4.13). One
// Registry is built per tenant, bound to that tenant's identity resolver.
type Registry struct {
	resolver *identity.Resolver
	byClass  map[int]Normalizer
	def      Normalizer
}

// NewRegistry builds a Registry bound to a tenant resolver and pre-registers the
// v0 normalizers. Adding a class is a Register call, not a change to dispatch.
func NewRegistry(r *identity.Resolver) *Registry {
	reg := &Registry{resolver: r, byClass: make(map[int]Normalizer)}
	reg.def = &opaqueNormalizer{r: r}
	reg.Register(&processNormalizer{r: r})
	reg.Register(&authNormalizer{r: r})
	reg.Register(&networkNormalizer{r: r})
	reg.Register(&dnsNormalizer{r: r})
	reg.Register(&fileActivityNormalizer{r: r})
	// One registry normalizer handles both Windows registry classes, and one
	// account normalizer the three account classes (adr/0356).
	reg.Register(&registryNormalizer{r: r, class: classRegistryKey})
	reg.Register(&registryNormalizer{r: r, class: classRegistryValue})
	reg.Register(&scheduledJobNormalizer{r: r})
	reg.Register(&moduleNormalizer{r: r})
	reg.Register(&accountNormalizer{r: r, class: classAccountChange})
	reg.Register(&accountNormalizer{r: r, class: classUserAccess})
	reg.Register(&accountNormalizer{r: r, class: classGroupManagement})
	reg.Register(&emailNormalizer{r: r})
	reg.Register(&emailURLNormalizer{r: r})
	reg.Register(&emailFileNormalizer{r: r})
	reg.Register(&incidentNormalizer{r: r})
	reg.Register(&deviceInventoryNormalizer{r: r})
	reg.Register(&userInventoryNormalizer{r: r})

	reg.Register(&apiActivityNormalizer{r: r})
	reg.Register(&cloudInventoryNormalizer{r: r})
	reg.Register(&detectionNormalizer{r: r})
	return reg
}

// Identity returns the identity resolver this registry mints with — the scoped
// one after Scoped — for objects the resolver itself mints (adr/0061).
func (reg *Registry) Identity() *identity.Resolver { return reg.resolver }

// Scoped returns a Registry that mints identity in a source scope's namespace
// (03 §3.5). An empty scope returns the receiver (the common single-organization
// path allocates nothing); a non-empty scope builds a Registry bound to
// r.resolver.Scoped(scope), so entities from the same call land in that
// organization's namespace and never merge across organizations. Cheap enough
// to build per scoped call — normalizers are tiny structs and calls are gated by
// adapter I/O.
func (reg *Registry) Scoped(scope string) *Registry {
	if scope == "" {
		return reg
	}
	return NewRegistry(reg.resolver.Scoped(scope))
}

// Register adds a normalizer, keyed by its ClassUID. A later registration for
// the same class replaces the earlier one.
func (reg *Registry) Register(n Normalizer) { reg.byClass[n.ClassUID()] = n }

// Normalize dispatches to the class's normalizer, or the opaque default when no
// normalizer is registered for the event's class.
func (reg *Registry) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	if normalizeHook != nil {
		defer normalizeHook(evt)()
	}
	if n, ok := reg.byClass[evt.ClassUID]; ok {
		return n.Normalize(evt)
	}
	return reg.def.Normalize(evt)
}

// HandledClasses returns the class_uids with a dedicated normalizer, sorted for
// stable introspection/testing.
func (reg *Registry) HandledClasses() []int {
	out := make([]int, 0, len(reg.byClass))
	for c := range reg.byClass {
		out = append(out, c)
	}
	// small n; insertion order-independent, so a simple sort keeps it stable
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// opaqueNormalizer is the §4.13 default: it emits a single ObservedData with no
// object_refs and a derived-from edge, so the LLM still sees the raw payload
// without typed entities.
type opaqueNormalizer struct{ r *identity.Resolver }

func (*opaqueNormalizer) ClassUID() int { return 0 }
func (*opaqueNormalizer) Version() int  { return 1 }
func (n *opaqueNormalizer) Normalize(evt OcsfEvent) (NormalizationResult, error) {
	return newBuilder(n.r, evt, "opaque", n.Version()).finish(nil), nil
}

// --- result builder ----------------------------------------------------------

// resultBuilder accumulates a NormalizationResult, handling the boilerplate
// shared by every normalizer: minting the ObservedData, wiring extracted-from /
// derived-from edges, and deduping repeated entities within one event.
type resultBuilder struct {
	r    *identity.Resolver
	evt  OcsfEvent
	prov Provenance

	// devices are the device objects the event's class carries, set by the
	// normalizer: the only places the shared helpers look for an OS
	// (adr/0356).
	devices []at

	seen   map[identity.STIXID]bool // SCOs and relationships recorded
	inRefs map[identity.STIXID]bool // SCOs in object_refs
	scos   []SCO
	rels   []Relationship
	edges  []Edge
	refs   []identity.STIXID
}

func newBuilder(r *identity.Resolver, evt OcsfEvent, normalizer string, version int) *resultBuilder {
	return &resultBuilder{
		r:      r,
		evt:    evt,
		seen:   make(map[identity.STIXID]bool),
		inRefs: make(map[identity.STIXID]bool),
		prov: Provenance{
			DerivationMode:    DerivationDirect,
			Tool:              evt.SourceTool,
			Normalizer:        normalizer,
			NormalizerVersion: version,
			ObservedAt:        evt.Time,
		},
	}
}

// addSCO records an SCO plus its extracted-from edge and object_ref, deduping if
// the same id was already added for this event. Returns the id for convenience.
func (b *resultBuilder) addSCO(id identity.STIXID, typ string, props map[string]any) identity.STIXID {
	if id == "" {
		return id
	}
	if !b.seen[id] {
		b.seen[id] = true
		b.scos = append(b.scos, SCO{ID: id, Type: typ, Properties: props, Provenance: b.prov})
		b.edges = append(b.edges, Edge{Type: EdgeExtractedFrom, SourceRef: id, OcsfEventID: b.evt.ID.String()})
	}
	if !b.inRefs[id] {
		b.inRefs[id] = true
		b.refs = append(b.refs, id)
	}
	return id
}

// addLinkedSCO records an SCO and its extracted-from edge without adding it to
// the observation's object_refs: an object the record names only through a
// relationship, such as the weak host an alias joins to the strong one
// (adr/0211). It dedupes like addSCO, and a later addSCO of the same id still
// puts it in object_refs.
func (b *resultBuilder) addLinkedSCO(id identity.STIXID, typ string, props map[string]any) identity.STIXID {
	if id == "" || b.seen[id] {
		return id
	}
	b.seen[id] = true
	b.scos = append(b.scos, SCO{ID: id, Type: typ, Properties: props, Provenance: b.prov})
	b.edges = append(b.edges, Edge{Type: EdgeExtractedFrom, SourceRef: id, OcsfEventID: b.evt.ID.String()})
	return id
}

// addRel records a STIX relationship between two already-extracted objects.
// The same relationship named twice in one record (an account that is both
// the actor and the target, aliasing its login twice) is recorded once.
func (b *resultBuilder) addRel(relType string, src, tgt identity.STIXID) {
	if src == "" || tgt == "" {
		return
	}
	id := b.r.Relationship(relType, src, tgt)
	if b.seen[id] {
		return
	}
	b.seen[id] = true
	b.rels = append(b.rels, Relationship{
		ID:         id,
		Type:       relType,
		SourceRef:  src,
		TargetRef:  tgt,
		Provenance: b.prov,
	})
}

// finish mints the ObservedData over the collected refs, adds its derived-from
// edge, and returns the assembled result. extensions carries custom fields
// (e.g. logon_type/status).
func (b *resultBuilder) finish(extensions map[string]any) NormalizationResult {
	odID := b.r.ObservedData(b.evt.ClassUID, b.evt.Time, b.evt.SourceTool, b.prov.NormalizerVersion, b.evt.Payload)
	b.edges = append(b.edges, Edge{Type: EdgeDerivedFrom, SourceRef: odID, OcsfEventID: b.evt.ID.String()})
	return NormalizationResult{
		ObservedData: []ObservedData{{
			ID:             odID,
			FirstObserved:  b.evt.Time,
			LastObserved:   b.evt.Time,
			NumberObserved: 1,
			ObjectRefs:     b.refs,
			Extensions:     extensions,
			Provenance:     b.prov,
		}},
		SCOs:          b.scos,
		Relationships: b.rels,
		Edges:         b.edges,
	}
}

// --- shared OCSF entity extraction ------------------------------------------
// These read the builder's event payload, mint identity via the resolver, add
// the SCO, and return its id (or "" when the source fields are absent).

// at names one object of an event: the object it is read from (the event
// itself, or an object inside it such as an entry of evidences[]) and its
// path there. The zero value names nothing.
type at struct {
	obj  map[string]any
	path string
}

// top names an object at the top of the event.
func (b *resultBuilder) top(path string) at { return at{obj: b.evt.Payload, path: path} }

// deviceObjects records the device objects the event's class carries, by
// their paths from the top of the event.
func (b *resultBuilder) deviceObjects(paths ...string) {
	b.devices = b.devices[:0]
	for _, p := range paths {
		b.devices = append(b.devices, b.top(p))
	}
}

// host extracts an x-host SCO from the OCSF device object at prefix of the
// event ("device"). See hostAt.
func (b *resultBuilder) host(prefix string) identity.STIXID {
	return b.hostAt(b.top(prefix), true)
}

// endpointHost extracts an x-host SCO from the OCSF network endpoint at
// prefix of the event ("dst_endpoint"). An endpoint has no uid_alt, so none
// is read (adr/0356).
func (b *resultBuilder) endpointHost(prefix string) identity.STIXID {
	return b.hostAt(b.top(prefix), false)
}

// hostAt extracts an x-host SCO from an OCSF device or endpoint object,
// shaped per adr/0211 (03 §7.2, §7.4). device says the object is a device,
// which carries uid_alt; a network endpoint does not.
//
// The hostname is the identity: a record that carries one mints the strong
// host on its short name, and keeps everything else the device object says as
// properties: the FQDN, the domain (device.domain, else the FQDN's remainder),
// the MAC, the device id keyed by the source that gave it, device.uid_alt and
// the agent list. OCSF defines device.uid as the device's own id and
// agent_list[].uid as the sensor's, but a source with only an agent id puts it
// in device.uid; neither is identity.
//
// A record with no hostname mints a weak host on the MAC, or failing that on
// (source tool, device.uid), marked weak, with the same properties less the
// hostname, FQDN and the FQDN's domain. A record with a hostname and a MAC
// or device id also mints the weak host each of those alone would have minted,
// outside object_refs, with an aliases edge from it to the strong host: the
// source said in this record that the identifier names that machine, so an
// event it later attributes to the identifier alone reads under the host. A
// record with no hostname, a MAC and a device id applies the same rule to the
// MAC weak host it mints: the device id's weak host is aliased to it.
//
// A placeholder hostname, MAC or device id (identity.Placeholder*) reads as
// absent: it mints no host, no weak host and no alias, and is not kept as a
// property.
func (b *resultBuilder) hostAt(loc at, device bool) identity.STIXID {
	if loc.path == "" {
		return ""
	}
	p, prefix := loc.obj, loc.path
	hostname := strings.TrimRight(strings.TrimSpace(pathStr(p, prefix+".hostname")), ".")
	mac := strings.TrimSpace(pathStr(p, prefix+".mac"))
	uid := strings.TrimSpace(pathStr(p, prefix+".uid"))

	// A placeholder MAC or device id names nothing (adr/0211): it is neither
	// a property nor an alias.
	hasMAC, hasUID := !identity.PlaceholderMAC(mac), !identity.PlaceholderDeviceUID(uid)
	props := b.deviceFacts(loc, device, mac, uid, hasMAC, hasUID)

	strong := b.r.Host(identity.HostIdentity{Hostname: hostname})
	if strong == "" {
		// The weak host keeps the record's facts too, under the key it is
		// minted on and the weak mark.
		id, idProps := b.weakHost(mac, uid)
		if id == "" {
			return ""
		}
		for k, v := range idProps {
			props[k] = v
		}
		b.addSCO(id, identity.TypeHost, props)
		// Minted on the MAC, the record also says its device id names that
		// machine: the one-record alias rule, as for a strong host.
		if hasMAC && hasUID {
			weak, weakProps := b.weakHost("", uid)
			b.addLinkedSCO(weak, identity.TypeHost, weakProps)
			b.addRel(RelAliases, weak, id)
		}
		return id
	}

	// The reported name split as the resolver splits it, case kept: a value
	// identity.SplitHostname keeps whole (an address in the hostname field) is
	// whole here too, with no FQDN and no domain.
	short, rest, hasDot := strings.Cut(hostname, ".")
	if _, domain := identity.SplitHostname(hostname); hasDot && domain == "" {
		short, rest, hasDot = hostname, "", false
	}
	props["hostname"] = short
	if hasDot {
		props["fqdn"] = hostname
	}
	if _, ok := props["domain"]; !ok && !identity.PlaceholderDomain(rest) {
		props["domain"] = rest
	}
	b.addSCO(strong, identity.TypeHost, props)

	if hasMAC {
		weak, weakProps := b.weakHost(mac, "")
		b.addLinkedSCO(weak, identity.TypeHost, weakProps)
		b.addRel(RelAliases, weak, strong)
	}
	if hasUID {
		weak, weakProps := b.weakHost("", uid)
		b.addLinkedSCO(weak, identity.TypeHost, weakProps)
		b.addRel(RelAliases, weak, strong)
	}
	return strong
}

// deviceFacts is what a device or endpoint object says about the machine
// besides its hostname, kept as properties on the host the record mints,
// strong or weak: its domain, the MAC, the device id keyed by the source that
// gave it, the agent list and, on a device, uid_alt. A placeholder MAC or
// device id (hasMAC, hasUID false) is left out.
func (b *resultBuilder) deviceFacts(loc at, device bool, mac, uid string, hasMAC, hasUID bool) map[string]any {
	p, prefix := loc.obj, loc.path
	props := map[string]any{}
	// A placeholder domain ("-", "unknown"), judged whole, names none.
	if domain := strings.TrimSpace(pathStr(p, prefix+".domain")); !identity.PlaceholderDomain(domain) {
		props["domain"] = domain
	}
	if hasMAC {
		props["mac"] = mac
	}
	switch {
	case hasUID && b.evt.SourceTool != "":
		props["source_ids"] = map[string]any{b.evt.SourceTool: uid}
	case hasUID:
		props["device_uid"] = uid // no source to key it by
	}
	if device {
		if alt := strings.TrimSpace(pathStr(p, prefix+".uid_alt")); alt != "" {
			props["device_uid_alt"] = alt
		}
	}
	if agents := agentList(p, prefix+".agent_list"); len(agents) > 0 {
		props["agents"] = agents
	}
	return props
}

// weakHost mints the weak x-host a device object with no hostname identifies
// (adr/0211): on the MAC, else on (source tool, device uid), the tiers
// identity.Host applies, a placeholder in either read as absent. It returns ""
// when neither is usable.
func (b *resultBuilder) weakHost(mac, uid string) (identity.STIXID, map[string]any) {
	if !identity.PlaceholderMAC(mac) {
		return b.r.Host(identity.HostIdentity{MAC: mac}), map[string]any{"mac": mac, "weak": true}
	}
	if identity.PlaceholderDeviceUID(uid) {
		return "", nil
	}
	id := b.r.Host(identity.HostIdentity{SourceTool: b.evt.SourceTool, SourceUID: uid})
	if id == "" {
		return "", nil
	}
	return id, map[string]any{"source_tool": b.evt.SourceTool, "device_uid": uid, "weak": true}
}

// agentList reads an OCSF agent_list at path as [{uid, name, vendor_name}],
// keeping only the fields present and dropping empty entries.
func agentList(p map[string]any, path string) []any {
	v, ok := lookupPath(p, path)
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []any
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		agent := map[string]any{}
		for _, k := range []string{"uid", "name", "vendor_name"} {
			if s := strings.TrimSpace(toString(fieldOf(m, k))); s != "" {
				agent[k] = s
			}
		}
		if len(agent) > 0 {
			out = append(out, agent)
		}
	}
	return out
}

// ipAddr extracts an ipv4-addr or ipv6-addr SCO from a string path (e.g.
// "src_endpoint.ip").
func (b *resultBuilder) ipAddr(path string) identity.STIXID {
	return b.addIPValue(pathStr(b.evt.Payload, path))
}

// addIPValue adds an ipv4-addr or ipv6-addr SCO for a literal address value
// (used when the value comes from an array element, not a fixed path). A
// placeholder address mints no id, so nothing is added and "" is returned.
func (b *resultBuilder) addIPValue(v string) identity.STIXID {
	if v == "" {
		return ""
	}
	ip := net.ParseIP(strings.TrimSpace(v))
	if ip != nil && ip.To4() == nil {
		return b.addSCO(b.r.IPv6Addr(v), identity.TypeIPv6Addr, map[string]any{"value": v})
	}
	return b.addSCO(b.r.IPv4Addr(v), identity.TypeIPv4Addr, map[string]any{"value": v})
}

// addDomain adds a domain-name SCO for a literal domain value.
func (b *resultBuilder) addDomain(v string) identity.STIXID {
	if v == "" {
		return ""
	}
	return b.addSCO(b.r.DomainName(v), identity.TypeDomainName, map[string]any{"value": v})
}

// emailAddr adds an email-addr SCO for a literal address value.
func (b *resultBuilder) emailAddr(v string) identity.STIXID {
	if v == "" {
		return ""
	}
	return b.addSCO(b.r.EmailAddr(v), identity.TypeEmailAddr, map[string]any{"value": v})
}

// emailAddrs adds an email-addr SCO for each string in the array at path of
// m, returning their ids.
func (b *resultBuilder) emailAddrs(m map[string]any, path string) []identity.STIXID {
	v, ok := lookupPath(m, path)
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var refs []identity.STIXID
	for _, item := range arr {
		if id := b.emailAddr(toString(item)); id != "" {
			refs = append(refs, id)
		}
	}
	return refs
}

// url adds a url SCO for a literal URL value.
func (b *resultBuilder) url(v string) identity.STIXID {
	if v == "" {
		return ""
	}
	return b.addSCO(b.r.URL(v), identity.TypeURL, map[string]any{"value": v})
}

// fileFrom adds a file SCO from an OCSF file object: one the caller reached
// through lookupPath or fieldOf, such as an entry of email.files[].
func (b *resultBuilder) fileFrom(m map[string]any) identity.STIXID {
	name := toString(fieldOf(m, "name"))
	filePath := toString(fieldOf(m, "path"))
	hashes := hashesFromValue(fieldOf(m, "hashes"))
	if name == "" && filePath == "" && len(hashes) == 0 {
		return ""
	}
	id := b.r.File(identity.FileIdentity{
		Hashes:          hashes,
		Name:            name,
		ParentDirectory: parentDir(filePath),
		Size:            int64(toInt(fieldOf(m, "size"))),
	})
	props := map[string]any{"name": name}
	if filePath != "" {
		props["path"] = filePath
	}
	if len(hashes) > 0 {
		props["hashes"] = hashes
	}
	return b.addSCO(id, identity.TypeFile, props)
}

// objectAt reads the object at path of m, or nil when there is none.
func objectAt(m map[string]any, path string) map[string]any {
	v, ok := lookupPath(m, path)
	if !ok {
		return nil
	}
	obj, _ := v.(map[string]any)
	return obj
}

// objectsAt reads the list at path of m as its object entries.
func objectsAt(m map[string]any, path string) []map[string]any {
	v, ok := lookupPath(m, path)
	if !ok {
		return nil
	}
	list, _ := v.([]any)
	var out []map[string]any
	for _, item := range list {
		if obj, ok := item.(map[string]any); ok {
			out = append(out, obj)
		}
	}
	return out
}

// activityExt captures the OCSF activity (create/modify/disable/…) as an
// ObservedData extension — the high-information field for the persistence and
// account-change classes (§4.6/§4.7/§4.9). Returns nil when absent.
func activityExt(p map[string]any) map[string]any {
	if a := pathStr(p, "activity_name"); a != "" {
		return map[string]any{"activity": a}
	}
	if id := pathInt(p, "activity_id"); id != 0 {
		return map[string]any{"activity_id": id}
	}
	return nil
}

// file extracts a file SCO from the OCSF file object at prefix of the event
// (e.g. "process.file").
func (b *resultBuilder) file(prefix string) identity.STIXID {
	return b.fileAt(b.evt.Payload, prefix)
}

// fileAt extracts a file SCO from the OCSF file object at prefix of m.
func (b *resultBuilder) fileAt(m map[string]any, prefix string) identity.STIXID {
	obj := objectAt(m, prefix)
	if obj == nil {
		return ""
	}
	return b.fileFrom(obj)
}

// process extracts the process an OCSF process object at prefix of m
// describes, on host: the process with its command line, its image file, and
// its parent with a parent-process-of edge. created_time drives the
// deterministic identity; when absent the resolver returns an anonymous SCO
// (§7.2), which is still a valid node, just not stitchable.
func (b *resultBuilder) process(m map[string]any, prefix string, host identity.STIXID) identity.STIXID {
	b.fileAt(m, prefix+".file")
	pid := pathInt(m, prefix+".pid")
	procID, _ := b.r.Process(host, pid, pathTime(m, prefix+".created_time"))
	b.addSCO(procID, identity.TypeProcess, map[string]any{
		"pid":          pid,
		"name":         pathStr(m, prefix+".name"),
		"command_line": pathStr(m, prefix+".cmd_line"),
	})
	if ppid := pathInt(m, prefix+".parent_process.pid"); ppid != 0 {
		parentID, _ := b.r.Process(host, ppid, pathTime(m, prefix+".parent_process.created_time"))
		b.addSCO(parentID, identity.TypeProcess, map[string]any{
			"pid":  ppid,
			"name": pathStr(m, prefix+".parent_process.name"),
		})
		b.addRel(RelParentProcessOf, parentID, procID)
	}
	return procID
}

// actorProcess extracts the actor.process of m (if a pid is present) on
// host, returning its id. Shared by the classes that carry an acting process
// and by a detection's evidence.
func (b *resultBuilder) actorProcess(m map[string]any, host identity.STIXID) identity.STIXID {
	pid := pathInt(m, "actor.process.pid")
	if pid == 0 {
		return ""
	}
	procID, _ := b.r.Process(host, pid, pathTime(m, "actor.process.created_time"))
	return b.addSCO(procID, identity.TypeProcess, map[string]any{
		"pid":  pid,
		"name": pathStr(m, "actor.process.name"),
	})
}

// endpoints extracts the addresses of m's src_endpoint and dst_endpoint, and
// the destination's domain with a resolves-to edge to its address. It returns
// the two address ids.
func (b *resultBuilder) endpoints(m map[string]any) (src, dst identity.STIXID) {
	src = b.addIPValue(pathStr(m, "src_endpoint.ip"))
	dst = b.addIPValue(pathStr(m, "dst_endpoint.ip"))
	if dom := b.addDomain(pathStr(m, "dst_endpoint.domain")); dom != "" && dst != "" {
		b.addRel(RelResolvesTo, dom, dst)
	}
	return src, dst
}

// --- payload accessors ------------------------------------------------------

func pathStr(m map[string]any, path string) string {
	if v, ok := lookupPath(m, path); ok {
		return toString(v)
	}
	return ""
}

func pathInt(m map[string]any, path string) int {
	if v, ok := lookupPath(m, path); ok {
		return toInt(v)
	}
	return 0
}

func pathTime(m map[string]any, path string) time.Time {
	if v, ok := lookupPath(m, path); ok {
		return toTime(v)
	}
	return time.Time{}
}

// hashesFromValue reads an OCSF hashes value, a list of fingerprints
// ([{"algorithm": "...", "value": "..."}]), as algorithm → digest. OCSF has
// no other form, so nothing else is read (adr/0356).
func hashesFromValue(v any) map[string]string {
	list, _ := v.([]any)
	out := map[string]string{}
	for _, item := range list {
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if algo, val := toString(fieldOf(im, "algorithm")), toString(fieldOf(im, "value")); algo != "" && val != "" {
			out[algo] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parentDir returns the directory portion of an OCSF file path, tolerating both
// Windows ("\") and POSIX ("/") separators.
func parentDir(p string) string {
	if p == "" {
		return ""
	}
	i := strings.LastIndexAny(p, `/\`)
	if i <= 0 {
		return ""
	}
	return p[:i]
}
