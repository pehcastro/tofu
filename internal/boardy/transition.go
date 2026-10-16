package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"tofu/internal/rule"
)

func (m Managed) Create(key string, ticket Ticket, actor string) (Ticket, error) {
	board, err := m.Store.Board(key)
	if err != nil {
		return Ticket{}, err
	}
	if m.role(board, actor) != RoleManager {
		return m.request(key, ticket, actor)
	}
	return m.create(key, ticket, actor, "")
}

func (m Managed) create(key string, ticket Ticket, actor, replaces string) (Ticket, error) {
	board, err := m.Store.Board(key)
	if err != nil {
		return Ticket{}, err
	}
	if err := m.vetCreate(board, ticket, cmp.Or(ticket.Status, Backlog), replaces); err != nil {
		return Ticket{}, err
	}
	preview, err := os.CreateTemp(m.Store.BoardDir(key), ".tofu-create-*.md")
	if err != nil {
		return Ticket{}, err
	}
	_, err = preview.Write(ticket.Markdown())
	err = errors.Join(err, preview.Close())
	if err == nil {
		err = m.gate(key, rule.BoardEvent{Event: rule.EventCreate, Path: preview.Name(), To: string(cmp.Or(ticket.Status, Backlog)), Actor: actor, ByPerson: m.actsForPerson(actor)})
	}
	if err = errors.Join(err, os.Remove(preview.Name())); err != nil {
		return Ticket{}, err
	}
	return m.Local.Create(key, ticket, actor)
}

func (m Managed) Move(id string, to Status, reason, actor string) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	e := m.moveEvent(ticket, to, actor)
	fired, err := m.vetMove(board, ticket, e)
	if err = errors.Join(m.Store.recordFired(board.Key, e, fired), err); err != nil {
		return ticket, err
	}
	return m.Local.Move(id, to, reason, actor)
}

func (m Managed) Check(event rule.Event, id string, to Status, actor string) ([]Fired, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return nil, err
	}
	to = cmp.Or(to, ticket.Status)
	switch event {
	case rule.EventMove, rule.EventClose:
		return m.vetMove(board, ticket, m.moveEvent(ticket, to, actor))
	case rule.EventCreate:
		err = m.vetCreate(board, ticket, to, "")
	case rule.EventWiden:
		err = m.mayWiden(board, ticket, actor)
	default:
		return nil, fmt.Errorf("%q is not a board event", event)
	}
	if err != nil {
		return nil, err
	}
	return m.Store.fire(board.Key, m.boardEvent(event, ticket, to, actor))
}

func (m Managed) moveEvent(ticket Ticket, to Status, actor string) rule.BoardEvent {
	if !to.Live() {
		return m.boardEvent(rule.EventClose, ticket, to, actor)
	}
	return m.boardEvent(rule.EventMove, ticket, to, actor)
}

func (m Managed) vetMove(board Board, ticket Ticket, e rule.BoardEvent) ([]Fired, error) {
	if err := m.mayMove(m.role(board, e.Actor), ticket, Status(e.To), e.Actor); err != nil {
		return nil, err
	}
	if err := m.validMove(board, ticket, Status(e.To)); err != nil {
		return nil, err
	}
	return m.Store.fire(board.Key, e)
}

func (m Managed) vetCreate(board Board, ticket Ticket, status Status, replaces string) error {
	if err := m.referencesResolve(ticket); err != nil {
		return err
	}
	if err := board.cited(ticket); err != nil {
		return err
	}
	return m.ownsFree(board.Key, ticket.Front, ticket.Owns, status, replaces)
}

func (m Managed) mayWiden(board Board, ticket Ticket, actor string) error {
	if err := m.Manages(board.Key, actor, "widen owns"); err != nil {
		return err
	}
	return board.cited(ticket)
}

func (m Managed) Comment(id, text, actor string) (Ticket, error) {
	return m.appendLog(id, text, actor, Event{Kind: Commented, Text: text})
}

func (m Managed) AskInLog(id, question, actor string) (Ticket, error) {
	return m.appendLog(id, "question for a manager: "+question, actor, Event{Kind: Asked, Text: question})
}

func (m Managed) appendLog(id, text, actor string, event Event) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	if m.role(board, actor) == RoleAgent && ticket.Assignee != actor {
		return ticket, RefusedError{Ticket: id, Why: fmt.Sprintf("it is assigned to %q, and an agent appends only to its own ticket's Log", ticket.Assignee)}
	}
	return m.Local.appendLog(id, text, actor, event)
}

func (m Managed) Widen(id string, owns []string, actor string) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	if err := m.mayWiden(board, ticket, actor); err != nil {
		return ticket, err
	}
	added := slices.DeleteFunc(slices.Clone(owns), func(glob string) bool { return slices.Contains(ticket.Owns, glob) })
	if len(added) == 0 {
		return ticket, fmt.Errorf("%s already owns %s", id, strings.Join(owns, ", "))
	}
	if err := m.ownsFree(board.Key, ticket.Front, added, ticket.Status, ""); err != nil {
		return ticket, err
	}
	if err := m.gate(board.Key, m.boardEvent(rule.EventWiden, ticket, ticket.Status, actor)); err != nil {
		return ticket, err
	}
	widened, err := m.Store.Edit(id, func(t *Ticket) error {
		t.Owns = append(t.Owns, added...)
		return nil
	})
	if err != nil {
		return widened, err
	}
	return widened, m.Store.Record(board.Key, Event{Ticket: id, Kind: Widened, Actor: actor, Text: strings.Join(added, ", ")})
}

func (m Managed) ticketOnBoard(id string) (Ticket, Board, error) {
	ticket, err := m.Store.Get(id)
	if err != nil {
		return ticket, Board{}, err
	}
	board, err := m.Store.Board(ticket.Board())
	return ticket, board, err
}

func (m Managed) boardEvent(event rule.Event, ticket Ticket, to Status, actor string) rule.BoardEvent {
	path, _ := m.Store.TicketPath(ticket.ID)
	return rule.BoardEvent{Event: event, Ticket: ticket.ID, Path: path, From: string(ticket.Status), To: string(to), Actor: actor, ByPerson: m.actsForPerson(actor)}
}

func (m Managed) mayMove(role Role, ticket Ticket, to Status, actor string) error {
	switch role {
	case RoleManager:
		return nil
	case RolePerson:
		if !to.Live() {
			return nil
		}
		return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("%s is not a manager of %s, and %s only closes a ticket", actor, ticket.Board(), RolePerson)}
	case RoleAgent:
		if ticket.Assignee != actor {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it is assigned to %q, and an agent moves only its own ticket", ticket.Assignee)}
		}
		if ticket.Status != Doing || to != Review {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("an agent moves its own ticket from %s to %s, and only a manager moves it from %s to %s", Doing, Review, ticket.Status, to)}
		}
		return nil
	}
	panic("boardy: unknown role " + string(role))
}

func (m Managed) validMove(board Board, ticket Ticket, to Status) error {
	if slices.Contains([]Status{Doing, Review, Done}, to) && !slices.ContainsFunc(ticket.Acceptance, func(lines string) bool { return strings.TrimSpace(lines) != "" }) {
		return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it has no Acceptance, and a ticket reaches %s only with one", to)}
	}
	if need, have := max(board.LogToReview, 1), utf8.RuneCountInString(strings.TrimSpace(ticket.Log)); to == Review && have < need {
		return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("its Log has %d characters of evidence, and a ticket reaches review on %s with at least %d", have, board.Key, need)}
	}
	if to == NeedsDecision {
		if err := decidable(ticket); err != nil {
			return err
		}
	}
	if to == Done {
		if why := DoneRefusal(m.Store.Root, ticket.Owns); why != "" {
			return RefusedError{Ticket: ticket.ID, Why: why}
		}
	}
	if to == Review || to == Done {
		children, _ := filepath.Glob(filepath.Join(board.Dir, TicketsDirName, ticket.ID, "*.md"))
		var live []string
		for _, path := range children {
			child, err := frontOf(path)
			if err != nil {
				return err
			}
			if child.Status.Live() {
				live = append(live, child.ID)
			}
		}
		if len(live) > 0 {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("its sub-tickets %s are still live, and a parent reaches %s only once they are done or dropped", strings.Join(live, ", "), to)}
		}
	}
	var err error
	if to == Doing || (!ticket.Status.Live() && to.Live()) {
		err = m.referencesResolve(ticket)
	}
	if !board.Holding(ticket.Status) {
		err = errors.Join(err, m.ownsFree(board.Key, ticket.Front, ticket.Owns, to, ""))
	}
	return err
}

func (m Managed) referencesResolve(ticket Ticket) error {
	for _, id := range slices.Concat(ticket.Depends, ticket.Blocks, ticket.Relates, ticket.Duplicates) {
		if _, err := m.Store.Get(id); err != nil {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it names %s, which does not resolve: %v", id, err)}
		}
	}
	return nil
}

func (m Managed) ownsFree(key string, ticket Front, owns []string, status Status, replaces string) error {
	id := ticket.ID
	for _, glob := range owns {
		if err := unreadableOwns(glob); err != nil {
			return RefusedError{Ticket: id, Why: err.Error()}
		}
	}
	if ticket.Parent != "" {
		parent, err := m.Store.Get(ticket.Parent)
		if err != nil {
			return err
		}
		if glob := outsideOwns(owns, parent.Owns); glob != "" {
			return RefusedError{Ticket: id, Why: fmt.Sprintf("%s is outside the owns of its parent %s (%s), and a parent's owns are the ceiling for its sub-tickets", glob, parent.ID, strings.Join(parent.Owns, ", "))}
		}
	}
	boards, err := m.Store.Boards()
	if err != nil {
		return err
	}
	if len(owns) == 0 || !slices.ContainsFunc(boards, func(b Board) bool { return b.Key == key && b.Holding(status) }) {
		return nil
	}
	for _, board := range boards {
		fronts, err := m.Store.Fronts(board.Key)
		if err != nil {
			return err
		}
		for _, held := range fronts {
			if held.ID == id || held.ID == replaces || family(ticket, held) || !board.Holding(held.Status) {
				continue
			}
			if glob, other, found := ownsMeet(owns, held.Owns); found {
				return RefusedError{Ticket: id, Why: fmt.Sprintf("%s overlaps %s, held by %s (%s)", glob, other, held.ID, held.Status)}
			}
		}
	}
	return nil
}

func logLine(actor, text string) string {
	return "- " + time.Now().Format(time.DateOnly) + " " + actor + ": " + text
}
