package boardy

import (
	"errors"
	"fmt"
	"time"
)

type API struct {
	Board Managed
	Actor string
	Words Words
}

type BoardSummary struct {
	Board
	Tickets int `json:"tickets"`
	Live    int `json:"live"`
}

type BoardsAnswer struct {
	Boards []BoardSummary `json:"boards"`
	Words  Words          `json:"words"`
}

func (a API) Boards() (BoardsAnswer, error) {
	boards, err := a.Board.Store.Boards()
	answer := BoardsAnswer{Boards: []BoardSummary{}, Words: a.Words}
	for _, board := range boards {
		tickets, ticketsErr := a.Board.Store.Tickets(board.Key)
		err = errors.Join(err, ticketsErr)
		summary := BoardSummary{Board: board, Tickets: len(tickets)}
		for _, ticket := range tickets {
			if ticket.Status.Live() {
				summary.Live++
			}
		}
		answer.Boards = append(answer.Boards, summary)
	}
	return answer, err
}

type ListRequest struct {
	Board  string `json:"board,omitempty"`
	View   string `json:"view,omitempty"`
	Filter Filter `json:"filter,omitzero"`
}

type ListAnswer struct {
	Board  string  `json:"board"`
	View   View    `json:"view"`
	Sprint string  `json:"sprint,omitempty"`
	Groups []Group `json:"groups"`
	Words  Words   `json:"words"`
}

func (a API) List(request ListRequest) (ListAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return ListAnswer{}, err
	}
	view, err := a.Board.Store.View(key, request.View)
	if err != nil {
		return ListAnswer{}, err
	}
	tickets, err := a.Board.Store.Tickets(key)
	if err != nil {
		return ListAnswer{}, err
	}
	sprints, err := a.Board.Store.Sprints(key)
	active, _ := ActiveSprint(sprints)
	groups := Apply(view, request.Filter, tickets, Resolved{Sprint: active.ID, Me: a.Actor})
	return ListAnswer{Board: key, View: view, Sprint: active.ID, Groups: groups, Words: a.Words}, err
}

type ReportRequest struct {
	Board string    `json:"board,omitempty"`
	Since time.Time `json:"since,omitzero"`
}

type ReportAnswer struct {
	Report
	Words Words `json:"words"`
}

func (a API) Report(request ReportRequest) (ReportAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return ReportAnswer{}, err
	}
	store := a.Board.Store
	tickets, ticketsErr := store.Tickets(key)
	events, eventsErr := store.Events(key, 0)
	epics, epicsErr := store.Epics(key)
	sprints, sprintsErr := store.Sprints(key)
	return ReportAnswer{Report: BuildReport(key, request.Since, time.Now(), tickets, events, epics, sprints), Words: a.Words}, errors.Join(ticketsErr, eventsErr, epicsErr, sprintsErr)
}

type GetRequest struct {
	ID string `json:"id"`
}

type GetAnswer struct {
	Ticket Ticket  `json:"ticket"`
	Actual Actual  `json:"actual"`
	Events []Event `json:"events"`
}

func (a API) Get(request GetRequest) (GetAnswer, error) {
	ticket, err := a.Board.Store.Get(request.ID)
	if err != nil {
		return GetAnswer{}, err
	}
	all, err := a.Board.Store.Events(ticket.Board(), 0)
	answer := GetAnswer{Ticket: ticket, Actual: Actuals(all, time.Now())[ticket.ID], Events: []Event{}}
	for _, event := range all {
		if event.Ticket == ticket.ID {
			answer.Events = append(answer.Events, event)
		}
	}
	return answer, err
}

type EventsRequest struct {
	Board string `json:"board,omitempty"`
	Since int    `json:"since,omitempty"`
}

type EventsAnswer struct {
	Board  string  `json:"board"`
	Events []Event `json:"events"`
	Next   int     `json:"next"`
}

func (a API) Events(request EventsRequest) (EventsAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return EventsAnswer{}, err
	}
	events, err := a.Board.Store.Events(key, request.Since)
	return EventsAnswer{Board: key, Events: append([]Event{}, events...), Next: request.Since + len(events)}, err
}

type CreateRequest struct {
	Board  string `json:"board,omitempty"`
	Ticket Ticket `json:"ticket"`
}

type TicketAnswer struct {
	Ticket Ticket `json:"ticket"`
}

func (a API) Create(request CreateRequest) (TicketAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return TicketAnswer{}, err
	}
	ticket, err := a.Board.Create(key, request.Ticket, a.Actor)
	return TicketAnswer{Ticket: ticket}, err
}

type MoveRequest struct {
	ID     string `json:"id"`
	To     Status `json:"to"`
	Reason string `json:"reason,omitempty"`
}

func (a API) Move(request MoveRequest) (TicketAnswer, error) {
	if _, err := ParseStatus(string(request.To)); err != nil {
		return TicketAnswer{}, err
	}
	ticket, err := a.Board.Move(request.ID, request.To, request.Reason, a.Actor)
	return TicketAnswer{Ticket: ticket}, err
}

type LogRequest struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func (a API) Log(request LogRequest) (TicketAnswer, error) {
	ticket, err := a.Board.Comment(request.ID, request.Text, a.Actor)
	return TicketAnswer{Ticket: ticket}, err
}

type AssignRequest struct {
	ID       string `json:"id"`
	Assignee string `json:"assignee"`
}

func (a API) Assign(request AssignRequest) (TicketAnswer, error) {
	ticket, err := a.Board.Assign(request.ID, request.Assignee, a.Actor)
	return TicketAnswer{Ticket: ticket}, err
}

type HandRequest struct {
	ID      string `json:"id"`
	To      string `json:"to"`
	Project string `json:"project,omitempty"`
}

func (a API) Hand(request HandRequest) (TicketAnswer, error) {
	to := a.Board.Store
	if request.Project != "" {
		var err error
		if to, err = OpenStore(request.Project); err != nil {
			return TicketAnswer{}, err
		}
	}
	ticket, err := a.Board.Hand(request.ID, request.To, to, a.Actor)
	return TicketAnswer{Ticket: ticket}, err
}

type TriageAction string

const (
	TriageList   TriageAction = "list"
	TriageAccept TriageAction = "accept"
	TriageDrop   TriageAction = "drop"
)

type TriageRequest struct {
	Board   string       `json:"board,omitempty"`
	Action  TriageAction `json:"action"`
	Request string       `json:"request,omitempty"`
	Reason  string       `json:"reason,omitempty"`
}

type TriageAnswer struct {
	Board    string    `json:"board"`
	Requests []Request `json:"requests"`
	Accepted *Ticket   `json:"accepted,omitempty"`
}

func (a API) Triage(request TriageRequest) (TriageAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return TriageAnswer{}, err
	}
	answer := TriageAnswer{Board: key}
	switch request.Action {
	case TriageList:
	case TriageAccept:
		var ticket Ticket
		ticket, err = a.Board.Accept(key, request.Request, a.Actor)
		answer.Accepted = &ticket
	case TriageDrop:
		err = a.Board.Drop(key, request.Request, request.Reason, a.Actor)
	default:
		return answer, fmt.Errorf("triage action %q is not list, accept or drop", request.Action)
	}
	if err != nil {
		return answer, err
	}
	requests, err := a.Board.Store.Requests(key)
	answer.Requests = append([]Request{}, requests...)
	return answer, err
}

func (a API) boardKey(key string) (string, error) {
	if key != "" {
		return key, nil
	}
	boards, err := a.Board.Store.Boards()
	if err != nil {
		return "", err
	}
	if len(boards) != 1 {
		return "", fmt.Errorf("this project has %d boards, so name one", len(boards))
	}
	return boards[0].Key, nil
}
