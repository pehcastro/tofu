package boardy

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/rule"
	"tofu/internal/sys"
)

const (
	NeedsDecision Status = "needs-decision"

	Asked    EventKind = "asked"
	Answered EventKind = "answered"

	QuestionsHeading = "Questions"
)

type Question struct {
	ID        int      `json:"id"`
	TLDR      string   `json:"tldr"`
	ELI5      string   `json:"eli5"`
	Options   []string `json:"options"`
	Recommend int      `json:"recommend"`
	Why       string   `json:"why"`
}

var (
	questionHeading = sys.LazyRegexp(`^### Q([1-9][0-9]*) (.+)$`)
	optionLine      = sys.LazyRegexp(`^([1-9])\. (.+)$`)
	recommendLine   = sys.LazyRegexp(`^Recommend ([1-9]): (.+)$`)
)

func (q Question) tidy() Question {
	var lines []string
	for line := range strings.Lines(q.ELI5) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	q.TLDR, q.ELI5, q.Why = strings.TrimSpace(q.TLDR), strings.Join(lines, "\n"), strings.TrimSpace(q.Why)
	q.Options = slices.Clone(q.Options)
	for i := range q.Options {
		q.Options[i] = strings.TrimSpace(q.Options[i])
	}
	return q
}

func (q Question) complete() error {
	oneLine := func(text string) bool { return text != "" && !strings.ContainsAny(text, "\r\n") }
	eli5 := strings.Split(q.ELI5, "\n")
	switch {
	case !oneLine(q.TLDR) || utf8.RuneCountInString(q.TLDR) > konst.BoardTLDRRunesMost:
		return fmt.Errorf("its tldr is one line of at most %d characters", konst.BoardTLDRRunesMost)
	case q.ELI5 == "" || len(eli5) > konst.BoardELI5LinesMost:
		return fmt.Errorf("its eli5 is one to %d plain lines", konst.BoardELI5LinesMost)
	case slices.ContainsFunc(eli5, func(line string) bool { return optionLine().MatchString(line) || recommendLine().MatchString(line) }):
		return errors.New("an eli5 line reads as an option or a recommendation")
	case len(q.Options) < konst.AskPersonOptionsLeast || len(q.Options) > konst.AskPersonOptionsMost:
		return fmt.Errorf("it has %d options, and a question offers %d to %d", len(q.Options), konst.AskPersonOptionsLeast, konst.AskPersonOptionsMost)
	case slices.ContainsFunc(q.Options, func(option string) bool { return !oneLine(option) }):
		return errors.New("an option is empty or runs over a line")
	case q.Recommend < 1 || q.Recommend > len(q.Options):
		return fmt.Errorf("it recommends option %d of %d", q.Recommend, len(q.Options))
	case !oneLine(q.Why):
		return errors.New("its recommendation says why in one line")
	}
	return nil
}

func QuestionsMarkdown(questions []Question) string {
	var lines []string
	for _, q := range questions {
		lines = append(lines, fmt.Sprintf("### Q%d %s", q.ID, q.TLDR), "", q.ELI5, "")
		for i, option := range q.Options {
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, option))
		}
		lines = append(lines, "", fmt.Sprintf("Recommend %d: %s", q.Recommend, q.Why), "")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func parseQuestions(body string) ([]Question, error) {
	var questions []Question
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if match := questionHeading().FindStringSubmatch(line); match != nil {
			id, _ := strconv.Atoi(match[1])
			questions = append(questions, Question{ID: id, TLDR: match[2]})
			continue
		}
		if len(questions) == 0 {
			return nil, fmt.Errorf("the section %s has text before its first ### Q<n> heading: %q", QuestionsHeading, line)
		}
		q := &questions[len(questions)-1]
		if match := optionLine().FindStringSubmatch(line); match != nil {
			if match[1] != strconv.Itoa(len(q.Options)+1) {
				return nil, fmt.Errorf("question %d lists option %s out of order", q.ID, match[1])
			}
			q.Options = append(q.Options, match[2])
			continue
		}
		if match := recommendLine().FindStringSubmatch(line); match != nil {
			q.Recommend, _ = strconv.Atoi(match[1])
			q.Why = match[2]
			continue
		}
		q.ELI5 = strings.TrimSpace(q.ELI5 + "\n" + line)
	}
	return questions, nil
}

func (m Managed) Ask(id string, q Question, actor string) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	if m.role(board, actor) == RoleAgent && ticket.Assignee != actor {
		return ticket, RefusedError{Ticket: id, Why: fmt.Sprintf("it is assigned to %q, and an agent asks only on its own ticket", ticket.Assignee)}
	}
	q = q.tidy()
	if err := q.complete(); err != nil {
		return ticket, RefusedError{Ticket: id, Why: err.Error()}
	}
	s := m.subjectOf(rule.HookQuestionAsk, board.Key, ticket, ticket.Status, actor, 0)
	if err := m.gate(&s); err != nil {
		return ticket, err
	}
	asked, err := m.Store.Edit(id, func(t *Ticket) error {
		for _, open := range t.Questions {
			q.ID = max(q.ID, open.ID)
		}
		q.ID++
		t.Questions = append(t.Questions, q)
		return nil
	})
	if err == nil {
		err = m.Store.Record(board.Key, Event{Ticket: id, Kind: Asked, Actor: actor, Text: fmt.Sprintf("Q%d %s", q.ID, q.TLDR)})
	}
	if err != nil {
		return asked, err
	}
	s.ticket = asked
	return asked, m.after(s)
}

func decidable(ticket Ticket) error {
	if len(ticket.Questions) == 0 {
		return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it has no open question, and a ticket reaches %s only with one carrying a tldr, an eli5, options and a recommendation: tofu boardy ask %s", NeedsDecision, ticket.ID)}
	}
	for _, q := range ticket.Questions {
		if err := q.complete(); err != nil {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("Q%d is not ready to ask: %v", q.ID, err)}
		}
	}
	return nil
}
