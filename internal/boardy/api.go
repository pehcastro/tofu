package boardy

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"tofu/internal/safe"
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

type InitRequest struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (a API) Init(request InitRequest) (BoardsAnswer, error) {
	if _, err := a.Board.Store.Init(request.Key, request.Name); err != nil {
		return BoardsAnswer{}, err
	}
	if err := a.Board.Store.WriteStarterHooks(request.Key); err != nil {
		return BoardsAnswer{}, err
	}
	return a.Boards()
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

type Row struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Type      Kind      `json:"type"`
	Status    Status    `json:"status"`
	Reason    string    `json:"reason,omitempty"`
	Priority  Priority  `json:"priority"`
	Points    int       `json:"points,omitempty"`
	Epic      string    `json:"epic,omitempty"`
	Sprint    string    `json:"sprint,omitempty"`
	Assignee  string    `json:"assignee,omitempty"`
	Model     string    `json:"model,omitempty"`
	Tools     int       `json:"tools,omitempty"`
	Reads     int       `json:"reads,omitempty"`
	Labels    []string  `json:"labels,omitempty"`
	Questions int       `json:"questions,omitempty"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
}

type RowsAnswer struct {
	Board  string `json:"board"`
	Sprint string `json:"sprint,omitempty"`
	Rows   []Row  `json:"rows"`
	Words  Words  `json:"words"`
}

func (a API) Rows(request ListRequest) (RowsAnswer, error) {
	listed, err := a.List(request)
	runs, runsErr := a.Board.Store.runs(listed.Board, listed.Groups)
	answer := RowsAnswer{Board: listed.Board, Sprint: listed.Sprint, Rows: []Row{}, Words: a.Words}
	for _, group := range listed.Groups {
		for _, t := range group.Tickets {
			worked := runs[t.ID]
			answer.Rows = append(answer.Rows, Row{ID: t.ID, Title: t.Title, Type: t.Type, Status: t.Status, Reason: t.Reason, Priority: t.Priority, Points: t.Points,
				Epic: t.Epic, Sprint: t.Sprint, Assignee: t.Assignee, Model: worked.model, Tools: worked.tools, Reads: worked.reads, Labels: t.Labels,
				Questions: len(t.Questions), Created: t.Created, Updated: t.Updated})
		}
	}
	return answer, errors.Join(err, runsErr)
}

type PlanRequest struct {
	Board string `json:"board"`
}

type EpicState string

const (
	EpicOpen EpicState = "open"
	EpicDone EpicState = "done"
)

func (EpicState) Enum() []string { return enum(EpicOpen, EpicDone) }

type PlannedEpic struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    EpicState `json:"status"`
	Milestone string    `json:"milestone,omitempty"`
}

type PlannedMilestone struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Due   Day    `json:"due,omitempty"`
}

type PlannedSprint struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
	Start  Day    `json:"start,omitempty"`
	End    Day    `json:"end,omitempty"`
}

type PlanAnswer struct {
	Epics      []PlannedEpic      `json:"epics"`
	Milestones []PlannedMilestone `json:"milestones"`
	Sprints    []PlannedSprint    `json:"sprints"`
	Views      []string           `json:"views"`
	Words      Words              `json:"words"`
}

func (a API) Plan(request PlanRequest) (PlanAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return PlanAnswer{}, err
	}
	store := a.Board.Store
	epics, epicsErr := store.Epics(key)
	milestones, milestonesErr := store.Milestones(key)
	sprints, sprintsErr := store.Sprints(key)
	views, viewsErr := store.Views(key)
	plan := PlanAnswer{Epics: []PlannedEpic{}, Milestones: []PlannedMilestone{}, Sprints: []PlannedSprint{}, Views: []string{}, Words: a.Words}
	for _, epic := range epics {
		state := EpicOpen
		if epic.Done {
			state = EpicDone
		}
		plan.Epics = append(plan.Epics, PlannedEpic{ID: epic.ID, Title: epic.Title, Status: state, Milestone: epic.Milestone})
	}
	for _, milestone := range milestones {
		plan.Milestones = append(plan.Milestones, PlannedMilestone{ID: milestone.ID, Title: milestone.Title, Due: milestone.Due})
	}
	for _, sprint := range sprints {
		plan.Sprints = append(plan.Sprints, PlannedSprint{ID: sprint.ID, Title: sprint.Title, Active: sprint.State == Active, Start: sprint.Start, End: sprint.End})
	}
	for _, view := range views {
		plan.Views = append(plan.Views, view.Name)
	}
	return plan, errors.Join(epicsErr, milestonesErr, sprintsErr, viewsErr)
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
	var events []Event
	var eventsErr error
	var reading sync.WaitGroup
	reading.Go(func() {
		eventsErr = safe.Call(store.crashed, "boardy report events", func() (err error) {
			events, err = store.Events(key, 0)
			return err
		})
	})
	tickets, ticketsErr := store.Tickets(key)
	reading.Wait()
	epics, epicsErr := store.Epics(key)
	sprints, sprintsErr := store.Sprints(key)
	return ReportAnswer{Report: BuildReport(key, request.Since, time.Now(), tickets, events, epics, sprints), Words: a.Words}, errors.Join(ticketsErr, eventsErr, epicsErr, sprintsErr)
}

type QuestionsRequest struct {
	Board string `json:"board,omitempty"`
}

type AskedQuestion struct {
	Ticket      string   `json:"ticket"`
	ID          int      `json:"id"`
	TLDR        string   `json:"tldr"`
	ELI5        string   `json:"eli5"`
	Options     []string `json:"options"`
	Recommended int      `json:"recommended"`
	Answer      string   `json:"answer,omitempty"`
}

type QuestionsAnswer struct {
	Questions []AskedQuestion `json:"questions"`
	Words     Words           `json:"words"`
}

func (a API) Questions(request QuestionsRequest) (QuestionsAnswer, error) {
	boards := []Board{{Key: request.Board}}
	if request.Board == "" {
		var err error
		if boards, err = a.Board.Store.Boards(); err != nil {
			return QuestionsAnswer{}, err
		}
	}
	answer := QuestionsAnswer{Questions: []AskedQuestion{}, Words: a.Words}
	for _, board := range boards {
		listed, err := a.Board.OpenQuestions(board.Key)
		if err != nil {
			return answer, err
		}
		for _, open := range listed.Open {
			q := open.Question
			answer.Questions = append(answer.Questions, AskedQuestion{Ticket: open.Ticket, ID: q.ID, TLDR: q.TLDR, ELI5: q.ELI5, Options: q.Options, Recommended: q.Recommend, Answer: open.Answer})
		}
	}
	return answer, nil
}

type AnswerRequest struct {
	Ticket string `json:"ticket"`
	ID     int    `json:"id"`
	Answer string `json:"answer"`
}

func (a API) Answer(request AnswerRequest) (TicketAnswer, error) {
	return a.named(a.Board.Answer(request.Ticket, request.ID, request.Answer, a.Actor))
}

type GetRequest struct {
	ID string `json:"id"`
}

type GetAnswer struct {
	Ticket Ticket  `json:"ticket"`
	Actual Actual  `json:"actual"`
	Events []Event `json:"events"`
	Words  Words   `json:"words"`
}

func (a API) Get(request GetRequest) (GetAnswer, error) {
	ticket, err := a.Board.Store.Get(request.ID)
	if err != nil {
		return GetAnswer{}, err
	}
	events, err := a.Board.Store.ticketEvents(ticket.ID)
	return GetAnswer{Ticket: ticket, Actual: Actuals(events, time.Now())[ticket.ID], Events: threaded(events), Words: a.Words}, err
}

type EventsRequest struct {
	Board string `json:"board,omitempty"`
	Since int    `json:"since,omitempty"`
}

type EventsAnswer struct {
	Board  string  `json:"board"`
	Events []Event `json:"events"`
	Next   int     `json:"next"`
	Words  Words   `json:"words"`
}

func (a API) Events(request EventsRequest) (EventsAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return EventsAnswer{}, err
	}
	events, err := a.Board.Store.Events(key, request.Since)
	return EventsAnswer{Board: key, Events: append([]Event{}, events...), Next: request.Since + len(events), Words: a.Words}, err
}

type CreateRequest struct {
	Board  string `json:"board,omitempty"`
	Ticket Ticket `json:"ticket"`
}

type TicketAnswer struct {
	Ticket Ticket `json:"ticket"`
	Words  Words  `json:"words"`
}

func (a API) named(ticket Ticket, err error) (TicketAnswer, error) {
	return TicketAnswer{Ticket: ticket, Words: a.Words}, err
}

func (a API) Create(request CreateRequest) (TicketAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return TicketAnswer{}, err
	}
	return a.named(a.Board.Create(key, request.Ticket, a.Actor))
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
	return a.named(a.Board.Move(request.ID, request.To, request.Reason, a.Actor))
}

type LogRequest struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func (a API) Log(request LogRequest) (TicketAnswer, error) {
	return a.named(a.Board.Comment(request.ID, request.Text, a.Actor))
}

type AssignRequest struct {
	ID       string `json:"id"`
	Assignee string `json:"assignee"`
}

func (a API) Assign(request AssignRequest) (TicketAnswer, error) {
	return a.named(a.Board.Assign(request.ID, request.Assignee, a.Actor))
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
	return a.named(a.Board.Hand(request.ID, request.To, to, a.Actor))
}

type TriageAction string

const (
	TriageList   TriageAction = "list"
	TriageAccept TriageAction = "accept"
	TriageDrop   TriageAction = "drop"
)

func (TriageAction) Enum() []string { return enum(TriageList, TriageAccept, TriageDrop) }

type TriageRequest struct {
	Board   string       `json:"board,omitempty"`
	Action  TriageAction `json:"action"`
	Request string       `json:"request,omitempty"`
	Reason  string       `json:"reason,omitempty"`
	Epic    string       `json:"epic,omitempty"`
	Sprint  string       `json:"sprint,omitempty"`
}

type TriageAnswer struct {
	Board    string    `json:"board"`
	Requests []Request `json:"requests"`
	Accepted *Ticket   `json:"accepted,omitempty"`
	Words    Words     `json:"words"`
}

func (a API) Triage(request TriageRequest) (TriageAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return TriageAnswer{}, err
	}
	answer := TriageAnswer{Board: key, Words: a.Words}
	switch request.Action {
	case TriageList:
	case TriageAccept:
		var ticket Ticket
		ticket, err = a.Board.AcceptPlanned(key, request.Request, request.Epic, request.Sprint, a.Actor)
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
