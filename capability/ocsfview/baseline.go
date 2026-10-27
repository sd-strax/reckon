package ocsfview

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// A baseline is a counting fact about the retrieved record, never a judgment
// (adr/0095; binding gap 14): for one entity within one investigation, the
// events of one class grouped by the class's pattern key; a group with at
// least MinPatternEvents events on at least MinPatternDays distinct days is a
// repeated pattern, and an event of the same class that matches no pattern is
// a deviation, listed with the fields that differ. The word "normal" is a
// section title in the sheet; the body says "N events over M days".
const (
	MinPatternEvents = 3
	MinPatternDays   = 3
)

// PatternKey is the class's pattern key over a payload (impl §8.3): auth →
// logon type · source · destination; network → destination · port; process →
// image name · parent. Nil for a class with no key.
func PatternKey(classUID int, payload map[string]any) map[string]string {
	b := &builder{p: payload, refs: func(string, string) string { return "" }}
	switch classUID {
	case 3002:
		_, dst := b.endpoint("dst_endpoint")
		if dst == "" {
			_, dst = b.endpoint("device")
		}
		_, src := b.endpoint("src_endpoint")
		return keyOf("logon_type", b.str("logon_type"), "source", src, "destination", dst)
	case 4001:
		dst := b.first("dst_endpoint.domain", "dst_endpoint.hostname", "dst_endpoint.ip")
		return keyOf("destination", dst, "port", b.str("dst_endpoint.port"))
	case 1007:
		return keyOf("image", b.first("process.name", "process.file.name"), "parent", b.str("process.parent_process.name"))
	default:
		return nil
	}
}

func keyOf(kv ...string) map[string]string {
	m := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// BaselineEvent is one retrieved event as the fold reads it.
type BaselineEvent struct {
	ID        string
	ClassUID  int
	ClassName string
	Time      time.Time
	Key       map[string]string
}

// Pattern is one repeated pattern: the key, the counts, and the events.
type Pattern struct {
	ClassUID    int               `json:"class_uid"`
	ClassName   string            `json:"class_name"`
	Key         map[string]string `json:"key"`
	Count       int               `json:"count"`
	Days        int               `json:"days"`
	TypicalHour int               `json:"typical_hour"` // the most frequent UTC hour
	First       time.Time         `json:"first"`
	Last        time.Time         `json:"last"`
	EventIDs    []string          `json:"event_ids"`
}

// Deviation is a retrieved event of a class that has a pattern but matches
// none, with the key fields that differ from the nearest pattern.
type Deviation struct {
	EventID   string            `json:"event_id"`
	ClassUID  int               `json:"class_uid"`
	ClassName string            `json:"class_name"`
	Time      time.Time         `json:"time"`
	Key       map[string]string `json:"key"`
	Differs   []string          `json:"differs"`
}

// BaselineView is the fold's answer.
type BaselineView struct {
	Patterns   []Pattern   `json:"patterns"`
	Deviations []Deviation `json:"deviations"`
	// The window the retrieved record actually covers.
	From             time.Time `json:"from"`
	To               time.Time `json:"to"`
	EventsConsidered int       `json:"events_considered"`
}

// Baseline folds the events into patterns and deviations. Pure; the order of
// patterns is by class then first event, of deviations by time.
func Baseline(events []BaselineEvent) BaselineView {
	v := BaselineView{Patterns: []Pattern{}, Deviations: []Deviation{}, EventsConsidered: len(events)}
	if len(events) == 0 {
		return v
	}
	groups := map[string][]BaselineEvent{}
	order := []string{}
	for i, e := range events {
		if i == 0 || e.Time.Before(v.From) {
			v.From = e.Time
		}
		if e.Time.After(v.To) {
			v.To = e.Time
		}
		if e.Key == nil {
			continue
		}
		k := groupKey(e.ClassUID, e.Key)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], e)
	}
	patternByClass := map[int][]Pattern{}
	for _, k := range order {
		g := groups[k]
		days := map[string]bool{}
		hours := map[int]int{}
		for _, e := range g {
			days[e.Time.UTC().Format("2006-01-02")] = true
			hours[e.Time.UTC().Hour()]++
		}
		if len(g) < MinPatternEvents || len(days) < MinPatternDays {
			continue
		}
		sort.Slice(g, func(a, b int) bool { return g[a].Time.Before(g[b].Time) })
		typical, best := 0, -1
		for h, n := range hours {
			if n > best || (n == best && h < typical) {
				typical, best = h, n
			}
		}
		p := Pattern{ClassUID: g[0].ClassUID, ClassName: g[0].ClassName, Key: g[0].Key, Count: len(g), Days: len(days), TypicalHour: typical, First: g[0].Time, Last: g[len(g)-1].Time}
		for _, e := range g {
			p.EventIDs = append(p.EventIDs, e.ID)
		}
		patternByClass[p.ClassUID] = append(patternByClass[p.ClassUID], p)
	}
	classes := make([]int, 0, len(patternByClass))
	for c := range patternByClass {
		classes = append(classes, c)
	}
	sort.Ints(classes)
	for _, c := range classes {
		ps := patternByClass[c]
		sort.Slice(ps, func(a, b int) bool { return ps[a].First.Before(ps[b].First) })
		v.Patterns = append(v.Patterns, ps...)
	}
	// Deviations: events of a class with a pattern that match none of them.
	for _, e := range events {
		ps := patternByClass[e.ClassUID]
		if len(ps) == 0 || e.Key == nil {
			continue
		}
		matched := false
		nearest, nearestShared := Pattern{}, -1
		for _, p := range ps {
			shared := 0
			for k, val := range p.Key {
				if e.Key[k] == val {
					shared++
				}
			}
			if shared == len(p.Key) {
				matched = true
				break
			}
			if shared > nearestShared {
				nearest, nearestShared = p, shared
			}
		}
		if matched {
			continue
		}
		var differs []string
		for k := range nearest.Key {
			if e.Key[k] != nearest.Key[k] {
				differs = append(differs, k)
			}
		}
		sort.Strings(differs)
		v.Deviations = append(v.Deviations, Deviation{EventID: e.ID, ClassUID: e.ClassUID, ClassName: e.ClassName, Time: e.Time, Key: e.Key, Differs: differs})
	}
	sort.Slice(v.Deviations, func(a, b int) bool { return v.Deviations[a].Time.Before(v.Deviations[b].Time) })
	return v
}

// groupKey is the stable string of a class and its pattern key.
func groupKey(classUID int, key map[string]string) string {
	ks := make([]string, 0, len(key))
	for k := range key {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var b strings.Builder
	b.WriteString(strconv.Itoa(classUID))
	for _, k := range ks {
		b.WriteString("|" + k + "=" + key[k])
	}
	return b.String()
}
