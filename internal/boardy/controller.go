package boardy

import (
	"fmt"
	"strings"
	"time"
)

type Management string

const (
	ManagementOff    Management = "off"
	ManagementBoardy Management = "boardy"
)

type Controller interface {
	List(board string) ([]Ticket, error)
	Get(id string) (Ticket, error)
	Create(board string, ticket Ticket, actor string) (Ticket, error)
	Move(id string, to Status, reason, actor string) (Ticket, error)
	Comment(id, text, actor string) (Ticket, error)
	Watch(board string, fromLine int) ([]Event, error)
}

type Local struct{ Store Store }

func (l Local) List(board string) ([]Ticket, error) { return l.Store.Tickets(board) }

func (l Local) Get(id string) (Ticket, error) { return l.Store.Get(id) }

func (l Local) Watch(board string, fromLine int) ([]Event, error) {
	return l.Store.Events(board, fromLine)
}

func (l Local) Create(board string, ticket Ticket, actor string) (Ticket, error) {
	created, err := l.Store.Create(board, ticket)
	if err != nil {
		return created, err
	}
	return created, l.Store.Record(board, Event{Ticket: created.ID, Kind: Created, Actor: actor, To: created.Status, Text: created.Title})
}

func (l Local) Move(id string, to Status, reason, actor string) (Ticket, error) {
	if to == Blocked && reason == "" {
		return Ticket{}, fmt.Errorf("a move to blocked needs a reason")
	}
	var from Status
	moved, err := l.Store.Edit(id, func(ticket *Ticket) error {
		from = ticket.Status
		if from == to {
			return fmt.Errorf("%s is already %s", id, to)
		}
		ticket.Status, ticket.Reason = to, reason
		if to == Doing && from == Review && len(ticket.Acceptance) > 0 {
			ticket.Acceptance = append(ticket.Acceptance, ticket.Acceptance[len(ticket.Acceptance)-1:]...)
		}
		return nil
	})
	if err != nil {
		return moved, err
	}
	return moved, l.Store.Record(moved.Board(), Event{Ticket: id, Kind: Moved, Actor: actor, From: from, To: to, Text: reason})
}

func (l Local) Comment(id, text, actor string) (Ticket, error) {
	if text == "" {
		return Ticket{}, fmt.Errorf("a log line needs text")
	}
	who := ""
	if actor != "" {
		who = actor + ": "
	}
	logged, err := l.Store.Edit(id, func(ticket *Ticket) error {
		ticket.Log = strings.TrimSpace(ticket.Log + "\n- " + time.Now().Format(time.DateOnly) + " " + who + text)
		return nil
	})
	if err != nil {
		return logged, err
	}
	return logged, l.Store.Record(logged.Board(), Event{Ticket: id, Kind: Commented, Actor: actor, Text: text})
}
