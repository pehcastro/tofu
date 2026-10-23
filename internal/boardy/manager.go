package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/rule"
)

func (m Managed) AddManager(key, name, actor string) (Board, error) {
	return m.changeManagers(key, name, actor, func(board *Board) error {
		if slices.Contains(board.Managers, name) {
			return fmt.Errorf("%s already manages %s", name, key)
		}
		board.Managers = append(board.Managers, name)
		return nil
	})
}

func (m Managed) RemoveManager(key, name, actor string) (Board, error) {
	return m.changeManagers(key, name, actor, func(board *Board) error {
		at := slices.Index(board.Managers, name)
		if at < 0 {
			return fmt.Errorf("%s does not manage %s", name, key)
		}
		board.Managers = slices.Delete(board.Managers, at, at+1)
		return nil
	})
}

func (m Managed) Assign(id, assignee, actor string) (Ticket, error) {
	ref, err := SplitID(id)
	if err != nil {
		return Ticket{}, err
	}
	if err := m.Manages(ref.Key, actor, "assign a ticket"); err != nil {
		return Ticket{}, err
	}
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	return m.assign(board, ticket, assignee, actor, 0)
}

func (m Managed) assign(board Board, ticket Ticket, assignee, actor string, depth int) (Ticket, error) {
	s := m.subjectOf(rule.HookTicketAssign, board.Key, ticket, ticket.Status, actor, depth)
	if err := m.gate(&s); err != nil {
		return ticket, err
	}
	assigned, err := m.Store.Edit(ticket.ID, func(t *Ticket) error {
		t.Assignee = assignee
		return nil
	})
	if err == nil {
		err = m.Store.Record(board.Key, Event{Ticket: ticket.ID, Kind: Assigned, Actor: actor, Text: assignee})
	}
	if err != nil {
		return assigned, err
	}
	s.ticket = assigned
	return assigned, m.after(s)
}

func (m Managed) Plan(id, epic, sprint, actor string) (Ticket, error) {
	ref, err := SplitID(id)
	if err != nil {
		return Ticket{}, err
	}
	if err := m.Manages(ref.Key, actor, "set a ticket's epic or sprint"); err != nil {
		return Ticket{}, err
	}
	if err := m.Store.planned(ref.Key, Front{Epic: epic, Sprint: sprint}); err != nil {
		return Ticket{}, err
	}
	var said []string
	for _, field := range [][2]string{{"epic", epic}, {"sprint", sprint}} {
		if field[1] != "" {
			said = append(said, field[0]+" "+field[1])
		}
	}
	if said == nil {
		return Ticket{}, fmt.Errorf("name an epic, a sprint, or both")
	}
	text := "set " + strings.Join(said, ", ")
	planned, err := m.Store.Edit(id, func(t *Ticket) error {
		t.Epic, t.Sprint = cmp.Or(epic, t.Epic), cmp.Or(sprint, t.Sprint)
		t.Log = strings.TrimSpace(t.Log + "\n" + m.Store.logLine(actor, text))
		return nil
	})
	if err != nil {
		return planned, err
	}
	return planned, m.Store.Record(ref.Key, Event{Ticket: id, Kind: Commented, Actor: actor, Text: text})
}

func (s Store) planned(key string, front Front) error {
	epics, epicsErr := s.Epics(key)
	sprints, sprintsErr := s.Sprints(key)
	switch {
	case front.Epic != "" && !slices.ContainsFunc(epics, func(e Epic) bool { return e.ID == front.Epic }):
		return errors.Join(epicsErr, RefusedError{Ticket: front.ID, Why: fmt.Sprintf("%s has no epic %s: tofu boardy epic new %s <title>", key, front.Epic, front.Epic)})
	case front.Sprint != "" && !slices.ContainsFunc(sprints, func(one Sprint) bool { return one.ID == front.Sprint }):
		return errors.Join(sprintsErr, RefusedError{Ticket: front.ID, Why: fmt.Sprintf("%s has no sprint %s: tofu boardy sprint new %s <title>", key, front.Sprint, front.Sprint)})
	}
	return nil
}

func (m Managed) changeManagers(key, name, actor string, change func(*Board) error) (Board, error) {
	if name == "" {
		return Board{}, fmt.Errorf("a manager is a session id or the person's name")
	}
	var board Board
	err := m.Store.locked(key, "board", func() error {
		var err error
		if board, err = m.Store.Board(key); err != nil {
			return err
		}
		if !m.actsForPerson(actor) && m.role(board, actor) != RoleManager {
			return RefusedError{Why: fmt.Sprintf("%s is neither %s nor a manager of %s, so it cannot change who manages it", actor, RolePerson, key)}
		}
		if err := change(&board); err != nil {
			return err
		}
		return writeAtomic(filepath.Join(board.Dir, BoardFileName), board.toml())
	})
	if err != nil {
		return board, err
	}
	return board, m.Store.Record(key, Event{Kind: ManagersChanged, Actor: actor, Text: fmt.Sprintf("managers: %v", board.Managers)})
}
