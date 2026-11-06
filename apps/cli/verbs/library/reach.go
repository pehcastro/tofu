package library

import (
	"cmp"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/cli/render"
	"tofu/internal/librarian/rules"
	"tofu/internal/store/session"
)

const (
	reachUsage = "tofu library reach [--never] [--json]"
	whatTool   = "tool"
)

func reachKinds() []string {
	return []string{wiring.LibraryWhatRule, wiring.LibraryWhatReference, wiring.LibraryWhatSkill, wiring.LibraryWhatAgent, whatTool}
}

type reachRow struct {
	Kind    string    `json:"kind"`
	Name    string    `json:"name"`
	Reached int       `json:"reached"`
	Held    int       `json:"held"`
	Calls   int       `json:"calls"`
	LastAt  time.Time `json:"last_at,omitzero"`

	reachedIn, heldIn map[string]bool
}

func (r reachRow) used() bool {
	if r.Kind == whatTool {
		return r.Calls > 0
	}
	return r.Reached+r.Calls > 0
}

type reach struct {
	Sessions int        `json:"sessions"`
	Never    bool       `json:"never"`
	Rows     []reachRow `json:"rows"`
}

type reachTally map[string]*reachRow

func (t reachTally) item(kind, name string) *reachRow {
	key := kind + "\x00" + name
	if t[key] == nil {
		t[key] = &reachRow{Kind: kind, Name: name, reachedIn: map[string]bool{}, heldIn: map[string]bool{}}
	}
	return t[key]
}

func (t reachTally) saw(kind, name, id string, held bool, at time.Time) *reachRow {
	row := t.item(kind, name)
	if held {
		row.heldIn[id] = true
	} else {
		row.reachedIn[id] = true
	}
	if at.After(row.LastAt) {
		row.LastAt = at
	}
	return row
}

func (t reachTally) read(header session.Header, events []session.Event) {
	for _, event := range events {
		if header.CarriedFrom != nil && event.Turn == header.ID {
			continue
		}
		switch event.Kind {
		case session.EventPrompt:
			var prompt session.PromptBody
			_ = json.Unmarshal(event.Body, &prompt)
			for _, tool := range prompt.Tools {
				t.saw(whatTool, tool, header.ID, false, event.At)
			}
			for _, part := range prompt.Parts {
				if what, name := wiring.LibraryItem(part); what != "" && !part.Off && part.State != session.PartDropped {
					t.saw(what, name, header.ID, part.State == session.PartHeld, event.At)
				}
			}
		case session.EventToolCall:
			var call session.CallBody
			_ = json.Unmarshal(event.Body, &call)
			t.saw(whatTool, call.Tool, header.ID, false, event.At).Calls++
			if what, name := wiring.LibraryLoad(call); what != "" {
				t.saw(what, name, header.ID, false, event.At).Calls++
			}
			var spawned struct{ Agent string }
			if call.Tool == loop.SpawnToolName && json.Unmarshal(call.Args, &spawned) == nil {
				t.saw(wiring.LibraryWhatAgent, cmp.Or(spawned.Agent, wiring.LibraryUnnamedAgent), header.ID, false, event.At).Calls++
			}
		case session.EventToolResult:
			var result session.ResultBody
			_ = json.Unmarshal(event.Body, &result)
			for _, found := range result.Rules {
				t.saw(wiring.LibraryWhatRule, found.Rule, header.ID, false, event.At)
			}
		}
	}
}

func reachVerb(o output.Verb, args []string, now time.Time) int {
	o.Name, o.UsageLine = "library reach", reachUsage
	var report reach
	for _, arg := range args {
		if arg != "--never" {
			return o.Usage(errors.New("unknown argument " + strconv.Quote(arg)))
		}
		report.Never = true
	}
	files, _, _ := wiring.LibrarySource()
	domains, err := rules.LoadDomains(files, wiring.LibraryRoot)
	if err != nil {
		return o.Fail(err)
	}
	tally := reachTally{}
	for _, domain := range domains {
		for _, one := range domain.Rules {
			tally.item(wiring.LibraryWhatRule, one.ID)
		}
		for _, name := range domain.References {
			tally.item(wiring.LibraryWhatReference, name)
		}
		for kind, docs := range map[string][]rules.Document{wiring.LibraryWhatSkill: domain.Skills, wiring.LibraryWhatAgent: domain.Agents} {
			for _, doc := range docs {
				tally.item(kind, strings.TrimSuffix(path.Base(doc.File), path.Ext(doc.File)))
			}
		}
	}
	store, err := session.Open()
	if err != nil {
		return o.Fail(err)
	}
	listing, err := store.ReadOnce().Listing()
	if err != nil {
		return o.Fail(err)
	}
	report.Sessions = len(listing.Sessions)
	for _, header := range listing.Sessions {
		events, err := store.Events(header.ID)
		if err != nil {
			return o.Fail(err)
		}
		tally.read(header, events)
	}
	for _, row := range tally {
		row.Reached, row.Held = len(row.reachedIn), len(row.heldIn)
		if row.used() != report.Never {
			report.Rows = append(report.Rows, *row)
		}
	}
	slices.SortFunc(report.Rows, func(a, b reachRow) int {
		return cmp.Or(cmp.Compare(slices.Index(reachKinds(), a.Kind), slices.Index(reachKinds(), b.Kind)), cmp.Compare(b.Reached, a.Reached),
			cmp.Compare(b.Calls, a.Calls), cmp.Compare(b.Held, a.Held), strings.Compare(a.Name, b.Name))
	})
	return o.Done(true, report, func(page cli.Page) []string { return reachLines(page, report, now) })
}

func reachLines(page cli.Page, report reach, now time.Time) []string {
	title, listed, mark := "Library reach", "reached", cli.Done
	if report.Never {
		title, listed, mark = "Library never reached", "never reached", cli.Idle
	}
	if report.Sessions == 0 {
		return page.Title(title, nil, cli.Verdict{Mark: cli.Idle, Text: "no session is recorded in this project"})
	}
	lines := page.Title(title, []string{output.Plural(report.Sessions, "session")}, cli.Verdict{Text: strconv.Itoa(len(report.Rows)) + " " + listed})
	for _, kind := range reachKinds() {
		var rows []cli.Row
		for _, row := range report.Rows {
			if row.Kind != kind {
				continue
			}
			var said []string
			switch {
			case row.Reached > 0 && kind == whatTool:
				said = append(said, "offered in "+output.Plural(row.Reached, "session"))
			case row.Reached > 0:
				said = append(said, "reached "+output.Plural(row.Reached, "session"))
			}
			if row.Held > 0 {
				said = append(said, "held in "+output.Plural(row.Held, "session"))
			}
			switch {
			case row.Calls > 0 && kind == wiring.LibraryWhatAgent:
				said = append(said, output.Plural(row.Calls, "spawn"))
			case row.Calls > 0 || kind == whatTool:
				said = append(said, output.Plural(row.Calls, "call"))
			}
			if !row.LastAt.IsZero() {
				said = append(said, "last "+render.Until(now.Sub(row.LastAt))+" ago")
			}
			rows = append(rows, cli.Row{Mark: mark, Cells: []string{row.Name}, Detail: strings.Join(said, " · ")})
		}
		if len(rows) > 0 {
			lines = append(append(lines, "", page.Section(output.Plural(len(rows), kind), cli.Verdict{})), cli.Indent(page.Rows(rows)...)...)
		}
	}
	return lines
}
