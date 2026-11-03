package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/cli/page"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
)

const sessionTranscriptUsage = "tofu session transcript <name|id|head> [--from N] [--to N] [--md] [--json]"

type transcriptSpeaker string

const (
	speakerPerson transcriptSpeaker = "person"
	speakerLead   transcriptSpeaker = "lead"
	speakerBrief  transcriptSpeaker = "brief"
	speakerReport transcriptSpeaker = "report"
)

type transcriptEntry struct {
	Generation int               `json:"generation"`
	At         time.Time         `json:"at"`
	Speaker    transcriptSpeaker `json:"speaker"`
	Agent      string            `json:"agent,omitempty"`
	Text       string            `json:"text"`
}

type sessionTranscriptReport struct {
	Handle  string            `json:"handle"`
	From    int               `json:"from"`
	To      int               `json:"to"`
	Entries []transcriptEntry `json:"entries"`
}

type transcriptAsk struct {
	handle   string
	from, to int
	markdown bool
}

func sessionTranscriptVerb(o verbOutput, args []string) int {
	o.verb, o.usageLine = "session transcript", sessionTranscriptUsage
	ask, err := sessionTranscriptArgs(args)
	if err != nil {
		return o.usage(err)
	}
	store, err := session.Open()
	if err != nil {
		return o.fail(err)
	}
	family, _, err := sessionFamily(store.ReadOnce(), ask.handle)
	if err != nil {
		return o.fail(err)
	}
	last := len(family.Generations)
	ask.to = cmp.Or(ask.to, last)
	if ask.from > last || ask.to > last || ask.to < ask.from {
		return o.refuse(exitUsage, cli.Problem{What: fmt.Sprintf("%s has generations .1 to .%d, not .%d to .%d", family.Handle(), last, ask.from, ask.to),
			Hint: "tofu session " + family.Handle()})
	}
	entries, err := transcriptOf(store, family.Generations, ask.from, ask.to)
	if err != nil {
		return o.fail(err)
	}
	report := sessionTranscriptReport{Handle: family.Handle(), From: ask.from, To: ask.to, Entries: entries}
	if ask.markdown && !o.asJSON {
		if _, err := io.WriteString(o.out, transcriptMarkdown(report)); err != nil {
			return exitVerdict
		}
		return exitOK
	}
	return o.done(true, report, func(page cli.Page) []string { return transcriptLines(page, report) })
}

func transcriptOf(store *session.Store, generations []session.Header, from, to int) ([]transcriptEntry, error) {
	entries := []transcriptEntry{}
	for at, header := range generations[:to] {
		if info, err := os.Stat(store.EventsPath(header.ID)); at+1 < from && (err != nil || info.ModTime().Before(generations[from-1].At)) {
			continue
		}
		events, err := store.Events(header.ID)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			entry, said := transcriptEntryOf(event)
			if !said || header.CarriedFrom != nil && event.Turn == header.ID {
				continue
			}
			entry.Generation = at + 1
			if event.Agent != "" {
				recordedDuring := slices.IndexFunc(generations, func(next session.Header) bool { return next.At.After(event.At) })
				if recordedDuring < 0 {
					recordedDuring = len(generations)
				}
				entry.Generation = max(recordedDuring, at+1)
			}
			if entry.Generation >= from && entry.Generation <= to {
				entries = append(entries, entry)
			}
		}
	}
	slices.SortStableFunc(entries, func(a, b transcriptEntry) int { return cmp.Or(a.Generation-b.Generation, a.At.Compare(b.At)) })
	return entries, nil
}

func sessionTranscriptArgs(args []string) (transcriptAsk, error) {
	ask := transcriptAsk{from: 1}
	var handles []string
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == jsonFlag:
		case arg == "--md":
			ask.markdown = true
		case arg == "--from" || arg == "--to":
			if i+1 == len(args) {
				return ask, fmt.Errorf("%s needs a generation number", arg)
			}
			i++
			number, err := strconv.Atoi(strings.TrimPrefix(args[i], "."))
			if err != nil || number < 1 {
				return ask, fmt.Errorf("%s %q is not a generation number such as 1 or .2", arg, args[i])
			}
			if arg == "--from" {
				ask.from = number
			} else {
				ask.to = number
			}
		case strings.HasPrefix(arg, "-"):
			return ask, fmt.Errorf("unknown argument %q", arg)
		default:
			handles = append(handles, arg)
		}
	}
	if len(handles) != 1 {
		return ask, fmt.Errorf("%d operands, want <name|id>", len(handles))
	}
	ask.handle = handles[0]
	return ask, nil
}

func transcriptEntryOf(event session.Event) (transcriptEntry, bool) {
	entry := transcriptEntry{At: event.At, Agent: event.Agent}
	switch event.Kind {
	case session.EventMessage:
		var said session.MessageBody
		if event.Agent != "" || json.Unmarshal(event.Body, &said) != nil || strings.TrimSpace(said.Content) == "" {
			return entry, false
		}
		origin := turn.OriginOf(said)
		switch {
		case said.Role == session.RoleAssistant:
			entry.Speaker, entry.Text = speakerLead, said.Content
		case said.Role == session.RoleUser && origin.Kind() == llm.OriginPerson && origin.Source != llm.SourceForkTask:
			text, typed := turn.TaskIn(said.Content)
			if !typed {
				text = said.Content
			}
			entry.Speaker, entry.Text = speakerPerson, text
		default:
			return entry, false
		}
	case session.EventSpawn:
		var spawned session.SpawnBody
		if json.Unmarshal(event.Body, &spawned) != nil {
			return entry, false
		}
		entry.Speaker, entry.Agent, entry.Text = speakerBrief, spawned.Agent, spawned.Mission
	case session.EventReport:
		var reported session.ReportBody
		if json.Unmarshal(event.Body, &reported) != nil {
			return entry, false
		}
		entry.Speaker, entry.Text = speakerReport, reported.Text
	default:
		return entry, false
	}
	return entry, strings.TrimSpace(entry.Text) != ""
}

func (entry transcriptEntry) heading() string {
	switch entry.Speaker {
	case speakerBrief:
		return "brief to " + entry.Agent
	case speakerReport:
		return "report from " + entry.Agent
	}
	return string(entry.Speaker)
}

func transcriptMarkdown(report sessionTranscriptReport) string {
	var text strings.Builder
	generation := 0
	text.WriteString("# " + report.Handle + " transcript\n")
	for _, entry := range report.Entries {
		if entry.Generation != generation {
			generation = entry.Generation
			text.WriteString("\n## generation ." + strconv.Itoa(generation) + "\n")
		}
		text.WriteString("\n**" + entry.heading() + "** " + entry.At.Format(time.DateTime) + "\n\n" + strings.TrimSpace(entry.Text) + "\n")
	}
	return text.String()
}

func transcriptLines(page cli.Page, report sessionTranscriptReport) []string {
	lines := append(page.Title("Transcript", []string{report.Handle, "." + strconv.Itoa(report.From) + " to ." + strconv.Itoa(report.To), plural(len(report.Entries), "message")}, cli.Verdict{}), "")
	for _, entry := range report.Entries {
		lines = append(lines, page.Subject("."+strconv.Itoa(entry.Generation)+" "+entry.heading())+" "+page.Label(entry.At.Format(sessionClock)))
		lines = append(append(lines, cli.Indent(strings.Split(strings.TrimSpace(entry.Text), "\n")...)...), "")
	}
	return lines
}
