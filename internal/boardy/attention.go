package boardy

import (
	"cmp"
	"errors"
	"strings"
	"time"
)

type Need struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Assignee string    `json:"assignee,omitempty"`
	Since    time.Time `json:"since,omitzero"`
	Paths    []string  `json:"paths,omitempty"`
	Why      string    `json:"why,omitempty"`
}

type Attention struct {
	Board       string  `json:"board"`
	Statuses    []Tally `json:"statuses"`
	Tree        string  `json:"tree,omitempty"`
	Doing       []Need  `json:"doing"`
	Closable    []Need  `json:"closable"`
	Unstamped   []Need  `json:"unstamped"`
	Uncommitted []Need  `json:"uncommitted"`
	Refused     []Need  `json:"refused"`
	Decide      []Need  `json:"decide"`
	Triage      []Need  `json:"triage"`
}

func (m Managed) Attention(key string, now time.Time) (Attention, error) {
	board, err := m.Store.Board(key)
	if err != nil {
		return Attention{}, err
	}
	tickets, err := m.Store.Tickets(key)
	events, eventsErr := m.Store.Events(key, 0)
	requests, requestsErr := m.Store.Requests(key)
	if err = errors.Join(err, eventsErr, requestsErr); err != nil {
		return Attention{}, err
	}
	head := headGate(m.Store.Root)
	m.head = &head
	a := Attention{Board: key, Statuses: BuildReport(key, time.Time{}, now, tickets, events, nil, nil).Statuses, Tree: head.Tree}
	started := map[string]time.Time{}
	for _, event := range events {
		if event.Kind == Moved && event.To == Doing {
			started[event.Ticket] = event.At
		}
	}
	for _, ticket := range tickets {
		one := Need{ID: ticket.ID, Title: ticket.Title, Assignee: ticket.Assignee}
		switch ticket.Status {
		case Doing:
			one.Since = cmp.Or(started[ticket.ID], ticket.Updated)
			a.Doing = append(a.Doing, one)
		case Review:
			a.review(one, m.validMove(board, ticket, Done))
		case NeedsDecision:
			var asked []string
			for _, q := range ticket.OpenQuestions() {
				asked = append(asked, q.TLDR)
			}
			one.Why = cmp.Or(strings.Join(asked, "; "), "no open question")
			a.Decide = append(a.Decide, one)
		}
	}
	for _, request := range requests {
		a.Triage = append(a.Triage, Need{ID: request.ID, Title: request.Ticket.Title})
	}
	return a, nil
}

func (a *Attention) review(one Need, refusal error) {
	var refused RefusedError
	gated := errors.As(refusal, &refused) && refused.Done != nil && refused.Done.Unread == ""
	switch {
	case refusal == nil:
		a.Closable = append(a.Closable, one)
	case gated && len(refused.Done.Dirty) > 0:
		one.Paths = refused.Done.Dirty
		a.Uncommitted = append(a.Uncommitted, one)
	case gated:
		a.Unstamped = append(a.Unstamped, one)
	default:
		one.Why, _, _ = strings.Cut(refusal.Error(), "\n")
		a.Refused = append(a.Refused, one)
	}
}
