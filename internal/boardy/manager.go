package boardy

import (
	"fmt"
	"path/filepath"
	"slices"
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
	key, _, err := SplitID(id)
	if err != nil {
		return Ticket{}, err
	}
	if err := m.Manages(key, actor, "assign a ticket"); err != nil {
		return Ticket{}, err
	}
	assigned, err := m.Store.Edit(id, func(t *Ticket) error {
		t.Assignee = assignee
		return nil
	})
	if err != nil {
		return assigned, err
	}
	return assigned, m.Store.Record(key, Event{Ticket: id, Kind: Assigned, Actor: actor, Text: assignee})
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
		if !m.isPerson(actor) && m.role(board, actor) != RoleManager {
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
