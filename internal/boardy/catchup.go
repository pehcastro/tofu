package boardy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"tofu/internal/sys"
)

const CatchupFileName = "catchup.md"

type OpenQuestion struct {
	Ticket   string   `json:"ticket"`
	Title    string   `json:"title"`
	Status   Status   `json:"status"`
	Question Question `json:"question"`
	Answer   string   `json:"answer,omitempty"`
}

type TypedAnswer struct {
	Ticket   string `json:"ticket"`
	Question int    `json:"question"`
	TLDR     string `json:"tldr"`
	Answer   string `json:"answer"`
}

type Applied struct {
	TypedAnswer
	Logged string `json:"logged"`
	Back   Status `json:"back,omitempty"`
}

type Catchup struct {
	Board   string         `json:"board"`
	File    string         `json:"file"`
	Open    []OpenQuestion `json:"open"`
	Applied []Applied      `json:"applied"`
	Stale   []TypedAnswer  `json:"stale"`
}

var catchupHeading = sys.LazyRegexp(`^## ([A-Z][A-Z0-9]*-[1-9][0-9]*(?:-[1-9][0-9]*)?) Q([1-9][0-9]*): (.*)$`)

func (m Managed) Catchup(key string) (Catchup, error) {
	c, err := m.OpenQuestions(key)
	if err != nil {
		return c, err
	}
	return c, writeAtomic(c.File, c.markdown())
}

func (m Managed) OpenQuestions(key string) (Catchup, error) {
	path := filepath.Join(m.Store.BoardDir(key), CatchupFileName)
	typed, err := readTypedAnswers(path)
	if err != nil {
		return Catchup{}, err
	}
	tickets, err := m.Store.Tickets(key)
	if err != nil {
		return Catchup{}, err
	}
	c := Catchup{Board: key, File: path, Open: []OpenQuestion{}, Applied: []Applied{}, Stale: []TypedAnswer{}}
	for _, ticket := range tickets {
		for _, q := range ticket.Questions {
			open := OpenQuestion{Ticket: ticket.ID, Title: ticket.Title, Status: ticket.Status, Question: q}
			at := slices.IndexFunc(typed, func(a TypedAnswer) bool { return a.Ticket == ticket.ID && a.Question == q.ID })
			if at >= 0 && typed[at].TLDR == q.TLDR {
				open.Answer = typed[at].Answer
				typed = slices.Delete(typed, at, at+1)
			}
			c.Open = append(c.Open, open)
		}
	}
	c.Stale = append(c.Stale, typed...)
	return c, nil
}

func (m Managed) Answer(id string, question int, answer, actor string) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	if err := m.Manages(board.Key, actor, "apply the person's answers"); err != nil {
		return ticket, err
	}
	at := slices.IndexFunc(ticket.Questions, func(q Question) bool { return q.ID == question })
	answer = strings.Join(strings.Fields(answer), " ")
	switch {
	case at < 0:
		return ticket, RefusedError{Ticket: id, Why: fmt.Sprintf("it has no open question Q%d", question)}
	case answer == "":
		return ticket, RefusedError{Ticket: id, Why: "the answer is empty: give an option number or your own words"}
	}
	if _, err := m.apply(OpenQuestion{Ticket: ticket.ID, Title: ticket.Title, Status: ticket.Status, Question: ticket.Questions[at], Answer: answer}, actor); err != nil {
		return ticket, err
	}
	return m.Store.Get(id)
}

func (m Managed) Answers(key, actor string) (Catchup, error) {
	if err := m.Manages(key, actor, "apply the person's answers"); err != nil {
		return Catchup{}, err
	}
	listed, err := m.Catchup(key)
	if err != nil {
		return listed, err
	}
	var applied []Applied
	for _, open := range listed.Open {
		if open.Answer == "" {
			continue
		}
		done, err := m.apply(open, actor)
		applied = append(applied, done)
		if err != nil {
			return listed, err
		}
	}
	after, err := m.Catchup(key)
	after.Applied, after.Stale = append(after.Applied, applied...), listed.Stale
	return after, err
}

func (m Managed) apply(open OpenQuestion, actor string) (Applied, error) {
	q := open.Question
	done := Applied{TypedAnswer: TypedAnswer{Ticket: open.Ticket, Question: q.ID, TLDR: q.TLDR, Answer: open.Answer}, Logged: open.Answer}
	digits := strings.IndexFunc(open.Answer+" ", func(r rune) bool { return !unicode.IsDigit(r) })
	if picked, err := strconv.Atoi(open.Answer[:digits]); err == nil && picked >= 1 && picked <= len(q.Options) {
		done.Logged = fmt.Sprintf("option %d, %s", picked, q.Options[picked-1])
		if note := strings.TrimLeft(open.Answer[digits:], " ,.:;"); note != "" {
			done.Logged += "; " + note
		}
	}
	text := fmt.Sprintf("the person answered Q%d %s: %s", q.ID, q.TLDR, done.Logged)
	ticket, err := m.Store.Edit(open.Ticket, func(t *Ticket) error {
		t.Questions = slices.DeleteFunc(t.Questions, func(left Question) bool { return left.ID == q.ID })
		t.Log = strings.TrimSpace(t.Log + "\n" + logLine(actor, text))
		return nil
	})
	if err == nil {
		err = m.Store.Record(ticket.Board(), Event{Ticket: ticket.ID, Kind: Answered, Actor: actor, Text: text})
	}
	if err != nil || len(ticket.Questions) > 0 || ticket.Status != NeedsDecision {
		return done, err
	}
	if done.Back, err = m.cameFrom(ticket); err != nil {
		return done, err
	}
	_, err = m.Move(ticket.ID, done.Back, "", actor)
	return done, err
}

func (m Managed) cameFrom(ticket Ticket) (Status, error) {
	events, err := m.Store.Events(ticket.Board(), 0)
	for _, event := range slices.Backward(events) {
		if event.Kind == Moved && event.Ticket == ticket.ID && event.To == NeedsDecision {
			return event.From, err
		}
	}
	return "", errors.Join(err, fmt.Errorf("%s has no recorded move into %s, so it stays there for a manager to move", ticket.ID, NeedsDecision))
}

func readTypedAnswers(path string) ([]TypedAnswer, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var typed []TypedAnswer
	answering := false
	for line := range strings.Lines(strings.ReplaceAll(string(raw), "\r\n", "\n")) {
		line = strings.TrimSpace(line)
		if match := catchupHeading().FindStringSubmatch(line); match != nil {
			number, _ := strconv.Atoi(match[2])
			typed, answering = append(typed, TypedAnswer{Ticket: match[1], Question: number, TLDR: match[3]}), false
			continue
		}
		if rest, found := strings.CutPrefix(line, "answer:"); found && len(typed) > 0 {
			line, answering = rest, true
		}
		if answering {
			last := &typed[len(typed)-1]
			last.Answer = strings.Join(strings.Fields(last.Answer+" "+line), " ")
		}
	}
	return slices.DeleteFunc(typed, func(a TypedAnswer) bool { return a.Answer == "" }), err
}

func (c Catchup) markdown() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s catch up: %d open questions\n\nAnswer under a question with an option number or your own words after answer:, then the agent runs tofu boardy catchup --answers.\n", c.Board, len(c.Open))
	for _, open := range c.Open {
		q := open.Question
		fmt.Fprintf(&b, "\n## %s Q%d: %s\n\nOn: %s (%s)\n%s\n\n", open.Ticket, q.ID, q.TLDR, open.Title, open.Status, q.ELI5)
		for i, option := range q.Options {
			fmt.Fprintf(&b, "%d. %s\n", i+1, option)
		}
		fmt.Fprintf(&b, "\nRecommend %d: %s\n\nanswer:", q.Recommend, q.Why)
		if open.Answer != "" {
			b.WriteString(" " + open.Answer)
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}
