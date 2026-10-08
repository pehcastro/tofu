package learn

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"time"

	"tofu/internal/memory"
)

type Class string

const (
	ClassDefect   Class = "defect"
	ClassLibrary  Class = "library"
	ClassPersonal Class = "personal"
	ClassSetting  Class = "setting"
	ClassProject  Class = "project"
)

var classes = []Class{ClassDefect, ClassLibrary, ClassPersonal, ClassSetting, ClassProject}

func (c Class) Label() string {
	switch c {
	case ClassDefect:
		return "a tofu defect"
	case ClassLibrary:
		return "a tofu library rule"
	case ClassPersonal:
		return "your own rule"
	case ClassSetting:
		return "a setting"
	case ClassProject:
		return "about your project"
	}
	panic("learn: unknown class " + string(c))
}

type Target string

const (
	TargetMemory   Target = "memory"
	TargetRule     Target = "rule"
	TargetUpstream Target = "upstream"
	TargetSetting  Target = "setting"
	TargetNone     Target = "none"
)

type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

type Mode string

const (
	ModeLocal Mode = "local"
	ModeModel Mode = "model"
)

const (
	forkCarry   = "fork carry"
	leadTurnEnd = "lead turn end"
)

type Quote struct {
	Session string    `json:"session"`
	At      time.Time `json:"at"`
	Text    string    `json:"text"`
	Calls   int       `json:"calls"`
}

type Check struct {
	Session string    `json:"session"`
	At      time.Time `json:"at"`
	Request string    `json:"request"`
	Present bool      `json:"present"`
}

type Finding struct {
	ID           int          `json:"id,omitempty"`
	Key          string       `json:"key"`
	Class        Class        `json:"class"`
	Title        string       `json:"title"`
	Rule         string       `json:"rule,omitempty"`
	Reason       string       `json:"reason"`
	Setting      string       `json:"setting,omitempty"`
	Value        string       `json:"value,omitempty"`
	Confidence   Confidence   `json:"confidence"`
	Times        int          `json:"times"`
	Sessions     int          `json:"sessions"`
	Repeats      int          `json:"repeats"`
	Calls        int          `json:"calls"`
	Built        string       `json:"last_seen_on,omitempty"`
	FixedIn      string       `json:"fixed_in,omitempty"`
	FixedSameDay bool         `json:"fixed_the_day_last_seen,omitempty"`
	Scope        memory.Scope `json:"scope,omitempty"`
	Said         string       `json:"said,omitempty"`
	Session      string       `json:"session,omitempty"`
	Retire       string       `json:"retire,omitempty"`
	Mechanism    string       `json:"mechanism,omitempty"`
	WrittenBy    string       `json:"written_by,omitempty"`
	Quotes       []Quote      `json:"quotes"`
	Checks       []Check      `json:"checks,omitempty"`
	words        []string
	offered      string
	byWords      bool
}

func (f Finding) Present() int {
	present := 0
	for _, c := range f.Checks {
		if c.Present {
			present++
		}
	}
	return present
}

func (f Finding) Target() Target {
	switch f.Class {
	case ClassDefect, ClassLibrary:
		return TargetUpstream
	case ClassSetting:
		return TargetSetting
	case ClassProject:
		return TargetNone
	case ClassPersonal:
		if f.Retire != "" {
			return TargetRule
		}
		return TargetMemory
	}
	panic("learn: unknown class " + string(f.Class))
}

func (f Finding) Command() string {
	switch f.Target() {
	case TargetUpstream:
		return "tofu learn upstream " + strconv.Itoa(f.ID)
	case TargetNone:
		return ""
	}
	if f.Rule == "" && f.Setting == "" {
		return ""
	}
	return "tofu learn apply " + strconv.Itoa(f.ID)
}

type Summary struct {
	Wrong   []string `json:"wrong"`
	Repeats int      `json:"repeats"`
	Calls   int      `json:"calls"`
	Do      []string `json:"do"`
}

func confidenceOf(sessions int, mode Mode) Confidence {
	level := min(sessions, HighConfidenceSessions)
	if mode == ModeLocal {
		level--
	}
	return []Confidence{ConfidenceLow, ConfidenceLow, ConfidenceMedium, ConfidenceHigh}[max(level, 0)]
}

func (r *Run) settle(found []Finding, known Known) {
	found = append(found, lostWords(found)...)
	r.Findings, r.Held, r.Watching, r.Fixed, r.Project, r.Decided = nil, nil, nil, nil, nil, nil
	var proposed []Finding
	retired := map[string]bool{}
	for _, f := range found {
		f.Times, f.Repeats, f.Confidence = len(f.Quotes), len(f.Quotes)-1, confidenceOf(f.Sessions, r.Mode)
		for _, q := range f.Quotes[1:] {
			f.Calls += q.Calls
		}
		f.markFixed(known.Releases)
		if f.Class == ClassPersonal {
			f.Scope, f.Said, f.Session = memory.Global, f.Quotes[len(f.Quotes)-1].Text, f.Quotes[len(f.Quotes)-1].Session
			if entry, again := needed(f, known.Memory, retired); again {
				f.Scope, f.Retire, f.Rule, f.Said = entry.Scope, entry.ID, entry.Text, entry.Said
				retired[entry.ID] = true
			}
		}
		f.Key = keyOf(f)
		switch {
		case f.FixedIn != "":
			r.Fixed = append(r.Fixed, f)
		case f.Class == ClassProject:
			if f.Sessions >= CorroboratingPlaces {
				r.Project = append(r.Project, f)
			}
		case f.Sessions < CorroboratingPlaces:
			r.Watching = append(r.Watching, f)
		case quiet(f, known.Decisions):
			r.Decided = append(r.Decided, f)
		default:
			proposed = append(proposed, f)
		}
	}
	slices.SortStableFunc(proposed, func(a, b Finding) int { return cmp.Or(b.Sessions-a.Sessions, b.Times-a.Times) })
	for i := range proposed {
		proposed[i].ID = i + 1
	}
	cut := min(len(proposed), ProposalCap)
	r.Findings, r.Held = proposed[:cut], proposed[cut:]
	r.Summary = Summary{}
	for _, f := range slices.DeleteFunc(slices.Clone(proposed), func(f Finding) bool { return f.Mechanism == forkCarry }) {
		r.Summary.Repeats, r.Summary.Calls = r.Summary.Repeats+f.Repeats, r.Summary.Calls+f.Calls
	}
	for _, f := range r.Findings {
		if len(r.Summary.Wrong) < ShownInSummary {
			r.Summary.Wrong = append(r.Summary.Wrong, f.Title)
		}
		if command := f.Command(); command != "" && len(r.Summary.Do) < ShownInSummary {
			r.Summary.Do = append(r.Summary.Do, command)
		}
	}
}

func lostWords(found []Finding) []Finding {
	lost := Finding{Class: ClassDefect, Mechanism: forkCarry, byWords: true,
		Title: "A correction made before a fork did not hold after it: your earlier words were missing from the request"}
	sessions := map[string]bool{}
	for _, f := range found {
		if f.Class != ClassPersonal || f.Present() == len(f.Checks) {
			continue
		}
		lost.Quotes, lost.Checks = append(lost.Quotes, f.Quotes...), append(lost.Checks, f.Checks...)
		for _, q := range f.Quotes {
			sessions[q.Session] = true
		}
	}
	if len(lost.Quotes) == 0 {
		return nil
	}
	slices.SortStableFunc(lost.Quotes, func(a, b Quote) int { return a.At.Compare(b.At) })
	lost.Sessions = len(sessions)
	lost.Reason = "your earlier words were missing from the request the model answered in " + strconv.Itoa(len(lost.Checks)-lost.Present()) + " of " + strconv.Itoa(len(lost.Checks)) + " times a corrected mistake came back"
	return []Finding{lost}
}

func needed(f Finding, entries []memory.Entry, retired map[string]bool) (memory.Entry, bool) {
	for _, entry := range slices.DeleteFunc(slices.Clone(entries), func(e memory.Entry) bool { return retired[e.ID] }) {
		stems := stemsOf(entry.Text + " " + entry.Said)
		places := map[string]bool{}
		for _, q := range f.Quotes {
			if q.At.After(entry.At) && shared(stemsOf(q.Text), stems, nil) >= SharedTellingWords {
				places[q.Session] = true
			}
		}
		if len(places) >= CorroboratingPlaces {
			return entry, true
		}
	}
	return memory.Entry{}, false
}

func keyOf(f Finding) string {
	first := f.Mechanism
	if f.Mechanism == "" {
		first = flat(f.Quotes[0].Text)
	}
	sum := sha256.Sum256([]byte(string(f.Target()) + "\x00" + first))
	return hex.EncodeToString(sum[:4])
}
