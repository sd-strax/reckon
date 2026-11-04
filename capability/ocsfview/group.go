package ocsfview

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Grouping the retrieved record (implementation/workbench-react.md §8.3 read
// as a pivot and a group-by, adr/0095): events of one class fold together when
// the slots named as the key carry the same values. A slot's value is the
// object the record minted when there is one, so a file keys by its identity
// (hash-first, 03 §7) and a renamed copy folds with its original; a slot the
// record did not mint keys by its text. The process itself and the network
// flow are never a key by ref: they differ on every event by construction, so
// their slots carry the name only. The fold counts; it never judges.

// SlotSpec names one slot of a class: the name a key uses and its label.
type SlotSpec struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// SlotValue is a slot's value on one event.
type SlotValue struct {
	Name string `json:"slot"`
	Text string `json:"text"`
	Ref  string `json:"ref,omitempty"`
}

// KeyPart is one slot of a group's key with the value every member shares.
type KeyPart struct {
	Slot  string `json:"slot"`
	Label string `json:"label"`
	Text  string `json:"text"`
	Ref   string `json:"ref,omitempty"`
}

// Group is one set of events sharing a key: how many, the span they cover,
// how many distinct UTC days they fall on, and the members, newest first.
type Group struct {
	ClassUID int       `json:"class_uid"`
	Key      []KeyPart `json:"key"`
	Count    int       `json:"count"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	Days     int       `json:"days"`
	EventIDs []string  `json:"event_ids"`
}

// GroupItem is one event as the fold reads it: its id, class, time and the
// slot values its rendering registered.
type GroupItem struct {
	ID       string
	ClassUID int
	Time     time.Time
	Slots    []SlotValue
}

// Keys is the key per class, as slot names; a class absent folds on its
// default key.
type Keys map[int][]string

// classSlots is the slot catalogue per class, in the order a chooser lists
// them; every template registers exactly these names.
var classSlots = map[int][]SlotSpec{
	classAuthentication:  {{"user", "Account"}, {"logon_type", "Logon type"}, {"src", "Source"}, {"dst", "Destination"}, {"status", "Result"}},
	classProcess:         {{"user", "Account"}, {"host", "Host"}, {"process", "Process"}, {"parent", "Parent"}, {"image", "Image"}, {"cmd_line", "Command line"}, {"activity", "Activity"}},
	classNetwork:         {{"host", "Host"}, {"src", "Source"}, {"dst", "Destination"}, {"dst_port", "Port"}, {"domain", "Domain"}, {"protocol", "Protocol"}, {"process", "Process"}, {"activity", "Activity"}},
	classDNS:             {{"host", "Host"}, {"src", "Requestor"}, {"query", "Query"}, {"type", "Type"}, {"rcode", "Response"}, {"answers", "Answers"}},
	classFileActivity:    {{"user", "Account"}, {"host", "Host"}, {"file", "File"}, {"process", "Process"}, {"activity", "Activity"}},
	classRegistryKey:     {{"host", "Host"}, {"process", "Process"}, {"user", "Account"}, {"key", "Key"}, {"value", "Value"}, {"data", "Data"}, {"activity", "Activity"}},
	classScheduledJob:    {{"actor", "Actor"}, {"host", "Host"}, {"job", "Job"}, {"run_as", "Runs as"}, {"cmd_line", "Command line"}, {"activity", "Activity"}},
	classModule:          {{"host", "Host"}, {"process", "Process"}, {"module", "Module"}},
	classAccountChange:   {{"actor", "By"}, {"target", "Account"}, {"group", "Group"}, {"host", "Host"}, {"privileges", "Privileges"}, {"activity", "Activity"}},
	classEmail:           {{"from", "From"}, {"to", "To"}, {"subject", "Subject"}},
	classEmailURL:        {{"user", "Account"}, {"host", "Host"}, {"url", "URL"}, {"activity", "Activity"}},
	classDetection:       {{"vendor", "Vendor"}, {"title", "Finding"}, {"severity", "Severity"}, {"host", "Host"}, {"user", "Account"}},
	classCase:            {{"status", "Status"}, {"priority", "Priority"}, {"assigned", "Assigned to"}},
	classDeviceInventory: {{"host", "Host"}, {"os", "OS"}, {"owner", "Owner"}, {"criticality", "Criticality"}},
}

// defaultKeys are the §8.3 keys, the account added to authentication; a class
// absent here folds on every slot, so only identical events fold.
var defaultKeys = map[int][]string{
	classAuthentication: {"user", "logon_type", "src", "dst"},
	classNetwork:        {"dst", "dst_port"},
	classProcess:        {"image", "parent"},
}

// Slots lists the slots of a class a key may name; nil for a class without a
// template, which never folds.
func Slots(classUID int) []SlotSpec {
	return classSlots[classUID]
}

// DefaultKey is the slot names a class folds on when the caller names none.
func DefaultKey(classUID int) []string {
	if k, ok := defaultKeys[classUID]; ok {
		return k
	}
	specs := classSlots[classUID]
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return names
}

// ParseKeys reads a key spec as the wire carries it: "default", or per class
// "class:slot,slot" joined by ";" (3002:user,logon_type;4001:dst). A class
// named with no slots ("3002:") keys on nothing, so its events never fold; a
// class without slots in the catalogue is accepted that way, since that is
// what the read's own key list says of it. A slot the class does not have is
// an error.
func ParseKeys(spec string) (Keys, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "default" {
		return nil, nil
	}
	keys := Keys{}
	for _, part := range strings.Split(spec, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		class, names, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("key %q: want class:slot,slot", part)
		}
		uid, err := strconv.Atoi(strings.TrimSpace(class))
		if err != nil {
			return nil, fmt.Errorf("key %q: class is not a class_uid", part)
		}
		key := []string{} // present and empty: no key, not the default
		for _, n := range strings.Split(names, ",") {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if !hasSlot(uid, n) {
				return nil, fmt.Errorf("key %q: class %d has no slot %q", part, uid, n)
			}
			key = append(key, n)
		}
		keys[uid] = key
	}
	return keys, nil
}

func hasSlot(classUID int, name string) bool {
	for _, s := range classSlots[classUID] {
		if s.Name == name {
			return true
		}
	}
	return false
}

func slotLabel(classUID int, name string) string {
	for _, s := range classSlots[classUID] {
		if s.Name == name {
			return s.Label
		}
	}
	return name
}

// Fold groups items by class and key: the class's entry in keys when it has
// one (an empty entry is a choice: no key), else its default. dropRef, when
// set, is the pivoted entity: a key part bound to it is left out, since every
// event here already shares it. An item whose key has no value, or whose class
// has no slots, stands in a group of its own. Groups come newest first by
// their last event; members newest first.
func Fold(items []GroupItem, keys Keys, dropRef string) []Group {
	type acc struct {
		g     Group
		days  map[string]struct{}
		times []time.Time
	}
	byKey := map[string]*acc{}
	var order []string
	for _, it := range items {
		names, chosen := keys[it.ClassUID]
		if !chosen {
			names = DefaultKey(it.ClassUID)
		}
		parts := keyParts(it, names, dropRef)
		id := keyIdentity(it, parts)
		a, ok := byKey[id]
		if !ok {
			a = &acc{g: Group{ClassUID: it.ClassUID, Key: parts, First: it.Time, Last: it.Time}, days: map[string]struct{}{}}
			byKey[id] = a
			order = append(order, id)
		}
		a.g.Count++
		a.g.EventIDs = append(a.g.EventIDs, it.ID)
		a.times = append(a.times, it.Time)
		if it.Time.Before(a.g.First) {
			a.g.First = it.Time
		}
		if it.Time.After(a.g.Last) {
			a.g.Last = it.Time
		}
		a.days[it.Time.UTC().Format("2006-01-02")] = struct{}{}
	}
	out := make([]Group, 0, len(order))
	for _, id := range order {
		a := byKey[id]
		a.g.Days = len(a.days)
		if a.g.Key == nil {
			a.g.Key = []KeyPart{}
		}
		sort.SliceStable(a.g.EventIDs, func(i, j int) bool { return a.times[i].After(a.times[j]) })
		sort.SliceStable(a.times, func(i, j int) bool { return a.times[i].After(a.times[j]) })
		out = append(out, a.g)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out
}

// keyParts picks the named slots off the item, the pivoted entity's left out.
func keyParts(it GroupItem, names []string, dropRef string) []KeyPart {
	parts := make([]KeyPart, 0, len(names))
	for _, n := range names {
		p := KeyPart{Slot: n, Label: slotLabel(it.ClassUID, n)}
		for _, s := range it.Slots {
			if s.Name == n {
				p.Text, p.Ref = s.Text, s.Ref
				break
			}
		}
		if dropRef != "" && p.Ref != "" && p.Ref == dropRef {
			continue
		}
		parts = append(parts, p)
	}
	return parts
}

// keyIdentity is the string two items share when they fold: the class and,
// per part, the ref when bound else the text folded for case. A key with no
// value at all never folds: the item's own id makes it unique.
func keyIdentity(it GroupItem, parts []KeyPart) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(it.ClassUID))
	any := false
	for _, p := range parts {
		b.WriteString("\x1f" + p.Slot + "=")
		switch {
		case p.Ref != "":
			b.WriteString(p.Ref)
			any = true
		case p.Text != "":
			b.WriteString(strings.ToLower(p.Text))
			any = true
		}
	}
	if !any {
		b.WriteString("\x1f#" + it.ID)
	}
	return b.String()
}
