package boardy

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/subagent"
)

type EventKind string

const (
	Created   EventKind = "created"
	Moved     EventKind = "moved"
	Commented EventKind = "commented"
	Evidence  EventKind = "evidence"
	Spawned   EventKind = "spawned"
	Messaged  EventKind = "messaged"
	Reported  EventKind = "reported"
)

func (EventKind) Enum() []string {
	return enum(Created, Moved, Commented, Evidence, RuleFired, Waiting, Requested, Accepted, RequestDropped, Handed, Widened, ManagersChanged, Assigned, Asked, Answered, Spawned, Messaged, Reported, HookChanged, HookFailed)
}

type Event struct {
	At         time.Time        `json:"at"`
	Ticket     string           `json:"ticket"`
	Kind       EventKind        `json:"kind"`
	Actor      string           `json:"actor,omitempty"`
	Author     *Author          `json:"author,omitempty"`
	From       Status           `json:"from,omitempty"`
	To         Status           `json:"to,omitempty"`
	Text       string           `json:"text,omitempty"`
	Session    string           `json:"session,omitempty"`
	Agent      string           `json:"agent,omitempty"`
	Call       string           `json:"call,omitempty"`
	Files      []string         `json:"files,omitempty"`
	Brief      string           `json:"brief,omitempty"`
	Assignee   string           `json:"assignee,omitempty"`
	Line       int              `json:"line,omitempty"`
	Leave      []int            `json:"leave,omitempty"`
	Verdict    Verdict          `json:"verdict,omitempty"`
	Acceptance []AcceptanceLine `json:"acceptance,omitempty"`
}

type Verdict string

const (
	VerdictDone     Verdict = "done"
	VerdictConcerns Verdict = "done with concerns"
	VerdictBlocked  Verdict = "blocked"
	VerdictNeedsYou Verdict = "needs you"
)

func (Verdict) Enum() []string {
	return enum(VerdictDone, VerdictConcerns, VerdictBlocked, VerdictNeedsYou)
}

type LineState string

const (
	LineMet     LineState = "met"
	LineUnmet   LineState = "unmet"
	LineNotMine LineState = "not mine to prove"
)

func (LineState) Enum() []string { return enum(LineMet, LineUnmet, LineNotMine) }

type AcceptanceLine struct {
	Line  string    `json:"line"`
	State LineState `json:"state"`
}

func (s Store) Acceptance(ticket Ticket, leave []int) ([]AcceptanceLine, error) {
	events, err := s.ticketEvents(ticket.ID)
	if err != nil {
		return nil, err
	}
	return Accounted(subagent.InForce(ticket.Acceptance), events, leave), nil
}

func Accounted(lines []string, events []Event, leave []int) []AcceptanceLine {
	proved, unnamed, left := map[int]bool{}, 0, leave
	for _, event := range events {
		switch {
		case event.Kind == Evidence && event.Line > 0:
			proved[event.Line] = true
		case event.Kind == Evidence:
			unnamed++
		case event.Kind == Moved && event.To == Review && leave == nil:
			left = event.Leave
		}
	}
	accounted := make([]AcceptanceLine, len(lines))
	for i, line := range lines {
		accounted[i] = AcceptanceLine{Line: line, State: LineUnmet}
		switch {
		case proved[i+1]:
			accounted[i].State = LineMet
		case slices.Contains(left, i+1):
			accounted[i].State = LineNotMine
		case unnamed > 0:
			accounted[i].State, unnamed = LineMet, unnamed-1
		}
	}
	return accounted
}

func (s Store) RecordTicket(event Event) error {
	ref, err := SplitID(event.Ticket)
	if err != nil {
		return err
	}
	return s.Record(ref.Key, event)
}

func (s Store) Thread(id string) ([]Event, error) {
	events, err := s.ticketEvents(id)
	return threaded(events), err
}

func (s Store) ticketEvents(id string) ([]Event, error) {
	ref, err := SplitID(id)
	if err != nil {
		return nil, err
	}
	all, err := s.Events(ref.Key, 0)
	return slices.DeleteFunc(all, func(event Event) bool { return event.Ticket != id }), err
}

func threaded(events []Event) []Event {
	thread := []Event{}
	for _, event := range events {
		if event.Kind == Moved && event.Text == "" {
			continue
		}
		if last := len(thread) - 1; event.Kind == Spawned && last >= 0 && thread[last].Kind == Assigned && thread[last].Text == event.Assignee {
			thread = thread[:last]
		}
		if event.Kind == Reported {
			thread = slices.DeleteFunc(thread, func(said Event) bool {
				return said.Kind == Commented && said.Actor == event.Actor && event.Text != "" && strings.HasPrefix(said.Text, event.Text)
			})
		}
		thread = append(thread, event)
	}
	return thread
}

func (s Store) Record(key string, event Event) error {
	event.At = cmp.Or(event.At, time.Now().UTC().Truncate(time.Second))
	if event.Author == nil && event.Actor != "" {
		event.Author = s.authorOf(event.Actor)
	}
	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.locked(key, "events", func() error {
		file, err := os.OpenFile(filepath.Join(s.BoardDir(key), EventsFileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = file.Write(append(line, '\n'))
		return errors.Join(err, file.Close())
	})
}

func (s Store) Events(key string, from int) ([]Event, error) {
	file, err := os.Open(filepath.Join(s.BoardDir(key), EventsFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var lines [][]byte
	var skipped int
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 1<<24)
	for scanner.Scan() {
		if skipped < from {
			skipped++
			continue
		}
		lines = append(lines, slices.Clone(scanner.Bytes()))
	}
	events := make([]Event, len(lines))
	decodeErrs := s.eachInParallel(len(lines), func(at int) error { return json.Unmarshal(lines[at], &events[at]) })
	decoded, err := len(lines), scanner.Err()
	if at := slices.IndexFunc(decodeErrs, func(err error) bool { return err != nil }); at >= 0 {
		decoded, err = at, fmt.Errorf("%s line %d: %w", EventsFileName, skipped+at+1, decodeErrs[at])
	}
	if decoded == 0 {
		return nil, err
	}
	return events[:decoded], err
}
