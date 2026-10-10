package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"tofu/internal/rule"
	"tofu/internal/subagent"
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
	if err := m.referencesResolve(ticket); err != nil {
		return Ticket{}, err
	}
	if err := m.ownsFree(ticket.ID, ticket.Owns, replaces); err != nil {
		return Ticket{}, err
	}
	preview, err := os.CreateTemp(m.Store.BoardDir(key), ".tofu-create-*.md")
	if err != nil {
		return Ticket{}, err
	}
	_, err = preview.Write(ticket.Markdown())
	err = errors.Join(err, preview.Close())
	if err == nil {
		err = m.gate(key, rule.BoardEvent{Event: rule.EventCreate, Path: preview.Name(), To: string(cmp.Or(ticket.Status, Backlog)), Actor: actor, ByPerson: m.isPerson(actor)})
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
	if err := m.mayMove(m.role(board, actor), ticket, to, actor); err != nil {
		return ticket, err
	}
	if err := m.validMove(ticket, to); err != nil {
		return ticket, err
	}
	event := rule.EventMove
	if !to.Live() {
		event = rule.EventClose
	}
	if err := m.gate(board.Key, m.boardEvent(event, ticket, to, actor)); err != nil {
		return ticket, err
	}
	return m.Local.Move(id, to, reason, actor)
}

func (m Managed) Comment(id, text, actor string) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	if m.role(board, actor) == RoleAgent && ticket.Assignee != actor {
		return ticket, RefusedError{Ticket: id, Why: fmt.Sprintf("it is assigned to %q, and an agent appends only to its own ticket's Log", ticket.Assignee)}
	}
	return m.Local.Comment(id, text, actor)
}

func (m Managed) Widen(id string, owns []string, actor string) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	if err := m.Manages(board.Key, actor, "widen owns"); err != nil {
		return ticket, err
	}
	added := slices.DeleteFunc(slices.Clone(owns), func(glob string) bool { return slices.Contains(ticket.Owns, glob) })
	if len(added) == 0 {
		return ticket, fmt.Errorf("%s already owns %s", id, strings.Join(owns, ", "))
	}
	if err := m.ownsFree(id, added, ""); err != nil {
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
	return rule.BoardEvent{Event: event, Ticket: ticket.ID, Path: path, From: string(ticket.Status), To: string(to), Actor: actor, ByPerson: m.isPerson(actor)}
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

func (m Managed) validMove(ticket Ticket, to Status) error {
	if slices.Contains([]Status{Doing, Review, Done}, to) && !slices.ContainsFunc(ticket.Acceptance, func(lines string) bool { return strings.TrimSpace(lines) != "" }) {
		return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it has no Acceptance, and a ticket reaches %s only with one", to)}
	}
	if to == Review && strings.TrimSpace(ticket.Log) == "" {
		return RefusedError{Ticket: ticket.ID, Why: "its Log has no evidence, and a ticket reaches review with its evidence in the Log"}
	}
	if to == Doing || (!ticket.Status.Live() && to.Live()) {
		return errors.Join(m.referencesResolve(ticket), m.ownsFree(ticket.ID, ticket.Owns, ""))
	}
	return nil
}

func (m Managed) referencesResolve(ticket Ticket) error {
	for _, id := range slices.Concat(ticket.Depends, ticket.Blocks, ticket.Relates, ticket.Duplicates) {
		if _, err := m.Store.Get(id); err != nil {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it names %s, which does not resolve: %v", id, err)}
		}
	}
	return nil
}

func (m Managed) ownsFree(id string, owns []string, replaces string) error {
	if len(owns) == 0 {
		return nil
	}
	boards, err := m.Store.Boards()
	if err != nil {
		return err
	}
	for _, board := range boards {
		tickets, err := m.Store.Tickets(board.Key)
		if err != nil {
			return err
		}
		for _, held := range tickets {
			if held.ID == id || held.ID == replaces || !held.Status.Live() || len(held.Owns) == 0 {
				continue
			}
			var roster subagent.Roster
			var collision subagent.CollisionError
			if err := roster.Hold(subagent.SubAgent{ID: held.ID, Owns: held.Owns}); err != nil {
				return err
			}
			if errors.As(roster.Hold(subagent.SubAgent{ID: id, Owns: owns}), &collision) {
				return RefusedError{Ticket: id, Why: fmt.Sprintf("%s overlaps %s, owned by %s (%s)", collision.Glob, collision.HolderGlob, held.ID, held.Status)}
			}
		}
	}
	return nil
}

func logLine(actor, text string) string {
	return "- " + time.Now().Format(time.DateOnly) + " " + actor + ": " + text
}
