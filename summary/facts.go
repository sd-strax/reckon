package summary

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sd-strax/reckon/aggregate"
)

// Facts is the structured tier of the summary at any state (adr/0103; 08 §8.4
// Summary): every number comes from a projection, every word from the
// record; nothing here is a judgment.
type Facts struct {
	InvestigationID    string           `json:"investigation_id"`
	Title              string           `json:"title"`
	State              string           `json:"state"`
	AsOfSequence       int64            `json:"as_of_sequence"`
	CreatedAt          *time.Time       `json:"created_at,omitempty"`
	Seed               *SeedFact        `json:"seed,omitempty"`
	Verdict            *VerdictFact     `json:"verdict,omitempty"`
	Techniques         []string         `json:"techniques"`
	Containment        ContainmentFact  `json:"containment"`
	Notification       NotificationFact `json:"notification"`
	OpenQuestions      []OpenQuestion   `json:"open_questions"`
	OwnerItems         OwnerItemsFact   `json:"owner_items"`
	ProceduresFollowed []ProcedureFact  `json:"procedures_followed"`
	// StillToDo is the ordered list of what remains, as sentences.
	StillToDo []string `json:"still_to_do"`
}

// SeedFact is the seed as the summary names it.
type SeedFact struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// VerdictFact is the verdict of record with its author's name when the
// thread holds it.
type VerdictFact struct {
	Disposition string     `json:"disposition"`
	Rationale   string     `json:"rationale,omitempty"`
	At          *time.Time `json:"at,omitempty"`
	AuthorName  string     `json:"author_name,omitempty"`
}

// ContainmentFact counts the non-notification actions.
type ContainmentFact struct {
	Total         int        `json:"total"`
	Approved      int        `json:"approved"`
	Terminal      int        `json:"terminal"`
	Pending       int        `json:"pending"`
	NearestExpiry *time.Time `json:"nearest_expiry,omitempty"`
	Done          []string   `json:"done"` // "host.isolate on WIN-A" for every SUCCEEDED action
}

// NotificationFact is the notification's status word and counts.
type NotificationFact struct {
	Status          string `json:"status"` // notified | awaiting_approval | not_yet_notified
	Sent            int    `json:"sent"`
	Requested       int    `json:"requested"`
	AwaitingReplies int    `json:"awaiting_replies"`
}

// OpenQuestion is an undecided prediction.
type OpenQuestion struct {
	PredictionID string `json:"prediction_id"`
	Ordinal      int    `json:"ordinal"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Hypothesis   string `json:"hypothesis"`
}

// OwnerItemsFact counts the owner action items by whether they still wait.
type OwnerItemsFact struct {
	Open      int      `json:"open"`
	Sent      int      `json:"sent"`
	Confirmed int      `json:"confirmed"`
	Waived    int      `json:"waived"`
	Waiting   []string `json:"waiting"` // "Identity team: rotate the credential"
}

// ProcedureFact is a procedure the thread applied.
type ProcedureFact struct {
	SOPID string `json:"sop_id"`
	Title string `json:"title"`
}

// ThreadFact is what the fold needs from a thread entry.
type ThreadFact struct {
	InterpretationID string
	ActorName        string
	ConsultedSOPs    []aggregate.ConsultedSOP
}

// OwnerFact is what the fold needs from an owner item.
type OwnerFact struct {
	Title  string
	Owner  string
	Status string
}

// Inputs are the projections the fold reads.
type Inputs struct {
	Investigation   aggregate.InvestigationCurrent
	Hypotheses      []aggregate.HypothesisCurrent
	Predictions     []aggregate.PredictionCurrent
	Actions         []aggregate.ActionCurrent
	Thread          []ThreadFact
	OwnerItems      []OwnerFact
	AwaitingReplies int
	CreatedAt       time.Time
}

func terminal(status string) bool {
	switch status {
	case aggregate.ActionStatusRequested, aggregate.ActionStatusPendingSecondary, aggregate.ActionStatusApproved, "EXECUTING":
		return false
	}
	return true
}

// BuildFacts folds the inputs into Facts. Pure.
func BuildFacts(in Inputs) Facts {
	ic := in.Investigation
	f := Facts{
		InvestigationID: ic.AggregateID.String(), Title: ic.Title, State: ic.Status, AsOfSequence: ic.LastEventSequence,
		Techniques: Techniques(in.Hypotheses), OpenQuestions: []OpenQuestion{}, ProceduresFollowed: []ProcedureFact{}, StillToDo: []string{},
		Containment: ContainmentFact{Done: []string{}}, OwnerItems: OwnerItemsFact{Waiting: []string{}},
	}
	if !in.CreatedAt.IsZero() {
		t := in.CreatedAt
		f.CreatedAt = &t
	}
	if ic.Seed != nil {
		f.Seed = &SeedFact{Kind: ic.Seed.Type, Value: SeedValueOf(ic.Seed)}
	}
	if ic.VerdictDisposition != "" {
		v := &VerdictFact{Disposition: ic.VerdictDisposition, Rationale: ic.VerdictRationale}
		if ic.VerdictAt.Valid {
			t := ic.VerdictAt.Time
			v.At = &t
		}
		for _, e := range in.Thread {
			if e.InterpretationID == ic.VerdictInterpretationID.String() {
				v.AuthorName = e.ActorName
			}
		}
		f.Verdict = v
	}
	// Containment and notification over the actions.
	var nearest *time.Time
	for _, a := range in.Actions {
		pending := a.Status == aggregate.ActionStatusRequested || a.Status == aggregate.ActionStatusPendingSecondary
		if strings.HasPrefix(a.ActionType, "notify.") {
			switch {
			case a.Status == aggregate.ActionStatusSucceeded:
				f.Notification.Sent++
			case pending:
				f.Notification.Requested++
			}
			if pending {
				f.StillToDo = append(f.StillToDo, "Approve the notification to "+targetsOf(a))
			}
			continue
		}
		f.Containment.Total++
		if a.Status != aggregate.ActionStatusRequested && a.Status != aggregate.ActionStatusPendingSecondary && a.Status != aggregate.ActionStatusRejected && a.Status != aggregate.ActionStatusExpired {
			f.Containment.Approved++
		}
		if terminal(a.Status) {
			f.Containment.Terminal++
		}
		if a.Status == aggregate.ActionStatusSucceeded {
			f.Containment.Done = append(f.Containment.Done, a.ActionType+" on "+targetsOf(a))
		}
		if pending {
			f.Containment.Pending++
			f.StillToDo = append(f.StillToDo, "Approve "+a.ActionType+" on "+targetsOf(a))
			if !a.ExpiresAt.IsZero() && (nearest == nil || a.ExpiresAt.Before(*nearest)) {
				t := a.ExpiresAt
				nearest = &t
			}
		}
	}
	f.Containment.NearestExpiry = nearest
	switch {
	case f.Notification.Sent > 0:
		f.Notification.Status = "notified"
	case f.Notification.Requested > 0:
		f.Notification.Status = "awaiting_approval"
	default:
		f.Notification.Status = "not_yet_notified"
	}
	f.Notification.AwaitingReplies = in.AwaitingReplies
	if in.AwaitingReplies > 0 {
		f.StillToDo = append(f.StillToDo, "Await the replies still outstanding")
	}
	// Open questions: undecided predictions under live hypotheses, in record order.
	live := map[string]aggregate.HypothesisCurrent{}
	for _, h := range in.Hypotheses {
		if h.Status != aggregate.HypothesisAbandoned {
			live[h.ID] = h
		}
	}
	for i, p := range in.Predictions {
		h, ok := live[p.HypothesisRef]
		if !ok || (p.Status != aggregate.PredictionUntested && p.Status != aggregate.PredictionInconclusive) {
			continue
		}
		title := p.Title
		if title == "" {
			title = aggregate.DeriveTitle(p.Statement)
		}
		name := h.Handle
		if name == "" {
			name = "H" + itoa(indexOf(in.Hypotheses, h.ID)+1)
		}
		f.OpenQuestions = append(f.OpenQuestions, OpenQuestion{PredictionID: p.ID, Ordinal: i + 1, Title: title, Status: p.Status, Hypothesis: name})
		verb := "Test"
		if p.Status == aggregate.PredictionInconclusive {
			verb = "Resolve"
		}
		f.StillToDo = append(f.StillToDo, verb+" P"+itoa(i+1)+" under "+name+": "+title)
	}
	// Owner items.
	for _, o := range in.OwnerItems {
		switch o.Status {
		case "OPEN":
			f.OwnerItems.Open++
			f.OwnerItems.Waiting = append(f.OwnerItems.Waiting, o.Owner+": "+o.Title)
			f.StillToDo = append(f.StillToDo, "Ask "+o.Owner+": "+o.Title)
		case "SENT":
			f.OwnerItems.Sent++
			f.OwnerItems.Waiting = append(f.OwnerItems.Waiting, o.Owner+": "+o.Title)
			f.StillToDo = append(f.StillToDo, "Await confirmation from "+o.Owner+": "+o.Title)
		case "CONFIRMED":
			f.OwnerItems.Confirmed++
		case "WAIVED":
			f.OwnerItems.Waived++
		}
	}
	// Procedures followed: consulted and applied, once each.
	seen := map[string]bool{}
	for _, e := range in.Thread {
		for _, s := range e.ConsultedSOPs {
			if s.Used && !seen[s.SOPID] {
				seen[s.SOPID] = true
				title := s.Title
				if title == "" {
					title = s.SOPID
				}
				f.ProceduresFollowed = append(f.ProceduresFollowed, ProcedureFact{SOPID: s.SOPID, Title: title})
			}
		}
	}
	sort.SliceStable(f.ProceduresFollowed, func(i, j int) bool { return f.ProceduresFollowed[i].Title < f.ProceduresFollowed[j].Title })
	return f
}

func targetsOf(a aggregate.ActionCurrent) string {
	parts := make([]string, 0, len(a.Targets))
	for _, t := range a.Targets {
		if t.ResolvedIdentifier != "" {
			parts = append(parts, t.ResolvedIdentifier)
		} else if t.EntityRef != "" {
			parts = append(parts, t.EntityRef)
		}
	}
	if len(parts) == 0 {
		return "its targets"
	}
	return strings.Join(parts, ", ")
}

func indexOf(hs []aggregate.HypothesisCurrent, id string) int {
	for i, h := range hs {
		if h.ID == id {
			return i
		}
	}
	return -1
}

func itoa(n int) string { return strconv.Itoa(n) }

// HandoffNote renders the handoff text (08 §8.4 Summary) from the facts, the
// narrative and what remains — the host copies it to the clipboard.
func HandoffNote(f Facts, narrative string) string {
	var b strings.Builder
	b.WriteString(f.Title + "\n\n")
	if narrative != "" {
		b.WriteString(strings.TrimSpace(narrative) + "\n\n")
	}
	if f.Verdict != nil {
		b.WriteString("Verdict: " + f.Verdict.Disposition)
		if f.Verdict.Rationale != "" {
			b.WriteString(" — " + f.Verdict.Rationale)
		}
		if f.Verdict.AuthorName != "" {
			b.WriteString(" (" + f.Verdict.AuthorName + ")")
		}
		b.WriteString("\n")
	} else {
		b.WriteString("Verdict: none yet\n")
	}
	if len(f.Containment.Done) > 0 {
		b.WriteString("Done: " + strings.Join(f.Containment.Done, "; ") + "\n")
	}
	b.WriteString("Notification: " + strings.ReplaceAll(f.Notification.Status, "_", " ") + "\n")
	if len(f.ProceduresFollowed) > 0 {
		names := make([]string, 0, len(f.ProceduresFollowed))
		for _, p := range f.ProceduresFollowed {
			names = append(names, p.Title)
		}
		b.WriteString("Procedures followed: " + strings.Join(names, ", ") + "\n")
	}
	if len(f.StillToDo) > 0 {
		b.WriteString("Still to do:\n")
		for _, t := range f.StillToDo {
			b.WriteString("- " + t + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
