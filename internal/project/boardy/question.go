package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/librarian/rules"
	"tofu/internal/platform/fs"
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
	Answer    string   `json:"answer,omitempty"`
	Away      string   `json:"away,omitempty"`
}

var (
	questionHeading = fs.LazyRegexp(`^### Q([1-9][0-9]*) (.+)$`)
	optionLine      = fs.LazyRegexp(`^([1-9])\. (.+)$`)
	recommendLine   = fs.LazyRegexp(`^Recommend ([1-9]): (.+)$`)
	answerLine      = fs.LazyRegexp(`^Answer: (.+)$`)
	awayLine        = fs.LazyRegexp(`^Away: (.+)$`)
)

func (t Ticket) OpenQuestions() []Question {
	return slices.DeleteFunc(slices.Clone(t.Questions), func(q Question) bool { return q.Answer != "" })
}

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
	case slices.ContainsFunc(eli5, func(line string) bool {
		return optionLine().MatchString(line) || recommendLine().MatchString(line) || answerLine().MatchString(line) || awayLine().MatchString(line)
	}):
		return errors.New("an eli5 line reads as an option, a recommendation, an answer or an away note")
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
		if q.Answer != "" {
			lines = append(lines, "Answer: "+q.Answer, "")
		}
		if q.Away != "" {
			lines = append(lines, "Away: "+q.Away, "")
		}
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
		if match := answerLine().FindStringSubmatch(line); match != nil {
			q.Answer = match[1]
			continue
		}
		if match := awayLine().FindStringSubmatch(line); match != nil {
			q.Away = match[1]
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
	if m.role(board, actor) == RoleAgent && !heldBy(ticket, actor) {
		return ticket, RefusedError{Ticket: id, Why: fmt.Sprintf("it is assigned to %q, and an agent asks only on its own ticket", ticket.Assignee)}
	}
	q = q.tidy()
	if err := q.complete(); err != nil {
		return ticket, RefusedError{Ticket: id, Why: err.Error()}
	}
	s := m.subjectOf(rules.HookQuestionAsk, board.Key, ticket, ticket.Status, actor, 0)
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
		err = m.Store.Record(board.Key, Event{Ticket: id, Kind: Asked, Actor: actor, Question: q.ID, Text: fmt.Sprintf("Q%d %s", q.ID, q.TLDR)})
	}
	if err != nil {
		return asked, err
	}
	s.ticket = asked
	return asked, m.after(s)
}

func (m Managed) ReportNotPossible(id string, q Question, actor string) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	switch {
	case err != nil:
		return ticket, err
	case m.role(board, actor) != RoleAgent || !heldBy(ticket, actor):
		return ticket, RefusedError{Ticket: id, Why: fmt.Sprintf("it is assigned to %q, and only the agent working a ticket reports it not possible", ticket.Assignee)}
	case ticket.Status != Doing:
		return ticket, RefusedError{Ticket: id, Why: fmt.Sprintf("it is %s, and an agent reports a ticket not possible while it is %s", ticket.Status, Doing)}
	case strings.TrimSpace(q.ELI5) == "":
		return ticket, RefusedError{Ticket: id, Why: "the not_possible report has no reason: say in reason what cannot be done as asked and how you know, such as a vendor doc, an upstream issue or a probe you ran"}
	}
	q.TLDR = "not possible as asked: " + ticket.Title
	if runes := []rune(q.TLDR); len(runes) > konst.BoardTLDRRunesMost {
		q.TLDR = string(runes[:konst.BoardTLDRRunesMost])
	}
	hooks, err := m.Store.hooks(board.Key)
	if err != nil {
		return ticket, err
	}
	prospect := ticket
	prospect.Questions = append(slices.Clone(ticket.Questions), q.tidy())
	dry := m.subjectOf(rules.HookTicketMove, board.Key, prospect, NeedsDecision, actor, 0)
	dry.dry = true
	if _, _, err := m.weigh(slices.DeleteFunc(hooks, func(h rules.Hook) bool { return runsCheck(h, "may_move") }), rules.WhenBefore, dry); err != nil {
		return ticket, err
	}
	if ticket, err = m.Ask(id, q, actor); err != nil {
		return ticket, err
	}
	asked := ticket.Questions[len(ticket.Questions)-1]
	if err := m.Store.Record(board.Key, Event{Ticket: id, Kind: NotPossible, Actor: actor, Question: asked.ID, Text: asked.ELI5}); err != nil {
		return ticket, err
	}
	return m.move(board, ticket, NeedsDecision, "reported not possible, so the person decides Q"+strconv.Itoa(asked.ID), actor, 0, nil)
}

const (
	awayRef     = "away_decision@1"
	awayDecides = "irreversible"
)

func (m Managed) DecideAway(id string, q Question, actor string) (Ticket, error) {
	ticket, err := m.Store.Get(id)
	if err != nil {
		return ticket, err
	}
	option := q.Options[q.Recommend-1]
	ruled := m.judged(awayRef, awayDecides, map[string]string{"ticket": ticket.Title, "question": q.TLDR + "\n" + q.ELI5, "option": option, "why": q.Why}, nil)
	read := ruled.answers[awayDecides].Noul
	held := ""
	switch {
	case ruled.unread != "":
		held = fmt.Sprintf("Jev could not say whether recommended option %d can be undone, so the person decides it: %s", q.Recommend, ruled.unread)
	case read >= ruled.threshold:
		held = fmt.Sprintf("Jev read recommended option %d, %s, as one that cannot be undone (%s %.2f, held at %.2f), so the person decides it", q.Recommend, option, awayDecides, read, ruled.threshold)
	}
	if held != "" {
		held = "held while the person was away: " + held
		return m.Store.Edit(id, func(t *Ticket) error {
			if at := slices.IndexFunc(t.Questions, func(left Question) bool { return left.ID == q.ID }); at >= 0 {
				t.Questions[at].Away = held
			}
			t.Log = strings.TrimSpace(t.Log + "\n" + m.Store.logLine(actor, fmt.Sprintf("Q%d %s", q.ID, held)))
			return nil
		})
	}
	away := fmt.Sprintf("decided while the person was away, on the recommended option because %s, and Jev read it as one that can be undone (%s %.2f, below %.2f)", strings.TrimSuffix(q.Why, "."), awayDecides, read, ruled.threshold)
	if _, err := m.apply(OpenQuestion{Ticket: id, Question: q, Answer: strconv.Itoa(q.Recommend)}, actor, away); err != nil {
		return Ticket{}, err
	}
	return m.Store.Get(id)
}

func (m Managed) NotPossibleOpen(ticket Ticket) (Question, Event, bool, error) {
	events, err := m.Store.ticketEvents(ticket.ID)
	open := ticket.OpenQuestions()
	for _, event := range slices.Backward(events) {
		if at := slices.IndexFunc(open, func(q Question) bool { return q.ID == event.Question }); event.Kind == NotPossible && at >= 0 {
			return open[at], event, true, err
		}
	}
	return Question{}, Event{}, false, err
}

func (m Managed) heldForThePerson(ticket Ticket, actor, doing string) error {
	if ticket.Status != NeedsDecision || m.Person != "" && actor == m.Person {
		return nil
	}
	q, said, held, err := m.NotPossibleOpen(ticket)
	if err != nil || !held {
		return err
	}
	return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("%s reported it not possible (%s), so Q%d %q is the person's to answer, and only the person %s until then. it is already put to them: they answer it in the form, in catch up, in the desk or under it in %s, which tofu boardy catchup --answers applies. do not ask it again", cmp.Or(said.Agent, said.Actor), said.Text, q.ID, q.TLDR, doing, CatchupFileName)}
}

func decidable(ticket Ticket) error {
	open := ticket.OpenQuestions()
	if len(open) == 0 {
		return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it has no open question, and a ticket reaches %s only with one carrying a tldr, an eli5, options and a recommendation: tofu boardy ask %s", NeedsDecision, ticket.ID)}
	}
	for _, q := range open {
		if err := q.complete(); err != nil {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("Q%d is not ready to ask: %v", q.ID, err)}
		}
	}
	return nil
}
