package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/boardy"
)

const (
	boardKey       = "BENCH"
	mockSeed       = 1356
	epicCount      = 12
	sprintCount    = 4
	childEvery     = 25
	childrenEach   = 2
	logPerMoveMost = 7
)

var mockStart = time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)

var vocabulary = strings.Fields(`the a board ticket field value reader writer cache path folder file line row column
store answer request window pane label count number list order sort filter group view sprint epic
moves reads writes parses checks opens closes holds waits starts ends keeps drops builds renders
quickly once twice again later before after under over through across beside within without
small large first last next every other same new old plain typed raw clean stale fresh slow fast`)

type mock struct {
	random *rand.Rand
	store  boardy.Store
	events []boardy.Event
}

func (m *mock) words(least, most int) string {
	picked := make([]string, least+m.random.IntN(most-least+1))
	for i := range picked {
		picked[i] = vocabulary[m.random.IntN(len(vocabulary))]
	}
	return strings.Join(picked, " ")
}

func (m *mock) bullets(least, most int) string {
	lines := make([]string, least+m.random.IntN(most-least+1))
	for i := range lines {
		lines[i] = "- " + m.words(8, 24)
	}
	return strings.Join(lines, "\n")
}

func (m *mock) status() boardy.Status {
	roll := m.random.IntN(100)
	for _, share := range []struct {
		status boardy.Status
		upTo   int
	}{{boardy.Done, 68}, {boardy.Dropped, 73}, {boardy.Backlog, 82}, {boardy.Todo, 86}, {boardy.Doing, 89}, {boardy.Review, 93}, {boardy.Blocked, 95}, {boardy.NeedsDecision, 98}} {
		if roll < share.upTo {
			return share.status
		}
	}
	return boardy.Triage
}

func (m *mock) question(id int) boardy.Question {
	options := make([]string, 2+m.random.IntN(3))
	for i := range options {
		options[i] = m.words(4, 12)
	}
	return boardy.Question{ID: id, TLDR: m.words(4, 10), ELI5: m.words(10, 20) + "\n" + m.words(10, 20), Options: options, Recommend: 1 + m.random.IntN(len(options)), Why: m.words(6, 14)}
}

func (m *mock) ticket(id string, at time.Time) boardy.Ticket {
	status := m.status()
	t := boardy.Ticket{Front: boardy.Front{ID: id, Title: m.words(4, 10), Type: boardy.Task, Status: status, Priority: boardy.Priority("P" + strconv.Itoa(m.random.IntN(5))),
		Assignee: []string{"", "go-dev", "bench", "orchestrator"}[m.random.IntN(4)], Points: m.random.IntN(6), Owns: []string{"internal/" + m.words(1, 1) + "/**"}, Created: at},
		Problem: m.words(20, 60), Scope: m.bullets(2, 4), Acceptance: []string{m.bullets(2, 4)}}
	if ref, _ := boardy.SplitID(id); ref.Child != 0 {
		t.Type, t.Parent = boardy.Subtask, ref.Parent()
	}
	if m.random.IntN(3) > 0 {
		t.Epic = fmt.Sprintf("E%d", 1+m.random.IntN(epicCount))
	}
	if m.random.IntN(4) == 0 {
		t.Sprint = fmt.Sprintf("S%d", 1+m.random.IntN(sprintCount))
	}
	if status == boardy.Blocked {
		t.Reason = m.words(4, 10)
	}
	if status == boardy.NeedsDecision {
		for q := range 1 + m.random.IntN(2) {
			t.Questions = append(t.Questions, m.question(q+1))
		}
	}
	m.history(&t)
	return t
}

func (m *mock) history(t *boardy.Ticket) {
	m.events = append(m.events, boardy.Event{At: t.Created, Ticket: t.ID, Kind: boardy.Created, Actor: "orchestrator", To: boardy.Backlog, Text: t.Title})
	clock, from := t.Created, boardy.Backlog
	var log []string
	for _, step := range []boardy.Status{boardy.Todo, boardy.Doing, boardy.Review, t.Status} {
		if from == t.Status {
			break
		}
		clock = clock.Add(time.Duration(10+m.random.IntN(600)) * time.Minute)
		m.events = append(m.events, boardy.Event{At: clock, Ticket: t.ID, Kind: boardy.Moved, Actor: "orchestrator", From: from, To: step, Text: m.words(3, 12)})
		from = step
		for range m.random.IntN(logPerMoveMost + 1) {
			text := m.words(30, 90)
			clock = clock.Add(time.Duration(1+m.random.IntN(90)) * time.Minute)
			log = append(log, "- "+clock.Format(time.DateOnly)+" go-dev: "+text)
			m.events = append(m.events, boardy.Event{At: clock, Ticket: t.ID, Kind: boardy.Commented, Actor: "go-dev", Text: text})
		}
	}
	t.Log, t.Updated = strings.Join(log, "\n"), clock
}

func (m *mock) write(t boardy.Ticket) error {
	path, err := m.store.TicketPath(t.ID)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	if err == nil {
		err = os.WriteFile(path, t.Markdown(), 0o644)
	}
	return err
}

func (m *mock) plans() error {
	for i := 1; i <= epicCount; i++ {
		if err := m.store.SaveEpic(boardKey, boardy.Epic{ID: fmt.Sprintf("E%d", i), Title: m.words(3, 7), Milestone: fmt.Sprintf("M%d", 1+i%3), Text: m.words(10, 30)}); err != nil {
			return err
		}
	}
	for i := 1; i <= 3; i++ {
		if err := m.store.SaveMilestone(boardKey, boardy.Milestone{ID: fmt.Sprintf("M%d", i), Title: m.words(3, 6), Due: boardy.DayOf(mockStart.AddDate(0, 2*i, 0))}); err != nil {
			return err
		}
	}
	for i := 1; i <= sprintCount; i++ {
		state := boardy.Planned
		switch {
		case i < sprintCount-1:
			state = boardy.Closed
		case i == sprintCount-1:
			state = boardy.Active
		}
		start := mockStart.AddDate(0, 0, 14*(i-1))
		if err := m.store.SaveSprint(boardKey, boardy.Sprint{ID: fmt.Sprintf("S%d", i), Title: m.words(2, 5), State: state, Start: boardy.DayOf(start), End: boardy.DayOf(start.AddDate(0, 0, 13))}); err != nil {
			return err
		}
	}
	return nil
}

func generate(boardDir string, tickets int) (string, error) {
	m := &mock{random: rand.New(rand.NewPCG(mockSeed, mockSeed)), store: boardy.Store{Dir: filepath.Dir(boardDir)}}
	if err := m.plans(); err != nil {
		return "", err
	}
	written, number := 0, 0
	for written < tickets {
		number++
		at := mockStart.Add(time.Duration(number) * 37 * time.Minute)
		ids := []string{fmt.Sprintf("%s-%d", boardKey, number)}
		for child := 1; number%childEvery == 0 && child <= childrenEach; child++ {
			ids = append(ids, fmt.Sprintf("%s-%d-%d", boardKey, number, child))
		}
		for _, id := range ids[:min(len(ids), tickets-written)] {
			if err := m.write(m.ticket(id, at)); err != nil {
				return "", err
			}
			written++
		}
	}
	slices.SortStableFunc(m.events, func(a, b boardy.Event) int { return a.At.Compare(b.At) })
	var lines strings.Builder
	for _, event := range m.events {
		raw, err := json.Marshal(event)
		if err != nil {
			return "", err
		}
		lines.Write(append(raw, '\n'))
	}
	if err := os.WriteFile(filepath.Join(boardDir, boardy.EventsFileName), []byte(lines.String()), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(boardDir, "counter"), []byte(strconv.Itoa(number)+"\n"), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d tickets (%d top level), %d events", written, number, len(m.events)), nil
}
