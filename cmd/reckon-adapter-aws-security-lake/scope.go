package main

import (
	"strconv"
	"strings"

	"github.com/sd-strax/reckon/capability"
)

// postScope is a name-identified resource's scope, checked against each
// fetched event's own resources[]: ONE element must carry the name and every
// scope value given — type, region (the element's own; an element with no
// region does not match a given one), zone, group and namespace — so a
// scoped question never broadens to an unscoped answer, and two elements
// cannot together satisfy it. Zone, group and namespace exist only here; the
// rest repeat the query's any_match so the two agree. With neither uid nor
// name, the type and zone, group and namespace are checked on one element
// (a region then filters the event, in the query). The check can leave
// fewer rows than the limit (adr/0322). It applies only to a resource given
// without a uid: a uid is the provider's unique identifier, and no scope
// narrows it.
type postScope struct {
	name, typ, region      string
	zone, group, namespace string
}

func newPostScope(r resourceScope) postScope {
	if r.uid != "" {
		return postScope{}
	}
	ps := postScope{name: r.name, typ: r.typ, zone: r.zone, group: r.group, namespace: r.namespace}
	if r.name != "" {
		ps.region = r.region
	}
	return ps
}

func (ps postScope) empty() bool {
	return ps.typ == "" && ps.region == "" && ps.zone == "" && ps.group == "" && ps.namespace == ""
}

// filterScope keeps the events with a resources[] entry that is the resource
// the call named (by name, when given) and carries every scope value the
// call gave.
func filterScope(events []capability.OcsfPayload, r resourceScope) []capability.OcsfPayload {
	ps := newPostScope(r)
	if ps.empty() {
		return events
	}
	out := events[:0]
	for _, e := range events {
		if ps.matches(e.Raw) {
			out = append(out, e)
		}
	}
	return out
}

func (ps postScope) matches(raw map[string]any) bool {
	list, _ := raw["resources"].([]any)
	for _, item := range list {
		m, ok := item.(map[string]any)
		if ok && ps.entryMatches(m) {
			return true
		}
	}
	return false
}

func (ps postScope) entryMatches(m map[string]any) bool {
	if ps.name != "" && scalar(m["name"]) != ps.name {
		return false
	}
	g, _ := m["group"].(map[string]any)
	checks := []struct{ want, got string }{
		{ps.zone, scalar(m["zone"])},
		{ps.namespace, scalar(m["namespace"])},
		{ps.typ, scalar(m["type"])},
		{ps.region, scalar(m["region"])},
	}
	for _, c := range checks {
		if c.want != "" && c.want != c.got {
			return false
		}
	}
	if ps.group != "" && ps.group != scalar(g["name"]) && ps.group != scalar(g["uid"]) {
		return false
	}
	return true
}

// scalar is the trimmed string form of a decoded JSON string or number.
func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}
